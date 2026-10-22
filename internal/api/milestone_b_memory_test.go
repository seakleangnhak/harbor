//go:build linux

package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"runtime/metrics"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"private-drive/internal/domain"
	"private-drive/internal/storage"
)

type milestoneBRemote struct {
	storage.BlobStore
	gets, bytes, puts, active, peak atomic.Int64
}

func (s *milestoneBRemote) Get(ctx context.Context, ref string) ([]byte, error) {
	select {
	case <-time.After(25 * time.Millisecond):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	data, err := s.BlobStore.Get(ctx, ref)
	s.gets.Add(1)
	s.bytes.Add(int64(len(data)))
	return data, err
}

func (s *milestoneBRemote) Put(ctx context.Context, key string, data []byte) (string, error) {
	n := s.active.Add(1)
	defer s.active.Add(-1)
	for old := s.peak.Load(); n > old && !s.peak.CompareAndSwap(old, n); old = s.peak.Load() {
	}
	select {
	case <-time.After(75 * time.Millisecond):
	case <-ctx.Done():
		return "", ctx.Err()
	}
	s.puts.Add(1)
	return s.BlobStore.Put(ctx, key, data)
}

type milestoneBMemorySample struct {
	Seconds                                                float64 `json:"seconds"`
	Phase                                                  string  `json:"phase"`
	RSS, RSSHighWater, Cgroup, CgroupPeak, Anon, FileCache uint64
	HeapAlloc, HeapInuse, HeapSys, TotalAlloc, NumGC       uint64
	GoSys, HeapReleased, HeapObjects, NextGC, HeapLive     uint64
	CPUUsec                                                uint64
	MemoryMaxEvents, OOMEvents, OOMKills                   uint64
	Goroutines, Pending, StagedParts                       int
	DiskBytes, StagedBytes, CacheBytes                     int64
}

func milestoneBDiskSize(path string) int64 {
	var size int64
	_ = filepath.WalkDir(path, func(_ string, entry os.DirEntry, err error) error {
		if err == nil && entry.Type().IsRegular() {
			if info, err := entry.Info(); err == nil {
				size += info.Size()
			}
		}
		return nil
	})
	return size
}

func milestoneBReadFields(path string) map[string]uint64 {
	data, _ := os.ReadFile(path)
	values := map[string]uint64{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			values[strings.TrimSuffix(fields[0], ":")], _ = strconv.ParseUint(fields[1], 10, 64)
		}
	}
	return values
}

func milestoneBReadUint(path string) uint64 {
	data, _ := os.ReadFile(path)
	value, _ := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	return value
}

