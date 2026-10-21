package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"private-drive/internal/domain"
	"private-drive/internal/storage"
)

const testPassword = "local test owner password"

type fixture struct {
	t      *testing.T
	dir    string
	db     *sql.DB
	api    *Server
	cookie *http.Cookie
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, dir: t.TempDir()}
	f.start(testPassword)
	t.Cleanup(func() { f.db.Close() })
	return f
}

func (f *fixture) start(password string) {
	f.t.Helper()
	var err error
	f.db, err = OpenDB(filepath.Join(f.dir, "drive.sqlite3"))
	if err != nil {
		f.t.Fatal(err)
	}
	backend, err := storage.NewLocal(filepath.Join(f.dir, "blobs"))
	if err != nil {
		f.t.Fatal(err)
	}
	engine, err := storage.New(storage.Config{DB: f.db, Key: bytes.Repeat([]byte{7}, 32), Backend: backend, ChunkSize: 8})
	if err != nil {
		f.t.Fatal(err)
	}
	f.api, err = New(Config{DB: f.db, Storage: engine, OwnerPassword: password, Adapter: "local"})
	if err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) request(method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	f.t.Helper()
	var reader io.Reader
	if raw, ok := body.([]byte); ok {
		reader = bytes.NewReader(raw)
	} else if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			f.t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, "http://drive.test"+path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if f.cookie != nil {
		req.AddCookie(f.cookie)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	w := httptest.NewRecorder()
	f.api.Handler().ServeHTTP(w, req)
	return w
}

func requireStatus(t *testing.T, w *httptest.ResponseRecorder, status int) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status %d, want %d: %s", w.Code, status, w.Body.String())
	}
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder, status int) T {
	t.Helper()
	requireStatus(t, w, status)
	var value T
	if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func (f *fixture) login() {
	f.t.Helper()
	w := f.request("POST", "/api/v1/auth/login", map[string]string{"password": testPassword}, nil)
	requireStatus(f.t, w, 200)
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		f.t.Fatal("session cookie missing")
	}
	f.cookie = cookies[0]
}

func (f *fixture) folder(name, parent string) domain.Node {
	f.t.Helper()
	return decode[domain.Node](f.t, f.request("POST", "/api/v1/folders", map[string]string{"name": name, "parent_id": parent}, nil), 201)
}

func TestOwnerSessionAndOriginBoundaries(t *testing.T) {
	f := newFixture(t)
	requireStatus(t, f.request("GET", "/health", nil, nil), 200)
	for _, path := range []string{"/nodes", "/uploads", "/backup", "/diagnostics", "/nodes/missing/content", "/auth/session"} {
		requireStatus(t, f.request("GET", "/api/v1"+path, nil, nil), 401)
	}
	for _, route := range [][2]string{{"HEAD", "/nodes/missing/content"}, {"POST", "/folders"}, {"PATCH", "/nodes/missing"}, {"DELETE", "/nodes/missing"}, {"POST", "/nodes/missing/restore"}, {"DELETE", "/nodes/missing/permanent"}, {"POST", "/nodes/bulk"}, {"POST", "/uploads"}, {"PUT", "/uploads/missing/chunks/0"}, {"POST", "/uploads/missing/complete"}, {"DELETE", "/uploads/missing"}} {
		requireStatus(t, f.request(route[0], "/api/v1"+route[1], nil, nil), 401)
	}
	requireStatus(t, f.request("POST", "/api/v1/auth/login", map[string]string{"password": "incorrect"}, nil), 401)
	f.login()
	if !f.cookie.HttpOnly || f.cookie.SameSite != http.SameSiteStrictMode || len(f.cookie.Value) != 64 {
		t.Fatalf("unsafe cookie: %#v", f.cookie)
	}
	var stored string
	if err := f.db.QueryRow("SELECT token_hash FROM sessions").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == f.cookie.Value || stored != tokenHash(f.cookie.Value) {
		t.Fatal("raw session persisted")
	}
	requireStatus(t, f.request("GET", "/api/v1/auth/session", nil, nil), 200)
	for _, headers := range []map[string]string{{"Origin": "https://hostile.test"}, {"Sec-Fetch-Site": "cross-site"}, {"Origin": "http://drive.test", "Sec-Fetch-Site": "same-site"}} {
		requireStatus(t, f.request("POST", "/api/v1/folders", map[string]string{"name": "blocked"}, headers), 403)
	}
	requireStatus(t, f.request("POST", "/api/v1/folders", map[string]string{"name": "allowed"}, map[string]string{"Origin": "http://drive.test", "Sec-Fetch-Site": "same-origin"}), 201)
	requireStatus(t, f.request("POST", "/api/v1/folders", []byte(strings.Repeat(" ", 65<<10)), nil), 413)
	requireStatus(t, f.request("POST", "/api/v1/auth/logout", map[string]any{}, nil), 204)
	requireStatus(t, f.request("GET", "/api/v1/nodes", nil, nil), 401)
}

