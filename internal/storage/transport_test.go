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
	"testing/iotest"
	"time"

	"private-drive/internal/domain"
)

func transportEngine(t *testing.T, dir string, backend BlobStore, chunkSize int64) (*Engine, *sql.DB) {
	t.Helper()
	previous, db := testEngine(t, dir, backend, nil)
	e, err := New(Config{DB: db, Key: bytes.Repeat([]byte{37}, 32), Backend: previous.backend, ChunkSize: chunkSize, StagingDir: filepath.Join(dir, "parts")})
	if err != nil {
		t.Fatal(err)
	}
	return e, db
}

func fixtureHash(t *testing.T, offset, size int64) string {
	t.Helper()
	h := sha256.New()
	if _, err := io.Copy(h, io.NewSectionReader(largeChunkFixture{}, offset, size)); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func stageFixture(t *testing.T, e *Engine, u domain.Upload, chunkIndex, partIndex int) domain.Upload {
	t.Helper()
	size, _, err := transportGeometry(u, chunkIndex, partIndex)
	if err != nil {
		t.Fatal(err)
	}
	offset := int64(chunkIndex)*u.ChunkSize + int64(partIndex)*min(TransportPartSize, u.ChunkSize)
	u, err = e.PutPart(context.Background(), u.ID, chunkIndex, partIndex, io.NewSectionReader(largeChunkFixture{}, offset, size), fixtureHash(t, offset, size))
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestTransportRestartEncryptedPartsAndCommit(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	e, db := transportEngine(t, dir, nil, 8<<20)
	u, err := e.Begin(ctx, domain.UploadSpec{Name: "resumable.bin", Size: (8 << 20) + 31, SHA256: fixtureHash(t, 0, (8<<20)+31)})
	if err != nil || u.PartSize != TransportPartSize {
		t.Fatalf("part geometry: %+v %v", u, err)
	}
	u = stageFixture(t, e, u, 0, 1)
	if u.ReceivedBytes != 0 || u.StagedBytes != TransportPartSize || len(u.StagedParts) != 1 || u.StagedParts[0] != 1 || u.StagedChunk == nil || *u.StagedChunk != 0 {
		t.Fatalf("staged bytes counted as remote commit: %+v", u)
	}
	if _, err := e.Complete(ctx, u.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("staged upload became complete: %v", err)
	}
	if _, err := e.PutChunk(ctx, u.ID, 0, bytes.NewReader(nil), ""); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("legacy endpoint bypassed staged chunk: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	e, _ = transportEngine(t, dir, nil, 8<<20)
	u, err = e.Status(ctx, u.ID)
	if err != nil || u.StagedBytes != TransportPartSize || u.ReceivedBytes != 0 {
		t.Fatalf("restart lost stage: %+v %v", u, err)
	}
	u = stageFixture(t, e, u, 0, 0)
	parts, err := e.stagedParts(ctx, u.ID)
	if err != nil || len(parts) != 2 {
		t.Fatalf("parts: %+v %v", parts, err)
	}
	nonces := make(map[string]bool)
	for _, p := range parts {
		sealed, err := os.ReadFile(filepath.Join(dir, "parts", p.ref))
		if err != nil || string(sealed[:4]) != "HP01" {
			t.Fatalf("unencrypted staging: %v", err)
		}
		nonce := string(sealed[4 : 4+e.aead.NonceSize()])
		if nonces[nonce] {
			t.Fatal("part nonce reused")
		}
		nonces[nonce] = true
		plain := make([]byte, 1024)
		_, _ = (largeChunkFixture{}).ReadAt(plain, int64(p.index)*TransportPartSize)
		if bytes.Contains(sealed, plain) {
			t.Fatal("plaintext found in staging")
		}
		info, err := os.Stat(filepath.Join(dir, "parts", p.ref))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("stage permissions: %v", err)
		}
	}
	if _, err := e.CommitChunk(ctx, u.ID, 1); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("committed wrong pending chunk: %v", err)
	}
	u, err = e.CommitChunk(ctx, u.ID, 0)
	if err != nil || u.ReceivedBytes != 8<<20 || u.StagedChunk != nil || u.StagedBytes != 0 {
		t.Fatalf("commit: %+v %v", u, err)
	}
	if _, err := e.CommitChunk(ctx, u.ID, 0); err != nil {
		t.Fatalf("lost commit response not resumable: %v", err)
	}
	stageFixture(t, e, u, 1, 0)
	if _, err := e.CommitChunk(ctx, u.ID, 1); err != nil {
		t.Fatal(err)
	}
	manifest, err := e.Complete(ctx, u.ID)
	if err != nil || manifest.SHA256 != fixtureHash(t, 0, u.Size) {
		t.Fatalf("complete integrity: %+v %v", manifest, err)
	}
	if _, err := e.CommitChunk(ctx, u.ID, 0); err != nil {
		t.Fatalf("completed commit retry: %v", err)
	}
	r, _, err := e.Open(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	n, err := io.Copy(h, r)
	r.Close()
	if err != nil || n != u.Size || fmt.Sprintf("%x", h.Sum(nil)) != manifest.SHA256 {
		t.Fatalf("reconstructed content: %d %v", n, err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "parts"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("completed stages retained: %d %v", len(entries), err)
	}
}

func TestTransportPartRetriesBoundsAndLegacy(t *testing.T) {
	ctx := context.Background()
	e, _ := transportEngine(t, t.TempDir(), nil, 8)
	data := []byte("fixture!")
	u := begin(t, e, data, digest(data))
	if u.PartSize != 8 {
		t.Fatalf("legacy transport part size: %d", u.PartSize)
	}
	failure := errors.New("injected reader failure")
	for _, tc := range []struct {
		name  string
		index int
		part  int
		body  io.Reader
		hash  string
		want  error
	}{
		{"negative chunk", -1, 0, bytes.NewReader(data), digest(data), domain.ErrInvalid},
		{"negative part", 0, -1, bytes.NewReader(data), digest(data), domain.ErrInvalid},
		{"extra part", 0, 1, bytes.NewReader(data), digest(data), domain.ErrInvalid},
		{"absent hash", 0, 0, bytes.NewReader(data), "", domain.ErrInvalid},
		{"invalid hash", 0, 0, bytes.NewReader(data), "bad", domain.ErrInvalid},
		{"wrong hash", 0, 0, bytes.NewReader(data), digest(nil), domain.ErrIntegrity},
		{"short body", 0, 0, bytes.NewReader(data[:7]), digest(data), domain.ErrInvalid},
		{"long body", 0, 0, strings.NewReader("fixture!!"), digest(data), domain.ErrInvalid},
		{"read error", 0, 0, io.MultiReader(bytes.NewReader(data), iotest.ErrReader(failure)), digest(data), failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := e.PutPart(ctx, u.ID, tc.index, tc.part, tc.body, tc.hash); !errors.Is(err, tc.want) {
				t.Fatalf("part validation: %v; want %v", err, tc.want)
			}
		})
	}
	oversize := bytes.NewReader(bytes.Repeat([]byte{1}, 1024))
	if _, err := e.PutPart(ctx, u.ID, 0, 0, oversize, digest(data)); !errors.Is(err, domain.ErrInvalid) || oversize.Size()-int64(oversize.Len()) != 9 {
		t.Fatalf("part body bound: %d %v", oversize.Len(), err)
	}
	for range 2 {
		if _, err := e.PutPart(ctx, u.ID, 0, 0, bytes.NewReader(data), strings.ToUpper(digest(data))); err != nil {
			t.Fatal(err)
		}
	}
	parts, err := e.stagedParts(ctx, u.ID)
	if err != nil || len(parts) != 1 {
		t.Fatalf("duplicate staging: %+v %v", parts, err)
	}
	entries, err := os.ReadDir(e.staging.root)
	if err != nil || len(entries) != 1 {
		t.Fatalf("duplicate created blobs: %d %v", len(entries), err)
	}
	if _, err := e.PutPart(ctx, u.ID, 0, 0, strings.NewReader("changed!"), digest([]byte("changed!"))); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("different duplicate accepted: %v", err)
	}
	if _, err := e.CommitChunk(ctx, u.ID, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := e.PutPart(ctx, u.ID, 0, 0, bytes.NewReader(data), digest(data)); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("committed chunk changed: %v", err)
	}
	legacy := begin(t, e, data, digest(data))
	put(t, e, legacy, data, 0)
	if _, err := e.Complete(ctx, legacy.ID); err != nil {
		t.Fatalf("existing PutChunk compatibility: %v", err)
	}
	disabled, _ := testEngine(t, t.TempDir(), nil, nil)
	old := begin(t, disabled, data, "")
	if old.PartSize != 0 {
		t.Fatal("transport enabled without staging directory")
	}
	if _, err := disabled.PutPart(ctx, old.ID, 0, 0, bytes.NewReader(data), digest(data)); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("disabled transport accepted: %v", err)
	}
}

