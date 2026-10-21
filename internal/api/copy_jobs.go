package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"private-drive/internal/domain"
)

const maxCopyEntries = 10000
const maxCopyDepth = 512

type copyRequest struct {
	OperationID string   `json:"operation_id"`
	SourceIDs   []string `json:"source_ids"`
	ParentID    string   `json:"parent_id"`
}

type copyJob struct {
	ID               string   `json:"id"`
	Status           string   `json:"status"`
	TotalEntries     int      `json:"total_entries"`
	CompletedEntries int      `json:"completed_entries"`
	TotalBytes       int64    `json:"total_bytes"`
	CompletedBytes   int64    `json:"completed_bytes"`
	ErrorCode        string   `json:"error_code"`
	CreatedAt        string   `json:"created_at"`
	UpdatedAt        string   `json:"updated_at"`
	ResultIDs        []string `json:"result_ids"`
}

type copyEntry struct {
	Index    int
	Parent   int
	Source   domain.Node
	TargetID string
	Name     string
	State    string
	Copied   int64
}

const copyJobsSchema = `
CREATE TABLE IF NOT EXISTS copy_jobs (
 id TEXT PRIMARY KEY, request TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('queued','running','failed','cancelling','cancelled','complete')),
 error_code TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS copy_entries (
 job_id TEXT NOT NULL REFERENCES copy_jobs(id) ON DELETE CASCADE, idx INTEGER NOT NULL,
 parent_idx INTEGER NOT NULL, source TEXT NOT NULL, target_id TEXT NOT NULL,
 name TEXT NOT NULL, kind TEXT NOT NULL, size INTEGER NOT NULL CHECK(size>=0),
 state TEXT NOT NULL CHECK(state IN ('pending','ready')), copied_bytes INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(job_id,idx)
);
CREATE INDEX IF NOT EXISTS copy_pending ON copy_entries(job_id,state,idx);
CREATE INDEX IF NOT EXISTS storage_uploads_parent ON storage_uploads(parent_id);
`

func copyID(id string) bool {
	return len(id) == 32 && strings.Trim(id, "0123456789abcdef") == ""
}

func normalizeCopyRequest(spec *copyRequest) error {
	if !copyID(spec.OperationID) || len(spec.SourceIDs) == 0 || len(spec.SourceIDs) > 100 || spec.ParentID != "" && !copyID(spec.ParentID) {
		return domain.ErrInvalid
	}
	for _, id := range spec.SourceIDs {
		if !copyID(id) {
			return domain.ErrInvalid
		}
	}
	sort.Strings(spec.SourceIDs)
	ids := spec.SourceIDs[:0]
	for _, id := range spec.SourceIDs {
		if len(ids) == 0 || ids[len(ids)-1] != id {
			ids = append(ids, id)
		}
	}
	spec.SourceIDs = ids
	return nil
}

func (s *Server) createCopyJob(w http.ResponseWriter, r *http.Request) {
	var spec copyRequest
	if err := readJSON(w, r, &spec); err != nil {
		respondError(w, err)
		return
	}
	job, existed, err := s.planCopy(r.Context(), spec)
	if err != nil {
		respondError(w, err)
		return
	}
	status := http.StatusCreated
	if existed {
		status = http.StatusOK
	}
	writeJSON(w, status, job)
}

