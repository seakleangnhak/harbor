#!/usr/bin/env python3
"""Trusted Docker control only; target Go code runs offline inside bounded containers."""

import argparse
import hashlib
import json
import pathlib
import shutil
import subprocess
import time
import uuid


ROOT = pathlib.Path(__file__).resolve().parents[1]
BASE_IMAGE = "harbor:milestone-a-reliability-final"
BASE_ID = "sha256:ff5fa6b047dbb32cde879ea1fa2e735e8b11841dd770bf7f043db8fb6559800e"
BASE_COMMIT = "b6ae39e2d4bf0846448a916c6890e38b4d590a6a"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("label", choices=("baseline", "after"))
    parser.add_argument("--context", default="colima-harbor-a")
    parser.add_argument("--run", default="1", help="evidence suffix")
    parser.add_argument("--skip-build", action="store_true")
    args = parser.parse_args()
    if not args.run.isdecimal():
        parser.error("run must be numeric")
    out = ROOT / "output/milestone-b/memory" / f"{args.label}-{args.run}"
    out.mkdir(parents=True, exist_ok=True)
    prefix = ["docker", "--context", args.context]

    def run(*command, check=True):
        result = subprocess.run([*prefix, *command], capture_output=True, text=True)
        if check and result.returncode:
            raise RuntimeError(f"Docker {command[0]} failed: {result.stderr[-4000:]}")
        return result

    if run("image", "inspect", BASE_IMAGE, "--format", "{{.Id}}").stdout.strip() != BASE_ID:
        raise RuntimeError("compiler image identity differs")
    image = f"harbor:milestone-b-memory-{args.label}"
    api_binary = f"memory-b-{args.label}.test"
    storage_binary = f"memory-b-storage-{args.label}.test"
    source = ROOT / "output/milestone-b/memory/baseline-source" if args.label == "baseline" else ROOT
    if args.label == "baseline":
        paths = subprocess.run(["git", "ls-tree", "-r", "--name-only", BASE_COMMIT, "--", "internal", "go.mod", "go.sum"], cwd=ROOT, check=True, capture_output=True, text=True).stdout.splitlines()
        for relative in paths:
            if relative not in ("go.mod", "go.sum") and not relative.endswith(".go"):
                continue
            destination = source / relative
            destination.parent.mkdir(parents=True, exist_ok=True)
            original = subprocess.run(["git", "show", f"{BASE_COMMIT}:{relative}"], cwd=ROOT, check=True, capture_output=True).stdout
            if destination.exists() and destination.read_bytes() != original:
                raise RuntimeError(f"frozen baseline differs from {BASE_COMMIT}: {relative}")
            destination.write_bytes(original)
    cache_mount = "type=volume,src=harbor-a-race-cache,dst=/scratch"
    isolation = ["--network", "none", "--read-only", "--user", "65532:65532", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--pids-limit", "256"]
    clean_env = ["--entrypoint", "/usr/bin/env"]
    compile_env = ["-i", "PATH=/usr/local/go/bin:/usr/bin:/bin", "HOME=/scratch", "GOPATH=/go", "GOCACHE=/scratch/go-cache", "TMPDIR=/scratch/tmp", "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local", "CGO_ENABLED=1", "GOFLAGS=-buildvcs=false -trimpath", "GOGC=30", "GOMEMLIMIT=4608MiB"]
    if not args.skip_build:
        context = out / "build-context"
        context.mkdir(exist_ok=True)
        manifest = {}
        for path in [*source.glob("internal/**/*.go"), source / "go.mod", source / "go.sum"]:
            relative = path.relative_to(source)
            destination = context / "source" / relative
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(path, destination)
        for relative in ["internal/api/milestone_b_memory_test.go", "internal/storage/staging_memory_test.go"]:
            shutil.copyfile(ROOT / relative, context / "source" / relative)
        for path in sorted((context / "source").rglob("*")):
            if path.is_file():
                manifest[str(path.relative_to(context / "source"))] = hashlib.sha256(path.read_bytes()).hexdigest()
        (out / "source-manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
        (context / "Dockerfile").write_text(f"FROM {BASE_IMAGE}\nCOPY --chown=65532:65532 source /memory-src\nWORKDIR /memory-src\n")
        build = run("build", "--network", "none", "--pull=false", "-t", image, str(context))
        (out / "image-build.log").write_text(build.stdout + build.stderr)
        run("run", "--rm", *isolation, "--memory", "128m", "--memory-swap", "128m", "--cpus", "1", "--mount", cache_mount, "--entrypoint", "/bin/mkdir", image, "-p", "/scratch/tmp")
        for package, binary in [("./internal/api", api_binary), ("./internal/storage", storage_binary)]:
            compile_result = run("run", "--rm", *isolation, "--memory", "6656m", "--memory-swap", "6656m", "--cpus", "2", "--mount", cache_mount, *clean_env, image, *compile_env, "/usr/local/go/bin/go", "test", "-p=1", "-c", "-o", "/scratch/" + binary, package, check=False)
            (out / (binary + ".compile.log")).write_text(compile_result.stdout + compile_result.stderr)
            if compile_result.returncode:
                raise RuntimeError(f"isolated compile failed for {package}; see compile log")
    binary_hashes = run("run", "--rm", *isolation, "--memory", "128m", "--memory-swap", "128m", "--cpus", "1", "--mount", "type=volume,src=harbor-a-race-cache,dst=/compiled,readonly", "--entrypoint", "/usr/bin/sha256sum", image, "/compiled/" + api_binary, "/compiled/" + storage_binary).stdout
    (out / "binary-sha256.txt").write_text(binary_hashes)
    image_id = run("image", "inspect", image, "--format", "{{.Id}}").stdout.strip()
    volume = "harbor-b-memory-" + uuid.uuid4().hex[:12]
    name = "harbor-b-memory-" + uuid.uuid4().hex[:12]
    run("volume", "create", volume)
    try:
        run("run", "--rm", "--network", "none", "--read-only", "--user", "0:0", "--memory", "128m", "--memory-swap", "128m", "--cpus", "1", "--pids-limit", "32", "--cap-drop", "ALL", "--cap-add", "CHOWN", "--security-opt", "no-new-privileges", "--mount", f"type=volume,src={volume},dst=/scratch", "--entrypoint", "/bin/sh", image, "-ec", "mkdir /scratch/tmp; chmod 0700 /scratch; chown -R 65532:65532 /scratch")
        started = time.monotonic()
        run("run", "-d", "--name", name, *isolation, "--memory", "768m", "--memory-swap", "768m", "--cpus", "2", "--ulimit", "nofile=1024:1024", "--ulimit", "fsize=1073741824:1073741824", "--mount", "type=volume,src=harbor-a-race-cache,dst=/compiled,readonly", "--mount", f"type=volume,src={volume},dst=/scratch", *clean_env, image, "-i", "PATH=/usr/bin:/bin", "HOME=/scratch", "TMPDIR=/scratch/tmp", "DRIVE_ENV_FILE=-", "GOMEMLIMIT=512MiB", "HARBOR_MILESTONE_B_MEMORY=1", "HARBOR_MILESTONE_B_REPORT=/scratch/resources.json", "/compiled/" + api_binary, "-test.run=^TestMilestoneBSustainedMemoryBudget$", "-test.timeout=5m", "-test.v")
        while time.monotonic() - started < 330:
            state = json.loads(run("inspect", "--format", "{{json .State}}", name).stdout)
            if not state["Running"]:
                break
            time.sleep(1)
        else:
            raise RuntimeError("runtime exceeded wall limit")
        log = run("logs", name)
        (out / "runtime.log").write_text(log.stdout + log.stderr)
        run("cp", name + ":/scratch/resources.json", str(out / "resources.json"), check=False)
        result = {"result": "PASS" if state["ExitCode"] == 0 else "FAIL", "exit_code": state["ExitCode"], "oom_killed": state["OOMKilled"], "elapsed_seconds": round(time.monotonic()-started, 3), "hard_memory_bytes": 768*1024*1024, "swap_bytes": 0, "go_soft_limit": "512MiB", "cpu_limit": 2, "image_id": image_id, "compile_hard_memory_mib": 6656, "compile_go_soft_limit": "4608MiB", "scratch": "dedicated generated disk-backed volume; removed after run"}
        (out / "result.json").write_text(json.dumps(result, indent=2) + "\n")
        print(json.dumps(result, indent=2), flush=True)
        if state["ExitCode"]:
            print((log.stdout + log.stderr)[-6000:], flush=True)
            raise SystemExit(1)
        bench = run("run", "--rm", *isolation, "--memory", "768m", "--memory-swap", "768m", "--cpus", "2", "--mount", "type=volume,src=harbor-a-race-cache,dst=/compiled,readonly", "--mount", f"type=volume,src={volume},dst=/scratch", *clean_env, image, "-i", "PATH=/usr/bin:/bin", "HOME=/scratch", "TMPDIR=/scratch/tmp", "DRIVE_ENV_FILE=-", "GOMEMLIMIT=512MiB", "/compiled/" + storage_binary, "-test.run=^$", "-test.bench=^BenchmarkTransportReader100MiB$", "-test.benchtime=3x", "-test.count=3", "-test.benchmem", "-test.timeout=2m", check=False)
        (out / "staging-benchmark.log").write_text(bench.stdout + bench.stderr)
        if bench.returncode:
            raise RuntimeError("staging benchmark failed")
        print(bench.stdout, flush=True)
    finally:
        run("rm", "-f", name, check=False)
        run("volume", "rm", volume, check=False)


if __name__ == "__main__":
    main()
