package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrivateEnvironmentFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	t.Setenv("DRIVE_ENV_FILE", path)
	t.Setenv("HARBOR_TEST_EXISTING", "shell")
	for _, name := range []string{"HARBOR_TEST_QUOTED", "HARBOR_TEST_LITERAL", "HARBOR_TEST_EMPTY"} {
		t.Setenv(name, "")
		os.Unsetenv(name)
	}
	content := "# private fixture\nexport HARBOR_TEST_EXISTING=file\nHARBOR_TEST_QUOTED=\"line\\nend\\\\\" # comment\nHARBOR_TEST_LITERAL='$(do-not-execute) $HOME # literal'\nHARBOR_TEST_EMPTY=\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if err := loadEnvironment(); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("HARBOR_TEST_EXISTING") != "shell" || os.Getenv("HARBOR_TEST_QUOTED") != "line\nend\\" || os.Getenv("HARBOR_TEST_LITERAL") != "$(do-not-execute) $HOME # literal" {
		t.Fatal("file precedence, quoting or literal values changed")
	}
	if value, set := os.LookupEnv("HARBOR_TEST_EMPTY"); !set || value != "" {
		t.Fatal("empty assignment lost")
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if err := loadEnvironment(); err == nil {
		t.Fatal("public environment file accepted")
	}
}

func TestEnvironmentErrorsDoNotLeakValuesOrPartiallyLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	t.Setenv("DRIVE_ENV_FILE", path)
	t.Setenv("HARBOR_TEST_SECRET", "unchanged")
	for _, content := range []string{"HARBOR_TEST_SECRET=private-fixture\ninvalid assignment", "HARBOR_TEST_SECRET='private-fixture", "HARBOR_TEST_SECRET=\"private-fixture\" unexpected"} {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if err := loadEnvironment(); err == nil || strings.Contains(err.Error(), "private-fixture") {
			t.Fatalf("invalid environment accepted or leaked: %v", err)
		}
		if os.Getenv("HARBOR_TEST_SECRET") != "unchanged" {
			t.Fatal("invalid file partially changed environment")
		}
	}
	t.Setenv("DRIVE_ENV_FILE", "-")
	if err := loadEnvironment(); err != nil {
		t.Fatal(err)
	}
}
