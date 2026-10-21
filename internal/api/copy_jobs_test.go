package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"private-drive/internal/domain"
	"private-drive/internal/storage"
)

func copyFile(t *testing.T, f *fixture, parent string, payload []byte) domain.Node {
	t.Helper()
	ctx := context.Background()
	digest := sha256.Sum256(payload)
	u, err := f.api.cfg.Storage.Begin(ctx, domain.UploadSpec{Name: "fixture.txt", ParentID: parent, Size: int64(len(payload)), MimeType: "text/plain", SHA256: hex.EncodeToString(digest[:])})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < u.TotalChunks; i++ {
		start := int64(i) * u.ChunkSize
		if _, err = f.api.cfg.Storage.PutChunk(ctx, u.ID, i, bytes.NewReader(payload[start:min(start+u.ChunkSize, int64(len(payload)))]), ""); err != nil {
			t.Fatal(err)
		}
	}
	m, err := f.api.cfg.Storage.Complete(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	n := domain.Node{ID: m.ID, ParentID: parent, Name: m.Name, Kind: "file", Size: m.Size, MimeType: m.MimeType, SHA256: m.SHA256, Favorite: true, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err = insertNode(ctx, f.db, n); err != nil {
		t.Fatal(err)
	}
	return n
}

func finishCopy(t *testing.T, s *Server, id string) copyJob {
	t.Helper()
	for range 10000 {
		if err := s.copyStep(context.Background(), id); err != nil {
			t.Fatal(err)
		}
		job, err := s.copyJob(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == "complete" || job.Status == "cancelled" {
			return job
		}
	}
	t.Fatal("copy did not finish")
	return copyJob{}
}

func TestCopyJobScaleAtomicVisibilityAndOverlap(t *testing.T) {
	for _, count := range []int{100, 1000, 1001} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			f := newFixture(t)
			f.login()
			source, dest := f.folder("Source", ""), f.folder("Destination", "")
			tx, err := f.db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			ids := []string{source.ID}
			for i := 1; i < count; i++ {
				n := domain.Node{ID: domain.NewID(), ParentID: source.ID, Name: fmt.Sprintf("Folder %04d", i), Kind: "folder", Favorite: i == 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}
				if err := insertNode(context.Background(), tx, n); err != nil {
					t.Fatal(err)
				}
				if i == 1 {
					ids = append(ids, n.ID)
				}
			}
			if err = tx.Commit(); err != nil {
				t.Fatal(err)
			}
			spec := copyRequest{OperationID: domain.NewID(), SourceIDs: ids, ParentID: dest.ID}
			job, existed, err := f.api.planCopy(context.Background(), spec)
			if err != nil || existed || job.TotalEntries != count {
				t.Fatalf("plan %+v %v", job, err)
			}
			var visible int
			if err = f.db.QueryRow("SELECT count(*) FROM nodes WHERE parent_id=?", dest.ID).Scan(&visible); err != nil || visible != 0 {
				t.Fatalf("premature visibility: %d %v", visible, err)
			}
			repeated, existed, err := f.api.planCopy(context.Background(), spec)
			if err != nil || !existed || repeated.ID != job.ID {
				t.Fatalf("idempotent plan: %+v %v", repeated, err)
			}
			job = finishCopy(t, f.api, job.ID)
			if job.CompletedEntries != count || len(job.ResultIDs) != 1 {
				t.Fatalf("bad completion %+v", job)
			}
			if err = f.db.QueryRow(subtreeSQL+"SELECT count(*) FROM subtree", job.ResultIDs[0]).Scan(&visible); err != nil || visible != count {
				t.Fatalf("bad tree: %d %v", visible, err)
			}
			if err = f.api.copyStep(context.Background(), job.ID); err != nil {
				t.Fatal(err)
			}
			requireStatus(t, f.request("DELETE", "/api/v1/copy-jobs/"+job.ID, nil, nil), 409)
		})
	}
}

