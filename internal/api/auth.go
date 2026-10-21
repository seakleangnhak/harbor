package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type loginAttempt struct {
	count int
	reset time.Time
}
type sessionKey struct{}

func (s *Server) initOwner(password string) error {
	err := s.cfg.DB.QueryRow("SELECT password_hash FROM owner WHERE id=1").Scan(&s.password)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if len(password) < 12 || len(password) > 72 {
		return errors.New("first boot requires an owner password of 12 to 72 bytes")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if _, err := s.cfg.DB.Exec("INSERT INTO owner(id,password_hash) VALUES(1,?)", hash); err != nil {
		return err
	}
	s.password = hash
	return nil
}

func (s *Server) sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		site := r.Header.Get("Sec-Fetch-Site")
		if site != "" && site != "same-origin" && site != "none" {
			fail(w, 403, "forbidden_origin", "Cross-origin mutations are forbidden")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			scheme := "http"
			if r.TLS != nil || s.cfg.SecureCookies {
				scheme = "https"
			}
			if err != nil || u.Scheme != scheme || !strings.EqualFold(u.Host, r.Host) || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
				fail(w, 403, "forbidden_origin", "Cross-origin mutations are forbidden")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func tokenHash(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("drive_session")
		if err != nil || len(cookie.Value) != 64 {
			fail(w, 401, "unauthenticated", "Sign in required")
			return
		}
		hash := tokenHash(cookie.Value)
		var expires int64
		if err := s.cfg.DB.QueryRowContext(r.Context(), "SELECT expires_at FROM sessions WHERE token_hash=?", hash).Scan(&expires); err != nil || expires <= time.Now().Unix() {
			fail(w, 401, "unauthenticated", "Sign in required")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey{}, hash)))
	})
}

func (s *Server) allowLogin(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	now := time.Now()
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	for key, attempt := range s.attempts {
		if now.After(attempt.reset) {
			delete(s.attempts, key)
		}
	}
	a, exists := s.attempts[host]
	if !exists {
		if len(s.attempts) >= 1024 {
			return false
		}
		a.reset = now.Add(15 * time.Minute)
	}
	if a.count >= 8 {
		return false
	}
	a.count++
	s.attempts[host] = a
	return true
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !s.allowLogin(r.RemoteAddr) {
		w.Header().Set("Retry-After", "900")
		fail(w, 429, "throttled", "Too many sign-in attempts; retry later")
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := readJSON(w, r, &body); err != nil {
		respondError(w, err)
		return
	}
	if len(body.Password) > 72 || bcrypt.CompareHashAndPassword(s.password, []byte(body.Password)) != nil {
		fail(w, 401, "unauthenticated", "Incorrect password")
		return
	}
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		respondError(w, err)
		return
	}
	value := hex.EncodeToString(token[:])
	expires := time.Now().Add(7 * 24 * time.Hour)
	if _, err := s.cfg.DB.ExecContext(r.Context(), "DELETE FROM sessions WHERE expires_at<=?", time.Now().Unix()); err != nil {
		respondError(w, err)
		return
	}
	if _, err := s.cfg.DB.ExecContext(r.Context(), "INSERT INTO sessions(token_hash,expires_at) VALUES(?,?)", tokenHash(value), expires.Unix()); err != nil {
		respondError(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "drive_session", Value: value, Path: "/", HttpOnly: true, Secure: s.cfg.SecureCookies, SameSite: http.SameSiteStrictMode, Expires: expires, MaxAge: 7 * 24 * 3600})
	s.session(w, r)
}

func (s *Server) session(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{"authenticated": true, "owner": "Owner"})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if _, err := s.cfg.DB.ExecContext(r.Context(), "DELETE FROM sessions WHERE token_hash=?", r.Context().Value(sessionKey{})); err != nil {
		respondError(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "drive_session", Path: "/", HttpOnly: true, Secure: s.cfg.SecureCookies, SameSite: http.SameSiteStrictMode, MaxAge: -1, Expires: time.Unix(1, 0)})
	w.WriteHeader(204)
}
