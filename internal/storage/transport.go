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
	"os"
	"sort"
	"strings"
	"time"

	"private-drive/internal/domain"
)

type stagedPart struct {
	chunkIndex, index int
	size              int64
	hash, ref         string
}

func (e *Engine) stagedParts(ctx context.Context, id string) ([]stagedPart, error) {
	return stagedPartsFrom(ctx, e.db, id)
}

func stagedPartsFrom(ctx context.Context, query storageQuery, id string) ([]stagedPart, error) {
	rows, err := query.QueryContext(ctx, `SELECT chunk_idx,part_idx,size,sha256,ref FROM storage_parts WHERE upload_id=? ORDER BY chunk_idx,part_idx`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var parts []stagedPart
	for rows.Next() {
		var p stagedPart
		if err := rows.Scan(&p.chunkIndex, &p.index, &p.size, &p.hash, &p.ref); err != nil {
			return nil, err
		}
		parts = append(parts, p)
	}
	return parts, rows.Err()
}

func transportGeometry(u domain.Upload, index, part int) (int64, int64, error) {
	if index < 0 || index >= u.TotalChunks || part < 0 {
		return 0, 0, domain.ErrInvalid
	}
	size := min(u.ChunkSize, u.Size-int64(index)*u.ChunkSize)
	partSize := min(TransportPartSize, u.ChunkSize)
	count := (size + partSize - 1) / partSize
	if int64(part) >= count {
		return 0, 0, domain.ErrInvalid
	}
	return min(partSize, size-int64(part)*partSize), count, nil
}

func (e *Engine) stagedStatus(ctx context.Context, query storageQuery, u *domain.Upload) error {
	parts, err := stagedPartsFrom(ctx, query, u.ID)
	if err != nil {
		return err
	}
	committed := make(map[int]bool, len(u.ReceivedChunks))
	for _, index := range u.ReceivedChunks {
		committed[index] = true
	}
	pending := make(map[int]*domain.PendingChunk)
	rows, err := query.QueryContext(ctx, `SELECT idx,state,error_code FROM storage_pending_chunks WHERE upload_id=? ORDER BY idx`, u.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		p := &domain.PendingChunk{Parts: []int{}}
		if err := rows.Scan(&p.Index, &p.State, &p.ErrorCode); err != nil {
			rows.Close()
			return err
		}
		if _, _, err := transportGeometry(*u, p.Index, 0); err != nil {
			rows.Close()
			return domain.ErrIntegrity
		}
		switch p.State {
		case "receiving", "queued", "storing", "failed":
		default:
			rows.Close()
			return domain.ErrIntegrity
		}
		switch p.ErrorCode {
		case "", "integrity_failed", "storage_unavailable", "cancellation_pending":
		default:
			rows.Close()
			return domain.ErrIntegrity
		}
		if !committed[p.Index] {
			pending[p.Index] = p
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, p := range parts {
		if committed[p.chunkIndex] {
			continue // A crash after remote commit may leave disposable local parts.
		}
		size, _, err := transportGeometry(*u, p.chunkIndex, p.index)
		if err != nil || p.size != size || !validHash(p.hash) {
			return domain.ErrIntegrity
		}
		if pending[p.chunkIndex] == nil {
			pending[p.chunkIndex] = &domain.PendingChunk{Index: p.chunkIndex, Parts: []int{}, State: "receiving"}
		}
		path, err := e.staging.path(p.ref)
		if err != nil {
			return domain.ErrIntegrity
		}
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue // Recovery archives preserve committed content, not pending parts.
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() != 4+int64(e.aead.NonceSize())+p.size+int64(e.aead.Overhead()) {
			continue
		}
		pending[p.chunkIndex].Parts = append(pending[p.chunkIndex].Parts, p.index)
		pending[p.chunkIndex].Bytes += p.size
	}
	for _, p := range pending {
		if p.State == "queued" || p.State == "storing" {
			_, count, err := transportGeometry(*u, p.Index, 0)
			if err != nil || len(p.Parts) != int(count) {
				p.State, p.ErrorCode = "failed", "integrity_failed"
			}
		}
		u.PendingChunks = append(u.PendingChunks, *p)
	}
	sort.Slice(u.PendingChunks, func(i, j int) bool { return u.PendingChunks[i].Index < u.PendingChunks[j].Index })
	if len(u.PendingChunks) > 0 {
		oldest := u.PendingChunks[0]
		u.StagedChunk = &oldest.Index
		u.StagedParts = oldest.Parts
		u.StagedBytes = oldest.Bytes
	}
	return nil
}

func transportAAD(id string, chunkIndex, partIndex int) []byte {
	return []byte(fmt.Sprintf("harbor/transport/v1/%s/%d/%d", id, chunkIndex, partIndex))
}

func (e *Engine) stagedPlaintext(ctx context.Context, id string, p stagedPart) ([]byte, error) {
	data, _, err := e.stagedPlaintextBuffer(ctx, id, p, nil)
	return data, err
}

func (e *Engine) stagedPlaintextBuffer(ctx context.Context, id string, p stagedPart, buffer []byte) ([]byte, []byte, error) {
	n := e.aead.NonceSize()
	sealed, err := e.staging.getBoundedInto(ctx, p.ref, 4+int64(n)+p.size+int64(e.aead.Overhead()), buffer)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, fmt.Errorf("%w: staged part missing", domain.ErrIntegrity)
	}
	if err != nil {
		return nil, nil, err
	}
	if int64(len(sealed)) != 4+int64(n)+p.size+int64(e.aead.Overhead()) || string(sealed[:4]) != "HP01" {
		return nil, nil, domain.ErrIntegrity
	}
	data, err := e.aead.Open(sealed[4+n:4+n], sealed[4:4+n], sealed[4+n:], transportAAD(id, p.chunkIndex, p.index))
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(data)) != p.hash {
		return nil, nil, domain.ErrIntegrity
	}
	return data, sealed[:0], nil
}

func (e *Engine) PutPart(ctx context.Context, id string, chunkIndex, partIndex int, body io.Reader, expectedHash string) (domain.Upload, error) {
	if e.staging == nil || !validHash(expectedHash) {
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
	size, _, err := transportGeometry(u, chunkIndex, partIndex)
	if err != nil {
		return u, err
	}
	if err := e.writable(ctx, id); err != nil {
		return u, err
	}
	for _, index := range u.ReceivedChunks {
		if index == chunkIndex {
			return u, domain.ErrConflict
		}
	}
	state, err := e.reservePending(ctx, id, chunkIndex)
	if err != nil {
		return u, err
	}
	if state == "queued" {
		for _, pending := range u.PendingChunks {
			if pending.Index == chunkIndex && pending.State == "failed" && pending.ErrorCode == "integrity_failed" {
				if _, err := e.db.ExecContext(ctx, `UPDATE storage_pending_chunks SET state='failed',error_code='integrity_failed' WHERE upload_id=? AND idx=?`, id, chunkIndex); err != nil {
					return u, err
				}
				state = "failed"
			}
		}
	}
	if state == "queued" || state == "storing" {
		return u, domain.ErrConflict
	}
	defer e.clearEmptyReservation(ctx, id, chunkIndex)
	select {
	case e.partSlots <- struct{}{}:
		defer func() { <-e.partSlots }()
	case <-ctx.Done():
		return u, ctx.Err()
	}
	headerSize := 4 + e.aead.NonceSize()
	sealed := make([]byte, headerSize, int64(headerSize)+size+1+bytes.MinRead+int64(e.aead.Overhead()))
	buffer := bytes.NewBuffer(sealed[headerSize:])
	if _, err := buffer.ReadFrom(io.LimitReader(body, size+1)); err != nil {
		return u, err
	}
	data := buffer.Bytes()
	if int64(len(data)) != size {
		return u, fmt.Errorf("%w: part size mismatch", domain.ErrInvalid)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	if !strings.EqualFold(digest, expectedHash) {
		return u, domain.ErrIntegrity
	}
	old := stagedPart{chunkIndex: chunkIndex, index: partIndex}
	err = e.db.QueryRowContext(ctx, `SELECT size,sha256,ref FROM storage_parts WHERE upload_id=? AND chunk_idx=? AND part_idx=?`, id, chunkIndex, partIndex).Scan(&old.size, &old.hash, &old.ref)
	if err == nil {
		if old.hash != digest || old.size != size {
			return u, domain.ErrConflict
		}
		if _, err := e.stagedPlaintext(ctx, id, old); err == nil {
			return u, nil
		} else if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, domain.ErrIntegrity) {
			return u, err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return u, err
	}
	copy(sealed, "HP01")
	nonce := sealed[4:]
	if _, err := rand.Read(nonce); err != nil {
		return u, err
	}
	sealed = e.aead.Seal(sealed, nonce, data, transportAAD(id, chunkIndex, partIndex))
	ref, err := e.staging.Put(ctx, id, sealed)
	if err != nil {
		return u, err
	}
	_, err = e.db.ExecContext(ctx, `INSERT INTO storage_parts(upload_id,chunk_idx,part_idx,size,sha256,ref) VALUES(?,?,?,?,?,?) ON CONFLICT(upload_id,chunk_idx,part_idx) DO UPDATE SET ref=excluded.ref`, id, chunkIndex, partIndex, size, digest, ref)
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err != nil {
		_ = e.staging.Delete(cleanup, ref)
		return u, err
	}
	if old.ref != "" {
		_ = e.staging.Delete(cleanup, old.ref)
	}
	return e.Status(ctx, id)
}

type transportReader struct {
	engine *Engine
	ctx    context.Context
	id     string
	parts  []stagedPart
	data   []byte
	buffer []byte
}

func (r *transportReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) == 0 {
		return 0, nil
	}
	if len(r.data) == 0 {
		if len(r.parts) == 0 {
			return 0, io.EOF
		}
		var err error
		// Private buffer is reused only after its verified plaintext is consumed.
		r.data, r.buffer, err = r.engine.stagedPlaintextBuffer(r.ctx, r.id, r.parts[0], r.buffer)
		if err != nil {
			if errors.Is(err, domain.ErrIntegrity) {
				// Retain the expected hash but stop advertising a corrupt local part.
				cleanup, cancel := context.WithTimeout(context.WithoutCancel(r.ctx), 30*time.Second)
				_ = r.engine.staging.Delete(cleanup, r.parts[0].ref)
				cancel()
			}
			return 0, err
		}
		r.parts = r.parts[1:]
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

func (e *Engine) CommitChunk(ctx context.Context, id string, index int) (domain.Upload, error) {
	if e.staging == nil {
		return domain.Upload{}, domain.ErrInvalid
	}
	workDone, err := e.workLock(ctx, id)
	if err != nil {
		return domain.Upload{}, err
	}
	defer workDone()
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
	for _, committed := range u.ReceivedChunks {
		if committed == index {
			if err := e.clearStaged(ctx, id, &index); err != nil {
				return u, err
			}
			return e.Status(ctx, id)
		}
	}
	if err := e.writable(ctx, id); err != nil {
		return u, err
	}
	ready := false
	for _, pending := range u.PendingChunks {
		if pending.Index == index {
			ready = len(pending.Parts) == int(count) && pending.State != "queued" && pending.State != "storing"
		}
	}
	if !ready {
		return u, domain.ErrConflict
	}
	parts, err := e.stagedParts(ctx, id)
	if err != nil {
		return u, err
	}
	active := parts[:0]
	for _, p := range parts {
		if p.chunkIndex == index {
			active = append(active, p)
		}
	}
	reader := &transportReader{engine: e, ctx: ctx, id: id, parts: active}
	u, err = e.putChunkLocked(ctx, id, index, reader, "")
	if err != nil {
		return u, err
	}
	if err := e.clearStaged(ctx, id, &index); err != nil {
		return u, err
	}
	return e.Status(ctx, id)
}

func (e *Engine) clearStaged(ctx context.Context, id string, index *int) error {
	if e.staging == nil {
		return nil
	}
	parts, err := e.stagedParts(ctx, id)
	if err != nil {
		return err
	}
	for _, p := range parts {
		if index != nil && p.chunkIndex != *index {
			continue
		}
		if err := e.staging.Delete(ctx, p.ref); err != nil {
			return err
		}
	}
	tx, err := e.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if index == nil {
		_, err = tx.ExecContext(ctx, `DELETE FROM storage_parts WHERE upload_id=?`, id)
		if err == nil {
			_, err = tx.ExecContext(ctx, `DELETE FROM storage_pending_chunks WHERE upload_id=?`, id)
		}
	} else {
		_, err = tx.ExecContext(ctx, `DELETE FROM storage_parts WHERE upload_id=? AND chunk_idx=?`, id, *index)
		if err == nil {
			_, err = tx.ExecContext(ctx, `DELETE FROM storage_pending_chunks WHERE upload_id=? AND idx=?`, id, *index)
		}
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}
