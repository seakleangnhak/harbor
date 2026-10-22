package storage

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"private-drive/internal/domain"
)

const (
	pendingPerUpload = 2
	pendingGlobal    = 4
)

type storageQuery interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func initializePendingSchema(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(storage_uploads)`)
	if err != nil {
		return err
	}
	hasCanceling := false
	for rows.Next() {
		var cid, required, primary int
		var name, kind string
		var value any
		if err := rows.Scan(&cid, &name, &kind, &required, &value, &primary); err != nil {
			rows.Close()
			return err
		}
		hasCanceling = hasCanceling || name == "canceling"
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if !hasCanceling {
		if _, err := db.Exec(`ALTER TABLE storage_uploads ADD COLUMN canceling INTEGER NOT NULL DEFAULT 0 CHECK(canceling IN (0,1))`); err != nil {
			return err
		}
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS storage_pending_chunks (
 upload_id TEXT NOT NULL REFERENCES storage_uploads(id) ON DELETE CASCADE,
 idx INTEGER NOT NULL CHECK(idx >= 0),
 state TEXT NOT NULL CHECK(state IN ('receiving','queued','storing','failed')),
 error_code TEXT NOT NULL DEFAULT '', attempts INTEGER NOT NULL DEFAULT 0,
 created_at TEXT NOT NULL, PRIMARY KEY(upload_id,idx)
);
DELETE FROM storage_pending_chunks WHERE state='receiving' AND NOT EXISTS
 (SELECT 1 FROM storage_parts p WHERE p.upload_id=storage_pending_chunks.upload_id AND p.chunk_idx=storage_pending_chunks.idx);
INSERT OR IGNORE INTO storage_pending_chunks(upload_id,idx,state,created_at)
 SELECT p.upload_id,p.chunk_idx,'receiving',u.created_at FROM storage_parts p
 JOIN storage_uploads u ON u.id=p.upload_id
 WHERE u.status='uploading' AND NOT EXISTS
 (SELECT 1 FROM storage_chunks c WHERE c.upload_id=p.upload_id AND c.idx=p.chunk_idx)
 GROUP BY p.upload_id,p.chunk_idx;`)
	return err
}

func (e *Engine) writable(ctx context.Context, id string) error {
	var status string
	var canceling int
	err := e.db.QueryRowContext(ctx, `SELECT status,canceling FROM storage_uploads WHERE id=?`, id).Scan(&status, &canceling)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return err
	}
	if status != "uploading" || canceling != 0 {
		return domain.ErrConflict
	}
	return nil
}

func (e *Engine) pendingState(ctx context.Context, id string, index int) (string, error) {
	var state string
	err := e.db.QueryRowContext(ctx, `SELECT state FROM storage_pending_chunks WHERE upload_id=? AND idx=?`, id, index).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return state, err
}

func (e *Engine) reservePending(ctx context.Context, id string, index int) (string, error) {
	tx, err := e.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var state string
	err = tx.QueryRowContext(ctx, `SELECT state FROM storage_pending_chunks WHERE upload_id=? AND idx=?`, id, index).Scan(&state)
	if err == nil {
		return state, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	var total, own int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(upload_id=?),0) FROM storage_pending_chunks p
 WHERE NOT EXISTS(SELECT 1 FROM storage_chunks c WHERE c.upload_id=p.upload_id AND c.idx=p.idx)`, id).Scan(&total, &own)
	if err != nil {
		return "", err
	}
	if total >= pendingGlobal || own >= pendingPerUpload {
		return "", domain.ErrConflict
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO storage_pending_chunks(upload_id,idx,state,created_at) VALUES(?,?,'receiving',?)`, id, index, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return "", err
	}
	return "receiving", tx.Commit()
}

func (e *Engine) clearEmptyReservation(ctx context.Context, id string, index int) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	_, _ = e.db.ExecContext(cleanup, `DELETE FROM storage_pending_chunks WHERE upload_id=? AND idx=? AND state='receiving'
 AND NOT EXISTS(SELECT 1 FROM storage_parts WHERE upload_id=? AND chunk_idx=?)`, id, index, id, index)
}

