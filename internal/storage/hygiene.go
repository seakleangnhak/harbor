package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"private-drive/internal/domain"
)

const hygieneFileLimit = 100000

// HygieneReport contains opaque local references, never source names, keys or
// Telegram references. Callers must hold the drive's exclusive offline lock.
type HygieneReport struct {
	Integrity            IntegrityReport    `json:"database_integrity"`
	UploadStates         map[string]int     `json:"upload_states"`
	PendingStates        map[string]int     `json:"pending_chunk_states"`
	CopyStates           map[string]int     `json:"copy_job_states"`
	StagedBytes          int64              `json:"staged_bytes"`
	CacheBytes           int64              `json:"cache_bytes"`
	RecoverablePartFiles int                `json:"recoverable_part_files"`
	MissingPartFiles     int                `json:"missing_part_files"`
	UnresolvedPartFiles  int                `json:"unresolved_part_files"`
	LocalBlobCandidates  int                `json:"local_blob_candidates"`
	UnknownFiles         int                `json:"unknown_files"`
	ReclaimableBytes     int64              `json:"reclaimable_bytes"`
	Candidates           []HygieneCandidate `json:"cleanup_candidates"`
	Blocked              []string           `json:"cleanup_blocked"`
	PlanSHA256           string             `json:"plan_sha256"`
	MetadataSHA256       string             `json:"metadata_sha256"`
	RemoteInventory      string             `json:"remote_inventory"`
	RemoteOrphanCount    *int               `json:"remote_orphan_candidates"`
	LastContentCheck     *string            `json:"last_successful_content_integrity_check"`
	AbandonmentPolicy    string             `json:"abandonment_policy"`
	Applied              bool               `json:"applied"`
	RemovedFiles         int                `json:"removed_files"`
	RemovedPartRows      int                `json:"removed_part_rows"`
	RemovedBytes         int64              `json:"removed_bytes"`
}

type HygieneCandidate struct {
	Area       string `json:"area"`
	Ref        string `json:"ref"`
	Reason     string `json:"reason"`
	Bytes      int64  `json:"bytes"`
	Present    bool   `json:"present"`
	Device     uint64 `json:"device"`
	Inode      uint64 `json:"inode"`
	ModifiedNS int64  `json:"modified_ns"`
	UploadID   string `json:"upload_id,omitempty"`
	ChunkIndex int    `json:"chunk_index,omitempty"`
	PartIndex  int    `json:"part_index,omitempty"`
	PartSize   int64  `json:"part_size,omitempty"`
	PartSHA256 string `json:"part_sha256,omitempty"`
}

type hygienePart struct {
	HygieneCandidate
	disposable bool
}

