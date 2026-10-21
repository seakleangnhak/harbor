package api

import (
	"context"
	"net/http"
	"time"

	"private-drive/internal/domain"
	"private-drive/internal/storage"
)

func (s *Server) diagnostics(w http.ResponseWriter, r *http.Request) {
	var files, folders, used, trash int64
	err := s.cfg.DB.QueryRowContext(r.Context(), `SELECT coalesce(sum(CASE WHEN kind='file' AND trashed_at IS NULL THEN 1 ELSE 0 END),0),coalesce(sum(CASE WHEN kind='folder' AND trashed_at IS NULL THEN 1 ELSE 0 END),0),coalesce(sum(CASE WHEN kind='file' AND trashed_at IS NULL THEN size ELSE 0 END),0),coalesce(sum(CASE WHEN kind='file' AND trashed_at IS NOT NULL THEN size ELSE 0 END),0) FROM nodes`).Scan(&files, &folders, &used, &trash)
	if err != nil {
		respondError(w, err)
		return
	}
	uploads, err := s.cfg.Storage.List(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	pending := 0
	chunkSize := storage.DefaultChunkSize
	for _, u := range uploads {
		if u.Status != "complete" {
			pending++
		}
		chunkSize = u.ChunkSize
	}
	connection := domain.StorageStatus{State: "unknown"}
	if s.cfg.Adapter == "local" {
		connection.State = "local"
	}
	if s.cfg.StorageStatus != nil {
		connection = s.cfg.StorageStatus()
	}
	switch connection.State {
	case "connected", "disconnected", "connecting", "retrying", "local", "unknown":
	default:
		connection.State = "unknown"
	}
	writeJSON(w, 200, map[string]any{"adapter": s.cfg.Adapter, "telegram_configured": s.cfg.TelegramConfigured, "storage_connection": connection, "encryption": "AES-256-GCM", "database": "ok", "version": s.cfg.Version, "files": files, "folders": folders, "used_bytes": used, "trash_bytes": trash, "pending_uploads": pending, "chunk_size": chunkSize})
}

func (s *Server) backup(w http.ResponseWriter, r *http.Request) {
	fail(w, http.StatusGone, "encrypted_backup_required", "Use the server-side harbor-recovery tool to create a complete encrypted backup")
}

func (s *Server) readiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.cfg.DB.PingContext(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unready"})
		return
	}
	if s.cfg.Adapter == "telegram" && (s.cfg.StorageStatus == nil || s.cfg.StorageStatus().State != "connected") {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unready"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}