func (e *Engine) QueueChunk(ctx context.Context, id string, index int) (domain.Upload, error) {
	if e.staging == nil {
		return domain.Upload{}, domain.ErrInvalid
	}
	unlock, err := e.lock(ctx, id)
	if err != nil {
		return domain.Upload{}, err
	}
	defer unlock()
	u, err := e.Status(ctx, id)
	if err != nil {
		return u, err
	}
	_, count, err := transportGeometry(u, index, 0)
	if err != nil {
		return u, err
	}
	for _, received := range u.ReceivedChunks {
		if received == index {
			if err := e.clearStaged(ctx, id, &index); err != nil {
				return u, err
			}
			return e.Status(ctx, id)
		}
	}
	if err := e.writable(ctx, id); err != nil {
		return u, err
	}
	for _, pending := range u.PendingChunks {
		if pending.Index != index {
			continue
		}
		if len(pending.Parts) != int(count) {
			return u, domain.ErrConflict
		}
		if pending.State == "queued" || pending.State == "storing" {
			return u, nil
		}
		_, err = e.db.ExecContext(ctx, `UPDATE storage_pending_chunks SET state='queued',error_code='',attempts=0 WHERE upload_id=? AND idx=?`, id, index)
		if err != nil {
			return u, err
		}
		select {
		case e.pending <- struct{}{}:
		default:
		}
		return e.Status(ctx, id)
	}
	return u, domain.ErrConflict
}

// RunPending owns the durable background writer. Cancel and join it before closing
// the database or adapter. Construction and integrity commands start no workers.
func (e *Engine) RunPending(ctx context.Context) error {
	if e.staging == nil {
		return domain.ErrInvalid
	}
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return domain.ErrConflict
	}
	e.running = true
	e.mu.Unlock()
	defer func() { e.mu.Lock(); e.running = false; e.mu.Unlock() }()
	_, err := e.db.ExecContext(ctx, `UPDATE storage_pending_chunks SET state=CASE WHEN attempts<3 THEN 'queued' ELSE 'failed' END,
 error_code=CASE WHEN attempts<3 THEN '' ELSE 'storage_unavailable' END WHERE state='storing'`)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		var id string
		var index int
		err := e.db.QueryRowContext(ctx, `SELECT p.upload_id,p.idx FROM storage_pending_chunks p
 JOIN storage_uploads u ON u.id=p.upload_id WHERE p.state='queued' AND u.status='uploading' AND u.canceling=0
 ORDER BY p.created_at,p.upload_id,p.idx LIMIT 1`).Scan(&id, &index)
		if errors.Is(err, sql.ErrNoRows) {
			select {
			case <-ctx.Done():
				return nil
			case <-e.pending:
				continue
			}
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if err := e.storePending(ctx, id, index); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
	}
}

func (e *Engine) storePending(ctx context.Context, id string, index int) error {
	workDone, err := e.workLock(ctx, id)
	if err != nil {
		return err
	}
	defer workDone()
	unlock, err := e.lock(ctx, id)
	if err != nil {
		return err
	}
	u, err := e.Status(ctx, id)
	if err != nil {
		unlock()
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		if errors.Is(err, domain.ErrIntegrity) {
			_, updateErr := e.db.ExecContext(ctx, `UPDATE storage_pending_chunks SET state='failed',error_code='integrity_failed' WHERE upload_id=? AND idx=?`, id, index)
			return updateErr
		}
		return err
	}
	state, err := e.pendingState(ctx, id, index)
	if err != nil {
		unlock()
		return err
	}
	if state != "queued" {
		unlock()
		return nil
	}
	var attempts int
	if err := e.db.QueryRowContext(ctx, `SELECT attempts FROM storage_pending_chunks WHERE upload_id=? AND idx=?`, id, index).Scan(&attempts); err != nil {
		unlock()
		return err
	}
	if attempts >= 3 {
		_, err := e.db.ExecContext(ctx, `UPDATE storage_pending_chunks SET state='failed',error_code='storage_unavailable' WHERE upload_id=? AND idx=?`, id, index)
		unlock()
		return err
	}
	if err := e.writable(ctx, id); err != nil {
		unlock()
		if errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		return err
	}
	for _, received := range u.ReceivedChunks {
		if received == index {
			err := e.clearStaged(ctx, id, &index)
			unlock()
			return err
		}
	}
	parts, err := e.stagedParts(ctx, id)
	if err != nil {
		unlock()
		return err
	}
	active := parts[:0]
	for _, part := range parts {
		if part.chunkIndex == index {
			active = append(active, part)
		}
	}
	_, count, geometryErr := transportGeometry(u, index, 0)
	valid := geometryErr == nil
	for _, pending := range u.PendingChunks {
		if pending.Index == index {
			valid = valid && len(pending.Parts) == int(count)
		}
	}
	if !valid || len(active) != int(count) {
		_, err := e.db.ExecContext(ctx, `UPDATE storage_pending_chunks SET state='failed',error_code='integrity_failed' WHERE upload_id=? AND idx=?`, id, index)
		unlock()
		return err
	}
	_, err = e.db.ExecContext(ctx, `UPDATE storage_pending_chunks SET state='storing',error_code='',attempts=attempts+1 WHERE upload_id=? AND idx=?`, id, index)
	if err != nil {
		unlock()
		return err
	}
	jobCtx, cancel := context.WithCancel(ctx)
	e.mu.Lock()
	e.active[id] = cancel
	e.mu.Unlock()
	defer func() {
		cancel()
		e.mu.Lock()
		delete(e.active, id)
		e.mu.Unlock()
	}()
	unlock()
	if err := e.acquire(jobCtx); err != nil {
		return e.pendingFailure(jobCtx, id, index, err)
	}
	defer func() { <-e.slots }()
	reader := &transportReader{engine: e, ctx: jobCtx, id: id, parts: active}
	sealed, digest, err := e.sealChunk(u, index, reader, "")
	if err != nil {
		return e.pendingFailure(jobCtx, id, index, err)
	}
	ref, err := e.putCiphertext(jobCtx, id, sealed)
	if err != nil {
		return e.pendingFailure(jobCtx, id, index, err)
	}
	cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer stop()
	unlock, err = e.lock(cleanup, id)
	if err != nil {
		return err
	}
	defer unlock()
	tx, err := e.db.BeginTx(cleanup, nil)
	if err != nil {
		_ = e.backend.Delete(cleanup, ref)
		return err
	}
	defer tx.Rollback()
	size := min(u.ChunkSize, u.Size-int64(index)*u.ChunkSize)
	_, err = tx.ExecContext(cleanup, `INSERT INTO storage_chunks(upload_id,idx,size,sha256,ref) VALUES(?,?,?,?,?)`, id, index, size, digest, ref)
	if err == nil {
		_, err = tx.ExecContext(cleanup, `DELETE FROM storage_pending_chunks WHERE upload_id=? AND idx=?`, id, index)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		_ = tx.Rollback()
		_ = e.backend.Delete(cleanup, ref)
		return err
	}
	return e.clearStaged(cleanup, id, &index)
}

