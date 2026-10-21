package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"private-drive/internal/domain"
)

func integrityFixture(t *testing.T) (*Engine, string) {
	t.Helper()
	dir := t.TempDir()
	e, db := testEngine(t, dir, nil, nil)
	if _, err := db.Exec(`CREATE TABLE nodes(id TEXT PRIMARY KEY,parent_id TEXT NOT NULL DEFAULT '',kind TEXT,size INTEGER NOT NULL DEFAULT 0,sha256 TEXT NOT NULL DEFAULT '')`); err != nil {
		t.Fatal(err)
	}
	u := begin(t, e, []byte("integrity-check fixture"), digest([]byte("integrity-check fixture")))
	for i := 0; i < u.TotalChunks; i++ {
		put(t, e, u, []byte("integrity-check fixture"), i)
	}
	if _, err := e.Complete(context.Background(), u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO nodes(id,kind,size,sha256) VALUES(?,'file',?,?)", u.ID, u.Size, u.SHA256); err != nil {
		t.Fatal(err)
	}
	return e, dir
}

func hasIntegrityIssue(report IntegrityReport, code string) bool {
	for _, issue := range report.Issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}

func TestIntegrityAuditReadOnlyAndOrphanCandidates(t *testing.T) {
	e, dir := integrityFixture(t)
	metadata, err := CheckIntegrity(context.Background(), e.db)
	if err != nil || len(metadata.Issues) != 0 || metadata.Files != 1 || metadata.ContentChecked {
		t.Fatalf("metadata %+v %v", metadata, err)
	}
	verified, err := e.VerifyIntegrity(context.Background())
	if err != nil || len(verified.Issues) != 0 || verified.VerifiedFiles != 1 || !verified.ContentChecked {
		t.Fatalf("content %+v %v", verified, err)
	}
	store := e.backend.(*LocalStore)
	orphan := domain.NewID() + "-" + domain.NewID()
	if err := os.WriteFile(filepath.Join(dir, "blobs", orphan), []byte("orphan ciphertext fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	candidates, err := store.OrphanCandidates(context.Background(), e.db)
	if err != nil || len(candidates) != 1 || candidates[0] != orphan {
		t.Fatalf("orphan candidates %+v %v", candidates, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "blobs", orphan)); err != nil {
		t.Fatal("audit deleted orphan")
	}
	if _, err := e.db.Exec("INSERT INTO nodes(id,parent_id,kind) VALUES('bad-parent','absent','folder')"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec("INSERT INTO nodes(id,parent_id,kind) VALUES('cycle-a','cycle-b','folder'),('cycle-b','cycle-a','folder')"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec("INSERT INTO nodes(id,kind,size,sha256) VALUES('missing','file',3,?)", digest([]byte("foo"))); err != nil {
		t.Fatal(err)
	}
	report, err := CheckIntegrity(context.Background(), e.db)
	if err != nil || !hasIntegrityIssue(report, "invalid_parent") || !hasIntegrityIssue(report, "folder_cycle_or_depth") || !hasIntegrityIssue(report, "missing_manifest") {
		t.Fatalf("undetected corruption %+v %v", report, err)
	}
}

func TestIntegrityMissingCorruptChunksAndGeometry(t *testing.T) {
	for _, mode := range []string{"missing-blob", "corrupt-blob", "missing-chunk", "wrong-hash", "geometry"} {
		t.Run(mode, func(t *testing.T) {
			e, dir := integrityFixture(t)
			var id, ref string
			if err := e.db.QueryRow("SELECT upload_id,ref FROM storage_chunks LIMIT 1").Scan(&id, &ref); err != nil {
				t.Fatal(err)
			}
			want := "content_integrity_failed"
			switch mode {
			case "missing-blob":
				if err := os.Remove(filepath.Join(dir, "blobs", ref)); err != nil {
					t.Fatal(err)
				}
				want = "content_unavailable"
			case "corrupt-blob":
				if err := os.WriteFile(filepath.Join(dir, "blobs", ref), []byte("corrupt"), 0600); err != nil {
					t.Fatal(err)
				}
			case "missing-chunk":
				if _, err := e.db.Exec("DELETE FROM storage_chunks WHERE upload_id=? AND idx=0", id); err != nil {
					t.Fatal(err)
				}
			case "wrong-hash":
				if _, err := e.db.Exec("UPDATE storage_uploads SET sha256=? WHERE id=?", digest([]byte("other")), id); err != nil {
					t.Fatal(err)
				}
			case "geometry":
				if _, err := e.db.Exec("UPDATE storage_uploads SET chunk_size=999999999 WHERE id=?", id); err != nil {
					t.Fatal(err)
				}
			}
			report, err := e.VerifyIntegrity(context.Background())
			if err != nil || !hasIntegrityIssue(report, want) {
				t.Fatalf("missing issue %s %+v %v", want, report, err)
			}
			if report.VerifiedFiles != 0 {
				t.Fatal("broken file qualified")
			}
			if mode == "missing-chunk" && !hasIntegrityIssue(report, "missing_or_incorrect_chunks") {
				t.Fatal("missing chunk metadata unreported")
			}
			if mode == "geometry" && !hasIntegrityIssue(report, "invalid_upload_geometry") {
				t.Fatal("bad geometry unreported")
			}
		})
	}
}

func TestIntegrityCancelledAndUnpublishedStates(t *testing.T) {
	e, _ := integrityFixture(t)
	u := begin(t, e, nil, "")
	if _, err := e.Complete(context.Background(), u.ID); err != nil {
		t.Fatal(err)
	}
	report, err := CheckIntegrity(context.Background(), e.db)
	if err != nil || !hasIntegrityIssue(report, "unpublished_manifest") {
		t.Fatalf("unpublished state %+v %v", report, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CheckIntegrity(ctx, e.db); !errors.Is(err, context.Canceled) {
		t.Fatalf("audit ignored cancellation %v", err)
	}
}
