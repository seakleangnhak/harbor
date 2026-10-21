package api

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"private-drive/internal/domain"
	"private-drive/internal/storage"
)

func enableTransport(t *testing.T, f *fixture, chunkSize int64) {
	t.Helper()
	backend, err := storage.NewLocal(filepath.Join(f.dir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	engine, err := storage.New(storage.Config{DB: f.db, Key: bytes.Repeat([]byte{7}, 32), Backend: backend, ChunkSize: chunkSize, StagingDir: filepath.Join(f.dir, "upload-parts")})
	if err != nil {
		t.Fatal(err)
	}
	f.api.cfg.Storage = engine
}

func TestTransportResumeCommitAndVisibility(t *testing.T) {
	f := newFixture(t)
	enableTransport(t, f, storage.TransportPartSize+8)
	f.login()
	payload := bytes.Repeat([]byte("transport boundary fixture"), int(storage.TransportPartSize/26)+1)
	payload = append(payload[:storage.TransportPartSize], []byte("final twelve")...)
	digest := fmt.Sprintf("%x", sha256.Sum256(payload))
	u := decode[domain.Upload](t, f.request("POST", "/api/v1/uploads", domain.UploadSpec{Name: "parts.bin", Size: int64(len(payload)), SHA256: digest}, nil), 201)
	if u.PartSize != storage.TransportPartSize || u.TotalChunks != 2 {
		t.Fatalf("unexpected geometry: %+v", u)
	}
	base := "/api/v1/uploads/" + u.ID + "/chunks/"
	put := func(index, part int, data []byte) domain.Upload {
		t.Helper()
		return decode[domain.Upload](t, f.request("PUT", fmt.Sprintf("%s%d/parts/%d", base, index, part), data, map[string]string{"Content-Type": "application/octet-stream", "X-Part-SHA256": fmt.Sprintf("%x", sha256.Sum256(data))}), 200)
	}
	first := payload[:storage.TransportPartSize]
	u = put(0, 0, first)
	if u.ReceivedBytes != 0 || u.StagedBytes != storage.TransportPartSize || len(u.StagedParts) != 1 {
		t.Fatalf("staging counted as storage: %+v", u)
	}
	put(0, 0, first) // Lost acknowledgement must not duplicate accepted bytes.
	requireStatus(t, f.request("POST", base+"0/commit", map[string]any{}, nil), 409)
	requireStatus(t, f.request("POST", "/api/v1/uploads/"+u.ID+"/complete", map[string]any{}, nil), 409)
	requireStatus(t, f.request("GET", "/api/v1/nodes/"+u.ID, nil, nil), 404)
	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	f.start("")
	enableTransport(t, f, storage.DefaultChunkSize) // Persisted geometry wins after restart.
	u = decode[domain.Upload](t, f.request("GET", "/api/v1/uploads/"+u.ID, nil, nil), 200)
	if u.ChunkSize != storage.TransportPartSize+8 || u.StagedBytes != storage.TransportPartSize {
		t.Fatalf("staging lost on restart: %+v", u)
	}
	put(0, 1, payload[storage.TransportPartSize:storage.TransportPartSize+8])
	u = decode[domain.Upload](t, f.request("POST", base+"0/commit", map[string]any{}, nil), 200)
	if u.ReceivedBytes != u.ChunkSize || u.StagedBytes != 0 || len(u.ReceivedChunks) != 1 {
		t.Fatalf("storage commit not acknowledged: %+v", u)
	}
	decode[domain.Upload](t, f.request("POST", base+"0/commit", map[string]any{}, nil), 200)
	requireStatus(t, f.request("GET", "/api/v1/nodes/"+u.ID, nil, nil), 404)
	put(1, 0, payload[storage.TransportPartSize+8:])
	decode[domain.Upload](t, f.request("POST", base+"1/commit", map[string]any{}, nil), 200)
	n := decode[domain.Node](t, f.request("POST", "/api/v1/uploads/"+u.ID+"/complete", map[string]any{}, nil), 200)
	response := f.request("GET", "/api/v1/nodes/"+n.ID+"/content?download=1", nil, nil)
	requireStatus(t, response, 200)
	if n.SHA256 != digest || !bytes.Equal(response.Body.Bytes(), payload) {
		t.Fatal("transport roundtrip changed plaintext")
	}
	requireStatus(t, f.request("DELETE", "/api/v1/uploads/"+u.ID, nil, nil), 409)
}

func TestTransportBoundariesAndAccess(t *testing.T) {
	f := newFixture(t)
	enableTransport(t, f, 8)
	partPath := "/api/v1/uploads/missing/chunks/0/parts/0"
	commitPath := "/api/v1/uploads/missing/chunks/0/commit"
	requireStatus(t, f.request("PUT", partPath, []byte("abcd"), nil), 401)
	requireStatus(t, f.request("POST", commitPath, map[string]any{}, nil), 401)
	f.login()
	u := decode[domain.Upload](t, f.request("POST", "/api/v1/uploads", domain.UploadSpec{Name: "boundaries.bin", Size: 4}, nil), 201)
	partPath = strings.ReplaceAll(partPath, "missing", u.ID)
	commitPath = strings.ReplaceAll(commitPath, "missing", u.ID)
	headers := map[string]string{"Content-Type": "application/octet-stream", "X-Part-SHA256": fmt.Sprintf("%x", sha256.Sum256([]byte("abcd")))}
	requireStatus(t, f.request("PUT", partPath, []byte("abcd"), nil), 400)
	requireStatus(t, f.request("PUT", partPath, []byte("abcd"), map[string]string{"X-Part-SHA256": "invalid"}), 400)
	requireStatus(t, f.request("PUT", partPath, []byte("abcdefgh!"), headers), 413)
	requireStatus(t, f.request("PUT", partPath, []byte("abc"), headers), 400)
	requireStatus(t, f.request("PUT", strings.TrimSuffix(partPath, "0")+"-1", []byte("abcd"), headers), 400)
	headers["Origin"] = "https://hostile.test"
	requireStatus(t, f.request("PUT", partPath, []byte("abcd"), headers), 403)
	requireStatus(t, f.request("POST", commitPath, map[string]any{}, headers), 403)
	delete(headers, "Origin")
	requireStatus(t, f.request("PUT", partPath, []byte("abce"), headers), 409)
	requireStatus(t, f.request("PUT", partPath, []byte("abcd"), headers), 200)
	requireStatus(t, f.request("POST", commitPath, map[string]any{"unexpected": true}, nil), 400)
	requireStatus(t, f.request("POST", commitPath, map[string]any{}, nil), 200)
	requireStatus(t, f.request("PUT", partPath, []byte("abcd"), headers), 409)
	if _, err := f.db.Exec("UPDATE storage_uploads SET parent_id='copy:private-job' WHERE id=?", u.ID); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, f.request("PUT", partPath, []byte("abcd"), headers), http.StatusNotFound)
	requireStatus(t, f.request("POST", commitPath, map[string]any{}, nil), http.StatusNotFound)
}