func TestStorageConnectionDiagnostics(t *testing.T) {
	f := newFixture(t)
	called := 0
	checked := time.Date(2026, 10, 21, 1, 0, 0, 0, time.UTC)
	status := domain.StorageStatus{State: "connected", LastCheckedAt: &checked, RetryCount: 2}
	f.api.cfg.StorageStatus = func() domain.StorageStatus {
		called++
		return status
	}
	requireStatus(t, f.request("GET", "/api/v1/diagnostics", nil, nil), 401)
	if called != 0 {
		t.Fatal("unauthenticated diagnostics queried storage")
	}
	f.login()
	f.api.cfg.Adapter = "telegram"
	f.api.cfg.TelegramConfigured = true
	for _, state := range []string{"connected", "disconnected", "connecting", "retrying", "local", "unknown", "synthetic-private-session-secret"} {
		status.State = state
		w := f.request("GET", "/api/v1/diagnostics", nil, nil)
		got := decode[struct {
			TelegramConfigured bool                 `json:"telegram_configured"`
			StorageConnection  domain.StorageStatus `json:"storage_connection"`
		}](t, w, 200)
		want := state
		if state == "synthetic-private-session-secret" {
			want = "unknown"
		}
		if got.StorageConnection.State != want || got.StorageConnection.RetryCount != 2 || got.StorageConnection.LastCheckedAt == nil || !got.StorageConnection.LastCheckedAt.Equal(checked) || !got.TelegramConfigured {
			t.Fatalf("storage diagnostics mismatch: %+v", got)
		}
		if strings.Contains(w.Body.String(), "synthetic-private-session-secret") {
			t.Fatal("raw storage state leaked")
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(w.Body.Bytes(), &fields); err != nil {
			t.Fatal(err)
		}
		var connection map[string]json.RawMessage
		if err := json.Unmarshal(fields["storage_connection"], &connection); err != nil {
			t.Fatal(err)
		}
		if len(connection) != 3 {
			t.Fatalf("unexpected storage diagnostics fields: %v", connection)
		}
	}
	f.api.cfg.StorageStatus = nil
	for adapter, want := range map[string]string{"local": "local", "telegram": "unknown"} {
		f.api.cfg.Adapter = adapter
		got := decode[struct {
			StorageConnection domain.StorageStatus `json:"storage_connection"`
		}](t, f.request("GET", "/api/v1/diagnostics", nil, nil), 200)
		if got.StorageConnection.State != want {
			t.Fatalf("adapter %q reported %q, want %q", adapter, got.StorageConnection.State, want)
		}
	}
}

func TestInternalErrorRedactsHTTPAndLogs(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	w := httptest.NewRecorder()
	respondError(w, errors.New("provider request failed: synthetic-private-session-secret"))
	requireStatus(t, w, 500)
	if strings.Contains(w.Body.String()+logs.String(), "synthetic-private-session-secret") {
		t.Fatal("backend error leaked into HTTP or logs")
	}
	if !strings.Contains(logs.String(), "code=internal") || !strings.Contains(w.Body.String(), `"code":"internal"`) {
		t.Fatal("safe error classification missing")
	}
}

func TestHierarchyTrashRecoveryAndBulk(t *testing.T) {
	f := newFixture(t)
	f.login()
	project := f.folder("Projects", "")
	child := f.folder("Notes", project.ID)
	oldTrash := f.folder("Old", project.ID)
	requireStatus(t, f.request("POST", "/api/v1/folders", map[string]string{"name": "pRoJeCtS"}, nil), 409)
	requireStatus(t, f.request("POST", "/api/v1/folders", map[string]string{"name": "../escape"}, nil), 400)
	requireStatus(t, f.request("PATCH", "/api/v1/nodes/"+project.ID, map[string]string{"parent_id": child.ID}, nil), 409)
	patched := decode[domain.Node](t, f.request("PATCH", "/api/v1/nodes/"+child.ID, map[string]any{"name": "Research", "favorite": true}, nil), 200)
	if patched.Name != "Research" || !patched.Favorite {
		t.Fatal(patched)
	}
	listing := decode[struct {
		Items       []domain.Node `json:"items"`
		Breadcrumbs []crumb       `json:"breadcrumbs"`
	}](t, f.request("GET", "/api/v1/nodes?parent_id="+child.ID, nil, nil), 200)
	if len(listing.Breadcrumbs) != 3 || listing.Breadcrumbs[2].Name != "Research" {
		t.Fatal(listing.Breadcrumbs)
	}
	requireStatus(t, f.request("DELETE", "/api/v1/nodes/"+oldTrash.ID, nil, nil), 204)
	requireStatus(t, f.request("DELETE", "/api/v1/nodes/"+project.ID, nil, nil), 204)
	trashed := decode[domain.Node](t, f.request("GET", "/api/v1/nodes/"+child.ID, nil, nil), 200)
	if trashed.TrashedAt == nil {
		t.Fatal("subtree was not trashed")
	}
	replacement := f.folder("Projects", "")
	requireStatus(t, f.request("POST", "/api/v1/nodes/"+project.ID+"/restore", map[string]any{}, nil), 409)
	requireStatus(t, f.request("DELETE", "/api/v1/nodes/"+replacement.ID, nil, nil), 204)
	restored := decode[domain.Node](t, f.request("POST", "/api/v1/nodes/"+project.ID+"/restore", map[string]any{}, nil), 200)
	if restored.TrashedAt != nil {
		t.Fatal("folder not restored")
	}
	old := decode[domain.Node](t, f.request("GET", "/api/v1/nodes/"+oldTrash.ID, nil, nil), 200)
	if old.TrashedAt == nil {
		t.Fatal("previously trashed child was incorrectly restored")
	}
	bulk := decode[struct {
		Completed []string `json:"completed"`
		Errors    []struct {
			ID string `json:"id"`
		} `json:"errors"`
	}](t, f.request("POST", "/api/v1/nodes/bulk", map[string]any{"ids": []string{child.ID, "missing"}, "action": "unfavorite"}, nil), 200)
	if len(bulk.Completed) != 1 || len(bulk.Errors) != 1 {
		t.Fatal(bulk)
	}
	requireStatus(t, f.request("PATCH", "/api/v1/nodes/"+child.ID, map[string]string{"parent_id": ""}, nil), 200)
	moved := decode[domain.Node](t, f.request("GET", "/api/v1/nodes/"+child.ID, nil, nil), 200)
	if moved.ParentID != "" {
		t.Fatal("move failed")
	}
	f.folder("100%_match", "")
	search := decode[struct {
		Items []domain.Node `json:"items"`
	}](t, f.request("GET", "/api/v1/nodes?q=%25_", nil, nil), 200)
	if len(search.Items) != 1 || search.Items[0].Name != "100%_match" {
		t.Fatal(search)
	}
}

func TestUploadResumeRestartRangeAndBackup(t *testing.T) {
	f := newFixture(t)
	f.login()
	folder := f.folder("Files", "")
	payload := []byte("encrypted file roundtrip across restart")
	sum := sha256.Sum256(payload)
	expected := hex.EncodeToString(sum[:])
	u := decode[domain.Upload](t, f.request("POST", "/api/v1/uploads", domain.UploadSpec{Name: "sample.txt", ParentID: folder.ID, Size: int64(len(payload)), MimeType: "text/plain", SHA256: expected}, nil), 201)
	chunkPath := "/api/v1/uploads/" + u.ID + "/chunks/"
	requireStatus(t, f.request("PUT", chunkPath+"0", payload[:8], map[string]string{"Content-Type": "application/octet-stream"}), 200)
	requireStatus(t, f.request("PUT", chunkPath+"0", payload[:8], nil), 200)
	requireStatus(t, f.request("PUT", chunkPath+"0", []byte("replaced"), nil), 409)
	requireStatus(t, f.request("POST", "/api/v1/uploads/"+u.ID+"/complete", map[string]any{}, nil), 409)
	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	f.start("")
	status := decode[domain.Upload](t, f.request("GET", "/api/v1/uploads/"+u.ID, nil, nil), 200)
	if status.ReceivedBytes != 8 || len(status.ReceivedChunks) != 1 {
		t.Fatal(status)
	}
	for i := 1; i < u.TotalChunks; i++ {
		end := (i + 1) * 8
		if end > len(payload) {
			end = len(payload)
		}
		requireStatus(t, f.request("PUT", chunkPath+strconv.Itoa(i), payload[i*8:end], nil), 200)
	}
	n := decode[domain.Node](t, f.request("POST", "/api/v1/uploads/"+u.ID+"/complete", map[string]any{}, nil), 200)
	if n.ID != u.ID || n.SHA256 != expected || n.Size != int64(len(payload)) {
		t.Fatal(n)
	}
	requireStatus(t, f.request("POST", "/api/v1/uploads/"+u.ID+"/complete", map[string]any{}, nil), 200)
	requireStatus(t, f.request("DELETE", "/api/v1/uploads/"+u.ID, nil, nil), 409)
	contentPath := "/api/v1/nodes/" + n.ID + "/content"
	download := f.request("GET", contentPath+"?download=1", nil, nil)
	requireStatus(t, download, 200)
	if !bytes.Equal(download.Body.Bytes(), payload) || download.Header().Get("X-Content-SHA256") != expected {
		t.Fatal("download checksum mismatch")
	}
	ranged := f.request("GET", contentPath, nil, map[string]string{"Range": "bytes=7-19"})
	requireStatus(t, ranged, 206)
	if !bytes.Equal(ranged.Body.Bytes(), payload[7:20]) {
		t.Fatal("range bytes mismatch")
	}
	head := f.request("HEAD", contentPath, nil, nil)
	requireStatus(t, head, 200)
	if head.Body.Len() != 0 || head.Header().Get("Content-Length") != strconv.Itoa(len(payload)) {
		t.Fatal("HEAD response wrong")
	}
	requireStatus(t, f.request("DELETE", "/api/v1/nodes/"+folder.ID, nil, nil), 204)
	requireStatus(t, f.request("GET", contentPath, nil, nil), 404)
	requireStatus(t, f.request("POST", "/api/v1/nodes/"+folder.ID+"/restore", map[string]any{}, nil), 200)
	restored := f.request("GET", contentPath, nil, nil)
	requireStatus(t, restored, 200)
	if !bytes.Equal(restored.Body.Bytes(), payload) {
		t.Fatal("restore changed bytes")
	}
	backup := f.request("GET", "/api/v1/backup", nil, nil)
	requireStatus(t, backup, http.StatusGone)
	if bytes.Contains(backup.Body.Bytes(), []byte("SQLite format")) || !bytes.Contains(backup.Body.Bytes(), []byte("encrypted_backup_required")) {
		t.Fatal("plaintext backup export must remain disabled")
	}
}

func TestUnsafeInlineAndPermanentDeletion(t *testing.T) {
	f := newFixture(t)
	f.login()
	payload := []byte("<script>alert(1)</script>")
	u := decode[domain.Upload](t, f.request("POST", "/api/v1/uploads", domain.UploadSpec{Name: "page.html", Size: int64(len(payload)), MimeType: "text/html"}, nil), 201)
	for i := 0; i < u.TotalChunks; i++ {
		end := (i + 1) * 8
		if end > len(payload) {
			end = len(payload)
		}
		requireStatus(t, f.request("PUT", "/api/v1/uploads/"+u.ID+"/chunks/"+strconv.Itoa(i), payload[i*8:end], nil), 200)
	}
	n := decode[domain.Node](t, f.request("POST", "/api/v1/uploads/"+u.ID+"/complete", map[string]any{}, nil), 200)
	w := f.request("GET", "/api/v1/nodes/"+n.ID+"/content", nil, nil)
	requireStatus(t, w, 200)
	if w.Header().Get("Content-Type") != "application/octet-stream" || !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment") || !strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Fatal("unsafe inline policy", w.Header())
	}
	requireStatus(t, f.request("DELETE", "/api/v1/nodes/"+n.ID+"/permanent", nil, nil), 409)
	requireStatus(t, f.request("DELETE", "/api/v1/nodes/"+n.ID, nil, nil), 204)
	requireStatus(t, f.request("DELETE", "/api/v1/nodes/"+n.ID+"/permanent", nil, nil), 204)
	requireStatus(t, f.request("GET", "/api/v1/uploads/"+n.ID, nil, nil), 404)
	requireStatus(t, f.request("GET", "/api/v1/nodes/"+n.ID, nil, nil), 404)
}

