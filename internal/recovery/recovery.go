// Package recovery creates complete encrypted offline backups and restores only
// into new directories. Original metadata and content are read-only; the shared
// drive lock prevents concurrent application writes.
package recovery

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/bcrypt"
	"modernc.org/sqlite"
	"private-drive/internal/storage"
)

const (
	MaxDatabaseBytes = 256 << 20
	MaxMembers       = 250000
	MaxArchiveBytes  = 1 << 40
	maxManifestBytes = 64 << 20
	maxSessionBytes  = 4 << 20
)

type BackupOptions struct {
	DataDir           string
	Output            string
	KeyFile           string
	OwnerPasswordFile string
	SessionFile       string
	ConfigFile        string
	Password          []byte
}

type fileDigest struct {
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type manifest struct {
	Version   int                   `json:"version"`
	CreatedAt string                `json:"created_at"`
	Adapter   string                `json:"adapter"`
	Files     map[string]fileDigest `json:"files"`
}

type binding struct {
	Adapter   string `json:"adapter"`
	KeySHA256 string `json:"key_sha256"`
	ChannelID int64  `json:"telegram_channel_id,omitempty"`
}

type sourceFile struct {
	name string
	path string
	data []byte
	size int64
}

var (
	hexDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)
	blobName  = regexp.MustCompile(`^[0-9a-f]{32}-[0-9a-f]{32}$`)
)

func privateInfo(path string, directory bool) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, errors.New("required recovery source unavailable")
	}
	if info.Mode().Perm()&0077 != 0 || directory && !info.IsDir() || !directory && !info.Mode().IsRegular() {
		return nil, errors.New("recovery sources must be private regular files (0600) and directories (0700), without symlinks")
	}
	return info, nil
}

func readPrivate(path string, limit int64) ([]byte, error) {
	info, err := privateInfo(path, false)
	if err != nil {
		return nil, err
	}
	if info.Size() < 1 || info.Size() > limit {
		return nil, errors.New("recovery source size invalid")
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, errors.New("cannot open private recovery source")
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(b)) > limit {
		return nil, errors.New("cannot read private recovery source")
	}
	return b, nil
}

func storageBinding(raw, key []byte) (binding, error) {
	var b binding
	if json.Unmarshal(raw, &b) != nil || (b.Adapter != "local" && b.Adapter != "telegram") || !hexDigest.MatchString(b.KeySHA256) || len(key) != 32 {
		return b, errors.New("invalid storage binding or missing 32-byte content key")
	}
	hash := sha256.Sum256(key)
	if b.KeySHA256 != hex.EncodeToString(hash[:]) || b.Adapter == "telegram" && b.ChannelID <= 0 {
		return b, errors.New("content key or Telegram channel does not match storage binding")
	}
	return b, nil
}

func sourceDB(path string) (*sql.DB, error) {
	if _, err := privateInfo(path, false); err != nil {
		return nil, err
	}
	u := fileURL(path)
	db, err := sql.Open("sqlite", u+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, errors.New("cannot open recovery database")
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func checkDatabase(ctx context.Context, db *sql.DB) error {
	var integrity string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		return errors.New("SQLite integrity check failed")
	}
	for _, table := range []string{"owner", "sessions", "nodes", "storage_uploads", "storage_chunks"} {
		var name string
		if err := db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name); err != nil {
			return errors.New("required Harbor metadata table missing")
		}
	}
	var hash []byte
	if err := db.QueryRowContext(ctx, "SELECT password_hash FROM owner WHERE id=1").Scan(&hash); err != nil {
		return errors.New("backup requires initialized owner authentication")
	}
	if _, err := bcrypt.Cost(hash); err != nil {
		return errors.New("invalid owner authentication metadata")
	}
	return nil
}

func checkOwnerPassword(ctx context.Context, db *sql.DB, password []byte) error {
	password = bytes.TrimSuffix(bytes.TrimSuffix(password, []byte{'\n'}), []byte{'\r'})
	var hash []byte
	if err := db.QueryRowContext(ctx, "SELECT password_hash FROM owner WHERE id=1").Scan(&hash); err != nil || bcrypt.CompareHashAndPassword(hash, password) != nil {
		return errors.New("saved owner password does not match initialized owner; supply correct private recovery source")
	}
	return nil
}