// InspectHygiene never creates directories, initializes storage, probes remote
// objects or mutates SQLite. Unknown/unreferenced data is reported, not deleted.
func InspectHygiene(ctx context.Context, db *sql.DB, dataDir string) (HygieneReport, error) {
	r := HygieneReport{
		UploadStates: map[string]int{}, PendingStates: map[string]int{}, CopyStates: map[string]int{},
		Candidates: []HygieneCandidate{}, Blocked: []string{}, RemoteInventory: "not_enumerated",
		AbandonmentPolicy: "not_inferred_from_age_or_missing_metadata",
	}
	var err error
	r.Integrity, err = CheckIntegrity(ctx, db)
	if err != nil {
		return r, err
	}
	if len(r.Integrity.Issues) > 0 || r.Integrity.Suppressed > 0 {
		r.Blocked = append(r.Blocked, "metadata_consistency_issues")
	}
	for i := range r.Integrity.Issues {
		if !isHexID(r.Integrity.Issues[i].ID) {
			r.Integrity.Issues[i].ID = ""
		}
	}
	tables := map[string]bool{}
	for _, table := range []string{"storage_parts", "storage_pending_chunks", "copy_jobs", "copy_entries"} {
		var count int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&count); err != nil {
			return r, err
		}
		tables[table] = count == 1
		if count != 1 {
			r.Blocked = append(r.Blocked, "maintenance_schema_incomplete")
		}
	}
	for _, state := range []struct {
		table, column string
		counts        map[string]int
	}{
		{"storage_uploads", "status", r.UploadStates},
		{"storage_pending_chunks", "state", r.PendingStates},
		{"copy_jobs", "status", r.CopyStates},
	} {
		if state.table != "storage_uploads" && !tables[state.table] {
			continue
		}
		rows, err := db.QueryContext(ctx, "SELECT "+state.column+",count(*) FROM "+state.table+" GROUP BY "+state.column)
		if err != nil {
			return r, err
		}
		for rows.Next() {
			var name string
			var count int
			if err = rows.Scan(&name, &count); err != nil {
				break
			}
			if !hygieneKnownState(state.table, name) {
				name = "unknown"
				r.Blocked = append(r.Blocked, "invalid_job_state")
			}
			state.counts[name] += count
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return r, err
		}
	}
	parts := map[string]hygienePart{}
	if tables["storage_parts"] && tables["storage_pending_chunks"] && tables["copy_jobs"] && tables["copy_entries"] {
		rows, err := db.QueryContext(ctx, `SELECT p.upload_id,p.chunk_idx,p.part_idx,p.size,p.sha256,p.ref,
 u.status='complete' AND u.canceling=0 AND n.kind='file' AND n.size=u.size AND n.sha256=u.sha256
 AND NOT EXISTS(SELECT 1 FROM storage_pending_chunks q WHERE q.upload_id=p.upload_id)
 AND NOT EXISTS(SELECT 1 FROM copy_entries e JOIN copy_jobs j ON e.job_id=j.id
                WHERE e.target_id=p.upload_id AND j.status NOT IN ('complete','cancelled'))
 FROM storage_parts p JOIN storage_uploads u ON p.upload_id=u.id
 LEFT JOIN nodes n ON n.id=u.id ORDER BY p.upload_id,p.chunk_idx,p.part_idx`)
		if err != nil {
			return r, err
		}
		for rows.Next() {
			var p hygienePart
			var disposable sql.NullBool
			if err = rows.Scan(&p.UploadID, &p.ChunkIndex, &p.PartIndex, &p.PartSize, &p.PartSHA256, &p.Ref, &disposable); err != nil {
				break
			}
			if len(parts) >= hygieneFileLimit {
				err = domain.ErrInvalid
				break
			}
			if _, duplicate := parts[p.Ref]; duplicate {
				r.Blocked = append(r.Blocked, "duplicate_staged_reference")
			}
			p.Area, p.Reason = "upload-parts", "verified_published_staging"
			p.disposable = disposable.Valid && disposable.Bool && isHexID(p.UploadID) && validHash(p.PartSHA256) &&
				p.PartSize > 0 && p.PartSize <= TransportPartSize && p.ChunkIndex >= 0 && p.PartIndex >= 0
			parts[p.Ref] = p
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return r, err
		}
	}
	if err := inspectHygieneDirectory(ctx, dataDir, "upload-parts", func(name string, info os.FileInfo, root *os.Root) error {
		if info.Mode().IsRegular() {
			if err := hygieneAddBytes(&r.StagedBytes, info.Size()); err != nil {
				return err
			}
		}
		p, tracked := parts[name]
		delete(parts, name)
		if !tracked {
			r.UnresolvedPartFiles++
			return nil
		}
		if !p.disposable {
			r.RecoverablePartFiles++
			return nil
		}
		if !hygieneBlobName(name) || !hygieneSingleFile(info) || info.Size() != p.PartSize+32 || !hygieneHeader(root, name, "HP01") {
			r.UnknownFiles++
			return nil
		}
		p.HygieneCandidate = hygieneIdentity(p.HygieneCandidate, info)
		r.Candidates = append(r.Candidates, p.HygieneCandidate)
		return nil
	}); err != nil {
		return r, err
	}
	for _, p := range parts {
		r.MissingPartFiles++
		if p.disposable && hygieneBlobName(p.Ref) {
			p.Reason = "verified_published_missing_part_row"
			r.Candidates = append(r.Candidates, p.HygieneCandidate)
		}
	}
	cacheOwned := false
	if err := inspectHygieneDirectory(ctx, dataDir, "read-cache", func(name string, info os.FileInfo, root *os.Root) error {
		if name == readCacheMarker {
			if info.Mode().IsRegular() && info.Size() == int64(len(readCacheMagic)) {
				f, err := root.Open(name)
				if err == nil {
					data, readErr := io.ReadAll(io.LimitReader(f, int64(len(readCacheMagic))+1))
					f.Close()
					cacheOwned = readErr == nil && string(data) == readCacheMagic
				}
			}
			return nil
		}
		if info.Mode().IsRegular() {
			if err := hygieneAddBytes(&r.CacheBytes, info.Size()); err != nil {
				return err
			}
		}
		pending := strings.TrimPrefix(name, ".pending-")
		temporary := pending != name && pending != "" && strings.Trim(pending, "0123456789") == ""
		if (hygieneBlobName(name) || temporary) && hygieneSingleFile(info) && info.Size() >= 32 && info.Size() <= maxCiphertextSize && hygieneHeader(root, name, "HD01") {
			r.Candidates = append(r.Candidates, hygieneIdentity(HygieneCandidate{Area: "read-cache", Ref: name, Reason: "disposable_ciphertext_cache"}, info))
		} else {
			r.UnknownFiles++
		}
		return nil
	}); err != nil {
		return r, err
	}
	if !cacheOwned {
		kept := r.Candidates[:0]
		for _, c := range r.Candidates {
			if c.Area == "read-cache" {
				r.UnknownFiles++
			} else {
				kept = append(kept, c)
			}
		}
		r.Candidates = kept
	}
	refs := map[string]bool{}
	rows, err := db.QueryContext(ctx, "SELECT ref FROM storage_chunks")
	if err != nil {
		return r, err
	}
	for rows.Next() {
		var ref string
		if err = rows.Scan(&ref); err != nil {
			break
		}
		if len(refs) >= 1000000 {
			err = domain.ErrInvalid
			break
		}
		refs[ref] = true
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return r, err
	}
	if err := inspectHygieneDirectory(ctx, dataDir, "blobs", func(name string, info os.FileInfo, _ *os.Root) error {
		if !refs[name] {
			r.LocalBlobCandidates++
		}
		return nil
	}); err != nil {
		return r, err
	}
	sort.Slice(r.Candidates, func(i, j int) bool {
		return r.Candidates[i].Area+"/"+r.Candidates[i].Ref < r.Candidates[j].Area+"/"+r.Candidates[j].Ref
	})
	sort.Strings(r.Blocked)
	for _, c := range r.Candidates {
		if err := hygieneAddBytes(&r.ReclaimableBytes, c.Bytes); err != nil {
			return r, err
		}
	}
	r.MetadataSHA256, err = hygieneMetadataDigest(ctx, db, tables)
	if err != nil {
		return r, err
	}
	encoded, err := json.Marshal(r)
	if err != nil {
		return r, err
	}
	r.PlanSHA256 = fmt.Sprintf("%x", sha256.Sum256(encoded))
	return r, nil
}

