package storage

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"private-drive/internal/domain"
)

func hygieneFixture(t *testing.T) (*Engine, string, string) {
	t.Helper()
	e, dir := integrityFixture(t)
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	_, err := e.db.Exec(`CREATE TABLE copy_jobs(id TEXT PRIMARY KEY,status TEXT NOT NULL);
 CREATE TABLE copy_entries(job_id TEXT NOT NULL REFERENCES copy_jobs(id),idx INTEGER,target_id TEXT,state TEXT,PRIMARY KEY(job_id,idx));`)
	if err != nil {
		t.Fatal(err)
	}
	e.staging, err = NewLocal(filepath.Join(dir, "upload-parts"))
	if err != nil {
		t.Fatal(err)
	}
	var id string
	if err := e.db.QueryRow("SELECT id FROM storage_uploads LIMIT 1").Scan(&id); err != nil {
		t.Fatal(err)
	}
	return e, dir, id
}

func hygieneStage(t *testing.T, e *Engine, id string) string {
	t.Helper()
	data := []byte("integrity-check fixture")[:8]
	nonce := make([]byte, e.aead.NonceSize())
	nonce[0] = 1 // Generated fixture key only; no production encryption path changes.
	sealed := append([]byte("HP01"), nonce...)
	sealed = e.aead.Seal(sealed, nonce, data, transportAAD(id, 0, 0))
	ref, err := e.staging.Put(context.Background(), id, sealed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec("INSERT INTO storage_parts(upload_id,chunk_idx,part_idx,size,sha256,ref) VALUES(?,0,0,?,?,?)", id, len(data), digest(data), ref); err != nil {
		t.Fatal(err)
	}
	return ref
}

func hygieneCache(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "read-cache")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, readCacheMarker), []byte(readCacheMagic), 0600); err != nil {
		t.Fatal(err)
	}
	ref := domain.NewID() + "-" + domain.NewID()
	if err := os.WriteFile(filepath.Join(path, ref), append([]byte("HD01"), make([]byte, 28)...), 0600); err != nil {
		t.Fatal(err)
	}
	return ref
}

