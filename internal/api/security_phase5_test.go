package api

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPhase5ExpiredAndRevokedSessionsSurviveRestart(t *testing.T) {
	f := newFixture(t)
	f.login()
	expired := *f.cookie
	if _, err := f.db.Exec("UPDATE sessions SET expires_at=?", time.Now().Add(-time.Second).Unix()); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, f.request("GET", "/api/v1/nodes", nil, nil), 401)
	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	f.start("")
	requireStatus(t, f.request("GET", "/api/v1/auth/session", nil, nil), 401)
	f.cookie = nil
	f.login()
	current := *f.cookie
	if current.Value == expired.Value {
		t.Fatal("login reused expired token")
	}
	var count int
	if err := f.db.QueryRow("SELECT count(*) FROM sessions").Scan(&count); err != nil || count != 1 {
		t.Fatalf("expired sessions were not removed: count=%d err=%v", count, err)
	}
	requireStatus(t, f.request("POST", "/api/v1/auth/logout", map[string]any{}, nil), 204)
	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	f.start("")
	f.cookie = &current
	requireStatus(t, f.request("GET", "/api/v1/nodes", nil, nil), 401)
	f.cookie = &expired
	requireStatus(t, f.request("GET", "/api/v1/nodes", nil, nil), 401)
}

func TestPhase5LoginThrottleIgnoresSpoofedProxyHeaders(t *testing.T) {
	f := newFixture(t)
	for attempt := range 8 {
		headers := map[string]string{
			"X-Forwarded-For": fmt.Sprintf("192.0.2.%d", attempt+1),
			"Forwarded":       fmt.Sprintf("for=192.0.2.%d", attempt+1),
			"X-Real-IP":       fmt.Sprintf("198.51.100.%d", attempt+1),
		}
		w := f.request("POST", "/api/v1/auth/login", map[string]string{"password": "wrong"}, headers)
		requireStatus(t, w, 401)
		if len(w.Result().Cookies()) != 0 {
			t.Fatal("failed login issued a cookie")
		}
	}
	w := f.request("POST", "/api/v1/auth/login", map[string]string{"password": testPassword}, nil)
	requireStatus(t, w, 429)
	if w.Header().Get("Retry-After") != "900" || len(w.Result().Cookies()) != 0 {
		t.Fatal("throttle response is missing delay or issued a session")
	}
	var count int
	if err := f.db.QueryRow("SELECT count(*) FROM sessions").Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed or throttled login persisted a session: %d %v", count, err)
	}
	f.api.loginMu.Lock()
	for host, attempt := range f.api.attempts {
		attempt.reset = time.Now().Add(-time.Second)
		f.api.attempts[host] = attempt
	}
	f.api.loginMu.Unlock()
	f.login()
}

func TestPhase5SessionCredentialAndOriginBoundaries(t *testing.T) {
	f := newFixture(t)
	f.login()
	cookie := *f.cookie
	if cookie.Secure || cookie.Path != "/" || cookie.Domain != "" || cookie.MaxAge != 7*24*3600 {
		t.Fatal("unexpected local-session cookie policy")
	}
	f.cookie = nil
	requireStatus(t, f.request("GET", "/api/v1/nodes?token="+cookie.Value, nil, map[string]string{"Authorization": "Bearer " + cookie.Value}), 401)
	f.cookie = &http.Cookie{Name: "drive_session", Value: strings.Repeat("f", 64)}
	requireStatus(t, f.request("GET", "/api/v1/nodes", nil, nil), 401)
	f.cookie = &cookie
	for _, origin := range []string{"null", "https://drive.test", "http://drive.test.evil.test", "http://drive.test@evil.test", "http://drive.test/path", "http://drive.test?secret=x", "http://drive.test#fragment"} {
		requireStatus(t, f.request("POST", "/api/v1/folders", map[string]string{"name": "blocked"}, map[string]string{"Origin": origin}), 403)
	}
	for _, site := range []string{"cross-site", "same-site", "invalid"} {
		requireStatus(t, f.request("POST", "/api/v1/folders", map[string]string{"name": "blocked"}, map[string]string{"Origin": "http://drive.test", "Sec-Fetch-Site": site}), 403)
	}
	f.api.cfg.SecureCookies = true
	requireStatus(t, f.request("POST", "/api/v1/folders", map[string]string{"name": "proxy"}, map[string]string{"Origin": "https://drive.test", "Sec-Fetch-Site": "same-origin"}), 201)
	requireStatus(t, f.request("POST", "/api/v1/folders", map[string]string{"name": "blocked"}, map[string]string{"Origin": "http://drive.test", "X-Forwarded-Proto": "https"}), 403)
	f.cookie = nil
	f.login()
	if !f.cookie.Secure || !f.cookie.HttpOnly || f.cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("secure deployment cookie lost security flags")
	}
}

func TestPhase5PrivateSQLiteFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "drive.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE private_fixture(value TEXT); INSERT INTO private_fixture VALUES('private metadata')"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{path, path + "-wal", path + "-shm"} {
		info, err := os.Lstat(name)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			t.Fatalf("SQLite file is not private: %s: %v", filepath.Base(name), err)
		}
	}
}

func TestPhase5SQLiteRejectsSymlinkAndSpecialFiles(t *testing.T) {
	for _, suffix := range []string{"", "-wal", "-shm"} {
		for _, kind := range []string{"symlink", "directory"} {
			t.Run(suffix+"/"+kind, func(t *testing.T) {
				root := t.TempDir()
				path := filepath.Join(root, "drive.db")
				outside := filepath.Join(root, "outside")
				target, err := OpenDB(outside)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := target.Exec("CREATE TABLE preserve(value TEXT); INSERT INTO preserve VALUES('unrelated private fixture')"); err != nil {
					t.Fatal(err)
				}
				if err := target.Close(); err != nil {
					t.Fatal(err)
				}
				original, err := os.ReadFile(outside)
				if err != nil {
					t.Fatal(err)
				}
				if kind == "symlink" {
					err = os.Symlink(outside, path+suffix)
				} else {
					err = os.Mkdir(path+suffix, 0700)
				}
				if err != nil {
					t.Fatal(err)
				}
				db, err := OpenDB(path)
				if db != nil {
					db.Close()
				}
				if err == nil {
					t.Fatal("unsafe SQLite path accepted")
				}
				data, err := os.ReadFile(outside)
				if err != nil || !bytes.Equal(data, original) {
					t.Fatal("rejected SQLite open changed unrelated file")
				}
			})
		}
	}
}
