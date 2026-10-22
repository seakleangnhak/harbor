//go:build telegram

package storage

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"private-drive/internal/domain"
)

type liveTransferCounts struct {
	GetCalls, GetBytes, PutCalls, PutBytes int64
}

type liveTransferStore struct {
	BlobStore
	mu      sync.Mutex
	counts  liveTransferCounts
	watchID string
	started chan struct{}
}

func (s *liveTransferStore) Put(ctx context.Context, id string, data []byte) (string, error) {
	s.mu.Lock()
	s.counts.PutCalls++
	s.counts.PutBytes += int64(len(data))
	if id == s.watchID && s.started != nil {
		close(s.started)
		s.started = nil
	}
	s.mu.Unlock()
	return s.BlobStore.Put(ctx, id, data)
}

func (s *liveTransferStore) Get(ctx context.Context, ref string) ([]byte, error) {
	data, err := s.BlobStore.Get(ctx, ref)
	s.mu.Lock()
	s.counts.GetCalls++
	s.counts.GetBytes += int64(len(data))
	s.mu.Unlock()
	return data, err
}

func (s *liveTransferStore) snapshot() liveTransferCounts {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts
}

// This is opt-in qualification, never ordinary CI. Coordinator must hold the
// original installation lock and supply only its isolated copied authorization.
func TestLiveTransferQualification(t *testing.T) {
	if os.Getenv("HARBOR_LIVE_CONFIRM") != "generated-fixtures-only" {
		t.Skip("requires coordinator-authorized isolated Telegram fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	check := func(stage string, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("live qualification stage %s failed (%s)", stage, TelegramSetupErrorCode(err))
		}
	}
	number := func(name string) int64 {
		t.Helper()
		value := os.Getenv(name)
		if value == "" {
			return 0
		}
		n, err := strconv.ParseInt(value, 10, 64)
		check("config-"+name, err)
		return n
	}
	channel := number("TELEGRAM_CHANNEL_ID")
	if channel < -1000000000000 && channel > -2000000000000 {
		channel = -channel - 1000000000000
	}
	cfg := TelegramConfig{AppID: int(number("TELEGRAM_APP_ID")), AppHash: os.Getenv("TELEGRAM_APP_HASH"), SessionFile: os.Getenv("TELEGRAM_SESSION_FILE"), ChannelID: channel, ChannelAccessHash: number("TELEGRAM_CHANNEL_ACCESS_HASH"), ChannelLink: os.Getenv("USERNAME_OR_CHANNEL_LINK")}
	state := os.Getenv("HARBOR_LIVE_STATE_DIR")
	if state == "" || !filepath.IsAbs(state) {
		t.Fatal("fresh absolute HARBOR_LIVE_STATE_DIR required")
	}
	check("fresh-state-directory", os.Mkdir(state, 0700))
	key := make([]byte, 32)
	_, err := rand.Read(key)
	check("fixture-key", err)
	check("fixture-key-persistence", os.WriteFile(filepath.Join(state, "content.key"), key, 0600))
	dbPath := filepath.Join(state, "drive.db")
	file, err := os.OpenFile(dbPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	check("fixture-database", err)
	check("fixture-database-close", file.Close())
	db, err := sql.Open("sqlite", dbPath)
	check("fixture-database-open", err)
	defer db.Close()
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000`)
	check("fixture-database-config", err)
	remote, err := NewTelegram(ctx, cfg)
	check("telegram-connect", err)
	defer func() { remote.Close() }()
	store := &liveTransferStore{BlobStore: remote}
	engineConfig := Config{DB: db, Key: key, Backend: store, ChunkSize: DefaultChunkSize, StagingDir: filepath.Join(state, "parts"), ReadCacheDir: filepath.Join(state, "cache")}
	e, err := New(engineConfig)
	check("engine", err)
	worker := func() func() {
		workerCtx, stop := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- e.RunPending(workerCtx) }()
		var once sync.Once
		return func() {
			once.Do(func() {
				stop()
				select {
				case err := <-done:
					check("worker-stop", err)
				case <-time.After(30 * time.Second):
					t.Fatal("live worker did not join")
				}
			})
		}
	}
	stop := worker()
	defer func() { stop() }()
	beginFile := func(name string, size int64) domain.Upload {
		t.Helper()
		u, err := e.Begin(ctx, domain.UploadSpec{Name: name, Size: size, SHA256: fixtureHash(t, 0, size)})
		check("begin-generated-file", err)
		return u
	}
	queueFile := func(u domain.Upload) {
		t.Helper()
		for index := range u.TotalChunks {
			_, count, err := transportGeometry(u, index, 0)
			check("part-geometry", err)
			for part := range int(count) {
				size, _, err := transportGeometry(u, index, part)
				check("part-size", err)
				offset := int64(index)*u.ChunkSize + int64(part)*TransportPartSize
				_, err = e.PutPart(ctx, u.ID, index, part, io.NewSectionReader(largeChunkFixture{}, offset, size), fixtureHash(t, offset, size))
				check("part-acceptance", err)
			}
			_, err = e.QueueChunk(ctx, u.ID, index)
			check("durable-queue", err)
		}
	}
	finish := func(u domain.Upload) float64 {
		t.Helper()
		started := time.Now()
		for {
			state, err := e.Status(ctx, u.ID)
			check("saving-status", err)
			for _, p := range state.PendingChunks {
				if p.State == "failed" {
					t.Fatalf("live save failed (%s)", p.ErrorCode)
				}
			}
			if state.ReceivedBytes == u.Size {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("live saving deadline exceeded")
			case <-time.After(100 * time.Millisecond):
			}
		}
		_, err := e.Complete(ctx, u.ID)
		check("remote-integrity-verification", err)
		return time.Since(started).Seconds()
	}
	verifyFull := func(u domain.Upload) float64 {
		t.Helper()
		started := time.Now()
		r, _, err := e.Open(ctx, u.ID)
		check("full-download-open", err)
		h := sha256.New()
		n, err := io.Copy(h, r)
		r.Close()
		check("full-download", err)
		if n != u.Size || hex.EncodeToString(h.Sum(nil)) != u.SHA256 {
			t.Fatal("live full-download SHA-256 mismatch")
		}
		return time.Since(started).Seconds()
	}
	large := beginFile("Harbor generated Milestone A multipart fixture.bin", DefaultChunkSize+17)
	uploadStarted := time.Now()
	queueFile(large)
	finish(large)
	uploadSeconds := time.Since(uploadStarted).Seconds()
	downloadSeconds := verifyFull(large)
	small := beginFile("Harbor generated Milestone A interrupted fixture.bin", 1<<20)
	store.mu.Lock()
	store.watchID, store.started = small.ID, make(chan struct{})
	started := store.started
	store.mu.Unlock()
	queueFile(small)
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("live interrupted worker never entered Put")
	}
	stop()
	interrupted, err := e.Status(ctx, small.ID)
	check("interrupted-status", err)
	neededRecovery := interrupted.ReceivedBytes != small.Size
	retriesBeforeReconnect := remote.Status().RetryCount
	check("telegram-disconnect", remote.Close())
	reconnected, err := NewTelegram(ctx, cfg)
	check("telegram-reconnect", err)
	remote = reconnected
	store.BlobStore = remote
	e, err = New(engineConfig)
	check("engine-recreation", err)
	stop = worker()
	finish(small) // No File or part bytes supplied again after recreation.
	verifyFull(small)
	var senders sync.WaitGroup
	for index := range 2 {
		u := beginFile(fmt.Sprintf("Harbor generated Milestone A concurrent fixture %d.bin", index), 1<<20)
		senders.Add(1)
		go func() { defer senders.Done(); queueFile(u) }()
	}
	senders.Wait()
	if t.Failed() {
		t.Fatal("concurrent live part acceptance failed")
	}
	items, err := e.List(ctx)
	check("concurrent-upload-state", err)
	for _, item := range items {
		if item.ID != large.ID && item.ID != small.ID {
			finish(item)
			verifyFull(item)
		}
	}
	type rangeResult struct {
		Cache            bool
		Seconds          float64
		GetCalls         int64
		RemoteBytes      int64
		RangeSeconds     []float64
		RangeRemoteGets  []int64
		RangeRemoteBytes []int64
	}
	var ranges []rangeResult
	for _, enabled := range []bool{false, true} {
		if !enabled {
			e.readCache = nil
		} else {
			e.readCache, err = newReadCache(filepath.Join(state, "cache"))
			check("cold-cache", err)
		}
		before := store.snapshot()
		started := time.Now()
		result := rangeResult{Cache: enabled}
		for _, offset := range []int64{0, 128 << 10, 256 << 10, 0} {
			rangeStarted, rangeBefore := time.Now(), store.snapshot()
			r, _, err := e.Open(ctx, large.ID)
			check("range-open", err)
			_, err = r.Seek(offset, io.SeekStart)
			check("range-seek", err)
			data := make([]byte, 256<<10)
			_, err = io.ReadFull(r, data)
			r.Close()
			check("range-read", err)
			expected := make([]byte, len(data))
			largeChunkFixture{}.ReadAt(expected, offset)
			if !bytes.Equal(data, expected) {
				t.Fatal("live overlapping range bytes mismatch")
			}
			result.RangeSeconds = append(result.RangeSeconds, time.Since(rangeStarted).Seconds())
			result.RangeRemoteGets = append(result.RangeRemoteGets, store.snapshot().GetCalls-rangeBefore.GetCalls)
			result.RangeRemoteBytes = append(result.RangeRemoteBytes, store.snapshot().GetBytes-rangeBefore.GetBytes)
		}
		after := store.snapshot()
		result.Seconds, result.GetCalls, result.RemoteBytes = time.Since(started).Seconds(), after.GetCalls-before.GetCalls, after.GetBytes-before.GetBytes
		ranges = append(ranges, result)
	}
	if ranges[0].GetCalls != 4 || ranges[1].GetCalls != 1 || ranges[0].RemoteBytes != 4*maxCiphertextSize || ranges[1].RemoteBytes != maxCiphertextSize {
		t.Fatal("live cache remote-fetch accounting differs")
	}
	r, _, err := e.Open(ctx, large.ID)
	check("cross-chunk-range-open", err)
	_, err = r.Seek(DefaultChunkSize-26, io.SeekStart)
	check("cross-chunk-range-seek", err)
	var cross, expected [43]byte
	_, err = io.ReadFull(r, cross[:])
	r.Close()
	check("cross-chunk-range-read", err)
	largeChunkFixture{}.ReadAt(expected[:], DefaultChunkSize-26)
	if cross != expected {
		t.Fatal("live cross-chunk range bytes mismatch")
	}
	stop()
	report, err := json.Marshal(map[string]any{"status": "PASS", "adapter": "live Telegram", "storage_chunk_size": DefaultChunkSize, "client_part_size": TransportPartSize, "multipart_bytes": large.Size, "upload_seconds_including_remote_verification": uploadSeconds, "full_download_seconds": downloadSeconds, "upload_MiB_per_second": float64(large.Size) / (1 << 20) / uploadSeconds, "download_MiB_per_second": float64(large.Size) / (1 << 20) / downloadSeconds, "interrupted_worker_required_recovery": neededRecovery, "recreated_engine_without_reselecting_parts": true, "concurrent_small_uploads": 2, "sha256_verified": true, "cross_chunk_range_bytes": len(cross), "ranges": ranges, "adapter_calls": store.snapshot(), "adapter_byte_scope": "GetBytes are returned ciphertext bytes; PutBytes are input ciphertext bytes including canceled attempts, not network wire bytes", "retry_count": retriesBeforeReconnect + remote.Status().RetryCount, "cross_dc": "NOT TESTED", "remote_deletions": 0})
	check("safe-report", err)
	t.Log(string(report))
}
