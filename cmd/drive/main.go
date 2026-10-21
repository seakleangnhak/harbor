package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"private-drive/internal/api"
	"private-drive/internal/domain"
	"private-drive/internal/storage"
)

var buildVersion = "1.1.0"

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	if err := loadEnvironment(); err != nil {
		return err
	}
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	dataDir := env("DRIVE_DATA_DIR", "data")
	if command == "health" {
		client := http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get(env("DRIVE_HEALTH_URL", "http://127.0.0.1:8080/health"))
		if err != nil {
			return errors.New("health check failed")
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return errors.New("health check failed")
		}
		return nil
	}
	if command == "integrity" || command == "integrity-content" {
		if err := requireExistingMetadata(dataDir, true); err != nil {
			return err
		}
	}
	if err := preparePrivateDataDir(dataDir); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	if command == "init" {
		return initialize(dataDir)
	}
	if command == "password" {
		if err := loadWebConfiguration(dataDir); err != nil {
			return err
		}
		return showPassword(dataDir)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	telegramCommand := command == "telegram-setup" || command == "telegram-channels" || command == "telegram-setup-qr"
	if command != "serve" && command != "integrity" && command != "integrity-content" && !telegramCommand {
		return errors.New("usage: drive [init|serve|health|integrity|integrity-content|password|telegram-setup|telegram-setup-qr|telegram-channels]")
	}
	lock, err := os.OpenFile(filepath.Join(dataDir, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("data directory already in use; stop running drive before starting another instance")
	}
	if telegramCommand {
		if err := loadWebConfiguration(dataDir); err != nil {
			return err
		}
		return setupTelegram(ctx, dataDir, command == "telegram-channels", command == "telegram-setup-qr")
	}
	if err := requireExistingMetadata(dataDir, false); err != nil {
		return err
	}
	if err := loadWebConfiguration(dataDir); err != nil {
		return err
	}
	if command == "serve" {
		required, err := needsWebSetup(dataDir)
		if err != nil {
			return err
		}
		if required {
			log.Print("Harbor first-run setup ready; open web UI and enter your environment bootstrap token")
			if err := serveWebSetup(ctx, dataDir); err != nil {
				return err
			}
			if err := loadWebConfiguration(dataDir); err != nil {
				return err
			}
		}
	}
	key, err := loadKey(dataDir)
	if err != nil {
		return err
	}
	adapter := env("DRIVE_STORAGE", "local")
	if adapter != "local" && adapter != "telegram" {
		return errors.New("DRIVE_STORAGE must be local or telegram")
	}
	var backend storage.BlobStore
	var channelID int64
	var storageStatus func() domain.StorageStatus
	if adapter == "telegram" {
		telegramCfg, err := telegramConfig(dataDir)
		if err != nil {
			return err
		}
		tg, err := storage.NewTelegram(ctx, telegramCfg)
		if err != nil {
			return telegramFailure(err)
		}
		defer tg.Close()
		backend, channelID = tg, tg.ChannelID()
		storageStatus = tg.Status
	}
	password := os.Getenv("DRIVE_OWNER_PASSWORD")
	passwordFile := env("DRIVE_OWNER_PASSWORD_FILE", filepath.Join(dataDir, "owner-password"))
	if password == "" {
		b, readErr := readPrivateFile(passwordFile)
		if readErr == nil {
			password = strings.TrimSuffix(strings.TrimSuffix(string(b), "\n"), "\r")
		} else if !errors.Is(readErr, os.ErrNotExist) {
			return readErr
		}
	}
	if password == "" {
		if _, statErr := os.Stat(filepath.Join(dataDir, "drive.db")); errors.Is(statErr, os.ErrNotExist) {
			return errors.New("first boot requires drive init or DRIVE_OWNER_PASSWORD_FILE")
		}
	}
	if err := bindStorage(dataDir, adapter, key, channelID); err != nil {
		return err
	}
	db, err := api.OpenDB(filepath.Join(dataDir, "drive.db"))
	if err != nil {
		return fmt.Errorf("open metadata database: %w", err)
	}
	defer db.Close()
	if command == "integrity" || command == "integrity-content" {
		if _, err := storage.CheckIntegrity(ctx, db); err != nil {
			return errors.New("existing metadata incomplete; restore original metadata before integrity check")
		}
	}
	if adapter == "local" {
		backend, err = storage.NewLocal(filepath.Join(dataDir, "blobs"))
	}
	if err != nil {
		return fmt.Errorf("initialize storage: %w", err)
	}
	engine, err := storage.New(storage.Config{DB: db, Key: key, Backend: backend, ChunkSize: storage.DefaultChunkSize})
	if err != nil {
		return fmt.Errorf("initialize encrypted engine: %w", err)
	}
	if command == "integrity" || command == "integrity-content" {
		var report storage.IntegrityReport
		if command == "integrity-content" {
			report, err = engine.VerifyIntegrity(ctx)
		} else {
			report, err = storage.CheckIntegrity(ctx, db)
		}
		if err != nil {
			return errors.New("integrity check could not complete; no content changed")
		}
		if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
			return errors.New("cannot write integrity report")
		}
		if len(report.Issues) > 0 || report.Suppressed > 0 {
			return errors.New("integrity issues found; review report before changing data")
		}
		return nil
	}
	secure, err := listenPolicy()
	if err != nil {
		return err
	}
	app, err := api.New(api.Config{Version: buildVersion, DB: db, Storage: engine, OwnerPassword: password, SecureCookies: secure, StaticDir: env("DRIVE_STATIC_DIR", "web/dist"), Adapter: adapter, TelegramConfigured: adapter == "telegram", StorageStatus: storageStatus})
	if err != nil {
		return fmt.Errorf("initialize API (first boot requires drive init or DRIVE_OWNER_PASSWORD_FILE): %w", err)
	}
	jobsDone := make(chan error, 1)
	go func() {
		err := app.RunCopyJobs(ctx)
		jobsDone <- err
		if err != nil {
			stop()
		}
	}()
	defer func() {
		stop()
		if err := <-jobsDone; err != nil {
			log.Print("Copy worker stopped; pending jobs remain recoverable")
		}
	}()
	server := &http.Server{Addr: env("DRIVE_ADDR", "127.0.0.1:8080"), Handler: withSetupStatus(app.Handler()), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := server.Shutdown(shutdown); err != nil {
				_ = server.Close()
			}
		case <-done:
		}
	}()
	defer close(done)
	log.Printf("Harbor listening on %s; storage=%s; encrypted chunks=%d MiB", server.Addr, adapter, storage.DefaultChunkSize>>20)
	err = server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func initialize(dataDir string) error {
	for _, name := range []string{"drive.db", "storage.json", "setup"} {
		if _, err := os.Lstat(filepath.Join(dataDir, name)); !errors.Is(err, os.ErrNotExist) {
			return errors.New("existing initialization found; initialization cannot change owner password")
		}
	}
	if _, err := loadKey(dataDir); err != nil {
		return err
	}
	file := env("DRIVE_OWNER_PASSWORD_FILE", filepath.Join(dataDir, "owner-password"))
	err := writeExclusive(file, []byte(rand.Text()+"\n"))
	if errors.Is(err, os.ErrExist) {
		return errors.New("owner password file already exists; initialization leaves existing secrets intact")
	}
	if err != nil {
		return err
	}
	fmt.Printf("Owner password saved to %s (mode 0600). Read locally to sign in. Keep password and content.key in a separate private backup.\n", file)
	return nil
}

func readPrivateFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("secret file %s must be a regular file with permissions 0600", path)
	}
	return os.ReadFile(path)
}

func writeExclusive(path string, b []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(b); err != nil {
		return err
	}
	return f.Sync()
}

func loadKey(dataDir string) ([]byte, error) {
	path := env("DRIVE_KEY_FILE", filepath.Join(dataDir, "content.key"))
	key, err := readPrivateFile(path)
	if err == nil {
		if len(key) != 32 {
			return nil, errors.New("content encryption key must contain exactly 32 raw bytes")
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, name := range []string{"drive.db", "storage.json"} {
		if _, err := os.Stat(filepath.Join(dataDir, name)); !errors.Is(err, os.ErrNotExist) {
			return nil, errors.New("existing drive missing encryption key; restore original key instead of creating another")
		}
	}
	key = make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	if err := writeExclusive(path, key); err != nil {
		return nil, err
	}
	return key, nil
}

func bindStorage(dataDir, adapter string, key []byte, channelID int64) error {
	type binding struct {
		Adapter   string `json:"adapter"`
		KeySHA256 string `json:"key_sha256"`
		ChannelID int64  `json:"telegram_channel_id,omitempty"`
	}
	if adapter == "telegram" && channelID <= 0 {
		return errors.New("Telegram storage requires a resolved positive channel ID; run telegram-setup first")
	}
	hash := sha256.Sum256(key)
	want := binding{adapter, hex.EncodeToString(hash[:]), channelID}
	path := filepath.Join(dataDir, "storage.json")
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if _, statErr := os.Stat(filepath.Join(dataDir, "drive.db")); !errors.Is(statErr, os.ErrNotExist) {
			return errors.New("existing drive missing storage binding; restore original storage.json")
		}
		b, _ = json.Marshal(want)
		return writeExclusive(path, b)
	}
	if err != nil {
		return err
	}
	var got binding
	if json.Unmarshal(b, &got) != nil || got != want {
		return errors.New("storage adapter, Telegram channel or encryption key differs from this drive; restore original configuration")
	}
	return nil
}
