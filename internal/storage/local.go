package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"private-drive/internal/domain"
)

// LocalStore keeps ciphertext in an owner-only directory. Keys are opaque IDs.
type LocalStore struct{ root string }

func NewLocal(root string) (*LocalStore, error) {
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(root, 0700); err != nil {
		return nil, err
	}
	return &LocalStore{root: root}, nil
}

func (s *LocalStore) path(ref string) (string, error) {
	if len(ref) != 65 || ref[32] != '-' || !isHexID(ref[:32]) || !isHexID(ref[33:]) {
		return "", fmt.Errorf("%w: invalid blob reference", domain.ErrInvalid)
	}
	return filepath.Join(s.root, ref), nil
}

func (s *LocalStore) Put(ctx context.Context, key string, ciphertext []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !isHexID(key) || len(ciphertext) == 0 || int64(len(ciphertext)) > maxCiphertextSize {
		return "", fmt.Errorf("%w: invalid blob key", domain.ErrInvalid)
	}
	ref := key + "-" + domain.NewID()
	path, err := s.path(ref)
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp(s.root, ".pending-")
	if err != nil {
		return "", err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(ciphertext); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if err = os.Rename(tmp, path); err != nil {
		return "", err
	}
	// Persist the directory entry before SQLite can commit its reference.
	dir, err := os.Open(s.root)
	if err != nil {
		return "", err
	}
	err = dir.Sync()
	closeErr = dir.Close()
	if err != nil {
		return "", err
	}
	return ref, closeErr
}

func (s *LocalStore) Get(ctx context.Context, ref string) ([]byte, error) {
	return s.getBounded(ctx, ref, maxCiphertextSize)
}

func (s *LocalStore) getBounded(ctx context.Context, ref string, limit int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := s.path(ref)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, domain.ErrIntegrity
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buffer := bytes.NewBuffer(make([]byte, 0, info.Size()+1+bytes.MinRead))
	if _, err := buffer.ReadFrom(io.LimitReader(f, limit+1)); err != nil {
		return nil, err
	}
	if int64(buffer.Len()) > limit {
		return nil, domain.ErrIntegrity
	}
	return buffer.Bytes(), ctx.Err()
}

func (s *LocalStore) Delete(ctx context.Context, ref string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := s.path(ref)
	if err != nil {
		return err
	}
	if err = os.Remove(path); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func isHexID(id string) bool {
	return len(id) == 32 && strings.Trim(id, "0123456789abcdef") == ""
}
