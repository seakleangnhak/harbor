//go:build !windows

package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"modernc.org/sqlite"
	"private-drive/internal/domain"
	"private-drive/internal/storage"
)

const milestoneChunkSize = 8 << 20

func milestonePayload() []byte {
	data := make([]byte, milestoneChunkSize+17)
	for i := range data {
		data[i] = byte(i % 251)
	}
	return data
}

func milestoneHash(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }

type milestoneCrashStore struct {
	storage.BlobStore
	mode, dir string
	pause     func()
}

func (s *milestoneCrashStore) Put(ctx context.Context, key string, data []byte) (string, error) {
	if s.mode == "during_remote_write" {
		// Test-only half-written remote object: no adapter acknowledgement or manifest.
		if err := os.WriteFile(filepath.Join(s.dir, "blobs", ".interrupted-write"), data[:len(data)/2], 0600); err != nil {
			return "", err
		}
		s.pause()
	}
	ref, err := s.BlobStore.Put(ctx, key, data)
	if err == nil && s.mode == "after_remote_ack" {
		s.pause()
	}
	return ref, err
}

func (s *milestoneCrashStore) Get(ctx context.Context, ref string) ([]byte, error) {
	if s.mode == "during_verification" {
		s.pause()
	}
	return s.BlobStore.Get(ctx, ref)
}

type milestoneVerifiedGate struct {
	storage.Service
	pause func()
}

func (s *milestoneVerifiedGate) Complete(ctx context.Context, id string) (domain.Manifest, error) {
	m, err := s.Service.Complete(ctx, id)
	if err == nil {
		s.pause()
	}
	return m, err
}

