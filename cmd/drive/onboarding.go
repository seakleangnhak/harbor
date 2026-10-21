package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"private-drive/internal/onboarding"
	"private-drive/internal/recovery"
	"private-drive/internal/storage"
)

// An atomic directory rename activates owner credentials, configuration and the
// verified session together. An interrupted setup remains unclaimed and private.
func loadWebConfiguration(dataDir string) error {
	dir := filepath.Join(dataDir, "setup")
	if _, err := os.Lstat(dir); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err := preparePrivateDataDir(dir); err != nil {
		return errors.New("web setup directory must be private and intact")
	}
	raw, err := readPrivateFile(filepath.Join(dir, "config.env"))
	if err != nil || len(raw) > 16384 {
		return errors.New("web setup configuration unavailable; restore its private backup")
	}
	values, err := recovery.ReadConfig(raw)
	if err != nil || (values["DRIVE_STORAGE"] != "local" && values["DRIVE_STORAGE"] != "telegram") {
		return errors.New("web setup configuration invalid; restore its private backup")
	}
	values["DRIVE_OWNER_PASSWORD_FILE"] = filepath.Join(dir, "owner-password")
	if values["DRIVE_STORAGE"] == "telegram" {
		values["TELEGRAM_SESSION_FILE"] = filepath.Join(dir, "telegram.session")
	}
	for name, value := range values {
		if os.Getenv(name) == "" {
			if err := os.Setenv(name, value); err != nil {
				return errors.New("cannot load web setup configuration")
			}
		}
	}
	return nil
}

func needsWebSetup(dataDir string) (bool, error) {
	for _, name := range []string{"drive.db", "storage.json", "setup"} {
		_, err := os.Lstat(filepath.Join(dataDir, name))
		if err == nil {
			return false, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return false, errors.New("cannot inspect existing drive state")
		}
	}
	if os.Getenv("DRIVE_SETUP_TOKEN") != "" {
		return true, nil
	}
	if os.Getenv("DRIVE_OWNER_PASSWORD") != "" {
		return false, nil
	}
	_, err := os.Lstat(env("DRIVE_OWNER_PASSWORD_FILE", filepath.Join(dataDir, "owner-password")))
	if err == nil {
		return false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, errors.New("cannot inspect owner configuration")
	}
	return false, errors.New("fresh installation: set DRIVE_SETUP_TOKEN to a random secret of at least 32 characters, then open Harbor for web setup")
}

func listenPolicy() (bool, error) {
	secure, err := strconv.ParseBool(env("DRIVE_SECURE_COOKIES", "false"))
	if err != nil {
		return false, errors.New("DRIVE_SECURE_COOKIES must be true or false")
	}
	allow, err := strconv.ParseBool(env("DRIVE_ALLOW_REMOTE_HTTP", "false"))
	if err != nil {
		return false, errors.New("DRIVE_ALLOW_REMOTE_HTTP must be true or false")
	}
	return secure, validateListenConfig(env("DRIVE_ADDR", "127.0.0.1:8080"), secure, allow)
}

