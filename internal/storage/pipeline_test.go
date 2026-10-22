package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"private-drive/internal/domain"
)

func pendingWorker(t *testing.T, e *Engine) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- e.RunPending(ctx) }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("pending worker: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Error("pending worker did not stop")
			}
		})
	}
	t.Cleanup(stop)
	return stop
}

func waitPending(t *testing.T, e *Engine, id string, ready func(domain.Upload) bool) domain.Upload {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		u, err := e.Status(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if ready(u) {
			return u
		}
		select {
		case <-ctx.Done():
			t.Fatalf("pending state did not converge: %+v", u)
		case <-time.After(time.Millisecond):
		}
	}
}

func stagePending(t *testing.T, e *Engine, u domain.Upload, index int, data []byte) domain.Upload {
	t.Helper()
	u, err := e.PutPart(context.Background(), u.ID, index, 0, bytes.NewReader(data), digest(data))
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func pendingSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("pending worker signal timed out")
	}
}

func TestPendingPipelineOverlapsAndBounds(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	local, err := NewLocal(filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	backend := &blockedStore{BlobStore: local, started: make(chan struct{}, 4), release: make(chan struct{}, 4)}
	defer close(backend.release)
	e, _ := transportEngine(t, dir, backend, 8)
	data := []byte("firstone second thirdone")
	u := begin(t, e, data, digest(data))
	u = stagePending(t, e, u, 0, data[:8])
	if u.MaxPendingChunks != 2 || len(u.PendingChunks) != 1 || u.PendingChunks[0].State != "receiving" {
		t.Fatalf("pending capability: %+v", u)
	}
	if _, err := e.QueueChunk(ctx, u.ID, 0); err != nil {
		t.Fatal(err)
	}
	pendingWorker(t, e)
	select {
	case <-backend.started:
	case <-time.After(5 * time.Second):
		t.Fatal("background store did not start")
	}
	short, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	u, err = e.PutPart(short, u.ID, 1, 0, bytes.NewReader(data[8:16]), digest(data[8:16]))
	if err != nil || u.ReceivedBytes != 0 || len(u.PendingChunks) != 2 || u.PendingChunks[0].State != "storing" || u.PendingChunks[1].State != "receiving" {
		t.Fatalf("next part blocked behind remote store: %+v %v", u, err)
	}
	if _, err := e.QueueChunk(short, u.ID, 0); err != nil {
		t.Fatalf("idempotent queue waited for remote store: %v", err)
	}
	if _, err := e.QueueChunk(short, u.ID, 1); err != nil {
		t.Fatal(err)
	}
	body := bytes.NewReader(data[16:])
	if _, err := e.PutPart(short, u.ID, 2, 0, body, digest(data[16:])); !errors.Is(err, domain.ErrConflict) || body.Len() != len(data[16:]) {
		t.Fatalf("window accepted third chunk/read body: %v remaining=%d", err, body.Len())
	}
	if _, err := e.Complete(short, u.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("premature complete blocked or published queued bytes: %v", err)
	}
	backend.release <- struct{}{}
	select {
	case <-backend.started:
	case <-time.After(5 * time.Second):
		t.Fatal("second pending chunk did not start")
	}
	for range 100 {
		u, err = e.Status(ctx, u.ID)
		if err != nil {
			t.Fatal(err)
		}
		accepted := u.ReceivedBytes
		for _, pending := range u.PendingChunks {
			accepted += pending.Bytes
		}
		if accepted != 16 || u.ReceivedBytes != 8 || len(u.PendingChunks) != 1 {
			t.Fatalf("incoherent committed/pending snapshot: %+v", u)
		}
	}
	backend.release <- struct{}{}
	waitPending(t, e, u.ID, func(u domain.Upload) bool { return u.ReceivedBytes == 16 })
	stagePending(t, e, u, 2, data[16:])
	if _, err := e.QueueChunk(ctx, u.ID, 2); err != nil {
		t.Fatal(err)
	}
	pendingSignal(t, backend.started)
	backend.release <- struct{}{}
	waitPending(t, e, u.ID, func(u domain.Upload) bool { return u.ReceivedBytes == u.Size })
	manifest, err := e.Complete(ctx, u.ID)
	if err != nil || manifest.SHA256 != digest(data) {
		t.Fatalf("pipeline checksum: %+v %v", manifest, err)
	}
	if err := e.Cancel(ctx, u.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("cancel accepted completed file: %v", err)
	}
	var canceling int
	if err := e.db.QueryRow(`SELECT canceling FROM storage_uploads WHERE id=?`, u.ID).Scan(&canceling); err != nil || canceling != 0 {
		t.Fatalf("rejected cancellation changed completed file: %d %v", canceling, err)
	}
}

func TestPendingRemoteAcknowledgementDoesNotWaitForNextBrowserBody(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	local, err := NewLocal(filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	backend := &blockedStore{BlobStore: local, started: make(chan struct{}, 1), release: make(chan struct{}, 1)}
	e, _ := transportEngine(t, dir, backend, 8)
	data := []byte("fixture!second!!")
	u := begin(t, e, data, digest(data))
	stagePending(t, e, u, 0, data[:8])
	if _, err := e.QueueChunk(ctx, u.ID, 0); err != nil {
		t.Fatal(err)
	}
	pendingWorker(t, e)
	pendingSignal(t, backend.started)
	body, writer := io.Pipe()
	bodyEntered := make(chan struct{})
	finished := make(chan struct{})
	var partErr error
	go func() {
		_, partErr = e.PutPart(ctx, u.ID, 1, 0, &pendingBody{Reader: body, entered: bodyEntered}, digest(data[8:]))
		close(finished)
	}()
	t.Cleanup(func() {
		writer.Close()
		body.Close()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("next browser part did not stop")
		}
	})
	pendingSignal(t, bodyEntered)
	backend.release <- struct{}{}
	u = waitPending(t, e, u.ID, func(u domain.Upload) bool { return u.ReceivedBytes == 8 })
	if len(u.PendingChunks) != 1 || u.PendingChunks[0].Index != 1 || u.PendingChunks[0].Bytes != 0 {
		t.Fatalf("remote commit exposed inconsistent pending snapshot: %+v", u)
	}
	if _, err := e.Complete(ctx, u.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("part still receiving was published: %v", err)
	}
	if _, err := writer.Write(data[8:]); err != nil {
		t.Fatal(err)
	}
	writer.Close()
	pendingSignal(t, finished)
	if partErr != nil {
		t.Fatal(partErr)
	}
	if _, err := e.QueueChunk(ctx, u.ID, 1); err != nil {
		t.Fatal(err)
	}
	pendingSignal(t, backend.started)
	backend.release <- struct{}{}
	waitPending(t, e, u.ID, func(u domain.Upload) bool { return u.ReceivedBytes == u.Size })
	manifest, err := e.Complete(ctx, u.ID)
	if err != nil || manifest.SHA256 != digest(data) {
		t.Fatalf("concurrent browser body changed content: %+v %v", manifest, err)
	}
}

type pendingBody struct {
	io.Reader
	entered chan struct{}
	once    sync.Once
}

func (b *pendingBody) Read(p []byte) (int, error) {
	b.once.Do(func() { close(b.entered) })
	return b.Reader.Read(p)
}

func TestPendingRemoteFailureDoesNotWaitForNextBrowserBody(t *testing.T) {
	ctx := context.Background()
	e, _ := transportEngine(t, t.TempDir(), nil, 8)
	started, release := make(chan struct{}), make(chan struct{})
	e.backend = &injectedStore{BlobStore: e.backend, put: func(context.Context, string, []byte) (string, error) {
		close(started)
		<-release
		return "", errors.New("injected remote outage")
	}}
	data := []byte("fixture!second!!")
	u := begin(t, e, data, digest(data))
	stagePending(t, e, u, 0, data[:8])
	if _, err := e.QueueChunk(ctx, u.ID, 0); err != nil {
		t.Fatal(err)
	}
	pendingWorker(t, e)
	pendingSignal(t, started)
	body, writer := io.Pipe()
	bodyEntered, finished := make(chan struct{}), make(chan struct{})
	go func() {
		_, _ = e.PutPart(ctx, u.ID, 1, 0, &pendingBody{Reader: body, entered: bodyEntered}, digest(data[8:]))
		close(finished)
	}()
	t.Cleanup(func() {
		writer.Close()
		body.Close()
		pendingSignal(t, finished)
	})
	pendingSignal(t, bodyEntered)
	close(release)
	u = waitPending(t, e, u.ID, func(u domain.Upload) bool {
		return len(u.PendingChunks) == 2 && u.PendingChunks[0].State == "failed"
	})
	if u.ReceivedBytes != 0 || u.PendingChunks[0].Bytes != 8 || u.PendingChunks[0].ErrorCode != "storage_unavailable" {
		t.Fatalf("remote failure lost accepted parts or claimed remote storage: %+v", u)
	}
	if _, err := e.Complete(ctx, u.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("failed remote save was published: %v", err)
	}
}

func TestPendingGlobalCapacityAndBadBodyRelease(t *testing.T) {
	ctx := context.Background()
	e, _ := transportEngine(t, t.TempDir(), nil, 8)
	data := []byte("fixture!fixture!")
	var uploads []domain.Upload
	for range 3 {
		uploads = append(uploads, begin(t, e, data, digest(data)))
	}
	if _, err := e.PutPart(ctx, uploads[0].ID, 0, 0, bytes.NewReader(nil), digest(data[:8])); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("invalid body accepted: %v", err)
	}
	var count int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM storage_pending_chunks`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("bad body leaked capacity: %d %v", count, err)
	}
	for _, u := range uploads[:2] {
		stagePending(t, e, u, 0, data[:8])
		stagePending(t, e, u, 1, data[8:])
	}
	if _, err := e.PutPart(ctx, uploads[2].ID, 0, 0, bytes.NewReader(data[:8]), digest(data[:8])); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("global staging window exceeded: %v", err)
	}
	if err := e.Cancel(ctx, uploads[0].ID); err != nil {
		t.Fatal(err)
	}
	stagePending(t, e, uploads[2], 0, data[:8])
}

func TestPendingConcurrentUploadsAndDuplicateQueueRetries(t *testing.T) {
	ctx := context.Background()
	e, _ := transportEngine(t, t.TempDir(), nil, 8)
	data := []byte("fixture!")
	var uploads []domain.Upload
	for range pendingGlobal + 2 {
		uploads = append(uploads, begin(t, e, data, digest(data)))
	}
	start := make(chan struct{})
	type result struct {
		upload    domain.Upload
		err       error
		remaining int
	}
	accepted := make(chan result, len(uploads))
	for _, upload := range uploads {
		go func() {
			<-start
			body := bytes.NewReader(data)
			u, err := e.PutPart(ctx, upload.ID, 0, 0, body, digest(data))
			accepted <- result{u, err, body.Len()}
		}()
	}
	close(start)
	var staged []domain.Upload
	for range uploads {
		select {
		case result := <-accepted:
			if result.err == nil {
				staged = append(staged, result.upload)
			} else if !errors.Is(result.err, domain.ErrConflict) || result.remaining != len(data) {
				t.Fatalf("capacity consumed rejected body or returned unexpected error: %+v", result)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("concurrent part acceptance stalled")
		}
	}
	if len(staged) != pendingGlobal {
		t.Fatalf("concurrent capacity admitted %d uploads, want %d", len(staged), pendingGlobal)
	}
	local := e.backend
	var calls atomic.Int32
	e.backend = &injectedStore{BlobStore: local, put: func(ctx context.Context, id string, sealed []byte) (string, error) {
		calls.Add(1)
		return local.Put(ctx, id, sealed)
	}}
	pendingWorker(t, e)
	errs := make(chan error, 2*len(staged))
	for _, upload := range staged {
		for range 2 {
			go func() { _, err := e.QueueChunk(ctx, upload.ID, 0); errs <- err }()
		}
	}
	for range 2 * len(staged) {
		select {
		case err := <-errs:
			if err != nil {
				t.Fatalf("duplicate queue retry: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("duplicate queue retry stalled")
		}
	}
	for _, upload := range staged {
		waitPending(t, e, upload.ID, func(u domain.Upload) bool { return u.ReceivedBytes == u.Size })
		manifest, err := e.Complete(ctx, upload.ID)
		if err != nil || manifest.SHA256 != digest(data) {
			t.Fatalf("concurrent upload content: %+v %v", manifest, err)
		}
	}
	if calls.Load() != pendingGlobal {
		t.Fatalf("duplicate retry created %d remote chunks, want %d", calls.Load(), pendingGlobal)
	}
}

func TestPendingRestartRecoversQueuedAndStoringChunks(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	e, db := transportEngine(t, dir, nil, 8)
	data := []byte("fixture!second!!")
	u := begin(t, e, data, digest(data))
	for index := range 2 {
		stagePending(t, e, u, index, data[index*8:(index+1)*8])
		if _, err := e.QueueChunk(ctx, u.ID, index); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`UPDATE storage_pending_chunks SET state='storing',attempts=1 WHERE upload_id=? AND idx=0`, u.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	e, _ = transportEngine(t, dir, nil, 8)
	pendingWorker(t, e)
	waitPending(t, e, u.ID, func(u domain.Upload) bool { return u.ReceivedBytes == u.Size })
	manifest, err := e.Complete(ctx, u.ID)
	if err != nil || manifest.SHA256 != digest(data) {
		t.Fatalf("restart lost chunk manifest: %+v %v", manifest, err)
	}
}

func TestPendingFailureRequiresExplicitRetryAndRepairsCorruptStage(t *testing.T) {
	for _, failure := range []string{"remote", "missing", "corrupt"} {
		t.Run(failure, func(t *testing.T) {
			ctx := context.Background()
			e, _ := transportEngine(t, t.TempDir(), nil, 8)
			local := e.backend
			var calls atomic.Int32
			e.backend = &injectedStore{BlobStore: local, put: func(ctx context.Context, key string, data []byte) (string, error) {
				if calls.Add(1) == 1 && failure == "remote" {
					return "", errors.New("injected remote failure")
				}
				return local.Put(ctx, key, data)
			}}
			data := []byte("fixture!")
			u := begin(t, e, data, digest(data))
			stagePending(t, e, u, 0, data)
			if _, err := e.QueueChunk(ctx, u.ID, 0); err != nil {
				t.Fatal(err)
			}
			parts, _ := e.stagedParts(ctx, u.ID)
			path := filepath.Join(e.staging.root, parts[0].ref)
			if failure == "missing" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			} else if failure == "corrupt" {
				sealed, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				sealed[len(sealed)-1] ^= 0x80
				if err := os.WriteFile(path, sealed, 0600); err != nil {
					t.Fatal(err)
				}
			}
			pendingWorker(t, e)
			waitPending(t, e, u.ID, func(u domain.Upload) bool {
				if len(u.PendingChunks) != 1 || u.PendingChunks[0].State != "failed" {
					return false
				}
				state, err := e.pendingState(ctx, u.ID, 0)
				return err == nil && state == "failed"
			})
			before := calls.Load()
			time.Sleep(10 * time.Millisecond)
			if calls.Load() != before {
				t.Fatal("failed background job retried without request")
			}
			if _, err := e.Complete(ctx, u.ID); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("failed save became complete: %v", err)
			}
			stagePending(t, e, u, 0, data)
			if _, err := e.QueueChunk(ctx, u.ID, 0); err != nil {
				t.Fatal(err)
			}
			waitPending(t, e, u.ID, func(u domain.Upload) bool { return u.ReceivedBytes == u.Size })
			manifest, err := e.Complete(ctx, u.ID)
			if err != nil || manifest.SHA256 != digest(data) {
				t.Fatalf("retry changed bytes: %+v %v", manifest, err)
			}
		})
	}
}

func TestPendingCancellationKeepsLateRemoteReferenceForCleanup(t *testing.T) {
	ctx := context.Background()
	e, _ := transportEngine(t, t.TempDir(), nil, 8)
	local := e.backend
	started, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	e.backend = &injectedStore{BlobStore: local, put: func(ctx context.Context, key string, data []byte) (string, error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		<-release
		return local.Put(context.Background(), key, data)
	}}
	data := []byte("fixture!")
	u := begin(t, e, data, digest(data))
	stagePending(t, e, u, 0, data)
	if _, err := e.QueueChunk(ctx, u.ID, 0); err != nil {
		t.Fatal(err)
	}
	pendingWorker(t, e)
	pendingSignal(t, started)
	finished := make(chan error, 1)
	go func() { finished <- e.Cancel(ctx, u.ID) }()
	pendingSignal(t, canceled)
	if _, err := e.PutPart(ctx, u.ID, 0, 0, bytes.NewReader(data), digest(data)); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("canceled upload accepted new bytes: %v", err)
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation failed to join active store")
	}
	if _, err := e.Status(ctx, u.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("canceled upload survived: %v", err)
	}
	entries, err := os.ReadDir(local.(*LocalStore).root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("late acknowledged ciphertext was orphaned: %d %v", len(entries), err)
	}
}

func TestPendingStaleSelectionAfterCancellationIsHarmless(t *testing.T) {
	ctx := context.Background()
	e, _ := transportEngine(t, t.TempDir(), nil, 8)
	data := []byte("fixture!")
	u := begin(t, e, data, digest(data))
	stagePending(t, e, u, 0, data)
	if _, err := e.QueueChunk(ctx, u.ID, 0); err != nil {
		t.Fatal(err)
	}
	workDone, err := e.workLock(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	release := func() { once.Do(workDone) }
	defer release()
	finished := make(chan error, 1)
	go func() { finished <- e.storePending(ctx, u.ID, 0) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		e.mu.Lock()
		waiting := e.workLocks[u.ID].refs == 2
		e.mu.Unlock()
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("selected worker did not wait for cancellation lock")
		}
		time.Sleep(time.Millisecond)
	}
	if err := e.cancelPending(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	// Model Cancel's deletion critical section after a worker selected the old ID.
	if err := e.clearStaged(ctx, u.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`DELETE FROM storage_uploads WHERE id=?`, u.ID); err != nil {
		t.Fatal(err)
	}
	release()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatalf("stale selection stopped worker: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stale worker did not release cancellation lock")
	}
}

func TestPendingCrashBudgetDoesNotBlockOtherUploads(t *testing.T) {
	for _, state := range []string{"queued", "storing"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			e, db := transportEngine(t, t.TempDir(), nil, 8)
			data := []byte("fixture!")
			exhausted := begin(t, e, data, digest(data))
			valid := begin(t, e, data, digest(data))
			for _, u := range []domain.Upload{exhausted, valid} {
				stagePending(t, e, u, 0, data)
				if _, err := e.QueueChunk(ctx, u.ID, 0); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.Exec(`UPDATE storage_pending_chunks SET state=?,attempts=3 WHERE upload_id=?`, state, exhausted.ID); err != nil {
				t.Fatal(err)
			}
			local := e.backend
			var exhaustedCalls, validCalls atomic.Int32
			e.backend = &injectedStore{BlobStore: local, put: func(ctx context.Context, id string, sealed []byte) (string, error) {
				if id == exhausted.ID {
					exhaustedCalls.Add(1)
				} else {
					validCalls.Add(1)
				}
				return local.Put(ctx, id, sealed)
			}}
			pendingWorker(t, e)
			waitPending(t, e, exhausted.ID, func(u domain.Upload) bool {
				return len(u.PendingChunks) == 1 && u.PendingChunks[0].State == "failed" && u.PendingChunks[0].ErrorCode == "storage_unavailable"
			})
			waitPending(t, e, valid.ID, func(u domain.Upload) bool { return u.ReceivedBytes == u.Size })
			if exhaustedCalls.Load() != 0 || validCalls.Load() != 1 {
				t.Fatalf("crash budget called exhausted=%d valid=%d", exhaustedCalls.Load(), validCalls.Load())
			}
			if _, err := e.Complete(ctx, exhausted.ID); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("exhausted upload became complete: %v", err)
			}
			if manifest, err := e.Complete(ctx, valid.ID); err != nil || manifest.SHA256 != digest(data) {
				t.Fatalf("valid upload blocked by exhausted job: %+v %v", manifest, err)
			}
		})
	}
}

func TestPendingCleanupFailureStopsWorkerAndRecovers(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	e, db := transportEngine(t, dir, nil, 8)
	data := []byte("fixture!")
	u := begin(t, e, data, digest(data))
	stagePending(t, e, u, 0, data)
	if _, err := e.QueueChunk(ctx, u.ID, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER fail_pending_cleanup BEFORE DELETE ON storage_parts
 BEGIN SELECT RAISE(ABORT,'injected staging cleanup interruption'); END`); err != nil {
		t.Fatal(err)
	}
	workerCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := e.RunPending(workerCtx); err == nil || workerCtx.Err() != nil {
		t.Fatalf("worker did not fail closed on staging cleanup: %v", err)
	}
	state, err := e.Status(ctx, u.ID)
	if err != nil || state.Status != "uploading" || state.ReceivedBytes != state.Size || len(state.PendingChunks) != 0 {
		t.Fatalf("committed manifest lost or prematurely published: %+v %v", state, err)
	}
	var parts int
	if err := db.QueryRow(`SELECT COUNT(*) FROM storage_parts WHERE upload_id=?`, u.ID).Scan(&parts); err != nil || parts != 1 {
		t.Fatalf("cleanup failure discarded retry metadata: %d %v", parts, err)
	}
	if _, err := db.Exec(`DROP TRIGGER fail_pending_cleanup`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	e, db = transportEngine(t, dir, nil, 8)
	manifest, err := e.Complete(ctx, u.ID)
	if err != nil || manifest.SHA256 != digest(data) {
		t.Fatalf("restart could not verify committed chunk: %+v %v", manifest, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM storage_parts WHERE upload_id=?`, u.ID).Scan(&parts); err != nil || parts != 0 {
		t.Fatalf("restart left disposable staging: %d %v", parts, err)
	}
}
