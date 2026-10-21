package recovery

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"private-drive/internal/api"
	"private-drive/internal/domain"
	"private-drive/internal/storage"
)

var testPassword = []byte("synthetic-backup-password-12345")

type fixture struct {
	root, data, archive string
	db                  *sql.DB
	key, plaintext      []byte
	fileID              string
}

func writePrivate(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func makeFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	f := &fixture{root: root, data: filepath.Join(root, "source"), archive: filepath.Join(root, "backup.harbor"), key: bytes.Repeat([]byte{27}, 32), plaintext: bytes.Repeat([]byte("private recovery fixture\n"), 1000)}
	if err := os.Mkdir(f.data, 0700); err != nil {
		t.Fatal(err)
	}
	writePrivate(t, filepath.Join(f.data, "content.key"), f.key)
	writePrivate(t, filepath.Join(f.data, "owner-password"), []byte("synthetic-owner-password-12345\n"))
	hash := sha256.Sum256(f.key)
	raw, _ := json.Marshal(binding{Adapter: "local", KeySHA256: hex.EncodeToString(hash[:])})
	writePrivate(t, filepath.Join(f.data, "storage.json"), raw)
	var err error
	f.db, err = api.OpenDB(filepath.Join(f.data, "drive.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.db.Close() })
	local, err := storage.NewLocal(filepath.Join(f.data, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	engine, err := storage.New(storage.Config{DB: f.db, Key: f.key, Backend: local})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := api.New(api.Config{DB: f.db, Storage: engine, OwnerPassword: "synthetic-owner-password-12345", Adapter: "local"}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	upload, err := engine.Begin(ctx, domain.UploadSpec{Name: "recovery.txt", ParentID: "nested", Size: int64(len(f.plaintext)), MimeType: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.PutChunk(ctx, upload.ID, 0, bytes.NewReader(f.plaintext), ""); err != nil {
		t.Fatal(err)
	}
	manifest, err := engine.Complete(ctx, upload.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.fileID = manifest.ID
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, statement := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO sessions(token_hash,expires_at) VALUES('old-http-session',9999999999)`, nil},
		{`INSERT INTO nodes(id,parent_id,name,name_key,kind,favorite,trashed_at,trash_root,created_at,updated_at) VALUES('parent','','Original','original','folder',1,NULL,NULL,?,?)`, []any{now, now}},
		{`INSERT INTO nodes(id,parent_id,name,name_key,kind,favorite,trashed_at,trash_root,created_at,updated_at) VALUES('nested','parent','Nested','nested','folder',0,NULL,NULL,?,?)`, []any{now, now}},
		{`INSERT INTO nodes(id,parent_id,name,name_key,kind,favorite,trashed_at,trash_root,created_at,updated_at) VALUES('trash','','Trash fixture','trash fixture','folder',0,?,'trash',?,?)`, []any{now, now, now}},
		{`INSERT INTO nodes(id,parent_id,name,name_key,kind,size,mime_type,sha256,favorite,created_at,updated_at) VALUES(?,'nested','recovery.txt','recovery.txt','file',?,'text/plain',?,1,?,?)`, []any{f.fileID, manifest.Size, manifest.SHA256, now, now}},
	} {
		if _, err := f.db.Exec(statement.sql, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f *fixture) backup(t *testing.T) {
	t.Helper()
	if err := Backup(context.Background(), BackupOptions{DataDir: f.data, Output: f.archive, Password: testPassword}); err != nil {
		t.Fatal(err)
	}
}

func TestCompleteBackupRestoreIncludesCommittedWALAndRevokesOnlyRestoredSessions(t *testing.T) {
	f := makeFixture(t)
	// Keep the SQLite connection open: committed metadata remains in WAL.
	if info, err := os.Stat(filepath.Join(f.data, "drive.db-wal")); err != nil || info.Size() == 0 {
		t.Fatal("fixture has no committed WAL")
	}
	f.backup(t)
	raw, err := os.ReadFile(f.archive)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range [][]byte{f.key, f.plaintext, []byte("recovery.txt"), []byte("SQLite format 3"), []byte("synthetic-owner-password-12345")} {
		if bytes.Contains(raw, secret) {
			t.Fatal("plaintext recovery material in archive")
		}
	}
	if string(raw[:8]) != archiveMagic {
		t.Fatal("not versioned encrypted archive")
	}
	destination := filepath.Join(f.root, "restored")
	if err := Restore(context.Background(), f.archive, destination, testPassword); err != nil {
		t.Fatal(err)
	}
	if err := filepath.WalkDir(destination, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil || info.Mode().Perm()&0077 != 0 {
			t.Errorf("restored state lacks private permissions: %s", entry.Name())
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	restoredKey, _ := os.ReadFile(filepath.Join(destination, "content.key"))
	if !bytes.Equal(restoredKey, f.key) {
		t.Fatal("key not restored")
	}
	for restart := 0; restart < 2; restart++ {
		db, err := api.OpenDB(filepath.Join(destination, "drive.db"))
		if err != nil {
			t.Fatal(err)
		}
		var count int
		if err := db.QueryRow("SELECT count(*) FROM sessions").Scan(&count); err != nil || count != 0 {
			t.Fatal("restored HTTP sessions not revoked", err)
		}
		var parent string
		var favorite int
		if err := db.QueryRow("SELECT parent_id,favorite FROM nodes WHERE id=?", f.fileID).Scan(&parent, &favorite); err != nil || parent != "nested" || favorite != 1 {
			t.Fatal("metadata not preserved", err)
		}
		if err := db.QueryRow("SELECT count(*) FROM nodes WHERE id='trash' AND trashed_at IS NOT NULL AND trash_root='trash'").Scan(&count); err != nil || count != 1 {
			t.Fatal("trash not preserved", err)
		}
		local, err := storage.NewLocal(filepath.Join(destination, "blobs"))
		if err != nil {
			t.Fatal(err)
		}
		engine, err := storage.New(storage.Config{DB: db, Key: restoredKey, Backend: local})
		if err != nil {
			t.Fatal(err)
		}
		reader, manifest, err := engine.Open(context.Background(), f.fileID)
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil || !bytes.Equal(content, f.plaintext) || manifest.Size != int64(len(f.plaintext)) {
			t.Fatal("restored checksum roundtrip failed", err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
	var originalSessions int
	if err := f.db.QueryRow("SELECT count(*) FROM sessions").Scan(&originalSessions); err != nil || originalSessions != 1 {
		t.Fatal("original sessions changed", err)
	}
	config, _ := os.ReadFile(filepath.Join(destination, "recovery.env"))
	if !bytes.Contains(config, []byte(strconvQuote(destination))) || !bytes.Contains(config, []byte(`DRIVE_ADDR="127.0.0.1:8080"`)) {
		t.Fatal("restored paths and local bind not canonical")
	}
	if _, err := os.Stat(filepath.Join(destination, "chunk-manifests.jsonl")); err != nil {
		t.Fatal("chunk export missing", err)
	}
}

func strconvQuote(text string) string { raw, _ := json.Marshal(text); return string(raw) }

func TestEncryptedRecordAuthenticationRejectsOrderTruncationAndTrailingData(t *testing.T) {
	var output bytes.Buffer
	w, err := newEncryptedWriter(&output, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	plaintext := bytes.Repeat([]byte{73}, recordBytes*2+37)
	if _, err := w.Write(plaintext); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	raw := output.Bytes()
	firstEnd := headerBytes + 4 + int(binary.BigEndian.Uint32(raw[headerBytes:]))
	secondEnd := firstEnd + 4 + int(binary.BigEndian.Uint32(raw[firstEnd:]))
	reorder := append([]byte{}, raw[:headerBytes]...)
	reorder = append(reorder, raw[firstEnd:secondEnd]...)
	reorder = append(reorder, raw[headerBytes:firstEnd]...)
	reorder = append(reorder, raw[secondEnd:]...)
	duplicate := append([]byte{}, raw[:firstEnd]...)
	duplicate = append(duplicate, raw[headerBytes:firstEnd]...)
	duplicate = append(duplicate, raw[firstEnd:]...)
	tamper := append([]byte{}, raw...)
	tamper[firstEnd+20] ^= 1
	for name, data := range map[string][]byte{
		"wrong-password": raw, "truncated-record": raw[:firstEnd-1], "missing-terminator": raw[:len(raw)-21],
		"reordered": reorder, "duplicate": duplicate, "bit-flip": tamper, "appended": append(append([]byte{}, raw...), 1),
	} {
		t.Run(name, func(t *testing.T) {
			password := testPassword
			if name == "wrong-password" {
				password = []byte("incorrect-backup-password-12345")
			}
			r, err := newEncryptedReader(bytes.NewReader(data), password)
			if err == nil {
				_, err = io.Copy(io.Discard, r)
			}
			if err == nil {
				t.Fatal("malformed encrypted stream accepted")
			}
		})
	}
	r, err := newEncryptedReader(bytes.NewReader(raw), testPassword)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	if err != nil || !bytes.Equal(got, plaintext) {
		t.Fatal("valid stream failed", err)
	}
}

func TestWrongPasswordCorruptionAndInterruptedRestoreNeverPublish(t *testing.T) {
	f := makeFixture(t)
	f.backup(t)
	raw, _ := os.ReadFile(f.archive)
	for name, data := range map[string][]byte{
		"wrong-password": raw, "corrupt": append([]byte{}, raw...), "truncated": raw[:len(raw)-1], "plaintext-legacy": []byte("old plaintext tar archive"),
	} {
		t.Run(name, func(t *testing.T) {
			if name == "corrupt" {
				data[len(data)/2] ^= 1
			}
			archive := filepath.Join(f.root, name+".harbor")
			writePrivate(t, archive, data)
			password := testPassword
			if name == "wrong-password" {
				password = []byte("incorrect-backup-password-12345")
			}
			destination := filepath.Join(f.root, name)
			if err := Restore(context.Background(), archive, destination, password); err == nil {
				t.Fatal("invalid restore succeeded")
			}
			if _, err := os.Lstat(destination); !os.IsNotExist(err) {
				t.Fatal("invalid restore published")
			}
			staging, _ := filepath.Glob(filepath.Join(f.root, ".harbor-restore-*"))
			if len(staging) != 0 {
				t.Fatal("failed restore left private staging material")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Restore(ctx, f.archive, filepath.Join(f.root, "cancelled"), testPassword); err == nil {
		t.Fatal("interrupted restore succeeded")
	}
}

func TestBackupRejectsRunningDriveMissingKeyAndUnsafeSources(t *testing.T) {
	f := makeFixture(t)
	lock, err := os.OpenFile(filepath.Join(f.data, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if err := Backup(context.Background(), BackupOptions{DataDir: f.data, Output: f.archive, Password: testPassword}); err == nil {
		t.Fatal("live backup accepted")
	}
	_ = lock.Close()
	if err := Backup(context.Background(), BackupOptions{DataDir: f.data, Output: filepath.Join(f.data, "nested", "backup"), Password: testPassword}); err == nil {
		t.Fatal("recursive backup accepted")
	}
	alias := filepath.Join(f.root, "source-alias")
	if err := os.Symlink(f.data, alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(f.data, "exports"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := Backup(context.Background(), BackupOptions{DataDir: f.data, Output: filepath.Join(alias, "exports", "backup"), Password: testPassword}); err == nil {
		t.Fatal("recursive backup through aliased output accepted")
	}
	key := filepath.Join(f.data, "content.key")
	if err := os.Remove(key); err != nil {
		t.Fatal(err)
	}
	if err := Backup(context.Background(), BackupOptions{DataDir: f.data, Output: f.archive, Password: testPassword}); err == nil {
		t.Fatal("missing key accepted")
	}
	outside := filepath.Join(f.root, "external-key")
	writePrivate(t, outside, f.key)
	if err := os.Symlink(outside, key); err != nil {
		t.Fatal(err)
	}
	if err := Backup(context.Background(), BackupOptions{DataDir: f.data, Output: f.archive, Password: testPassword}); err == nil {
		t.Fatal("symlink key accepted")
	}
	_ = os.Remove(key)
	writePrivate(t, key, f.key)
	if err := os.Chmod(key, 0644); err != nil {
		t.Fatal(err)
	}
	if err := Backup(context.Background(), BackupOptions{DataDir: f.data, Output: f.archive, Password: testPassword}); err == nil {
		t.Fatal("public key file accepted")
	}
}

func TestExistingOutputAndRestoreDestinationStayIntact(t *testing.T) {
	f := makeFixture(t)
	f.backup(t)
	archive, _ := os.ReadFile(f.archive)
	if err := Backup(context.Background(), BackupOptions{DataDir: f.data, Output: f.archive, Password: testPassword}); err == nil {
		t.Fatal("backup overwrite accepted")
	}
	after, _ := os.ReadFile(f.archive)
	if !bytes.Equal(archive, after) {
		t.Fatal("existing backup changed")
	}
	if err := Restore(context.Background(), f.archive, f.data, testPassword); err == nil {
		t.Fatal("restore into original accepted")
	}
	destination := filepath.Join(f.root, "raced-directory")
	if err := os.Mkdir(destination, 0700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(f.root, "staged-directory")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := renameExclusive(source, destination); err == nil {
		t.Fatal("atomic publication replaced existing empty directory")
	}
}

func TestMissingLocalChunkAndDatabaseCorruptionRejectBackup(t *testing.T) {
	f := makeFixture(t)
	entries, _ := os.ReadDir(filepath.Join(f.data, "blobs"))
	if err := os.Remove(filepath.Join(f.data, "blobs", entries[0].Name())); err != nil {
		t.Fatal(err)
	}
	if err := Backup(context.Background(), BackupOptions{DataDir: f.data, Output: f.archive, Password: testPassword}); err == nil {
		t.Fatal("missing referenced ciphertext accepted")
	}
	if _, err := os.Lstat(f.archive); !os.IsNotExist(err) {
		t.Fatal("invalid backup published")
	}
}

func TestTelegramConfigAndSessionAreRequiredAndRecovered(t *testing.T) {
	f := makeFixture(t)
	hash := sha256.Sum256(f.key)
	b := binding{Adapter: "telegram", KeySHA256: hex.EncodeToString(hash[:]), ChannelID: 1234567}
	raw, _ := json.Marshal(b)
	writePrivate(t, filepath.Join(f.data, "storage.json"), raw)
	opts := BackupOptions{DataDir: f.data, Output: f.archive, Password: testPassword}
	if err := Backup(context.Background(), opts); err == nil {
		t.Fatal("Telegram backup without app configuration accepted")
	}
	config := filepath.Join(f.root, "private-config")
	writePrivate(t, config, []byte("TELEGRAM_APP_ID=1234\nTELEGRAM_APP_HASH=0123456789abcdef0123456789abcdef\nUSERNAME_OR_CHANNEL_LINK=https://t.me/+synthetic-private-link\nTELEGRAM_CHANNEL_ID=1234567\nDRIVE_KEY_FILE=/old/external/key\nUNRELATED_SECRET=must-not-be-archived\n"))
	opts.ConfigFile = config
	if err := Backup(context.Background(), opts); err == nil {
		t.Fatal("Telegram backup without session accepted")
	}
	session := filepath.Join(f.data, "telegram.session")
	writePrivate(t, session, []byte(`{"Version":1,"Data":{}}`))
	if err := Backup(context.Background(), opts); err == nil {
		t.Fatal("structurally invalid Telegram session accepted")
	}
	authKey := bytes.Repeat([]byte{19}, 256)
	keyFingerprint := sha1.Sum(authKey)
	state, _ := json.Marshal(map[string]any{"Version": 1, "Data": map[string]any{"DC": 2, "Addr": "", "AuthKey": authKey, "AuthKeyID": keyFingerprint[12:]}})
	writePrivate(t, session, state)
	if err := Backup(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(f.root, "telegram-restored")
	if err := Restore(context.Background(), f.archive, destination, testPassword); err != nil {
		t.Fatal(err)
	}
	restored, _ := os.ReadFile(filepath.Join(destination, "telegram.session"))
	if !bytes.Equal(restored, state) {
		t.Fatal("session recovery changed authorization")
	}
	recoveredConfig, _ := os.ReadFile(filepath.Join(destination, "recovery.env"))
	if bytes.Contains(recoveredConfig, []byte("must-not-be-archived")) || bytes.Contains(recoveredConfig, []byte("/old/external/key")) || !bytes.Contains(recoveredConfig, []byte("TELEGRAM_APP_HASH=")) {
		t.Fatal("configuration not allowlisted/canonicalized")
	}
	writePrivate(t, config, []byte("TELEGRAM_APP_ID=1234\nTELEGRAM_APP_HASH=0123456789abcdef0123456789abcdef\nUSERNAME_OR_CHANNEL_LINK=https://t.me/+synthetic-private-link\nTELEGRAM_CHANNEL_ID=7654321\n"))
	opts.Output = filepath.Join(f.root, "mismatch.harbor")
	if err := Backup(context.Background(), opts); err == nil {
		t.Fatal("mismatched channel accepted")
	}
}

func TestAllowlistedConfigLiteralParsing(t *testing.T) {
	values, err := ReadConfig([]byte("export TELEGRAM_APP_ID='1234' # literal\nTELEGRAM_APP_HASH=\"0123456789abcdef0123456789abcdef\"\nUSERNAME_OR_CHANNEL_LINK='https://t.me/+$(touch /tmp/never)'\nUNRELATED=ignored\n"))
	if err != nil || values["TELEGRAM_APP_ID"] != "1234" || !strings.Contains(values["USERNAME_OR_CHANNEL_LINK"], "$(touch") || values["UNRELATED"] != "" {
		t.Fatal("literal allowlisted configuration parsing failed", err)
	}
	for _, raw := range []string{"TELEGRAM_APP_ID=1\nTELEGRAM_APP_ID=2", "TELEGRAM_APP_ID='broken", `TELEGRAM_APP_ID="1234\n5"`} {
		if _, err := ReadConfig([]byte(raw)); err == nil {
			t.Fatal("malformed configuration accepted")
		}
	}
}

func TestAuthenticatedButIncompleteRecoveryMaterialRejected(t *testing.T) {
	f := makeFixture(t)
	database, err := snapshot(context.Background(), f.db)
	if err != nil {
		t.Fatal(err)
	}
	rawBinding, _ := os.ReadFile(filepath.Join(f.data, "storage.json"))
	for _, missing := range []string{"content.key", "storage.json", "drive.db", "recovery.env", "invalid-key", "invalid-database"} {
		t.Run(missing, func(t *testing.T) {
			contents := map[string][]byte{"drive.db": database, "storage.json": rawBinding, "content.key": f.key, "recovery.env": []byte("DRIVE_STORAGE=local\n"), "chunk-manifests.jsonl": nil}
			delete(contents, missing)
			if missing == "invalid-key" {
				contents["content.key"] = bytes.Repeat([]byte{1}, 32)
			}
			if missing == "invalid-database" {
				contents["drive.db"] = []byte("not a SQLite database")
			}
			var files []sourceFile
			for name, data := range contents {
				files = append(files, sourceFile{name: name, data: data, size: int64(len(data))})
			}
			archive := filepath.Join(f.root, missing+".harbor")
			output, err := os.OpenFile(archive, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				t.Fatal(err)
			}
			if err := writeArchive(context.Background(), output, testPassword, "local", files); err != nil {
				t.Fatal(err)
			}
			_ = output.Close()
			destination := filepath.Join(f.root, "restored-"+missing)
			if err := Restore(context.Background(), archive, destination, testPassword); err == nil {
				t.Fatal("incomplete or corrupted recovery material accepted")
			}
			if _, err := os.Lstat(destination); !os.IsNotExist(err) {
				t.Fatal("invalid recovery published")
			}
		})
	}
}

type interruptContext struct {
	context.Context
	checks atomic.Int32
}

func (ctx *interruptContext) Err() error {
	if ctx.checks.Add(1) >= 8 {
		return context.Canceled
	}
	return nil
}

func TestInterruptedExtractionCleansPrivateStagingAndPreservesOriginal(t *testing.T) {
	f := makeFixture(t)
	f.backup(t)
	ctx := &interruptContext{Context: context.Background()}
	destination := filepath.Join(f.root, "interrupted")
	if err := Restore(ctx, f.archive, destination, testPassword); err == nil {
		t.Fatal("interrupted extraction succeeded")
	}
	if ctx.checks.Load() < 8 {
		t.Fatal("interruption did not occur inside archive extraction")
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatal("interrupted restore published")
	}
	staging, _ := filepath.Glob(filepath.Join(f.root, ".harbor-restore-*"))
	if len(staging) != 0 {
		t.Fatal("interrupted extraction left plaintext staging files")
	}
	var originalCount int
	if err := f.db.QueryRow("SELECT count(*) FROM nodes").Scan(&originalCount); err != nil || originalCount != 4 {
		t.Fatal("interruption changed original metadata", err)
	}
}

func TestSavedOwnerPasswordMustMatchAuthenticationMetadata(t *testing.T) {
	f := makeFixture(t)
	writePrivate(t, filepath.Join(f.data, "owner-password"), []byte("stale-owner-password-12345\n"))
	if err := Backup(context.Background(), BackupOptions{DataDir: f.data, Output: f.archive, Password: testPassword}); err == nil {
		t.Fatal("stale owner recovery password accepted")
	}
	if _, err := os.Lstat(f.archive); !os.IsNotExist(err) {
		t.Fatal("unusable owner recovery published")
	}
}

func TestPersistedTelegramSessionFingerprintMatchesGotd(t *testing.T) {
	key := bytes.Repeat([]byte{42}, 256)
	fingerprint := sha1.Sum(key)
	for _, valid := range []bool{true, false} {
		id := append([]byte{}, fingerprint[12:]...)
		if !valid {
			id[0] ^= 1
		}
		raw, _ := json.Marshal(map[string]any{"Version": 1, "Data": map[string]any{"DC": 2, "AuthKey": key, "AuthKeyID": id}})
		if err := validateSession(raw); (err == nil) != valid {
			t.Fatal("session key fingerprint validation disagrees with gotd")
		}
	}
}

func TestRecoveryPrivateChannelLinksMatchStorageAdapterRules(t *testing.T) {
	b := binding{Adapter: "telegram", ChannelID: 1234567}
	base := "TELEGRAM_APP_ID=1234\nTELEGRAM_APP_HASH=0123456789abcdef0123456789abcdef\nUSERNAME_OR_CHANNEL_LINK="
	for _, link := range []string{"https://t.me/+private_fixture", "t.me/+private_fixture", "https://t.me/joinchat/private_fixture", "https://t.me/c/1234567/9", "https://T.ME/c/1234567/9"} {
		if _, err := configValues([]byte(base+link+"\n"), b); err != nil {
			t.Errorf("supported private channel link rejected: %v", err)
		}
	}
	for _, link := range []string{"https://t.me/c/7654321/9", "https://t.me/c/1234567/0", "https://t.me/c/1234567/2147483648", "https://t.me/c/01234567/9", "https://t.me/c/1234567/09", "https://t.me/public_alias", "https://t.me/+", "https://t.me/joinchat/", "https://t.me/+private_fixture?x=1", "https://t.me/+private_fixture#fragment", "https://t.me/c/1234567/9?", "https://t.me/%2bprivate_fixture", "https://user@t.me/+private_fixture", "https://t.me:443/+private_fixture", "http://t.me/+private_fixture", "https://t.me/+private.fixture"} {
		if _, err := configValues([]byte(base+link+"\n"), b); err == nil {
			t.Error("unsupported or mismatched private channel link accepted")
		}
	}
}
