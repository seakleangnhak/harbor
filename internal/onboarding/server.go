// Package onboarding serves the token-protected, first-run setup flow.
package onboarding

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"private-drive/internal/storage"

	"rsc.io/qr"
)

type TelegramInput struct {
	AppID       int    `json:"app_id"`
	AppHash     string `json:"app_hash"`
	ChannelLink string `json:"channel_link"`
	ChannelID   int64  `json:"channel_id,omitempty"`
}

type Completion struct {
	OwnerPassword string
	Adapter       string
	Telegram      TelegramInput
}

type Config struct {
	Token         string
	SecureCookies bool
	SetupTelegram func(context.Context, TelegramInput, func(context.Context, string) error, func(context.Context) (string, error)) error
	Commit        func(context.Context, Completion) error
	// Completed runs after the successful response is flushed. Signal shutdown;
	// do not synchronously wait for the HTTP server from this callback.
	Completed func()
}

type setupState struct {
	State        string `json:"state"`
	AccountReady bool   `json:"account_ready"`
	FlowActive   bool   `json:"flow_active"`
	QRImage      string `json:"qr_image,omitempty"`
	Error        string `json:"error,omitempty"`
	ErrorCode    string `json:"error_code,omitempty"`
}

type telegramFlow struct {
	input     TelegramInput
	cancel    context.CancelFunc
	done      chan struct{}
	password  chan string
	state     string
	qrImage   string
	error     string
	code      string
	passwords int
}

type attempt struct {
	count int
	reset time.Time
}

type Server struct {
	cfg        Config
	token      [32]byte
	expires    time.Time
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.Mutex
	password   string
	flow       *telegramFlow
	attempts   map[string]attempt
	starts     attempt
	committing chan struct{}
	completed  bool
	closed     bool
}

func New(cfg Config) (*Server, error) {
	if len(cfg.Token) < 24 || len(cfg.Token) > 512 || strings.ContainsAny(cfg.Token, " \t\r\n") {
		return nil, errors.New("setup requires a token of 24 to 512 non-space bytes")
	}
	if cfg.Commit == nil {
		return nil, errors.New("setup requires a commit callback")
	}
	token := sha256.Sum256([]byte(cfg.Token))
	cfg.Token = ""
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{cfg: cfg, token: token, expires: time.Now().Add(time.Hour), ctx: ctx, cancel: cancel, attempts: make(map[string]attempt)}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/setup/status", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		required := !s.completed
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]bool{"required": required})
	})
	mux.Handle("POST /api/v1/setup/verify", s.protect(http.HandlerFunc(s.status)))
	mux.Handle("POST /api/v1/setup/account", s.protect(http.HandlerFunc(s.account)))
	mux.Handle("GET /api/v1/setup/telegram", s.protect(http.HandlerFunc(s.status)))
	mux.Handle("POST /api/v1/setup/telegram", s.protect(http.HandlerFunc(s.telegram)))
	mux.Handle("POST /api/v1/setup/telegram/password", s.protect(http.HandlerFunc(s.telegramPassword)))
	mux.Handle("DELETE /api/v1/setup/telegram", s.protect(http.HandlerFunc(s.cancelTelegram)))
	mux.Handle("POST /api/v1/setup/complete", s.protect(http.HandlerFunc(s.complete)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		mux.ServeHTTP(w, r)
	})
}

// Close cancels setup and waits for its callbacks to release the session.
// Injected callbacks must respect their context.
func (s *Server) Close() {
	s.mu.Lock()
	s.closed = true
	s.cancel()
	var done chan struct{}
	if s.flow != nil {
		done = s.flow.done
	}
	committing := s.committing
	s.mu.Unlock()
	if done != nil {
		<-done
	}
	if committing != nil {
		<-committing
	}
	s.mu.Lock()
	s.password = ""
	s.flow = nil
	s.mu.Unlock()
}