func snapshot(ctx context.Context, db *sql.DB) ([]byte, error) {
	var pages, size int64
	if err := db.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pages); err != nil {
		return nil, errors.New("cannot read database size")
	}
	if err := db.QueryRowContext(ctx, "PRAGMA page_size").Scan(&size); err != nil || pages <= 0 || size <= 0 || pages > MaxDatabaseBytes/size {
		return nil, errors.New("metadata exceeds 256 MiB in-memory backup limit")
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	var result []byte
	err = conn.Raw(func(raw any) error {
		backup, err := raw.(interface {
			NewBackup(string) (*sqlite.Backup, error)
		}).NewBackup(":memory:")
		if err != nil {
			return errors.New("SQLite backup initialization failed")
		}
		for {
			if err := ctx.Err(); err != nil {
				_ = backup.Finish()
				return err
			}
			more, err := backup.Step(256)
			if err != nil {
				_ = backup.Finish()
				return errors.New("SQLite backup failed")
			}
			if !more {
				break
			}
		}
		target, err := backup.Commit()
		if err != nil {
			return errors.New("SQLite backup commit failed")
		}
		defer target.Close()
		// ponytail: cap metadata at 256 MiB to keep plaintext backups off disk;
		// an encrypted SQLite VFS is the upgrade path for larger metadata sets.
		result, err = target.(interface{ Serialize() ([]byte, error) }).Serialize()
		if err != nil || len(result) > MaxDatabaseBytes {
			clear(result)
			return errors.New("SQLite in-memory snapshot failed or exceeded limit")
		}
		return nil
	})
	return result, err
}

func chunkExport(ctx context.Context, db *sql.DB) ([]byte, error) {
	rows, err := db.QueryContext(ctx, `SELECT upload_id,idx,size,sha256,ref FROM storage_chunks ORDER BY upload_id,idx`)
	if err != nil {
		return nil, errors.New("cannot export chunk manifests")
	}
	defer rows.Close()
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	for rows.Next() {
		var row struct {
			UploadID string `json:"upload_id"`
			Index    int    `json:"index"`
			Size     int64  `json:"size"`
			SHA256   string `json:"sha256"`
			Ref      string `json:"ref"`
		}
		if err := rows.Scan(&row.UploadID, &row.Index, &row.Size, &row.SHA256, &row.Ref); err != nil || row.Index < 0 || row.Size <= 0 || row.Size > storage.MaxChunkSize || !hexDigest.MatchString(row.SHA256) || len(row.Ref) > 16384 {
			return nil, errors.New("invalid chunk manifest in metadata")
		}
		if err := encoder.Encode(row); err != nil || out.Len() > maxManifestBytes {
			return nil, errors.New("chunk manifest exceeds 64 MiB export limit")
		}
	}
	return out.Bytes(), rows.Err()
}

