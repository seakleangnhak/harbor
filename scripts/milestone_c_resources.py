#!/usr/bin/env python3
"""Trusted Docker controller: generated resource/failure fixtures, never owner state."""

import argparse
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import time
import uuid


ROOT = Path(__file__).resolve().parents[1]
TOOL = "harbor:milestone-a-reliability-final"
TOOL_ID = "sha256:ff5fa6b047dbb32cde879ea1fa2e735e8b11841dd770bf7f043db8fb6559800e"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--context", default="colima-harbor-a")
    parser.add_argument("--label", default="final")
    args = parser.parse_args()
    if not args.label.isalnum():
        parser.error("label must be alphanumeric")
    out = ROOT / "output/milestone-c/resources" / args.label
    out.mkdir(parents=True, exist_ok=False)
    prefix = ["docker", "--context", args.context]

    def docker(*command, check=True):
        result = subprocess.run([*prefix, *command], capture_output=True, text=True, timeout=900)
        if check and result.returncode:
            raise RuntimeError("Docker " + command[0] + " failed; review generated-fixture evidence")
        return result

    if docker("image", "inspect", TOOL, "--format", "{{.Id}}").stdout.strip() != TOOL_ID:
        raise RuntimeError("compiler identity differs")
    context = out / "context"
    source = context / "source"
    for path in [*ROOT.glob("internal/**/*.go"), ROOT / "go.mod", ROOT / "go.sum"]:
        destination = source / path.relative_to(ROOT)
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(path, destination)
    manifest = {str(p.relative_to(source)): hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(source.rglob("*")) if p.is_file()}
    (out / "source-manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
    (context / "Dockerfile").write_text("FROM " + TOOL + "\nCOPY --chown=65532:65532 source /resource-src\nWORKDIR /resource-src\n")
    image = "harbor:milestone-c-resources-" + args.label.lower()
    build = docker("build", "--network=none", "--pull=false", "-t", image, str(context))
    (out / "build.log").write_text(build.stdout + build.stderr)
    isolation = ["--network=none", "--read-only", "--user=65532:65532", "--cpus=2", "--pids-limit=256", "--cap-drop=ALL", "--security-opt=no-new-privileges"]
    cache = "type=volume,src=harbor-a-race-cache,dst=/scratch"
    compile_env = ["-i", "PATH=/usr/local/go/bin:/usr/bin:/bin", "HOME=/scratch", "TMPDIR=/scratch/tmp", "GOCACHE=/scratch/go-cache", "GOPATH=/go", "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off", "CGO_ENABLED=1", "GOFLAGS=-buildvcs=false -trimpath", "GOGC=30", "GOMEMLIMIT=4608MiB"]
    results = []
    for label, flags in [("default", []), ("telegram-race", ["-race", "-tags=telegram"])]:
        binary = "/scratch/resource-c-" + label + ".test"
        started = time.monotonic()
        compiled = docker("run", "--rm", *isolation, "--memory=6656m", "--memory-swap=6656m", "--mount", cache, "--entrypoint=/usr/bin/env", image, *compile_env, "go", "test", "-p=1", "-c", *flags, "-o", binary, "./internal/storage", check=False)
        (out / (label + "-compile.log")).write_text(compiled.stdout + compiled.stderr)
        if compiled.returncode:
            raise RuntimeError(label + " compile failed; inspect generated-fixture log")
        compile_seconds = round(time.monotonic() - started, 3)
        name = "harbor-c-resources-" + uuid.uuid4().hex[:12]
        started = time.monotonic()
        try:
            docker("run", "-d", "--name", name, *isolation, "--memory=768m", "--memory-swap=768m", "--tmpfs=/scratch:rw,nosuid,nodev,uid=65532,gid=65532,mode=0700,size=96m", "--tmpfs=/limited:rw,nosuid,nodev,noexec,uid=65532,gid=65532,mode=0700,size=12m", "--mount=type=volume,src=harbor-a-race-cache,dst=/compiled,readonly", "--entrypoint=/usr/bin/env", image, "-i", "PATH=/usr/bin:/bin", "HOME=/scratch", "TMPDIR=/scratch", "GOMEMLIMIT=512MiB", "GORACE=halt_on_error=1", "HARBOR_MILESTONE_B_FULL_DISK_DIR=/limited", "/compiled/resource-c-" + label + ".test", "-test.run=^(TestMilestoneC|TestMilestoneB|TestHygiene|TestReadCache|TestTransportEncryptedArchiveRestore)", "-test.v", "-test.timeout=120s")
            while time.monotonic() - started < 150:
                state = json.loads(docker("inspect", "--format", "{{json .State}}", name).stdout)
                if not state["Running"]:
                    break
                time.sleep(0.25)
            else:
                raise RuntimeError("resource runtime timeout")
            log = docker("logs", name)
            (out / (label + ".log")).write_text(log.stdout + log.stderr)
            result = {"mode": label, "result": "PASS" if state["ExitCode"] == 0 and not state["OOMKilled"] else "FAIL", "exit_code": state["ExitCode"], "oom_killed": state["OOMKilled"], "compile_seconds": compile_seconds, "runtime_seconds": round(time.monotonic() - started, 3), "hard_memory_bytes": 768 << 20, "go_soft": "512MiB", "tmpfs_scratch_bytes": 96 << 20, "full_disk_tmpfs_bytes": 12 << 20}
            results.append(result)
            (out / "result.json").write_text(json.dumps(results, indent=2) + "\n")
            print(json.dumps(result), flush=True)
            if result["result"] != "PASS":
                raise SystemExit(1)
        finally:
            docker("rm", "-f", name, check=False)


if __name__ == "__main__":
    main()
