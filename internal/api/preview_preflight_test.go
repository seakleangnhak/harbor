package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"private-drive/internal/domain"
	"private-drive/internal/storage"
)

type previewProbeService struct {
	storage.Service
	reads atomic.Int64
	fail  atomic.Bool
	first atomic.Int64
}

func (s *previewProbeService) Open(ctx context.Context, id string) (storage.ReadSeekCloser, domain.Manifest, error) {
	r, manifest, err := s.Service.Open(ctx, id)
	return previewProbeReader{r, s}, manifest, err
}

type previewProbeReader struct {
	storage.ReadSeekCloser
	service *previewProbeService
}

func (r previewProbeReader) Read(p []byte) (int, error) {
	if r.service.reads.Add(1) == 1 {
		offset, _ := r.ReadSeekCloser.Seek(0, io.SeekCurrent)
		r.service.first.Store(offset)
	}
	if r.service.fail.Load() {
		return 0, domain.ErrIntegrity
	}
	return r.ReadSeekCloser.Read(p)
}

func TestPreviewMetadataAvoidsAuthenticatedRemoteReads(t *testing.T) {
	f := newFixture(t)
	f.login()
	payload := []byte("preview-fixture-content")
	node := copyFile(t, f, "", payload)
	service := &previewProbeService{Service: f.api.cfg.Storage}
	f.api.cfg.Storage = service
	path := "/api/v1/nodes/" + node.ID + "/content"
	etag := `"` + node.SHA256 + `"`
	for _, test := range []struct {
		name, method string
		headers      map[string]string
		status       int
	}{
		{"head", "HEAD", nil, 200},
		{"head-range", "HEAD", map[string]string{"Range": "bytes=7-7"}, 206},
		{"etag", "GET", map[string]string{"If-None-Match": etag}, 304},
		{"weak-etag", "GET", map[string]string{"If-None-Match": "W/" + etag}, 304},
		{"precondition", "GET", map[string]string{"If-Match": `"different"`}, 412},
		{"invalid", "GET", map[string]string{"Range": "bytes=oops"}, 416},
		{"unsatisfiable", "GET", map[string]string{"Range": "bytes=999-"}, 416},
	} {
		t.Run(test.name, func(t *testing.T) {
			service.reads.Store(0)
			response := f.request(test.method, path, nil, test.headers)
			requireStatus(t, response, test.status)
			if service.reads.Load() != 0 {
				t.Fatal("metadata-only response fetched authenticated content")
			}
		})
	}
	for _, test := range []struct {
		name    string
		headers map[string]string
		status  int
		body    []byte
	}{
		{"full", nil, 200, payload},
		{"suffix", map[string]string{"Range": "bytes=-3"}, 206, payload[len(payload)-3:]},
		{"boundary", map[string]string{"Range": "bytes=7-7"}, 206, payload[7:8]},
		{"if-range-match", map[string]string{"Range": "bytes=8-10", "If-Range": etag}, 206, payload[8:11]},
		{"if-range-fallback", map[string]string{"Range": "bytes=8-10", "If-Range": `"different"`}, 200, payload},
		{"skip-unsatisfiable-prefix", map[string]string{"Range": "bytes=999-,8-10"}, 206, payload[8:11]},
		{"oversized-multirange", map[string]string{"Range": "bytes=0-22,0-22"}, 200, payload},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := f.request("GET", path, nil, test.headers)
			requireStatus(t, response, test.status)
			if !bytes.Equal(response.Body.Bytes(), test.body) {
				t.Fatalf("incorrect range body %q", response.Body.Bytes())
			}
		})
	}
	service.fail.Store(true)
	service.reads.Store(0)
	response := f.request("GET", path, nil, map[string]string{"Range": "bytes=999-,8-10,16-18"})
	requireStatus(t, response, http.StatusConflict)
	if service.first.Load() != 8 {
		t.Fatal("preflight fetched an unsatisfiable prefix instead of the first served chunk")
	}
	if response.Header().Get("Content-Type") != "application/json" || !bytes.Contains(response.Body.Bytes(), []byte("integrity_failed")) {
		t.Fatal("failed authentication emitted successful preview headers")
	}
}
