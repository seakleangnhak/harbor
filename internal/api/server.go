package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	_ "modernc.org/sqlite"

	"private-drive/internal/domain"
	"private-drive/internal/storage"
)

type Config struct {
	Version            string
	DB                 *sql.DB
	Storage            storage.Service
	OwnerPassword      string
	SecureCookies      bool
	StaticDir          string
	Adapter            string
	TelegramConfigured bool
	StorageStatus      func() domain.StorageStatus
}

type Server struct {
	cfg        Config
	handler    http.Handler
	mutate     sync.Mutex
	copyWorker sync.Mutex
	authMu     sync.Mutex
	loginMu    sync.Mutex
	attempts   map[string]loginAttempt
}

func OpenDB(path string) (*sql.DB, error) {
	if path == "" {
		return nil, errors.New("database path required")
	}
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return nil, err
		}
		if err := prepareDatabaseFiles(path); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	var schemaVersion int
	if err = db.QueryRow("PRAGMA user_version").Scan(&schemaVersion); err != nil {
		db.Close()
		return nil, err
	}
	if schemaVersion > 1 {
		db.Close()
		return nil, errors.New("metadata schema is newer than this Harbor version")
	}
	for _, pragma := range []string{"PRAGMA journal_mode=WAL", "PRAGMA synchronous=FULL", "PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=5000"} {
		if _, err = db.Exec(pragma); err != nil {
			db.Close()
			return nil, err
		}
	}
	if path != ":memory:" {
		if err = os.Chmod(path, 0600); err != nil {
			db.Close()
			return nil, err
		}
	}
	return db, nil
}

func New(cfg Config) (*Server, error) {
	if cfg.DB == nil || cfg.Storage == nil {
		return nil, errors.New("database and storage required")
	}
	if cfg.Version == "" {
		cfg.Version = "1.0.0"
	}
	s := &Server{cfg: cfg, attempts: make(map[string]loginAttempt)}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	if err := s.initOwner(cfg.OwnerPassword); err != nil {
		return nil, err
	}
	r := chi.NewRouter()
	r.Use(middleware.Recoverer, s.securityHeaders)
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	r.Get("/ready", s.readiness)
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(s.sameOrigin)
		r.Post("/auth/login", s.login)
		r.Group(func(r chi.Router) {
			r.Use(s.authenticate)
			r.Get("/auth/session", s.session)
			r.Post("/auth/logout", s.logout)
			r.Patch("/auth/profile", s.updateProfile)
			r.Post("/auth/password", s.changePassword)
			r.Get("/nodes", s.listNodes)
			r.Get("/nodes/{id}", s.getNode)
			r.Post("/folders", s.createFolder)
			r.Patch("/nodes/{id}", s.patchNode)
			r.Delete("/nodes/{id}", s.trashNode)
			r.Post("/nodes/{id}/restore", s.restoreNode)
			r.Delete("/nodes/{id}/permanent", s.permanentNode)
			r.Post("/nodes/bulk", s.bulkNodes)
			r.Get("/uploads", s.listUploads)
			r.Post("/uploads", s.beginUpload)
			r.Get("/uploads/{id}", s.uploadStatus)
			r.Put("/uploads/{id}/chunks/{index}", s.putChunk)
			r.Put("/uploads/{id}/chunks/{index}/parts/{part}", s.putPart)
			r.Post("/uploads/{id}/chunks/{index}/commit", s.commitChunk)
			r.Post("/uploads/{id}/complete", s.completeUpload)
			r.Delete("/uploads/{id}", s.cancelUpload)
			r.MethodFunc("GET", "/nodes/{id}/content", s.content)
			r.MethodFunc("HEAD", "/nodes/{id}/content", s.content)
			r.Get("/diagnostics", s.diagnostics)
			r.Get("/backup", s.backup)
			r.Post("/copy-jobs", s.createCopyJob)
			r.Get("/copy-jobs", s.listCopyJobs)
			r.Get("/copy-jobs/{id}", s.getCopyJob)
			r.Post("/copy-jobs/{id}/retry", s.retryCopyJob)
			r.Delete("/copy-jobs/{id}", s.cancelCopyJob)
		})
	})
	r.Handle("/*", s.static())
	s.handler = r
	return s, nil
}

