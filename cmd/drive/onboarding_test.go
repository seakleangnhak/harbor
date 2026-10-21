package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"private-drive/internal/onboarding"
)

func cleanSetupEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{"DRIVE_STORAGE", "DRIVE_OWNER_PASSWORD", "DRIVE_OWNER_PASSWORD_FILE", "DRIVE_SETUP_TOKEN", "TELEGRAM_APP_ID", "TELEGRAM_APP_HASH", "USERNAME_OR_CHANNEL_LINK", "TELEGRAM_CHANNEL_ID", "TELEGRAM_SESSION_FILE", "TELEGRAM_CHANNEL_ACCESS_HASH"} {
		t.Setenv(name, "")
	}
}

func TestWebSetupActivationAndRestart(t *testing.T) {
	cleanSetupEnvironment(t)
	dir := t.TempDir()
	_ = os.Chmod(dir, 0700)
	if _, err := needsWebSetup(dir); err == nil {
		t.Fatal("unprotected first-run setup allowed")
	}
	t.Setenv("DRIVE_SETUP_TOKEN", "synthetic-bootstrap-token-32-characters")
	if required, err := needsWebSetup(dir); err != nil || !required {
		t.Fatalf("fresh setup: %v %v", required, err)
	}
	key, err := loadKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	c := onboarding.Completion{OwnerPassword: "synthetic-owner-password", Adapter: "local"}
	if err := commitWebSetup(context.Background(), dir, c); err != nil {
		t.Fatal(err)
	}
	if required, err := needsWebSetup(dir); err != nil || required {
		t.Fatal("committed installation can be reclaimed")
	}
	if err := commitWebSetup(context.Background(), dir, c); err != nil {
		t.Fatal("identical commit retry failed")
	}
	if err := commitWebSetup(context.Background(), dir, onboarding.Completion{OwnerPassword: "different-owner-password", Adapter: "local"}); err == nil {
		t.Fatal("changed activation allowed")
	}
	if err := initialize(dir); err == nil {
		t.Fatal("CLI init changed activated setup")
	}
	if err := loadWebConfiguration(dir); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("DRIVE_STORAGE") != "local" {
		t.Fatal("adapter not restored")
	}
	password, err := readPrivateFile(os.Getenv("DRIVE_OWNER_PASSWORD_FILE"))
	if err != nil || string(password) != c.OwnerPassword {
		t.Fatal("owner credential not restored")
	}
	after, err := loadKey(dir)
	if err != nil || string(after) != string(key) {
		t.Fatal("key changed")
	}
}

func TestWebSetupExistingStateAndInterruption(t *testing.T) {
	for _, state := range []string{"drive.db", "storage.json"} {
		t.Run(state, func(t *testing.T) {
			cleanSetupEnvironment(t)
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, state), []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("DRIVE_SETUP_TOKEN", "synthetic-bootstrap-token-32-characters")
			if required, err := needsWebSetup(dir); err != nil || required {
				t.Fatal("existing state exposed setup")
			}
			if err := commitWebSetup(context.Background(), dir, onboarding.Completion{Adapter: "local", OwnerPassword: "synthetic-owner-password"}); err == nil {
				t.Fatal("existing state overwritten")
			}
		})
	}
	cleanSetupEnvironment(t)
	dir := t.TempDir()
	_ = os.Chmod(dir, 0700)
	pending := filepath.Join(dir, "setup.pending")
	if err := os.Mkdir(pending, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pending, "config.env"), []byte("DRIVE_STORAGE=telegram\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := loadWebConfiguration(dir); err != nil || os.Getenv("DRIVE_STORAGE") != "" {
		t.Fatal("incomplete setup activated")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := commitWebSetup(ctx, dir, onboarding.Completion{Adapter: "local", OwnerPassword: "synthetic-owner-password"}); err == nil {
		t.Fatal("canceled commit activated")
	}
	if err := commitWebSetup(context.Background(), dir, onboarding.Completion{Adapter: "telegram", OwnerPassword: "synthetic-owner-password"}); err == nil {
		t.Fatal("missing Telegram session accepted")
	}
	if err := commitWebSetup(context.Background(), dir, onboarding.Completion{Adapter: "local", OwnerPassword: "synthetic-owner-password"}); err != nil {
		t.Fatal(err)
	}
}

func TestWebSetupPrivateConfigurationAndOverrides(t *testing.T) {
	cleanSetupEnvironment(t)
	dir := t.TempDir()
	_ = os.Chmod(dir, 0700)
	c := onboarding.Completion{Adapter: "local", OwnerPassword: "synthetic-owner-password"}
	t.Setenv("DRIVE_STORAGE", "telegram")
	if err := commitWebSetup(context.Background(), dir, c); err == nil {
		t.Fatal("conflicting environment ignored")
	}
	t.Setenv("DRIVE_STORAGE", "")
	if err := commitWebSetup(context.Background(), dir, c); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, "setup", "config.env"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := loadWebConfiguration(dir); err == nil {
		t.Fatal("public-readable credential file accepted")
	}
}

func TestNormalServerSetupDisabled(t *testing.T) {
	h := withSetupStatus(http.NotFoundHandler())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/setup/status", nil))
	if w.Code != 200 || w.Body.String() != `{"required":false}` {
		t.Fatal("normal server setup status incorrect")
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/setup/account", nil))
	if w.Code != 404 {
		t.Fatal("normal server setup enabled")
	}
}
