package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"private-drive/internal/domain"
)

func TestReadCacheMixedReadersAndUpload(t *testing.T) {
	e, _, cache, id, _ := readCacheFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	original := []byte("01234567abcdefghABCDEFGH")
	start := make(chan struct{})
	errs := make(chan error, 9)
	var group sync.WaitGroup
	for reader := range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			for attempt := range 12 {
				offset := int64((reader + attempt) % 3 * 8)
				r, _, err := e.Open(ctx, id)
				if err != nil {
					errs <- err
					return
				}
				_, err = r.Seek(offset, io.SeekStart)
				var data [3]byte
				if err == nil {
					_, err = io.ReadFull(r, data[:])
				}
				r.Close()
				if err != nil || !bytes.Equal(data[:], original[offset:offset+3]) {
					errs <- fmt.Errorf("mixed range %d: %x %v", offset, data, err)
					return
				}
				cache.mu.Lock()
				bounded := cache.used+cache.reserved <= readCacheLimit && len(cache.entries)+cache.writes <= readCacheEntries
				cache.mu.Unlock()
				if !bounded {
					errs <- fmt.Errorf("mixed readers exceeded cache bounds")
					return
				}
			}
		}()
	}
	group.Add(1)
	go func() {
		defer group.Done()
		<-start
		payload := []byte("new-data")
		u, err := e.Begin(ctx, domain.UploadSpec{Name: "concurrent.bin", Size: int64(len(payload)), SHA256: digest(payload)})
		if err == nil {
			_, err = e.PutChunk(ctx, u.ID, 0, bytes.NewReader(payload), digest(payload))
		}
		if err == nil {
			var verified domain.Manifest
			verified, err = e.Complete(ctx, u.ID)
			if err == nil && verified.SHA256 != digest(payload) {
				err = domain.ErrIntegrity
			}
		}
		if err != nil {
			errs <- err
		}
	}()
	close(start)
	group.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if len(e.slots) != 0 || len(cache.flights) != 0 || cache.reserved != 0 || cache.writes != 0 {
		t.Fatal("mixed work retained buffers or unfinished fills")
	}
}

func BenchmarkReadCacheEvictionSequence100MiB(b *testing.B) {
	for _, enabled := range []bool{false, true} {
		b.Run(map[bool]string{false: "disabled", true: "enabled"}[enabled], func(b *testing.B) {
			dir := b.TempDir()
			local, err := NewLocal(filepath.Join(dir, "remote-fixture"))
			if err != nil {
				b.Fatal(err)
			}
			store := &injectedStore{BlobStore: local}
			_, db := testEngine(b, dir, store, nil)
			e, err := New(Config{DB: db, Key: bytes.Repeat([]byte{37}, 32), Backend: store, ChunkSize: DefaultChunkSize})
			if err != nil {
				b.Fatal(err)
			}
			ctx := context.Background()
			u, err := e.Begin(ctx, domain.UploadSpec{Name: "eviction-fixture.bin", Size: 3 * DefaultChunkSize})
			if err != nil {
				b.Fatal(err)
			}
			for index := range u.TotalChunks {
				if _, err := e.PutChunk(ctx, u.ID, index, io.NewSectionReader(largeChunkFixture{}, 0, DefaultChunkSize), ""); err != nil {
					b.Fatal(err)
				}
			}
			if _, err := e.Complete(ctx, u.ID); err != nil {
				b.Fatal(err)
			}
			if enabled {
				e.readCache, err = newReadCache(filepath.Join(dir, "cache"))
				if err != nil {
					b.Fatal(err)
				}
			}
			var gets, remoteBytes atomic.Int64
			store.get = func(ctx context.Context, ref string) ([]byte, error) {
				data, err := local.Get(ctx, ref)
				gets.Add(1)
				remoteBytes.Add(int64(len(data)))
				return data, err
			}
			b.SetBytes(4 * (256 << 10))
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				// Three distinct chunks exceed the two-entry cache; the return seek is cold.
				for _, index := range []int64{0, 1, 2, 0} {
					r, _, err := e.Open(ctx, u.ID)
					if err != nil {
						b.Fatal(err)
					}
					_, err = r.Seek(index*DefaultChunkSize, io.SeekStart)
					if err == nil {
						_, err = io.CopyN(io.Discard, r, 256<<10)
					}
					r.Close()
					if err != nil {
						b.Fatal(err)
					}
				}
			}
			b.StopTimer()
			// The last chunk-zero entry survives into the next iteration.
			want := int64(4 * b.N)
			if enabled {
				want = int64(3*b.N + 1)
			}
			if gets.Load() != want {
				b.Fatalf("remote fetch count %d, want %d", gets.Load(), want)
			}
			b.ReportMetric(float64(gets.Load())/float64(b.N), "remote_gets/op")
			b.ReportMetric(float64(remoteBytes.Load())/float64(b.N)/(1<<20), "remote_MiB/op")
		})
	}
}
