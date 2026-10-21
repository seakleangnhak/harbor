package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"

	"private-drive/internal/domain"
)

type IntegrityIssue struct {
	Code     string `json:"code"`
	ID       string `json:"id"`
	Severity string `json:"severity"`
}

type IntegrityReport struct {
	Nodes          int              `json:"nodes"`
	Files          int              `json:"files"`
	Uploads        int              `json:"uploads"`
	Chunks         int              `json:"chunks"`
	VerifiedFiles  int              `json:"verified_files"`
	ContentChecked bool             `json:"content_checked"`
	Issues         []IntegrityIssue `json:"issues"`
	Suppressed     int              `json:"suppressed_issues"`
}

func (report *IntegrityReport) issue(code, id, severity string) {
	if len(report.Issues) < 1000 {
		report.Issues = append(report.Issues, IntegrityIssue{code, id, severity})
	} else {
		report.Suppressed++
	}
}

// CheckIntegrity is read-only. Run offline for a consistent cross-table audit.
// It never probes Telegram or removes orphan candidates.
func CheckIntegrity(ctx context.Context, db *sql.DB) (IntegrityReport, error) {
	report := IntegrityReport{Issues: []IntegrityIssue{}}
	rows, err := db.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return report, err
	}
	for rows.Next() {
		var result string
		if err = rows.Scan(&result); err != nil {
			break
		}
		if result != "ok" {
			report.issue("sqlite_integrity_failed", "", "error")
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return report, err
	}
	rows, err = db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return report, err
	}
	for rows.Next() {
		var table, parent string
		var rowID sql.NullInt64
		var constraint int
		if err = rows.Scan(&table, &rowID, &parent, &constraint); err != nil {
			break
		}
		report.issue("foreign_key_violation", "", "error")
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return report, err
	}

	type node struct {
		id, parent, kind, digest string
		size                     int64
	}
	nodes := map[string]node{}
	rows, err = db.QueryContext(ctx, "SELECT id,parent_id,kind,size,sha256 FROM nodes")
	if err != nil {
		return report, err
	}
	for rows.Next() {
		var n node
		if err = rows.Scan(&n.id, &n.parent, &n.kind, &n.size, &n.digest); err != nil {
			break
		}
		if len(nodes) >= 250000 {
			err = domain.ErrInvalid
			break
		}
		nodes[n.id] = n
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return report, err
	}
	report.Nodes = len(nodes)
	for _, n := range nodes {
		if n.kind == "file" {
			report.Files++
		}
		seen := map[string]bool{n.id: true}
		for parent := n.parent; parent != ""; {
			if seen[parent] || len(seen) > 512 {
				report.issue("folder_cycle_or_depth", n.id, "error")
				break
			}
			seen[parent] = true
			p, ok := nodes[parent]
			if !ok || p.kind != "folder" {
				report.issue("invalid_parent", n.id, "error")
				break
			}
			parent = p.parent
		}
	}
	type upload struct {
		id, parent, digest, status string
		size, chunkSize            int64
	}
	uploads := map[string]upload{}
	rows, err = db.QueryContext(ctx, "SELECT id,parent_id,size,sha256,chunk_size,status FROM storage_uploads")
	if err != nil {
		return report, err
	}
	for rows.Next() {
		var u upload
		if err = rows.Scan(&u.id, &u.parent, &u.size, &u.digest, &u.chunkSize, &u.status); err != nil {
			break
		}
		if len(uploads) >= 250000 {
			err = domain.ErrInvalid
			break
		}
		uploads[u.id] = u
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return report, err
	}
	report.Uploads = len(uploads)
	for _, n := range nodes {
		if n.kind != "file" {
			continue
		}
		u, ok := uploads[n.id]
		if !ok {
			report.issue("missing_manifest", n.id, "error")
			continue
		}
		if u.status != "complete" || n.size != u.size || n.digest != u.digest || !validHash(n.digest) {
			report.issue("manifest_mismatch", n.id, "error")
		}
	}
	for _, u := range uploads {
		if u.size < 0 || u.chunkSize < 1 || u.chunkSize > 64<<20 || u.size/max(u.chunkSize, 1) > 1<<31-2 {
			report.issue("invalid_upload_geometry", u.id, "error")
			continue
		}
		if _, ok := nodes[u.id]; !ok && u.status == "complete" && !strings.HasPrefix(u.parent, "copy:") {
			report.issue("unpublished_manifest", u.id, "warning")
		}
		rows, err := db.QueryContext(ctx, "SELECT idx,size,sha256,ref FROM storage_chunks WHERE upload_id=? ORDER BY idx", u.id)
		if err != nil {
			return report, err
		}
		count := 0
		var bytes int64
		for rows.Next() {
			var index int
			var size int64
			var digest, ref string
			if err = rows.Scan(&index, &size, &digest, &ref); err != nil {
				break
			}
			report.Chunks++
			if index < 0 || int64(index) > u.size/u.chunkSize || size != min(u.chunkSize, u.size-int64(index)*u.chunkSize) || !validHash(digest) || ref == "" || u.status == "complete" && index != count {
				report.issue("invalid_chunk_manifest", u.id, "error")
			}
			count++
			bytes += size
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return report, err
		}
		expected := u.size / u.chunkSize
		if u.size%u.chunkSize != 0 {
			expected++
		}
		if u.status == "complete" && (int64(count) != expected || bytes != u.size || !validHash(u.digest)) {
			report.issue("missing_or_incorrect_chunks", u.id, "error")
		}
	}
	rows, err = db.QueryContext(ctx, "SELECT DISTINCT upload_id FROM storage_chunks WHERE upload_id NOT IN (SELECT id FROM storage_uploads)")
	if err != nil {
		return report, err
	}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		report.issue("orphan_chunk_manifest", id, "error")
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	return report, err
}