func (s *Server) planCopy(ctx context.Context, spec copyRequest) (copyJob, bool, error) {
	if err := normalizeCopyRequest(&spec); err != nil {
		return copyJob{}, false, err
	}
	encoded, _ := json.Marshal(spec)
	s.mutate.Lock()
	defer s.mutate.Unlock()
	var old string
	err := s.cfg.DB.QueryRowContext(ctx, "SELECT request FROM copy_jobs WHERE id=?", spec.OperationID).Scan(&old)
	if err == nil {
		if old != string(encoded) {
			return copyJob{}, true, domain.ErrConflict
		}
		job, err := s.copyJob(ctx, spec.OperationID)
		return job, true, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return copyJob{}, false, err
	}
	var pending int
	if err = s.cfg.DB.QueryRowContext(ctx, "SELECT count(*) FROM copy_jobs WHERE status NOT IN ('complete','cancelled')").Scan(&pending); err != nil {
		return copyJob{}, false, err
	}
	if pending >= 8 {
		return copyJob{}, false, domain.ErrConflict
	}
	tx, err := s.cfg.DB.BeginTx(ctx, nil)
	if err != nil {
		return copyJob{}, false, err
	}
	defer tx.Rollback()
	if err = validParent(ctx, tx, spec.ParentID); err != nil {
		return copyJob{}, false, err
	}
	destinationDepth, err := copyDestinationDepth(ctx, tx, spec.ParentID)
	if err != nil {
		return copyJob{}, false, err
	}
	selected := make(map[string]bool, len(spec.SourceIDs))
	for _, id := range spec.SourceIDs {
		selected[id] = true
	}
	entries := []copyEntry{}
	reserved := map[string]bool{}
	for _, id := range spec.SourceIDs {
		n, err := nodeFrom(ctx, tx, id)
		if err != nil {
			return copyJob{}, false, err
		}
		if n.TrashedAt != nil {
			return copyJob{}, false, domain.ErrConflict
		}
		parent, overlap, seen := n.ParentID, false, map[string]bool{id: true}
		for parent != "" {
			if seen[parent] || len(seen) >= maxCopyDepth {
				return copyJob{}, false, domain.ErrInvalid
			}
			seen[parent] = true
			if selected[parent] {
				overlap = true
			}
			p, err := nodeFrom(ctx, tx, parent)
			if err != nil {
				return copyJob{}, false, err
			}
			if p.Kind != "folder" || p.TrashedAt != nil {
				return copyJob{}, false, domain.ErrConflict
			}
			parent = p.ParentID
		}
		if overlap {
			continue
		}
		name, err := copyRootName(ctx, tx, spec.ParentID, n.Name, reserved)
		if err != nil {
			return copyJob{}, false, err
		}
		entries = append(entries, newCopyEntry(len(entries), -1, n, name))
	}
	// Breadth-first planning keeps parent mappings stable and bounds every query.
	depths := make([]int, len(entries))
	visited := map[string]bool{}
	for index := 0; index < len(entries); index++ {
		entry := entries[index]
		if err := validateName(entry.Name); err != nil {
			return copyJob{}, false, domain.ErrIntegrity
		}
		if destinationDepth+depths[index] >= maxCopyDepth {
			return copyJob{}, false, domain.ErrInvalid
		}
		if visited[entry.Source.ID] || len(entries) > maxCopyEntries {
			return copyJob{}, false, domain.ErrInvalid
		}
		visited[entry.Source.ID] = true
		if entry.Source.Kind != "folder" {
			if entry.Source.Size < 0 || entry.Source.Size > 1<<40 || !validDigest(entry.Source.SHA256) || entry.Source.SHA256 == "" {
				return copyJob{}, false, domain.ErrIntegrity
			}
			continue
		}
		rows, err := tx.QueryContext(ctx, "SELECT "+nodeColumns+" FROM nodes WHERE parent_id=? AND trashed_at IS NULL ORDER BY name_key,id LIMIT ?", entry.Source.ID, maxCopyEntries-len(entries)+1)
		if err != nil {
			return copyJob{}, false, err
		}
		for rows.Next() {
			n, scanErr := scanNode(rows)
			if scanErr != nil {
				err = scanErr
				break
			}
			if len(entries) == maxCopyEntries || depths[index]+1 >= maxCopyDepth {
				err = domain.ErrInvalid
				break
			}
			entries = append(entries, newCopyEntry(len(entries), index, n, n.Name))
			depths = append(depths, depths[index]+1)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return copyJob{}, false, err
		}
	}
	now := stamp(time.Now())
	if _, err = tx.ExecContext(ctx, "INSERT INTO copy_jobs(id,request,status,created_at,updated_at) VALUES(?,?,'queued',?,?)", spec.OperationID, string(encoded), now, now); err != nil {
		return copyJob{}, false, err
	}
	for _, entry := range entries {
		source, _ := json.Marshal(entry.Source)
		if _, err = tx.ExecContext(ctx, "INSERT INTO copy_entries(job_id,idx,parent_idx,source,target_id,name,kind,size,state) VALUES(?,?,?,?,?,?,?,?,?)", spec.OperationID, entry.Index, entry.Parent, string(source), entry.TargetID, entry.Name, entry.Source.Kind, entry.Source.Size, entry.State); err != nil {
			return copyJob{}, false, err
		}
	}
	if err = tx.Commit(); err != nil {
		return copyJob{}, false, err
	}
	job, err := s.copyJob(ctx, spec.OperationID)
	return job, false, err
}

