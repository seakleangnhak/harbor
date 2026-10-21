package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"private-drive/internal/domain"
)

const nodeColumns = "id,parent_id,name,kind,size,mime_type,sha256,favorite,trashed_at,created_at,updated_at"
const subtreeSQL = `WITH RECURSIVE subtree(id) AS (SELECT id FROM nodes WHERE id=? UNION SELECT n.id FROM nodes n JOIN subtree s ON n.parent_id=s.id) `
const nodeTypeSQL = `CASE
 WHEN kind='folder' THEN 'folder'
 WHEN mime_type LIKE 'image/%' THEN 'image'
 WHEN mime_type LIKE 'video/%' THEN 'video'
 WHEN mime_type LIKE 'audio/%' THEN 'audio'
 WHEN mime_type LIKE '%zip%' OR mime_type LIKE '%rar%' OR mime_type LIKE '%7z%' OR mime_type LIKE '%tar%'
   OR mime_type LIKE '%gzip%' OR mime_type LIKE '%bzip%' OR mime_type LIKE '%xz%' OR mime_type LIKE '%compressed%'
   OR lower(name) GLOB '*.zip' OR lower(name) GLOB '*.rar' OR lower(name) GLOB '*.7z' OR lower(name) GLOB '*.tar'
   OR lower(name) GLOB '*.gz' OR lower(name) GLOB '*.bz2' OR lower(name) GLOB '*.xz' THEN 'archive'
 WHEN mime_type LIKE 'text/%' OR mime_type LIKE '%pdf%' OR mime_type LIKE '%document%'
   OR mime_type LIKE '%sheet%' OR mime_type LIKE '%presentation%' OR mime_type LIKE '%msword%'
   OR lower(name) GLOB '*.pdf' OR lower(name) GLOB '*.doc' OR lower(name) GLOB '*.docx'
   OR lower(name) GLOB '*.xls' OR lower(name) GLOB '*.xlsx' OR lower(name) GLOB '*.ppt' OR lower(name) GLOB '*.pptx'
   OR lower(name) GLOB '*.md' OR lower(name) GLOB '*.json' OR lower(name) GLOB '*.csv' OR lower(name) GLOB '*.rtf' OR lower(name) GLOB '*.odt' THEN 'document'
 ELSE 'other' END`

var errDeletionStarted = errors.New("Permanent deletion already started; retry delete to finish")

type database interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
type scanner interface{ Scan(...any) error }

func scanNode(row scanner) (domain.Node, error) {
	var n domain.Node
	var trash sql.NullString
	var created, updated string
	if err := row.Scan(&n.ID, &n.ParentID, &n.Name, &n.Kind, &n.Size, &n.MimeType, &n.SHA256, &n.Favorite, &trash, &created, &updated); err != nil {
		return n, databaseError(err)
	}
	var err error
	if n.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return n, err
	}
	if n.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
		return n, err
	}
	if trash.Valid {
		t, err := time.Parse(time.RFC3339Nano, trash.String)
		if err != nil {
			return n, err
		}
		n.TrashedAt = &t
	}
	return n, nil
}

func nodeFrom(ctx context.Context, db database, id string) (domain.Node, error) {
	if id == "" {
		return domain.Node{}, domain.ErrNotFound
	}
	return scanNode(db.QueryRowContext(ctx, "SELECT "+nodeColumns+" FROM nodes WHERE id=?", id))
}

func validateName(name string) error {
	if !utf8.ValidString(name) || len(name) == 0 || len(name) > 255 || strings.TrimSpace(name) != name || name == "." || name == ".." {
		return domain.ErrInvalid
	}
	for _, c := range name {
		if c == '/' || c == '\\' || unicode.IsControl(c) {
			return domain.ErrInvalid
		}
	}
	return nil
}

func validParent(ctx context.Context, db database, parent string) error {
	if parent == "" {
		return nil
	}
	n, err := nodeFrom(ctx, db, parent)
	if err != nil {
		return err
	}
	if n.Kind != "folder" || n.TrashedAt != nil {
		return domain.ErrConflict
	}
	return nil
}

func siblingAvailable(ctx context.Context, db database, parent, name, except string) error {
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM nodes WHERE parent_id=? AND name_key=? AND id<>? AND trashed_at IS NULL", parent, strings.ToLower(name), except).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return domain.ErrConflict
	}
	return nil
}