func TestCorruptionFailsBeforeChunkPlaintext(t *testing.T) {
	f := newFixture(t)
	f.login()
	payload := []byte("chunkonechunktwochunkend")
	u := decode[domain.Upload](t, f.request("POST", "/api/v1/uploads", domain.UploadSpec{Name: "check.txt", Size: int64(len(payload)), MimeType: "text/plain"}, nil), 201)
	for i := 0; i < u.TotalChunks; i++ {
		end := (i + 1) * 8
		if end > len(payload) {
			end = len(payload)
		}
		requireStatus(t, f.request("PUT", "/api/v1/uploads/"+u.ID+"/chunks/"+strconv.Itoa(i), payload[i*8:end], nil), 200)
	}
	requireStatus(t, f.request("POST", "/api/v1/uploads/"+u.ID+"/complete", map[string]any{}, nil), 200)
	var ref string
	if err := f.db.QueryRow("SELECT ref FROM storage_chunks WHERE upload_id=? AND idx=1", u.ID).Scan(&ref); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(f.dir, "blobs", ref)
	sealed, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	sealed[len(sealed)-1] ^= 1
	if err = os.WriteFile(filename, sealed, 0600); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/nodes/" + u.ID + "/content"
	requireStatus(t, f.request("GET", path, nil, map[string]string{"Range": "bytes=8-15"}), 409)
	server := httptest.NewServer(f.api.Handler())
	defer server.Close()
	req, err := http.NewRequest("GET", server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(f.cookie)
	response, err := server.Client().Do(req)
	if err == nil {
		defer response.Body.Close()
		data, readErr := io.ReadAll(response.Body)
		if readErr == nil {
			t.Fatalf("corruption looked complete: %d bytes status %d", len(data), response.StatusCode)
		}
		if len(data) > 8 {
			t.Fatal("corrupted chunk plaintext leaked")
		}
	}
}

func TestStaticRootRejectsEscapingSymlinks(t *testing.T) {
	f := newFixture(t)
	root := filepath.Join(f.dir, "web")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("Harbor app"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.dir, "outside.txt"), []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(f.dir, "outside.txt"), filepath.Join(root, "escape.txt")); err != nil {
		t.Fatal(err)
	}
	f.api.cfg.StaticDir = root
	app := f.request("GET", "/folders/example", nil, nil)
	requireStatus(t, app, 200)
	if app.Body.String() != "Harbor app" {
		t.Fatal(app.Body.String())
	}
	requireStatus(t, f.request("GET", "/escape.txt", nil, nil), 404)
	requireStatus(t, f.request("GET", "/api/v1/unknown", nil, nil), 404)
}

