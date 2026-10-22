//go:build !windows

package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"private-drive/internal/domain"
)

func TestMilestoneBPreviewCrashChild(t *testing.T) {
	dir := os.Getenv("HARBOR_PREVIEW_CRASH_DIR")
	if dir == "" {
		t.Skip("subprocess fixture only")
	}
	e, _ := testEngine(t, dir, nil, nil)
	data := []byte("authenticated preview survives process termination")
	u := begin(t, e, data, digest(data))
	for i := range u.TotalChunks {
		put(t, e, u, data, i)
	}
	if _, err := e.Complete(context.Background(), u.ID); err != nil {
		t.Fatal(err)
	}
	cache, err := newReadCache(filepath.Join(dir, "cache"))
	if err != nil {
		t.Fatal(err)
	}
	e.readCache = cache
	e.backend = &injectedStore{BlobStore: e.backend, get: func(context.Context, string) ([]byte, error) {
		// Simulated torn cache fill; restart must discard only owned cache data.
		if err := os.WriteFile(filepath.Join(dir, "cache", ".pending-123"), []byte("HD01partial"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "ready"), []byte(u.ID), 0600); err != nil {
			t.Fatal(err)
		}
		for {
			time.Sleep(time.Hour)
		}
	}}
	r, _, err := e.Open(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	_, _ = r.Read(make([]byte, 1))
	t.Fatal("preview did not reach controlled fetch boundary")
}

func TestMilestoneBPreviewSIGKILLAndCacheRestart(t *testing.T) {
	dir := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestMilestoneBPreviewCrashChild$", "-test.timeout=45s")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "TMPDIR=" + dir, "GORACE=halt_on_error=1", "HARBOR_PREVIEW_CRASH_DIR=" + dir}
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	deadline := time.Now().Add(30 * time.Second)
	var id []byte
	for time.Now().Before(deadline) {
		id, err = os.ReadFile(filepath.Join(dir, "ready"))
		if err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	var exit *exec.ExitError
	if err := cmd.Wait(); !errors.As(err, &exit) || exit.ProcessState.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL || len(id) != 32 {
		t.Fatalf("controlled preview SIGKILL failed: %v, %s", err, output.String())
	}
	if bytes.Contains(output.Bytes(), []byte("WARNING: DATA RACE")) {
		t.Fatal(output.String())
	}
	e, _ := testEngine(t, dir, nil, nil)
	e.readCache, err = newReadCache(filepath.Join(dir, "cache"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "cache", ".pending-123")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("restart retained torn cache fill")
	}
	r, manifest, err := e.Open(context.Background(), string(id))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	data, err := io.ReadAll(r)
	if err != nil || string(data) != "authenticated preview survives process termination" || digest(data) != manifest.SHA256 {
		t.Fatalf("preview crash changed completed content: %v", err)
	}
}

func TestMilestoneBFullStagingDiskRetry(t *testing.T) {
	limited := os.Getenv("HARBOR_MILESTONE_B_FULL_DISK_DIR")
	if limited == "" {
		t.Skip("requires dedicated bounded empty tmpfs")
	}
	var filesystem syscall.Statfs_t
	if err := syscall.Statfs(limited, &filesystem); err != nil || uint64(filesystem.Blocks)*uint64(filesystem.Bsize) > 16<<20 {
		t.Fatal("full-disk test requires isolated filesystem at most 16 MiB")
	}
	dir := t.TempDir()
	e, db := transportEngine(t, dir, nil, 8<<20)
	stage := filepath.Join(limited, "parts")
	var err error
	e.staging, err = NewLocal(stage)
	if err != nil {
		t.Fatal(err)
	}
	u, err := e.Begin(context.Background(), domain.UploadSpec{Name: "disk-retry.bin", Size: 8 << 20, SHA256: fixtureHash(t, 0, 8<<20)})
	if err != nil {
		t.Fatal(err)
	}
	u = stageFixture(t, e, u, 0, 0)
	filler := filepath.Join(limited, "filler")
	f, err := os.OpenFile(filler, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	for err == nil {
		_, err = f.Write(make([]byte, 64<<10))
	}
	_ = f.Close()
	if !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("expected actual ENOSPC: %v", err)
	}
	_, err = e.PutPart(context.Background(), u.ID, 0, 1, io.NewSectionReader(largeChunkFixture{}, TransportPartSize, TransportPartSize), fixtureHash(t, TransportPartSize, TransportPartSize))
	if !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("full disk acknowledged part: %v", err)
	}
	status, err := e.Status(context.Background(), u.ID)
	if err != nil || status.ReceivedBytes != 0 || len(status.StagedParts) != 1 {
		t.Fatalf("full disk lost accepted part: %+v %v", status, err)
	}
	if _, err := e.Complete(context.Background(), u.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("incomplete file published: %v", err)
	}
	if err := os.Remove(filler); err != nil {
		t.Fatal(err)
	}
	// Reopen against the same accepted encrypted part after disk recovery.
	e, err = New(Config{DB: db, Key: bytes.Repeat([]byte{37}, 32), Backend: e.backend, ChunkSize: 8 << 20, StagingDir: stage})
	if err != nil {
		t.Fatal(err)
	}
	stageFixture(t, e, u, 0, 1)
	if _, err := e.CommitChunk(context.Background(), u.ID, 0); err != nil {
		t.Fatal(err)
	}
	manifest, err := e.Complete(context.Background(), u.ID)
	if err != nil || manifest.SHA256 != fixtureHash(t, 0, 8<<20) {
		t.Fatalf("disk recovery checksum failed: %v", err)
	}
}
