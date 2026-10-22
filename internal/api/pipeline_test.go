package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"private-drive/internal/domain"
	"private-drive/internal/storage"
)

type pipelineAPIStore struct {
	storage.BlobStore
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

type pipelineVerifyStore struct {
	storage.Service
	started           chan struct{}
	release           chan struct{}
	once              sync.Once
	afterVerification bool
}

func (s *pipelineVerifyStore) Complete(ctx context.Context, id string) (domain.Manifest, error) {
	var verified domain.Manifest
	if s.afterVerification {
		var err error
		verified, err = s.Service.Complete(ctx, id)
		if err != nil {
			return verified, err
		}
	}
	s.once.Do(func() { close(s.started) })
	select {
	case <-s.release:
		if s.afterVerification {
			return verified, nil
		}
		return s.Service.Complete(ctx, id)
	case <-ctx.Done():
		return domain.Manifest{}, ctx.Err()
	}
}

func TestCancelledVerifiedUploadCannotPublishStaleManifest(t *testing.T) {
	f := newFixture(t)
	f.login()
	u := decode[domain.Upload](t, f.request("POST", "/api/v1/uploads", domain.UploadSpec{Name: "cancel-before-publish.bin", Size: 4}, nil), 201)
	base := "/api/v1/uploads/" + u.ID
	decode[domain.Upload](t, f.request("PUT", base+"/chunks/0", []byte("data"), nil), 200)
	gate := &pipelineVerifyStore{Service: f.api.cfg.Storage, started: make(chan struct{}), release: make(chan struct{}), afterVerification: true}
	f.api.cfg.Storage = gate
	var release sync.Once
	defer release.Do(func() { close(gate.release) })
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- f.request("POST", base+"/complete", map[string]any{}, nil) }()
	select {
	case <-gate.started:
	case <-time.After(5 * time.Second):
		t.Fatal("verification never finished")
	}
	requireStatus(t, f.request("DELETE", base, nil, nil), 204)
	release.Do(func() { close(gate.release) })
	select {
	case response := <-done:
		requireStatus(t, response, 404)
	case <-time.After(5 * time.Second):
		t.Fatal("publication did not finish")
	}
	requireStatus(t, f.request("GET", "/api/v1/nodes/"+u.ID, nil, nil), 404)
	requireStatus(t, f.request("GET", base, nil, nil), 404)
}

func TestRemoteVerificationDoesNotBlockMetadataAndRechecksConflicts(t *testing.T) {
	f := newFixture(t)
	f.login()
	data := []byte("data")
	u := decode[domain.Upload](t, f.request("POST", "/api/v1/uploads", domain.UploadSpec{Name: "verification.bin", Size: 4}, nil), 201)
	decode[domain.Upload](t, f.request("PUT", "/api/v1/uploads/"+u.ID+"/chunks/0", data, nil), 200)
	gate := &pipelineVerifyStore{Service: f.api.cfg.Storage, started: make(chan struct{}), release: make(chan struct{})}
	f.api.cfg.Storage = gate
	var release sync.Once
	defer release.Do(func() { close(gate.release) })
	completed := make(chan *httptest.ResponseRecorder, 1)
	go func() { completed <- f.request("POST", "/api/v1/uploads/"+u.ID+"/complete", map[string]any{}, nil) }()
	select {
	case <-gate.started:
	case <-time.After(5 * time.Second):
		t.Fatal("verification never started")
	}
	folderDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		folderDone <- f.request("POST", "/api/v1/folders", map[string]string{"name": u.Name, "parent_id": ""}, nil)
	}()
	var conflict domain.Node
	select {
	case response := <-folderDone:
		conflict = decode[domain.Node](t, response, 201)
	case <-time.After(5 * time.Second):
		t.Fatal("remote verification blocked unrelated metadata work")
	}
	release.Do(func() { close(gate.release) })
	select {
	case response := <-completed:
		requireStatus(t, response, 409)
	case <-time.After(5 * time.Second):
		t.Fatal("verification did not finish")
	}
	requireStatus(t, f.request("GET", "/api/v1/nodes/"+u.ID, nil, nil), 404)
	requireStatus(t, f.request("DELETE", "/api/v1/nodes/"+conflict.ID, nil, nil), 204)
	decode[domain.Node](t, f.request("POST", "/api/v1/uploads/"+u.ID+"/complete", map[string]any{}, nil), 200)
}