func localBlobs(ctx context.Context, db *sql.DB, data string) ([]sourceFile, error) {
	if _, err := privateInfo(filepath.Join(data, "blobs"), true); err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT ref,size FROM storage_chunks ORDER BY ref`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := make(map[string]bool)
	var files []sourceFile
	for rows.Next() {
		var ref string
		var size int64
		if err := rows.Scan(&ref, &size); err != nil || !blobName.MatchString(ref) || size <= 0 || size > storage.MaxChunkSize {
			return nil, errors.New("invalid local chunk reference")
		}
		path := filepath.Join(data, "blobs", ref)
		info, err := privateInfo(path, false)
		if err != nil || info.Size() != size+32 {
			return nil, errors.New("missing or invalid referenced encrypted blob")
		}
		if !seen[ref] {
			files = append(files, sourceFile{name: "blobs/" + ref, path: path, size: info.Size()})
			seen[ref] = true
		}
	}
	return files, rows.Err()
}

func Backup(ctx context.Context, opts BackupOptions) error {
	data, err := filepath.Abs(opts.DataDir)
	if err != nil {
		return errors.New("invalid source directory")
	}
	if _, err := privateInfo(data, true); err != nil {
		return err
	}
	data, err = filepath.EvalSymlinks(data)
	if err != nil {
		return errors.New("cannot resolve source data directory")
	}
	output, err := filepath.Abs(opts.Output)
	if err != nil || inside(data, output) {
		return errors.New("backup output must be outside source data directory")
	}
	lock, err := os.OpenFile(filepath.Join(data, ".lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return errors.New("cannot lock source drive")
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("drive is running; stop it before complete backup")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	if opts.KeyFile == "" {
		opts.KeyFile = filepath.Join(data, "content.key")
	}
	key, err := readPrivate(opts.KeyFile, 32)
	if err != nil {
		return err
	}
	defer clear(key)
	rawBinding, err := readPrivate(filepath.Join(data, "storage.json"), 16384)
	if err != nil {
		return err
	}
	b, err := storageBinding(rawBinding, key)
	if err != nil {
		return err
	}
	// Web setup keeps one atomically activated private bundle on the data volume.
	webConfig := filepath.Join(data, "setup", "config.env")
	if _, err := os.Lstat(webConfig); err == nil {
		if opts.ConfigFile == "" {
			opts.ConfigFile = webConfig
		}
		if opts.OwnerPasswordFile == "" {
			opts.OwnerPasswordFile = filepath.Join(data, "setup", "owner-password")
		}
		if opts.SessionFile == "" {
			opts.SessionFile = filepath.Join(data, "setup", "telegram.session")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return errors.New("web setup recovery source unavailable")
	}
	config, err := backupConfig(opts.ConfigFile, b)
	if err != nil {
		return err
	}
	defer clear(config)
	db, err := sourceDB(filepath.Join(data, "drive.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	if err := checkDatabase(ctx, db); err != nil {
		return err
	}
	database, err := snapshot(ctx, db)
	if err != nil {
		return err
	}
	defer clear(database)
	chunks, err := chunkExport(ctx, db)
	if err != nil {
		return err
	}
	defer clear(chunks)
	files := []sourceFile{
		{name: "drive.db", data: database}, {name: "storage.json", data: rawBinding},
		{name: "content.key", data: key}, {name: "recovery.env", data: config},
		{name: "chunk-manifests.jsonl", data: chunks},
	}
	explicitOwnerPassword := opts.OwnerPasswordFile != ""
	if opts.OwnerPasswordFile == "" {
		opts.OwnerPasswordFile = filepath.Join(data, "owner-password")
	}
	if _, err := os.Lstat(opts.OwnerPasswordFile); err == nil || !errors.Is(err, os.ErrNotExist) {
		password, err := readPrivate(opts.OwnerPasswordFile, 4096)
		if err != nil {
			return err
		}
		defer clear(password)
		if err := checkOwnerPassword(ctx, db, password); err != nil {
			return err
		}
		files = append(files, sourceFile{name: "owner-password", data: password})
	} else if explicitOwnerPassword {
		return errors.New("explicit owner password recovery source is missing")
	}
	if b.Adapter == "telegram" {
		if opts.SessionFile == "" {
			opts.SessionFile = filepath.Join(data, "telegram.session")
		}
		session, err := readPrivate(opts.SessionFile, maxSessionBytes)
		if err != nil || validateSession(session) != nil {
			return errors.New("complete Telegram backup requires a valid private session file")
		}
		defer clear(session)
		files = append(files, sourceFile{name: "telegram.session", data: session})
	} else {
		blobs, err := localBlobs(ctx, db, data)
		if err != nil {
			return err
		}
		files = append(files, blobs...)
	}
	if len(files)+1 > MaxMembers {
		return errors.New("backup exceeds 250000 member limit")
	}
	var total int64
	for i := range files {
		if files[i].path == "" {
			files[i].size = int64(len(files[i].data))
		}
		total += files[i].size
		if total > MaxArchiveBytes-maxManifestBytes {
			return errors.New("backup exceeds 1 TiB limit")
		}
	}
	if err := os.MkdirAll(filepath.Dir(output), 0700); err != nil {
		return errors.New("cannot create backup directory")
	}
	if _, err := privateInfo(filepath.Dir(output), true); err != nil {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(output))
	if err != nil || inside(data, filepath.Join(parent, filepath.Base(output))) {
		return errors.New("backup output must be outside source data directory")
	}
	tmp, err := os.CreateTemp(parent, ".harbor-encrypted-*")
	if err != nil {
		return errors.New("cannot create encrypted backup")
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if err := writeArchive(ctx, tmp, opts.Password, b.Adapter, files); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return errors.New("cannot persist encrypted backup")
	}
	if err := Verify(ctx, tmp.Name(), opts.Password); err != nil {
		return err
	}
	// Hard-link publication never replaces a concurrently-created backup.
	if err := os.Link(tmp.Name(), output); err != nil {
		return errors.New("backup output exists or cannot be published")
	}
	return syncDir(parent)
}

func writeArchive(ctx context.Context, output io.Writer, password []byte, adapter string, files []sourceFile) error {
	encrypted, err := newEncryptedWriter(output, password)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(encrypted)
	m := manifest{Version: 2, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Adapter: adapter, Files: make(map[string]fileDigest)}
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
	buf := make([]byte, recordBytes)
	defer clear(buf)
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := tw.WriteHeader(&tar.Header{Name: file.name, Mode: 0600, Size: file.size, Typeflag: tar.TypeReg}); err != nil {
			return errors.New("cannot write encrypted backup header")
		}
		var r io.Reader = bytes.NewReader(file.data)
		var f *os.File
		if file.path != "" {
			f, err = os.OpenFile(file.path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
			if err != nil {
				return errors.New("cannot open encrypted chunk for backup")
			}
			r = f
		}
		hash := sha256.New()
		n, copyErr := io.CopyBuffer(io.MultiWriter(tw, hash), &contextReader{ctx: ctx, reader: r}, buf)
		if f != nil {
			_ = f.Close()
		}
		if copyErr != nil || n != file.size {
			return errors.New("recovery source changed or backup interrupted")
		}
		m.Files[file.name] = fileDigest{Size: n, SHA256: hex.EncodeToString(hash.Sum(nil))}
	}
	raw, err := json.Marshal(m)
	if err != nil || len(raw) > maxManifestBytes {
		return errors.New("backup manifest exceeds limit")
	}
	if err := tw.WriteHeader(&tar.Header{Name: "backup-manifest.json", Mode: 0600, Size: int64(len(raw)), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	if _, err := tw.Write(raw); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return encrypted.Close()
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func inside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && (rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}

func syncDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return errors.New("cannot open recovery directory for persistence")
	}
	defer f.Close()
	if err := f.Sync(); err != nil {
		return errors.New("cannot persist recovery directory")
	}
	return nil
}

func fileURL(path string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
}

func Verify(ctx context.Context, archive string, password []byte) error {
	_, err := readArchive(ctx, archive, password, "")
	return err
}

func allowedSize(name string, size int64) bool {
	if size < 0 {
		return false
	}
	switch name {
	case "drive.db":
		return size > 0 && size <= MaxDatabaseBytes
	case "storage.json", "recovery.env":
		return size > 0 && size <= 16384
	case "content.key":
		return size == 32
	case "owner-password":
		return size > 0 && size <= 4096
	case "telegram.session":
		return size > 0 && size <= maxSessionBytes
	case "backup-manifest.json", "chunk-manifests.jsonl":
		return size <= maxManifestBytes
	default:
		return strings.HasPrefix(name, "blobs/") && blobName.MatchString(strings.TrimPrefix(name, "blobs/")) && size > 32 && size <= storage.MaxChunkSize+32
	}
}

func readArchive(ctx context.Context, archive string, password []byte, destination string) (binding, error) {
	var b binding
	f, err := os.OpenFile(archive, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return b, errors.New("cannot open encrypted backup")
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		return b, errArchive
	}
	encrypted, err := newEncryptedReader(&contextReader{ctx: ctx, reader: f}, password)
	if err != nil {
		return b, err
	}
	tr := tar.NewReader(encrypted)
	files := make(map[string]fileDigest)
	seen := make(map[string]bool)
	captured := make(map[string][]byte)
	defer func() {
		for _, data := range captured {
			clear(data)
		}
	}()
	var total int64
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return b, errArchive
		}
		total += header.Size
		if header.Typeflag != tar.TypeReg || !allowedSize(header.Name, header.Size) || len(seen)+1 > MaxMembers || total > MaxArchiveBytes || seen[header.Name] {
			return b, errArchive
		}
		seen[header.Name] = true
		hash := sha256.New()
		var target io.Writer = io.Discard
		var output *os.File
		if destination != "" {
			path := filepath.Join(destination, filepath.FromSlash(header.Name))
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				return b, errors.New("cannot create private restore directory")
			}
			output, err = os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				return b, errors.New("cannot create private restored file")
			}
			target = output
		}
		var capture bytes.Buffer
		captureFile := header.Name != "drive.db" && header.Name != "chunk-manifests.jsonl" && !strings.HasPrefix(header.Name, "blobs/")
		if captureFile {
			target = io.MultiWriter(target, &capture)
		}
		n, copyErr := io.Copy(io.MultiWriter(target, hash), &contextReader{ctx: ctx, reader: tr})
		if output != nil {
			if copyErr == nil {
				copyErr = output.Sync()
			}
			closeErr := output.Close()
			if copyErr == nil {
				copyErr = closeErr
			}
		}
		if copyErr != nil || n != header.Size {
			return b, errArchive
		}
		if header.Name != "backup-manifest.json" {
			files[header.Name] = fileDigest{Size: n, SHA256: hex.EncodeToString(hash.Sum(nil))}
		}
		if captureFile {
			captured[header.Name] = capture.Bytes()
		}
	}
	// tar ends before the encryption stream. Consume its authenticated terminator
	// and reject any hidden plaintext after tar's final zero blocks.
	padding := make([]byte, recordBytes)
	for {
		n, err := encrypted.Read(padding)
		for _, value := range padding[:n] {
			if value != 0 {
				return b, errArchive
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return b, errArchive
		}
	}
	var m manifest
	if json.Unmarshal(captured["backup-manifest.json"], &m) != nil || m.Version != 2 || len(m.Files) != len(files) {
		return b, errArchive
	}
	for name, actual := range files {
		if m.Files[name] != actual || !hexDigest.MatchString(actual.SHA256) {
			return b, errArchive
		}
	}
	for _, name := range []string{"drive.db", "content.key", "storage.json", "recovery.env", "chunk-manifests.jsonl"} {
		if _, ok := files[name]; !ok {
			return b, errors.New("complete backup is missing a required recovery file")
		}
	}
	b, err = storageBinding(captured["storage.json"], captured["content.key"])
	if err != nil || m.Adapter != b.Adapter {
		return b, errArchive
	}
	if _, err := configValues(captured["recovery.env"], b); err != nil {
		return b, err
	}
	if b.Adapter == "telegram" && validateSession(captured["telegram.session"]) != nil {
		return b, errors.New("complete Telegram backup is missing valid session state")
	}
	return b, nil
}

func Restore(ctx context.Context, archive, destination string, password []byte) error {
	destination, err := filepath.Abs(destination)
	if err != nil {
		return errors.New("invalid restore destination")
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		return errors.New("restore destination must not exist")
	}
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return errors.New("cannot create restore parent")
	}
	if _, err := privateInfo(parent, true); err != nil {
		return err
	}
	scratch, err := os.MkdirTemp(parent, ".harbor-restore-*")
	if err != nil {
		return errors.New("cannot create private restore staging directory")
	}
	defer os.RemoveAll(scratch)
	b, err := readArchive(ctx, archive, password, scratch)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", fileURL(filepath.Join(scratch, "drive.db"))+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return errors.New("cannot open restored database")
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	if err := checkDatabase(ctx, db); err != nil {
		return err
	}
	ownerPath := filepath.Join(scratch, "owner-password")
	if _, err := os.Lstat(ownerPath); err == nil {
		password, err := readPrivate(ownerPath, 4096)
		if err != nil {
			return err
		}
		err = checkOwnerPassword(ctx, db, password)
		clear(password)
		if err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return errors.New("cannot inspect restored owner password file")
	}
	if b.Adapter == "local" {
		if err := os.MkdirAll(filepath.Join(scratch, "blobs"), 0700); err != nil {
			return errors.New("cannot create restored encrypted blob directory")
		}
		if _, err := localBlobs(ctx, db, scratch); err != nil {
			return err
		}
	}
	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode=DELETE; DELETE FROM sessions;"); err != nil {
		return errors.New("cannot revoke restored HTTP sessions")
	}
	if err := db.Close(); err != nil {
		return errors.New("cannot close restored database")
	}
	if err := restoreConfig(scratch, destination, b); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(scratch, "backup-manifest.json")); err != nil {
		return errors.New("cannot finish restore manifest verification")
	}
	if err := filepath.WalkDir(scratch, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return syncDir(path)
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		syncErr := f.Sync()
		closeErr := f.Close()
		return errors.Join(syncErr, closeErr)
	}); err != nil {
		return errors.New("cannot persist restored data")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := renameExclusive(scratch, destination); err != nil {
		return errors.New("restore destination exists or cannot be published")
	}
	return syncDir(parent)
}

// Status returns fixed, secret-free text for CLI success messages.
func Status(command string) string {
	switch command {
	case "backup":
		return "Complete encrypted backup created and verified."
	case "restore":
		return "Recovery verified. Restored HTTP sessions revoked. Start with private recovery.env."
	default:
		return "Encrypted archive integrity verified."
	}
}