func (s *Server) protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.sameOrigin(r) {
			fail(w, http.StatusForbidden, "forbidden_origin", "Use the same Harbor address for setup")
			return
		}
		s.mu.Lock()
		if s.closed || s.completed {
			s.mu.Unlock()
			fail(w, http.StatusNotFound, "setup_unavailable", "Setup is no longer available")
			return
		}
		if time.Now().After(s.expires) {
			s.mu.Unlock()
			fail(w, http.StatusUnauthorized, "setup_expired", "Setup expired; restart Harbor to begin again")
			return
		}
		provided := sha256.Sum256([]byte(r.Header.Get("X-Harbor-Setup-Token")))
		if subtle.ConstantTimeCompare(provided[:], s.token[:]) != 1 {
			if !s.allowFailedToken(r.RemoteAddr) {
				s.mu.Unlock()
				w.Header().Set("Retry-After", "900")
				fail(w, http.StatusTooManyRequests, "throttled", "Too many setup attempts; retry later")
				return
			}
			s.mu.Unlock()
			fail(w, http.StatusUnauthorized, "setup_token_invalid", "Enter the setup token from your container environment")
			return
		}
		// Handlers own state locking; never hold it across Telegram or persistence.
		s.mu.Unlock()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) sameOrigin(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		scheme := "http"
		if r.TLS != nil || s.cfg.SecureCookies {
			scheme = "https"
		}
		return err == nil && u.Scheme == scheme && strings.EqualFold(u.Host, r.Host) && u.Path == "" && u.RawQuery == "" && u.Fragment == "" && u.User == nil
	}
	return true
}