func hygieneAddBytes(total *int64, size int64) error {
	if size < 0 || *total > math.MaxInt64-size {
		return domain.ErrInvalid
	}
	*total += size
	return nil
}

func hygieneKnownState(table, state string) bool {
	switch table {
	case "storage_uploads":
		return state == "uploading" || state == "complete"
	case "storage_pending_chunks":
		return state == "receiving" || state == "queued" || state == "storing" || state == "failed"
	case "copy_jobs":
		return state == "queued" || state == "running" || state == "failed" || state == "cancelling" || state == "cancelled" || state == "complete"
	}
	return false
}

// Bind confirmation to the complete logical maintenance state, including changed
// references that leave aggregate counts unchanged. Values are hashed, never emitted.
func hygieneMetadataDigest(ctx context.Context, db *sql.DB, tables map[string]bool) (string, error) {
	hash := sha256.New()
	queries := []struct{ table, order string }{
		{"nodes", "id"}, {"storage_uploads", "id"}, {"storage_chunks", "upload_id,idx"},
		{"storage_parts", "upload_id,chunk_idx,part_idx"}, {"storage_pending_chunks", "upload_id,idx"},
		{"copy_jobs", "id"}, {"copy_entries", "job_id,idx"},
		{"sqlite_master", "type,name,tbl_name"},
	}
	for _, q := range queries {
		if present, optional := tables[q.table]; optional && !present {
			continue
		}
		if err := json.NewEncoder(hash).Encode(q.table); err != nil {
			return "", err
		}
		rows, err := db.QueryContext(ctx, "SELECT * FROM "+q.table+" ORDER BY "+q.order)
		if err != nil {
			return "", err
		}
		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			return "", err
		}
		values, pointers := make([]any, len(columns)), make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		count := 0
		for rows.Next() {
			count++
			if count > 1000000 {
				err = domain.ErrInvalid
				break
			}
			if err = rows.Scan(pointers...); err != nil {
				break
			}
			if err = json.NewEncoder(hash).Encode(values); err != nil {
				break
			}
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func hygieneBlobName(name string) bool {
	return len(name) == 65 && name[32] == '-' && isHexID(name[:32]) && isHexID(name[33:])
}

func hygieneIdentity(c HygieneCandidate, info os.FileInfo) HygieneCandidate {
	c.Present, c.Bytes, c.ModifiedNS = true, info.Size(), info.ModTime().UnixNano()
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		c.Device, c.Inode = uint64(st.Dev), uint64(st.Ino)
	}
	return c
}

func hygieneSingleFile(info os.FileInfo) bool {
	if !info.Mode().IsRegular() {
		return false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Nlink == 1
}

func hygieneHeader(root *os.Root, name, magic string) bool {
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return false
	}
	defer f.Close()
	var header [4]byte
	_, err = io.ReadFull(f, header[:])
	return err == nil && string(header[:]) == magic
}

func inspectHygieneDirectory(ctx context.Context, dataDir, area string, visit func(string, os.FileInfo, *os.Root) error) error {
	path := filepath.Join(dataDir, area)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return domain.ErrInvalid
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return err
	}
	defer root.Close()
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	count := 0
	for {
		entries, err := dir.ReadDir(256)
		if err != nil && err != io.EOF {
			return err
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			count++
			if count > hygieneFileLimit {
				return domain.ErrInvalid
			}
			info, err := root.Lstat(entry.Name())
			if err != nil {
				return err
			}
			if err := visit(entry.Name(), info, root); err != nil {
				return err
			}
		}
		if err == io.EOF {
			return nil
		}
	}
}