func TestHygieneDiagnosticsAndScopedCleanup(t *testing.T) {
	e, dir, id := hygieneFixture(t)
	stage, cache := hygieneStage(t, e, id), hygieneCache(t, dir)
	orphan := domain.NewID() + "-" + domain.NewID()
	for _, file := range []string{filepath.Join(dir, "blobs", orphan), filepath.Join(dir, "upload-parts", "untracked"), filepath.Join(dir, "read-cache", "unknown")} {
		if err := os.WriteFile(file, []byte("retain generated fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	before, err := InspectHygiene(context.Background(), e.db, dir)
	if err != nil || len(before.Blocked) != 0 || len(before.Candidates) != 2 || before.LocalBlobCandidates != 1 || before.UnresolvedPartFiles != 1 || before.ReclaimableBytes != 72 || before.Integrity.ContentChecked {
		t.Fatalf("diagnostics: %+v %v", before, err)
	}
	again, err := InspectHygiene(context.Background(), e.db, dir)
	if err != nil || before.PlanSHA256 != again.PlanSHA256 {
		t.Fatalf("read-only plan changed: %+v %v", again, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "storage-cleanup.jsonl")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("dry-run created audit")
	}
	after, err := ApplyHygiene(context.Background(), e.db, dir, before.PlanSHA256)
	if err != nil || !after.Applied || after.RemovedFiles != 2 || after.RemovedPartRows != 1 || after.RemovedBytes != 72 {
		t.Fatalf("cleanup: %+v %v", after, err)
	}
	for _, file := range []string{filepath.Join(dir, "upload-parts", stage), filepath.Join(dir, "read-cache", cache)} {
		if _, err := os.Stat(file); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("confirmed disposable file retained")
		}
	}
	for _, file := range []string{filepath.Join(dir, "blobs", orphan), filepath.Join(dir, "upload-parts", "untracked"), filepath.Join(dir, "read-cache", "unknown"), filepath.Join(dir, "read-cache", readCacheMarker)} {
		if _, err := os.Stat(file); err != nil {
			t.Fatal("unapproved file removed")
		}
	}
	audit, err := os.ReadFile(filepath.Join(dir, "storage-cleanup.jsonl"))
	if err != nil || !bytes.Contains(audit, []byte("remove_intent")) || !bytes.Contains(audit, []byte("complete")) {
		t.Fatalf("audit incomplete: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "storage-cleanup.jsonl"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("audit not private")
	}
	verified, err := e.VerifyIntegrity(context.Background())
	if err != nil || len(verified.Issues) != 0 || verified.VerifiedFiles != 1 {
		t.Fatalf("cleanup changed original content: %+v %v", verified, err)
	}
}

func TestHygieneRetainsRecoverableAndUnknownState(t *testing.T) {
	for _, state := range []string{"receiving", "queued", "storing", "failed", "copy", "unpublished", "unowned-cache", "bad-header", "symlink"} {
		t.Run(state, func(t *testing.T) {
			e, dir, id := hygieneFixture(t)
			ref := hygieneStage(t, e, id)
			var err error
			switch state {
			case "copy":
				_, err = e.db.Exec("INSERT INTO copy_jobs VALUES('job','failed'); INSERT INTO copy_entries VALUES('job',0,?,'ready')", id)
			case "unpublished":
				_, err = e.db.Exec("DELETE FROM nodes WHERE id=?", id)
			case "unowned-cache":
				cache := hygieneCache(t, dir)
				if err := os.Remove(filepath.Join(dir, "read-cache", readCacheMarker)); err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(filepath.Join(dir, "read-cache", cache)); err != nil {
					t.Fatal(err)
				}
			case "bad-header":
				err = os.WriteFile(filepath.Join(dir, "upload-parts", ref), make([]byte, 40), 0600)
			case "symlink":
				path := filepath.Join(dir, "upload-parts", ref)
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				err = os.Symlink(filepath.Join(dir, "drive.db"), path)
			default:
				if _, err = e.db.Exec("UPDATE storage_uploads SET status='uploading' WHERE id=?", id); err == nil {
					_, err = e.db.Exec("DELETE FROM nodes WHERE id=?", id)
				}
				if err == nil {
					_, err = e.db.Exec("INSERT INTO storage_pending_chunks(upload_id,idx,state,created_at) VALUES(?,0,?,'fixture')", id, state)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			r, err := InspectHygiene(context.Background(), e.db, dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range r.Candidates {
				if c.Ref == ref && state != "unowned-cache" || c.Area == "read-cache" && state == "unowned-cache" {
					t.Fatal("recoverable or unowned data offered for deletion")
				}
			}
			if _, err := os.Lstat(filepath.Join(dir, "upload-parts", ref)); err != nil {
				t.Fatal("diagnostics deleted data")
			}
		})
	}
}

func TestHygieneRejectsStaleOrUnsafePlans(t *testing.T) {
	for _, mutation := range []string{"wrong-confirmation", "metadata-ref", "replacement", "symlink", "schema", "manifest", "audit-symlink", "audit-hardlink", "part-hardlink"} {
		t.Run(mutation, func(t *testing.T) {
			e, dir, id := hygieneFixture(t)
			ref := hygieneStage(t, e, id)
			plan, err := InspectHygiene(context.Background(), e.db, dir)
			if err != nil {
				t.Fatal(err)
			}
			confirmation := plan.PlanSHA256
			path := filepath.Join(dir, "upload-parts", ref)
			switch mutation {
			case "wrong-confirmation":
				confirmation = strings.Repeat("0", 64)
			case "metadata-ref":
				_, err = e.db.Exec("UPDATE storage_chunks SET ref=? WHERE upload_id=? AND idx=0", domain.NewID()+"-"+domain.NewID(), id)
			case "replacement":
				info, statErr := os.Stat(path)
				if statErr != nil {
					t.Fatal(statErr)
				}
				err = os.WriteFile(path+".replacement", make([]byte, info.Size()), 0600)
				if err == nil {
					err = os.Rename(path+".replacement", path)
				}
				if err == nil {
					err = os.Chtimes(path, info.ModTime(), info.ModTime())
				}
			case "symlink":
				if err = os.Remove(path); err == nil {
					err = os.Symlink(filepath.Join(dir, "drive.db"), path)
				}
			case "schema":
				_, err = e.db.Exec("DROP TABLE copy_entries")
			case "manifest":
				_, err = e.db.Exec("DELETE FROM storage_chunks WHERE upload_id=? AND idx=0", id)
			case "audit-symlink":
				err = os.Symlink(filepath.Join(dir, "drive.db"), filepath.Join(dir, "storage-cleanup.jsonl"))
			case "audit-hardlink":
				err = os.Link(filepath.Join(dir, "drive.db"), filepath.Join(dir, "storage-cleanup.jsonl"))
			case "part-hardlink":
				err = os.Link(path, filepath.Join(dir, "retained-alias"))
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ApplyHygiene(context.Background(), e.db, dir, confirmation); err == nil {
				t.Fatal("unsafe or stale plan accepted")
			}
			if _, err := os.Lstat(path); err != nil {
				t.Fatal("rejected plan removed data")
			}
			var count int
			if err := e.db.QueryRow("SELECT count(*) FROM storage_parts").Scan(&count); err != nil || count != 1 {
				t.Fatal("rejected plan changed staged association")
			}
		})
	}
}

func TestHygieneFailedTransactionAndInterruptedCleanupRetry(t *testing.T) {
	e, dir, id := hygieneFixture(t)
	ref := hygieneStage(t, e, id)
	if _, err := e.db.Exec("CREATE TRIGGER block_cleanup BEFORE DELETE ON storage_parts BEGIN SELECT RAISE(ABORT,'fixture'); END"); err != nil {
		t.Fatal(err)
	}
	plan, err := InspectHygiene(context.Background(), e.db, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyHygiene(context.Background(), e.db, dir, plan.PlanSHA256); err == nil {
		t.Fatal("failed transaction succeeded")
	}
	if _, err := os.Stat(filepath.Join(dir, "upload-parts", ref)); err != nil {
		t.Fatal("SQL failure removed source part")
	}
	if _, err := e.db.Exec("DROP TRIGGER block_cleanup"); err != nil {
		t.Fatal(err)
	}
	plan, err = InspectHygiene(context.Background(), e.db, dir)
	if err != nil || len(plan.Candidates) != 1 {
		t.Fatalf("plan: %+v %v", plan, err)
	}
	// Model death after unlink but before transaction commit. The published remote
	// manifest is untouched; the remaining row becomes a safe SQL-only retry.
	err = applyHygieneCandidate(context.Background(), e.db, dir, plan.Candidates[0], func(_ HygieneCandidate, outcome string) error {
		if outcome == "remove_intent" {
			if err := os.Remove(filepath.Join(dir, "upload-parts", ref)); err != nil {
				t.Fatal(err)
			}
			return errors.New("controlled interruption")
		}
		return nil
	})
	if err == nil {
		t.Fatal("interruption ignored")
	}
	if err := os.Remove(filepath.Join(dir, "upload-parts")); err != nil {
		t.Fatal(err)
	}
	plan, err = InspectHygiene(context.Background(), e.db, dir)
	if err != nil || len(plan.Candidates) != 1 || plan.Candidates[0].Present || plan.MissingPartFiles != 1 {
		t.Fatalf("missing-part retry: %+v %v", plan, err)
	}
	result, err := ApplyHygiene(context.Background(), e.db, dir, plan.PlanSHA256)
	if err != nil || !result.Applied || result.RemovedFiles != 0 || result.RemovedPartRows != 1 {
		t.Fatalf("interrupted cleanup retry: %+v %v", result, err)
	}
	verified, err := e.VerifyIntegrity(context.Background())
	if err != nil || verified.VerifiedFiles != 1 || len(verified.Issues) != 0 {
		t.Fatalf("original file damaged: %+v %v", verified, err)
	}
}

func TestHygieneCancellation(t *testing.T) {
	e, dir, id := hygieneFixture(t)
	ref := hygieneStage(t, e, id)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := InspectHygiene(ctx, e.db, dir); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation ignored: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "upload-parts", ref)); err != nil {
		t.Fatal("canceled diagnostics changed data")
	}
}