// This opt-in resource test includes its streaming HTTP client in the process.
// It measures real API/copy/pending workers with a delayed local ciphertext store,
// not Telegram latency. Compile outside the measured runtime budget.
func TestMilestoneBSustainedMemoryBudget(t *testing.T) {
	if os.Getenv("HARBOR_MILESTONE_B_MEMORY") != "1" {
		t.Skip("requires isolated 768 MiB runtime, disk-backed scratch and GOMEMLIMIT=512MiB")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	dir := t.TempDir()
	f, _ := milestoneFixture(t, dir, nil, storage.DefaultChunkSize)
	local, err := storage.NewLocal(filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	remote := &milestoneBRemote{BlobStore: local}
	newEngine := func() *storage.Engine {
		e, err := storage.New(storage.Config{DB: f.db, Key: bytes.Repeat([]byte{7}, 32), Backend: remote, ChunkSize: storage.DefaultChunkSize, StagingDir: filepath.Join(dir, "parts"), ReadCacheDir: filepath.Join(dir, "cache")})
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	engine := newEngine()
	f.api.cfg.Storage = engine
	server := httptest.NewServer(f.api.Handler())
	defer func() { cancel(); server.Close() }()
	client := &http.Client{Transport: &http.Transport{MaxConnsPerHost: 8}, Timeout: 90 * time.Second}
	defer client.CloseIdleConnections()
	request := func(method, path string, body io.Reader, headers map[string]string, want int, out any) error {
		req, err := http.NewRequestWithContext(ctx, method, server.URL+path, body)
		if err != nil {
			return err
		}
		req.AddCookie(f.cookie)
		req.Header.Set("Origin", server.URL)
		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != want {
			return fmt.Errorf("%s %s status %d, want %d", method, path, resp.StatusCode, want)
		}
		if out != nil {
			return json.NewDecoder(resp.Body).Decode(out)
		}
		_, err = io.Copy(io.Discard, resp.Body)
		return err
	}
	var phase atomic.Value
	phase.Store("idle")
	started := time.Now()
	var samples []milestoneBMemorySample
	var sampleMu sync.Mutex
	sample := func() {
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)
		cg := milestoneBReadFields("/sys/fs/cgroup/memory.stat")
		events := milestoneBReadFields("/sys/fs/cgroup/memory.events")
		process := milestoneBReadFields("/proc/self/status")
		s := milestoneBMemorySample{Seconds: time.Since(started).Seconds(), Phase: phase.Load().(string), RSS: process["VmRSS"] * 1024, RSSHighWater: process["VmHWM"] * 1024, Cgroup: milestoneBReadUint("/sys/fs/cgroup/memory.current"), CgroupPeak: milestoneBReadUint("/sys/fs/cgroup/memory.peak"), Anon: cg["anon"], FileCache: cg["file"], HeapAlloc: mem.HeapAlloc, HeapInuse: mem.HeapInuse, HeapSys: mem.HeapSys, TotalAlloc: mem.TotalAlloc, NumGC: uint64(mem.NumGC), CPUUsec: milestoneBReadFields("/sys/fs/cgroup/cpu.stat")["usage_usec"], Goroutines: runtime.NumGoroutine(), DiskBytes: milestoneBDiskSize(dir), StagedBytes: milestoneBDiskSize(filepath.Join(dir, "parts")), CacheBytes: milestoneBDiskSize(filepath.Join(dir, "cache"))}
		s.MemoryMaxEvents, s.OOMEvents, s.OOMKills = events["max"], events["oom"], events["oom_kill"]
		s.GoSys, s.HeapReleased, s.HeapObjects, s.NextGC = mem.Sys, mem.HeapReleased, mem.HeapObjects, mem.NextGC
		live := []metrics.Sample{{Name: "/gc/heap/live:bytes"}}
		metrics.Read(live)
		s.HeapLive = live[0].Value.Uint64()
		_ = f.db.QueryRow("SELECT COUNT(*) FROM storage_pending_chunks").Scan(&s.Pending)
		_ = f.db.QueryRow("SELECT COUNT(*) FROM storage_parts").Scan(&s.StagedParts)
		sampleMu.Lock()
		samples = append(samples, s)
		sampleMu.Unlock()
	}
	sample()
	samplingDone, samplingStop := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(samplingDone)
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				sample()
			case <-samplingStop:
				return
			}
		}
	}()
	t.Cleanup(func() { close(samplingStop); <-samplingDone })
	var latencyMu sync.Mutex
	latencies := map[string][]float64{}
	record := func(kind string, start time.Time) {
		latencyMu.Lock()
		latencies[kind] = append(latencies[kind], time.Since(start).Seconds()*1000)
		latencyMu.Unlock()
	}
	phase.Store("seed")
	folder := f.folder("Synthetic mixed source", "")
	size := storage.DefaultChunkSize
	hash := milestonePatternHash(t, size)
	var sources []domain.Node
	for i := range 3 {
		u, err := engine.Begin(ctx, domain.UploadSpec{Name: fmt.Sprintf("source-%d.bin", i), ParentID: folder.ID, Size: size, SHA256: hash})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = engine.PutChunk(ctx, u.ID, 0, io.NewSectionReader(milestonePattern{}, 0, size), hash); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, decode[domain.Node](t, f.request("POST", "/api/v1/uploads/"+u.ID+"/complete", map[string]any{}, nil), 200))
	}
	var workerCancel context.CancelFunc
	var workerDone chan error
	startWorkers := func() {
		var workers context.Context
		workers, workerCancel = context.WithCancel(ctx)
		workerDone = make(chan error, 2)
		go func() { workerDone <- engine.RunPending(workers) }()
		go func() { workerDone <- f.api.RunCopyJobs(workers) }()
	}
	stopWorkers := func() {
		if workerCancel == nil {
			return
		}
		workerCancel()
		for range 2 {
			select {
			case err := <-workerDone:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(30 * time.Second):
				t.Error("mixed workers failed to join")
			}
		}
		workerCancel = nil
	}
	defer stopWorkers()
	startWorkers()
	upload := func(name string, queueOnly bool) (domain.Upload, error) {
		start := time.Now()
		defer record("upload", start)
		body, _ := json.Marshal(domain.UploadSpec{Name: name, Size: size, SHA256: hash})
		var u domain.Upload
		if err := request("POST", "/api/v1/uploads", bytes.NewReader(body), nil, 201, &u); err != nil {
			return u, err
		}
		for part, offset := 0, int64(0); offset < size; part, offset = part+1, offset+u.PartSize {
			n := min(u.PartSize, size-offset)
			h := sha256.New()
			_, _ = io.Copy(h, io.NewSectionReader(milestonePattern{}, offset, n))
			for {
				err := request("PUT", fmt.Sprintf("/api/v1/uploads/%s/chunks/0/parts/%d", u.ID, part), io.NewSectionReader(milestonePattern{}, offset, n), map[string]string{"X-Part-SHA256": fmt.Sprintf("%x", h.Sum(nil))}, 200, nil)
				if err == nil {
					break
				}
				if ctx.Err() != nil || !strings.Contains(err.Error(), "status 409") {
					return u, err
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
		if err := request("POST", "/api/v1/uploads/"+u.ID+"/chunks/0/commit", strings.NewReader(`{"background":true}`), nil, 202, &u); err != nil {
			return u, err
		}
		if queueOnly {
			return u, nil
		}
		for u.ReceivedBytes != size {
			if err := request("GET", "/api/v1/uploads/"+u.ID, nil, nil, 200, &u); err != nil {
				return u, err
			}
			if ctx.Err() != nil {
				return u, ctx.Err()
			}
			time.Sleep(10 * time.Millisecond)
		}
		if err := request("POST", "/api/v1/uploads/"+u.ID+"/complete", strings.NewReader(`{}`), nil, 200, nil); err != nil {
			return u, err
		}
		return u, nil
	}
	phase.Store("sequential_upload")
	for i := range 2 {
		if _, err := upload(fmt.Sprintf("sequential-%d.bin", i), false); err != nil {
			t.Fatal(err)
		}
	}
	phase.Store("mixed_upload_copy_preview")
	mixedStarted := time.Now()
	mixedDeadline := mixedStarted.Add(65 * time.Second)
	var mixedUploads, fullDownloads, lastUploadNS, lastDownloadNS atomic.Int64
	var readers sync.WaitGroup
	errors := make(chan error, 8)
	preview := func(duration time.Duration) {
		deadline := time.Now().Add(duration)
		for lane := range 2 {
			readers.Add(1)
			go func(lane int) {
				defer readers.Done()
				for i := lane; time.Now().Before(deadline); i++ {
					node := sources[i%len(sources)]
					start := time.Now()
					req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/v1/nodes/"+node.ID+"/content", nil)
					req.AddCookie(f.cookie)
					// Rotate overlapping and end-of-chunk ranges across three chunks.
					offset := int64((i % 4) * 128 << 10)
					if i%5 == 0 {
						offset = size - (256 << 10)
					}
					req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", offset, offset+(256<<10)-1))
					resp, err := client.Do(req)
					if err != nil {
						errors <- err
						return
					}
					h := sha256.New()
					n, readErr := io.Copy(h, resp.Body)
					_ = resp.Body.Close()
					expected := sha256.New()
					_, _ = io.Copy(expected, io.NewSectionReader(milestonePattern{}, offset, 256<<10))
					if readErr != nil || resp.StatusCode != 206 || n != 256<<10 || !bytes.Equal(h.Sum(nil), expected.Sum(nil)) {
						errors <- fmt.Errorf("preview range integrity: %d %d %v", resp.StatusCode, n, readErr)
						return
					}
					record("preview_range", start)
				}
			}(lane)
		}
	}
	preview(65 * time.Second)
	readers.Add(1)
	go func() {
		defer readers.Done()
		for i := 0; time.Now().Before(mixedDeadline); i++ {
			start := time.Now()
			node := sources[i%len(sources)]
			req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/v1/nodes/"+node.ID+"/content", nil)
			req.AddCookie(f.cookie)
			resp, err := client.Do(req)
			if err != nil {
				errors <- err
				return
			}
			h := sha256.New()
			n, readErr := io.Copy(h, resp.Body)
			_ = resp.Body.Close()
			if readErr != nil || resp.StatusCode != 200 || n != size || fmt.Sprintf("%x", h.Sum(nil)) != hash {
				errors <- fmt.Errorf("full HTTP download checksum: %d %d %v", resp.StatusCode, n, readErr)
				return
			}
			fullDownloads.Add(1)
			lastDownloadNS.Store(time.Since(mixedStarted).Nanoseconds())
			record("full_http_download", start)
		}
	}()
	var uploaders sync.WaitGroup
	for lane := range 2 {
		uploaders.Add(1)
		go func(lane int) {
			defer uploaders.Done()
			for i := 0; i < 100 && time.Now().Before(mixedDeadline); i++ {
				u, err := upload(fmt.Sprintf("concurrent-%d-%d.bin", lane, i), false)
				if err != nil {
					errors <- err
					return
				}
				mixedUploads.Add(1)
				lastUploadNS.Store(time.Since(mixedStarted).Nanoseconds())
				// Recycle only this test's freshly uploaded local fixture, bounding
				// retained library size independently of workload duration.
				for _, suffix := range []string{"", "/permanent"} {
					if err := request("DELETE", "/api/v1/nodes/"+u.ID+suffix, nil, nil, 204, nil); err != nil {
						errors <- err
						return
					}
				}
			}
		}(lane)
	}
	var jobs []copyJob
	for iteration := range 2 {
		if iteration == 1 {
			select {
			case <-time.After(time.Until(mixedStarted.Add(30 * time.Second))):
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
		start := time.Now()
		job, _, err := f.api.planCopy(ctx, copyRequest{OperationID: domain.NewID(), SourceIDs: []string{folder.ID}})
		if err != nil {
			t.Fatal(err)
		}
		for {
			job, err = f.api.copyJob(ctx, job.ID)
			if err != nil || job.Status == "failed" || job.Status == "cancelled" {
				t.Fatalf("mixed copy: %+v %v", job, err)
			}
			if job.Status == "complete" {
				break
			}
			if ctx.Err() != nil {
				t.Fatal(ctx.Err())
			}
			time.Sleep(20 * time.Millisecond)
		}
		jobs = append(jobs, job)
		record("mixed_copy_completion", start)
	}
	uploaders.Wait()
	readers.Wait()
	for len(errors) > 0 {
		t.Error(<-errors)
	}
	if t.Failed() {
		return
	}
	if mixedUploads.Load() < 4 || fullDownloads.Load() < 4 || lastUploadNS.Load() < int64(60*time.Second) || lastDownloadNS.Load() < int64(60*time.Second) {
		t.Fatalf("mixed workload did not sustain transfers for 60s: uploads=%d downloads=%d last_upload=%d last_download=%d", mixedUploads.Load(), fullDownloads.Load(), lastUploadNS.Load(), lastDownloadNS.Load())
	}
	stopWorkers()
	phase.Store("restart_queued_upload")
	pending, err := upload("restart-queued.bin", true)
	if err != nil || pending.ReceivedBytes != 0 {
		t.Fatalf("restart queue: %+v %v", pending, err)
	}
	engine = newEngine() // No active readers/writers; restart clears ciphertext cache.
	f.api.cfg.Storage = engine
	startWorkers()
	milestoneWaitStored(t, engine, pending.ID)
	if err := request("POST", "/api/v1/uploads/"+pending.ID+"/complete", strings.NewReader(`{}`), nil, 200, nil); err != nil {
		t.Fatal(err)
	}
	phase.Store("post_restart_preview_eviction")
	preview(15 * time.Second)
	readers.Wait()
	for len(errors) > 0 {
		t.Error(<-errors)
	}
	phase.Store("final_integrity")
	verified := append([]domain.Node{}, sources...)
	for _, job := range jobs {
		listing := decode[struct {
			Items []domain.Node `json:"items"`
		}](t, f.request("GET", "/api/v1/nodes?parent_id="+job.ResultIDs[0], nil, nil), 200)
		if len(listing.Items) != 3 {
			t.Fatalf("copy hierarchy incomplete: %d", len(listing.Items))
		}
		verified = append(verified, listing.Items...)
	}
	for _, node := range verified {
		r, manifest, err := engine.Open(ctx, node.ID)
		if err != nil {
			t.Fatal(err)
		}
		h := sha256.New()
		n, readErr := io.Copy(h, r)
		closeErr := r.Close()
		if readErr != nil || closeErr != nil || n != size || fmt.Sprintf("%x", h.Sum(nil)) != manifest.SHA256 {
			t.Fatalf("full checksum: %d %v %v", n, readErr, closeErr)
		}
	}
	stopWorkers()
	runtime.GC()
	phase.Store("settled")
	time.Sleep(time.Second)
	sample()
	sampleMu.Lock()
	defer sampleMu.Unlock()
	for _, s := range samples {
		if s.Pending > 4 || s.CacheBytes > (256<<20)+4096 || s.DiskBytes > 4<<30 || s.OOMEvents != 0 || s.OOMKills != 0 {
			t.Fatalf("bounded queue/cache violated: %+v", s)
		}
	}
	var pendingCount, partsCount int
	if err := f.db.QueryRow("SELECT COUNT(*) FROM storage_pending_chunks").Scan(&pendingCount); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow("SELECT COUNT(*) FROM storage_parts").Scan(&partsCount); err != nil {
		t.Fatal(err)
	}
	if pendingCount != 0 || partsCount != 0 || remote.peak.Load() != 1 {
		t.Fatalf("jobs or writer bound: pending=%d parts=%d writers=%d", pendingCount, partsCount, remote.peak.Load())
	}
	percentiles := map[string]map[string]float64{}
	for kind, values := range latencies {
		sort.Float64s(values)
		percentiles[kind] = map[string]float64{"count": float64(len(values)), "p50_ms": values[(len(values)-1)*50/100], "p95_ms": values[(len(values)-1)*95/100], "max_ms": values[len(values)-1]}
	}
	report := map[string]any{"workload": "synthetic delayed local ciphertext, actual HTTP/copy/background workers; in-process streaming clients included", "mixed_minimum_seconds": 60, "mixed_configured_seconds": 65, "mixed_uploads": mixedUploads.Load(), "full_http_downloads": fullDownloads.Load(), "last_mixed_upload_seconds": float64(lastUploadNS.Load()) / 1e9, "last_mixed_download_seconds": float64(lastDownloadNS.Load()) / 1e9, "copy_jobs": len(jobs), "storage_chunk_bytes": size, "transport_part_bytes": storage.TransportPartSize, "elapsed_seconds": time.Since(started).Seconds(), "remote_gets": remote.gets.Load(), "remote_bytes": remote.bytes.Load(), "remote_puts": remote.puts.Load(), "peak_remote_writers": remote.peak.Load(), "latencies": percentiles, "samples": samples, "go_version": runtime.Version()}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if path := os.Getenv("HARBOR_MILESTONE_B_REPORT"); path != "" {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("sustained mixed workload PASS elapsed=%.2fs ranges=%d remote_gets=%d remote_bytes=%d peak_writers=%d", time.Since(started).Seconds(), len(latencies["preview_range"]), remote.gets.Load(), remote.bytes.Load(), remote.peak.Load())
}
