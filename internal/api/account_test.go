package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const changedPassword = "new synthetic owner password"

func TestAccountProfileValidationAndPersistence(t *testing.T) {
	f := newFixture(t)
	requireStatus(t, f.request("PATCH", "/api/v1/auth/profile", map[string]string{"display_name": "Owner"}, nil), 401)
	requireStatus(t, f.request("POST", "/api/v1/auth/password", map[string]string{"current_password": testPassword, "new_password": changedPassword}, nil), 401)
	f.login()
	for _, route := range [][2]string{{"PATCH", "/api/v1/auth/profile"}, {"POST", "/api/v1/auth/password"}} {
		for _, headers := range []map[string]string{{"Origin": "https://hostile.test"}, {"Sec-Fetch-Site": "cross-site"}} {
			requireStatus(t, f.request(route[0], route[1], map[string]string{}, headers), 403)
		}
	}
	for _, name := range []string{"", "  ", "\nOwner", "a\x00b", "a\u007fb", strings.Repeat("a", 81), strings.Repeat("界", 81)} {
		requireStatus(t, f.request("PATCH", "/api/v1/auth/profile", map[string]string{"display_name": name}, nil), 400)
	}
	for _, body := range [][]byte{
		[]byte(`{"display_name":"Owner","role":"admin"}`),
		append(append([]byte(`{"display_name":"`), byte(0xff)), []byte(`"}`)...),
	} {
		requireStatus(t, f.request("PATCH", "/api/v1/auth/profile", body, nil), 400)
	}
	requireStatus(t, f.request("PATCH", "/api/v1/auth/profile", []byte(strings.Repeat(" ", 65<<10)), nil), 413)
	name := strings.Repeat("界", 80)
	got := decode[struct {
		Authenticated bool   `json:"authenticated"`
		Owner         string `json:"owner"`
	}](t, f.request("PATCH", "/api/v1/auth/profile", map[string]string{"display_name": "  " + name + "  "}, nil), 200)
	if !got.Authenticated || got.Owner != name {
		t.Fatal("profile response did not contain trimmed Unicode name")
	}
	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	f.start("")
	session := decode[map[string]any](t, f.request("GET", "/api/v1/auth/session", nil, nil), 200)
	if session["owner"] != name || len(session) != 2 {
		t.Fatal("session lost display name or disclosed private account fields")
	}
}

func TestAccountProfileMigrationPreservesExistingOwner(t *testing.T) {
	f := newFixture(t)
	f.login()
	if _, err := f.db.Exec("DROP TABLE owner_profile"); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	f.start("")
	got := decode[map[string]any](t, f.request("GET", "/api/v1/auth/session", nil, nil), 200)
	if got["owner"] != "Owner" {
		t.Fatal("legacy migration did not create default owner profile")
	}
	var changed string
	if err := f.db.QueryRow("SELECT password_changed_at FROM owner_profile WHERE id=1").Scan(&changed); err != nil || changed != "" {
		t.Fatal("legacy migration incorrectly recorded a password change")
	}
}