func insertNode(ctx context.Context, db database, n domain.Node) error {
	_, err := db.ExecContext(ctx, "INSERT INTO nodes(id,parent_id,name,name_key,kind,size,mime_type,sha256,favorite,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)", n.ID, n.ParentID, n.Name, strings.ToLower(n.Name), n.Kind, n.Size, n.MimeType, n.SHA256, n.Favorite, stamp(n.CreatedAt), stamp(n.UpdatedAt))
	return databaseError(err)
}

func (s *Server) getNode(w http.ResponseWriter, r *http.Request) {
	n, err := nodeFrom(r.Context(), s.cfg.DB, chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, 200, n)
}

type crumb struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (s *Server) breadcrumbs(ctx context.Context, parent string) ([]crumb, error) {
	trail := []crumb{}
	seen := map[string]bool{}
	for parent != "" {
		if seen[parent] || len(seen) >= 512 {
			return nil, domain.ErrInvalid
		}
		seen[parent] = true
		n, err := nodeFrom(ctx, s.cfg.DB, parent)
		if err != nil {
			return nil, err
		}
		if n.Kind != "folder" || n.TrashedAt != nil {
			return nil, domain.ErrNotFound
		}
		trail = append(trail, crumb{n.ID, n.Name})
		parent = n.ParentID
	}
	out := []crumb{{"", "My drive"}}
	for i := len(trail) - 1; i >= 0; i-- {
		out = append(out, trail[i])
	}
	return out, nil
}

func (s *Server) listNodes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	view, query, parent := q.Get("view"), q.Get("q"), q.Get("parent_id")
	if len(query) > 512 {
		respondError(w, domain.ErrInvalid)
		return
	}
	where, args := "trashed_at IS NULL", []any{}
	crumbs := []crumb{{"", "My drive"}}
	switch view {
	case "", "all":
		if query == "" {
			var err error
			crumbs, err = s.breadcrumbs(r.Context(), parent)
			if err != nil {
				respondError(w, err)
				return
			}
			where += " AND parent_id=?"
			args = append(args, parent)
		}
	case "favorites":
		where += " AND favorite=1"
	case "recent":
		where += " AND kind='file'"
	case "trash":
		where = "trashed_at IS NOT NULL AND (parent_id='' OR parent_id NOT IN (SELECT id FROM nodes WHERE trashed_at IS NOT NULL))"
	default:
		respondError(w, domain.ErrInvalid)
		return
	}
	if query != "" {
		where += ` AND name LIKE ? ESCAPE '\'`
		query = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
		args = append(args, "%"+query+"%")
	}
	if kind := q.Get("type"); kind != "" {
		switch kind {
		case "folder", "image", "video", "audio", "document", "archive", "other":
		default:
			respondError(w, domain.ErrInvalid)
			return
		}
		where += " AND (" + nodeTypeSQL + ")=?"
		args = append(args, kind)
	}
	sort := q.Get("sort")
	if sort == "" {
		sort = "name"
		if view == "recent" {
			sort = "updated_at"
		}
	}
	if sort != "name" && sort != "size" && sort != "updated_at" {
		respondError(w, domain.ErrInvalid)
		return
	}
	direction := q.Get("direction")
	if direction == "" {
		direction = "asc"
		if view == "recent" {
			direction = "desc"
		}
	}
	if direction != "asc" && direction != "desc" {
		respondError(w, domain.ErrInvalid)
		return
	}
	limit, offset := 10000, 0
	var err error
	if raw := q.Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 50000 {
			respondError(w, domain.ErrInvalid)
			return
		}
	}
	if raw := q.Get("offset"); raw != "" {
		offset, err = strconv.Atoi(raw)
		if err != nil || offset < 0 {
			respondError(w, domain.ErrInvalid)
			return
		}
	}
	var total int
	if err := s.cfg.DB.QueryRowContext(r.Context(), "SELECT count(*) FROM nodes WHERE "+where, args...).Scan(&total); err != nil {
		respondError(w, err)
		return
	}
	order := sort + " " + direction + ",id ASC"
	if sort == "name" {
		order = "name_key " + direction + ",id ASC"
	}
	args = append(args, limit, offset)
	rows, err := s.cfg.DB.QueryContext(r.Context(), "SELECT "+nodeColumns+" FROM nodes WHERE "+where+" ORDER BY kind DESC,"+order+" LIMIT ? OFFSET ?", args...)
	if err != nil {
		respondError(w, err)
		return
	}
	items := []domain.Node{}
	for rows.Next() {
		n, scanErr := scanNode(rows)
		if scanErr != nil {
			err = scanErr
			break
		}
		items = append(items, n)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "breadcrumbs": crumbs, "total": total})
}

