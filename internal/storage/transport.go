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
	rows, err := e.db.QueryContext(ctx, `SELECT chunk_idx,part_idx,size,sha256,ref FROM storage_parts WHERE upload_id=? ORDER BY chunk_idx,part_idx`, id)
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

func (e *Engine) stagedStatus(ctx context.Context, u *domain.Upload) error {
	parts, err := e.stagedParts(ctx, u.ID)
	if err != nil {
		return err
	}
	committed := make(map[int]bool, len(u.ReceivedChunks))
	for _, index := range u.ReceivedChunks {
		committed[index] = true
	}
	for _, p := range parts {
		if committed[p.chunkIndex] {
			continue // A crash after remote commit may leave disposable local parts.
		}
		size, _, err := transportGeometry(*u, p.chunkIndex, p.index)
		if err != nil || p.size != size || !validHash(p.hash) || (u.StagedChunk != nil && *u.StagedChunk != p.chunkIndex) {
			return domain.ErrIntegrity
		}
		index := p.chunkIndex
		u.StagedChunk = &index
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
		u.StagedParts = append(u.StagedParts, p.index)
		u.StagedBytes += p.size
	}
	return nil
}

func transportAAD(id string, chunkIndex, partIndex int) []byte {
	return []byte(fmt.Sprintf("harbor/transport/v1/%s/%d/%d", id, chunkIndex, partIndex))
}

func (e *Engine) stagedPlaintext(ctx context.Context, id string, p stagedPart) ([]byte, error) {
	n := e.aead.NonceSize()
	sealed, err := e.staging.getBounded(ctx, p.ref, 4+int64(n)+p.size+int64(e.aead.Overhead()))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: staged part missing", domain.ErrIntegrity)
	}
	if err != nil {
		return nil, err
	}
	if int64(len(sealed)) != 4+int64(n)+p.size+int64(e.aead.Overhead()) || string(sealed[:4]) != "HP01" {
		return nil, domain.ErrIntegrity
	}
	data, err := e.aead.Open(sealed[4+n:4+n], sealed[4:4+n], sealed[4+n:], transportAAD(id, p.chunkIndex, p.index))
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(data)) != p.hash {
		return nil, domain.ErrIntegrity
	}
	return data, nil
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
	if u.Status != "uploading" || (u.StagedChunk != nil && *u.StagedChunk != chunkIndex) {
		return u, domain.ErrConflict
	}
	for _, index := range u.ReceivedChunks {
		if index == chunkIndex {
			return u, domain.ErrConflict
		}
	}
	if err := e.acquire(ctx); err != nil {
		return u, err
	}
	defer func() { <-e.slots }()
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
		r.data, err = r.engine.stagedPlaintext(r.ctx, r.id, r.parts[0])
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
	if u.StagedChunk == nil || *u.StagedChunk != index || len(u.StagedParts) != int(count) {
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
	if index == nil {
		_, err = e.db.ExecContext(ctx, `DELETE FROM storage_parts WHERE upload_id=?`, id)
	} else {
		_, err = e.db.ExecContext(ctx, `DELETE FROM storage_parts WHERE upload_id=? AND chunk_idx=?`, id, *index)
	}
	return err
}
