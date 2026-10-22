package storage

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"private-drive/internal/domain"
)

type observedReadContext struct {
	context.Context
	observed chan struct{}
	once     sync.Once
}

func (c *observedReadContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })
	return c.Context.Done()
}

func readCacheFixture(t *testing.T) (*Engine, *injectedStore, *readCache, string, []chunk) {
	t.Helper()
	e, store := injectedEngine(t)
	data := []byte("01234567abcdefghABCDEFGH")
	u := begin(t, e, data, digest(data))
	for index := range u.TotalChunks {
		put(t, e, u, data, index)
	}
	if _, err := e.Complete(context.Background(), u.ID); err != nil {
		t.Fatal(err)
	}
	cache, err := newReadCache(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	e.readCache = cache
	parts, err := e.chunks(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	return e, store, cache, u.ID, parts
}

func cachedRange(t *testing.T, e *Engine, id string, offset int64, size int) []byte {
	t.Helper()
	r, _, err := e.Open(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := r.Seek(offset, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(r, data); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestReadCacheReusesCiphertextAcrossReadersAndKeepsPrivateBuffers(t *testing.T) {
	e, store, cache, id, parts := readCacheFixture(t)
	var gets atomic.Int32
	store.get = func(ctx context.Context, ref string) ([]byte, error) {
		gets.Add(1)
		return store.BlobStore.Get(ctx, ref)
	}
	type readResult struct {
		data []byte
		err  error
	}
	cold := make(chan readResult, 1)
	go func() {
		data, err := cache.plaintext(context.Background(), e, id, parts[0])
		cold <- readResult{data, err}
	}()
	select {
	case result := <-cold:
		if result.err != nil || string(result.data) != "01234567" {
			t.Fatalf("cold cache miss changed content: %q %v", result.data, result.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cold cache miss did not complete")
	}
	for _, offset := range []int64{0, 2, 5, 1} {
		if got := cachedRange(t, e, id, offset, 2); !bytes.Equal(got, []byte("01234567")[offset:offset+2]) {
			t.Fatalf("range changed: %q", got)
		}
	}
	if gets.Load() != 1 {
		t.Fatalf("repeated HTTP-style readers fetched remote %d times", gets.Load())
	}
	private, err := cache.plaintext(context.Background(), e, id, parts[0])
	if err != nil {
		t.Fatal(err)
	}
	private[0] = 'x'
	if got := cachedRange(t, e, id, 0, 2); string(got) != "01" || gets.Load() != 1 {
		t.Fatal("caller mutation changed cached ciphertext")
	}
	entry := cache.entries[readCacheKey(id, parts[0])]
	sealed, err := os.ReadFile(filepath.Join(cache.store.root, entry.file))
	if err != nil {
		t.Fatal(err)
	}
	original, err := store.BlobStore.Get(context.Background(), parts[0].ref)
	if err != nil || !bytes.Equal(sealed, original) || !bytes.HasPrefix(sealed, []byte("HD01")) {
		t.Fatal("cache did not retain exact encrypted envelope")
	}
	for _, path := range []string{cache.store.root, filepath.Join(cache.store.root, entry.file), filepath.Join(cache.store.root, readCacheMarker)} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != map[bool]os.FileMode{true: 0700, false: 0600}[info.IsDir()] {
			t.Fatalf("private cache permissions incorrect: %v", err)
		}
	}
	if len(e.slots) != 0 {
		t.Fatal("cached readers retained transfer slots")
	}
}

func TestReadCacheAuthenticatesEveryHitAndRepairsOnce(t *testing.T) {
	for _, remoteMissing := range []bool{false, true} {
		t.Run(map[bool]string{false: "repair", true: "remote_missing"}[remoteMissing], func(t *testing.T) {
			e, store, cache, id, parts := readCacheFixture(t)
			cachedRange(t, e, id, 0, 1)
			key := readCacheKey(id, parts[0])
			entry := cache.entries[key]
			path := filepath.Join(cache.store.root, entry.file)
			sealed, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			sealed[len(sealed)-1] ^= 1
			if err := os.WriteFile(path, sealed, 0600); err != nil {
				t.Fatal(err)
			}
			gets := 0
			store.get = func(ctx context.Context, ref string) ([]byte, error) {
				gets++
				if remoteMissing {
					return nil, domain.ErrNotFound
				}
				return store.BlobStore.Get(ctx, ref)
			}
			got, err := cache.plaintext(context.Background(), e, id, parts[0])
			if gets != 1 {
				t.Fatalf("corrupt cache triggered %d remote repairs", gets)
			}
			if remoteMissing {
				if !errors.Is(err, domain.ErrNotFound) || got != nil || cache.entries[key] != nil {
					t.Fatalf("missing remote returned corrupt cache: %v", err)
				}
			} else if err != nil || string(got) != "01234567" {
				t.Fatalf("authenticated remote repair failed: %v", err)
			}
		})
	}
}

func TestReadCacheRejectsRemoteCorruptionAndManifestSubstitution(t *testing.T) {
	for _, change := range []string{"ciphertext", "hash", "size", "identity", "index"} {
		t.Run(change, func(t *testing.T) {
			e, store, cache, id, parts := readCacheFixture(t)
			part := parts[0]
			switch change {
			case "hash":
				part.hash = digest([]byte("different"))
			case "size":
				part.size++
			case "identity":
				id = domain.NewID()
			case "index":
				part.index++
			case "ciphertext":
				store.get = func(ctx context.Context, ref string) ([]byte, error) {
					sealed, err := store.BlobStore.Get(ctx, ref)
					if err == nil {
						sealed[len(sealed)-1] ^= 1
					}
					return sealed, err
				}
			}
			got, err := cache.plaintext(context.Background(), e, id, part)
			if !errors.Is(err, domain.ErrIntegrity) || got != nil || len(cache.entries) != 0 || cache.used != 0 || cache.reserved != 0 {
				t.Fatalf("unverified bytes escaped or entered cache: %v", err)
			}
			files, err := os.ReadDir(cache.store.root)
			if err != nil || len(files) != 1 || files[0].Name() != readCacheMarker {
				t.Fatal("invalid ciphertext fill was not removed")
			}
		})
	}
}

func TestReadCacheCoalescesMissesAndWaiterCancellation(t *testing.T) {
	e, store, cache, id, parts := readCacheFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var gets atomic.Int32
	store.get = func(ctx context.Context, ref string) ([]byte, error) {
		if gets.Add(1) == 1 {
			close(entered)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
			return store.BlobStore.Get(ctx, ref)
		}
	}
	leader := make(chan error, 1)
	go func() { _, err := cache.plaintext(context.Background(), e, id, parts[0]); leader <- err }()
	<-entered
	base, cancel := context.WithCancel(context.Background())
	ctx := &observedReadContext{Context: base, observed: make(chan struct{})}
	waiter := make(chan error, 1)
	go func() { _, err := cache.plaintext(ctx, e, id, parts[0]); waiter <- err }()
	select {
	case <-ctx.observed:
	case <-time.After(time.Second):
		t.Fatal("waiter never reached the pending read")
	}
	cancel()
	select {
	case err := <-waiter:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled waiter returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("waiter cancellation blocked on remote fetch")
	}
	var group sync.WaitGroup
	errs := make(chan error, 3)
	for range 3 {
		group.Add(1)
		go func() {
			defer group.Done()
			got, err := cache.plaintext(context.Background(), e, id, parts[0])
			if err == nil && string(got) != "01234567" {
				err = domain.ErrIntegrity
			}
			errs <- err
		}()
	}
	close(release)
	if err := <-leader; err != nil {
		t.Fatal(err)
	}
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if gets.Load() != 1 || len(cache.flights) != 0 || cache.reserved != 0 || cache.writes != 0 {
		t.Fatalf("identical misses were not coalesced: %d fetches", gets.Load())
	}
}

func TestReadCacheCanceledLeaderDoesNotCancelAnotherReader(t *testing.T) {
	e, store, cache, id, parts := readCacheFixture(t)
	entered := make(chan struct{})
	var gets atomic.Int32
	store.get = func(ctx context.Context, ref string) ([]byte, error) {
		if gets.Add(1) == 1 {
			close(entered)
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return store.BlobStore.Get(ctx, ref)
	}
	ctx, cancel := context.WithCancel(context.Background())
	leader := make(chan error, 1)
	go func() { _, err := cache.plaintext(ctx, e, id, parts[0]); leader <- err }()
	<-entered
	waiter := make(chan error, 1)
	waiterCtx := &observedReadContext{Context: context.Background(), observed: make(chan struct{})}
	go func() {
		got, err := cache.plaintext(waiterCtx, e, id, parts[0])
		if err == nil && string(got) != "01234567" {
			err = domain.ErrIntegrity
		}
		waiter <- err
	}()
	select {
	case <-waiterCtx.observed:
	case <-time.After(time.Second):
		t.Fatal("independent waiter never joined pending read")
	}
	cancel()
	if err := <-leader; !errors.Is(err, context.Canceled) {
		t.Fatalf("leader ignored cancellation: %v", err)
	}
	select {
	case err := <-waiter:
		if err != nil {
			t.Fatalf("another request inherited canceled context: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("independent waiter failed to retry canceled leader")
	}
	if gets.Load() != 2 || cache.disabled {
		t.Fatalf("canceled fill was retained or disabled cache: %d fetches", gets.Load())
	}
}

func TestReadCacheEvictionReservationsAndInvalidation(t *testing.T) {
	e, _, cache, id, parts := readCacheFixture(t)
	for _, index := range []int{0, 1, 0, 2} {
		if _, err := cache.plaintext(context.Background(), e, id, parts[index]); err != nil {
			t.Fatal(err)
		}
	}
	if len(cache.entries) != 2 || cache.entries[readCacheKey(id, parts[1])] != nil || cache.used > readCacheLimit {
		t.Fatal("LRU did not keep exactly two bounded entries")
	}
	cache.mu.Lock()
	first, second, third := cache.reserveLocked(maxCiphertextSize), cache.reserveLocked(maxCiphertextSize), cache.reserveLocked(maxCiphertextSize)
	if !first || !second || third || cache.used+cache.reserved > readCacheLimit || len(cache.entries)+cache.writes > readCacheEntries {
		t.Fatal("in-flight writes bypassed disk bound")
	}
	cache.reserved, cache.writes = 0, 0
	cache.mu.Unlock()
	if _, err := cache.plaintext(context.Background(), e, id, parts[0]); err != nil {
		t.Fatal(err)
	}
	cache.invalidate(parts[0].ref)
	if len(cache.entries) != 0 || cache.used != 0 {
		t.Fatal("delete invalidation retained cached ciphertext")
	}
}

func TestReadCacheInvalidationPreventsLatePublication(t *testing.T) {
	e, store, cache, id, parts := readCacheFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	store.get = func(ctx context.Context, ref string) ([]byte, error) {
		close(entered)
		<-release
		return store.BlobStore.Get(ctx, ref)
	}
	finished := make(chan error, 1)
	go func() { _, err := cache.plaintext(context.Background(), e, id, parts[0]); finished <- err }()
	<-entered
	cache.invalidate(parts[0].ref)
	close(release)
	if err := <-finished; err != nil || len(cache.entries) != 0 || cache.used != 0 {
		t.Fatalf("invalidated fill was published: %v", err)
	}
}

func TestReadCacheRestartDropsDisposableCiphertextAndRefusesOtherFiles(t *testing.T) {
	e, _, cache, id, parts := readCacheFixture(t)
	if _, err := cache.plaintext(context.Background(), e, id, parts[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := newReadCache(cache.store.root); err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(cache.store.root)
	if err != nil || len(files) != 1 || files[0].Name() != readCacheMarker {
		t.Fatal("restart reused or leaked old cache state")
	}
	other := t.TempDir()
	path := filepath.Join(other, "preserve.db")
	if err := os.WriteFile(path, []byte("unrelated fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := newReadCache(other); err == nil {
		t.Fatal("cache accepted unrelated nonempty directory")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "unrelated fixture" {
		t.Fatal("cache modified unrelated data")
	}
	link := filepath.Join(t.TempDir(), "cache-link")
	if err := os.Symlink(other, link); err != nil {
		t.Fatal(err)
	}
	if _, err := newReadCache(link); err == nil {
		t.Fatal("cache followed a directory symlink")
	}
}

func TestReadCacheWriteFailureFallsBackWithoutPlaintextStaging(t *testing.T) {
	e, _, cache, id, parts := readCacheFixture(t)
	if err := os.Remove(filepath.Join(cache.store.root, readCacheMarker)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(cache.store.root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cache.store.root, []byte("unavailable fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		got, err := cache.plaintext(context.Background(), e, id, parts[0])
		if err != nil || string(got) != "01234567" {
			t.Fatalf("optional cache IO failure blocked read: %v", err)
		}
	}
	if !cache.disabled || len(cache.entries) != 0 || cache.reserved != 0 || cache.writes != 0 {
		t.Fatal("failed cache continued to stage data")
	}
}

func TestReadCacheWrongKeyAndSymlinkNeverExposeData(t *testing.T) {
	e, _, cache, id, parts := readCacheFixture(t)
	cachedRange(t, e, id, 0, 1)
	block, err := aes.NewCipher(bytes.Repeat([]byte{93}, 32))
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	wrong := &Engine{backend: e.backend, aead: aead}
	if data, err := cache.plaintext(context.Background(), wrong, id, parts[0]); !errors.Is(err, domain.ErrIntegrity) || data != nil || len(cache.entries) != 0 {
		t.Fatalf("wrong content key accepted cached bytes: %v", err)
	}
	cachedRange(t, e, id, 0, 1)
	entry := cache.entries[readCacheKey(id, parts[0])]
	path := filepath.Join(cache.store.root, entry.file)
	outside := filepath.Join(t.TempDir(), "preserve.txt")
	if err := os.WriteFile(outside, []byte("unrelated fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if got := cachedRange(t, e, id, 0, 1); string(got) != "0" {
		t.Fatal("cache symlink changed plaintext")
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "unrelated fixture" {
		t.Fatal("cache followed or deleted symlink target")
	}
}

func TestReadCacheDeleteInvalidatesAndCompletionStillVerifiesRemote(t *testing.T) {
	e, store, cache, id, parts := readCacheFixture(t)
	cachedRange(t, e, id, 0, 1)
	if err := e.DeleteFile(context.Background(), id); err != nil || len(cache.entries) != 0 || cache.used != 0 {
		t.Fatalf("completed deletion retained cache: %v", err)
	}
	data := []byte("newfile!")
	u := begin(t, e, data, digest(data))
	put(t, e, u, data, 0)
	parts, err := e.chunks(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cache.plaintext(context.Background(), e, u.ID, parts[0]); err != nil {
		t.Fatal(err)
	}
	network := errors.New("injected remote outage")
	store.get = func(context.Context, string) ([]byte, error) { return nil, network }
	if _, err := e.Complete(context.Background(), u.ID); !errors.Is(err, network) {
		t.Fatalf("final verification trusted cache instead of remote: %v", err)
	}
	state, err := e.Status(context.Background(), u.ID)
	if err != nil || state.Status == "complete" {
		t.Fatal("unverified file appeared complete")
	}
}

func BenchmarkReadCacheRangeSequence100MiB(b *testing.B) {
	for _, enabled := range []bool{false, true} {
		name := "disabled"
		if enabled {
			name = "enabled"
		}
		b.Run(name, func(b *testing.B) {
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
			u, err := e.Begin(context.Background(), domain.UploadSpec{Name: "range-fixture.bin", Size: DefaultChunkSize})
			if err != nil {
				b.Fatal(err)
			}
			if _, err := e.PutChunk(context.Background(), u.ID, 0, io.NewSectionReader(largeChunkFixture{}, 0, u.Size), ""); err != nil {
				b.Fatal(err)
			}
			if _, err := e.Complete(context.Background(), u.ID); err != nil {
				b.Fatal(err)
			}
			parts, err := e.chunks(context.Background(), u.ID)
			if err != nil {
				b.Fatal(err)
			}
			if enabled {
				e.readCache, err = newReadCache(filepath.Join(dir, "cache"))
				if err != nil {
					b.Fatal(err)
				}
			}
			var gets atomic.Int64
			store.get = func(ctx context.Context, ref string) ([]byte, error) {
				gets.Add(1)
				// Synthetic fixed per-document latency; never a Telegram throughput claim.
				timer := time.NewTimer(20 * time.Millisecond)
				defer timer.Stop()
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-timer.C:
					return local.Get(ctx, ref)
				}
			}
			b.SetBytes(4 * (256 << 10))
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if enabled {
					b.StopTimer()
					e.readCache.invalidate(parts[0].ref)
					b.StartTimer()
				}
				for _, offset := range []int64{0, 256 << 10, 512 << 10, 768 << 10} {
					r, _, err := e.Open(context.Background(), u.ID)
					if err != nil {
						b.Fatal(err)
					}
					_, err = r.Seek(offset, io.SeekStart)
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
			want := int64(4 * b.N)
			if enabled {
				want = int64(b.N)
			}
			if gets.Load() != want {
				b.Fatalf("remote fetch count %d, want %d", gets.Load(), want)
			}
			b.ReportMetric(float64(gets.Load())/float64(b.N), "remote_gets/op")
			b.ReportMetric(float64(gets.Load())*float64(maxCiphertextSize)/float64(b.N)/(1<<20), "remote_MiB/op")
		})
	}
}
