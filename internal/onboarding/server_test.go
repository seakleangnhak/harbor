package onboarding

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image/png"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var fixtureToken = strings.Repeat("setup-fixture-", 3)

func fixtureTelegram() TelegramInput {
	return TelegramInput{AppID: 12345, AppHash: strings.Repeat("a1", 16), ChannelLink: "https://t.me/+synthetic_invite"}
}

func fixtureServer(t *testing.T, cfg Config) *Server {
	t.Helper()
	if cfg.Token == "" {
		cfg.Token = fixtureToken
	}
	if cfg.Commit == nil {
		cfg.Commit = func(context.Context, Completion) error { return nil }
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func request(s *Server, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	var data []byte
	if raw, ok := body.(json.RawMessage); ok {
		data = raw
	} else if body != nil {
		data, _ = json.Marshal(body)
	}
	r := httptest.NewRequest(method, "http://harbor.test"+path, bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Harbor-Setup-Token", fixtureToken)
	for key, value := range headers {
		r.Header.Set(key, value)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func requireStatus(t *testing.T, response *httptest.ResponseRecorder, expected int) {
	t.Helper()
	if response.Code != expected {
		t.Fatalf("status %d, expected %d: %s", response.Code, expected, response.Body.String())
	}
}

func waitState(t *testing.T, s *Server, wanted string) setupState {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		response := request(s, "GET", "/api/v1/setup/telegram", nil, nil)
		requireStatus(t, response, 200)
		var state setupState
		if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
			t.Fatal(err)
		}
		if state.State == wanted {
			return state
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("setup never reached %s", wanted)
	return setupState{}
}

func TestSetupAuthorizationAndPublicStatus(t *testing.T) {
	s := fixtureServer(t, Config{})
	response := request(s, "GET", "/api/v1/setup/status", nil, map[string]string{"X-Harbor-Setup-Token": ""})
	requireStatus(t, response, 200)
	if strings.TrimSpace(response.Body.String()) != `{"required":true}` || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("public status exposed setup state or cacheable data")
	}
	for _, path := range []string{"", "?token=" + fixtureToken} {
		requireStatus(t, request(s, "POST", "/api/v1/setup/verify"+path, nil, map[string]string{"X-Harbor-Setup-Token": ""}), 401)
	}
	for _, headers := range []map[string]string{
		{"Origin": "https://hostile.test"},
		{"Origin": "http://harbor.test/path"},
		{"Origin": "http://harbor.test?query"},
		{"Origin": "null"},
		{"Sec-Fetch-Site": "same-site"},
		{"Sec-Fetch-Site": "cross-site"},
	} {
		requireStatus(t, request(s, "GET", "/api/v1/setup/telegram", nil, headers), 403)
	}
	requireStatus(t, request(s, "POST", "/api/v1/setup/verify", nil, map[string]string{"Origin": "http://harbor.test", "Sec-Fetch-Site": "same-origin"}), 200)
	for i := 2; i < 8; i++ {
		requireStatus(t, request(s, "POST", "/api/v1/setup/verify", nil, map[string]string{"X-Harbor-Setup-Token": "wrong"}), 401)
	}
	requireStatus(t, request(s, "POST", "/api/v1/setup/verify", nil, map[string]string{"X-Harbor-Setup-Token": "wrong"}), 429)
	// A known token remains usable behind a proxy shared with a blocked peer.
	requireStatus(t, request(s, "POST", "/api/v1/setup/verify", nil, nil), 200)
	secure := fixtureServer(t, Config{SecureCookies: true})
	requireStatus(t, request(secure, "POST", "/api/v1/setup/verify", nil, map[string]string{"Origin": "https://harbor.test"}), 200)
	requireStatus(t, request(secure, "POST", "/api/v1/setup/verify", nil, map[string]string{"Origin": "http://harbor.test", "X-Forwarded-Proto": "http"}), 403)
}

func TestSetupBodiesAndPasswordValidation(t *testing.T) {
	s := fixtureServer(t, Config{})
	for _, password := range []string{"short", strings.Repeat("a", 73), strings.Repeat("界", 25), "line break password\n", "line break\rpassword", "line break password\r\n"} {
		requireStatus(t, request(s, "POST", "/api/v1/setup/account", map[string]string{"password": password}, nil), 400)
	}
	requireStatus(t, request(s, "POST", "/api/v1/setup/account", map[string]string{"password": "synthetic owner password"}, nil), 200)
	requireStatus(t, request(s, "POST", "/api/v1/setup/account", map[string]string{"password": "synthetic owner password", "extra": "not allowed"}, nil), 400)
	requireStatus(t, request(s, "POST", "/api/v1/setup/account", map[string]string{"password": strings.Repeat("a", 9<<10)}, nil), 413)
	requireStatus(t, request(s, "POST", "/api/v1/setup/account", map[string]string{"password": "synthetic owner password"}, map[string]string{"Content-Type": "text/plain"}), 415)
	requireStatus(t, request(s, "POST", "/api/v1/setup/account", json.RawMessage(`{"password":"synthetic owner password"} {}`), nil), 400)
	requireStatus(t, request(s, "POST", "/api/v1/setup/complete", map[string]string{"adapter": "other"}, nil), 400)
}

func TestLocalCompletionAndPermanentlyClosedSetup(t *testing.T) {
	var committed Completion
	var completed atomic.Int32
	s := fixtureServer(t, Config{
		Commit:    func(_ context.Context, value Completion) error { committed = value; return nil },
		Completed: func() { completed.Add(1) },
	})
	requireStatus(t, request(s, "POST", "/api/v1/setup/complete", map[string]string{"adapter": "local"}, nil), 409)
	requireStatus(t, request(s, "POST", "/api/v1/setup/account", map[string]string{"password": "synthetic owner password"}, nil), 200)
	requireStatus(t, request(s, "POST", "/api/v1/setup/complete", map[string]string{"adapter": "telegram"}, nil), 409)
	response := request(s, "POST", "/api/v1/setup/complete", map[string]string{"adapter": "local"}, nil)
	requireStatus(t, response, 200)
	if !response.Flushed || completed.Load() != 1 || committed.Adapter != "local" || committed.OwnerPassword != "synthetic owner password" || committed.Telegram.AppID != 0 {
		t.Fatal("completion did not preserve owner configuration and flush response")
	}
	requireStatus(t, request(s, "POST", "/api/v1/setup/account", map[string]string{"password": "replacement owner password"}, nil), 404)
	if response = request(s, "GET", "/api/v1/setup/status", nil, nil); strings.TrimSpace(response.Body.String()) != `{"required":false}` {
		t.Fatal("completed setup remained available")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.password != "" || s.flow != nil {
		t.Fatal("completed setup retained secret state")
	}
}

func TestTelegramQRPasswordAndCommit(t *testing.T) {
	input := fixtureTelegram()
	input.ChannelID = -1000000012345
	var committed Completion
	s := fixtureServer(t, Config{
		SetupTelegram: func(ctx context.Context, got TelegramInput, showQR func(context.Context, string) error, password func(context.Context) (string, error)) error {
			if got.ChannelID != 12345 {
				return errors.New("channel ID was not normalized")
			}
			if err := showQR(ctx, "tg://login?token=c3ludGhldGlj"); err != nil {
				return err
			}
			value, err := password(ctx)
			if err != nil {
				return err
			}
			if value != "synthetic two-step password" {
				return errors.New("unexpected password")
			}
			return nil
		},
		Commit: func(_ context.Context, value Completion) error { committed = value; return nil },
	})
	requireStatus(t, request(s, "POST", "/api/v1/setup/account", map[string]string{"password": "synthetic owner password"}, nil), 200)
	requireStatus(t, request(s, "POST", "/api/v1/setup/telegram/password", map[string]string{"password": "synthetic two-step password"}, nil), 409)
	requireStatus(t, request(s, "POST", "/api/v1/setup/telegram", input, nil), 202)
	state := waitState(t, s, "password")
	if !state.AccountReady || state.QRImage != "" {
		t.Fatal("two-step state did not hide QR")
	}
	requireStatus(t, request(s, "POST", "/api/v1/setup/telegram", input, nil), 409)
	requireStatus(t, request(s, "POST", "/api/v1/setup/complete", map[string]string{"adapter": "telegram"}, nil), 409)
	requireStatus(t, request(s, "POST", "/api/v1/setup/telegram/password", map[string]string{"password": "synthetic two-step password"}, nil), 202)
	waitState(t, s, "verified")
	response := request(s, "GET", "/api/v1/setup/telegram", nil, nil)
	for _, secret := range []string{input.AppHash, input.ChannelLink, "two-step password", "synthetic owner password"} {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatal("protected state echoed credentials")
		}
	}
	requireStatus(t, request(s, "POST", "/api/v1/setup/complete", map[string]string{"adapter": "telegram"}, nil), 200)
	if committed.Telegram.ChannelID != 12345 || committed.Telegram.AppHash != input.AppHash || committed.Telegram.ChannelLink != input.ChannelLink {
		t.Fatal("verified Telegram configuration did not reach commit")
	}
}

func TestQRRenderingCancellationAndSafeErrors(t *testing.T) {
	var active atomic.Int32
	s := fixtureServer(t, Config{SetupTelegram: func(ctx context.Context, _ TelegramInput, showQR func(context.Context, string) error, _ func(context.Context) (string, error)) error {
		if active.Add(1) != 1 {
			t.Error("overlapping Telegram sessions")
		}
		defer active.Add(-1)
		if err := showQR(ctx, "tg://login?token=c3ludGhldGlj"); err != nil {
			return err
		}
		<-ctx.Done()
		return ctx.Err()
	}})
	requireStatus(t, request(s, "POST", "/api/v1/setup/telegram", fixtureTelegram(), nil), 202)
	state := waitState(t, s, "scanning")
	pngBytes, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(state.QRImage, "data:image/png;base64,"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := png.Decode(bytes.NewReader(pngBytes)); err != nil {
		t.Fatal("QR was not a valid PNG")
	}
	requireStatus(t, request(s, "DELETE", "/api/v1/setup/telegram", nil, nil), 204)
	state = waitState(t, s, "failed")
	if state.ErrorCode != "canceled" || state.QRImage != "" {
		t.Fatal("cancellation retained credential QR")
	}
	s.Close()
	if active.Load() != 0 {
		t.Fatal("Close returned before Telegram session exited")
	}
	requireStatus(t, request(s, "POST", "/api/v1/setup/verify", nil, nil), 404)
	failed := fixtureServer(t, Config{SetupTelegram: func(context.Context, TelegramInput, func(context.Context, string) error, func(context.Context) (string, error)) error {
		return errors.New("sensitive upstream detail")
	}})
	requireStatus(t, request(failed, "POST", "/api/v1/setup/telegram", fixtureTelegram(), nil), 202)
	if state := waitState(t, failed, "failed"); strings.Contains(state.Error, "sensitive") || state.ErrorCode != "unknown" {
		t.Fatal("provider error escaped redaction")
	}
}

func TestFailedCommitIsRetryableAndConcurrentMutationsFreeze(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	s := fixtureServer(t, Config{Commit: func(ctx context.Context, _ Completion) error {
		if calls.Add(1) == 1 {
			close(started)
			select {
			case <-release:
				return errors.New("sensitive path or key")
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}})
	requireStatus(t, request(s, "POST", "/api/v1/setup/account", map[string]string{"password": "synthetic owner password"}, nil), 200)
	finished := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		finished <- request(s, "POST", "/api/v1/setup/complete", map[string]string{"adapter": "local"}, nil)
	}()
	<-started
	requireStatus(t, request(s, "POST", "/api/v1/setup/account", map[string]string{"password": "replacement owner password"}, nil), 409)
	requireStatus(t, request(s, "DELETE", "/api/v1/setup/telegram", nil, nil), 409)
	requireStatus(t, request(s, "POST", "/api/v1/setup/complete", map[string]string{"adapter": "local"}, nil), 409)
	close(release)
	response := <-finished
	requireStatus(t, response, 500)
	if strings.Contains(response.Body.String(), "sensitive") {
		t.Fatal("commit error escaped redaction")
	}
	requireStatus(t, request(s, "POST", "/api/v1/setup/complete", map[string]string{"adapter": "local"}, nil), 200)
}

func TestCanceledSuccessCannotCommitTelegram(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	s := fixtureServer(t, Config{SetupTelegram: func(context.Context, TelegramInput, func(context.Context, string) error, func(context.Context) (string, error)) error {
		close(started)
		<-release
		return nil
	}})
	requireStatus(t, request(s, "POST", "/api/v1/setup/account", map[string]string{"password": "synthetic owner password"}, nil), 200)
	requireStatus(t, request(s, "POST", "/api/v1/setup/telegram", fixtureTelegram(), nil), 202)
	<-started
	s.mu.Lock()
	done := s.flow.done
	s.mu.Unlock()
	requireStatus(t, request(s, "DELETE", "/api/v1/setup/telegram", nil, nil), 204)
	close(release)
	<-done
	if state := waitState(t, s, "failed"); state.ErrorCode != "canceled" {
		t.Fatal("late success replaced cancellation")
	}
	requireStatus(t, request(s, "POST", "/api/v1/setup/complete", map[string]string{"adapter": "telegram"}, nil), 409)
}

func TestTelegramInputValidationAndAttemptLimits(t *testing.T) {
	for _, link := range []string{"", "https://t.me/public_name", "https://evil.test/+invite", "http://t.me/+invite", "https://t.me/+invite?secret", "https://t.me/+invite#fragment", "https://t.me/%2Bencoded", "https://t.me/c/001/1", "https://t.me/c/123/0"} {
		input := fixtureTelegram()
		input.ChannelLink = link
		if validTelegram(&input) {
			t.Fatalf("accepted invalid private link %q", link)
		}
	}
	for _, link := range []string{"https://t.me/+synthetic_invite", "t.me/joinchat/synthetic_invite", "https://t.me/c/123/4"} {
		input := fixtureTelegram()
		input.ChannelLink = link
		if !validTelegram(&input) {
			t.Fatalf("rejected private link %q", link)
		}
	}
	input := fixtureTelegram()
	input.ChannelID, input.ChannelLink = 456, "https://t.me/c/123/4"
	if validTelegram(&input) {
		t.Fatal("accepted mismatched private-message channel ID")
	}
	s := fixtureServer(t, Config{SetupTelegram: func(context.Context, TelegramInput, func(context.Context, string) error, func(context.Context) (string, error)) error {
		return context.DeadlineExceeded
	}})
	for i := 0; i < 4; i++ {
		requireStatus(t, request(s, "POST", "/api/v1/setup/telegram", fixtureTelegram(), nil), 202)
		if state := waitState(t, s, "failed"); state.ErrorCode != "timeout" {
			t.Fatal("timeout classification lost")
		}
	}
	requireStatus(t, request(s, "POST", "/api/v1/setup/telegram", fixtureTelegram(), nil), 429)
	s.mu.Lock()
	s.expires = time.Now().Add(-time.Second)
	s.mu.Unlock()
	requireStatus(t, request(s, "POST", "/api/v1/setup/verify", nil, nil), 401)
}

func TestInvalidServerConfiguration(t *testing.T) {
	if _, err := New(Config{Token: "short", Commit: func(context.Context, Completion) error { return nil }}); err == nil {
		t.Fatal("accepted short setup token")
	}
	if _, err := New(Config{Token: fixtureToken}); err == nil {
		t.Fatal("accepted missing commit callback")
	}
}
