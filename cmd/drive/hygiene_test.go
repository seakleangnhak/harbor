package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"private-drive/internal/storage"
)

func hygieneCLIFixture(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(privateFixtureDir(t), "private drive")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".lock", "storage.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("generated fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "drive.db"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`PRAGMA journal_mode=WAL;
 CREATE TABLE nodes(id TEXT PRIMARY KEY,parent_id TEXT,kind TEXT,size INTEGER,sha256 TEXT);
 CREATE TABLE storage_uploads(id TEXT PRIMARY KEY,parent_id TEXT,size INTEGER,sha256 TEXT,chunk_size INTEGER,status TEXT,canceling INTEGER);
 CREATE TABLE storage_chunks(upload_id TEXT,idx INTEGER,size INTEGER,sha256 TEXT,ref TEXT);
 CREATE TABLE storage_parts(upload_id TEXT,chunk_idx INTEGER,part_idx INTEGER,size INTEGER,sha256 TEXT,ref TEXT);
 CREATE TABLE storage_pending_chunks(upload_id TEXT,idx INTEGER,state TEXT);
 CREATE TABLE copy_jobs(id TEXT,status TEXT);
 CREATE TABLE copy_entries(job_id TEXT,idx INTEGER,target_id TEXT);`)
	closeErr := db.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("fixture DB: %v %v", err, closeErr)
	}
	if err := os.Chmod(filepath.Join(dir, "drive.db"), 0600); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(dir, "read-cache")
	if err := os.Mkdir(cache, 0700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		".harbor-read-cache-v1":                                 []byte("Harbor ciphertext read cache v1\n"),
		strings.Repeat("a", 32) + "-" + strings.Repeat("b", 32): append([]byte("HD01"), make([]byte, 28)...),
	} {
		if err := os.WriteFile(filepath.Join(cache, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestStorageHygieneCLIReadOnlyAndExplicitCleanup(t *testing.T) {
	dir := hygieneCLIFixture(t)
	before, err := os.ReadFile(filepath.Join(dir, "drive.db"))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := runStorageHygiene(context.Background(), dir, "storage-diagnostics", nil, &output); err != nil {
		t.Fatal(err)
	}
	var report storage.HygieneReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil || len(report.Candidates) != 1 || len(report.Blocked) != 0 || len(report.PlanSHA256) != 64 {
		t.Fatalf("report: %+v %v", report, err)
	}
	after, err := os.ReadFile(filepath.Join(dir, "drive.db"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("diagnostics changed database")
	}
	for _, name := range []string{"content.key", "owner-password", "telegram.session", "setup", "storage-cleanup.jsonl"} {
		if _, err := os.Lstat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("diagnostics initialized %s", name)
		}
	}
	if strings.Contains(output.String(), dir) || strings.Contains(output.String(), "generated fixture") {
		t.Fatal("diagnostics leaked private path or file content")
	}
	files, err := os.ReadDir(filepath.Join(dir, "read-cache"))
	if err != nil || len(files) != 2 {
		t.Fatal("diagnostics cleared cache")
	}
	output.Reset()
	if err := runStorageHygiene(context.Background(), dir, "storage-cleanup", []string{"--confirm", report.PlanSHA256}, &output); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(output.Bytes(), &report); err != nil || !report.Applied || report.RemovedFiles != 1 {
		t.Fatalf("explicit cleanup: %+v %v", report, err)
	}
}

func TestStorageHygieneCLIRequiresOfflineExistingProtectedData(t *testing.T) {
	for _, issue := range []string{"locked", "missing-db", "missing-binding", "missing-lock", "symlink-db", "symlink-dir", "public-db", "public-dir"} {
		t.Run(issue, func(t *testing.T) {
			dir := hygieneCLIFixture(t)
			switch issue {
			case "locked":
				lock, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_RDWR, 0)
				if err != nil {
					t.Fatal(err)
				}
				defer lock.Close()
				if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
					t.Fatal(err)
				}
			case "missing-db", "missing-binding", "missing-lock":
				name := map[string]string{"missing-db": "drive.db", "missing-binding": "storage.json", "missing-lock": ".lock"}[issue]
				if err := os.Remove(filepath.Join(dir, name)); err != nil {
					t.Fatal(err)
				}
			case "symlink-db":
				if err := os.Remove(filepath.Join(dir, "drive.db")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(dir, "storage.json"), filepath.Join(dir, "drive.db")); err != nil {
					t.Fatal(err)
				}
			case "symlink-dir":
				if err := os.Symlink(dir, dir+"-link"); err != nil {
					t.Fatal(err)
				}
				dir += "-link"
			case "public-db":
				if err := os.Chmod(filepath.Join(dir, "drive.db"), 0644); err != nil {
					t.Fatal(err)
				}
			case "public-dir":
				if err := os.Chmod(dir, 0755); err != nil {
					t.Fatal(err)
				}
			}
			var output bytes.Buffer
			err := runStorageHygiene(context.Background(), dir, "storage-diagnostics", nil, &output)
			if err == nil || output.Len() != 0 || strings.Contains(err.Error(), dir) {
				t.Fatalf("unsafe preflight accepted/leaked details: %v", err)
			}
		})
	}
}

func TestStorageHygieneCLIRejectsArgumentsWithoutInitialization(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	for _, request := range []struct {
		command string
		args    []string
	}{
		{"storage-diagnostics", []string{"--delete"}},
		{"storage-cleanup", nil},
		{"storage-cleanup", []string{"--confirm", "wrong"}},
		{"unknown", nil},
	} {
		if err := runStorageHygiene(context.Background(), dir, request.command, request.args, &bytes.Buffer{}); err == nil {
			t.Fatal("invalid request accepted")
		}
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("invalid request initialized data directory")
	}
}