func milestoneFixture(t *testing.T, dir string, backend storage.BlobStore, chunkSize int64) (*fixture, *storage.Engine) {
	t.Helper()
	db, err := OpenDB(filepath.Join(dir, "drive.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	// A killed transaction rolls back before reopening; remove only test hooks.
	if _, err := db.Exec(`DROP TRIGGER IF EXISTS milestone_a_gate`); err != nil {
		t.Fatal(err)
	}
	if backend == nil {
		backend, err = storage.NewLocal(filepath.Join(dir, "blobs"))
		if err != nil {
			t.Fatal(err)
		}
	}
	engine, err := storage.New(storage.Config{DB: db, Key: bytes.Repeat([]byte{7}, 32), Backend: backend, ChunkSize: chunkSize, StagingDir: filepath.Join(dir, "parts")})
	if err != nil {
		t.Fatal(err)
	}
	app, err := New(Config{DB: db, Storage: engine, OwnerPassword: testPassword, Adapter: "local"})
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, dir: dir, db: db, api: app}
	f.login()
	return f, engine
}

func milestoneStage(t *testing.T, f *fixture, u domain.Upload, data []byte) {
	t.Helper()
	for index := 0; index < u.TotalChunks; index++ {
		end := min(int64(len(data)), int64(index+1)*u.ChunkSize)
		for part, offset := 0, int64(index)*u.ChunkSize; offset < end; part, offset = part+1, offset+u.PartSize {
			body := data[offset:min(end, offset+u.PartSize)]
			decode[domain.Upload](t, f.request("PUT", fmt.Sprintf("/api/v1/uploads/%s/chunks/%d/parts/%d", u.ID, index, part), body, map[string]string{"X-Part-SHA256": milestoneHash(body)}), 200)
		}
	}
}

func milestoneWaitStored(t *testing.T, engine *storage.Engine, id string) domain.Upload {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for {
		u, err := engine.Status(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if u.ReceivedBytes == u.Size {
			return u
		}
		select {
		case <-ctx.Done():
			t.Fatalf("pending worker stalled: %+v", u)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestMilestoneACrashChild(t *testing.T) {
	mode, dir := os.Getenv("HARBOR_MILESTONE_CRASH_MODE"), os.Getenv("HARBOR_MILESTONE_CRASH_DIR")
	if mode == "" {
		t.Skip("child process only")
	}
	pause := func() {
		if err := os.WriteFile(filepath.Join(dir, "ready"), []byte(mode), 0600); err != nil {
			t.Fatal(err)
		}
		for {
			time.Sleep(time.Hour)
		}
	}
	if err := sqlite.RegisterScalarFunction("milestone_a_pause", 0, func(*sqlite.FunctionContext, []driver.Value) (driver.Value, error) { pause(); return int64(0), nil }); err != nil {
		t.Fatal(err)
	}
	local, err := storage.NewLocal(filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	remote := &milestoneCrashStore{BlobStore: local, dir: dir, pause: pause}
	f, engine := milestoneFixture(t, dir, remote, milestoneChunkSize)
	existing := copyFile(t, f, "", []byte("preexisting completed content stays unchanged"))
	data := milestonePayload()
	u := decode[domain.Upload](t, f.request("POST", "/api/v1/uploads", domain.UploadSpec{Name: "pending.bin", Size: int64(len(data)), SHA256: milestoneHash(data)}, nil), 201)
	state, _ := json.Marshal(map[string]string{"upload": u.ID, "existing": existing.ID})
	if err := os.WriteFile(filepath.Join(dir, "state.json"), state, 0600); err != nil {
		t.Fatal(err)
	}
	trigger := ""
	switch mode {
	case "before_part_ack":
		trigger = "AFTER INSERT ON storage_parts"
	case "before_manifest_commit":
		trigger = "AFTER DELETE ON storage_pending_chunks"
	}
	if trigger != "" {
		if _, err := f.db.Exec("CREATE TRIGGER milestone_a_gate " + trigger + " BEGIN SELECT milestone_a_pause(); END"); err != nil {
			t.Fatal(err)
		}
	}
	first := data[:u.PartSize]
	decode[domain.Upload](t, f.request("PUT", fmt.Sprintf("/api/v1/uploads/%s/chunks/0/parts/0", u.ID), first, map[string]string{"X-Part-SHA256": milestoneHash(first)}), 200)
	if mode == "after_part_ack" {
		pause()
	}
	milestoneStage(t, f, u, data) // Repeated accepted part must stay idempotent.
	if mode == "before_queue" {
		pause()
	}
	for index := 0; index < u.TotalChunks; index++ {
		decode[domain.Upload](t, f.request("POST", fmt.Sprintf("/api/v1/uploads/%s/chunks/%d/commit", u.ID, index), map[string]any{"background": true}, nil), 202)
	}
	if mode == "after_queue" {
		pause()
	}
	if mode == "during_remote_write" || mode == "after_remote_ack" {
		remote.mode = mode
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		if err := engine.RunPending(ctx); err != nil {
			t.Error(err)
		}
	}()
	milestoneWaitStored(t, engine, u.ID)
	if mode == "during_verification" {
		remote.mode = mode
	}
	if mode == "verified_before_publication" {
		f.api.cfg.Storage = &milestoneVerifiedGate{Service: engine, pause: pause}
	}
	decode[domain.Node](t, f.request("POST", "/api/v1/uploads/"+u.ID+"/complete", map[string]any{}, nil), 200)
	t.Fatal("crash boundary was not reached")
}

func TestMilestoneAUploadProcessCrashMatrix(t *testing.T) {
	for _, mode := range []string{"before_part_ack", "after_part_ack", "before_queue", "after_queue", "during_remote_write", "after_remote_ack", "before_manifest_commit", "during_verification", "verified_before_publication"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(executable, "-test.run=^TestMilestoneACrashChild$", "-test.timeout=2m")
			cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "TMPDIR=" + dir, "GORACE=halt_on_error=1", "HARBOR_MILESTONE_CRASH_MODE=" + mode, "HARBOR_MILESTONE_CRASH_DIR=" + dir}
			var output bytes.Buffer
			cmd.Stdout, cmd.Stderr = &output, &output
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = cmd.Process.Kill() })
			deadline := time.Now().Add(30 * time.Second)
			for {
				if _, err := os.Stat(filepath.Join(dir, "ready")); err == nil {
					break
				}
				if time.Now().After(deadline) {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
					t.Fatalf("child failed to reach %s: %s", mode, output.String())
				}
				time.Sleep(5 * time.Millisecond)
			}
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			var exit *exec.ExitError
			waitErr := cmd.Wait()
			if bytes.Contains(output.Bytes(), []byte("WARNING: DATA RACE")) {
				t.Fatalf("race detected in crash child: %s", output.String())
			}
			if !errors.As(waitErr, &exit) || exit.ProcessState.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
				t.Fatalf("child did not terminate with SIGKILL: %v", waitErr)
			}
			var ids map[string]string
			state, err := os.ReadFile(filepath.Join(dir, "state.json"))
			if err != nil || json.Unmarshal(state, &ids) != nil {
				t.Fatal("child fixture identity missing")
			}
			f, engine := milestoneFixture(t, dir, nil, milestoneChunkSize)
			var integrity string
			if err := f.db.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
				t.Fatalf("crash damaged SQLite: %s %v", integrity, err)
			}
			requireStatus(t, f.request("GET", "/api/v1/nodes/"+ids["upload"], nil, nil), 404)
			existing := f.request("GET", "/api/v1/nodes/"+ids["existing"]+"/content", nil, nil)
			requireStatus(t, existing, 200)
			if existing.Body.String() != "preexisting completed content stays unchanged" {
				t.Fatal("completed source file changed")
			}
			u, err := engine.Status(context.Background(), ids["upload"])
			if err != nil {
				t.Fatal(err)
			}
			accepted := u.ReceivedBytes
			for _, chunk := range u.PendingChunks {
				accepted += chunk.Bytes
			}
			wantAccepted := u.Size
			if mode == "before_part_ack" {
				wantAccepted = 0
			} else if mode == "after_part_ack" {
				wantAccepted = storage.TransportPartSize
			}
			if accepted != wantAccepted || (u.Status == "complete") != (mode == "verified_before_publication") {
				t.Fatalf("lost acknowledgement or premature completion: %+v accepted=%d want=%d", u, accepted, wantAccepted)
			}
			needsOriginal := accepted != u.Size
			if needsOriginal {
				milestoneStage(t, f, u, milestonePayload())
			}
			for _, chunk := range u.PendingChunks {
				if chunk.State == "receiving" {
					decode[domain.Upload](t, f.request("POST", fmt.Sprintf("/api/v1/uploads/%s/chunks/%d/commit", u.ID, chunk.Index), map[string]any{"background": true}, nil), 202)
				}
			}
			if needsOriginal {
				for index := 0; index < u.TotalChunks; index++ {
					decode[domain.Upload](t, f.request("POST", fmt.Sprintf("/api/v1/uploads/%s/chunks/%d/commit", u.ID, index), map[string]any{"background": true}, nil), 202)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- engine.RunPending(ctx) }()
			t.Cleanup(func() {
				cancel()
				select {
				case err := <-done:
					if err != nil {
						t.Error(err)
					}
				case <-time.After(10 * time.Second):
					t.Error("recovered worker did not join")
				}
			})
			milestoneWaitStored(t, engine, u.ID)
			n := decode[domain.Node](t, f.request("POST", "/api/v1/uploads/"+u.ID+"/complete", map[string]any{}, nil), 200)
			decode[domain.Node](t, f.request("POST", "/api/v1/uploads/"+u.ID+"/complete", map[string]any{}, nil), 200)
			response := f.request("GET", "/api/v1/nodes/"+n.ID+"/content", nil, nil)
			requireStatus(t, response, 200)
			if got := response.Body.Bytes(); n.SHA256 != milestoneHash(milestonePayload()) || !bytes.Equal(got, milestonePayload()) {
				t.Fatal("crash recovery changed encrypted content")
			}
			var chunks, nodes int
			if err := f.db.QueryRow("SELECT count(*) FROM storage_chunks WHERE upload_id=?", u.ID).Scan(&chunks); err != nil || chunks != u.TotalChunks {
				t.Fatalf("duplicate/incomplete manifest: %d %v", chunks, err)
			}
			if err := f.db.QueryRow("SELECT count(*) FROM nodes WHERE id=?", u.ID).Scan(&nodes); err != nil || nodes != 1 {
				t.Fatalf("duplicate publication: %d %v", nodes, err)
			}
			files, err := os.ReadDir(filepath.Join(dir, "blobs"))
			var tracked int
			if err != nil {
				t.Fatal(err)
			}
			if err := f.db.QueryRow("SELECT count(*) FROM storage_chunks").Scan(&tracked); err != nil {
				t.Fatal(err)
			}
			orphans := len(files) - tracked
			wantOrphans := 0
			if mode == "during_remote_write" || mode == "after_remote_ack" || mode == "before_manifest_commit" {
				wantOrphans = 1
			}
			if orphans != wantOrphans {
				t.Fatalf("unexpected remote orphan window: %d want=%d", orphans, wantOrphans)
			}
			t.Logf("SIGKILL boundary=%s accepted=%d remotely_stored=%d original_file_required=%t remote_orphan_candidates=%d recovery_checksum=PASS existing_file=unchanged", mode, accepted, u.ReceivedBytes, needsOriginal, orphans)
		})
	}
}