func TestCopyJobRestartResumesCommittedChunksAndBeginWindow(t *testing.T) {
	f := newFixture(t)
	f.login()
	payload := []byte("durable multipart copy crosses many chunks")
	source := copyFile(t, f, "", payload)
	spec := copyRequest{OperationID: domain.NewID(), SourceIDs: []string{source.ID}}
	job, _, err := f.api.planCopy(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	// Emulate process loss after Begin persisted but before plan recorded its ID.
	u, err := f.api.cfg.Storage.Begin(context.Background(), domain.UploadSpec{Name: "fixture.txt copy", ParentID: "copy:" + job.ID + ":0", Size: source.Size, MimeType: source.MimeType, SHA256: source.SHA256})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.api.cfg.Storage.PutChunk(context.Background(), u.ID, 0, bytes.NewReader(payload[:u.ChunkSize]), ""); err != nil {
		t.Fatal(err)
	}
	entriesBefore, err := os.ReadDir(filepath.Join(f.dir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	if err = f.db.Close(); err != nil {
		t.Fatal(err)
	}
	f.start("")
	if err = f.api.copyStep(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	var target string
	if err = f.db.QueryRow("SELECT target_id FROM copy_entries WHERE job_id=?", job.ID).Scan(&target); err != nil || target != u.ID {
		t.Fatalf("lost Begin reconciliation %s %v", target, err)
	}
	entriesAfter, err := os.ReadDir(filepath.Join(f.dir, "blobs"))
	if err != nil || len(entriesAfter) != len(entriesBefore)+1 {
		t.Fatalf("committed chunk was reuploaded %d %d %v", len(entriesBefore), len(entriesAfter), err)
	}
	job = finishCopy(t, f.api, job.ID)
	if job.CompletedBytes != int64(len(payload)) {
		t.Fatal(job)
	}
	reader, m, err := f.api.cfg.Storage.Open(context.Background(), job.ResultIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || !bytes.Equal(got, payload) || m.SHA256 != source.SHA256 {
		t.Fatalf("copy integrity %v", err)
	}
	n, err := nodeFrom(context.Background(), f.db, source.ID)
	if err != nil || n.SHA256 != source.SHA256 {
		t.Fatalf("original changed %v", err)
	}
}

func TestCopyJobCancellationCleanupAndFailurePreservesOriginal(t *testing.T) {
	f := newFixture(t)
	f.login()
	source := copyFile(t, f, "", []byte("cancelled copy retains original"))
	job, _, err := f.api.planCopy(context.Background(), copyRequest{OperationID: domain.NewID(), SourceIDs: []string{source.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.api.copyStep(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	staged := decode[struct {
		Items []domain.Upload `json:"items"`
	}](t, f.request("GET", "/api/v1/uploads", nil, nil), 200)
	if len(staged.Items) != 0 {
		t.Fatalf("staged upload leaked %+v", staged.Items)
	}
	requireStatus(t, f.request("DELETE", "/api/v1/copy-jobs/"+job.ID, nil, nil), 204)
	finishCopy(t, f.api, job.ID)
	uploads, err := f.api.cfg.Storage.List(context.Background())
	if err != nil || len(uploads) != 1 || uploads[0].ID != source.ID {
		t.Fatalf("cancel left data or deleted original %+v %v", uploads, err)
	}
	requireStatus(t, f.request("DELETE", "/api/v1/copy-jobs/"+job.ID, nil, nil), 204)
	requireStatus(t, f.request("POST", "/api/v1/copy-jobs/"+job.ID+"/retry", nil, nil), 409)
	queued, _, err := f.api.planCopy(context.Background(), copyRequest{OperationID: domain.NewID(), SourceIDs: []string{source.ID}})
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, f.request("DELETE", "/api/v1/copy-jobs/"+queued.ID, nil, nil), 204)
	finishCopy(t, f.api, queued.ID)
}

func TestCopyJobSnapshotAndDestinationCollisionRollback(t *testing.T) {
	f := newFixture(t)
	f.login()
	source, dest := f.folder("Source", ""), f.folder("Destination", "")
	child := f.folder("Captured", source.ID)
	job, _, err := f.api.planCopy(context.Background(), copyRequest{OperationID: domain.NewID(), SourceIDs: []string{source.ID}, ParentID: dest.ID})
	if err != nil {
		t.Fatal(err)
	}
	name := "Renamed after planning"
	newParent := ""
	if _, err = f.api.updateNode(context.Background(), child.ID, patch{Name: &name, ParentID: &newParent}); err != nil {
		t.Fatal(err)
	}
	conflict := f.folder("Source", dest.ID)
	if err = f.api.copyStep(context.Background(), job.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("collision accepted %v", err)
	}
	var count int
	if err = f.db.QueryRow("SELECT count(*) FROM nodes WHERE parent_id=?", dest.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("partial tree %d %v", count, err)
	}
	if err = f.api.trash(context.Background(), conflict.ID); err != nil {
		t.Fatal(err)
	}
	job = finishCopy(t, f.api, job.ID)
	if err = f.db.QueryRow("SELECT count(*) FROM nodes WHERE parent_id=? AND name='Captured'", job.ResultIDs[0]).Scan(&count); err != nil || count != 1 {
		t.Fatalf("snapshot mutated %d %v", count, err)
	}
}

func TestCopyJobDeepBoundAndLargeLimit(t *testing.T) {
	f := newFixture(t)
	f.login()
	root := f.folder("Deep", "")
	parent := root.ID
	tx, err := f.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < maxCopyDepth; i++ {
		n := domain.Node{ID: domain.NewID(), ParentID: parent, Name: fmt.Sprint(i), Kind: "folder", CreatedAt: time.Now(), UpdatedAt: time.Now()}
		if err = insertNode(context.Background(), tx, n); err != nil {
			t.Fatal(err)
		}
		parent = n.ID
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	job, _, err := f.api.planCopy(context.Background(), copyRequest{OperationID: domain.NewID(), SourceIDs: []string{root.ID}})
	if err != nil || job.TotalEntries != maxCopyDepth {
		t.Fatalf("deep plan %+v %v", job, err)
	}
	finishCopy(t, f.api, job.ID)
	f.folder("Too deep", parent)
	if _, _, err = f.api.planCopy(context.Background(), copyRequest{OperationID: domain.NewID(), SourceIDs: []string{root.ID}}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("unbounded depth: %v", err)
	}
	large := f.folder("Large", "")
	tx, err = f.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= maxCopyEntries; i++ {
		if err = insertNode(context.Background(), tx, domain.Node{ID: domain.NewID(), ParentID: large.ID, Name: fmt.Sprint(i), Kind: "folder", CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.api.planCopy(context.Background(), copyRequest{OperationID: domain.NewID(), SourceIDs: []string{large.ID}}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("unbounded plan: %v", err)
	}
}

func TestCopyJobAuthIdempotencyWorkerAndCorruption(t *testing.T) {
	f := newFixture(t)
	for _, route := range [][2]string{{"GET", "/copy-jobs"}, {"POST", "/copy-jobs"}, {"GET", "/copy-jobs/missing"}, {"POST", "/copy-jobs/missing/retry"}, {"DELETE", "/copy-jobs/missing"}} {
		requireStatus(t, f.request(route[0], "/api/v1"+route[1], nil, nil), 401)
	}
	f.login()
	source := copyFile(t, f, "", []byte("corruption cannot publish"))
	spec := copyRequest{OperationID: domain.NewID(), SourceIDs: []string{source.ID}}
	job := decode[copyJob](t, f.request("POST", "/api/v1/copy-jobs", spec, nil), 201)
	requireStatus(t, f.request("POST", "/api/v1/copy-jobs", spec, nil), 200)
	spec.ParentID = domain.NewID()
	requireStatus(t, f.request("POST", "/api/v1/copy-jobs", spec, nil), 409)
	requireStatus(t, f.request("POST", "/api/v1/copy-jobs", spec, map[string]string{"Origin": "https://evil.invalid"}), 403)
	var ref string
	if err := f.db.QueryRow("SELECT ref FROM storage_chunks WHERE upload_id=? AND idx=0", source.ID).Scan(&ref); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.dir, "blobs", ref), []byte("corrupted"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- f.api.RunCopyJobs(ctx) }()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		state, err := f.api.copyJob(context.Background(), job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if state.Status == "failed" {
			if state.ErrorCode != "integrity_failed" {
				t.Fatal(state)
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	state, err := f.api.copyJob(context.Background(), job.ID)
	if err != nil || state.Status != "failed" {
		t.Fatalf("worker failure %+v %v", state, err)
	}
	if err := f.api.RunCopyJobs(ctx); err == nil {
		t.Fatal("second worker accepted")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	var target string
	if err := f.db.QueryRow("SELECT target_id FROM copy_entries WHERE job_id=?", job.ID).Scan(&target); err != nil {
		t.Fatal(err)
	}
	if _, err := nodeFrom(context.Background(), f.db, target); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("corrupted copy published %v", err)
	}
	requireStatus(t, f.request("POST", "/api/v1/copy-jobs/"+job.ID+"/retry", nil, nil), 200)
}

func TestCopyJobConcurrentCancellationAndMetadata(t *testing.T) {
	f := newFixture(t)
	f.login()
	parent := f.folder("Concurrent", "")
	source := copyFile(t, f, parent.ID, []byte(strings.Repeat("12345678", 32)))
	job, _, err := f.api.planCopy(context.Background(), copyRequest{OperationID: domain.NewID(), SourceIDs: []string{parent.ID}})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := range 3 {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			if index == 0 {
				for range 3 {
					_ = f.api.copyStep(context.Background(), job.ID)
				}
			} else if index == 1 {
				_ = f.request("DELETE", "/api/v1/copy-jobs/"+job.ID, nil, nil)
			} else {
				name := "Changed"
				f.api.mutate.Lock()
				_, _ = f.api.updateNode(context.Background(), source.ID, patch{Name: &name})
				f.api.mutate.Unlock()
			}
		}(i)
	}
	wg.Wait()
	finishCopy(t, f.api, job.ID)
	if _, err = nodeFrom(context.Background(), f.db, source.ID); err != nil {
		t.Fatalf("original lost %v", err)
	}
}

func TestCopyJobPublicationRollbackAndStagedCompletion(t *testing.T) {
	f := newFixture(t)
	f.login()
	source := copyFile(t, f, "", []byte("staged completion survives restart"))
	job, _, err := f.api.planCopy(context.Background(), copyRequest{OperationID: domain.NewID(), SourceIDs: []string{source.ID}})
	if err != nil {
		t.Fatal(err)
	}
	for range 20 {
		if err = f.api.copyStep(context.Background(), job.ID); err != nil {
			t.Fatal(err)
		}
		state, err := f.api.copyJob(context.Background(), job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if state.CompletedEntries == state.TotalEntries {
			break
		}
	}
	staged := decode[struct {
		Items []domain.Upload `json:"items"`
	}](t, f.request("GET", "/api/v1/uploads", nil, nil), 200)
	if len(staged.Items) != 0 {
		t.Fatal("completed staged upload exposed")
	}
	if err = f.db.Close(); err != nil {
		t.Fatal(err)
	}
	f.start("")
	if _, err = f.db.Exec(`CREATE TRIGGER fail_copy_publish BEFORE UPDATE OF status ON copy_jobs WHEN NEW.status='complete' BEGIN SELECT RAISE(ABORT,'injected publication failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err = f.api.copyStep(context.Background(), job.ID); err == nil {
		t.Fatal("publication injection ignored")
	}
	var count int
	if err = f.db.QueryRow("SELECT count(*) FROM nodes").Scan(&count); err != nil || count != 1 {
		t.Fatalf("transaction leaked visible copy %d %v", count, err)
	}
	if _, err = f.db.Exec("DROP TRIGGER fail_copy_publish"); err != nil {
		t.Fatal(err)
	}
	finishCopy(t, f.api, job.ID)
}

type copyFailService struct {
	storage.Service
	failDelete atomic.Bool
	attempts   atomic.Int64
}

func (s *copyFailService) Cancel(ctx context.Context, id string) error {
	s.attempts.Add(1)
	if s.failDelete.Load() {
		return errors.New("synthetic cleanup outage")
	}
	return s.Service.Cancel(ctx, id)
}

func TestCopyJobFailedCleanupBoundedAndRetryable(t *testing.T) {
	f := newFixture(t)
	f.login()
	source := copyFile(t, f, "", []byte("cleanup failure fixture"))
	controlled := &copyFailService{Service: f.api.cfg.Storage}
	controlled.failDelete.Store(true)
	f.api.cfg.Storage = controlled
	job, _, err := f.api.planCopy(context.Background(), copyRequest{OperationID: domain.NewID(), SourceIDs: []string{source.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.api.copyStep(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, f.request("DELETE", "/api/v1/copy-jobs/"+job.ID, nil, nil), 204)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- f.api.RunCopyJobs(ctx) }()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		state, err := f.api.copyJob(context.Background(), job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if state.Status == "failed" {
			if state.ErrorCode != "cleanup_failed" {
				t.Fatal(state)
			}
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	cancel()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if controlled.attempts.Load() != 1 {
		t.Fatalf("unbounded cleanup retry %d", controlled.attempts.Load())
	}
	controlled.failDelete.Store(false)
	state := decode[copyJob](t, f.request("POST", "/api/v1/copy-jobs/"+job.ID+"/retry", nil, nil), 200)
	if state.Status != "cancelling" {
		t.Fatalf("cleanup resumed copying %+v", state)
	}
	finishCopy(t, f.api, job.ID)
	if _, err = nodeFrom(context.Background(), f.db, source.ID); err != nil {
		t.Fatalf("original removed %v", err)
	}
}

func TestCopyJobCorruptPlanRejectedWithoutOriginalLoss(t *testing.T) {
	for _, mode := range []string{"source-json", "target-map"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			f.login()
			source := f.folder("Original", "")
			if mode == "source-json" {
				source = copyFile(t, f, "", []byte("manifest fixture"))
			}
			job, _, err := f.api.planCopy(context.Background(), copyRequest{OperationID: domain.NewID(), SourceIDs: []string{source.ID}})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "source-json" {
				_, err = f.db.Exec("UPDATE copy_entries SET source='{' WHERE job_id=?", job.ID)
			} else {
				_, err = f.db.Exec("UPDATE copy_entries SET target_id='' WHERE job_id=?", job.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = f.api.copyStep(context.Background(), job.ID); !errors.Is(err, domain.ErrIntegrity) {
				t.Fatalf("bad plan accepted %v", err)
			}
			if _, err = nodeFrom(context.Background(), f.db, source.ID); err != nil {
				t.Fatalf("original lost %v", err)
			}
		})
	}
}