func (e *Engine) pendingFailure(ctx context.Context, id string, index int, cause error) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	unlock, err := e.lock(cleanup, id)
	if err != nil {
		return err
	}
	defer unlock()
	state, code := "failed", "storage_unavailable"
	if errors.Is(cause, domain.ErrIntegrity) || errors.Is(cause, domain.ErrInvalid) {
		code = "integrity_failed"
	}
	if ctx.Err() != nil {
		state, code = "queued", ""
	}
	_, err = e.db.ExecContext(cleanup, `UPDATE storage_pending_chunks SET state=?,error_code=? WHERE upload_id=? AND idx=? AND state='storing'`, state, code, id, index)
	return err
}

func (e *Engine) cancelPending(ctx context.Context, id string) error {
	unlock, err := e.lock(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()
	tx, err := e.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM storage_uploads WHERE id=?`, id).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrNotFound
		}
		return err
	}
	if status == "complete" {
		return domain.ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `UPDATE storage_uploads SET canceling=1 WHERE id=?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE storage_pending_chunks SET state='failed',error_code='cancellation_pending' WHERE upload_id=?`, id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	e.mu.Lock()
	cancel := e.active[id]
	e.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

func (e *Engine) sealChunk(u domain.Upload, index int, body io.Reader, expectedHash string) ([]byte, string, error) {
	size := min(u.ChunkSize, u.Size-int64(index)*u.ChunkSize)
	headerSize := 4 + e.aead.NonceSize()
	sealed := make([]byte, headerSize, int64(headerSize)+size+1+bytes.MinRead+int64(e.aead.Overhead()))
	buffer := bytes.NewBuffer(sealed[headerSize:])
	if _, err := buffer.ReadFrom(io.LimitReader(body, size+1)); err != nil {
		return nil, "", err
	}
	data := buffer.Bytes()
	if int64(len(data)) != size {
		return nil, "", fmt.Errorf("%w: chunk size mismatch", domain.ErrInvalid)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	if expectedHash != "" && !strings.EqualFold(digest, expectedHash) {
		return nil, "", domain.ErrIntegrity
	}
	copy(sealed, "HD01")
	nonce := sealed[4:]
	if _, err := rand.Read(nonce); err != nil {
		return nil, "", err
	}
	return e.aead.Seal(sealed, nonce, data, chunkAAD(u.ID, index)), digest, nil
}

func (e *Engine) putCiphertext(ctx context.Context, id string, sealed []byte) (string, error) {
	if e.staging != nil {
		select {
		case e.writers <- struct{}{}:
			defer func() { <-e.writers }()
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return e.backend.Put(ctx, id, sealed)
}
