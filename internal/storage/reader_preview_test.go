package storage

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"sync/atomic"
	"testing"

	"private-drive/internal/domain"
)

func TestPreviewPreflightRetainsAuthenticatedBoundaryChunk(t *testing.T) {
	for _, offset := range []int64{7, 23} {
		e, store, _, id, _ := readCacheFixture(t)
		e.readCache = nil
		var gets atomic.Int32
		store.get = func(ctx context.Context, ref string) ([]byte, error) {
			gets.Add(1)
			return store.BlobStore.Get(ctx, ref)
		}
		r, _, err := e.Open(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if _, err := r.Seek(offset, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			var data [1]byte
			if _, err := io.ReadFull(r, data[:]); err != nil || data[0] != []byte("01234567abcdefghABCDEFGH")[offset] {
				t.Fatalf("preflight/range mismatch at %d: %x %v", offset, data, err)
			}
		}
		if gets.Load() != 1 {
			t.Errorf("preflight plus seek-back at %d fetched the same chunk %d times", offset, gets.Load())
		}
		if err := r.Close(); err != nil || len(e.slots) != 0 {
			t.Fatal("closed preview retained a transfer slot")
		}
		if r.(*chunkReader).data != nil {
			t.Fatal("closed preview retained plaintext")
		}
	}
}

func BenchmarkPreviewBoundaryPreflight100MiB(b *testing.B) {
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
	u, err := e.Begin(ctx, domain.UploadSpec{Name: "preview-boundary.bin", Size: DefaultChunkSize})
	if err != nil {
		b.Fatal(err)
	}
	if _, err := e.PutChunk(ctx, u.ID, 0, io.NewSectionReader(largeChunkFixture{}, 0, u.Size), ""); err != nil {
		b.Fatal(err)
	}
	if _, err := e.Complete(ctx, u.ID); err != nil {
		b.Fatal(err)
	}
	var gets atomic.Int64
	store.get = func(ctx context.Context, ref string) ([]byte, error) {
		gets.Add(1)
		return local.Get(ctx, ref)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		r, _, err := e.Open(ctx, u.ID)
		if err != nil {
			b.Fatal(err)
		}
		for range 2 {
			if _, err := r.Seek(u.Size-1, io.SeekStart); err != nil {
				b.Fatal(err)
			}
			var data [1]byte
			if _, err := io.ReadFull(r, data[:]); err != nil {
				b.Fatal(err)
			}
			var expected [1]byte
			largeChunkFixture{}.ReadAt(expected[:], u.Size-1)
			if data != expected {
				b.Fatal("boundary preview bytes changed")
			}
		}
		r.Close()
	}
	b.StopTimer()
	b.ReportMetric(float64(gets.Load())/float64(b.N), "remote_gets/op")
	b.ReportMetric(float64(gets.Load())*float64(DefaultChunkSize)/(1<<20)/float64(b.N), "authenticated_MiB/op")
}
