package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"private-drive/internal/domain"
	"private-drive/internal/recovery"
)

const integrationSetupToken = "synthetic-web-setup-token-32-characters"
const integrationOwnerPassword = "synthetic-web-owner-password"

// Re-exec the built test binary so startup, persisted configuration and process
// locks are exercised without building another binary or inheriting host secrets.
func TestWebSetupProcessHelper(t *testing.T) {
	if os.Getenv("HARBOR_WEB_SETUP_PROCESS_TEST") != "1" {
		return
	}
	os.Args = []string{"drive", "serve"}
	if err := run(); err != nil {
		t.Fatal("isolated server startup or shutdown failed")
	}
}

type webSetupProcess struct {
	command *exec.Cmd
	done    chan error
	stopped bool
	base    string
	client  *http.Client
}

func startWebSetupProcess(t *testing.T, data, config string) *webSetupProcess {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(data, 0700); err != nil {
		t.Fatal(err)
	}
	scratch := t.TempDir()
	command := exec.Command(executable, "-test.run=^TestWebSetupProcessHelper$", "-test.timeout=300s")
	command.Dir = scratch
	command.Env = []string{
		"HARBOR_WEB_SETUP_PROCESS_TEST=1", "DRIVE_ENV_FILE=" + config,
		"DRIVE_DATA_DIR=" + data, "DRIVE_ADDR=" + address,
		"DRIVE_SETUP_TOKEN=" + integrationSetupToken, "DRIVE_STATIC_DIR=" + scratch,
		"DRIVE_SECURE_COOKIES=false", "DRIVE_ALLOW_REMOTE_HTTP=false",
		"HOME=" + scratch, "TMPDIR=" + scratch, "GOMAXPROCS=2", "GORACE=atexit_sleep_ms=0",
	}
	command.Stdout, command.Stderr = io.Discard, io.Discard
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		command.Process.Kill()
		command.Wait()
		t.Fatal(err)
	}
	process := &webSetupProcess{command: command, done: make(chan error, 1), base: "http://" + address,
		client: &http.Client{Jar: jar, Timeout: 90 * time.Second, Transport: &http.Transport{
			// Authenticate a full 100 MiB chunk before Range output, including under -race.
			DialContext: (&net.Dialer{Timeout: time.Second}).DialContext, ResponseHeaderTimeout: 60 * time.Second,
			MaxIdleConns: 4, IdleConnTimeout: time.Second,
		}}}
	go func() { process.done <- command.Wait() }()
	t.Cleanup(func() { process.stop(t) })
	process.wait(t, "/health", http.StatusOK)
	return process
}

func (p *webSetupProcess) stop(t *testing.T) {
	t.Helper()
	if p.stopped {
		return
	}
	p.stopped = true
	p.client.CloseIdleConnections()
	select {
	case err := <-p.done:
		if err != nil {
			t.Errorf("isolated server exited unsuccessfully: %v", err)
		}
		return
	default:
	}
	if err := p.command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Errorf("cannot stop isolated server: %v", err)
	}
	select {
	case err := <-p.done:
		if err != nil {
			t.Errorf("isolated server did not shut down cleanly: %v", err)
		}
	case <-time.After(10 * time.Second):
		p.command.Process.Kill()
		<-p.done
		t.Error("isolated server shutdown timed out")
	}
}

func (p *webSetupProcess) wait(t *testing.T, path string, status int) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		response, err := p.client.Get(p.base + path)
		if err == nil {
			io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
			response.Body.Close()
			if response.StatusCode == status {
				return
			}
		}
		select {
		case <-p.done:
			p.stopped = true
			t.Fatal("isolated server exited before readiness")
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Fatalf("isolated server did not return %d for %s", status, path)
}