type milestoneMixedStore struct {
	storage.BlobStore
	started, release chan struct{}
	once             sync.Once
	active, peak     atomic.Int32
}

func (s *milestoneMixedStore) Put(ctx context.Context, key string, data []byte) (string, error) {
	active := s.active.Add(1)
	defer s.active.Add(-1)
	for peak := s.peak.Load(); active > peak && !s.peak.CompareAndSwap(peak, active); peak = s.peak.Load() {
	}
	s.once.Do(func() { close(s.started) })
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-s.release:
		return s.BlobStore.Put(ctx, key, data)
	}
}

func TestMilestoneAConcurrentCopyAndQueuedUpload(t *testing.T) {
	dir := t.TempDir()
	f, _ := milestoneFixture(t, dir, nil, milestoneChunkSize)
	sourceFolder := f.folder("Source", "")
	data := milestonePayload()
	source := copyFile(t, f, sourceFolder.ID, data)
	local, err := storage.NewLocal(filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	backend := &milestoneMixedStore{BlobStore: local, started: make(chan struct{}), release: make(chan struct{})}
	engine, err := storage.New(storage.Config{DB: f.db, Key: bytes.Repeat([]byte{7}, 32), Backend: backend, ChunkSize: milestoneChunkSize, StagingDir: filepath.Join(dir, "parts")})
	if err != nil {
		t.Fatal(err)
	}
	f.api.cfg.Storage = engine
	job, _, err := f.api.planCopy(context.Background(), copyRequest{OperationID: domain.NewID(), SourceIDs: []string{sourceFolder.ID}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 2)
	go func() { done <- f.api.RunCopyJobs(ctx) }()
	go func() { done <- engine.RunPending(ctx) }()
	var release sync.Once
	t.Cleanup(func() {
		release.Do(func() { close(backend.release) })
		cancel()
		for range 2 {
			select {
			case err := <-done:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(10 * time.Second):
				t.Error("mixed workers failed to join")
			}
		}
	})
	select {
	case <-backend.started:
	case <-time.After(10 * time.Second):
		t.Fatal("copy remote write did not start")
	}
	u := decode[domain.Upload](t, f.request("POST", "/api/v1/uploads", domain.UploadSpec{Name: "parallel.bin", Size: int64(len(data)), SHA256: milestoneHash(data)}, nil), 201)
	milestoneStage(t, f, u, data)
	for index := 0; index < u.TotalChunks; index++ {
		queued := decode[domain.Upload](t, f.request("POST", fmt.Sprintf("/api/v1/uploads/%s/chunks/%d/commit", u.ID, index), map[string]any{"background": true}, nil), 202)
		if queued.ReceivedBytes != 0 {
			t.Fatal("queued upload bypassed blocked remote writer")
		}
	}
	requireStatus(t, f.request("GET", "/api/v1/nodes/"+u.ID, nil, nil), 404)
	release.Do(func() { close(backend.release) })
	milestoneWaitStored(t, engine, u.ID)
	n := decode[domain.Node](t, f.request("POST", "/api/v1/uploads/"+u.ID+"/complete", map[string]any{}, nil), 200)
	deadline := time.Now().Add(15 * time.Second)
	for {
		job, err = f.api.copyJob(context.Background(), job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == "complete" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("copy blocked by parallel upload: %+v", job)
		}
		time.Sleep(5 * time.Millisecond)
	}
	for _, id := range []string{source.ID, n.ID} {
		response := f.request("GET", "/api/v1/nodes/"+id+"/content", nil, nil)
		requireStatus(t, response, 200)
		if !bytes.Equal(response.Body.Bytes(), data) {
			t.Fatal("mixed copy/upload changed file bytes")
		}
	}
	listing := decode[struct {
		Items []domain.Node `json:"items"`
	}](t, f.request("GET", "/api/v1/nodes?parent_id="+job.ResultIDs[0], nil, nil), 200)
	if len(listing.Items) != 1 || listing.Items[0].SHA256 != source.SHA256 || backend.peak.Load() != 1 {
		t.Fatalf("copy manifest or writer bound violated: %+v peak=%d", listing, backend.peak.Load())
	}
}

type milestonePattern struct{}

func (milestonePattern) ReadAt(p []byte, offset int64) (int, error) {
	for i := range p {
		p[i] = byte((offset + int64(i)) % 251)
	}
	return len(p), nil
}

func milestonePatternHash(t *testing.T, size int64) string {
	t.Helper()
	h := sha256.New()
	if _, err := io.Copy(h, io.NewSectionReader(milestonePattern{}, 0, size)); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func TestMilestoneAProductionMemoryBudget(t *testing.T) {
	if os.Getenv("HARBOR_MILESTONE_MEMORY") != "1" {
		t.Skip("run separately with 768 MiB container memory and GOMEMLIMIT=512MiB")
	}
	dir := t.TempDir()
	f, sourceEngine := milestoneFixture(t, dir, nil, storage.DefaultChunkSize)
	sourceFolder := f.folder("Memory source", "")
	size := storage.DefaultChunkSize
	hash := milestonePatternHash(t, size)
	source, err := sourceEngine.Begin(context.Background(), domain.UploadSpec{Name: "source.bin", ParentID: sourceFolder.ID, Size: size, SHA256: hash})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sourceEngine.PutChunk(context.Background(), source.ID, 0, io.NewSectionReader(milestonePattern{}, 0, size), hash); err != nil {
		t.Fatal(err)
	}
	decode[domain.Node](t, f.request("POST", "/api/v1/uploads/"+source.ID+"/complete", map[string]any{}, nil), 200)
	local, err := storage.NewLocal(filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	backend := &milestoneMixedStore{BlobStore: local, started: make(chan struct{}), release: make(chan struct{})}
	engine, err := storage.New(storage.Config{DB: f.db, Key: bytes.Repeat([]byte{7}, 32), Backend: backend, ChunkSize: size, StagingDir: filepath.Join(dir, "parts")})
	if err != nil {
		t.Fatal(err)
	}
	f.api.cfg.Storage = engine
	job, _, err := f.api.planCopy(context.Background(), copyRequest{OperationID: domain.NewID(), SourceIDs: []string{sourceFolder.ID}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 2)
	go func() { done <- f.api.RunCopyJobs(ctx) }()
	go func() { done <- engine.RunPending(ctx) }()
	var release sync.Once
	t.Cleanup(func() {
		release.Do(func() { close(backend.release) })
		cancel()
		for range 2 {
			select {
			case err := <-done:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(15 * time.Second):
				t.Error("production-geometry workers did not join")
			}
		}
	})
	select {
	case <-backend.started:
	case <-time.After(15 * time.Second):
		t.Fatal("production-geometry copy did not start")
	}
	u := decode[domain.Upload](t, f.request("POST", "/api/v1/uploads", domain.UploadSpec{Name: "background.bin", Size: size + 17, SHA256: milestonePatternHash(t, size+17)}, nil), 201)
	for index := 0; index < u.TotalChunks; index++ {
		end := min(u.Size, int64(index+1)*u.ChunkSize)
		for part, offset := 0, int64(index)*u.ChunkSize; offset < end; part, offset = part+1, offset+u.PartSize {
			body := make([]byte, min(u.PartSize, end-offset))
			_, _ = (milestonePattern{}).ReadAt(body, offset)
			decode[domain.Upload](t, f.request("PUT", fmt.Sprintf("/api/v1/uploads/%s/chunks/%d/parts/%d", u.ID, index, part), body, map[string]string{"X-Part-SHA256": milestoneHash(body)}), 200)
		}
		queued := decode[domain.Upload](t, f.request("POST", fmt.Sprintf("/api/v1/uploads/%s/chunks/%d/commit", u.ID, index), map[string]any{"background": true}, nil), 202)
		if queued.ReceivedBytes != 0 || len(queued.PendingChunks) > 2 {
			t.Fatal("pending bytes bypassed remote acknowledgement or queue cap")
		}
	}
	var pending int
	if err := f.db.QueryRow("SELECT count(*) FROM storage_pending_chunks").Scan(&pending); err != nil || pending > 4 {
		t.Fatalf("global queue cap violated: %d %v", pending, err)
	}
	ranges := make(chan error, 2)
	for _, start := range []int64{0, 128 << 10} {
		go func(start int64) {
			req := httptest.NewRequest("GET", "http://drive.test/api/v1/nodes/"+source.ID+"/content", nil)
			req.AddCookie(f.cookie)
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, start+(256<<10)-1))
			response := httptest.NewRecorder()
			f.api.Handler().ServeHTTP(response, req)
			expected := make([]byte, 256<<10)
			_, _ = (milestonePattern{}).ReadAt(expected, start)
			if response.Code != 206 || !bytes.Equal(expected, response.Body.Bytes()) {
				ranges <- fmt.Errorf("overlapping preview range failed: status=%d", response.Code)
				return
			}
			ranges <- nil
		}(start)
	}
	release.Do(func() { close(backend.release) })
	milestoneWaitStored(t, engine, u.ID)
	decode[domain.Node](t, f.request("POST", "/api/v1/uploads/"+u.ID+"/complete", map[string]any{}, nil), 200)
	for range 2 {
		select {
		case err := <-ranges:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(15 * time.Second):
			t.Fatal("preview deadlocked behind copy/upload")
		}
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		job, err = f.api.copyJob(context.Background(), job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == "complete" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("production-geometry copy did not complete: %+v", job)
		}
		time.Sleep(10 * time.Millisecond)
	}
	listing := decode[struct {
		Items []domain.Node `json:"items"`
	}](t, f.request("GET", "/api/v1/nodes?parent_id="+job.ResultIDs[0], nil, nil), 200)
	if len(listing.Items) != 1 || backend.peak.Load() != 1 {
		t.Fatal("copy publication or single writer invariant failed")
	}
	for _, id := range []string{source.ID, u.ID, listing.Items[0].ID} {
		reader, manifest, err := engine.Open(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		h := sha256.New()
		_, copyErr := io.Copy(h, reader)
		closeErr := reader.Close()
		if copyErr != nil || closeErr != nil || fmt.Sprintf("%x", h.Sum(nil)) != manifest.SHA256 {
			t.Fatalf("production-geometry integrity failed: %v %v", copyErr, closeErr)
		}
	}
	t.Logf("local mixed workload PASS storage_chunk_bytes=%d upload_bytes=%d copy_bytes=%d range_requests=2 pending_at_block=%d peak_remote_writers=%d", size, u.Size, size, pending, backend.peak.Load())
}