func copyDestinationDepth(ctx context.Context, db database, parent string) (int, error) {
	depth := 0
	seen := map[string]bool{}
	for parent != "" {
		if seen[parent] || depth >= maxCopyDepth {
			return 0, domain.ErrInvalid
		}
		seen[parent] = true
		depth++
		n, err := nodeFrom(ctx, db, parent)
		if err != nil {
			return 0, err
		}
		if n.Kind != "folder" || n.TrashedAt != nil {
			return 0, domain.ErrConflict
		}
		parent = n.ParentID
	}
	return depth, nil
}

func newCopyEntry(index, parent int, source domain.Node, name string) copyEntry {
	entry := copyEntry{Index: index, Parent: parent, Source: source, Name: name, State: "pending"}
	if source.Kind == "folder" {
		entry.State, entry.TargetID = "ready", domain.NewID()
	}
	return entry
}

func copyRootName(ctx context.Context, db database, parent, name string, reserved map[string]bool) (string, error) {
	for attempt := 0; attempt <= maxCopyEntries; attempt++ {
		candidate := name
		if attempt > 0 {
			suffix := " copy"
			if attempt > 1 {
				suffix += " " + strconv.Itoa(attempt)
			}
			base := name
			for len(base)+len(suffix) > 255 {
				_, width := utf8.DecodeLastRuneInString(base)
				base = base[:len(base)-width]
			}
			candidate = strings.TrimSpace(base) + suffix
		}
		if reserved[strings.ToLower(candidate)] {
			continue
		}
		if err := siblingAvailable(ctx, db, parent, candidate, ""); err != nil {
			if errors.Is(err, domain.ErrConflict) {
				continue
			}
			return "", err
		}
		reserved[strings.ToLower(candidate)] = true
		return candidate, nil
	}
	return "", domain.ErrConflict
}

func (s *Server) copyJob(ctx context.Context, id string) (copyJob, error) {
	job := copyJob{ResultIDs: []string{}}
	if err := s.cfg.DB.QueryRowContext(ctx, "SELECT id,status,error_code,created_at,updated_at FROM copy_jobs WHERE id=?", id).Scan(&job.ID, &job.Status, &job.ErrorCode, &job.CreatedAt, &job.UpdatedAt); err != nil {
		return job, databaseError(err)
	}
	if err := s.cfg.DB.QueryRowContext(ctx, "SELECT count(*),coalesce(sum(state='ready'),0),coalesce(sum(size),0),coalesce(sum(copied_bytes),0) FROM copy_entries WHERE job_id=?", id).Scan(&job.TotalEntries, &job.CompletedEntries, &job.TotalBytes, &job.CompletedBytes); err != nil {
		return job, err
	}
	if job.Status == "complete" {
		rows, err := s.cfg.DB.QueryContext(ctx, "SELECT target_id FROM copy_entries WHERE job_id=? AND parent_idx=-1 ORDER BY idx", id)
		if err != nil {
			return job, err
		}
		defer rows.Close()
		for rows.Next() {
			var target string
			if err := rows.Scan(&target); err != nil {
				return job, err
			}
			job.ResultIDs = append(job.ResultIDs, target)
		}
		return job, rows.Err()
	}
	return job, nil
}

func (s *Server) getCopyJob(w http.ResponseWriter, r *http.Request) {
	job, err := s.copyJob(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, 200, job)
}

