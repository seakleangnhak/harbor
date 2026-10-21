package storage

import (
	"bytes"
	"context"
	"io"
	"testing"

	"private-drive/internal/domain"
)

type benchmarkStore struct{ data []byte }

func (s *benchmarkStore) Put(_ context.Context, _ string, data []byte) (string, error) {
	s.data = bytes.Clone(data)
	return "fixture", nil
}
func (s *benchmarkStore) Get(context.Context, string) ([]byte, error) {
	return bytes.Clone(s.data), nil
}
func (s *benchmarkStore) Delete(context.Context, string) error { s.data = nil; return nil }

func benchmarkEngine(b *testing.B) (*Engine, *benchmarkStore, []byte) {
	b.Helper()
	store := &benchmarkStore{}
	e, _ := testEngine(b, b.TempDir(), store, nil)
	e.chunkSize = 4 << 20
	return e, store, bytes.Repeat([]byte{0xa7}, int(e.chunkSize))
}

func BenchmarkEnginePutChunk4MiB(b *testing.B) {
	e, _, data := benchmarkEngine(b)
	ctx := context.Background()
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		u, err := e.Begin(ctx, domain.UploadSpec{Name: "fixture.bin", Size: int64(len(data))})
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		_, err = e.PutChunk(ctx, u.ID, 0, bytes.NewReader(data), "")
		b.StopTimer()
		if err != nil {
			b.Fatal(err)
		}
		if err := e.Cancel(ctx, u.ID); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
}

func BenchmarkEnginePlaintext4MiB(b *testing.B) {
	e, _, data := benchmarkEngine(b)
	ctx := context.Background()
	u := begin(b, e, data, "")
	put(b, e, u, data, 0)
	chunks, err := e.chunks(ctx, u.ID)
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := e.plaintext(ctx, u.ID, chunks[0]); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEngineStream4MiB(b *testing.B) { benchmarkEngineRead(b, false) }

func BenchmarkEngineServeContent4MiB(b *testing.B) { benchmarkEngineRead(b, true) }

func benchmarkEngineRead(b *testing.B, preflight bool) {
	e, _, data := benchmarkEngine(b)
	ctx := context.Background()
	u := begin(b, e, data, "")
	put(b, e, u, data, 0)
	if _, err := e.Complete(ctx, u.ID); err != nil {
		b.Fatal(err)
	}
	buf := make([]byte, 32<<10)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		r, _, err := e.Open(ctx, u.ID)
		if err != nil {
			b.Fatal(err)
		}
		if preflight {
			if _, err := r.Read(buf[:1]); err != nil {
				b.Fatal(err)
			}
			for _, whence := range []int{io.SeekStart, io.SeekEnd, io.SeekStart} {
				if _, err := r.Seek(0, whence); err != nil {
					b.Fatal(err)
				}
			}
		}
		n, err := io.CopyBuffer(io.Discard, r, buf)
		r.Close()
		if err != nil || n != int64(len(data)) {
			b.Fatalf("stream: %d %v", n, err)
		}
	}
}