func (s *Server) createFolder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string `json:"name"`
		ParentID string `json:"parent_id"`
	}
	if err := readJSON(w, r, &body); err != nil {
		respondError(w, err)
		return
	}
	if err := validateName(body.Name); err != nil {
		respondError(w, err)
		return
	}
	s.mutate.Lock()
	defer s.mutate.Unlock()
	if err := validParent(r.Context(), s.cfg.DB, body.ParentID); err != nil {
		respondError(w, err)
		return
	}
	now := time.Now().UTC()
	n := domain.Node{ID: domain.NewID(), ParentID: body.ParentID, Name: body.Name, Kind: "folder", CreatedAt: now, UpdatedAt: now}
	if err := insertNode(r.Context(), s.cfg.DB, n); err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, 201, n)
}

type patch struct {
	Name     *string `json:"name"`
	ParentID *string `json:"parent_id"`
	Favorite *bool   `json:"favorite"`
}

func (s *Server) updateNode(ctx context.Context, id string, body patch) (domain.Node, error) {
	n, err := nodeFrom(ctx, s.cfg.DB, id)
	if err != nil {
		return n, err
	}
	if n.TrashedAt != nil {
		return n, domain.ErrConflict
	}
	if body.Name != nil {
		if err := validateName(*body.Name); err != nil {
			return n, err
		}
		n.Name = *body.Name
	}
	if body.ParentID != nil {
		if err := validParent(ctx, s.cfg.DB, *body.ParentID); err != nil {
			return n, err
		}
		if n.Kind == "folder" && *body.ParentID != "" {
			var exists int
			if err := s.cfg.DB.QueryRowContext(ctx, subtreeSQL+"SELECT count(*) FROM subtree WHERE id=?", n.ID, *body.ParentID).Scan(&exists); err != nil {
				return n, err
			}
			if exists != 0 {
				return n, domain.ErrConflict
			}
		}
		n.ParentID = *body.ParentID
	}
	if body.Favorite != nil {
		n.Favorite = *body.Favorite
	}
	n.UpdatedAt = time.Now().UTC()
	_, err = s.cfg.DB.ExecContext(ctx, "UPDATE nodes SET parent_id=?,name=?,name_key=?,favorite=?,updated_at=? WHERE id=?", n.ParentID, n.Name, strings.ToLower(n.Name), n.Favorite, stamp(n.UpdatedAt), id)
	return n, databaseError(err)
}

func (s *Server) patchNode(w http.ResponseWriter, r *http.Request) {
	var body patch
	if err := readJSON(w, r, &body); err != nil {
		respondError(w, err)
		return
	}
	if body.Name == nil && body.ParentID == nil && body.Favorite == nil {
		respondError(w, domain.ErrInvalid)
		return
	}
	s.mutate.Lock()
	defer s.mutate.Unlock()
	n, err := s.updateNode(r.Context(), chi.URLParam(r, "id"), body)
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, 200, n)
}

func (s *Server) trash(ctx context.Context, id string) error {
	n, err := nodeFrom(ctx, s.cfg.DB, id)
	if err != nil {
		return err
	}
	if n.TrashedAt != nil {
		return nil
	}
	_, err = s.cfg.DB.ExecContext(ctx, subtreeSQL+"UPDATE nodes SET trashed_at=?,trash_root=?,updated_at=? WHERE id IN (SELECT id FROM subtree) AND trashed_at IS NULL", id, stamp(time.Now()), id, stamp(time.Now()))
	return err
}

func (s *Server) trashNode(w http.ResponseWriter, r *http.Request) {
	s.mutate.Lock()
	defer s.mutate.Unlock()
	if err := s.trash(r.Context(), chi.URLParam(r, "id")); err != nil {
		respondError(w, err)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) restore(ctx context.Context, id string) (domain.Node, error) {
	var pending int
	if err := s.cfg.DB.QueryRowContext(ctx, subtreeSQL+"SELECT count(*) FROM pending_deletions WHERE node_id IN (SELECT id FROM subtree)", id).Scan(&pending); err != nil {
		return domain.Node{}, err
	}
	if pending != 0 {
		return domain.Node{}, errDeletionStarted
	}
	tx, err := s.cfg.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Node{}, err
	}
	defer tx.Rollback()
	n, err := nodeFrom(ctx, tx, id)
	if err != nil {
		return n, err
	}
	if n.TrashedAt == nil {
		return n, nil
	}
	var root string
	if err = tx.QueryRowContext(ctx, "SELECT coalesce(trash_root,id) FROM nodes WHERE id=?", id).Scan(&root); err != nil {
		return n, err
	}
	if err = validParent(ctx, tx, n.ParentID); err != nil {
		if !errors.Is(err, domain.ErrNotFound) && !errors.Is(err, domain.ErrConflict) {
			return n, err
		}
		n.ParentID = ""
	}
	if _, err = tx.ExecContext(ctx, "UPDATE nodes SET parent_id=? WHERE id=?", n.ParentID, id); err != nil {
		return n, err
	}
	_, err = tx.ExecContext(ctx, subtreeSQL+"UPDATE nodes SET trashed_at=NULL,trash_root=NULL,updated_at=? WHERE id IN (SELECT id FROM subtree) AND trash_root=?", id, stamp(time.Now()), root)
	if err != nil {
		return n, databaseError(err)
	}
	n, err = nodeFrom(ctx, tx, id)
	if err != nil {
		return n, err
	}
	if err = tx.Commit(); err != nil {
		return n, databaseError(err)
	}
	return n, nil
}

