package api

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"private-drive/internal/domain"
)

func TestPhase5FutureSchemaRefusedBeforeWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "drive.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("PRAGMA user_version=99"); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if opened, err := OpenDB(path); err == nil {
		opened.Close()
		t.Fatal("future schema opened")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("future schema modified")
	}
}

func TestPhase5MigrationFailureRollsBack(t *testing.T) {
	db, err := OpenDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("CREATE TABLE copy_entries(job_id TEXT)"); err != nil {
		t.Fatal(err)
	}
	server := &Server{cfg: Config{DB: db}}
	if err = server.migrate(); err == nil {
		t.Fatal("bad preexisting schema accepted")
	}
	var version, count int
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 0 {
		t.Fatal(version, err)
	}
	if err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='owner'").Scan(&count); err != nil || count != 0 {
		t.Fatal("partial schema committed", count, err)
	}
}

func TestPhase5ReadinessConnection(t *testing.T) {
	f := newFixture(t)
	requireStatus(t, f.request("GET", "/ready", nil, nil), 200)
	f.api.cfg.Adapter = "telegram"
	f.api.cfg.StorageStatus = func() domain.StorageStatus { return domain.StorageStatus{State: "disconnected"} }
	requireStatus(t, f.request("GET", "/ready", nil, nil), 503)
	f.api.cfg.StorageStatus = func() domain.StorageStatus { return domain.StorageStatus{State: "connected"} }
	requireStatus(t, f.request("GET", "/ready", nil, nil), 200)
}

func TestPhase5InternalCopyStageCannotBeMutatedAsUserUpload(t *testing.T) {
	f := newFixture(t)
	f.login()
	upload, err := f.api.cfg.Storage.Begin(context.Background(), domain.UploadSpec{Name: "staged.txt", ParentID: "copy:" + domain.NewID() + ":0", Size: 1, MimeType: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}
	response := f.request("GET", "/api/v1/uploads", nil, nil)
	if response.Code != 200 || bytes.Contains(response.Body.Bytes(), []byte(upload.ID)) {
		t.Fatal("internal stage leaked in queue")
	}
	base := "/api/v1/uploads/" + upload.ID
	for _, request := range []struct {
		method, path string
		body         any
	}{
		{"GET", base, nil}, {"PUT", base + "/chunks/0", []byte("x")},
		{"POST", base + "/complete", map[string]any{}}, {"DELETE", base, nil},
	} {
		requireStatus(t, f.request(request.method, request.path, request.body, nil), 404)
	}
	if _, err := f.api.cfg.Storage.Status(context.Background(), upload.ID); err != nil {
		t.Fatal("user upload API changed job stage", err)
	}
}