type interruptedDeletionStore struct {
	storage.BlobStore
	deletions int
}

func (s *interruptedDeletionStore) Delete(ctx context.Context, ref string) error {
	s.deletions++
	if s.deletions == 2 {
		return errors.New("fixture: storage deletion interrupted")
	}
	return s.BlobStore.Delete(ctx, ref)
}

func TestPermanentDeletionIntentSurvivesFailureAndRestart(t *testing.T) {
	f := newFixture(t)
	f.login()
	folder := f.folder("Deleting", "")
	payload := []byte("firstonefollowupthe-end")
	u := decode[domain.Upload](t, f.request("POST", "/api/v1/uploads", domain.UploadSpec{Name: "partial.txt", ParentID: folder.ID, Size: int64(len(payload)), MimeType: "text/plain"}, nil), 201)
	for i := 0; i < u.TotalChunks; i++ {
		end := (i + 1) * 8
		if end > len(payload) {
			end = len(payload)
		}
		requireStatus(t, f.request("PUT", "/api/v1/uploads/"+u.ID+"/chunks/"+strconv.Itoa(i), payload[i*8:end], nil), 200)
	}
	requireStatus(t, f.request("POST", "/api/v1/uploads/"+u.ID+"/complete", map[string]any{}, nil), 200)
	requireStatus(t, f.request("DELETE", "/api/v1/nodes/"+folder.ID, nil, nil), 204)
	local, err := storage.NewLocal(filepath.Join(f.dir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	faults := &interruptedDeletionStore{BlobStore: local}
	engine, err := storage.New(storage.Config{DB: f.db, Key: bytes.Repeat([]byte{7}, 32), Backend: faults, ChunkSize: 8})
	if err != nil {
		t.Fatal(err)
	}
	f.api.cfg.Storage = engine
	requireStatus(t, f.request("DELETE", "/api/v1/nodes/"+folder.ID+"/permanent", nil, nil), 500)
	if faults.deletions != 2 {
		t.Fatalf("fault did not follow successful deletion: %d", faults.deletions)
	}
	var pending int
	if err = f.db.QueryRow("SELECT count(*) FROM pending_deletions").Scan(&pending); err != nil || pending != 2 {
		t.Fatalf("durable subtree intents: %d %v", pending, err)
	}
	for _, id := range []string{folder.ID, u.ID} {
		blocked := f.request("POST", "/api/v1/nodes/"+id+"/restore", map[string]any{}, nil)
		requireStatus(t, blocked, 409)
		if !strings.Contains(blocked.Body.String(), errDeletionStarted.Error()) {
			t.Fatal(blocked.Body.String())
		}
	}
	if err = f.db.Close(); err != nil {
		t.Fatal(err)
	}
	f.start("")
	requireStatus(t, f.request("POST", "/api/v1/nodes/"+folder.ID+"/restore", map[string]any{}, nil), 409)
	requireStatus(t, f.request("POST", "/api/v1/nodes/"+u.ID+"/restore", map[string]any{}, nil), 409)
	requireStatus(t, f.request("DELETE", "/api/v1/nodes/"+folder.ID+"/permanent", nil, nil), 204)
	requireStatus(t, f.request("GET", "/api/v1/nodes/"+folder.ID, nil, nil), 404)
	requireStatus(t, f.request("GET", "/api/v1/nodes/"+u.ID, nil, nil), 404)
	if err = f.db.QueryRow("SELECT count(*) FROM pending_deletions").Scan(&pending); err != nil || pending != 0 {
		t.Fatalf("deletion intents not cleaned: %d %v", pending, err)
	}
}

func TestCancelCompletedUnpublishedTransfer(t *testing.T) {
	f := newFixture(t)
	f.login()
	payload := []byte("contents")
	u := decode[domain.Upload](t, f.request("POST", "/api/v1/uploads", domain.UploadSpec{Name: "conflict.txt", Size: int64(len(payload)), MimeType: "text/plain"}, nil), 201)
	requireStatus(t, f.request("PUT", "/api/v1/uploads/"+u.ID+"/chunks/0", payload, nil), 200)
	// Simulate interruption after durable completion but before node publication.
	if _, err := f.api.cfg.Storage.Complete(context.Background(), u.ID); err != nil {
		t.Fatal(err)
	}
	folder := f.folder("conflict.txt", "")
	requireStatus(t, f.request("POST", "/api/v1/uploads/"+u.ID+"/complete", map[string]any{}, nil), 409)
	requireStatus(t, f.request("DELETE", "/api/v1/uploads/"+u.ID, nil, nil), 204)
	requireStatus(t, f.request("GET", "/api/v1/uploads/"+u.ID, nil, nil), 404)
	requireStatus(t, f.request("GET", "/api/v1/nodes/"+folder.ID, nil, nil), 200)
	entries, err := os.ReadDir(filepath.Join(f.dir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("unpublished ciphertext not deleted")
	}
}

func TestLargeListingSearchFiltersAndPagination(t *testing.T) {
	f := newFixture(t)
	f.login()
	parent := f.folder("Large folder", "")
	trash := f.folder("Trash root", "")
	nested := f.folder("Nested", trash.ID)
	ctx := context.Background()
	tx, err := f.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	base := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	const fileCount = 10050
	mimeTypes := []string{"text/plain", "image/png", "video/mp4", "application/zip", "application/octet-stream"}
	for i := 0; i < fileCount; i++ {
		n := domain.Node{ID: fmt.Sprintf("%032x", i+1), ParentID: parent.ID, Name: fmt.Sprintf("item-%05d.txt", i), Kind: "file", Size: int64(i + 1), MimeType: mimeTypes[i%len(mimeTypes)], Favorite: i%100 == 0, CreatedAt: base, UpdatedAt: base.Add(time.Duration(i) * time.Second)}
		if err = insertNode(ctx, tx, n); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 30; i++ {
		n := domain.Node{ID: fmt.Sprintf("%032x", fileCount+i+1), ParentID: parent.ID, Name: fmt.Sprintf("Folder-%02d", i), Kind: "folder", CreatedAt: base, UpdatedAt: base}
		if err = insertNode(ctx, tx, n); err != nil {
			t.Fatal(err)
		}
	}
	if err = insertNode(ctx, tx, domain.Node{ID: fmt.Sprintf("%032x", fileCount+31), ParentID: nested.ID, Name: "needle-hidden.txt", Kind: "file", MimeType: "text/plain", Favorite: true, CreatedAt: base, UpdatedAt: base.Add(24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, f.request("DELETE", "/api/v1/nodes/"+trash.ID, nil, nil), 204)
	type listing struct {
		Items []domain.Node `json:"items"`
		Total int           `json:"total"`
	}
	list := func(query string) listing {
		return decode[listing](t, f.request("GET", "/api/v1/nodes?"+query, nil, nil), 200)
	}
	defaults := list("parent_id=" + parent.ID)
	if defaults.Total != 10080 || len(defaults.Items) != 10000 || defaults.Items[0].Name != "Folder-00" || defaults.Items[30].Name != "item-00000.txt" {
		t.Fatal("default page or folder-first ordering mismatch", defaults.Total, len(defaults.Items))
	}
	all := list("parent_id=" + parent.ID + "&limit=50000")
	if all.Total != 10080 || len(all.Items) != 10080 {
		t.Fatal("max page lost rows", all.Total, len(all.Items))
	}
	page := list("parent_id=" + parent.ID + "&limit=100&offset=10000")
	if page.Total != 10080 || len(page.Items) != 80 || page.Items[0].Name != "item-09970.txt" || page.Items[79].Name != "item-10049.txt" {
		t.Fatal("offset page mismatch", page.Total, len(page.Items))
	}
	empty := list("parent_id=" + parent.ID + "&limit=10&offset=20000")
	if empty.Total != 10080 || len(empty.Items) != 0 {
		t.Fatal("empty page lost total")
	}
	largest := list("parent_id=" + parent.ID + "&sort=size&direction=desc&offset=30&limit=2")
	if len(largest.Items) != 2 || largest.Items[0].Size != fileCount || largest.Items[1].Size != fileCount-1 {
		t.Fatal("size sort mismatch", largest.Items)
	}
	descending := list("parent_id=" + parent.ID + "&sort=name&direction=desc&limit=2")
	if descending.Items[0].Name != "Folder-29" || descending.Items[1].Name != "Folder-28" {
		t.Fatal("descending folder ordering mismatch")
	}
	for _, kind := range []string{"image", "video", "document", "archive", "other"} {
		filtered := list("parent_id=" + parent.ID + "&type=" + kind + "&limit=1")
		if filtered.Total != fileCount/5 || len(filtered.Items) != 1 {
			t.Fatalf("%s category overlaps or omits items: %d", kind, filtered.Total)
		}
	}
	folders := list("parent_id=" + parent.ID + "&type=folder")
	if folders.Total != 30 {
		t.Fatal("folder filter", folders.Total)
	}
	favorites := list("view=favorites")
	if favorites.Total != 101 || len(favorites.Items) != 101 {
		t.Fatal("favorites includes trash or loses items", favorites.Total)
	}
	recent := list("view=recent&limit=2")
	if recent.Total != fileCount || len(recent.Items) != 2 || recent.Items[0].Name != "item-10049.txt" || recent.Items[1].Name != "item-10048.txt" {
		t.Fatal("recent includes folders/trash or wrong order", recent.Total)
	}
	global := list("q=item-000&parent_id=missing&limit=200")
	if global.Total != 100 || len(global.Items) != 100 {
		t.Fatal("search is not global", global.Total)
	}
	if hidden := list("q=needle"); hidden.Total != 0 {
		t.Fatal("search exposed trashed descendant")
	}
	if roots := list("view=trash"); roots.Total != 1 || roots.Items[0].ID != trash.ID {
		t.Fatal("trash exposed nested descendants", roots.Total)
	}
	for _, query := range []string{"limit=0", "limit=50001", "offset=-1"} {
		requireStatus(t, f.request("GET", "/api/v1/nodes?"+query, nil, nil), 400)
	}
}

func TestFileTypeFiltersMatchDisplayPrecedence(t *testing.T) {
	f := newFixture(t)
	f.login()
	type sample struct{ name, mime, kind string }
	samples := []sample{
		{"notes.md", "application/octet-stream", "document"},
		{"archive.ZIP", "application/octet-stream", "archive"},
		{"budget.xlsx", "application/octet-stream", "document"},
		{"office.unknown", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "document"},
		{"bundle.unknown", "application/x-tar", "archive"},
		{"paper.unknown", "application/pdf", "document"},
		{"picture.zip", "image/png", "image"},
		{"audio.zip", "audio/mpeg", "audio"},
		{"video.pdf", "video/mp4", "video"},
		{"compressed.md", "application/zip", "archive"},
		{"plaintext.zip", "text/plain", "archive"},
		{"binary.bin", "application/x-executable", "other"},
	}
	want := map[string]string{}
	now := time.Now().UTC()
	for _, s := range samples {
		id := domain.NewID()
		if err := insertNode(context.Background(), f.db, domain.Node{ID: id, Name: s.name, Kind: "file", MimeType: s.mime, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		want[id] = s.kind
	}
	seen := map[string]int{}
	for _, kind := range []string{"folder", "image", "video", "audio", "archive", "document", "other"} {
		page := decode[struct {
			Items []domain.Node `json:"items"`
			Total int           `json:"total"`
		}](t, f.request("GET", "/api/v1/nodes?type="+kind, nil, nil), 200)
		if page.Total != len(page.Items) {
			t.Fatal("filter total mismatch")
		}
		for _, n := range page.Items {
			seen[n.ID]++
			if want[n.ID] != kind {
				t.Errorf("%s classified %s, want %s", n.Name, kind, want[n.ID])
			}
		}
	}
	for id, kind := range want {
		if seen[id] != 1 {
			t.Errorf("%s appeared %d times; want exclusive %s", id, seen[id], kind)
		}
	}
}

func TestDiagnosticsBuildVersion(t *testing.T) {
	f := newFixture(t)
	f.login()
	for _, version := range []string{"1.0.0", "1.0.1"} {
		if version == "1.0.1" {
			f.api.cfg.Version = version
		}
		got := decode[struct {
			Version string `json:"version"`
		}](t, f.request("GET", "/api/v1/diagnostics", nil, nil), 200)
		if got.Version != version {
			t.Fatalf("version %q, want %q", got.Version, version)
		}
	}
}