func (b *pipelineAPIStore) Put(ctx context.Context, key string, data []byte) (string, error) {
	b.once.Do(func() { close(b.started) })
	select {
	case <-b.release:
		return b.BlobStore.Put(ctx, key, data)
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func TestBackgroundStoragePipelineHTTP(t *testing.T) {
	f := newFixture(t)
	local, err := storage.NewLocal(filepath.Join(f.dir, "pipeline-blobs"))
	if err != nil {
		t.Fatal(err)
	}
	backend := &pipelineAPIStore{BlobStore: local, started: make(chan struct{}), release: make(chan struct{})}
	engine, err := storage.New(storage.Config{DB: f.db, Key: bytes.Repeat([]byte{7}, 32), Backend: backend, ChunkSize: 8, StagingDir: filepath.Join(f.dir, "pipeline-parts")})
	if err != nil {
		t.Fatal(err)
	}
	f.api.cfg.Storage = engine
	requireStatus(t, f.request("POST", "/api/v1/uploads/missing/chunks/0/commit", map[string]any{"background": true}, nil), 401)
	f.login()
	payload := []byte("abcdefghijklmnopqrst")
	digest := fmt.Sprintf("%x", sha256.Sum256(payload))
	u := decode[domain.Upload](t, f.request("POST", "/api/v1/uploads", domain.UploadSpec{Name: "pipelined.bin", Size: int64(len(payload)), SHA256: digest}, nil), 201)
	base := "/api/v1/uploads/" + u.ID
	put := func(index int, status int) domain.Upload {
		t.Helper()
		data := payload[index*8 : min((index+1)*8, len(payload))]
		response := f.request("PUT", fmt.Sprintf("%s/chunks/%d/parts/0", base, index), data, map[string]string{"X-Part-SHA256": fmt.Sprintf("%x", sha256.Sum256(data))})
		if status != 200 {
			requireStatus(t, response, status)
			return domain.Upload{}
		}
		return decode[domain.Upload](t, response, 200)
	}
	queue := func(index int) domain.Upload {
		t.Helper()
		return decode[domain.Upload](t, f.request("POST", fmt.Sprintf("%s/chunks/%d/commit", base, index), map[string]any{"background": true}, nil), 202)
	}
	put(0, 200)
	requireStatus(t, f.request("POST", base+"/chunks/0/commit", map[string]any{"background": true}, map[string]string{"Origin": "https://hostile.test"}), 403)
	requireStatus(t, f.request("POST", base+"/chunks/0/commit", map[string]any{"background": "true"}, nil), 400)
	u = queue(0)
	if u.ReceivedBytes != 0 || u.MaxPendingChunks != 2 || len(u.PendingChunks) != 1 || u.PendingChunks[0].State != "queued" {
		t.Fatalf("queue must persist before adapter write: %+v", u)
	}
	queue(0) // A lost acknowledgement must not enqueue a second remote write.
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
		case <-time.After(5 * time.Second):
			t.Error("storage worker did not stop")
		}
	})
	select {
	case <-backend.started:
	case <-time.After(5 * time.Second):
		t.Fatal("background save never started")
	}
	u = put(1, 200) // This must return while chunk zero remains blocked in remote Put.
	if u.ReceivedBytes != 0 || len(u.PendingChunks) != 2 {
		t.Fatalf("next chunk was not accepted during saving: %+v", u)
	}
	queue(1)
	put(2, 409) // Two pending chunks apply backpressure without accepting extra bytes.
	requireStatus(t, f.request("POST", base+"/complete", map[string]any{}, nil), 409)
	requireStatus(t, f.request("GET", "/api/v1/nodes/"+u.ID, nil, nil), 404)
	close(backend.release)
	waitStored := func(count int) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			u = decode[domain.Upload](t, f.request("GET", base, nil, nil), 200)
			if len(u.ReceivedChunks) == count {
				return
			}
			time.Sleep(time.Millisecond * 5)
		}
		t.Fatalf("stored chunk count did not reach %d: %+v", count, u)
	}
	waitStored(2)
	put(2, 200)
	queue(2)
	waitStored(3)
	n := decode[domain.Node](t, f.request("POST", base+"/complete", map[string]any{}, nil), 200)
	response := f.request("GET", "/api/v1/nodes/"+n.ID+"/content", nil, nil)
	requireStatus(t, response, 200)
	if n.SHA256 != digest || !bytes.Equal(response.Body.Bytes(), payload) {
		t.Fatal("pipelined content failed checksum roundtrip")
	}
}