func (s *Server) restoreNode(w http.ResponseWriter, r *http.Request) {
	s.mutate.Lock()
	defer s.mutate.Unlock()
	n, err := s.restore(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, 200, n)
}

func (s *Server) permanent(ctx context.Context, id string) error {
	n, err := nodeFrom(ctx, s.cfg.DB, id)
	if err != nil {
		return err
	}
	if n.TrashedAt == nil {
		return domain.ErrConflict
	}
	rows, err := s.cfg.DB.QueryContext(ctx, subtreeSQL+"SELECT id,kind,trashed_at FROM nodes WHERE id IN (SELECT id FROM subtree)", id)
	if err != nil {
		return err
	}
	files := []string{}
	for rows.Next() {
		var item, kind string
		var trash sql.NullString
		if err = rows.Scan(&item, &kind, &trash); err != nil {
			break
		}
		if !trash.Valid {
			err = domain.ErrConflict
			break
		}
		if kind == "file" {
			files = append(files, item)
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	// Commit intent before deleting any bytes. A failed delete cannot become recoverable.
	if _, err = s.cfg.DB.ExecContext(ctx, subtreeSQL+"INSERT OR IGNORE INTO pending_deletions(node_id) SELECT id FROM subtree", id); err != nil {
		return err
	}
	for _, file := range files {
		if err = s.cfg.Storage.DeleteFile(ctx, file); err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
	}
	_, err = s.cfg.DB.ExecContext(ctx, subtreeSQL+"DELETE FROM nodes WHERE id IN (SELECT id FROM subtree)", id)
	return err
}

func (s *Server) permanentNode(w http.ResponseWriter, r *http.Request) {
	s.mutate.Lock()
	defer s.mutate.Unlock()
	if err := s.permanent(r.Context(), chi.URLParam(r, "id")); err != nil {
		respondError(w, err)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) bulkNodes(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs      []string `json:"ids"`
		Action   string   `json:"action"`
		ParentID *string  `json:"parent_id"`
	}
	if err := readJSON(w, r, &body); err != nil {
		respondError(w, err)
		return
	}
	if len(body.IDs) == 0 || len(body.IDs) > 100 {
		respondError(w, domain.ErrInvalid)
		return
	}
	if body.Action != "trash" && body.Action != "restore" && body.Action != "favorite" && body.Action != "unfavorite" && body.Action != "move" || body.Action == "move" && body.ParentID == nil {
		respondError(w, domain.ErrInvalid)
		return
	}
	type itemError struct {
		ID    string `json:"id"`
		Error string `json:"error"`
	}
	completed, failures := []string{}, []itemError{}
	s.mutate.Lock()
	defer s.mutate.Unlock()
	for _, id := range body.IDs {
		var err error
		switch body.Action {
		case "trash":
			err = s.trash(r.Context(), id)
		case "restore":
			_, err = s.restore(r.Context(), id)
		case "move":
			_, err = s.updateNode(r.Context(), id, patch{ParentID: body.ParentID})
		default:
			favorite := body.Action == "favorite"
			_, err = s.updateNode(r.Context(), id, patch{Favorite: &favorite})
		}
		if err != nil {
			message := "Operation failed"
			if errors.Is(err, domain.ErrNotFound) {
				message = "Resource not found"
			}
			if errors.Is(err, domain.ErrConflict) {
				message = "Name or folder state conflicts"
			}
			if errors.Is(err, errDeletionStarted) {
				message = errDeletionStarted.Error()
			}
			failures = append(failures, itemError{id, message})
		} else {
			completed = append(completed, id)
		}
	}
	writeJSON(w, 200, map[string]any{"completed": completed, "errors": failures})
}
