#!/usr/bin/env python3
"""Trusted Docker controller; generated target workload runs in an offline sandbox."""

import argparse
import hashlib
import json
import shutil
import subprocess
import time
import uuid

from milestone_b_memory_check import BASE_ID, BASE_IMAGE, ROOT


def summarize(report):
    samples = report["samples"]
    mixed = [s for s in samples if s["phase"] == "mixed_upload_copy_preview"]
    assert len(mixed) >= report["mixed_configured_seconds"] - 10, "missing time-series interval"
    assert report["mixed_configured_seconds"] >= 1800
    assert report["mixed_uploads"] >= 4 and report["full_http_downloads"] >= 4
    assert report["peak_remote_writers"] == 1
    assert all(s["OOMEvents"] == s["OOMKills"] == s["PublishedIncomplete"] == 0 for s in samples)
    assert all(s["Pending"] <= 4 and s["CacheBytes"] <= (256 << 20) + 4096 and s["DiskBytes"] <= 4 << 30 for s in samples)
    fields = ("RSS", "RSSHighWater", "Cgroup", "CgroupPeak", "Anon", "FileCache", "HeapAlloc", "HeapLive", "GoSys", "Goroutines", "Pending", "StagedParts", "DiskBytes", "StagedBytes", "CacheBytes")
    result = {"peaks": {name: max(s[name] for s in samples) for name in fields}, "settled": samples[-1], "sample_count": len(samples)}
    windows = []
    first = mixed[0]["seconds"]
    for offset in range(0, int(report["mixed_configured_seconds"]), 300):
        window = [s for s in mixed if first + offset <= s["seconds"] < first + offset + 300]
        if len(window) < 2:
            continue
        a, b = window[0], window[-1]
        elapsed = b["seconds"] - a["seconds"]
        windows.append({"start_seconds": a["seconds"], "end_seconds": b["seconds"], "uploads": b["MixedUploads"] - a["MixedUploads"], "downloads": b["FullDownloads"] - a["FullDownloads"], "average_cpu_percent": (b["CPUUsec"] - a["CPUUsec"]) / elapsed / 10000, "allocation_bytes_per_second": (b["TotalAlloc"] - a["TotalAlloc"]) / elapsed, "rss_peak": max(s["RSS"] for s in window), "live_heap_peak": max(s["HeapLive"] for s in window), "goroutines_peak": max(s["Goroutines"] for s in window), "disk_first": a["DiskBytes"], "disk_last": b["DiskBytes"]})
    result["five_minute_windows"] = windows
    assert all(w["uploads"] > 0 and w["downloads"] > 0 for w in windows), "five-minute transfer stall"
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--minutes", type=int, choices=(30, 60), default=30)
    parser.add_argument("--memory-mib", type=int, choices=(768, 1024), default=768)
    parser.add_argument("--context", default="colima-harbor-a")
    parser.add_argument("--run", default="1")
    args = parser.parse_args()
    if not args.run.isdecimal():
        parser.error("run must be numeric")
    label = f"{args.minutes}m-{args.run}" if args.memory_mib == 768 else f"{args.minutes}m-{args.memory_mib}m-{args.run}"
    out = ROOT / "output/milestone-c/soak" / label
    out.mkdir(parents=True, exist_ok=False)
    prefix = ["docker", "--context", args.context]

    def run(*command, check=True, timeout=600):
        result = subprocess.run([*prefix, *command], capture_output=True, text=True, timeout=timeout)
        if check and result.returncode:
            raise RuntimeError(f"Docker {command[0]} failed: {result.stderr[-4000:]}")
        return result

    if run("image", "inspect", BASE_IMAGE, "--format", "{{.Id}}").stdout.strip() != BASE_ID:
        raise RuntimeError("compiler image identity differs")
    context = out / "build-context"
    context.mkdir()
    manifest = {}
    for path in [*ROOT.glob("internal/**/*.go"), ROOT / "go.mod", ROOT / "go.sum"]:
        relative = path.relative_to(ROOT)
        destination = context / "source" / relative
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(path, destination)
        manifest[str(relative)] = hashlib.sha256(path.read_bytes()).hexdigest()
    (out / "source-manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
    image = "harbor:milestone-c-soak-" + uuid.uuid4().hex[:12]
    (context / "Dockerfile").write_text(f"FROM {BASE_IMAGE}\nCOPY --chown=65532:65532 source /soak-src\nWORKDIR /soak-src\n")
    build = run("build", "--network", "none", "--pull=false", "-t", image, str(context))
    (out / "image-build.log").write_text(build.stdout + build.stderr)
    isolation = ["--network", "none", "--read-only", "--user", "65532:65532", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--pids-limit", "256"]
    binary = "milestone-c-soak.test"
    compiler = run("run", "--rm", *isolation, "--memory", "6656m", "--memory-swap", "6656m", "--cpus", "2", "--mount", "type=volume,src=harbor-a-race-cache,dst=/scratch", "--entrypoint", "/usr/bin/env", image, "-i", "PATH=/usr/local/go/bin:/usr/bin:/bin", "HOME=/scratch", "GOPATH=/go", "GOCACHE=/scratch/go-cache", "TMPDIR=/scratch/tmp", "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local", "CGO_ENABLED=1", "GOFLAGS=-buildvcs=false -trimpath", "GOGC=30", "GOMEMLIMIT=4608MiB", "/usr/local/go/bin/go", "test", "-p=1", "-c", "-o", "/scratch/" + binary, "./internal/api", check=False)
    (out / "compile.log").write_text(compiler.stdout + compiler.stderr)
    if compiler.returncode:
        raise RuntimeError("isolated API compile failed; inspect compile.log")
    validation = run("run", "--rm", *isolation, "--memory", "128m", "--memory-swap", "128m", "--cpus", "1", "--mount", "type=volume,src=harbor-a-race-cache,dst=/compiled,readonly", "--entrypoint", "/compiled/" + binary, image, "-test.run=^TestMilestoneCSoakDuration$", "-test.v")
    (out / "duration-test.log").write_text(validation.stdout + validation.stderr)
    hashes = run("run", "--rm", *isolation, "--memory", "128m", "--memory-swap", "128m", "--cpus", "1", "--mount", "type=volume,src=harbor-a-race-cache,dst=/compiled,readonly", "--entrypoint", "/usr/bin/sha256sum", image, "/compiled/" + binary).stdout
    (out / "binary-sha256.txt").write_text(hashes)
    image_id = run("image", "inspect", image, "--format", "{{.Id}}").stdout.strip()
    volume = "harbor-c-soak-" + uuid.uuid4().hex[:12]
    name = "harbor-c-soak-" + uuid.uuid4().hex[:12]
    run("volume", "create", volume)
    try:
        run("run", "--rm", "--network", "none", "--read-only", "--user", "0:0", "--memory", "128m", "--memory-swap", "128m", "--cpus", "1", "--pids-limit", "32", "--cap-drop", "ALL", "--cap-add", "CHOWN", "--security-opt", "no-new-privileges", "--mount", f"type=volume,src={volume},dst=/scratch", "--entrypoint", "/bin/sh", image, "-ec", "mkdir /scratch/tmp; chmod 0700 /scratch; chown -R 65532:65532 /scratch")
        started = time.monotonic()
        seconds = args.minutes * 60
        run("run", "-d", "--name", name, *isolation, "--memory", f"{args.memory_mib}m", "--memory-swap", f"{args.memory_mib}m", "--cpus", "2", "--ulimit", "nofile=1024:1024", "--ulimit", "fsize=1073741824:1073741824", "--mount", "type=volume,src=harbor-a-race-cache,dst=/compiled,readonly", "--mount", f"type=volume,src={volume},dst=/scratch", "--entrypoint", "/usr/bin/env", image, "-i", "PATH=/usr/bin:/bin", "HOME=/scratch", "TMPDIR=/scratch/tmp", "DRIVE_ENV_FILE=-", "GOMEMLIMIT=512MiB", "HARBOR_MILESTONE_B_MEMORY=1", f"HARBOR_MILESTONE_C_SOAK_SECONDS={seconds}", "HARBOR_MILESTONE_C_SAMPLES=/scratch/samples.jsonl", "HARBOR_MILESTONE_B_REPORT=/scratch/resources.json", "/compiled/" + binary, "-test.run=^TestMilestoneBSustainedMemoryBudget$", f"-test.timeout={seconds+240}s", "-test.v")
        (out / "running.json").write_text(json.dumps({"container": name, "volume": volume, "context": args.context, "image_id": image_id, "mixed_seconds": seconds, "hard_memory_bytes": args.memory_mib << 20}, indent=2) + "\n")
        print(f"Started {seconds}s mixed soak in {name}; compiler cache free, runtime uses Colima CPU2/{args.memory_mib}MiB.", flush=True)
        next_progress = 60
        while time.monotonic() - started < seconds + 270:
            state = json.loads(run("inspect", "--format", "{{json .State}}", name).stdout)
            if not state["Running"]:
                break
            elapsed = time.monotonic() - started
            if elapsed >= next_progress:
                print(f"Soak process running after {int(elapsed)}s; no completion claim yet.", flush=True)
                next_progress += 60
            time.sleep(2)
        else:
            raise RuntimeError("soak exceeded wall limit")
        log = run("logs", name)
        (out / "runtime.log").write_text(log.stdout + log.stderr)
        for filename in ("resources.json", "samples.jsonl"):
            run("cp", name + ":/scratch/" + filename, str(out / filename), check=False)
        result = {"result": "FAIL", "exit_code": state["ExitCode"], "oom_killed": state["OOMKilled"], "elapsed_seconds": round(time.monotonic()-started, 3), "mixed_seconds": seconds, "hard_memory_bytes": args.memory_mib << 20, "swap_bytes": 0, "go_soft_limit": "512MiB", "cpu_limit": 2, "image_id": image_id, "compile_hard_memory_mib": 6656, "compile_go_soft_limit": "4608MiB", "scratch": "dedicated generated disk-backed volume, fixture bytes asserted below 4 GiB; removed after run"}
        (out / "result.json").write_text(json.dumps(result, indent=2) + "\n")
        if state["ExitCode"] or state["OOMKilled"]:
            print(json.dumps(result, indent=2), flush=True)
            print((log.stdout + log.stderr)[-6000:], flush=True)
            raise SystemExit(1)
        summary = summarize(json.loads((out / "resources.json").read_text()))
        (out / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
        result["result"] = "PASS"
        (out / "result.json").write_text(json.dumps(result, indent=2) + "\n")
        print(json.dumps(result, indent=2), flush=True)
        print(json.dumps(summary, indent=2), flush=True)
    finally:
        run("rm", "-f", name, check=False)
        run("volume", "rm", volume, check=False)


if __name__ == "__main__":
    main()