func TestTransportMissingCorruptStagesAndRepair(t *testing.T) {
	for _, mutation := range []string{"missing", "corrupt", "truncated", "swapped"} {
		t.Run(mutation, func(t *testing.T) {
			ctx := context.Background()
			e, _ := transportEngine(t, t.TempDir(), nil, 8)
			data := []byte("fixture!")
			u := begin(t, e, data, digest(data))
			if _, err := e.PutPart(ctx, u.ID, 0, 0, bytes.NewReader(data), digest(data)); err != nil {
				t.Fatal(err)
			}
			parts, _ := e.stagedParts(ctx, u.ID)
			path := filepath.Join(e.staging.root, parts[0].ref)
			if mutation == "missing" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			} else {
				sealed, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if mutation == "truncated" {
					sealed = sealed[:len(sealed)-1]
				} else if mutation == "swapped" {
					other := begin(t, e, data, digest(data))
					if _, err := e.PutPart(ctx, other.ID, 0, 0, bytes.NewReader(data), digest(data)); err != nil {
						t.Fatal(err)
					}
					otherParts, _ := e.stagedParts(ctx, other.ID)
					sealed, err = os.ReadFile(filepath.Join(e.staging.root, otherParts[0].ref))
					if err != nil {
						t.Fatal(err)
					}
				} else {
					sealed[len(sealed)-1] ^= 0x80
				}
				if err := os.WriteFile(path, sealed, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := e.CommitChunk(ctx, u.ID, 0); err == nil {
				t.Fatal("lost/corrupt stage committed")
			}
			state, err := e.Status(ctx, u.ID)
			if err != nil || state.ReceivedBytes != 0 || state.StagedBytes != 0 || len(state.StagedParts) != 0 {
				t.Fatalf("invalid stage advertised as resumable: %+v %v", state, err)
			}
			if _, err := e.PutPart(ctx, u.ID, 0, 0, strings.NewReader("changed!"), digest([]byte("changed!"))); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("lost stage forgot original digest: %v", err)
			}
			if _, err := e.PutPart(ctx, u.ID, 0, 0, bytes.NewReader(data), digest(data)); err != nil {
				t.Fatalf("stage repair: %v", err)
			}
			if _, err := e.CommitChunk(ctx, u.ID, 0); err != nil {
				t.Fatalf("repair commit: %v", err)
			}
			if _, err := e.Complete(ctx, u.ID); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTransportRemoteFailureCancellationAndConcurrentRetries(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	local, err := NewLocal(filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	backend := &injectedStore{BlobStore: local}
	e, _ := transportEngine(t, dir, backend, 8)
	data := []byte("fixture!")
	u := begin(t, e, data, digest(data))
	var group sync.WaitGroup
	errs := make(chan error, 4)
	for range 4 {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := e.PutPart(ctx, u.ID, 0, 0, bytes.NewReader(data), digest(data))
			errs <- err
		}()
	}
	group.Wait()
	for range 4 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	backend.put = func(context.Context, string, []byte) (string, error) {
		return "", errors.New("injected remote failure")
	}
	if _, err := e.CommitChunk(ctx, u.ID, 0); err == nil {
		t.Fatal("remote failure committed")
	}
	state, err := e.Status(ctx, u.ID)
	if err != nil || state.ReceivedBytes != 0 || state.StagedBytes != 8 {
		t.Fatalf("remote failure lost retry state: %+v %v", state, err)
	}
	started := make(chan struct{})
	backend.put = func(ctx context.Context, _ string, _ []byte) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	}
	commitCtx, cancel := context.WithCancel(ctx)
	finished := make(chan error, 1)
	go func() { _, err := e.CommitChunk(commitCtx, u.ID, 0); finished <- err }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("commit did not reach adapter")
	}
	cancel()
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("commit cancellation: %v", err)
	}
	state, err = e.Status(ctx, u.ID)
	if err != nil || state.StagedBytes != 8 || state.ReceivedBytes != 0 || len(e.slots) != 0 {
		t.Fatalf("canceled commit lost state/slot: %+v %v", state, err)
	}
	backend.put = nil
	if _, err := e.CommitChunk(ctx, u.ID, 0); err != nil {
		t.Fatal(err)
	}
	pending := begin(t, e, data, digest(data))
	if _, err := e.PutPart(ctx, pending.ID, 0, 0, bytes.NewReader(data), digest(data)); err != nil {
		t.Fatal(err)
	}
	if err := e.Cancel(ctx, pending.ID); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(e.staging.root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("cancel retained stages: %d %v", len(entries), err)
	}
	if _, err := e.Status(ctx, pending.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cancel retained session: %v", err)
	}
}

func TestTransport100MiBAggregationAndMissingArchiveStages(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	e, db := transportEngine(t, dir, nil, 0)
	size := DefaultChunkSize + 17
	u, err := e.Begin(ctx, domain.UploadSpec{Name: "large.bin", Size: size, SHA256: fixtureHash(t, 0, size)})
	if err != nil || u.ChunkSize != 100<<20 || u.PartSize != 4<<20 {
		t.Fatalf("100 MiB storage geometry: %+v %v", u, err)
	}
	if _, err := e.PutPart(ctx, u.ID, 0, 25, bytes.NewReader(nil), digest(nil)); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("26th part accepted: %v", err)
	}
	for part := 0; part < 25; part++ {
		u = stageFixture(t, e, u, 0, part)
	}
	if u.ReceivedBytes != 0 || u.StagedBytes != DefaultChunkSize || len(u.StagedParts) != 25 {
		t.Fatalf("storage chunk not staged in 25 bounded parts: %+v", u)
	}
	if _, err := e.CommitChunk(ctx, u.ID, 0); err != nil {
		t.Fatal(err)
	}
	chunks, err := e.chunks(ctx, u.ID)
	if err != nil || len(chunks) != 1 || chunks[0].size != DefaultChunkSize {
		t.Fatalf("transport parts escaped into remote manifests: %+v %v", chunks, err)
	}
	stageFixture(t, e, u, 1, 0)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	// A complete recovery archive intentionally excludes uncommitted transport files.
	if err := os.RemoveAll(filepath.Join(dir, "parts")); err != nil {
		t.Fatal(err)
	}
	e, _ = transportEngine(t, dir, nil, 0)
	u, err = e.Status(ctx, u.ID)
	if err != nil || u.ReceivedBytes != DefaultChunkSize || u.StagedBytes != 0 || len(u.StagedParts) != 0 {
		t.Fatalf("restore advertised missing pending data: %+v %v", u, err)
	}
	stageFixture(t, e, u, 1, 0)
	if _, err := e.CommitChunk(ctx, u.ID, 1); err != nil {
		t.Fatal(err)
	}
	manifest, err := e.Complete(ctx, u.ID)
	if err != nil || manifest.SHA256 != fixtureHash(t, 0, size) {
		t.Fatalf("100 MiB checksum: %+v %v", manifest, err)
	}
	r, _, err := e.Open(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	n, err := io.Copy(h, r)
	r.Close()
	if err != nil || n != size || fmt.Sprintf("%x", h.Sum(nil)) != manifest.SHA256 {
		t.Fatalf("100 MiB decrypted roundtrip: %d %v", n, err)
	}
}

func TestTransportCorruptMetadataFailsClosed(t *testing.T) {
	for name, mutation := range map[string]string{
		"chunk index": `UPDATE storage_parts SET chunk_idx=1 WHERE upload_id=?`,
		"part index":  `UPDATE storage_parts SET part_idx=25 WHERE upload_id=?`,
		"part size":   `UPDATE storage_parts SET size=9 WHERE upload_id=?`,
		"hash":        `UPDATE storage_parts SET sha256='invalid' WHERE upload_id=?`,
		"reference":   `UPDATE storage_parts SET ref='../outside' WHERE upload_id=?`,
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			e, db := transportEngine(t, t.TempDir(), nil, 8)
			data := []byte("fixture!")
			u := begin(t, e, data, digest(data))
			if _, err := e.PutPart(ctx, u.ID, 0, 0, bytes.NewReader(data), digest(data)); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(mutation, u.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := e.Status(ctx, u.ID); !errors.Is(err, domain.ErrIntegrity) {
				t.Fatalf("malformed stage status: %v", err)
			}
			if _, err := e.CommitChunk(ctx, u.ID, 0); !errors.Is(err, domain.ErrIntegrity) {
				t.Fatalf("malformed stage commit: %v", err)
			}
			if _, err := e.Complete(ctx, u.ID); !errors.Is(err, domain.ErrIntegrity) {
				t.Fatalf("malformed stage complete: %v", err)
			}
			chunks, err := e.chunks(ctx, u.ID)
			if err != nil || len(chunks) != 0 {
				t.Fatalf("malformed stage stored remote data: %+v %v", chunks, err)
			}
		})
	}
}

