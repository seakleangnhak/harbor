package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"private-drive/internal/domain"

	_ "modernc.org/sqlite"
)

func testEngine(t testing.TB, dir string, backend BlobStore, key []byte) (*Engine, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(dir, "drive.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000"); err != nil {
		t.Fatal(err)
	}
	if backend == nil {
		backend, err = NewLocal(filepath.Join(dir, "blobs"))
		if err != nil {
			t.Fatal(err)
		}
	}
	if key == nil {
		key = bytes.Repeat([]byte{37}, 32)
	}
	e, err := New(Config{DB: db, Key: key, Backend: backend, ChunkSize: 8})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return e, db
}

func digest(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }

func begin(t testing.TB, e *Engine, data []byte, hash string) domain.Upload {
	t.Helper()
	u, err := e.Begin(context.Background(), domain.UploadSpec{Name: "fixture.txt", Size: int64(len(data)), MimeType: "text/plain", SHA256: hash})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func put(t testing.TB, e *Engine, u domain.Upload, data []byte, index int) {
	t.Helper()
	start := int64(index) * u.ChunkSize
	end := min(start+u.ChunkSize, int64(len(data)))
	part := data[start:end]
	if _, err := e.PutChunk(context.Background(), u.ID, index, bytes.NewReader(part), digest(part)); err != nil {
		t.Fatal(err)
	}
}

func TestResumeRestartRoundTripAndSeeking(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	e, db := testEngine(t, dir, nil, nil)
	data := []byte("private fixture crossing chunk boundaries")
	u := begin(t, e, data, digest(data))
	put(t, e, u, data, 0)
	put(t, e, u, data, 0)
	entries, err := os.ReadDir(filepath.Join(dir, "blobs"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("retry created extra blob: %v %d", err, len(entries))
	}
	if _, err := e.PutChunk(ctx, u.ID, 0, strings.NewReader("mismatch"), ""); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("mismatched retry: %v", err)
	}
	if _, err := e.Complete(ctx, u.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("incomplete completion: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	e, _ = testEngine(t, dir, nil, nil)
	resumed, err := e.Status(ctx, u.ID)
	if err != nil || resumed.ReceivedBytes != 8 || len(resumed.ReceivedChunks) != 1 {
		t.Fatalf("restart lost state: %+v %v", resumed, err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, u.TotalChunks)
	for i := 1; i < u.TotalChunks; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			start := int64(index) * u.ChunkSize
			end := min(start+u.ChunkSize, int64(len(data)))
			_, err := e.PutChunk(ctx, u.ID, index, bytes.NewReader(data[start:end]), "")
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	m, err := e.Complete(ctx, u.ID)
	if err != nil || m.SHA256 != digest(data) || m.Size != int64(len(data)) {
		t.Fatalf("complete: %+v %v", m, err)
	}
	m2, err := e.Complete(ctx, u.ID)
	if err != nil || m != m2 {
		t.Fatalf("completion retry: %+v %v", m2, err)
	}
	r, _, err := e.Open(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	got, err := io.ReadAll(r)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("roundtrip: %q %v", got, err)
	}
	if _, err := r.Seek(11, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	got, err = io.ReadAll(io.LimitReader(r, 19))
	if err != nil || !bytes.Equal(got, data[11:30]) {
		t.Fatalf("cross-chunk range: %q %v", got, err)
	}
	if _, err := r.Seek(-5, io.SeekEnd); err != nil {
		t.Fatal(err)
	}
	got, err = io.ReadAll(r)
	if err != nil || !bytes.Equal(got, data[len(data)-5:]) {
		t.Fatalf("end seek: %q %v", got, err)
	}
	if _, err := r.Seek(-1, io.SeekStart); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("negative seek: %v", err)
	}
	if err := e.Cancel(ctx, u.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("cancel deleted completed file: %v", err)
	}
	all, err := e.List(ctx)
	if err != nil || len(all) != 1 || all[0].Status != "complete" {
		t.Fatalf("completed recovery listing: %+v %v", all, err)
	}
}

func TestAuthenticatedChunksRejectCorruptionBeforeRead(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	e, _ := testEngine(t, dir, nil, nil)
	data := []byte("secret Asecret B")
	u := begin(t, e, data, "")
	for i := 0; i < u.TotalChunks; i++ {
		put(t, e, u, data, i)
	}
	if _, err := e.Complete(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	chunks, err := e.chunks(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "blobs", chunks[1].ref)
	sealed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, data[8:]) {
		t.Fatal("plaintext persisted in blob")
	}
	sealed[len(sealed)-1] ^= 0x80
	if err := os.WriteFile(path, sealed, 0600); err != nil {
		t.Fatal(err)
	}
	r, _, err := e.Open(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	got := make([]byte, 8)
	if n, err := r.Read(got); err != nil || n != 8 || !bytes.Equal(got, data[:8]) {
		t.Fatalf("intact chunk: %q %d %v", got, n, err)
	}
	if n, err := r.Read(got); n != 0 || !errors.Is(err, domain.ErrIntegrity) {
		t.Fatalf("corrupt chunk emitted bytes: %d %v", n, err)
	}
	if _, err := r.Seek(10, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if n, err := r.Read(got); n != 0 || !errors.Is(err, domain.ErrIntegrity) {
		t.Fatalf("corrupt ranged read emitted bytes: %d %v", n, err)
	}
}

func TestEmptyInvalidAndDigestMismatch(t *testing.T) {
	ctx := context.Background()
	e, _ := testEngine(t, t.TempDir(), nil, nil)
	for _, spec := range []domain.UploadSpec{{Name: "../bad"}, {Name: "x", Size: -1}, {Name: "x", SHA256: "bad"}, {Name: "x", MimeType: "text/plain\r\nX: y"}} {
		if _, err := e.Begin(ctx, spec); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("invalid spec %+v: %v", spec, err)
		}
	}
	u := begin(t, e, nil, "")
	m, err := e.Complete(ctx, u.ID)
	if err != nil || m.SHA256 != digest(nil) {
		t.Fatalf("empty complete: %+v %v", m, err)
	}
	r, _, err := e.Open(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(r)
	r.Close()
	if err != nil || len(b) != 0 {
		t.Fatalf("empty read: %q %v", b, err)
	}
	u = begin(t, e, []byte("12345678"), digest([]byte("wrong")))
	for _, tc := range []struct {
		index int
		data  string
		hash  string
		want  error
	}{{-1, "12345678", "", domain.ErrInvalid}, {1, "12345678", "", domain.ErrInvalid}, {0, "1234567", "", domain.ErrInvalid}, {0, "123456789", "", domain.ErrInvalid}, {0, "12345678", "not-a-hash", domain.ErrInvalid}, {0, "12345678", digest([]byte("bad")), domain.ErrIntegrity}} {
		if _, err := e.PutChunk(ctx, u.ID, tc.index, strings.NewReader(tc.data), tc.hash); !errors.Is(err, tc.want) {
			t.Fatalf("invalid chunk %+v: %v", tc, err)
		}
	}
	put(t, e, u, []byte("12345678"), 0)
	if _, err := e.Complete(ctx, u.ID); !errors.Is(err, domain.ErrIntegrity) {
		t.Fatalf("whole-file digest mismatch: %v", err)
	}
}

type failingStore struct {
	BlobStore
	putFailure    bool
	deleteFailure bool
}

func (s *failingStore) Put(ctx context.Context, key string, b []byte) (string, error) {
	if s.putFailure {
		return "", errors.New("temporary network error")
	}
	return s.BlobStore.Put(ctx, key, b)
}

func (s *failingStore) Delete(ctx context.Context, ref string) error {
	if s.deleteFailure {
		return errors.New("temporary network error")
	}
	return s.BlobStore.Delete(ctx, ref)
}

func TestFailureRecoveryAndDeletion(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	local, err := NewLocal(filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	backend := &failingStore{BlobStore: local, putFailure: true}
	e, _ := testEngine(t, dir, backend, nil)
	data := []byte("failure fixture")
	u := begin(t, e, data, "")
	if _, err := e.PutChunk(ctx, u.ID, 0, bytes.NewReader(data[:8]), ""); err == nil {
		t.Fatal("failed adapter put accepted")
	}
	state, err := e.Status(ctx, u.ID)
	if err != nil || len(state.ReceivedChunks) != 0 {
		t.Fatalf("failed put persisted state: %+v %v", state, err)
	}
	backend.putFailure = false
	for i := 0; i < u.TotalChunks; i++ {
		put(t, e, u, data, i)
	}
	backend.deleteFailure = true
	if err := e.Cancel(ctx, u.ID); err == nil {
		t.Fatal("failed deletion accepted")
	}
	if _, err := e.Status(ctx, u.ID); err != nil {
		t.Fatalf("failed deletion lost retry state: %v", err)
	}
	backend.deleteFailure = false
	if err := e.Cancel(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Status(ctx, u.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cancel retained upload: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "blobs"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("cancel retained ciphertext: %d %v", len(entries), err)
	}
	u = begin(t, e, nil, "")
	if _, err := e.Complete(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.DeleteFile(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
}

func TestReaderLimitCancellationAndWrongKey(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	e, _ := testEngine(t, dir, nil, nil)
	data := []byte("fixture")
	u := begin(t, e, data, "")
	put(t, e, u, data, 0)
	if _, err := e.Complete(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	var readers []ReadSeekCloser
	for range 4 {
		r, _, err := e.Open(ctx, u.ID)
		if err != nil {
			t.Fatal(err)
		}
		readers = append(readers, r)
	}
	limited, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if _, _, err := e.Open(limited, u.ID); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("reader bound ignored context: %v", err)
	}
	for _, r := range readers {
		r.Close()
		r.Close()
	}
	wrong, _ := testEngine(t, dir, nil, bytes.Repeat([]byte{99}, 32))
	r, _, err := wrong.Open(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if n, err := r.Read(make([]byte, 16)); n != 0 || !errors.Is(err, domain.ErrIntegrity) {
		t.Fatalf("wrong key released plaintext: %d %v", n, err)
	}
}

func TestLocalRejectsReferencesAndCanceledWrite(t *testing.T) {
	local, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"../secret", "/etc/passwd", strings.Repeat("a", 65)} {
		if _, err := local.Get(context.Background(), ref); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("unsafe ref %q: %v", ref, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := local.Put(ctx, domain.NewID(), []byte("ciphertext")); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled put: %v", err)
	}
}

func TestChunkIdentityAndManifestIntegrity(t *testing.T) {
	ctx := context.Background()
	e, db := testEngine(t, t.TempDir(), nil, nil)
	data := []byte("samebys!samebys!")
	u := begin(t, e, data, "")
	for i := 0; i < u.TotalChunks; i++ {
		put(t, e, u, data, i)
	}
	chunks, err := e.chunks(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE storage_chunks SET ref=? WHERE upload_id=? AND idx=0`, chunks[1].ref, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Complete(ctx, u.ID); !errors.Is(err, domain.ErrIntegrity) {
		t.Fatalf("same-plaintext chunk substitution accepted: %v", err)
	}
	if _, err := db.Exec(`UPDATE storage_chunks SET ref=? WHERE upload_id=? AND idx=0`, chunks[0].ref, u.ID); err != nil {
		t.Fatal(err)
	}
	other := begin(t, e, data, "")
	for i := 0; i < other.TotalChunks; i++ {
		put(t, e, other, data, i)
	}
	otherChunks, err := e.chunks(ctx, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE storage_chunks SET ref=? WHERE upload_id=? AND idx=0`, otherChunks[0].ref, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Complete(ctx, u.ID); !errors.Is(err, domain.ErrIntegrity) {
		t.Fatalf("cross-upload chunk substitution accepted: %v", err)
	}
	if _, err := db.Exec(`UPDATE storage_chunks SET ref=? WHERE upload_id=? AND idx=0`, chunks[0].ref, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Complete(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE storage_chunks SET size=CASE idx WHEN 0 THEN 9 ELSE 6 END WHERE upload_id=?`, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.Open(ctx, u.ID); !errors.Is(err, domain.ErrIntegrity) {
		t.Fatalf("corrupt manifest accepted: %v", err)
	}
}

func TestUploadLockHonorsCancellation(t *testing.T) {
	e, _ := testEngine(t, t.TempDir(), nil, nil)
	id := domain.NewID()
	unlock, err := e.lock(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := e.lock(ctx, id); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked upload lock ignored cancellation: %v", err)
	}
}

func TestFreshNoncesOrderingAndMissingChunks(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	e, db := testEngine(t, dir, nil, nil)
	data := []byte("samebys!samebys!")
	nonces := make(map[string]bool)
	for range 16 {
		u := begin(t, e, data, digest(data))
		// Upload order must not affect reconstruction order.
		put(t, e, u, data, 1)
		put(t, e, u, data, 0)
		chunks, err := e.chunks(ctx, u.ID)
		if err != nil {
			t.Fatal(err)
		}
		for index, c := range chunks {
			if c.index != index {
				t.Fatal("manifest is not ordered by chunk index")
			}
			sealed, err := e.backend.Get(ctx, c.ref)
			if err != nil {
				t.Fatal(err)
			}
			nonce := string(sealed[4 : 4+e.aead.NonceSize()])
			if nonces[nonce] || bytes.Contains(sealed, data[:8]) {
				t.Fatal("reused nonce or plaintext found in encrypted blob")
			}
			nonces[nonce] = true
		}
		if _, err := e.Complete(ctx, u.ID); err != nil {
			t.Fatal(err)
		}
		r, _, err := e.Open(ctx, u.ID)
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(r)
		r.Close()
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("out-of-order upload reconstructed incorrectly: %v", err)
		}
		if err := os.Remove(filepath.Join(dir, "blobs", chunks[0].ref)); err != nil {
			t.Fatal(err)
		}
		r, _, err = e.Open(ctx, u.ID)
		if err != nil {
			t.Fatal(err)
		}
		n, err := r.Read(make([]byte, 8))
		r.Close()
		if n != 0 || err == nil {
			t.Fatalf("missing blob released plaintext: %d %v", n, err)
		}
		if _, err := db.Exec(`DELETE FROM storage_chunks WHERE upload_id=? AND idx=0`, u.ID); err != nil {
			t.Fatal(err)
		}
		if _, _, err := e.Open(ctx, u.ID); !errors.Is(err, domain.ErrIntegrity) {
			t.Fatalf("missing manifest chunk accepted: %v", err)
		}
	}
}

type blockedStore struct {
	BlobStore
	started chan struct{}
	release chan struct{}
}

func (s *blockedStore) Put(ctx context.Context, key string, data []byte) (string, error) {
	s.started <- struct{}{}
	select {
	case <-s.release:
		return s.BlobStore.Put(ctx, key, data)
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func TestParallelUploadsBoundedAndCancelable(t *testing.T) {
	dir := t.TempDir()
	local, err := NewLocal(filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	backend := &blockedStore{BlobStore: local, started: make(chan struct{}, 5), release: make(chan struct{})}
	defer close(backend.release)
	e, _ := testEngine(t, dir, backend, nil)
	data := []byte("fixture!")
	var uploads []domain.Upload
	for range 5 {
		uploads = append(uploads, begin(t, e, data, digest(data)))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	errs := make(chan error, 4)
	for _, u := range uploads[:4] {
		go func(id string) {
			_, err := e.PutChunk(ctx, id, 0, bytes.NewReader(data), "")
			errs <- err
		}(u.ID)
	}
	for range 4 {
		select {
		case <-backend.started:
		case <-ctx.Done():
			t.Fatal("parallel uploads did not reach adapter")
		}
	}
	limited, cancelLimited := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancelLimited()
	if _, err := e.PutChunk(limited, uploads[4].ID, 0, bytes.NewReader(data), ""); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("fifth in-flight upload ignored context: %v", err)
	}
	select {
	case <-backend.started:
		t.Fatal("fifth upload exceeded global transfer limit")
	default:
	}
	state, err := e.Status(ctx, uploads[4].ID)
	if err != nil || len(state.ReceivedChunks) != 0 || state.Status != "uploading" {
		t.Fatalf("canceled transfer recorded completion: %+v %v", state, err)
	}
	// Each release permits one adapter request to finish without closing the gate early.
	for range 4 {
		backend.release <- struct{}{}
	}
	for range 4 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
}