// ApplyHygiene recomputes the complete plan before writing an audit or deleting
// anything. The caller must hold the drive lock until this function returns.
func ApplyHygiene(ctx context.Context, db *sql.DB, dataDir, confirmation string) (HygieneReport, error) {
	r, err := InspectHygiene(ctx, db, dataDir)
	if err != nil {
		return r, err
	}
	if !validHash(confirmation) || confirmation != r.PlanSHA256 || len(r.Blocked) > 0 {
		return r, fmt.Errorf("%w: cleanup requires the current unblocked plan SHA-256", domain.ErrConflict)
	}
	if len(r.Candidates) == 0 {
		return r, nil
	}
	audit, err := os.OpenFile(filepath.Join(dataDir, "storage-cleanup.jsonl"), os.O_WRONLY|os.O_CREATE|os.O_APPEND|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return r, err
	}
	defer audit.Close()
	info, err := audit.Stat()
	if err != nil || !hygieneSingleFile(info) || info.Mode().Perm()&0077 != 0 {
		return r, domain.ErrInvalid
	}
	dir, err := os.Open(dataDir)
	if err != nil {
		return r, err
	}
	err = dir.Sync()
	dir.Close()
	if err != nil {
		return r, err
	}
	writeAudit := func(c HygieneCandidate, outcome string) error {
		event := struct {
			At, Plan, Area, Ref, Outcome string
		}{time.Now().UTC().Format(time.RFC3339Nano), r.PlanSHA256, c.Area, c.Ref, outcome}
		if err := json.NewEncoder(audit).Encode(event); err != nil {
			return err
		}
		return audit.Sync()
	}
	if err := writeAudit(HygieneCandidate{}, "started"); err != nil {
		return r, err
	}
	for _, c := range r.Candidates {
		if err := ctx.Err(); err != nil {
			return r, err
		}
		err := applyHygieneCandidate(ctx, db, dataDir, c, writeAudit)
		if err != nil {
			_ = writeAudit(c, "failed_or_interrupted")
			return r, err
		}
		if c.Present {
			r.RemovedFiles++
			r.RemovedBytes += c.Bytes
		}
		if c.Area == "upload-parts" {
			r.RemovedPartRows++
		}
	}
	r.Applied = true
	return r, writeAudit(HygieneCandidate{}, "complete")
}