func TestAccountPasswordValidationRevocationAndRestart(t *testing.T) {
	f := newFixture(t)
	f.login()
	first := *f.cookie
	f.login()
	second := *f.cookie
	requireStatus(t, f.request("PATCH", "/api/v1/auth/profile", map[string]string{"display_name": "Private owner"}, nil), 200)
	for _, password := range []string{"", strings.Repeat("a", 11), strings.Repeat("a", 73), "password\nwith line", "password\rwith line", strings.Repeat("界", 25)} {
		requireStatus(t, f.request("POST", "/api/v1/auth/password", map[string]string{"current_password": testPassword, "new_password": password}, nil), 400)
	}
	invalidUTF8 := append(append([]byte(`{"current_password":"`+testPassword+`","new_password":"valid-prefix`), byte(0xff)), []byte(`"}`)...)
	requireStatus(t, f.request("POST", "/api/v1/auth/password", invalidUTF8, nil), 400)
	wrong := f.request("POST", "/api/v1/auth/password", map[string]string{"current_password": "incorrect", "new_password": changedPassword}, nil)
	got := decode[map[string]string](t, wrong, 403)
	if got["code"] != "incorrect_password" || len(wrong.Result().Cookies()) != 0 {
		t.Fatal("wrong current password must preserve session and return actionable error")
	}
	requireStatus(t, f.request("GET", "/api/v1/auth/session", nil, nil), 200)
	f.api.cfg.SecureCookies = true
	w := f.request("POST", "/api/v1/auth/password", map[string]string{"current_password": testPassword, "new_password": changedPassword}, nil)
	requireStatus(t, w, 204)
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Value != "" || cookies[0].MaxAge != -1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("password change did not clear secure session cookie")
	}
	var hash []byte
	var changed string
	var count int
	if err := f.db.QueryRow("SELECT password_hash FROM owner WHERE id=1").Scan(&hash); err != nil || bcrypt.CompareHashAndPassword(hash, []byte(changedPassword)) != nil || bytes.Contains(hash, []byte(changedPassword)) {
		t.Fatal("new password was not persisted as bcrypt hash")
	}
	if err := f.db.QueryRow("SELECT password_changed_at FROM owner_profile WHERE id=1").Scan(&changed); err != nil {
		t.Fatal(err)
	}
	if _, err := time.Parse(time.RFC3339Nano, changed); err != nil {
		t.Fatal("password change recovery marker missing")
	}
	if err := f.db.QueryRow("SELECT count(*) FROM sessions").Scan(&count); err != nil || count != 0 {
		t.Fatal("password change did not revoke every session")
	}
	for _, cookie := range []*http.Cookie{&first, &second} {
		f.cookie = cookie
		requireStatus(t, f.request("GET", "/api/v1/auth/session", nil, nil), 401)
	}
	f.cookie = nil
	requireStatus(t, f.request("POST", "/api/v1/auth/login", map[string]string{"password": testPassword}, nil), 401)
	gotSession := decode[map[string]any](t, f.request("POST", "/api/v1/auth/login", map[string]string{"password": changedPassword}, nil), 200)
	if gotSession["owner"] != "Private owner" {
		t.Fatal("password change lost owner display name")
	}
	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	// Startup credentials initialize empty databases; an old initialization value cannot undo a change.
	f.start(testPassword)
	requireStatus(t, f.request("POST", "/api/v1/auth/login", map[string]string{"password": testPassword}, nil), 401)
	requireStatus(t, f.request("POST", "/api/v1/auth/login", map[string]string{"password": changedPassword}, nil), 200)
	for _, cookie := range []*http.Cookie{&first, &second} {
		f.cookie = cookie
		requireStatus(t, f.request("GET", "/api/v1/auth/session", nil, nil), 401)
	}
}

func TestAccountPasswordUTF8ByteBoundaries(t *testing.T) {
	f := newFixture(t)
	f.login()
	current := testPassword
	for _, next := range []string{strings.Repeat("界", 4), strings.Repeat("界", 24)} {
		requireStatus(t, f.request("POST", "/api/v1/auth/password", map[string]string{"current_password": current, "new_password": next}, nil), 204)
		f.cookie = nil
		w := f.request("POST", "/api/v1/auth/login", map[string]string{"password": next}, nil)
		requireStatus(t, w, 200)
		cookies := w.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatal("boundary password did not establish a new session")
		}
		f.cookie = cookies[0]
		current = next
	}
}

func TestAccountPasswordVerificationThrottle(t *testing.T) {
	f := newFixture(t)
	f.login()
	for attempt := range 7 {
		headers := map[string]string{"X-Forwarded-For": "192.0.2." + strings.Repeat("1", attempt+1)}
		requireStatus(t, f.request("POST", "/api/v1/auth/password", map[string]string{"current_password": "incorrect", "new_password": changedPassword}, headers), 403)
	}
	w := f.request("POST", "/api/v1/auth/password", map[string]string{"current_password": testPassword, "new_password": changedPassword}, nil)
	requireStatus(t, w, 429)
	if w.Header().Get("Retry-After") != "900" || len(w.Result().Cookies()) != 0 {
		t.Fatal("throttled password change issued cookie or omitted retry guidance")
	}
	requireStatus(t, f.request("GET", "/api/v1/auth/session", nil, nil), 200)
}