// VerifyIntegrity additionally reads every published file through AEAD/SHA verification.
// Reads one file at a time; local and Telegram adapters share the same bounded reader.
func (e *Engine) VerifyIntegrity(ctx context.Context) (IntegrityReport, error) {
	report, err := CheckIntegrity(ctx, e.db)
	if err != nil {
		return report, err
	}
	rows, err := e.db.QueryContext(ctx, "SELECT id,size,sha256 FROM nodes WHERE kind='file' ORDER BY id")
	if err != nil {
		return report, err
	}
	type file struct {
		id, digest string
		size       int64
	}
	files := []file{}
	for rows.Next() {
		var f file
		if err = rows.Scan(&f.id, &f.size, &f.digest); err != nil {
			break
		}
		files = append(files, f)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return report, err
	}
	buffer := make([]byte, 32<<10)
	for _, f := range files {
		if ctx.Err() != nil {
			return report, ctx.Err()
		}
		reader, manifest, err := e.Open(ctx, f.id)
		if err == nil {
			h := sha256.New()
			var size int64
			size, err = io.CopyBuffer(h, reader, buffer)
			reader.Close()
			if err == nil && (size != f.size || manifest.SHA256 != f.digest || hex.EncodeToString(h.Sum(nil)) != f.digest) {
				err = domain.ErrIntegrity
			}
		}
		if ctx.Err() != nil {
			return report, ctx.Err()
		}
		if err != nil {
			code := "content_unavailable"
			if errors.Is(err, domain.ErrIntegrity) {
				code = "content_integrity_failed"
			}
			report.issue(code, f.id, "error")
		} else {
			report.VerifiedFiles++
		}
	}
	report.ContentChecked = true
	return report, nil
}

// OrphanCandidates enumerates local ciphertext only. Offline callers must review
// candidates; a concurrent upload can legitimately have an uncommitted blob.
func (s *LocalStore) OrphanCandidates(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, "SELECT ref FROM storage_chunks")
	if err != nil {
		return nil, err
	}
	references := map[string]bool{}
	for rows.Next() {
		var ref string
		if err = rows.Scan(&ref); err != nil {
			break
		}
		if len(references) >= 1000000 {
			err = domain.ErrInvalid
			break
		}
		references[ref] = true
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, err
	}
	candidates := []string{}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !references[entry.Name()] {
			candidates = append(candidates, entry.Name())
		}
	}
	return candidates, nil
}
