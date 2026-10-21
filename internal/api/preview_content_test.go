package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"private-drive/internal/domain"
)

func TestInlineContentType(t *testing.T) {
	for _, test := range []struct{ name, declared, want string }{
		{"photo.jpeg", "", "image/jpeg"},
		{"PHOTO.JPG", "application/octet-stream", "image/jpeg"},
		{"photo.jpe", "application/octet-stream; filename=photo.jpe", "image/jpeg"},
		{"photo.png", "application/octet-stream", "image/png"},
		{"photo.gif", "application/octet-stream", "image/gif"},
		{"photo.webp", "application/octet-stream", "image/webp"},
		{"photo.avif", "application/octet-stream", "image/avif"},
		{"photo.bmp", "application/octet-stream", "image/bmp"},
		{"photo.ico", "application/octet-stream", "image/x-icon"},
		{"photo.HEIC", "application/octet-stream", "image/heic"},
		{"photo.heif", "", "image/heif"},
		{"paper.PDF", "application/octet-stream", "application/pdf"},
		{"clip.MOV", "application/octet-stream", "video/quicktime"},
		{"clip.mp4", "", "video/mp4"},
		{"clip.m4v", "", "video/mp4"},
		{"clip.webm", "", "video/webm"},
		{"clip.ogv", "", "video/ogg"},
		{"track.mp3", "", "audio/mpeg"},
		{"track.m4a", "", "audio/mp4"},
		{"track.ogg", "", "audio/ogg"},
		{"track.wav", "", "audio/wav"},
		{"track.flac", "", "audio/flac"},
		{"photo.bin", "IMAGE/JPEG; name=photo.jpg", "image/jpeg"},
		{"photo.bin", "image/heic", "image/heic"},
		{"photo.bin", "image/heif", "image/heif"},
		{"paper.bin", "Application/PDF; charset=utf-8", "application/pdf"},
		{"notes.bin", "text/plain; charset=utf-8", "text/plain"},
		{"track.bin", "audio/x-wav", "audio/x-wav"},
		{"track.bin", "audio/webm", "audio/webm"},
		{"photo.jpg", "text/html", ""},
		{"photo.jpg", "image/svg+xml", ""},
		{"photo.jpg", "application/xml", ""},
		{"photo.jpg", "text/xml", ""},
		{"photo.jpg", "image/jpeg; broken", ""},
		{"photo.jpg", "unknown", ""},
		{"photo.jpg", "application/x-custom", ""},
		{"graphic.svg", "application/octet-stream", ""},
		{"page.html", "", ""},
		{"jpg", "application/octet-stream", ""},
		{"unknown.bin", "application/octet-stream", ""},
	} {
		t.Run(test.name+"/"+test.declared, func(t *testing.T) {
			if got := inlineContentType(test.name, test.declared); got != test.want {
				t.Fatalf("inlineContentType(%q, %q) = %q, want %q", test.name, test.declared, got, test.want)
			}
		})
	}
}

func TestPreviewContentHeadersIntegrityAndRanges(t *testing.T) {
	f := newFixture(t)
	f.login()
	payload := []byte("preview-fixture-content")
	digest := sha256.Sum256(payload)
	expected := hex.EncodeToString(digest[:])
	for _, test := range []struct{ name, declared, wantType, disposition string }{
		{"photo.jpg", "", "image/jpeg", "inline"},
		{"scan.jpeg", "application/octet-stream", "image/jpeg", "inline"},
		{"paper.pdf", "application/octet-stream", "application/pdf", "inline"},
		{"clip.mov", "application/octet-stream", "video/quicktime", "inline"},
		{"native.HEIC", "image/heic", "image/heic", "inline"},
		{"parameterized.png", "IMAGE/PNG; name=photo.png", "image/png", "inline"},
		{"disguised.jpg", "text/html", "application/octet-stream", "attachment"},
		{"vector.svg", "image/svg+xml", "application/octet-stream", "attachment"},
		{"generic.svg", "application/octet-stream", "application/octet-stream", "attachment"},
	} {
		t.Run(test.name, func(t *testing.T) {
			upload := decode[domain.Upload](t, f.request("POST", "/api/v1/uploads", domain.UploadSpec{Name: test.name, Size: int64(len(payload)), MimeType: "application/octet-stream", SHA256: expected}, nil), http.StatusCreated)
			for index := 0; index < upload.TotalChunks; index++ {
				start := int64(index) * upload.ChunkSize
				end := min(start+upload.ChunkSize, int64(len(payload)))
				requireStatus(t, f.request("PUT", "/api/v1/uploads/"+upload.ID+"/chunks/"+strconv.Itoa(index), payload[start:end], nil), http.StatusOK)
			}
			node := decode[domain.Node](t, f.request("POST", "/api/v1/uploads/"+upload.ID+"/complete", map[string]any{}, nil), http.StatusOK)
			// Preserve legacy MIME metadata rather than rewriting files on preview.
			if _, err := f.db.ExecContext(context.Background(), "UPDATE nodes SET mime_type=? WHERE id=?", test.declared, node.ID); err != nil {
				t.Fatal(err)
			}
			contentPath := "/api/v1/nodes/" + node.ID + "/content"
			cookie := f.cookie
			f.cookie = nil
			requireStatus(t, f.request("GET", contentPath, nil, nil), http.StatusUnauthorized)
			f.cookie = cookie
			response := f.request("GET", contentPath, nil, nil)
			requireStatus(t, response, http.StatusOK)
			if response.Header().Get("Content-Type") != test.wantType || !strings.HasPrefix(response.Header().Get("Content-Disposition"), test.disposition+";") {
				t.Fatalf("preview headers: %v", response.Header())
			}
			if response.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(response.Header().Get("Content-Security-Policy"), "sandbox") {
				t.Fatalf("preview lost safety headers: %v", response.Header())
			}
			if !bytes.Equal(response.Body.Bytes(), payload) || response.Header().Get("X-Content-SHA256") != expected {
				t.Fatal("preview changed original bytes or digest")
			}
			ranged := f.request("GET", contentPath, nil, map[string]string{"Range": "bytes=3-14"})
			requireStatus(t, ranged, http.StatusPartialContent)
			if !bytes.Equal(ranged.Body.Bytes(), payload[3:15]) || ranged.Header().Get("Content-Range") != "bytes 3-14/23" || ranged.Header().Get("Content-Type") != test.wantType {
				t.Fatalf("range response changed: %v, %q", ranged.Header(), ranged.Body.Bytes())
			}
			download := f.request("GET", contentPath+"?download=1", nil, nil)
			requireStatus(t, download, http.StatusOK)
			if download.Header().Get("Content-Type") != test.wantType || !strings.HasPrefix(download.Header().Get("Content-Disposition"), "attachment;") || !bytes.Equal(download.Body.Bytes(), payload) {
				t.Fatal("explicit download no longer returns original attachment")
			}
			var storedType string
			if err := f.db.QueryRowContext(context.Background(), "SELECT mime_type FROM nodes WHERE id=?", node.ID).Scan(&storedType); err != nil || storedType != test.declared {
				t.Fatalf("preview mutated stored MIME: %q, %v", storedType, err)
			}
		})
	}
}
