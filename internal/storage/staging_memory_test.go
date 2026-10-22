package storage

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"testing"

	"private-drive/internal/domain"
)

func BenchmarkTransportReader100MiB(b *testing.B) {
	dir := b.TempDir()
	previous, db := testEngine(b, dir, nil, nil)
	e, err := New(Config{DB: db, Key: bytes.Repeat([]byte{37}, 32), Backend: previous.backend, ChunkSize: DefaultChunkSize, StagingDir: filepath.Join(dir, "parts")})
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	u, err := e.Begin(ctx, domain.UploadSpec{Name: "staging-memory.bin", Size: DefaultChunkSize})
	if err != nil {
		b.Fatal(err)
	}
	for part, offset := 0, int64(0); offset < u.Size; part, offset = part+1, offset+u.PartSize {
		hash := digest(bytes.Repeat([]byte{byte(part)}, int(u.PartSize)))
		if _, err := e.PutPart(ctx, u.ID, 0, part, bytes.NewReader(bytes.Repeat([]byte{byte(part)}, int(u.PartSize))), hash); err != nil {
			b.Fatal(err)
		}
	}
	parts, err := e.stagedParts(ctx, u.ID)
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(DefaultChunkSize)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		r := &transportReader{engine: e, ctx: ctx, id: u.ID, parts: parts}
		if n, err := io.Copy(io.Discard, r); err != nil || n != DefaultChunkSize {
			b.Fatalf("reader: %d %v", n, err)
		}
	}
}
