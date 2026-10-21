package storage

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"private-drive/internal/domain"
)

type Engine struct {
	db        *sql.DB
	backend   BlobStore
	aead      cipher.AEAD
	chunkSize int64
	slots     chan struct{}
	mu        sync.Mutex
	locks     map[string]*uploadLock
}

type uploadLock struct {
	gate chan struct{}
	refs int
}

type chunk struct {
	index int
	size  int64
	hash  string
	ref   string
}

var _ Service = (*Engine)(nil)

func New(cfg Config) (*Engine, error) {
	if cfg.DB == nil || cfg.Backend == nil || len(cfg.Key) != 32 {
		return nil, fmt.Errorf("%w: database, backend and 32-byte key required", domain.ErrInvalid)
	}
	if cfg.ChunkSize == 0 {
		cfg.ChunkSize = DefaultChunkSize
	}
	if cfg.ChunkSize < 1 || cfg.ChunkSize > MaxChunkSize {
		return nil, fmt.Errorf("%w: chunk size out of bounds", domain.ErrInvalid)
	}
	block, err := aes.NewCipher(cfg.Key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	_, err = cfg.DB.Exec(`
CREATE TABLE IF NOT EXISTS storage_uploads (
 id TEXT PRIMARY KEY, name TEXT NOT NULL, parent_id TEXT NOT NULL,
 size INTEGER NOT NULL CHECK(size >= 0), mime_type TEXT NOT NULL,
 expected_sha256 TEXT NOT NULL, sha256 TEXT NOT NULL DEFAULT '',
 chunk_size INTEGER NOT NULL CHECK(chunk_size > 0),
 status TEXT NOT NULL CHECK(status IN ('uploading','complete')),
 created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS storage_chunks (
 upload_id TEXT NOT NULL REFERENCES storage_uploads(id) ON DELETE CASCADE,
 idx INTEGER NOT NULL CHECK(idx >= 0), size INTEGER NOT NULL CHECK(size > 0),
 sha256 TEXT NOT NULL, ref TEXT NOT NULL,
 PRIMARY KEY(upload_id, idx)
);`)
	if err != nil {
		return nil, err
	}
	var persistedChunkSize int64
	if err := cfg.DB.QueryRow(`SELECT COALESCE(MAX(chunk_size),0) FROM storage_uploads`).Scan(&persistedChunkSize); err != nil {
		return nil, err
	}
	// Readers keep one verified chunk until consumed/closed. Share a 256 MiB
	// active-buffer budget with uploads/finalization, including older manifests.
	largestChunk := max(cfg.ChunkSize, min(persistedChunkSize, MaxChunkSize))
	slots := min(4, int((256<<20)/largestChunk))
	return &Engine{db: cfg.DB, backend: cfg.Backend, aead: aead, chunkSize: cfg.ChunkSize, slots: make(chan struct{}, slots), locks: make(map[string]*uploadLock)}, nil
}

func (e *Engine) lock(ctx context.Context, id string) (func(), error) {
	e.mu.Lock()
	l := e.locks[id]
	if l == nil {
		l = &uploadLock{gate: make(chan struct{}, 1)}
		e.locks[id] = l
	}
	l.refs++
	e.mu.Unlock()
	releaseRef := func() {
		e.mu.Lock()
		l.refs--
		if l.refs == 0 {
			delete(e.locks, id)
		}
		e.mu.Unlock()
	}
	select {
	case l.gate <- struct{}{}:
		return func() { <-l.gate; releaseRef() }, nil
	case <-ctx.Done():
		releaseRef()
		return nil, ctx.Err()
	}
}

func (e *Engine) acquire(ctx context.Context) error {
	select {
	case e.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func validHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func (e *Engine) Begin(ctx context.Context, spec domain.UploadSpec) (domain.Upload, error) {
	if spec.Size < 0 || spec.Size/e.chunkSize > 1<<31-2 || spec.Name == "" || len(spec.Name) > 255 || spec.Name == "." || spec.Name == ".." || strings.ContainsAny(spec.Name, "/\\\x00\r\n") || len(spec.MimeType) > 255 || strings.ContainsAny(spec.MimeType, "\x00\r\n") || (spec.SHA256 != "" && !validHash(spec.SHA256)) {
		return domain.Upload{}, domain.ErrInvalid
	}
	id := domain.NewID()
	_, err := e.db.ExecContext(ctx, `INSERT INTO storage_uploads(id,name,parent_id,size,mime_type,expected_sha256,chunk_size,status,created_at) VALUES(?,?,?,?,?,?,?,'uploading',?)`, id, spec.Name, spec.ParentID, spec.Size, spec.MimeType, strings.ToLower(spec.SHA256), e.chunkSize, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return domain.Upload{}, err
	}
	return e.Status(ctx, id)
}

func (e *Engine) Status(ctx context.Context, id string) (domain.Upload, error) {
	var u domain.Upload
	var created, expected string
	err := e.db.QueryRowContext(ctx, `SELECT id,name,parent_id,size,mime_type,expected_sha256,sha256,chunk_size,status,created_at FROM storage_uploads WHERE id=?`, id).Scan(&u.ID, &u.Name, &u.ParentID, &u.Size, &u.MimeType, &expected, &u.SHA256, &u.ChunkSize, &u.Status, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return u, domain.ErrNotFound
	}
	if err != nil {
		return u, err
	}
	if u.Size < 0 || u.ChunkSize < 1 || u.ChunkSize > MaxChunkSize || u.Size/u.ChunkSize > 1<<31-2 {
		return u, domain.ErrIntegrity
	}
	u.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return u, err
	}
	if u.SHA256 == "" {
		u.SHA256 = expected
	}
	u.TotalChunks = int(u.Size / u.ChunkSize)
	if u.Size%u.ChunkSize != 0 {
		u.TotalChunks++
	}
	u.ReceivedChunks = []int{}
	rows, err := e.db.QueryContext(ctx, `SELECT idx,size FROM storage_chunks WHERE upload_id=? ORDER BY idx`, id)
	if err != nil {
		return u, err
	}
	defer rows.Close()
	for rows.Next() {
		var idx int
		var size int64
		if err := rows.Scan(&idx, &size); err != nil {
			return u, err
		}
		u.ReceivedChunks = append(u.ReceivedChunks, idx)
		u.ReceivedBytes += size
	}
	return u, rows.Err()
}

func (e *Engine) List(ctx context.Context) ([]domain.Upload, error) {
	rows, err := e.db.QueryContext(ctx, `SELECT id FROM storage_uploads ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	result := make([]domain.Upload, 0, len(ids))
	for _, id := range ids {
		u, err := e.Status(ctx, id)
		if errors.Is(err, domain.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		result = append(result, u)
	}
	return result, nil
}

func (e *Engine) chunks(ctx context.Context, id string) ([]chunk, error) {
	rows, err := e.db.QueryContext(ctx, `SELECT idx,size,sha256,ref FROM storage_chunks WHERE upload_id=? ORDER BY idx`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var chunks []chunk
	for rows.Next() {
		var c chunk
		if err := rows.Scan(&c.index, &c.size, &c.hash, &c.ref); err != nil {
			return nil, err
		}
		chunks = append(chunks, c)
	}
	return chunks, rows.Err()
}

func (e *Engine) PutChunk(ctx context.Context, id string, index int, body io.Reader, expectedHash string) (domain.Upload, error) {
	unlock, err := e.lock(ctx, id)
	if err != nil {
		return domain.Upload{}, err
	}
	defer unlock()
	u, err := e.Status(ctx, id)
	if err != nil {
		return u, err
	}
	if u.Status != "uploading" {
		return u, domain.ErrConflict
	}
	if index < 0 || index >= u.TotalChunks || (expectedHash != "" && !validHash(expectedHash)) {
		return u, domain.ErrInvalid
	}
	expectedSize := u.ChunkSize
	if index == u.TotalChunks-1 {
		expectedSize = u.Size - int64(index)*u.ChunkSize
	}
	if err = e.acquire(ctx); err != nil {
		return u, err
	}
	defer func() { <-e.slots }()
	headerSize := 4 + e.aead.NonceSize()
	sealed := make([]byte, headerSize, int64(headerSize)+expectedSize+1+bytes.MinRead+int64(e.aead.Overhead()))
	buffer := bytes.NewBuffer(sealed[headerSize:])
	_, err = buffer.ReadFrom(io.LimitReader(body, expectedSize+1))
	if err != nil {
		return u, err
	}
	data := buffer.Bytes()
	if int64(len(data)) != expectedSize {
		return u, fmt.Errorf("%w: chunk size mismatch", domain.ErrInvalid)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	if expectedHash != "" && !strings.EqualFold(digest, expectedHash) {
		return u, domain.ErrIntegrity
	}
	var oldHash string
	err = e.db.QueryRowContext(ctx, `SELECT sha256 FROM storage_chunks WHERE upload_id=? AND idx=?`, id, index).Scan(&oldHash)
	if err == nil {
		if oldHash != digest {
			return u, domain.ErrConflict
		}
		return u, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return u, err
	}
	copy(sealed, "HD01")
	nonce := sealed[4:]
	if _, err = rand.Read(nonce); err != nil {
		return u, err
	}
	sealed = e.aead.Seal(sealed, nonce, data, chunkAAD(id, index))
	ref, err := e.backend.Put(ctx, id, sealed)
	if err != nil {
		return u, err
	}
	// ponytail: a crash before the SQL commit can orphan ciphertext; add conservative
	// adapter enumeration and garbage collection when orphan storage needs reclaiming.
	_, err = e.db.ExecContext(ctx, `INSERT INTO storage_chunks(upload_id,idx,size,sha256,ref) VALUES(?,?,?,?,?)`, id, index, len(data), digest, ref)
	if err != nil {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		_ = e.backend.Delete(cleanup, ref)
		return u, err
	}
	return e.Status(ctx, id)
}

func chunkAAD(id string, index int) []byte { return []byte(fmt.Sprintf("harbor/v1/%s/%d", id, index)) }

func (e *Engine) plaintext(ctx context.Context, id string, c chunk) ([]byte, error) {
	sealed, err := e.backend.Get(ctx, c.ref)
	if err != nil {
		return nil, err
	}
	n := e.aead.NonceSize()
	if len(sealed) < 4+n+e.aead.Overhead() || string(sealed[:4]) != "HD01" || int64(len(sealed)) != 4+int64(n)+c.size+int64(e.aead.Overhead()) {
		return nil, domain.ErrIntegrity
	}
	data, err := e.aead.Open(sealed[4+n:4+n], sealed[4:4+n], sealed[4+n:], chunkAAD(id, c.index))
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(data)) != c.hash {
		return nil, domain.ErrIntegrity
	}
	return data, nil
}

func manifest(u domain.Upload) domain.Manifest {
	return domain.Manifest{ID: u.ID, Name: u.Name, ParentID: u.ParentID, Size: u.Size, MimeType: u.MimeType, SHA256: u.SHA256, CreatedAt: u.CreatedAt}
}

func (e *Engine) Complete(ctx context.Context, id string) (domain.Manifest, error) {
	unlock, err := e.lock(ctx, id)
	if err != nil {
		return domain.Manifest{}, err
	}
	defer unlock()
	u, err := e.Status(ctx, id)
	if err != nil {
		return domain.Manifest{}, err
	}
	if u.Status == "complete" {
		return manifest(u), nil
	}
	if len(u.ReceivedChunks) != u.TotalChunks || u.ReceivedBytes != u.Size {
		return domain.Manifest{}, fmt.Errorf("%w: upload incomplete", domain.ErrConflict)
	}
	chunks, err := e.chunks(ctx, id)
	if err != nil {
		return domain.Manifest{}, err
	}
	if err = e.acquire(ctx); err != nil {
		return domain.Manifest{}, err
	}
	defer func() { <-e.slots }()
	h := sha256.New()
	if !validChunks(u, chunks) {
		return domain.Manifest{}, domain.ErrIntegrity
	}
	for _, c := range chunks {
		data, err := e.plaintext(ctx, id, c)
		if err != nil {
			return domain.Manifest{}, err
		}
		_, _ = h.Write(data)
	}
	digest := hex.EncodeToString(h.Sum(nil))
	if u.SHA256 != "" && u.SHA256 != digest {
		return domain.Manifest{}, domain.ErrIntegrity
	}
	_, err = e.db.ExecContext(ctx, `UPDATE storage_uploads SET status='complete',sha256=? WHERE id=?`, digest, id)
	if err != nil {
		return domain.Manifest{}, err
	}
	u.SHA256 = digest
	return manifest(u), nil
}

func (e *Engine) Open(ctx context.Context, id string) (ReadSeekCloser, domain.Manifest, error) {
	unlock, err := e.lock(ctx, id)
	if err != nil {
		return nil, domain.Manifest{}, err
	}
	defer unlock()
	u, err := e.Status(ctx, id)
	if err != nil {
		return nil, domain.Manifest{}, err
	}
	if u.Status != "complete" {
		return nil, domain.Manifest{}, domain.ErrConflict
	}
	chunks, err := e.chunks(ctx, id)
	if err != nil {
		return nil, domain.Manifest{}, err
	}
	if u.ReceivedBytes != u.Size || !validChunks(u, chunks) {
		return nil, domain.Manifest{}, domain.ErrIntegrity
	}
	if err = e.acquire(ctx); err != nil {
		return nil, domain.Manifest{}, err
	}
	return &chunkReader{engine: e, ctx: ctx, upload: u, chunks: chunks, loaded: -1}, manifest(u), nil
}

func (e *Engine) Cancel(ctx context.Context, id string) error { return e.remove(ctx, id, false) }

func (e *Engine) DeleteFile(ctx context.Context, id string) error { return e.remove(ctx, id, true) }

func (e *Engine) remove(ctx context.Context, id string, completed bool) error {
	unlock, err := e.lock(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()
	u, err := e.Status(ctx, id)
	if err != nil {
		return err
	}
	if !completed && u.Status == "complete" {
		return domain.ErrConflict
	}
	chunks, err := e.chunks(ctx, id)
	if err != nil {
		return err
	}
	// Keep manifest until every deletion succeeds, so interruption can retry.
	for _, c := range chunks {
		if err := e.backend.Delete(ctx, c.ref); err != nil {
			return err
		}
	}
	tx, err := e.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM storage_chunks WHERE upload_id=?`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM storage_uploads WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func validChunks(u domain.Upload, chunks []chunk) bool {
	if len(chunks) != u.TotalChunks {
		return false
	}
	for i, c := range chunks {
		expected := min(u.ChunkSize, u.Size-int64(i)*u.ChunkSize)
		if c.index != i || c.size != expected || !validHash(c.hash) || c.ref == "" {
			return false
		}
	}
	return true
}