func serveWebSetup(ctx context.Context, dataDir string) error {
	token := os.Getenv("DRIVE_SETUP_TOKEN")
	if len(token) < 32 || len(token) > 256 || strings.TrimSpace(token) != token {
		return errors.New("DRIVE_SETUP_TOKEN must be a random secret of 32–256 characters")
	}
	secure, err := listenPolicy()
	if err != nil {
		return err
	}
	pending := filepath.Join(dataDir, "setup.pending")
	if err := preparePrivateDataDir(pending); err != nil {
		return err
	}
	complete := make(chan struct{}, 1)
	setup, err := onboarding.New(onboarding.Config{
		Token: token, SecureCookies: secure,
		SetupTelegram: func(ctx context.Context, input onboarding.TelegramInput, qr func(context.Context, string) error, password func(context.Context) (string, error)) error {
			// Each explicit attempt can select the intended account; never reuse a
			// wrong account left by a failed private-channel check.
			if err := os.Remove(filepath.Join(pending, "telegram.session")); err != nil && !errors.Is(err, os.ErrNotExist) {
				return errors.New("cannot reset pending authorization")
			}
			cfg := storage.TelegramConfig{AppID: input.AppID, AppHash: input.AppHash, ChannelLink: input.ChannelLink, ChannelID: input.ChannelID, SessionFile: filepath.Join(pending, "telegram.session"), QRCode: qr}
			_, err := storage.SetupTelegram(ctx, cfg, "", nil, password)
			return err
		},
		Commit: func(ctx context.Context, completion onboarding.Completion) error {
			return commitWebSetup(ctx, dataDir, completion)
		},
		Completed: func() {
			select {
			case complete <- struct{}{}:
			default:
			}
		},
	})
	if err != nil {
		return err
	}
	defer setup.Close()
	mux := http.NewServeMux()
	mux.Handle("/api/", setup.Handler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "setup required", http.StatusServiceUnavailable)
	})
	static := http.FileServer(http.Dir(env("DRIVE_STATIC_DIR", "web/dist")))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; form-action 'self'; base-uri 'self'")
		if r.URL.Path != "/" && !strings.HasPrefix(r.URL.Path, "/assets/") && r.URL.Path != "/favicon.svg" {
			http.NotFound(w, r)
			return
		}
		static.ServeHTTP(w, r)
	})
	server := &http.Server{Addr: env("DRIVE_ADDR", "127.0.0.1:8080"), Handler: mux, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 45 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	result := make(chan error, 1)
	go func() { result <- server.ListenAndServe() }()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
	case <-complete:
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil {
		_ = server.Close()
		return errors.New("web setup shutdown failed; restart to use saved configuration")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

func commitWebSetup(ctx context.Context, dataDir string, c onboarding.Completion) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.Adapter != "local" && c.Adapter != "telegram" || len(c.OwnerPassword) < 12 || len(c.OwnerPassword) > 72 || strings.ContainsAny(c.OwnerPassword, "\r\n") {
		return errors.New("invalid setup completion")
	}
	if os.Getenv("DRIVE_OWNER_PASSWORD_FILE") != "" {
		return errors.New("owner password file override conflicts with web setup")
	}
	for name, value := range map[string]string{"DRIVE_STORAGE": c.Adapter, "DRIVE_OWNER_PASSWORD": c.OwnerPassword} {
		if existing := os.Getenv(name); existing != "" && existing != value {
			return errors.New("environment conflicts with web setup; remove storage/password overrides before setup")
		}
	}
	if c.Adapter == "telegram" {
		for name, value := range map[string]string{"TELEGRAM_APP_ID": strconv.Itoa(c.Telegram.AppID), "TELEGRAM_APP_HASH": c.Telegram.AppHash, "USERNAME_OR_CHANNEL_LINK": c.Telegram.ChannelLink, "TELEGRAM_CHANNEL_ID": strconv.FormatInt(c.Telegram.ChannelID, 10)} {
			if existing := os.Getenv(name); existing != "" && existing != value {
				return errors.New("environment conflicts with web setup; remove Telegram overrides before setup")
			}
		}
		if os.Getenv("TELEGRAM_SESSION_FILE") != "" || os.Getenv("TELEGRAM_CHANNEL_ACCESS_HASH") != "" {
			return errors.New("legacy Telegram overrides conflict with web setup")
		}
	}
	config := "# Private Harbor web configuration. Never share or commit.\nDRIVE_STORAGE=" + strconv.Quote(c.Adapter) + "\n"
	if c.Adapter == "telegram" {
		config += fmt.Sprintf("TELEGRAM_APP_ID=%d\nTELEGRAM_APP_HASH=%s\nUSERNAME_OR_CHANNEL_LINK=%s\n", c.Telegram.AppID, strconv.Quote(c.Telegram.AppHash), strconv.Quote(c.Telegram.ChannelLink))
		if c.Telegram.ChannelID != 0 {
			config += fmt.Sprintf("TELEGRAM_CHANNEL_ID=%d\n", c.Telegram.ChannelID)
		}
	}
	pending, active := filepath.Join(dataDir, "setup.pending"), filepath.Join(dataDir, "setup")
	for _, name := range []string{"drive.db", "storage.json"} {
		if _, err := os.Lstat(filepath.Join(dataDir, name)); !errors.Is(err, os.ErrNotExist) {
			return errors.New("existing metadata prevents new setup")
		}
	}
	if _, err := os.Lstat(active); !errors.Is(err, os.ErrNotExist) {
		// Retrying a parent-directory fsync must not reset a committed identity.
		if err := preparePrivateDataDir(active); err != nil {
			return err
		}
		savedConfig, configErr := readPrivateFile(filepath.Join(active, "config.env"))
		savedPassword, passwordErr := readPrivateFile(filepath.Join(active, "owner-password"))
		if configErr != nil || passwordErr != nil || string(savedConfig) != config || string(savedPassword) != c.OwnerPassword {
			return errors.New("setup already committed with different configuration")
		}
		return syncSetupDir(dataDir)
	}
	if err := preparePrivateDataDir(pending); err != nil {
		return err
	}
	if c.Adapter == "telegram" {
		if _, err := readPrivateFile(filepath.Join(pending, "telegram.session")); err != nil {
			return errors.New("verified Telegram session is missing")
		}
	} else if err := os.Remove(filepath.Join(pending, "telegram.session")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.New("cannot discard unused setup session")
	}
	for name, content := range map[string][]byte{"config.env": []byte(config), "owner-password": []byte(c.OwnerPassword)} {
		file, err := os.CreateTemp(pending, ".commit-")
		if err != nil {
			return errors.New("cannot stage private setup")
		}
		tmp := file.Name()
		_, err = file.Write(content)
		if err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil {
			err = os.Rename(tmp, filepath.Join(pending, name))
		}
		_ = os.Remove(tmp)
		if err != nil {
			return errors.New("cannot persist private setup")
		}
	}
	if err := syncSetupDir(pending); err != nil {
		return err
	}
	if err := os.Rename(pending, active); err != nil {
		return errors.New("cannot activate setup")
	}
	return syncSetupDir(dataDir)
}

func syncSetupDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return errors.New("cannot sync setup directory")
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return errors.New("cannot sync setup directory")
	}
	return nil
}

func withSetupStatus(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/setup/status" && r.Method == http.MethodGet {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"required":false}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}