func TestTransportOversizedLocalPartCanBeRepaired(t *testing.T) {
	ctx := context.Background()
	e, _ := transportEngine(t, t.TempDir(), nil, 8)
	data := []byte("fixture!")
	u := begin(t, e, data, digest(data))
	if _, err := e.PutPart(ctx, u.ID, 0, 0, bytes.NewReader(data), digest(data)); err != nil {
		t.Fatal(err)
	}
	parts, _ := e.stagedParts(ctx, u.ID)
	if err := os.Truncate(filepath.Join(e.staging.root, parts[0].ref), 64<<20); err != nil {
		t.Fatal(err)
	}
	if _, err := e.stagedPlaintext(ctx, u.ID, parts[0]); !errors.Is(err, domain.ErrIntegrity) {
		t.Fatalf("oversized part accepted: %v", err)
	}
	u, err := e.Status(ctx, u.ID)
	if err != nil || u.StagedBytes != 0 {
		t.Fatalf("oversized part advertised: %+v %v", u, err)
	}
	if _, err := e.PutPart(ctx, u.ID, 0, 0, bytes.NewReader(data), digest(data)); err != nil {
		t.Fatalf("oversized part repair: %v", err)
	}
	if _, err := e.CommitChunk(ctx, u.ID, 0); err != nil {
		t.Fatal(err)
	}
}