func (s *Server) Handler() http.Handler { return s.handler }

func (s *Server) migrate() error {
	tx, err := s.cfg.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int
	if err = tx.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > 1 {
		return errors.New("metadata schema is newer than this Harbor version; restore compatible backup or upgrade executable")
	}
	_, err = tx.Exec(`
CREATE TABLE IF NOT EXISTS owner (id INTEGER PRIMARY KEY CHECK(id=1), password_hash BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS owner_profile (id INTEGER PRIMARY KEY CHECK(id=1), display_name TEXT NOT NULL DEFAULT 'Owner', password_changed_at TEXT NOT NULL DEFAULT '');
INSERT OR IGNORE INTO owner_profile(id) VALUES(1);
CREATE TABLE IF NOT EXISTS sessions (token_hash TEXT PRIMARY KEY, expires_at INTEGER NOT NULL);
CREATE INDEX IF NOT EXISTS sessions_expiry ON sessions(expires_at);
CREATE TABLE IF NOT EXISTS nodes (
 id TEXT PRIMARY KEY, parent_id TEXT NOT NULL DEFAULT '', name TEXT NOT NULL, name_key TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('file','folder')), size INTEGER NOT NULL DEFAULT 0 CHECK(size>=0),
 mime_type TEXT NOT NULL DEFAULT '', sha256 TEXT NOT NULL DEFAULT '', favorite INTEGER NOT NULL DEFAULT 0,
 trashed_at TEXT, trash_root TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS nodes_active_name ON nodes(parent_id,name_key) WHERE trashed_at IS NULL;
CREATE INDEX IF NOT EXISTS nodes_parent ON nodes(parent_id);
CREATE INDEX IF NOT EXISTS nodes_trash ON nodes(trashed_at);
CREATE TABLE IF NOT EXISTS pending_deletions (
 node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE
);
` + copyJobsSchema)
	if err != nil {
		return err
	}
	if _, err = tx.Exec("PRAGMA user_version=1"); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; media-src 'self' blob:; frame-src 'self' blob:; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) static() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead || strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		if s.cfg.StaticDir == "" {
			http.NotFound(w, r)
			return
		}
		root, err := os.OpenRoot(s.cfg.StaticDir)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer root.Close()
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" || name == "." {
			name = "index.html"
		}
		file, err := root.Open(name)
		if err != nil && path.Ext(r.URL.Path) == "" {
			file, err = root.Open("index.html")
			name = "index.html"
		}
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, name, info.ModTime(), file)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if status != http.StatusNoContent {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func fail(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error": message, "code": code})
}

func respondError(w http.ResponseWriter, err error) {
	var max *http.MaxBytesError
	switch {
	case errors.As(err, &max):
		fail(w, 413, "body_too_large", "Request body too large")
	case errors.Is(err, domain.ErrNotFound):
		fail(w, 404, "not_found", "Resource not found")
	case errors.Is(err, errDeletionStarted):
		fail(w, 409, "deletion_started", errDeletionStarted.Error())
	case errors.Is(err, domain.ErrConflict):
		fail(w, 409, "conflict", "Name or transfer state conflicts with existing data")
	case errors.Is(err, domain.ErrInvalid):
		fail(w, 400, "invalid_request", "Invalid request")
	case errors.Is(err, domain.ErrIntegrity):
		fail(w, 409, "integrity_failed", "Content integrity check failed")
	case errors.Is(err, context.Canceled):
		fail(w, 408, "interrupted", "Request interrupted")
	default:
		slog.Error("API operation failed", "code", "internal")
		fail(w, 500, "internal", "Operation failed; retry or check server diagnostics")
	}
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) error {
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		return domain.ErrInvalid
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		var max *http.MaxBytesError
		if errors.As(err, &max) {
			return err
		}
		return domain.ErrInvalid
	}
	if err := d.Decode(new(any)); err != io.EOF {
		var max *http.MaxBytesError
		if errors.As(err, &max) {
			return err
		}
		return domain.ErrInvalid
	}
	return nil
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func databaseError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	if strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return domain.ErrConflict
	}
	return err
}
