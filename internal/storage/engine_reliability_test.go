package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"time"

	"private-drive/internal/domain"
)

type injectedStore struct {
	BlobStore
	put    func(context.Context, string, []byte) (string, error)
	get    func(context.Context, string) ([]byte, error)
	delete func(context.Context, string) error
}

func (s *injectedStore) Put(ctx context.Context, id string, data []byte) (string, error) {
	if s.put != nil {
		return s.put(ctx, id, data)
	}
	return s.BlobStore.Put(ctx, id, data)
}
func (s *injectedStore) Get(ctx context.Context, ref string) ([]byte, error) {
	if s.get != nil {
		return s.get(ctx, ref)
	}
	return s.BlobStore.Get(ctx, ref)
}
func (s *injectedStore) Delete(ctx context.Context, ref string) error {
	if s.delete != nil {
		return s.delete(ctx, ref)
	}
	return s.BlobStore.Delete(ctx, ref)
}

func injectedEngine(t *testing.T) (*Engine, *injectedStore) {
	t.Helper()
	dir := t.TempDir()
	local, err := NewLocal(filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	store := &injectedStore{BlobStore: local}
	e, _ := testEngine(t, dir, store, nil)
	return e, store
}

func TestCanceledSuccessfulPutRollsBackBeforeRetry(t *testing.T) {
	e, store := injectedEngine(t)
	data := []byte("fixture!")
	u := begin(t, e, data, digest(data))
	stored, release := make(chan struct{}), make(chan struct{})
	var nonces [][]byte
	store.put = func(ctx context.Context, id string, data []byte) (string, error) {
		nonces = append(nonces, bytes.Clone(data[4:4+e.aead.NonceSize()]))
		ref, err := store.BlobStore.Put(ctx, id, data)
		if err != nil {
			return "", err
		}
		close(stored)
		<-release
		return ref, nil
	}
	var deletions int
	store.delete = func(ctx context.Context, ref string) error {
		deletions++
		if err := ctx.Err(); err != nil {
			return err
		}
		return store.BlobStore.Delete(ctx, ref)
	}
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() { _, err := e.PutChunk(ctx, u.ID, 0, bytes.NewReader(data), ""); finished <- err }()
	select {
	case <-stored:
	case <-time.After(5 * time.Second):
		t.Fatal("adapter did not persist fixture")
	}
	cancel()
	close(release)
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel after remote success: %v", err)
	}
	if deletions != 1 {
		t.Fatalf("rollback deletes: %d", deletions)
	}
	state, err := e.Status(context.Background(), u.ID)
	if err != nil || state.ReceivedBytes != 0 || state.Status != "uploading" {
		t.Fatalf("canceled write published state: %+v %v", state, err)
	}
	if len(e.slots) != 0 {
		t.Fatal("canceled write leaked slot")
	}
	store.put = func(ctx context.Context, id string, data []byte) (string, error) {
		nonces = append(nonces, bytes.Clone(data[4:4+e.aead.NonceSize()]))
		return store.BlobStore.Put(ctx, id, data)
	}
	put(t, e, u, data, 0)
	if len(nonces) != 2 || bytes.Equal(nonces[0], nonces[1]) {
		t.Fatal("retry reused encryption nonce")
	}
	if _, err := e.Complete(context.Background(), u.ID); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentDuplicateChunksProduceOneBlob(t *testing.T) {
	e, store := injectedEngine(t)
	var puts atomic.Int32
	store.put = func(ctx context.Context, id string, data []byte) (string, error) {
		puts.Add(1)
		return store.BlobStore.Put(ctx, id, data)
	}
	data := []byte("fixture!")
	u := begin(t, e, data, digest(data))
	gate := make(chan struct{})
	errs := make(chan error, 8)
	for range 8 {
		go func() {
			<-gate
			_, err := e.PutChunk(context.Background(), u.ID, 0, bytes.NewReader(data), "")
			errs <- err
		}()
	}
	close(gate)
	for range 8 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	state, err := e.Status(context.Background(), u.ID)
	if err != nil || len(state.ReceivedChunks) != 1 || state.ReceivedBytes != int64(len(data)) || puts.Load() != 1 {
		t.Fatalf("duplicate mutation: %+v puts=%d %v", state, puts.Load(), err)
	}
}

func TestFailedVerificationRemainsUnpublishedAndReaderRetries(t *testing.T) {
	e, store := injectedEngine(t)
	ctx := context.Background()
	data := []byte("fixture!chunktwo")
	u := begin(t, e, data, digest(data))
	for i := 0; i < u.TotalChunks; i++ {
		put(t, e, u, data, i)
	}
	chunks, err := e.chunks(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	network := errors.New("injected network loss")
	fail := true
	store.get = func(ctx context.Context, ref string) ([]byte, error) {
		if fail && ref == chunks[1].ref {
			return nil, network
		}
		return store.BlobStore.Get(ctx, ref)
	}
	if _, err := e.Complete(ctx, u.ID); !errors.Is(err, network) {
		t.Fatalf("verification failure: %v", err)
	}
	state, err := e.Status(ctx, u.ID)
	if err != nil || state.Status != "uploading" {
		t.Fatalf("failed verification published: %+v %v", state, err)
	}
	if _, _, err := e.Open(ctx, u.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("unverified file opened: %v", err)
	}
	fail = false
	if _, err := e.Complete(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	r, _, err := e.Open(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	buf := make([]byte, 8)
	if n, err := r.Read(buf); n != 8 || err != nil || !bytes.Equal(buf, data[:8]) {
		t.Fatalf("first chunk: %d %v", n, err)
	}
	fail = true
	if n, err := r.Read(buf); n != 0 || !errors.Is(err, network) {
		t.Fatalf("failed chunk released plaintext: %d %v", n, err)
	}
	fail = false
	if n, err := r.Read(buf); n != 8 || err != nil || !bytes.Equal(buf, data[8:]) {
		t.Fatalf("failed chunk retry position: %d %v", n, err)
	}
}

func TestReaderBackpressureSeekAndCancellation(t *testing.T) {
	e, store := injectedEngine(t)
	ctx := context.Background()
	data := []byte("fixture!chunktwo")
	u := begin(t, e, data, digest(data))
	for i := 0; i < u.TotalChunks; i++ {
		put(t, e, u, data, i)
	}
	if _, err := e.Complete(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	gets := 0
	store.get = func(ctx context.Context, ref string) ([]byte, error) { gets++; return store.BlobStore.Get(ctx, ref) }
	readCtx, cancel := context.WithCancel(ctx)
	r, _, err := e.Open(readCtx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if gets != 0 {
		t.Fatal("Open eagerly fetched content")
	}
	buf := make([]byte, 1)
	if n, err := r.Read(buf); n != 1 || err != nil {
		t.Fatalf("first byte: %d %v", n, err)
	}
	if gets != 1 {
		t.Fatalf("single-byte read prefetched: %d", gets)
	}
	if _, err := r.Seek(3, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if n, err := r.Read(buf); n != 1 || err != nil || buf[0] != data[3] || gets != 1 {
		t.Fatalf("same-chunk seek: %d %d %v", n, gets, err)
	}
	cancel()
	if n, err := r.Read(buf); n != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled reader: %d %v", n, err)
	}
	r.Close()
	r.Close()
	if len(e.slots) != 0 {
		t.Fatal("reader leaked or double released slot")
	}
}

func TestBlockedReadersCancelReleaseAllSlots(t *testing.T) {
	e, store := injectedEngine(t)
	data := []byte("fixture!")
	u := begin(t, e, data, digest(data))
	put(t, e, u, data, 0)
	if _, err := e.Complete(context.Background(), u.ID); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 4)
	store.get = func(ctx context.Context, ref string) ([]byte, error) {
		started <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for range 4 {
		r, _, err := e.Open(ctx, u.ID)
		if err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func() { defer wg.Done(); defer r.Close(); _, err := r.Read(make([]byte, 1)); errs <- err }()
	}
	for range 4 {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("reader did not reach backend")
		}
	}
	cancel()
	wg.Wait()
	for range 4 {
		if err := <-errs; !errors.Is(err, context.Canceled) {
			t.Fatalf("blocked reader cancellation: %v", err)
		}
	}
	if len(e.slots) != 0 {
		t.Fatal("canceled readers leaked transfer slots")
	}
}

func TestServeContentSeekPatternKeepsFirstChunk(t *testing.T) {
	e, store := injectedEngine(t)
	data := []byte("fixture!chunktwo")
	u := begin(t, e, data, digest(data))
	for i := 0; i < u.TotalChunks; i++ {
		put(t, e, u, data, i)
	}
	if _, err := e.Complete(context.Background(), u.ID); err != nil {
		t.Fatal(err)
	}
	gets := 0
	store.get = func(ctx context.Context, ref string) ([]byte, error) { gets++; return store.BlobStore.Get(ctx, ref) }
	r, _, err := e.Open(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if n, err := r.Read(make([]byte, 1)); n != 1 || err != nil {
		t.Fatalf("preflight: %d %v", n, err)
	}
	for _, seek := range []struct {
		offset int64
		whence int
	}{{0, io.SeekStart}, {0, io.SeekEnd}, {0, io.SeekStart}} {
		if _, err := r.Seek(seek.offset, seek.whence); err != nil {
			t.Fatal(err)
		}
	}
	got := make([]byte, 3)
	if n, err := r.Read(got); n != len(got) || err != nil || !bytes.Equal(got, data[:len(got)]) {
		t.Fatalf("after size seek: %q %d %v", got, n, err)
	}
	if gets != 1 {
		t.Fatalf("preflight fetched first encrypted chunk %d times; want 1", gets)
	}
}

func TestReaderCanSeekBackAfterFailedFetch(t *testing.T) {
	e, store := injectedEngine(t)
	data := []byte("fixture!chunktwo")
	u := begin(t, e, data, digest(data))
	for i := 0; i < u.TotalChunks; i++ {
		put(t, e, u, data, i)
	}
	if _, err := e.Complete(context.Background(), u.ID); err != nil {
		t.Fatal(err)
	}
	chunks, err := e.chunks(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	network := errors.New("injected network loss")
	store.get = func(ctx context.Context, ref string) ([]byte, error) {
		if ref == chunks[1].ref {
			return nil, network
		}
		return store.BlobStore.Get(ctx, ref)
	}
	r, _, err := e.Open(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if n, err := r.Read(make([]byte, 1)); n != 1 || err != nil {
		t.Fatalf("preflight: %d %v", n, err)
	}
	if _, err := r.Seek(8, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if n, err := r.Read(make([]byte, 1)); n != 0 || !errors.Is(err, network) {
		t.Fatalf("failed fetch: %d %v", n, err)
	}
	if _, err := r.Seek(3, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 1)
	if n, err := r.Read(got); n != 1 || err != nil || got[0] != data[3] {
		t.Fatalf("seek after failed fetch: %d %v", n, err)
	}
}

func TestCorruptManifestNeverCompletes(t *testing.T) {
	for name, mutation := range map[string]string{
		"missing chunk":      `DELETE FROM storage_chunks WHERE upload_id=? AND idx=1`,
		"out of range index": `UPDATE storage_chunks SET idx=3 WHERE upload_id=? AND idx=1`,
		"wrong chunk size":   `UPDATE storage_chunks SET size=CASE idx WHEN 0 THEN 9 ELSE 7 END WHERE upload_id=?`,
		"invalid digest":     `UPDATE storage_chunks SET sha256='invalid' WHERE upload_id=? AND idx=1`,
		"empty reference":    `UPDATE storage_chunks SET ref='' WHERE upload_id=? AND idx=1`,
		"wrong whole digest": `UPDATE storage_uploads SET expected_sha256='0000000000000000000000000000000000000000000000000000000000000000' WHERE id=?`,
	} {
		t.Run(name, func(t *testing.T) {
			e, db := testEngine(t, t.TempDir(), nil, nil)
			data := []byte("fixture!chunktwo")
			u := begin(t, e, data, digest(data))
			for i := 0; i < u.TotalChunks; i++ {
				put(t, e, u, data, i)
			}
			if _, err := db.Exec(mutation, u.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := e.Complete(context.Background(), u.ID); err == nil {
				t.Fatal("corrupt manifest completed")
			}
			state, err := e.Status(context.Background(), u.ID)
			if err != nil || state.Status != "uploading" {
				t.Fatalf("corrupt file became complete: %+v %v", state, err)
			}
			if _, _, err := e.Open(context.Background(), u.ID); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("corrupt incomplete file opened: %v", err)
			}
		})
	}
}

func TestChunkInputBoundsAndReadErrors(t *testing.T) {
	data := []byte("fixture!")
	failure := errors.New("injected body error")
	for _, tc := range []struct {
		name string
		body io.Reader
		want error
	}{
		{"exact", bytes.NewReader(data), nil},
		{"one byte reads", iotest.OneByteReader(bytes.NewReader(data)), nil},
		{"EOF with final bytes", iotest.DataErrReader(bytes.NewReader(data)), nil},
		{"short", bytes.NewReader(data[:7]), domain.ErrInvalid},
		{"oversize", bytes.NewReader(append(bytes.Clone(data), 0)), domain.ErrInvalid},
		{"body failure after exact bytes", io.MultiReader(bytes.NewReader(data), iotest.ErrReader(failure)), failure},
		{"explicit unexpected EOF", io.MultiReader(bytes.NewReader(data), iotest.ErrReader(io.ErrUnexpectedEOF)), io.ErrUnexpectedEOF},
		{"canceled body", iotest.ErrReader(context.Canceled), context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, _ := testEngine(t, t.TempDir(), nil, nil)
			u := begin(t, e, data, digest(data))
			_, err := e.PutChunk(context.Background(), u.ID, 0, tc.body, "")
			if !errors.Is(err, tc.want) {
				t.Fatalf("chunk read error: %v; want %v", err, tc.want)
			}
			if tc.want != nil {
				state, err := e.Status(context.Background(), u.ID)
				if err != nil || state.ReceivedBytes != 0 {
					t.Fatalf("failed read persisted chunk: %+v %v", state, err)
				}
			}
		})
	}
	e, _ := testEngine(t, t.TempDir(), nil, nil)
	u := begin(t, e, data, digest(data))
	large := bytes.NewReader(bytes.Repeat([]byte{1}, 1024))
	if _, err := e.PutChunk(context.Background(), u.ID, 0, large, ""); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("oversize chunk: %v", err)
	}
	if consumed := large.Size() - int64(large.Len()); consumed != u.ChunkSize+1 {
		t.Fatalf("body read past limit: %d", consumed)
	}
}

func TestPersistedUploadGeometryBounds(t *testing.T) {
	for _, tc := range []struct {
		name            string
		size, chunkSize int64
		invalid         bool
	}{
		{"zero chunk size", 8, 0, true},
		{"negative chunk size", 8, -1, true},
		{"oversize chunk size", 1 << 40, 1 << 39, true},
		{"negative file size", -1, 8, true},
		{"too many chunks", 1<<31 - 1, 1, true},
		{"empty file", 0, DefaultChunkSize, false},
		{"maximum chunk size", 64 << 20, 64 << 20, false},
		{"maximum chunk count with remainder", (1<<31-2)*8 + 7, 8, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, db := testEngine(t, t.TempDir(), nil, nil)
			u := begin(t, e, []byte("fixture!"), "")
			// Simulate corrupt persisted values even where SQLite normally enforces CHECK.
			if _, err := db.Exec("PRAGMA ignore_check_constraints=ON"); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`UPDATE storage_uploads SET size=?,chunk_size=? WHERE id=?`, tc.size, tc.chunkSize, u.ID); err != nil {
				t.Fatal(err)
			}
			state, err := e.Status(context.Background(), u.ID)
			if !tc.invalid {
				if err != nil || state.Size != tc.size || state.ChunkSize != tc.chunkSize {
					t.Fatalf("valid boundary rejected: %+v %v", state, err)
				}
				return
			}
			if !errors.Is(err, domain.ErrIntegrity) {
				t.Fatalf("corrupt geometry status: %v", err)
			}
			if _, err := e.PutChunk(context.Background(), u.ID, 0, bytes.NewReader([]byte("fixture!")), ""); !errors.Is(err, domain.ErrIntegrity) {
				t.Fatalf("corrupt geometry upload: %v", err)
			}
			if _, err := e.Complete(context.Background(), u.ID); !errors.Is(err, domain.ErrIntegrity) {
				t.Fatalf("corrupt geometry completion: %v", err)
			}
			if _, _, err := e.Open(context.Background(), u.ID); !errors.Is(err, domain.ErrIntegrity) {
				t.Fatalf("corrupt geometry open: %v", err)
			}
		})
	}
}
