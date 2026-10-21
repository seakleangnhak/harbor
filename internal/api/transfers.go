package api

import (
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"private-drive/internal/domain"
	"private-drive/internal/storage"
)

func validDigest(value string) bool {
	if value == "" {
		return true
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func (s *Server) beginUpload(w http.ResponseWriter, r *http.Request) {
	var spec domain.UploadSpec
	if err := readJSON(w, r, &spec); err != nil {
		respondError(w, err)
		return
	}
	if err := validateName(spec.Name); err != nil {
		respondError(w, err)
		return
	}
	if spec.Size < 0 || spec.Size > 1<<40 || !validDigest(spec.SHA256) || len(spec.MimeType) > 255 {
		respondError(w, domain.ErrInvalid)
		return
	}
	if spec.MimeType == "" {
		spec.MimeType = "application/octet-stream"
	}
	typeName, _, err := mime.ParseMediaType(spec.MimeType)
	if err != nil {
		respondError(w, domain.ErrInvalid)
		return
	}
	spec.MimeType = typeName
	spec.SHA256 = strings.ToLower(spec.SHA256)
	s.mutate.Lock()
	defer s.mutate.Unlock()
	if err = validParent(r.Context(), s.cfg.DB, spec.ParentID); err != nil {
		respondError(w, err)
		return
	}
	if err = siblingAvailable(r.Context(), s.cfg.DB, spec.ParentID, spec.Name, ""); err != nil {
		respondError(w, err)
		return
	}
	upload, err := s.cfg.Storage.Begin(r.Context(), spec)
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, 201, upload)
}

func (s *Server) listUploads(w http.ResponseWriter, r *http.Request) {
	uploads, err := s.cfg.Storage.List(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	items := []domain.Upload{}
	for _, u := range uploads {
		if strings.HasPrefix(u.ParentID, "copy:") {
			continue
		}
		if u.Status == "complete" {
			_, err = nodeFrom(r.Context(), s.cfg.DB, u.ID)
			if err == nil {
				continue
			}
			if !errors.Is(err, domain.ErrNotFound) {
				respondError(w, err)
				return
			}
		}
		items = append(items, u)
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

// Internal copy stages are managed only through their durable job.
func (s *Server) userUpload(r *http.Request) (domain.Upload, error) {
	upload, err := s.cfg.Storage.Status(r.Context(), chi.URLParam(r, "id"))
	if err == nil && strings.HasPrefix(upload.ParentID, "copy:") {
		err = domain.ErrNotFound
	}
	return upload, err
}

func (s *Server) uploadStatus(w http.ResponseWriter, r *http.Request) {
	u, err := s.userUpload(r)
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, 200, u)
}

func (s *Server) putChunk(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	index, err := strconv.Atoi(chi.URLParam(r, "index"))
	if err != nil || index < 0 || !validDigest(r.Header.Get("X-Chunk-SHA256")) {
		respondError(w, domain.ErrInvalid)
		return
	}
	u, err := s.userUpload(r)
	if err != nil {
		respondError(w, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, u.ChunkSize+1)
	u, err = s.cfg.Storage.PutChunk(r.Context(), id, index, r.Body, strings.ToLower(r.Header.Get("X-Chunk-SHA256")))
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, 200, u)
}

func (s *Server) putPart(w http.ResponseWriter, r *http.Request) {
	index, err := strconv.Atoi(chi.URLParam(r, "index"))
	part, partErr := strconv.Atoi(chi.URLParam(r, "part"))
	digest := r.Header.Get("X-Part-SHA256")
	if err != nil || index < 0 || partErr != nil || part < 0 || digest == "" || !validDigest(digest) {
		respondError(w, domain.ErrInvalid)
		return
	}
	u, err := s.userUpload(r)
	if err != nil {
		respondError(w, err)
		return
	}
	limit := min(storage.TransportPartSize, u.ChunkSize)
	if r.ContentLength > limit {
		respondError(w, &http.MaxBytesError{Limit: limit})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	u, err = s.cfg.Storage.PutPart(r.Context(), u.ID, index, part, r.Body, strings.ToLower(digest))
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, 200, u)
}

func (s *Server) commitChunk(w http.ResponseWriter, r *http.Request) {
	index, err := strconv.Atoi(chi.URLParam(r, "index"))
	if err != nil || index < 0 {
		respondError(w, domain.ErrInvalid)
		return
	}
	var body struct{}
	if err = readJSON(w, r, &body); err != nil {
		respondError(w, err)
		return
	}
	u, err := s.userUpload(r)
	if err != nil {
		respondError(w, err)
		return
	}
	u, err = s.cfg.Storage.CommitChunk(r.Context(), u.ID, index)
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, 200, u)
}

func (s *Server) completeUpload(w http.ResponseWriter, r *http.Request) {
	var body struct{}
	if err := readJSON(w, r, &body); err != nil {
		respondError(w, err)
		return
	}
	id := chi.URLParam(r, "id")
	s.mutate.Lock()
	defer s.mutate.Unlock()
	if n, err := nodeFrom(r.Context(), s.cfg.DB, id); err == nil {
		writeJSON(w, 200, n)
		return
	} else if !errors.Is(err, domain.ErrNotFound) {
		respondError(w, err)
		return
	}
	u, err := s.userUpload(r)
	if err != nil {
		respondError(w, err)
		return
	}
	if err = validParent(r.Context(), s.cfg.DB, u.ParentID); err != nil {
		respondError(w, err)
		return
	}
	if err = siblingAvailable(r.Context(), s.cfg.DB, u.ParentID, u.Name, id); err != nil {
		respondError(w, err)
		return
	}
	m, err := s.cfg.Storage.Complete(r.Context(), id)
	if err != nil {
		respondError(w, err)
		return
	}
	if m.ID != id {
		respondError(w, errors.New("manifest identity mismatch"))
		return
	}
	now := time.Now().UTC()
	n := domain.Node{ID: m.ID, ParentID: m.ParentID, Name: m.Name, Kind: "file", Size: m.Size, MimeType: m.MimeType, SHA256: m.SHA256, CreatedAt: m.CreatedAt, UpdatedAt: now}
	if err = insertNode(r.Context(), s.cfg.DB, n); err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, 200, n)
}

func (s *Server) cancelUpload(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s.mutate.Lock()
	defer s.mutate.Unlock()
	if _, err := nodeFrom(r.Context(), s.cfg.DB, id); err == nil {
		respondError(w, domain.ErrConflict)
		return
	} else if !errors.Is(err, domain.ErrNotFound) {
		respondError(w, err)
		return
	}
	upload, err := s.userUpload(r)
	if err != nil {
		respondError(w, err)
		return
	}
	if upload.Status == "complete" {
		err = s.cfg.Storage.DeleteFile(r.Context(), id)
	} else {
		err = s.cfg.Storage.Cancel(r.Context(), id)
	}
	if err != nil {
		respondError(w, err)
		return
	}
	w.WriteHeader(204)
}

func inlineContentType(name, value string) string {
	typeName := ""
	if strings.TrimSpace(value) != "" {
		var err error
		typeName, _, err = mime.ParseMediaType(value)
		if err != nil {
			return ""
		}
	}
	if typeName == "" || typeName == "application/octet-stream" {
		switch strings.ToLower(path.Ext(name)) {
		case ".jpg", ".jpeg", ".jpe":
			typeName = "image/jpeg"
		case ".png":
			typeName = "image/png"
		case ".gif":
			typeName = "image/gif"
		case ".webp":
			typeName = "image/webp"
		case ".avif":
			typeName = "image/avif"
		case ".bmp":
			typeName = "image/bmp"
		case ".ico":
			typeName = "image/x-icon"
		case ".heic":
			typeName = "image/heic"
		case ".heif":
			typeName = "image/heif"
		case ".pdf":
			typeName = "application/pdf"
		case ".mp3":
			typeName = "audio/mpeg"
		case ".m4a":
			typeName = "audio/mp4"
		case ".ogg":
			typeName = "audio/ogg"
		case ".wav":
			typeName = "audio/wav"
		case ".flac":
			typeName = "audio/flac"
		case ".mp4", ".m4v":
			typeName = "video/mp4"
		case ".webm":
			typeName = "video/webm"
		case ".ogv":
			typeName = "video/ogg"
		case ".mov":
			typeName = "video/quicktime"
		}
	}
	switch typeName {
	case "image/jpeg", "image/png", "image/gif", "image/webp", "image/avif", "image/bmp", "image/x-icon", "image/heic", "image/heif", "application/pdf", "text/plain", "audio/mpeg", "audio/ogg", "audio/wav", "audio/x-wav", "audio/mp4", "audio/flac", "audio/webm", "video/mp4", "video/webm", "video/ogg", "video/quicktime":
		return typeName
	}
	return ""
}

// First-byte verification catches a broken starting chunk before HTTP headers.
// Later authenticated-read failures abort the connection instead of looking complete.
type checkedStream struct{ storage.ReadSeekCloser }

func (s checkedStream) Read(p []byte) (int, error) {
	n, err := s.ReadSeekCloser.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		panic(http.ErrAbortHandler)
	}
	return n, err
}

