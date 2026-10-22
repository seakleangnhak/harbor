//go:build !windows

package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"private-drive/internal/domain"
)

type cleanupCrashFixture struct {
	Directory, PublishedID, RecoverableID, RecoverableRef string
}

func TestMilestoneCCleanupCrashChild(t *testing.T) {
	control := os.Getenv("HARBOR_CLEANUP_CRASH_CONTROL")
	if control == "" {
		t.Skip("subprocess fixture only")
	}
	e, dir, id := hygieneFixture(t)
	hygieneStage(t, e, id)
	hygieneCache(t, dir)
	for range 2048 {
		name := domain.NewID() + "-" + domain.NewID()
		if err := os.WriteFile(filepath.Join(dir, "read-cache", name), append([]byte("HD01"), make([]byte, 28)...), 0600); err != nil {
			t.Fatal(err)
		}
	}
	pending := begin(t, e, []byte("integrity-check fixture"), digest([]byte("integrity-check fixture")))
	ref := hygieneStage(t, e, pending.ID)
	if err := os.WriteFile(filepath.Join(dir, "upload-parts", "untracked"), []byte("retain unknown fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := InspectHygiene(context.Background(), e.db, dir)
	if err != nil || len(plan.Blocked) != 0 || len(plan.Candidates) != 2050 {
		t.Fatalf("cleanup crash plan: candidates=%d blocked=%v err=%v", len(plan.Candidates), plan.Blocked, err)
	}
	fixture, err := json.Marshal(cleanupCrashFixture{dir, id, pending.ID, ref})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(control, "ready.json"), fixture, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyHygiene(context.Background(), e.db, dir, plan.PlanSHA256); err != nil {
		t.Fatal(err)
	}
	t.Fatal("cleanup completed before controlled process termination")
}

func TestMilestoneCCleanupSIGKILLReconciliation(t *testing.T) {
	control := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestMilestoneCCleanupCrashChild$", "-test.timeout=45s")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + control, "TMPDIR=" + control, "GOMEMLIMIT=512MiB", "GORACE=halt_on_error=1", "HARBOR_CLEANUP_CRASH_CONTROL=" + control}
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	deadline := time.Now().Add(30 * time.Second)
	var fixture cleanupCrashFixture
	var audit []byte
	for time.Now().Before(deadline) {
		ready, err := os.ReadFile(filepath.Join(control, "ready.json"))
		if err == nil && json.Unmarshal(ready, &fixture) == nil {
			audit, _ = os.ReadFile(filepath.Join(fixture.Directory, "storage-cleanup.jsonl"))
			if bytes.Contains(audit, []byte(`"Outcome":"remove_intent"`)) {
				break
			}
		}
		time.Sleep(time.Millisecond)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	var exit *exec.ExitError
	if err := cmd.Wait(); !errors.As(err, &exit) || exit.ProcessState.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
		t.Fatalf("cleanup SIGKILL failed: %v; %s", err, output.String())
	}
	if !bytes.Contains(audit, []byte(`"Outcome":"remove_intent"`)) || bytes.Contains(audit, []byte(`"Outcome":"complete"`)) {
		t.Fatalf("process did not reach incomplete audited cleanup: %s", output.String())
	}
	if bytes.Contains(output.Bytes(), []byte("WARNING: DATA RACE")) {
		t.Fatal(output.String())
	}
	e, _ := testEngine(t, fixture.Directory, nil, nil)
	e.staging, err = NewLocal(filepath.Join(fixture.Directory, "upload-parts"))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := InspectHygiene(context.Background(), e.db, fixture.Directory)
	if err != nil || len(plan.Blocked) != 0 || len(plan.Candidates) == 0 {
		t.Fatalf("restart cleanup plan: candidates=%d blocked=%v err=%v", len(plan.Candidates), plan.Blocked, err)
	}
	result, err := ApplyHygiene(context.Background(), e.db, fixture.Directory, plan.PlanSHA256)
	if err != nil || !result.Applied {
		t.Fatalf("cleanup reconciliation failed: %v", err)
	}
	again, err := InspectHygiene(context.Background(), e.db, fixture.Directory)
	if err != nil || len(again.Blocked) != 0 || len(again.Candidates) != 0 || again.RecoverablePartFiles != 1 || again.UnresolvedPartFiles != 1 {
		t.Fatalf("unreconciled or unapproved state: %+v %v", again, err)
	}
	for _, name := range []string{fixture.RecoverableRef, "untracked"} {
		if _, err := os.Stat(filepath.Join(fixture.Directory, "upload-parts", name)); err != nil {
			t.Fatal("cleanup removed recoverable or unknown data")
		}
	}
	status, err := e.Status(context.Background(), fixture.RecoverableID)
	if err != nil || status.Status != "uploading" || len(status.StagedParts) != 1 || status.ReceivedBytes != 0 {
		t.Fatalf("recoverable upload changed: %+v %v", status, err)
	}
	verified, err := e.VerifyIntegrity(context.Background())
	if err != nil || verified.VerifiedFiles != 1 || len(verified.Issues) != 0 {
		t.Fatalf("cleanup changed published ciphertext: %+v %v", verified, err)
	}
	t.Logf("actual SIGKILL after observed remove_intent; restart reconciled %d remaining candidates; published file, accepted encrypted part and unknown fixture retained", len(plan.Candidates))
}
