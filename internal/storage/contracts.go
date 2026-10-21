package storage

import (
	"context"
	"database/sql"
	"io"

	"private-drive/internal/domain"
)

const DefaultChunkSize int64 = 4 << 20

// BlobStore receives encrypted bytes only. References must survive restarts.
type BlobStore interface {
	Put(ctx context.Context, key string, ciphertext []byte) (string, error)
	Get(ctx context.Context, ref string) ([]byte, error)
	Delete(ctx context.Context, ref string) error
}

type ReadSeekCloser interface {
	io.Reader
	io.Seeker
	io.Closer
}

type Config struct {
	DB        *sql.DB
	Key       []byte
	Backend   BlobStore
	ChunkSize int64
}

// Engine implementation supplies these operations; kept as a compile-time port
// for the HTTP boundary and test fixtures.
type Service interface {
	Begin(context.Context, domain.UploadSpec) (domain.Upload, error)
	List(context.Context) ([]domain.Upload, error)
	Status(context.Context, string) (domain.Upload, error)
	PutChunk(context.Context, string, int, io.Reader, string) (domain.Upload, error)
	Complete(context.Context, string) (domain.Manifest, error)
	Open(context.Context, string) (ReadSeekCloser, domain.Manifest, error)
	Cancel(context.Context, string) error
	DeleteFile(context.Context, string) error
}