func firstRangeOffset(header string, size int64) int64 {
	if !strings.HasPrefix(header, "bytes=") {
		return 0
	}
	first, _, _ := strings.Cut(strings.TrimPrefix(header, "bytes="), ",")
	start, end, ok := strings.Cut(strings.TrimSpace(first), "-")
	if !ok {
		return 0
	}
	if start == "" {
		suffix, err := strconv.ParseInt(end, 10, 64)
		if err == nil && suffix > 0 && suffix < size {
			return size - suffix
		}
		return 0
	}
	value, err := strconv.ParseInt(start, 10, 64)
	if err != nil || value < 0 || value >= size {
		return 0
	}
	return value
}

func (s *Server) content(w http.ResponseWriter, r *http.Request) {
	n, err := nodeFrom(r.Context(), s.cfg.DB, chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, err)
		return
	}
	if n.Kind != "file" || n.TrashedAt != nil {
		respondError(w, domain.ErrNotFound)
		return
	}
	reader, manifest, err := s.cfg.Storage.Open(r.Context(), n.ID)
	if err != nil {
		respondError(w, err)
		return
	}
	defer reader.Close()
	if manifest.SHA256 != n.SHA256 || manifest.Size != n.Size {
		respondError(w, domain.ErrIntegrity)
		return
	}
	if n.Size > 0 && r.Method != http.MethodHead {
		offset := firstRangeOffset(r.Header.Get("Range"), n.Size)
		if _, err = reader.Seek(offset, io.SeekStart); err != nil {
			respondError(w, err)
			return
		}
		var first [1]byte
		if _, err = io.ReadFull(reader, first[:]); err != nil {
			if errors.Is(err, io.EOF) {
				err = domain.ErrIntegrity
			}
			respondError(w, err)
			return
		}
		if _, err = reader.Seek(0, io.SeekStart); err != nil {
			respondError(w, err)
			return
		}
	}
	typeName := inlineContentType(n.Name, n.MimeType)
	disposition := "inline"
	if typeName == "" {
		typeName = "application/octet-stream"
		disposition = "attachment"
	} else if r.URL.Query().Get("download") == "1" {
		disposition = "attachment"
	}
	w.Header().Set("Content-Type", typeName)
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": n.Name}))
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("ETag", `"`+n.SHA256+`"`)
	w.Header().Set("X-Content-SHA256", n.SHA256)
	http.ServeContent(w, r, n.Name, n.UpdatedAt, checkedStream{reader})
}
