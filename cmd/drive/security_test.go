package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func privateFixtureDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestPrivateDataDirectoryBoundaries(t *testing.T) {
	t.Run("new directory", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "new")
		if err := preparePrivateDataDir(path); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0700 {
			t.Fatal("data directory is not private")
		}
		if err := os.WriteFile(filepath.Join(path, "drive.db"), []byte("preserve"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := preparePrivateDataDir(path); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(path, "drive.db"))
		if err != nil || string(data) != "preserve" {
			t.Fatal("validation changed existing metadata")
		}
	})
	t.Run("permissive directory", func(t *testing.T) {
		path := t.TempDir()
		if err := os.Chmod(path, 0755); err != nil {
			t.Fatal(err)
		}
		if err := preparePrivateDataDir(path); err == nil {
			t.Fatal("permissive data directory accepted")
		}
	})
	t.Run("directory symlink", func(t *testing.T) {
		root := privateFixtureDir(t)
		link := filepath.Join(root, "link")
		if err := os.Symlink(root, link); err != nil {
			t.Fatal(err)
		}
		if err := preparePrivateDataDir(link); err == nil {
			t.Fatal("data directory symlink accepted")
		}
	})
	for _, name := range []string{".lock", "drive.db", "drive.db-wal", "drive.db-shm", "storage.json", "content.key", "owner-password", "telegram.session"} {
		for _, kind := range []string{"symlink", "directory", "public"} {
			t.Run(name+"/"+kind, func(t *testing.T) {
				root := privateFixtureDir(t)
				path := filepath.Join(root, name)
				var err error
				switch kind {
				case "symlink":
					err = os.Symlink("nonexistent", path)
				case "directory":
					err = os.Mkdir(path, 0700)
				case "public":
					err = os.WriteFile(path, []byte("synthetic-private-fixture"), 0644)
					if err == nil {
						err = os.Chmod(path, 0644)
					}
				}
				if err != nil {
					t.Fatal(err)
				}
				if kind == "public" && (name == "drive.db" || name == "drive.db-wal" || name == "drive.db-shm") {
					if err := preparePrivateDataDir(root); err != nil {
						t.Fatal(err)
					}
					info, err := os.Stat(path)
					data, readErr := os.ReadFile(path)
					if err != nil || readErr != nil || info.Mode().Perm() != 0600 || string(data) != "synthetic-private-fixture" {
						t.Fatal("SQLite permission tightening changed metadata or failed to protect it")
					}
					return
				}
				if err := preparePrivateDataDir(root); err == nil || strings.Contains(err.Error(), "synthetic-private-fixture") {
					t.Fatal("unsafe file accepted or its contents disclosed")
				}
			})
		}
	}
}

func TestListenConfigurationRequiresRemoteOptIn(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:8080", "127.0.0.2:8080", "[::1]:8080", "localhost:8080"} {
		if err := validateListenConfig(addr, false, false); err != nil {
			t.Fatalf("loopback listener rejected: %s: %v", addr, err)
		}
	}
	for _, addr := range []string{"0.0.0.0:8080", ":8080", "[::]:8080", "192.0.2.12:8080"} {
		if err := validateListenConfig(addr, false, false); err == nil {
			t.Fatalf("remote HTTP accepted without opt-in: %s", addr)
		}
		if err := validateListenConfig(addr, false, true); err != nil {
			t.Fatalf("explicit trusted-network listener rejected: %s: %v", addr, err)
		}
		if err := validateListenConfig(addr, true, false); err != nil {
			t.Fatalf("secure-cookie reverse proxy configuration rejected: %s: %v", addr, err)
		}
	}
	for _, addr := range []string{"8080", "drive.example:8080", "http://127.0.0.1:8080", "127.0.0.1:", "127.0.0.1:-1", "127.0.0.1:65536"} {
		if err := validateListenConfig(addr, true, true); err == nil {
			t.Fatalf("invalid listener accepted: %s", addr)
		}
	}
}

func TestBoundDriveMissingDatabaseFailsClosed(t *testing.T) {
	dir := privateFixtureDir(t)
	for _, name := range []string{"content.key", "storage.json", "owner-password"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("preserve"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("DRIVE_ENV_FILE", "-")
	t.Setenv("DRIVE_DATA_DIR", dir)
	previous := os.Args
	os.Args = []string{"drive"}
	defer func() { os.Args = previous }()
	if err := run(); err == nil || !strings.Contains(err.Error(), "metadata missing") {
		t.Fatal("missing database accepted", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "drive.db")); !os.IsNotExist(err) {
		t.Fatal("missing database recreated")
	}
	for _, name := range []string{"content.key", "storage.json", "owner-password"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(data) != "preserve" {
			t.Fatal("existing state changed", name)
		}
	}
}

func TestIntegrityCannotInitializeMissingDrive(t *testing.T) {
	for _, command := range []string{"integrity", "integrity-content"} {
		dir := filepath.Join(privateFixtureDir(t), "absent")
		t.Setenv("DRIVE_ENV_FILE", "-")
		t.Setenv("DRIVE_DATA_DIR", dir)
		previous := os.Args
		os.Args = []string{"drive", command}
		err := run()
		os.Args = previous
		if err == nil {
			t.Fatal("empty audit accepted")
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatal("audit initialized missing state")
		}
	}
}
