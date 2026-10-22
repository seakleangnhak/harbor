package storage

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"private-drive/internal/domain"
)

const (
	readCacheLimit   = 256 << 20
	readCacheEntries = 2
	readCacheMarker  = ".harbor-read-cache-v1"
	readCacheMagic   = "Harbor ciphertext read cache v1\n"
)

type readCache struct {
	store    *LocalStore
	mu       sync.Mutex
	entries  map[string]*readCacheEntry
	flights  map[string]*readCacheFlight
	clock    uint64
	used     int64
	reserved int64
	writes   int
	disabled bool
}

type readCacheEntry struct {
	file, source string
	size         int64
	used         uint64
	pins         int
}

type readCacheFlight struct {
	done        chan struct{}
	source      string
	err         error
	invalidated bool
}

func newReadCache(dir string) (*readCache, error) {
	if dir == "" {
		return nil, nil
	}
	if info, err := os.Lstat(dir); err == nil && !info.IsDir() {
		return nil, domain.ErrInvalid
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	marker := filepath.Join(dir, readCacheMarker)
	if len(files) == 0 {
		file, err := os.OpenFile(marker, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return nil, err
		}
		_, writeErr := file.WriteString(readCacheMagic)
		closeErr := file.Close()
		if writeErr != nil {
			return nil, writeErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
	} else {
		info, err := os.Lstat(marker)
		if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len(readCacheMagic)) {
			return nil, domain.ErrInvalid
		}
		data, err := os.ReadFile(marker)
		if err != nil || string(data) != readCacheMagic {
			return nil, domain.ErrInvalid
		}
		for _, file := range files {
			name := file.Name()
			pending := strings.TrimPrefix(name, ".pending-")
			valid := len(name) == 65 && name[32] == '-' && isHexID(name[:32]) && isHexID(name[33:])
			valid = valid || pending != name && pending != "" && strings.Trim(pending, "0123456789") == ""
			if name != readCacheMarker && (!valid || !file.Type().IsRegular()) {
				return nil, domain.ErrInvalid
			}
		}
		// Cache state is disposable; never reuse an unverified crash-time fill.
		for _, file := range files {
			if file.Name() != readCacheMarker {
				if err := os.Remove(filepath.Join(dir, file.Name())); err != nil {
					return nil, err
				}
			}
		}
		if err := os.Chmod(marker, 0600); err != nil {
			return nil, err
		}
	}
	store, err := NewLocal(dir)
	if err != nil {
		return nil, err
	}
	return &readCache{store: store, entries: make(map[string]*readCacheEntry), flights: make(map[string]*readCacheFlight)}, nil
}

func readCacheKey(id string, part chunk) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("harbor/read-cache/v1/%s/%d/%d/%s/%s", id, part.index, part.size, part.hash, part.ref))))
}

func (c *readCache) plaintext(ctx context.Context, engine *Engine, id string, part chunk) ([]byte, error) {
	key := readCacheKey(id, part)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		c.mu.Lock()
		if c.disabled {
			c.mu.Unlock()
			return engine.plaintext(ctx, id, part)
		}
		if entry := c.entries[key]; entry != nil {
			entry.pins++
			c.clock++
			entry.used = c.clock
			c.mu.Unlock()
			sealed, err := c.store.Get(ctx, entry.file)
			c.mu.Lock()
			entry.pins--
			c.mu.Unlock()
			if err == nil {
				var data []byte
				data, err = engine.decryptChunk(id, part, sealed)
				if err == nil {
					if err = ctx.Err(); err != nil {
						return nil, err
					}
					return data, nil
				}
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			c.mu.Lock()
			if c.entries[key] == entry {
				c.removeLocked(key, entry)
			}
			c.mu.Unlock()
		} else {
			c.mu.Unlock()
		}
		c.mu.Lock()
		if c.entries[key] != nil {
			c.mu.Unlock()
			continue
		}
		if flight := c.flights[key]; flight != nil {
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-flight.done:
			}
			if flight.err != nil && !errors.Is(flight.err, context.Canceled) && !errors.Is(flight.err, context.DeadlineExceeded) {
				return nil, flight.err
			}
			continue
		}
		flight := &readCacheFlight{done: make(chan struct{}), source: part.ref}
		c.flights[key] = flight
		c.mu.Unlock()
		data, err := c.load(ctx, engine, id, part, key, flight)
		c.mu.Lock()
		flight.err = err
		delete(c.flights, key)
		close(flight.done)
		c.mu.Unlock()
		return data, err
	}
}

func (c *readCache) load(ctx context.Context, engine *Engine, id string, part chunk, key string, flight *readCacheFlight) ([]byte, error) {
	sealed, err := engine.backend.Get(ctx, part.ref)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	size := int64(len(sealed))
	c.mu.Lock()
	reserved := size == part.size+4+int64(engine.aead.NonceSize()+engine.aead.Overhead()) && size <= maxCiphertextSize && !flight.invalidated && c.reserveLocked(size)
	c.mu.Unlock()
	file := ""
	if reserved {
		// Write ciphertext before in-place decryption, but publish only after authentication.
		file, err = c.store.Put(ctx, key[:32], sealed)
		if err != nil {
			c.mu.Lock()
			c.reserved -= size
			c.writes--
			if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				c.disabled = true
			}
			c.mu.Unlock()
			reserved = false
		}
	}
	data, err := engine.decryptChunk(id, part, sealed)
	if ctx.Err() != nil {
		data, err = nil, ctx.Err()
	}
	if reserved {
		c.mu.Lock()
		if err == nil && !c.disabled && !flight.invalidated {
			c.clock++
			c.entries[key] = &readCacheEntry{file: file, source: part.ref, size: size, used: c.clock}
			c.used += size
		} else if deleteErr := c.store.Delete(context.Background(), file); deleteErr != nil {
			c.disabled = true
			c.used += size
		}
		c.reserved -= size
		c.writes--
		c.mu.Unlock()
	}
	return data, err
}

func (c *readCache) reserveLocked(size int64) bool {
	if c.disabled {
		return false
	}
	for c.used+c.reserved+size > readCacheLimit || len(c.entries)+c.writes >= readCacheEntries {
		var oldest string
		for key, entry := range c.entries {
			if entry.pins == 0 && (oldest == "" || entry.used < c.entries[oldest].used) {
				oldest = key
			}
		}
		if oldest == "" || !c.removeLocked(oldest, c.entries[oldest]) {
			return false
		}
	}
	c.reserved += size
	c.writes++
	return true
}

func (c *readCache) removeLocked(key string, entry *readCacheEntry) bool {
	delete(c.entries, key)
	if err := c.store.Delete(context.Background(), entry.file); err != nil {
		c.disabled = true
		return false
	}
	c.used -= entry.size
	return true
}

func (c *readCache) invalidate(ref string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, entry := range c.entries {
		if entry.source == ref {
			c.removeLocked(key, entry)
		}
	}
	for _, flight := range c.flights {
		if flight.source == ref {
			flight.invalidated = true
		}
	}
}
