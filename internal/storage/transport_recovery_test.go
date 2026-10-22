package storage_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"private-drive/internal/api"
	"private-drive/internal/domain"
	"private-drive/internal/recovery"
	"private-drive/internal/storage"
)

func TestTransportEncryptedArchiveRestoreAndOriginalFileResume(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{63}, 32)
	owner := "synthetic-transport-owner-12345"
	password := []byte("synthetic-transport-recovery-12345")
	write := func(path string, data []byte) {
		t.Helper()
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	hash := func(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }
	write(filepath.Join(source, "content.key"), key)
	write(filepath.Join(source, "owner-password"), []byte(owner+"\n"))
	binding, err := json.Marshal(map[string]string{"adapter": "local", "key_sha256": hash(key)})
	if err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(source, "storage.json"), binding)
	db, err := api.OpenDB(filepath.Join(source, "drive.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	backend, err := storage.NewLocal(filepath.Join(source, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	chunkSize := storage.TransportPartSize + 8
	engine, err := storage.New(storage.Config{DB: db, Key: key, Backend: backend, ChunkSize: chunkSize, StagingDir: filepath.Join(source, "upload-parts")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := api.New(api.Config{DB: db, Storage: engine, OwnerPassword: owner, Adapter: "local"}); err != nil {
		t.Fatal(err)
	}
	put := func(e *storage.Engine, u domain.Upload, chunkIndex, partIndex int, data []byte) {
		t.Helper()
		if _, err := e.PutPart(ctx, u.ID, chunkIndex, partIndex, bytes.NewReader(data), hash(data)); err != nil {
			t.Fatal(err)
		}
	}
	completedBytes := []byte("already committed encrypted recovery fixture")
	completed, err := engine.Begin(ctx, domain.UploadSpec{Name: "committed.txt", Size: int64(len(completedBytes)), SHA256: hash(completedBytes)})
	if err != nil {
		t.Fatal(err)
	}
	put(engine, completed, 0, 0, completedBytes)
	if _, err := engine.CommitChunk(ctx, completed.ID, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Complete(ctx, completed.ID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO nodes(id,name,name_key,kind,size,sha256,favorite,created_at,updated_at) VALUES(?,'committed.txt','committed.txt','file',?,?,1,?,?)`, completed.ID, len(completedBytes), hash(completedBytes), now, now); err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("bounded recovery fixture"), int(2*chunkSize/24)+1)[:2*chunkSize]
	pending, err := engine.Begin(ctx, domain.UploadSpec{Name: "pending.bin", Size: int64(len(payload)), SHA256: hash(payload)})
	if err != nil {
		t.Fatal(err)
	}
	put(engine, pending, 0, 0, payload[:storage.TransportPartSize])
	put(engine, pending, 0, 1, payload[storage.TransportPartSize:chunkSize])
	if _, err := engine.CommitChunk(ctx, pending.ID, 0); err != nil {
		t.Fatal(err)
	}
	put(engine, pending, 1, 0, payload[chunkSize:chunkSize+storage.TransportPartSize])
	put(engine, pending, 1, 1, payload[chunkSize+storage.TransportPartSize:])
	queued, err := engine.QueueChunk(ctx, pending.ID, 1)
	if err != nil || len(queued.PendingChunks) != 1 || queued.PendingChunks[0].State != "queued" || queued.PendingChunks[0].Bytes != chunkSize {
		t.Fatalf("durable queue before backup: %+v %v", queued, err)
	}
	var stagedRef string
	if err := db.QueryRow(`SELECT ref FROM storage_parts WHERE upload_id=? AND chunk_idx=1 AND part_idx=0`, pending.ID).Scan(&stagedRef); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "transport.harbor")
	if err := recovery.Backup(ctx, recovery.BackupOptions{DataDir: source, Output: archive, Password: password}); err != nil {
		t.Fatalf("backup with pending transport rows: %v", err)
	}
	if err := recovery.Verify(ctx, archive, password); err != nil {
		t.Fatalf("archive integrity: %v", err)
	}
	rawArchive, err := os.ReadFile(archive)
	if err != nil || !bytes.HasPrefix(rawArchive, []byte("HBRBK002")) || bytes.Contains(rawArchive, key) || bytes.Contains(rawArchive, []byte(owner)) || bytes.Contains(rawArchive, completedBytes) {
		t.Fatalf("archive encryption/version: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	// Original paths disappear before restore; recovery must use archive alone.
	offline := filepath.Join(root, "source-offline")
	if err := os.Rename(source, offline); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "restored")
	if err := recovery.Restore(ctx, archive, destination, password); err != nil {
		t.Fatalf("fresh archive restoration: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destination, "upload-parts")); !os.IsNotExist(err) {
		t.Fatalf("uncommitted private transport files entered archive: %v", err)
	}
	restoredKey, err := os.ReadFile(filepath.Join(destination, "content.key"))
	if err != nil || !bytes.Equal(restoredKey, key) {
		t.Fatalf("content key recovery: %v", err)
	}
	configBytes, err := os.ReadFile(filepath.Join(destination, "recovery.env"))
	if err != nil {
		t.Fatal(err)
	}
	config, err := recovery.ReadConfig(configBytes)
	if err != nil || config["DRIVE_STORAGE"] != "local" || !bytes.Contains(configBytes, []byte("DRIVE_DATA_DIR="+strconv.Quote(destination)+"\n")) || !bytes.Contains(configBytes, []byte("DRIVE_KEY_FILE="+strconv.Quote(filepath.Join(destination, "content.key"))+"\n")) {
		t.Fatalf("self-contained recovery configuration: %v", err)
	}
	export, err := os.ReadFile(filepath.Join(destination, "chunk-manifests.jsonl"))
	if err != nil || len(bytes.Split(bytes.TrimSpace(export), []byte{'\n'})) != 2 || bytes.Contains(export, []byte(stagedRef)) {
		t.Fatalf("committed-only chunk manifest export: %v", err)
	}
	db, err = api.OpenDB(filepath.Join(destination, "drive.db"))
	if err != nil {
		t.Fatal(err)
	}
	backend, err = storage.NewLocal(filepath.Join(destination, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	engine, err = storage.New(storage.Config{DB: db, Key: restoredKey, Backend: backend, StagingDir: filepath.Join(destination, "upload-parts")})
	if err != nil {
		t.Fatal(err)
	}
	state, err := engine.Status(ctx, pending.ID)
	if err != nil || state.Status != "uploading" || state.ReceivedBytes != chunkSize || len(state.ReceivedChunks) != 1 || state.ReceivedChunks[0] != 0 || state.StagedBytes != 0 || len(state.StagedParts) != 0 || state.StagedChunk == nil || *state.StagedChunk != 1 || len(state.PendingChunks) != 1 || state.PendingChunks[0].State != "failed" || state.PendingChunks[0].ErrorCode != "integrity_failed" || state.PendingChunks[0].Bytes != 0 || len(state.PendingChunks[0].Parts) != 0 {
		t.Fatalf("restored transport acknowledgements: %+v %v", state, err)
	}
	var favorite int
	if err := db.QueryRow(`SELECT favorite FROM nodes WHERE id=?`, completed.ID).Scan(&favorite); err != nil || favorite != 1 {
		t.Fatalf("restored file metadata: %v", err)
	}
	verify := func(e *storage.Engine, id string, data []byte) {
		t.Helper()
		r, manifest, err := e.Open(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		h := sha256.New()
		n, err := io.Copy(h, r)
		r.Close()
		if err != nil || n != int64(len(data)) || fmt.Sprintf("%x", h.Sum(nil)) != hash(data) || manifest.SHA256 != hash(data) {
			t.Fatalf("archive-restored file checksum: %d %v", n, err)
		}
	}
	verify(engine, completed.ID, completedBytes)
	put(engine, pending, 1, 0, payload[chunkSize:chunkSize+storage.TransportPartSize])
	put(engine, pending, 1, 1, payload[chunkSize+storage.TransportPartSize:])
	if _, err := engine.CommitChunk(ctx, pending.ID, 1); err != nil {
		t.Fatalf("missing-stage original-file resend: %v", err)
	}
	if _, err := engine.Complete(ctx, pending.ID); err != nil {
		t.Fatalf("resumed file whole-hash verification: %v", err)
	}
	verify(engine, pending.ID, payload)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = api.OpenDB(filepath.Join(destination, "drive.db"))
	if err != nil {
		t.Fatal(err)
	}
	engine, err = storage.New(storage.Config{DB: db, Key: restoredKey, Backend: backend, StagingDir: filepath.Join(destination, "upload-parts")})
	if err != nil {
		t.Fatal(err)
	}
	verify(engine, pending.ID, payload)
	if _, err := os.Stat(filepath.Join(offline, "upload-parts", stagedRef)); err != nil {
		t.Fatalf("restore changed original staging: %v", err)
	}
}