func (p *webSetupProcess) request(t *testing.T, method, path string, body io.Reader, token bool, status int) *http.Response {
	t.Helper()
	request, err := http.NewRequest(method, p.base+path, body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", p.base)
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	if method == http.MethodPut {
		request.Header.Set("Content-Type", "application/octet-stream")
		if section, ok := body.(*io.SectionReader); ok {
			request.ContentLength = section.Size()
		}
	}
	if token {
		request.Header.Set("X-Harbor-Setup-Token", integrationSetupToken)
	}
	response, err := p.client.Do(request)
	if err != nil {
		t.Fatalf("%s %s failed: %v", method, path, err)
	}
	if response.StatusCode != status {
		response.Body.Close()
		t.Fatalf("%s %s returned %d; want %d", method, path, response.StatusCode, status)
	}
	return response
}

func (p *webSetupProcess) json(t *testing.T, method, path string, value any, token bool, status int, output any) {
	t.Helper()
	var body io.Reader
	if value != nil {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(data)
	}
	response := p.request(t, method, path, body, token, status)
	defer response.Body.Close()
	if output != nil {
		if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(output); err != nil {
			t.Fatal(err)
		}
	} else {
		io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	}
}

type webSetupFixture struct{}

func (webSetupFixture) ReadAt(p []byte, offset int64) (int, error) {
	for i := range p {
		p[i] = byte((offset + int64(i)) / 251)
	}
	return len(p), nil
}

func (p *webSetupProcess) download(t *testing.T, id, digest string, size int64) {
	t.Helper()
	response := p.request(t, http.MethodGet, "/api/v1/nodes/"+id+"/content", nil, false, http.StatusOK)
	defer response.Body.Close()
	hash := sha256.New()
	n, err := io.Copy(hash, response.Body)
	if err != nil || n != size || fmt.Sprintf("%x", hash.Sum(nil)) != digest || response.Header.Get("X-Content-SHA256") != digest {
		t.Fatalf("streamed checksum or length mismatch: bytes=%d error=%v", n, err)
	}
}

func TestWebOnboardingProcessResumeAndEncryptedRecovery(t *testing.T) {
	data := t.TempDir()
	p := startWebSetupProcess(t, data, "-")
	var status map[string]bool
	p.json(t, "GET", "/api/v1/setup/status", nil, false, 200, &status)
	if len(status) != 1 || !status["required"] {
		t.Fatal("fresh server did not expose minimal setup status")
	}
	p.json(t, "POST", "/api/v1/setup/account", map[string]string{"password": integrationOwnerPassword}, false, 401, nil)
	p.json(t, "GET", "/ready", nil, false, 503, nil)
	p.json(t, "POST", "/api/v1/setup/account", map[string]string{"password": integrationOwnerPassword}, true, 200, nil)
	p.json(t, "POST", "/api/v1/setup/complete", map[string]string{"adapter": "local"}, true, 200, nil)
	p.wait(t, "/ready", 200)
	p.json(t, "GET", "/api/v1/auth/session", nil, false, 401, nil)
	p.json(t, "POST", "/api/v1/auth/login", map[string]string{"password": integrationOwnerPassword}, false, 200, nil)
	p.json(t, "POST", "/api/v1/setup/account", map[string]string{"password": "different-test-password"}, true, 404, nil)
	p.json(t, "GET", "/api/v1/setup/status", nil, false, 200, &status)
	if status["required"] {
		t.Fatal("initialized server reopened setup")
	}
	var folder, nested, trashed domain.Node
	p.json(t, "POST", "/api/v1/folders", map[string]string{"name": "Workspace", "parent_id": ""}, false, 201, &folder)
	p.json(t, "PATCH", "/api/v1/nodes/"+folder.ID, map[string]bool{"favorite": true}, false, 200, &folder)
	p.json(t, "POST", "/api/v1/folders", map[string]string{"name": "Drafts", "parent_id": folder.ID}, false, 201, &nested)
	p.json(t, "PATCH", "/api/v1/nodes/"+nested.ID, map[string]string{"name": "Final", "parent_id": ""}, false, 200, &nested)
	p.json(t, "PATCH", "/api/v1/nodes/"+nested.ID, map[string]string{"parent_id": folder.ID}, false, 200, &nested)
	p.json(t, "DELETE", "/api/v1/nodes/"+nested.ID, nil, false, 204, nil)
	p.json(t, "POST", "/api/v1/nodes/"+nested.ID+"/restore", map[string]any{}, false, 200, &nested)
	p.json(t, "POST", "/api/v1/folders", map[string]string{"name": "Removed fixture", "parent_id": ""}, false, 201, &trashed)
	p.json(t, "DELETE", "/api/v1/nodes/"+trashed.ID, nil, false, 204, nil)
	const size int64 = (100 << 20) + 17
	hash := sha256.New()
	if _, err := io.Copy(hash, io.NewSectionReader(webSetupFixture{}, 0, size)); err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%x", hash.Sum(nil))
	var upload domain.Upload
	p.json(t, "POST", "/api/v1/uploads", domain.UploadSpec{Name: "large-fixture.bin", ParentID: nested.ID, Size: size, MimeType: "application/octet-stream", SHA256: digest}, false, 201, &upload)
	if upload.ChunkSize != 100<<20 || upload.TotalChunks != 2 {
		t.Fatal("real API did not create 100 MiB chunks")
	}
	response := p.request(t, "PUT", "/api/v1/uploads/"+upload.ID+"/chunks/0", io.NewSectionReader(webSetupFixture{}, 0, upload.ChunkSize), false, 200)
	response.Body.Close()
	p.json(t, "POST", "/api/v1/uploads/"+upload.ID+"/complete", map[string]any{}, false, 409, nil)
	p.stop(t)
	p = startWebSetupProcess(t, data, "-")
	p.wait(t, "/ready", 200)
	p.json(t, "GET", "/api/v1/uploads/"+upload.ID, nil, false, 401, nil)
	p.json(t, "POST", "/api/v1/auth/login", map[string]string{"password": integrationOwnerPassword}, false, 200, nil)
	p.json(t, "GET", "/api/v1/uploads/"+upload.ID, nil, false, 200, &upload)
	if upload.ReceivedBytes != 100<<20 || len(upload.ReceivedChunks) != 1 || upload.ReceivedChunks[0] != 0 || upload.Status != "uploading" {
		t.Fatal("process restart lost persisted first chunk")
	}
	response = p.request(t, "PUT", "/api/v1/uploads/"+upload.ID+"/chunks/1", io.NewSectionReader(webSetupFixture{}, upload.ChunkSize, 17), false, 200)
	response.Body.Close()
	var file domain.Node
	p.json(t, "POST", "/api/v1/uploads/"+upload.ID+"/complete", map[string]any{}, false, 200, &file)
	if file.SHA256 != digest || file.ParentID != nested.ID {
		t.Fatal("completed file metadata differs from verified upload")
	}
	p.download(t, file.ID, digest, size)
	request, err := http.NewRequest("GET", p.base+"/api/v1/nodes/"+file.ID+"/content", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", upload.ChunkSize-8, size-1))
	response, err = p.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(io.LimitReader(response.Body, 26))
	response.Body.Close()
	want := make([]byte, 25)
	webSetupFixture{}.ReadAt(want, upload.ChunkSize-8)
	if err != nil || response.StatusCode != 206 || !bytes.Equal(got, want) {
		t.Fatalf("range across application chunks failed: status=%d error=%v", response.StatusCode, err)
	}
	p.stop(t)
	p = startWebSetupProcess(t, data, "-")
	p.wait(t, "/ready", 200)
	p.json(t, "POST", "/api/v1/auth/login", map[string]string{"password": integrationOwnerPassword}, false, 200, nil)
	p.download(t, file.ID, digest, size)
	checkpoint := p.request(t, "GET", "/api/v1/auth/session", nil, false, 200)
	oldCookies := p.client.Jar.Cookies(checkpoint.Request.URL)
	checkpoint.Body.Close()
	p.stop(t)
	parent := t.TempDir()
	if err := os.Chmod(parent, 0700); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(parent, "complete.harbor")
	backupPassword := []byte("synthetic-backup-recovery-password")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := recovery.Backup(ctx, recovery.BackupOptions{DataDir: data, Output: archive, Password: backupPassword}); err != nil {
		t.Fatalf("web setup complete encrypted backup failed: %v", err)
	}
	wrong := filepath.Join(parent, "wrong-password")
	if err := recovery.Restore(ctx, archive, wrong, []byte("incorrect-test-recovery-password")); err == nil {
		t.Fatal("incorrect backup password restored data")
	}
	if _, err := os.Stat(wrong); !os.IsNotExist(err) {
		t.Fatal("failed restore published destination")
	}
	restored := filepath.Join(parent, "restored")
	if err := recovery.Restore(ctx, archive, restored, backupPassword); err != nil {
		t.Fatalf("fresh recovery of web setup and 100 MiB blobs failed: %v", err)
	}
	p = startWebSetupProcess(t, restored, filepath.Join(restored, "recovery.env"))
	p.wait(t, "/ready", 200)
	p.json(t, "GET", "/api/v1/setup/status", nil, false, 200, &status)
	if status["required"] {
		t.Fatal("restored installation requires reclaiming owner account")
	}
	request, err = http.NewRequest("GET", p.base+"/api/v1/auth/session", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, cookie := range oldCookies {
		request.AddCookie(cookie)
	}
	response, err = p.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("restored installation accepted a pre-backup web session")
	}
	p.json(t, "POST", "/api/v1/auth/login", map[string]string{"password": integrationOwnerPassword}, false, 200, nil)
	p.json(t, "GET", "/api/v1/nodes/"+folder.ID, nil, false, 200, &folder)
	p.json(t, "GET", "/api/v1/nodes/"+nested.ID, nil, false, 200, &nested)
	p.json(t, "GET", "/api/v1/nodes/"+trashed.ID, nil, false, 200, &trashed)
	if !folder.Favorite || nested.ParentID != folder.ID || nested.Name != "Final" || trashed.TrashedAt == nil {
		t.Fatal("recovery lost hierarchy, favorite or Trash metadata")
	}
	p.download(t, file.ID, digest, size)
	p.stop(t)
}
