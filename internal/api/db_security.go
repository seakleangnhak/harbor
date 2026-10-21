package api

import (
	"errors"
	"os"
)

func prepareDatabaseFiles(path string) error {
	for _, name := range []string{path, path + "-wal", path + "-shm"} {
		info, err := os.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("metadata and SQLite sidecars must be regular files, without symlinks")
		}
		if err = os.Chmod(name, 0600); err != nil {
			return err
		}
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return file.Close()
}