func (s *Server) listCopyJobs(w http.ResponseWriter, r *http.Request) {
	rows, err := s.cfg.DB.QueryContext(r.Context(), "SELECT id FROM copy_jobs ORDER BY (status IN ('complete','cancelled')),created_at DESC LIMIT 100")
	if err != nil {
		respondError(w, err)
		return
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		respondError(w, err)
		return
	}
	jobs := []copyJob{}
	for _, id := range ids {
		job, err := s.copyJob(r.Context(), id)
		if err != nil {
			respondError(w, err)
			return
		}
		jobs = append(jobs, job)
	}
	writeJSON(w, 200, map[string]any{"items": jobs})
}

func (s *Server) retryCopyJob(w http.ResponseWriter, r *http.Request) {
	s.mutate.Lock()
	defer s.mutate.Unlock()
	id := chi.URLParam(r, "id")
	job, err := s.copyJob(r.Context(), id)
	if err == nil && job.Status != "failed" {
		err = domain.ErrConflict
	}
	if err == nil {
		_, err = s.cfg.DB.ExecContext(r.Context(), "UPDATE copy_jobs SET status=CASE WHEN error_code='cleanup_failed' THEN 'cancelling' ELSE 'queued' END,error_code='',updated_at=? WHERE id=? AND status='failed'", stamp(time.Now()), id)
	}
	if err != nil {
		respondError(w, err)
		return
	}
	job, err = s.copyJob(r.Context(), id)
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, 200, job)
}

