package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"syscall"

	"private-drive/internal/storage"
)

// Maintenance deliberately bypasses initialization, key/session loading and
// engine construction. Even a diagnostics-only engine would clear its cache.
func runStorageHygiene(ctx context.Context, dataDir, command string, args []string, out io.Writer) error {
	confirmation := ""
	switch command {
	case "storage-diagnostics":
		if len(args) != 0 {
			return errors.New("usage: drive storage-diagnostics")
		}
	case "storage-cleanup":
		if len(args) != 2 || args[0] != "--confirm" || len(args[1]) != 64 {
			return errors.New("usage: drive storage-cleanup --confirm PLAN_SHA256")
		}
		confirmation = args[1]
	default:
		return errors.New("unknown storage maintenance command")
	}
	info, err := os.Lstat(dataDir)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.New("maintenance requires an existing private data directory")
	}
	root, err := os.OpenRoot(dataDir)
	if err != nil {
		return errors.New("cannot open maintenance data directory")
	}
	defer root.Close()
	info, err = root.Lstat(".lock")
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return errors.New("maintenance requires an existing protected lock file")
	}
	lock, err := root.OpenFile(".lock", os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return errors.New("cannot open existing maintenance lock")
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("stop Harbor before storage maintenance; data directory already in use")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	for _, name := range []string{"drive.db", "drive.db-wal", "drive.db-shm", "storage.json"} {
		info, err := root.Lstat(name)
		optional := name == "drive.db-wal" || name == "drive.db-shm"
		if optional && errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return errors.New("maintenance requires existing protected metadata and binding files")
		}
		if stat, ok := info.Sys().(*syscall.Stat_t); !ok || stat.Nlink != 1 {
			return errors.New("maintenance refuses metadata with multiple hard links")
		}
	}
	path, err := filepath.Abs(filepath.Join(dataDir, "drive.db"))
	if err != nil {
		return errors.New("cannot resolve maintenance database")
	}
	uri := url.URL{Scheme: "file", Path: path}
	query := uri.Query()
	query.Set("mode", "ro")
	if command == "storage-cleanup" {
		query.Set("mode", "rw")
	}
	query.Add("_pragma", "foreign_keys(1)")
	uri.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return errors.New("cannot open existing maintenance database")
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var report storage.HygieneReport
	if command == "storage-cleanup" {
		report, err = storage.ApplyHygiene(ctx, db, dataDir, confirmation)
	} else {
		report, err = storage.InspectHygiene(ctx, db, dataDir)
	}
	if encodeErr := json.NewEncoder(out).Encode(report); encodeErr != nil {
		return errors.New("cannot write storage maintenance report")
	}
	if err != nil {
		return errors.New("storage maintenance did not complete; inspect report before retrying")
	}
	return nil
}
