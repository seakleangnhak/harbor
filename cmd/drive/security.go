package main

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func preparePrivateDataDir(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return errors.New("cannot create private data directory")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.New("data directory must be a real directory with mode 0700")
	}
	for _, name := range []string{".lock", "drive.db", "drive.db-wal", "drive.db-shm", "storage.json", "content.key", "owner-password", "telegram.session"} {
		file := filepath.Join(path, name)
		info, err := os.Lstat(file)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("%s must be a regular private file with mode 0600", name)
		}
		if info.Mode().Perm()&0077 != 0 {
			switch name {
			case "drive.db", "drive.db-wal", "drive.db-shm":
				if err := os.Chmod(file, 0600); err != nil {
					return errors.New("cannot protect metadata file permissions")
				}
			default:
				return fmt.Errorf("%s must be a regular private file with mode 0600", name)
			}
		}
	}
	return nil
}

func validateListenConfig(addr string, secureCookies, allowRemoteHTTP bool) error {
	host, port, err := net.SplitHostPort(addr)
	n, portErr := strconv.Atoi(port)
	if err != nil || portErr != nil || n < 0 || n > 65535 {
		return errors.New("DRIVE_ADDR requires an IP address or localhost and numeric port")
	}
	ip, ipErr := netip.ParseAddr(host)
	localhost := strings.EqualFold(host, "localhost")
	if host != "" && !localhost && ipErr != nil {
		return errors.New("DRIVE_ADDR host must be an IP address or localhost")
	}
	if !localhost && !ip.IsLoopback() && !secureCookies && !allowRemoteHTTP {
		return errors.New("non-loopback HTTP requires DRIVE_ALLOW_REMOTE_HTTP=true; use HTTPS reverse proxy and secure cookies for remote access")
	}
	return nil
}

// A bound drive must never silently become a new, empty installation.
func requireExistingMetadata(dataDir string, audit bool) error {
	_, dbErr := os.Lstat(filepath.Join(dataDir, "drive.db"))
	_, bindingErr := os.Lstat(filepath.Join(dataDir, "storage.json"))
	if audit && (dbErr != nil || bindingErr != nil) || errors.Is(dbErr, os.ErrNotExist) && !errors.Is(bindingErr, os.ErrNotExist) {
		return errors.New("existing drive metadata missing; restore original drive.db and storage.json before starting")
	}
	return nil
}