func (s *Server) cancelCopyJob(w http.ResponseWriter, r *http.Request) {
	s.mutate.Lock()
	defer s.mutate.Unlock()
	id := chi.URLParam(r, "id")
	job, err := s.copyJob(r.Context(), id)
	if err == nil && job.Status == "complete" {
		err = domain.ErrConflict
	}
	if err == nil && job.Status != "cancelled" {
		_, err = s.cfg.DB.ExecContext(r.Context(), "UPDATE copy_jobs SET status='cancelling',error_code='',updated_at=? WHERE id=?", stamp(time.Now()), id)
	}
	if err != nil {
		respondError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RunCopyJobs owns one serial worker. Caller must cancel and join it before closing storage.
func (s *Server) RunCopyJobs(ctx context.Context) error {
	if !s.copyWorker.TryLock() {
		return errors.New("copy worker already running")
	}
	defer s.copyWorker.Unlock()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for ctx.Err() == nil {
		var id string
		err := s.cfg.DB.QueryRowContext(ctx, "SELECT id FROM copy_jobs WHERE status IN ('queued','running','cancelling') ORDER BY created_at,id LIMIT 1").Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			select {
			case <-ctx.Done():
			case <-ticker.C:
			}
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if err = s.copyStep(ctx, id); err != nil && ctx.Err() == nil {
			if _, err := s.cfg.DB.ExecContext(ctx, "UPDATE copy_jobs SET error_code=CASE WHEN status='cancelling' THEN 'cleanup_failed' ELSE ? END,status='failed',updated_at=? WHERE id=? AND status NOT IN ('complete','cancelled')", copyErrorCode(err), stamp(time.Now()), id); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyErrorCode(err error) string {
	switch {
	case errors.Is(err, domain.ErrIntegrity):
		return "integrity_failed"
	case errors.Is(err, domain.ErrNotFound):
		return "source_missing"
	case errors.Is(err, domain.ErrConflict):
		return "destination_conflict"
	default:
		return "storage_unavailable"
	}
}

func (s *Server) copyStep(ctx context.Context, id string) error {
	job, err := s.copyJob(ctx, id)
	if err != nil {
		return err
	}
	if job.Status == "cancelling" {
		return s.cleanupCopy(ctx, id)
	}
	if job.Status != "queued" && job.Status != "running" {
		return nil
	}
	if _, err = s.cfg.DB.ExecContext(ctx, "UPDATE copy_jobs SET status='running',updated_at=? WHERE id=? AND status='queued'", stamp(time.Now()), id); err != nil {
		return err
	}
	var entry copyEntry
	var source string
	err = s.cfg.DB.QueryRowContext(ctx, "SELECT idx,parent_idx,source,target_id,name,state,copied_bytes FROM copy_entries WHERE job_id=? AND state='pending' ORDER BY idx LIMIT 1", id).Scan(&entry.Index, &entry.Parent, &source, &entry.TargetID, &entry.Name, &entry.State, &entry.Copied)
	if errors.Is(err, sql.ErrNoRows) {
		return s.publishCopy(ctx, id)
	}
	if err != nil {
		return err
	}
	if err = json.Unmarshal([]byte(source), &entry.Source); err != nil {
		return domain.ErrIntegrity
	}
	marker := "copy:" + id + ":" + strconv.Itoa(entry.Index)
	if entry.TargetID == "" {
		// Reconcile a crash between Begin and recording its randomly generated ID.
		rows, err := s.cfg.DB.QueryContext(ctx, "SELECT id FROM storage_uploads WHERE parent_id=? LIMIT 2", marker)
		if err != nil {
			return err
		}
		for rows.Next() {
			var uploadID string
			if err = rows.Scan(&uploadID); err != nil {
				break
			}
			if entry.TargetID != "" {
				err = domain.ErrIntegrity
				break
			}
			entry.TargetID = uploadID
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return err
		}
		if entry.TargetID == "" {
			upload, err := s.cfg.Storage.Begin(ctx, domain.UploadSpec{Name: entry.Name, ParentID: marker, Size: entry.Source.Size, MimeType: entry.Source.MimeType, SHA256: entry.Source.SHA256})
			if err != nil {
				return err
			}
			entry.TargetID = upload.ID
		}
		if _, err = s.cfg.DB.ExecContext(ctx, "UPDATE copy_entries SET target_id=? WHERE job_id=? AND idx=?", entry.TargetID, id, entry.Index); err != nil {
			return err
		}
	}
	upload, err := s.cfg.Storage.Status(ctx, entry.TargetID)
	if err != nil {
		return err
	}
	if upload.ParentID != marker || upload.Name != entry.Name || upload.Size != entry.Source.Size || upload.SHA256 != entry.Source.SHA256 {
		return domain.ErrIntegrity
	}
	if upload.Status != "complete" && len(upload.ReceivedChunks) < upload.TotalChunks {
		index := 0
		for _, received := range upload.ReceivedChunks {
			if received != index {
				break
			}
			index++
		}
		reader, manifest, err := s.cfg.Storage.Open(ctx, entry.Source.ID)
		if err != nil {
			return err
		}
		if manifest.Size != entry.Source.Size || manifest.SHA256 != entry.Source.SHA256 {
			reader.Close()
			return domain.ErrIntegrity
		}
		offset := int64(index) * upload.ChunkSize
		_, err = reader.Seek(offset, io.SeekStart)
		if err == nil {
			// Engine consumes at most one application chunk plus an overflow sentinel.
			upload, err = s.cfg.Storage.PutChunk(ctx, upload.ID, index, io.LimitReader(reader, min(upload.ChunkSize, upload.Size-offset)), "")
		}
		reader.Close()
		if err != nil {
			return err
		}
		_, err = s.cfg.DB.ExecContext(ctx, "UPDATE copy_entries SET copied_bytes=? WHERE job_id=? AND idx=?", upload.ReceivedBytes, id, entry.Index)
		if err != nil {
			return err
		}
		_, err = s.cfg.DB.ExecContext(ctx, "UPDATE copy_jobs SET updated_at=? WHERE id=?", stamp(time.Now()), id)
		return err
	}
	manifest, err := s.cfg.Storage.Complete(ctx, entry.TargetID)
	if err != nil {
		return err
	}
	if manifest.ID != entry.TargetID || manifest.Size != entry.Source.Size || manifest.SHA256 != entry.Source.SHA256 {
		return domain.ErrIntegrity
	}
	_, err = s.cfg.DB.ExecContext(ctx, "UPDATE copy_entries SET state='ready',copied_bytes=? WHERE job_id=? AND idx=?", entry.Source.Size, id, entry.Index)
	return err
}

func (s *Server) publishCopy(ctx context.Context, id string) error {
	s.mutate.Lock()
	defer s.mutate.Unlock()
	tx, err := s.cfg.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status, request string
	if err = tx.QueryRowContext(ctx, "SELECT status,request FROM copy_jobs WHERE id=?", id).Scan(&status, &request); err != nil {
		return err
	}
	if status != "running" && status != "queued" {
		return nil
	}
	var spec copyRequest
	if err = json.Unmarshal([]byte(request), &spec); err != nil {
		return domain.ErrIntegrity
	}
	if err = validParent(ctx, tx, spec.ParentID); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, "SELECT idx,parent_idx,source,target_id,name,state FROM copy_entries WHERE job_id=? ORDER BY idx", id)
	if err != nil {
		return err
	}
	entries := []copyEntry{}
	for rows.Next() {
		var entry copyEntry
		var source string
		if err = rows.Scan(&entry.Index, &entry.Parent, &source, &entry.TargetID, &entry.Name, &entry.State); err != nil {
			break
		}
		if err = json.Unmarshal([]byte(source), &entry.Source); err != nil {
			break
		}
		if entry.State != "ready" || !copyID(entry.TargetID) || entry.Index != len(entries) || entry.Parent >= entry.Index || entry.Parent < -1 {
			err = domain.ErrIntegrity
			break
		}
		entries = append(entries, entry)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	if len(entries) == 0 || len(entries) > maxCopyEntries {
		return domain.ErrIntegrity
	}
	destinationDepth, err := copyDestinationDepth(ctx, tx, spec.ParentID)
	if err != nil {
		return err
	}
	depths := make([]int, len(entries))
	for i, entry := range entries {
		if entry.Parent != -1 {
			depths[i] = depths[entry.Parent] + 1
		}
		if destinationDepth+depths[i] >= maxCopyDepth {
			return domain.ErrInvalid
		}
	}
	now := time.Now().UTC()
	for _, entry := range entries {
		if entry.Source.Kind == "file" {
			var complete int
			if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM storage_uploads WHERE id=? AND status='complete' AND size=? AND sha256=? AND parent_id=?", entry.TargetID, entry.Source.Size, entry.Source.SHA256, "copy:"+id+":"+strconv.Itoa(entry.Index)).Scan(&complete); err != nil {
				return err
			}
			if complete != 1 {
				return domain.ErrIntegrity
			}
		}
		node := entry.Source
		node.ID, node.Name = entry.TargetID, entry.Name
		node.ParentID = spec.ParentID
		if entry.Parent != -1 {
			node.ParentID = entries[entry.Parent].TargetID
		}
		node.CreatedAt, node.UpdatedAt = now, now
		if err = insertNode(ctx, tx, node); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE copy_jobs SET status='complete',error_code='',updated_at=? WHERE id=?", stamp(now), id); err != nil {
		return err
	}
	return databaseError(tx.Commit())
}

func (s *Server) cleanupCopy(ctx context.Context, id string) error {
	// Parent marker also discovers Begin calls not yet attached to their plan entry.
	uploads, err := s.cfg.Storage.List(ctx)
	if err != nil {
		return err
	}
	for _, upload := range uploads {
		if !strings.HasPrefix(upload.ParentID, "copy:"+id+":") {
			continue
		}
		if _, err := nodeFrom(ctx, s.cfg.DB, upload.ID); !errors.Is(err, domain.ErrNotFound) {
			if err == nil {
				return domain.ErrConflict
			}
			return err
		}
		if upload.Status == "complete" {
			err = s.cfg.Storage.DeleteFile(ctx, upload.ID)
		} else {
			err = s.cfg.Storage.Cancel(ctx, upload.ID)
		}
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
	}
	_, err = s.cfg.DB.ExecContext(ctx, "UPDATE copy_jobs SET status='cancelled',error_code='',updated_at=? WHERE id=? AND status='cancelling'", stamp(time.Now()), id)
	return err
}
