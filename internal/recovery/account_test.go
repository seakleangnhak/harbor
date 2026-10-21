package recovery

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"private-drive/internal/api"
	"private-drive/internal/storage"
)

func accountServer(t *testing.T, f *fixture) *api.Server {
	t.Helper()
	local, err := storage.NewLocal(filepath.Join(f.data, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	engine, err := storage.New(storage.Config{DB: f.db, Key: f.key, Backend: local})
	if err != nil {
		t.Fatal(err)
	}
	server, err := api.New(api.Config{DB: f.db, Storage: engine, Adapter: "local"})
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func TestAccountChangeSurvivesEncryptedRecovery(t *testing.T) {
	for _, webSetup := range []bool{false, true} {
		name := "legacy-initialization"
		if webSetup {
			name = "web-onboarding"
		}
		t.Run(name, func(t *testing.T) {
			f := makeFixture(t)
			if webSetup {
				setup := filepath.Join(f.data, "setup")
				if err := os.Mkdir(setup, 0700); err != nil {
					t.Fatal(err)
				}
				writePrivate(t, filepath.Join(setup, "config.env"), []byte("DRIVE_STORAGE=local\n"))
				writePrivate(t, filepath.Join(setup, "owner-password"), []byte("synthetic-owner-password-12345\n"))
			}
			server := accountServer(t, f)
			login := httptest.NewRecorder()
			r := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewBufferString(`{"password":"synthetic-owner-password-12345"}`))
			r.Header.Set("Content-Type", "application/json")
			server.Handler().ServeHTTP(login, r)
			if login.Code != 200 || len(login.Result().Cookies()) != 1 {
				t.Fatal("fixture login failed")
			}
			for _, operation := range []struct{ method, path, body string }{
				{"PATCH", "/api/v1/auth/profile", `{"display_name":"Harbor Test Owner"}`},
				{"POST", "/api/v1/auth/password", `{"current_password":"synthetic-owner-password-12345","new_password":"replacement-owner-password-12345"}`},
			} {
				r := httptest.NewRequest(operation.method, operation.path, bytes.NewBufferString(operation.body))
				r.Header.Set("Content-Type", "application/json")
				r.AddCookie(login.Result().Cookies()[0])
				w := httptest.NewRecorder()
				server.Handler().ServeHTTP(w, r)
				if w.Code != 200 && w.Code != 204 {
					t.Fatalf("account fixture operation failed: %d", w.Code)
				}
			}
			f.backup(t)
			if err := Verify(context.Background(), f.archive, testPassword); err != nil {
				t.Fatal(err)
			}
			destination := filepath.Join(f.root, "account-restored")
			if err := Restore(context.Background(), f.archive, destination, testPassword); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Lstat(filepath.Join(destination, "owner-password")); !os.IsNotExist(err) {
				t.Fatal("obsolete initialization password restored")
			}
			for restart := 0; restart < 2; restart++ {
				db, err := api.OpenDB(filepath.Join(destination, "drive.db"))
				if err != nil {
					t.Fatal(err)
				}
				restored := &fixture{data: destination, db: db, key: f.key}
				app := accountServer(t, restored)
				for password, expected := range map[string]int{"synthetic-owner-password-12345": 401, "replacement-owner-password-12345": 200} {
					body, _ := json.Marshal(map[string]string{"password": password})
					r := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(body))
					r.Header.Set("Content-Type", "application/json")
					w := httptest.NewRecorder()
					app.Handler().ServeHTTP(w, r)
					if w.Code != expected {
						t.Fatalf("restored login status=%d want=%d", w.Code, expected)
					}
					if expected == 200 {
						var session struct{ Owner string }
						if json.Unmarshal(w.Body.Bytes(), &session) != nil || session.Owner != "Harbor Test Owner" {
							t.Fatal("restored display name changed")
						}
					}
				}
				local, err := storage.NewLocal(filepath.Join(destination, "blobs"))
				if err != nil {
					t.Fatal(err)
				}
				engine, err := storage.New(storage.Config{DB: db, Key: f.key, Backend: local})
				if err != nil {
					t.Fatal(err)
				}
				reader, _, err := engine.Open(context.Background(), f.fileID)
				if err != nil {
					t.Fatal(err)
				}
				content, err := io.ReadAll(reader)
				_ = reader.Close()
				if err != nil || !bytes.Equal(content, f.plaintext) {
					t.Fatal("account recovery changed encrypted file content")
				}
				_ = db.Close()
			}
			// Explicit recovery input is still checked, even after a password change.
			if err := Backup(context.Background(), BackupOptions{DataDir: f.data, Output: filepath.Join(f.root, "stale-explicit.harbor"), Password: testPassword, OwnerPasswordFile: filepath.Join(f.data, "owner-password")}); err == nil {
				t.Fatal("explicit obsolete owner password accepted")
			}
		})
	}
}

func TestLegacyArchiveWithoutOwnerProfileRemainsRestorable(t *testing.T) {
	f := makeFixture(t)
	if _, err := f.db.Exec("DROP TABLE owner_profile"); err != nil {
		t.Fatal(err)
	}
	f.backup(t)
	destination := filepath.Join(f.root, "legacy-account-restored")
	if err := Restore(context.Background(), f.archive, destination, testPassword); err != nil {
		t.Fatal(err)
	}
	password, err := os.ReadFile(filepath.Join(destination, "owner-password"))
	if err != nil || string(bytes.TrimSpace(password)) != "synthetic-owner-password-12345" {
		t.Fatal("legacy initialization password recovery changed")
	}
}
