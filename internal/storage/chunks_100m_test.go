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
	"testing"
	"time"

	"private-drive/internal/domain"
)

type largeChunkFixture struct{}

func (largeChunkFixture) ReadAt(p []byte, offset int64) (int, error) {
	for i := range p {
		p[i] = byte((offset + int64(i)) / 251)
	}
	return len(p), nil
}

func largeChunkDigest(t *testing.T, size int64) string {
	t.Helper()
	h := sha256.New()
	if _, err := io.Copy(h, io.NewSectionReader(largeChunkFixture{}, 0, size)); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func defaultChunkEngine(t *testing.T, dir string) (*Engine, *sql.DB) {
	t.Helper()
	previous, db := testEngine(t, dir, nil, nil)
	e, err := New(Config{DB: db, Key: bytes.Repeat([]byte{37}, 32), Backend: previous.backend})
	if err != nil {
		t.Fatal(err)
	}
	return e, db
}

func Test100MiBChunksResumeRestartIntegrityAndLegacyGeometry(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	legacy, db := testEngine(t, dir, nil, nil)
	legacy.chunkSize = 4 << 20
	old, err := legacy.Begin(ctx, domain.UploadSpec{Name: "legacy.bin", Size: (4 << 20) + 17})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = legacy.PutChunk(ctx, old.ID, 0, io.NewSectionReader(largeChunkFixture{}, 0, old.ChunkSize), ""); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	e, db := defaultChunkEngine(t, dir)
	old, err = e.Status(ctx, old.ID)
	if err != nil || old.ChunkSize != 4<<20 || old.ReceivedBytes != 4<<20 || old.TotalChunks != 2 {
		t.Fatalf("legacy resume geometry changed: %+v %v", old, err)
	}
	if _, err = e.PutChunk(ctx, old.ID, 1, io.NewSectionReader(largeChunkFixture{}, old.ChunkSize, 17), ""); err != nil {
		t.Fatal(err)
	}
	manifest, err := e.Complete(ctx, old.ID)
	if err != nil || manifest.SHA256 != largeChunkDigest(t, old.Size) {
		t.Fatalf("legacy checksum after upgrade: %+v %v", manifest, err)
	}
	file, err := e.Begin(ctx, domain.UploadSpec{Name: "large.bin", Size: (100 << 20) + 17, SHA256: largeChunkDigest(t, (100<<20)+17)})
	if err != nil || file.ChunkSize != 100<<20 || file.TotalChunks != 2 {
		t.Fatalf("new chunk geometry: %+v %v", file, err)
	}
	if _, err = e.PutChunk(ctx, file.ID, 0, io.NewSectionReader(largeChunkFixture{}, 0, file.ChunkSize), ""); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	e, db = defaultChunkEngine(t, dir)
	file, err = e.Status(ctx, file.ID)
	if err != nil || file.ChunkSize != 100<<20 || file.ReceivedBytes != 100<<20 || len(file.ReceivedChunks) != 1 {
		t.Fatalf("large chunk resume lost state: %+v %v", file, err)
	}
	if _, err = e.Complete(ctx, file.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("incomplete 100 MiB file published: %v", err)
	}
	if _, err = e.PutChunk(ctx, file.ID, 1, io.NewSectionReader(largeChunkFixture{}, file.ChunkSize, 17), ""); err != nil {
		t.Fatal(err)
	}
	chunks, err := e.chunks(ctx, file.ID)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "blobs", chunks[0].ref)
	info, err := os.Stat(path)
	if err != nil || info.Size() != maxCiphertextSize {
		t.Fatalf("100 MiB encrypted envelope length: %v %v", info, err)
	}
	if err = os.Rename(path, path+".missing"); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Complete(ctx, file.ID); err == nil {
		t.Fatal("missing 100 MiB blob completed")
	}
	if err = os.Rename(path+".missing", path); err != nil {
		t.Fatal(err)
	}
	manifest, err = e.Complete(ctx, file.ID)
	if err != nil || manifest.SHA256 != file.SHA256 {
		t.Fatalf("100 MiB file checksum: %+v %v", manifest, err)
	}
	reader, _, err := e.Open(ctx, file.ID)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	n, err := io.Copy(h, reader)
	reader.Close()
	if err != nil || n != file.Size || fmt.Sprintf("%x", h.Sum(nil)) != file.SHA256 {
		t.Fatalf("100 MiB streamed checksum: bytes=%d error=%v", n, err)
	}
	reader, _, err = e.Open(ctx, file.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if _, err = reader.Seek(file.ChunkSize-8, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	got, want := make([]byte, 25), make([]byte, 25)
	largeChunkFixture{}.ReadAt(want, file.ChunkSize-8)
	if _, err = io.ReadFull(reader, got); err != nil || !bytes.Equal(got, want) {
		t.Fatalf("range crossing 100 MiB boundary: %v", err)
	}
	reader.Close()
	if _, err = db.Exec(`CREATE TABLE nodes(id TEXT PRIMARY KEY,parent_id TEXT NOT NULL DEFAULT '',kind TEXT,size INTEGER NOT NULL DEFAULT 0,sha256 TEXT NOT NULL DEFAULT '')`); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{old.ID, file.ID} {
		if _, err = db.Exec(`INSERT INTO nodes(id,kind,size,sha256) SELECT id,'file',size,sha256 FROM storage_uploads WHERE id=?`, id); err != nil {
			t.Fatal(err)
		}
	}
	report, err := CheckIntegrity(ctx, db)
	if err != nil || len(report.Issues) != 0 {
		t.Fatalf("mixed legacy/new manifests fail audit: %+v %v", report, err)
	}
}

func TestLargeChunkSharedSlotsAllowNestedCopyAndCancellation(t *testing.T) {
	e, _ := defaultChunkEngine(t, t.TempDir())
	data := []byte("copy fixture")
	u := begin(t, e, data, digest(data))
	put(t, e, u, data, 0)
	if _, err := e.Complete(context.Background(), u.ID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	reader, _, err := e.Open(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	copyUpload := begin(t, e, data, digest(data))
	if _, err = e.PutChunk(ctx, copyUpload.ID, 0, reader, digest(data)); err != nil {
		reader.Close()
		t.Fatalf("nested copy exhausted its own read/write slots: %v", err)
	}
	second, _, err := e.Open(ctx, u.ID)
	if err != nil {
		reader.Close()
		t.Fatal(err)
	}
	waitCtx, stopWaiting := context.WithTimeout(ctx, 20*time.Millisecond)
	defer stopWaiting()
	if _, err = e.PutChunk(waitCtx, copyUpload.ID, 0, bytes.NewReader(data), ""); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("third 100 MiB slot was admitted or ignored cancellation: %v", err)
	}
	reader.Close()
	second.Close()
	if _, err = e.Complete(ctx, copyUpload.ID); err != nil {
		t.Fatalf("canceled waiter leaked slot: %v", err)
	}
}

func TestLocalCiphertextBoundRejectsOversizedBlob(t *testing.T) {
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ref := domain.NewID() + "-" + domain.NewID()
	path, err := store.path(ref)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Truncate(maxCiphertextSize + 1); err != nil {
		t.Fatal(err)
	}
	file.Close()
	if _, err = store.Get(context.Background(), ref); !errors.Is(err, domain.ErrIntegrity) {
		t.Fatalf("oversized encrypted blob accepted: %v", err)
	}
}