func TestAccountPasswordChangeRollsBackWhenRevocationFails(t *testing.T) {
	f := newFixture(t)
	f.login()
	var before []byte
	if err := f.db.QueryRow("SELECT password_hash FROM owner WHERE id=1").Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec("CREATE TRIGGER refuse_session_revoke BEFORE DELETE ON sessions BEGIN SELECT RAISE(ABORT,'synthetic revoke failure'); END"); err != nil {
		t.Fatal(err)
	}
	w := f.request("POST", "/api/v1/auth/password", map[string]string{"current_password": testPassword, "new_password": changedPassword}, nil)
	requireStatus(t, w, 500)
	if len(w.Result().Cookies()) != 0 || strings.Contains(w.Body.String(), "synthetic revoke failure") {
		t.Fatal("failed transaction cleared cookie or exposed SQL error")
	}
	var after []byte
	var changed string
	if err := f.db.QueryRow("SELECT password_hash FROM owner WHERE id=1").Scan(&after); err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed revocation persisted new password")
	}
	if err := f.db.QueryRow("SELECT password_changed_at FROM owner_profile WHERE id=1").Scan(&changed); err != nil || changed != "" {
		t.Fatal("failed revocation persisted recovery marker")
	}
	requireStatus(t, f.request("GET", "/api/v1/auth/session", nil, nil), 200)
	if _, err := f.db.Exec("DROP TRIGGER refuse_session_revoke"); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, f.request("POST", "/api/v1/auth/password", map[string]string{"current_password": testPassword, "new_password": changedPassword}, nil), 204)
}

type accountReadSignal struct {
	io.Reader
	once  sync.Once
	ready chan<- struct{}
}

func (r *accountReadSignal) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err == io.EOF {
		r.once.Do(func() { r.ready <- struct{}{} })
	}
	return n, err
}

func TestAccountConcurrentLoginAndPasswordChanges(t *testing.T) {
	for _, concurrent := range []string{"login", "change"} {
		t.Run(concurrent, func(t *testing.T) {
			f := newFixture(t)
			f.login()
			ready := make(chan struct{}, 2)
			type response struct {
				kind string
				w    *httptest.ResponseRecorder
			}
			responses := make(chan response, 2)
			f.api.authMu.Lock()
			for _, kind := range []string{"change", concurrent} {
				body := map[string]string{"current_password": testPassword, "new_password": changedPassword}
				path := "/api/v1/auth/password"
				if kind == "login" {
					body = map[string]string{"password": testPassword}
					path = "/api/v1/auth/login"
				}
				raw, err := json.Marshal(body)
				if err != nil {
					f.api.authMu.Unlock()
					t.Fatal(err)
				}
				req := httptest.NewRequest("POST", "http://drive.test"+path, &accountReadSignal{Reader: bytes.NewReader(raw), ready: ready})
				req.Header.Set("Content-Type", "application/json")
				req.AddCookie(f.cookie)
				go func() {
					w := httptest.NewRecorder()
					f.api.Handler().ServeHTTP(w, req)
					responses <- response{kind, w}
				}()
			}
			for range 2 {
				select {
				case <-ready:
				case <-time.After(5 * time.Second):
					f.api.authMu.Unlock()
					t.Fatal("requests did not reach serialization boundary")
				}
			}
			f.api.authMu.Unlock()
			changed := 0
			for range 2 {
				select {
				case result := <-responses:
					if result.kind == "change" && result.w.Code == 204 {
						changed++
					} else if result.kind == "change" {
						requireStatus(t, result.w, 401)
					} else if result.w.Code != 200 && result.w.Code != 401 {
						t.Fatalf("unexpected concurrent login status %d", result.w.Code)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("serialized requests did not finish")
				}
			}
			if changed != 1 {
				t.Fatal("concurrent password requests did not produce exactly one change")
			}
			var count int
			if err := f.db.QueryRow("SELECT count(*) FROM sessions").Scan(&count); err != nil || count != 0 {
				t.Fatal("old-password login issued a session after revocation")
			}
			f.cookie = nil
			requireStatus(t, f.request("POST", "/api/v1/auth/login", map[string]string{"password": changedPassword}, nil), 200)
		})
	}
}