func applyHygieneCandidate(ctx context.Context, db *sql.DB, dataDir string, c HygieneCandidate, audit func(HygieneCandidate, string) error) error {
	var tx *sql.Tx
	if c.Area == "upload-parts" {
		var err error
		tx, err = db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		result, err := tx.ExecContext(ctx, `DELETE FROM storage_parts WHERE upload_id=? AND chunk_idx=? AND part_idx=? AND ref=? AND size=? AND sha256=?
 AND EXISTS(SELECT 1 FROM storage_uploads u JOIN nodes n ON n.id=u.id WHERE u.id=storage_parts.upload_id
            AND u.status='complete' AND u.canceling=0 AND n.kind='file' AND n.size=u.size AND n.sha256=u.sha256)
 AND NOT EXISTS(SELECT 1 FROM storage_pending_chunks p WHERE p.upload_id=storage_parts.upload_id)
 AND NOT EXISTS(SELECT 1 FROM copy_entries e JOIN copy_jobs j ON e.job_id=j.id
                WHERE e.target_id=storage_parts.upload_id AND j.status NOT IN ('complete','cancelled'))`, c.UploadID, c.ChunkIndex, c.PartIndex, c.Ref, c.PartSize, c.PartSHA256)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil || count != 1 {
			return domain.ErrConflict
		}
	}
	if err := audit(c, "remove_intent"); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := os.OpenRoot(filepath.Join(dataDir, c.Area))
	if errors.Is(err, os.ErrNotExist) && !c.Present && tx != nil {
		if err := tx.Commit(); err != nil {
			return err
		}
		return audit(c, "removed")
	}
	if err != nil {
		return err
	}
	defer root.Close()
	info, err := root.Lstat(c.Ref)
	if !c.Present {
		if !errors.Is(err, os.ErrNotExist) {
			return domain.ErrConflict
		}
	} else {
		if err != nil || !hygieneSingleFile(info) {
			return domain.ErrConflict
		}
		current := hygieneIdentity(c, info)
		if current.Bytes != c.Bytes || current.ModifiedNS != c.ModifiedNS || current.Device != c.Device || current.Inode != c.Inode {
			return domain.ErrConflict
		}
		if err := root.Remove(c.Ref); err != nil {
			return err
		}
		dir, err := root.Open(".")
		if err != nil {
			return err
		}
		err = dir.Sync()
		dir.Close()
		if err != nil {
			return err
		}
	}
	if tx != nil {
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return audit(c, "removed")
}
