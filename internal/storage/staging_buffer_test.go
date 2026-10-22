package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"private-drive/internal/domain"
)

func TestTransportReadersKeepIndependentPartData(t *testing.T) {
	ctx := context.Background()
	e, _ := transportEngine(t, t.TempDir(), nil, 8<<20)
	first, second := bytes.Repeat([]byte{19}, 4<<20), bytes.Repeat([]byte{73}, 4<<20)
	u, err := e.Begin(ctx, domain.UploadSpec{Name: "private-staging-readers.bin", Size: 8 << 20})
	if err != nil {
		t.Fatal(err)
	}
	for index, data := range [][]byte{first, second} {
		if _, err := e.PutPart(ctx, u.ID, 0, index, bytes.NewReader(data), digest(data)); err != nil {
			t.Fatal(err)
		}
	}
	parts, err := e.stagedParts(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	a := &transportReader{engine: e, ctx: ctx, id: u.ID, parts: parts}
	b := &transportReader{engine: e, ctx: ctx, id: u.ID, parts: parts}
	// One reader advances while the other still owns an unread verified part.
	var prefix [1]byte
	if _, err := io.ReadFull(b, prefix[:]); err != nil || prefix[0] != 19 {
		t.Fatal("first reader prefix", err)
	}
	saved := make([]byte, len(first))
	if _, err := io.ReadFull(a, saved); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(second))
	if _, err := io.ReadFull(a, got); err != nil || !bytes.Equal(got, second) {
		t.Fatal("next part changed", err)
	}
	if !bytes.Equal(saved, first) {
		t.Fatal("advancing reader mutated caller data")
	}
	if _, err := io.ReadFull(b, saved[:len(first)-1]); err != nil || !bytes.Equal(saved[:len(first)-1], first[1:]) {
		t.Fatal("another reader changed unread part", err)
	}
	// Reused memory must still authenticate the next encrypted part.
	path := filepath.Join(e.staging.root, parts[1].ref)
	sealed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sealed[len(sealed)-1] ^= 1
	if err := os.WriteFile(path, sealed, 0600); err != nil {
		t.Fatal(err)
	}
	if n, err := b.Read(got); n != 0 || !errors.Is(err, domain.ErrIntegrity) {
		t.Fatalf("corrupt next part returned plaintext: %d %v", n, err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("corrupt part stayed advertised: %v", err)
	}
}