func TestTransportRestartAfterRemoteCommitBeforeStageCleanup(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	e, db := transportEngine(t, dir, nil, 8)
	data := []byte("fixture!chunktwo")
	u := begin(t, e, data, digest(data))
	if _, err := e.PutPart(ctx, u.ID, 0, 0, bytes.NewReader(data[:8]), digest(data[:8])); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER fail_stage_cleanup BEFORE DELETE ON storage_parts BEGIN SELECT RAISE(ABORT,'injected stage cleanup interruption'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.CommitChunk(ctx, u.ID, 0); err == nil {
		t.Fatal("injected cleanup interruption ignored")
	}
	u, err := e.Status(ctx, u.ID)
	if err != nil || u.ReceivedBytes != 8 || u.StagedBytes != 0 || u.StagedChunk != nil {
		t.Fatalf("durable commit lost or pending counted twice: %+v %v", u, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	e, db = transportEngine(t, dir, nil, 8)
	if _, err := db.Exec(`DROP TRIGGER fail_stage_cleanup`); err != nil {
		t.Fatal(err)
	}
	backend := &injectedStore{BlobStore: e.backend, put: func(context.Context, string, []byte) (string, error) {
		t.Fatal("retry re-uploaded durably committed chunk")
		return "", nil
	}}
	e.backend = backend
	if _, err := e.CommitChunk(ctx, u.ID, 0); err != nil {
		t.Fatalf("restart commit retry: %v", err)
	}
	parts, err := e.stagedParts(ctx, u.ID)
	if err != nil || len(parts) != 0 {
		t.Fatalf("retry retained stale rows: %+v %v", parts, err)
	}
	backend.put = nil
	if _, err := e.PutPart(ctx, u.ID, 1, 0, bytes.NewReader(data[8:]), digest(data[8:])); err != nil {
		t.Fatal(err)
	}
	if _, err := e.CommitChunk(ctx, u.ID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Complete(ctx, u.ID); err != nil {
		t.Fatalf("restart whole-file integrity: %v", err)
	}
}