// Called with mu held. Failed-token state is bounded even with spoofed peers.
func (s *Server) allowFailedToken(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	now := time.Now()
	for key, a := range s.attempts {
		if now.After(a.reset) {
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

func (s *Server) stateLocked() setupState {
	state := setupState{State: "waiting", AccountReady: s.password != "", FlowActive: s.flow != nil && s.flow.state != "failed"}
	if s.flow != nil {
		state.State, state.QRImage, state.Error, state.ErrorCode = s.flow.state, s.flow.qrImage, s.flow.error, s.flow.code
	}
	return state
}

func (s *Server) status(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	state := s.stateLocked()
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) mutableLocked(w http.ResponseWriter) bool {
	if s.completed || s.closed || s.committing != nil {
		fail(w, http.StatusConflict, "setup_busy", "Setup is being saved or has already finished")
		return false
	}
	return true
}

func (s *Server) account(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if len(body.Password) < 12 || len(body.Password) > 72 || strings.ContainsAny(body.Password, "\r\n") {
		fail(w, http.StatusBadRequest, "invalid_password", "Use an owner password of 12 to 72 bytes without line breaks")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.mutableLocked(w) {
		return
	}
	s.password = body.Password
	writeJSON(w, http.StatusOK, s.stateLocked())
}

func (s *Server) telegram(w http.ResponseWriter, r *http.Request) {
	var input TelegramInput
	if !readJSON(w, r, &input) {
		return
	}
	if !validTelegram(&input) {
		fail(w, http.StatusBadRequest, "invalid_telegram", "Use a valid API ID/hash and an owned private Telegram channel link")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.mutableLocked(w) {
		return
	}
	if s.cfg.SetupTelegram == nil {
		fail(w, http.StatusServiceUnavailable, "build_disabled", "This build does not support Telegram setup")
		return
	}
	if s.flow != nil {
		select {
		case <-s.flow.done:
		default:
			fail(w, http.StatusConflict, "setup_busy", "Cancel or finish the current Telegram login first")
			return
		}
	}
	now := time.Now()
	if now.After(s.starts.reset) {
		s.starts = attempt{reset: now.Add(15 * time.Minute)}
	}
	if s.starts.count >= 4 {
		w.Header().Set("Retry-After", "900")
		fail(w, http.StatusTooManyRequests, "throttled", "Too many Telegram login attempts; wait before retrying")
		return
	}
	s.starts.count++
	ctx, cancel := context.WithTimeout(s.ctx, 3*time.Minute)
	flow := &telegramFlow{input: input, cancel: cancel, done: make(chan struct{}), password: make(chan string, 1), state: "waiting"}
	s.flow = flow
	go s.runTelegram(ctx, flow, input)
	writeJSON(w, http.StatusAccepted, s.stateLocked())
}

func (s *Server) runTelegram(ctx context.Context, flow *telegramFlow, input TelegramInput) {
	err := s.cfg.SetupTelegram(ctx, input, func(ctx context.Context, value string) error {
		u, parseErr := url.Parse(value)
		if parseErr != nil || len(value) > 2048 || u.Scheme != "tg" || u.Host != "login" || u.Path != "" || u.User != nil || u.Fragment != "" || len(u.Query()) != 1 || u.Query().Get("token") == "" {
			return errors.New("invalid Telegram QR")
		}
		code, encodeErr := qr.Encode(value, qr.M)
		if encodeErr != nil {
			return errors.New("cannot render Telegram QR")
		}
		image := "data:image/png;base64," + base64.StdEncoding.EncodeToString(code.PNG())
		s.mu.Lock()
		defer s.mu.Unlock()
		if ctx.Err() != nil || s.closed {
			return context.Canceled
		}
		flow.state, flow.qrImage = "scanning", image
		return nil
	}, func(ctx context.Context) (string, error) {
		s.mu.Lock()
		if flow.passwords >= 3 || ctx.Err() != nil || s.closed {
			s.mu.Unlock()
			return "", context.Canceled
		}
		flow.passwords++
		flow.state, flow.qrImage = "password", ""
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case password := <-flow.password:
			return password, nil
		}
	})
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	flow.qrImage = ""
	if err == nil {
		flow.state = "verified"
	} else {
		flow.state = "failed"
		flow.input = TelegramInput{}
		flow.code = storage.TelegramSetupErrorCode(err)
		flow.error = telegramErrorMessage(flow.code)
	}
	flow.cancel()
	select {
	case <-flow.password:
	default:
	}
	close(flow.done)
}

func (s *Server) telegramPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if len(body.Password) == 0 || len(body.Password) > 1024 {
		fail(w, http.StatusBadRequest, "invalid_password", "Enter the Telegram two-step password")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.mutableLocked(w) {
		return
	}
	if s.flow == nil || s.flow.state != "password" {
		fail(w, http.StatusConflict, "password_not_requested", "Telegram is not waiting for a two-step password")
		return
	}
	select {
	case s.flow.password <- body.Password:
		s.flow.state = "waiting"
		writeJSON(w, http.StatusAccepted, s.stateLocked())
	default:
		fail(w, http.StatusConflict, "setup_busy", "A two-step password is already being checked")
	}
}

func (s *Server) cancelTelegram(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	if !s.mutableLocked(w) {
		s.mu.Unlock()
		return
	}
	if s.flow != nil {
		s.flow.cancel()
		s.flow.input = TelegramInput{}
		s.flow.state, s.flow.qrImage, s.flow.code = "failed", "", "canceled"
		s.flow.error = telegramErrorMessage("canceled")
	}
	// Keep the worker attached until it exits, preventing concurrent session use.
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) complete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Adapter string `json:"adapter"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if body.Adapter != "telegram" && body.Adapter != "local" {
		fail(w, http.StatusBadRequest, "invalid_adapter", "Choose Telegram or local storage")
		return
	}
	s.mu.Lock()
	if !s.mutableLocked(w) {
		s.mu.Unlock()
		return
	}
	if s.password == "" {
		s.mu.Unlock()
		fail(w, http.StatusConflict, "account_required", "Create the owner password before finishing setup")
		return
	}
	completion := Completion{OwnerPassword: s.password, Adapter: body.Adapter}
	if s.flow != nil {
		select {
		case <-s.flow.done:
		default:
			s.mu.Unlock()
			fail(w, http.StatusConflict, "setup_busy", "Finish or cancel Telegram login before saving")
			return
		}
	}
	if body.Adapter == "telegram" {
		if s.flow == nil || s.flow.state != "verified" {
			s.mu.Unlock()
			fail(w, http.StatusConflict, "telegram_required", "Verify the Telegram account and channel before finishing setup")
			return
		}
		completion.Telegram = s.flow.input
	}
	done := make(chan struct{})
	s.committing = done
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
	err := s.cfg.Commit(ctx, completion)
	cancel()
	s.mu.Lock()
	s.committing = nil
	if err == nil {
		s.completed = true
		s.password, s.flow = "", nil
	}
	close(done)
	s.mu.Unlock()
	if err != nil {
		fail(w, http.StatusInternalServerError, "setup_save_failed", "Setup could not be saved; check private storage permissions and retry")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"required": false})
	if flush, ok := w.(http.Flusher); ok {
		flush.Flush()
	}
	if s.cfg.Completed != nil {
		s.cfg.Completed()
	}
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		fail(w, http.StatusUnsupportedMediaType, "invalid_content_type", "Send setup data as JSON")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(dst); err == nil {
		if err = decoder.Decode(new(any)); errors.Is(err, io.EOF) {
			return true
		}
	}
	var oversized *http.MaxBytesError
	if errors.As(err, &oversized) {
		fail(w, http.StatusRequestEntityTooLarge, "body_too_large", "Setup request is too large")
	} else {
		fail(w, http.StatusBadRequest, "invalid_request", "Invalid setup data")
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func fail(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error": message, "code": code})
}

func validTelegram(input *TelegramInput) bool {
	appHash, err := hex.DecodeString(input.AppHash)
	if input.AppID <= 0 || int64(input.AppID) > 1<<31-1 || err != nil || len(appHash) != 16 {
		return false
	}
	if input.ChannelID < 0 {
		if input.ChannelID >= -1000000000000 || input.ChannelID <= -2000000000000 {
			return false
		}
		input.ChannelID = -input.ChannelID - 1000000000000
	}
	link := strings.TrimSpace(input.ChannelLink)
	if len(link) > 2048 || strings.ContainsAny(link, "?#") {
		return false
	}
	if strings.HasPrefix(strings.ToLower(link), "t.me/") {
		link = "https://" + link
	}
	u, err := url.Parse(link)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || !strings.EqualFold(u.Host, "t.me") || u.User != nil || u.RawPath != "" {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	invite := ""
	switch {
	case len(parts) == 1 && strings.HasPrefix(parts[0], "+"):
		invite = strings.TrimPrefix(parts[0], "+")
	case len(parts) == 2 && parts[0] == "joinchat":
		invite = parts[1]
	case len(parts) == 3 && parts[0] == "c":
		id, idErr := strconv.ParseInt(parts[1], 10, 64)
		message, msgErr := strconv.ParseInt(parts[2], 10, 32)
		if idErr != nil || msgErr != nil || id <= 0 || message <= 0 || strconv.FormatInt(id, 10) != parts[1] || strconv.FormatInt(message, 10) != parts[2] || input.ChannelID != 0 && input.ChannelID != id {
			return false
		}
		input.ChannelLink = link
		return true
	}
	if invite == "" || len(invite) > 256 {
		return false
	}
	for _, ch := range invite {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-') {
			return false
		}
	}
	input.ChannelLink = link
	return true
}

func telegramErrorMessage(code string) string {
	switch code {
	case "api_credentials":
		return "Telegram rejected the API ID/hash; check the application details"
	case "password_invalid", "password_required":
		return "Telegram two-step verification failed; retry with the account password"
	case "channel_mismatch":
		return "The channel link does not match the expected channel ID"
	case "channel_access":
		return "Use a valid private link and the dedicated account that owns the channel"
	case "channel_retention":
		return "Disable channel auto-delete before using it for storage"
	case "flood_wait":
		return "Telegram requested a wait; avoid repeated login attempts and retry later"
	case "timeout":
		return "Telegram setup timed out; check connectivity and scan a fresh QR"
	case "canceled":
		return "Telegram setup was canceled"
	case "session_invalid", "auth_restart":
		return "Telegram authorization expired; start a fresh QR login"
	case "build_disabled":
		return "This build does not support Telegram; use the Telegram-enabled image"
	default:
		return "Telegram setup failed; check connectivity and account/channel access before retrying"
	}
}
