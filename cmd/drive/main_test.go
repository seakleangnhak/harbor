package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestKeyAndAdapterBinding(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DRIVE_KEY_FILE", filepath.Join(dir, "content.key"))
	key, err := loadKey(dir)
	if err != nil || len(key) != 32 {
		t.Fatalf("key initialization: %v", err)
	}
	loaded, err := loadKey(dir)
	if err != nil || !bytes.Equal(key, loaded) {
		t.Fatalf("key changed on restart: %v", err)
	}
	if err = bindStorage(dir, "local", key, 0); err != nil {
		t.Fatal(err)
	}
	if err = bindStorage(dir, "local", key, 0); err != nil {
		t.Fatal(err)
	}
	if err = bindStorage(dir, "telegram", key, 123); err == nil {
		t.Fatal("adapter change accepted")
	}
	wrong := bytes.Repeat([]byte{42}, 32)
	if err = bindStorage(dir, "local", wrong, 0); err == nil {
		t.Fatal("wrong encryption key accepted")
	}
	if err = os.Remove(filepath.Join(dir, "content.key")); err != nil {
		t.Fatal(err)
	}
	if _, err = loadKey(dir); err == nil {
		t.Fatal("lost key replaced for existing drive")
	}
}

func TestPrivateFilesAndExclusiveSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	if err := writeExclusive(path, []byte("original")); err != nil {
		t.Fatal(err)
	}
	if err := writeExclusive(path, []byte("overwrite")); !os.IsExist(err) {
		t.Fatalf("existing secret overwrite: %v", err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivateFile(path); err == nil {
		t.Fatal("publicly readable secret accepted")
	}
	link := path + "-link"
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivateFile(link); err == nil {
		t.Fatal("symlink secret accepted")
	}
}

func TestTelegramChannelBinding(t *testing.T) {
	dir := t.TempDir()
	key := bytes.Repeat([]byte{7}, 32)
	if err := bindStorage(dir, "telegram", key, 0); err == nil {
		t.Fatal("unconfigured channel accepted")
	}
	if err := bindStorage(dir, "telegram", key, 123); err != nil {
		t.Fatal(err)
	}
	if err := bindStorage(dir, "telegram", key, 123); err != nil {
		t.Fatal(err)
	}
	bindingPath := filepath.Join(dir, "storage.json")
	before, err := os.ReadFile(bindingPath)
	if err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "drive.db")
	if err := os.WriteFile(dbPath, []byte("existing metadata fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := bindStorage(dir, "telegram", key, 456); err == nil {
		t.Fatal("channel namespace changed")
	}
	after, err := os.ReadFile(bindingPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("rejected channel changed storage binding")
	}
	metadata, err := os.ReadFile(dbPath)
	if err != nil || string(metadata) != "existing metadata fixture" {
		t.Fatal("rejected channel changed metadata")
	}
}

func TestMissingStorageBindingCannotRebindExistingDrive(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "drive.db"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, adapter := range []string{"local", "telegram"} {
		if err := bindStorage(dir, adapter, bytes.Repeat([]byte{7}, 32), 123); err == nil {
			t.Fatalf("missing binding recreated for existing %s drive", adapter)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "storage.json")); !os.IsNotExist(err) {
		t.Fatal("failed binding check wrote storage.json")
	}
}

func TestMissingFirstBootPasswordStillAllowsInitialization(t *testing.T) {
	dir := privateFixtureDir(t)
	t.Setenv("DRIVE_DATA_DIR", dir)
	t.Setenv("DRIVE_STORAGE", "local")
	t.Setenv("DRIVE_OWNER_PASSWORD", "")
	t.Setenv("DRIVE_OWNER_PASSWORD_FILE", filepath.Join(dir, "owner-password"))
	t.Setenv("DRIVE_KEY_FILE", filepath.Join(dir, "content.key"))
	previous := os.Args
	os.Args = []string{"drive"}
	defer func() { os.Args = previous }()
	if err := run(); err == nil {
		t.Fatal("uninitialized owner accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "drive.db")); !os.IsNotExist(err) {
		t.Fatal("failed first boot created metadata database")
	}
	if err := initialize(dir); err != nil {
		t.Fatalf("initialization blocked after failed first boot: %v", err)
	}
}

func TestTelegramCommandsRequireInteractiveTerminal(t *testing.T) {
	dir := privateFixtureDir(t)
	input, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	previousInput := os.Stdin
	os.Stdin = input
	defer func() { os.Stdin = previousInput }()
	t.Setenv("DRIVE_DATA_DIR", dir)
	t.Setenv("TELEGRAM_APP_ID", "12345")
	t.Setenv("TELEGRAM_APP_HASH", strings.Repeat("0", 32))
	t.Setenv("USERNAME_OR_CHANNEL_LINK", "")
	t.Setenv("TELEGRAM_CHANNEL_ID", "")
	t.Setenv("TELEGRAM_CHANNEL_ACCESS_HASH", "")
	previous := os.Args
	defer func() { os.Args = previous }()
	for _, command := range []string{"telegram-setup", "telegram-setup-qr", "telegram-channels"} {
		os.Args = []string{"drive", command}
		if err := run(); err == nil || !strings.Contains(err.Error(), "interactive terminal") {
			t.Fatalf("%s non-terminal execution: %v", command, err)
		}
	}
	for _, file := range []string{"telegram.session", "drive.db", "content.key"} {
		if _, err := os.Stat(filepath.Join(dir, file)); !os.IsNotExist(err) {
			t.Fatalf("rejected setup created %s", file)
		}
	}
}

func TestTelegramCommandsRespectDataDirectoryLock(t *testing.T) {
	dir := privateFixtureDir(t)
	t.Setenv("DRIVE_DATA_DIR", dir)
	lock, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	previous := os.Args
	defer func() { os.Args = previous }()
	for _, command := range []string{"telegram-setup", "telegram-setup-qr", "telegram-channels"} {
		os.Args = []string{"drive", command}
		if err := run(); err == nil || !strings.Contains(err.Error(), "data directory already in use") {
			t.Fatalf("%s ignored active data directory lock: %v", command, err)
		}
	}
}

func TestTelegramChannelConfiguration(t *testing.T) {
	t.Setenv("TELEGRAM_APP_ID", "12345")
	t.Setenv("TELEGRAM_APP_HASH", strings.Repeat("0", 32))
	for _, tc := range []struct {
		name, link, id, hash string
		invalid              bool
	}{
		{name: "new channel"},
		{name: "private invite", link: "https://t.me/+PrivateFixture"},
		{name: "private message", link: "https://t.me/c/12345/7"},
		{name: "invite with expected ID", link: "https://t.me/+PrivateFixture", id: "12345"},
		{name: "invite with Bot API ID", link: "https://t.me/+PrivateFixture", id: "-1000000012345"},
		{name: "blank selector", link: " \t ", invalid: true},
		{name: "legacy signed hash", id: "12345", hash: "-987654321"},
		{name: "mixed selectors", link: "https://t.me/+PrivateFixture", id: "12345", hash: "-987654321", invalid: true},
		{name: "missing hash", id: "12345", invalid: true},
		{name: "missing ID", hash: "-987654321", invalid: true},
		{name: "bot ID", id: "-10012345", hash: "123", invalid: true},
		{name: "invalid ID", id: "not-an-id", hash: "123", invalid: true},
		{name: "invalid hash", id: "12345", hash: "not-a-hash", invalid: true},
		{name: "zero hash", id: "12345", hash: "0", invalid: true},
		{name: "overflow hash", id: "12345", hash: "9223372036854775808", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("USERNAME_OR_CHANNEL_LINK", tc.link)
			t.Setenv("TELEGRAM_CHANNEL_ID", tc.id)
			t.Setenv("TELEGRAM_CHANNEL_ACCESS_HASH", tc.hash)
			cfg, err := telegramConfig(t.TempDir())
			if (err != nil) != tc.invalid {
				t.Fatalf("configuration error = %v, invalid = %v", err, tc.invalid)
			}
			if !tc.invalid && (cfg.ChannelLink != tc.link || (tc.hash == "-987654321" && cfg.ChannelAccessHash != -987654321)) {
				t.Fatal("channel selector or signed hash changed")
			}
			if !tc.invalid && tc.id != "" && cfg.ChannelID != 12345 {
				t.Fatal("channel ID normalization changed target")
			}
		})
	}
}
