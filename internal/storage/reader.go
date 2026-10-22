package storage

import (
	"context"
	"errors"
	"io"
	"math"

	"private-drive/internal/domain"
)

type chunkReader struct {
	engine *Engine
	ctx    context.Context
	upload domain.Upload
	chunks []chunk
	pos    int64
	loaded int
	data   []byte
	closed bool
}

func (r *chunkReader) Read(p []byte) (int, error) {
	if r.closed {
		return 0, errors.New("reader closed")
	}
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) == 0 {
		return 0, nil
	}
	if r.pos >= r.upload.Size {
		return 0, io.EOF
	}
	idx := int(r.pos / r.upload.ChunkSize)
	if r.loaded != idx {
		r.data, r.loaded = nil, -1
		var data []byte
		var err error
		if r.engine.readCache != nil {
			data, err = r.engine.readCache.plaintext(r.ctx, r.engine, r.upload.ID, r.chunks[idx])
		} else {
			data, err = r.engine.plaintext(r.ctx, r.upload.ID, r.chunks[idx])
		}
		if err != nil {
			return 0, err
		}
		r.data, r.loaded = data, idx
	}
	n := copy(p, r.data[r.pos%r.upload.ChunkSize:])
	r.pos += int64(n)
	return n, nil
}

func (r *chunkReader) Seek(offset int64, whence int) (int64, error) {
	if r.closed {
		return 0, errors.New("reader closed")
	}
	var base int64
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		base = r.pos
	case io.SeekEnd:
		base = r.upload.Size
	default:
		return r.pos, domain.ErrInvalid
	}
	if (offset > 0 && base > math.MaxInt64-offset) || (offset < 0 && base < math.MinInt64-offset) || base+offset < 0 {
		return r.pos, domain.ErrInvalid
	}
	r.pos = base + offset
	return r.pos, nil
}

func (r *chunkReader) Close() error {
	if !r.closed {
		r.closed = true
		r.data = nil
		<-r.engine.slots
	}
	return nil
}
