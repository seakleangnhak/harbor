//go:build telegram

package storage

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/auth/qrlogin"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"

	"private-drive/internal/domain"
)

func TestTelegramSetupErrorCodeNeverReturnsProviderText(t *testing.T) {
	for _, fixture := range []struct {
		err  error
		code string
	}{
		{context.Canceled, "canceled"},
		{context.DeadlineExceeded, "timeout"},
		{errTelegramAuthMethod, "auth_method_unsupported"},
		{tgerr.New(400, "API_ID_INVALID"), "api_credentials"},
		{tgerr.New(400, "API_ID_PUBLISHED_FLOOD"), "api_credentials"},
		{errTelegramCredentials, "api_credentials"},
		{tgerr.New(406, "PHONE_NUMBER_INVALID"), "phone_invalid"},
		{tgerr.New(400, "PHONE_NUMBER_BANNED"), "phone_banned"},
		{tgerr.New(400, "PHONE_CODE_INVALID"), "code_invalid"},
		{tgerr.New(400, "PHONE_CODE_EMPTY"), "code_invalid"},
		{tgerr.New(400, "PHONE_CODE_EXPIRED"), "code_expired"},
		{tgerr.New(400, "PHONE_CODE_HASH_INVALID"), "code_expired"},
		{auth.ErrPasswordInvalid, "password_invalid"},
		{tgerr.New(400, "PASSWORD_HASH_INVALID"), "password_invalid"},
		{auth.ErrPasswordNotProvided, "password_required"},
		{auth.ErrPasswordAuthNeeded, "password_required"},
		{tgerr.New(401, "SESSION_PASSWORD_NEEDED"), "password_required"},
		{tgerr.New(420, "FLOOD_WAIT_12345"), "flood_wait"},
		{tgerr.New(400, "PHONE_NUMBER_FLOOD"), "flood_wait"},
		{session.ErrNotFound, "session_invalid"},
		{errTelegramSession, "session_invalid"},
		{tgerr.New(401, "SESSION_REVOKED"), "session_invalid"},
		{tgerr.New(500, "AUTH_RESTART_12345"), "auth_restart"},
		{errTelegramChannelMismatch, "channel_mismatch"},
		{errTelegramChannelAccess, "channel_access"},
		{tgerr.New(400, "INVITE_HASH_EXPIRED"), "channel_access"},
		{errTelegramRetention, "channel_retention"},
		{errTelegramConnection, "connection"},
		{&net.DNSError{Err: "private-fixture", Name: "private-fixture", IsTimeout: true}, "timeout"},
		{&net.OpError{Op: "private-fixture", Net: "private-fixture", Err: errors.New("private-fixture")}, "connection"},
		{tgerr.New(400, "PRIVATE_FIXTURE_12345"), "unknown"},
		{errors.New("private-fixture phone, code and API hash"), "unknown"},
	} {
		err := fmt.Errorf("private-fixture phone, code and API hash: %w", fixture.err)
		if got := TelegramSetupErrorCode(err); got != fixture.code {
			t.Fatalf("expected static classification %q, got %q", fixture.code, got)
		}
	}
	if got := TelegramSetupErrorCode(opaqueTelegramError{cause: auth.ErrPasswordInvalid}); got != "password_invalid" {
		t.Fatalf("opaque wrapped error not classified: %q", got)
	}
}

type opaqueTelegramError struct{ cause error }

func (opaqueTelegramError) Error() string   { panic("provider error text must never be read") }
func (e opaqueTelegramError) Unwrap() error { return e.cause }

func TestTelegramCodeDeliveryForwardsOnlyStaticMethod(t *testing.T) {
	for _, fixture := range []struct {
		kind      tg.AuthSentCodeTypeClass
		method    string
		supported bool
	}{
		{&tg.AuthSentCodeTypeApp{Length: 5}, "app", true},
		{&tg.AuthSentCodeTypeSMS{Length: 5}, "sms", true},
		{&tg.AuthSentCodeTypeCall{Length: 5}, "call", true},
		{&tg.AuthSentCodeTypeMissedCall{Prefix: "private-fixture-phone", Length: 5}, "missed_call", false},
		{&tg.AuthSentCodeTypeSMSWord{Beginning: "private-fixture-word"}, "sms_word", true},
		{&tg.AuthSentCodeTypeSMSPhrase{Beginning: "private-fixture-phrase"}, "sms_phrase", true},
		{&tg.AuthSentCodeTypeEmailCode{EmailPattern: "private-fixture-email", Length: 5}, "email_code", false},
		{&tg.AuthSentCodeTypeSetUpEmailRequired{}, "email_setup", false},
		{&tg.AuthSentCodeTypeFirebaseSMS{Receipt: "private-fixture-receipt", Nonce: []byte("private-fixture-nonce")}, "firebase_sms", false},
		{&tg.AuthSentCodeTypeFragmentSMS{URL: "https://private-fixture.invalid/private-fixture", Length: 5}, "fragment_sms", false},
		{&tg.AuthSentCodeTypeFlashCall{Pattern: "private-fixture-pattern"}, "flash_call", false},
		{nil, "unsupported", false},
	} {
		t.Run(fixture.method, func(t *testing.T) {
			calls, notices := 0, 0
			a := terminalAuth{
				phone: "private-fixture-phone",
				code: func(context.Context) (string, error) {
					calls++
					if notices != 1 {
						t.Fatal("code prompt preceded delivery guidance")
					}
					return "private-fixture-code", nil
				},
				delivery: func(method string) {
					notices++
					if method != fixture.method || strings.Contains(method, "private-fixture") {
						t.Fatalf("delivery exposed more than static method: %q", method)
					}
				},
			}
			value, err := a.Code(context.Background(), &tg.AuthSentCode{Type: fixture.kind, PhoneCodeHash: "private-fixture-hash"})
			if notices != 1 {
				t.Fatalf("wrong delivery notification count: %d", notices)
			}
			if fixture.supported {
				if calls != 1 || err != nil || value != "private-fixture-code" {
					t.Fatalf("supported method failed: calls=%d failed=%v", calls, err != nil)
				}
			} else if calls != 0 || value != "" || TelegramSetupErrorCode(err) != "auth_method_unsupported" {
				t.Fatalf("unsupported method prompted ordinary code: calls=%d code=%q", calls, TelegramSetupErrorCode(err))
			}
		})
	}
	a := terminalAuth{code: func(context.Context) (string, error) { return "fixture", nil }}
	if value, err := a.Code(context.Background(), &tg.AuthSentCode{Type: &tg.AuthSentCodeTypeApp{}}); err != nil || value != "fixture" {
		t.Fatal("optional delivery hook broke existing code callback")
	}
	if _, err := a.Code(context.Background(), nil); TelegramSetupErrorCode(err) != "auth_method_unsupported" {
		t.Fatal("missing sent-code metadata prompted ordinary code")
	}
}

func TestTelegramQRCodeAuthenticationUsesNativeFlow(t *testing.T) {
	for _, fixture := range []struct {
		name       string
		authorized bool
		password   bool
		migrate    bool
		showErr    bool
	}{
		{name: "existing session", authorized: true},
		{name: "scan while rendering"},
		{name: "two-step password", password: true},
		{name: "DC migration", migrate: true},
		{name: "renderer failure", showErr: true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			dispatcher, accepted := telegramQRUpdates()
			exports, renders, passwords, passwordChecks, migrations, imports := 0, 0, 0, 0, 0, 0
			rendererError := errors.New("fixture renderer unavailable")
			api := tg.NewClient(telegramTestInvoker(func(_ context.Context, input bin.Encoder, output bin.Decoder) error {
				switch input.(type) {
				case *tg.UsersGetUsersRequest:
					if fixture.authorized {
						output.(*tg.UserClassVector).Elems = []tg.UserClass{&tg.User{ID: 1}}
						return nil
					}
					return tgerr.New(401, "AUTH_KEY_UNREGISTERED")
				case *tg.AuthExportLoginTokenRequest:
					exports++
					if exports == 1 {
						output.(*tg.AuthLoginTokenBox).LoginToken = &tg.AuthLoginToken{Token: []byte("private-fixture-token"), Expires: int(time.Now().Add(time.Minute).Unix())}
						return nil
					}
					if fixture.password {
						return tgerr.New(401, "SESSION_PASSWORD_NEEDED")
					}
					if fixture.migrate {
						output.(*tg.AuthLoginTokenBox).LoginToken = &tg.AuthLoginTokenMigrateTo{DCID: 3, Token: []byte("private-fixture-migration")}
					} else {
						output.(*tg.AuthLoginTokenBox).LoginToken = &tg.AuthLoginTokenSuccess{Authorization: &tg.AuthAuthorization{User: &tg.User{ID: 1}}}
					}
				case *tg.AuthImportLoginTokenRequest:
					imports++
					output.(*tg.AuthLoginTokenBox).LoginToken = &tg.AuthLoginTokenSuccess{Authorization: &tg.AuthAuthorization{User: &tg.User{ID: 1}}}
				case *tg.AccountGetPasswordRequest:
					passwordChecks++
					return tgerr.New(400, "PASSWORD_HASH_INVALID")
				default:
					t.Fatalf("QR authentication performed unexpected/OTP RPC: %T", input)
				}
				return nil
			}))
			client := auth.NewClient(api, rand.Reader, 1, strings.Repeat("0", 32))
			qr := qrlogin.NewQR(api, 1, strings.Repeat("0", 32), qrlogin.Options{Migrate: func(_ context.Context, dc int) error {
				migrations++
				if dc != 3 {
					t.Fatal("unexpected migration destination")
				}
				return nil
			}})
			a := terminalAuth{
				code:     func(context.Context) (string, error) { t.Fatal("QR requested phone OTP"); return "", nil },
				password: func(context.Context) (string, error) { passwords++; return "fixture-password", nil },
			}
			err := a.authenticate(context.Background(), client, qr, accepted, func(ctx context.Context, value string) error {
				renders++
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > 3*time.Minute || !strings.HasPrefix(value, "tg://login?token=") {
					t.Fatal("QR renderer missing bounded context or native login URL")
				}
				if fixture.showErr {
					return rendererError
				}
				// Acceptance before renderer returns must not be lost or block.
				return dispatcher.Handle(ctx, &tg.UpdateShort{Update: &tg.UpdateLoginToken{}})
			})
			switch {
			case fixture.authorized:
				if err != nil || exports != 0 || renders != 0 {
					t.Fatal("existing authorized session requested QR login")
				}
			case fixture.showErr:
				if !errors.Is(err, rendererError) || exports != 1 {
					t.Fatal("renderer failure did not stop QR requests")
				}
			case fixture.password:
				if TelegramSetupErrorCode(err) != "password_invalid" || passwords != 1 || passwordChecks != 1 || exports != 2 {
					t.Fatal("QR login skipped native two-step password flow")
				}
			default:
				if err != nil || renders != 1 || exports != 2 {
					t.Fatal("native QR login failed after buffered acceptance")
				}
				if fixture.migrate && (migrations != 1 || imports != 1) {
					t.Fatal("native QR DC migration skipped")
				}
			}
		})
	}
}

func TestTelegramQRCodeCancellationAndNoPhoneRequirement(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	exports := 0
	api := tg.NewClient(telegramTestInvoker(func(_ context.Context, input bin.Encoder, output bin.Decoder) error {
		switch input.(type) {
		case *tg.UsersGetUsersRequest:
			return tgerr.New(401, "AUTH_KEY_UNREGISTERED")
		case *tg.AuthExportLoginTokenRequest:
			exports++
			output.(*tg.AuthLoginTokenBox).LoginToken = &tg.AuthLoginToken{Token: []byte("fixture-token"), Expires: int(time.Now().Add(time.Minute).Unix())}
		default:
			t.Fatalf("canceled QR performed unexpected RPC: %T", input)
		}
		return nil
	}))
	client := auth.NewClient(api, rand.Reader, 1, strings.Repeat("0", 32))
	qr := qrlogin.NewQR(api, 1, strings.Repeat("0", 32), qrlogin.Options{})
	err := (terminalAuth{}).authenticate(ctx, client, qr, make(chan struct{}), func(context.Context, string) error { return nil })
	if !errors.Is(err, context.DeadlineExceeded) || exports != 1 {
		t.Fatal("QR cancellation did not stop native login")
	}
	canceled, stop := context.WithCancel(context.Background())
	stop()
	cfg := TelegramConfig{AppID: 1, AppHash: strings.Repeat("0", 32), SessionFile: filepath.Join(t.TempDir(), "telegram.session"), ChannelLink: "https://t.me/+private_fixture", QRCode: func(context.Context, string) error { t.Fatal("canceled QR rendered token"); return nil }}
	password := func(context.Context) (string, error) { t.Fatal("canceled QR requested password"); return "", nil }
	if _, err := SetupTelegram(canceled, cfg, "", nil, password); !errors.Is(err, context.Canceled) {
		t.Fatal("QR setup rejected missing phone/code or ignored cancellation")
	}
	if _, err := DiscoverTelegramChannels(canceled, cfg, "", nil, password); !errors.Is(err, context.Canceled) {
		t.Fatal("QR discovery rejected missing phone/code or ignored cancellation")
	}
}

func TestTelegramFloodWaitCancellationAndTerminalErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	attempts := 0
	_, err := retryTelegram(ctx, func() (int, error) {
		attempts++
		return 0, tgerr.New(420, "FLOOD_WAIT_60")
	})
	if !errors.Is(err, context.DeadlineExceeded) || attempts != 1 {
		t.Fatalf("flood wait ignored context: %d %v", attempts, err)
	}
	attempts = 0
	want := tgerr.New(400, "CHANNEL_PRIVATE")
	_, err = retryTelegram(context.Background(), func() (int, error) {
		attempts++
		return 0, want
	})
	if !errors.Is(err, want) || attempts != 1 {
		t.Fatalf("terminal RPC retried: %d %v", attempts, err)
	}
	attempts = 0
	value, err := retryTelegram(context.Background(), func() (int, error) {
		attempts++
		if attempts == 1 {
			return 0, tgerr.New(500, "INTERNAL")
		}
		return 42, nil
	})
	if err != nil || value != 42 || attempts != 2 {
		t.Fatalf("transient retry: %d %d %v", attempts, value, err)
	}
}

func TestTelegramRetryBoundAndStatusLifecycle(t *testing.T) {
	store := &TelegramStore{ctx: context.Background()}
	store.connectionState(telegram.ConnectionStateConnecting)
	if got := store.Status(); got.State != "connecting" || got.LastCheckedAt == nil || got.RetryCount != 0 {
		t.Fatalf("initial connection status: %+v", got)
	}
	store.statusMu.Lock()
	store.verified = true
	store.statusMu.Unlock()
	store.connectionState(telegram.ConnectionStateReady)
	ctx, finish, onRetry := store.operationContext(context.Background())
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > 2*time.Minute || time.Until(deadline) < 119*time.Second {
		t.Fatalf("operation deadline not bounded: %v %v", deadline, ok)
	}
	attempts := 0
	_, err := retryTelegram(ctx, func() (int, error) {
		attempts++
		return 0, tgerr.New(420, "FLOOD_WAIT_0")
	}, onRetry)
	if err == nil || attempts != 6 {
		t.Fatalf("unbounded retry attempts: %d %v", attempts, err)
	}
	if got := store.Status(); got.State != "retrying" || got.RetryCount != 5 {
		t.Fatalf("retry status: %+v", got)
	}
	finish()
	if got := store.Status(); got.State != "connected" || got.RetryCount != 5 || store.activeRetries != 0 {
		t.Fatalf("completed retry status: %+v", got)
	}
	store.connectionState(telegram.ConnectionStateDisconnected)
	if got := store.Status(); got.State != "disconnected" {
		t.Fatalf("connection loss status: %+v", got)
	}
	store.connectionState(telegram.ConnectionStateConnecting)
	if got := store.Status(); got.State != "retrying" || got.RetryCount != 6 {
		t.Fatalf("reconnection status: %+v", got)
	}
	store.connectionState(telegram.ConnectionStateReady)
	if got := store.Status(); got.State != "connected" || got.RetryCount != 6 {
		t.Fatalf("reconnected status: %+v", got)
	}
	copy := store.Status()
	*copy.LastCheckedAt = time.Time{}
	if store.Status().LastCheckedAt.IsZero() {
		t.Fatal("status exposed mutable timestamp")
	}
	store.statusMu.Lock()
	store.closed = true
	store.refreshStatusLocked()
	store.statusMu.Unlock()
	store.connectionState(telegram.ConnectionStateReady)
	if got := store.Status(); got.State != "disconnected" {
		t.Fatalf("closed status overwritten: %+v", got)
	}
}

func TestTelegramStatusConcurrentOperationsAndCancellation(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	store := &TelegramStore{ctx: parent, verified: true, transport: telegram.ConnectionStateReady}
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			ctx, finish, retry := store.operationContext(context.Background())
			defer finish()
			for range 25 {
				retry()
				if got := store.Status(); got.State != "retrying" || got.LastCheckedAt == nil {
					t.Errorf("concurrent retry status: %+v", got)
				}
			}
			<-ctx.Done()
		})
	}
	cancel()
	workers.Wait()
	if got := store.Status(); got.RetryCount != 200 || store.activeRetries != 0 {
		t.Fatalf("lost concurrent retry state: %+v active=%d", got, store.activeRetries)
	}
}

func TestTelegramStartupClosedBeforeReadyNeverReturnsNilStoreWithoutError(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		for range 100 {
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			close(done)
			if canceled {
				cancel()
			}
			store := &TelegramStore{ctx: ctx, cancel: cancel, done: done}
			got, err := store.awaitReady(make(chan error))
			cancel()
			if got != nil || err == nil {
				t.Fatalf("closed constructor returned (%v, %v); canceled=%v", got, err, canceled)
			}
		}
	}
}

func TestTelegramResolutionCancellationAndEmptySuccessRejected(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, resolved := range []bool{false, true} {
		if err := telegramResolutionError(ctx, nil, resolved); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled setup/discovery accepted: resolved=%v %v", resolved, err)
		}
	}
	if err := telegramResolutionError(context.Background(), nil, false); err == nil {
		t.Fatal("empty setup/discovery result reported success")
	}
	if err := telegramResolutionError(context.Background(), nil, true); err != nil {
		t.Fatalf("successful resolution rejected: %v", err)
	}
	if err := telegramResolutionError(context.Background(), domain.ErrInvalid, true); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("failed resolution lost error: %v", err)
	}
}

func TestTelegramSessionPermissionsAndAtomicReplace(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "telegram.session")
	s := &privateSession{path: path}
	if _, err := s.LoadSession(ctx); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("missing session: %v", err)
	}
	for _, data := range [][]byte{[]byte("first fixture"), []byte("replacement fixture")} {
		if err := s.StoreSession(ctx, data); err != nil {
			t.Fatal(err)
		}
		got, err := s.LoadSession(ctx)
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("session roundtrip: %q %v", got, err)
		}
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("session permission: %v %v", info, err)
		}
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadSession(ctx); err == nil {
		t.Fatal("world-readable session accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(filepath.Dir(path), "target"), path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadSession(ctx); err == nil {
		t.Fatal("symlink session accepted")
	}
}

func TestTelegramPrivateChannelAndReferenceValidation(t *testing.T) {
	valid := tg.Channel{ID: 12, AccessHash: 33, Title: "Private fixture", Creator: true, Broadcast: true}
	if channel, err := privateChannel(&valid); err != nil || channel.ID != 12 || channel.Title != valid.Title {
		t.Fatalf("private owned channel rejected: %+v %v", channel, err)
	}
	for _, change := range []func(*tg.Channel){func(c *tg.Channel) { c.Creator = false }, func(c *tg.Channel) { c.Left = true }, func(c *tg.Channel) { c.Username = "public" }, func(c *tg.Channel) { c.HasLink = true }, func(c *tg.Channel) { c.Broadcast = false }} {
		channel := valid
		change(&channel)
		if _, err := privateChannel(&channel); err == nil {
			t.Fatalf("unsafe channel accepted: %+v", channel)
		}
	}
	s := &TelegramStore{channel: &tg.InputChannel{ChannelID: 12}}
	for _, ref := range []string{"tg:13:2", "tg:12:-1", "tg:12:2147483648", "tg:12:1:2", "file:12:2"} {
		if _, err := s.messageID(ref); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("unsafe reference %q: %v", ref, err)
		}
	}
	if id, err := s.messageID("tg:12:9"); err != nil || id != 9 {
		t.Fatalf("valid reference: %d %v", id, err)
	}
	updates := &tg.Updates{Updates: []tg.UpdateClass{&tg.UpdateMessageID{ID: 91, RandomID: 5}}}
	if id := uploadedMessageID(updates, 5, 12, "fixture.harbor"); id != 91 {
		t.Fatalf("message ref extraction: %d", id)
	}
	if id := uploadedMessageID(updates, 7, 12, "fixture.harbor"); id != 0 {
		t.Fatalf("wrong random ID matched: %d", id)
	}
	newMessage := &tg.UpdateNewChannelMessage{Message: &tg.Message{ID: 77, PeerID: &tg.PeerChannel{ChannelID: 12}, Media: &tg.MessageMediaDocument{Document: &tg.Document{MimeType: "application/octet-stream", Attributes: []tg.DocumentAttributeClass{&tg.DocumentAttributeFilename{FileName: "fixture.harbor"}}}}}}
	updates.Updates = append([]tg.UpdateClass{newMessage}, updates.Updates...)
	if id := uploadedMessageID(updates, 5, 12, "fixture.harbor"); id != 91 {
		t.Fatalf("unrelated update overrode authoritative ID: %d", id)
	}
	updates.Updates = updates.Updates[:1]
	if id := uploadedMessageID(updates, 5, 13, "fixture.harbor"); id != 0 {
		t.Fatalf("foreign channel matched fallback: %d", id)
	}
	if id := uploadedMessageID(updates, 5, 12, "other.harbor"); id != 0 {
		t.Fatalf("foreign document matched fallback: %d", id)
	}
	if id := uploadedMessageID(updates, 5, 12, "fixture.harbor"); id != 77 {
		t.Fatalf("validated document fallback: %d", id)
	}
	buffer := &ciphertextBuffer{remaining: 3}
	if n, err := buffer.Write([]byte("oversized")); n != 0 || !errors.Is(err, domain.ErrIntegrity) || buffer.Len() != 0 {
		t.Fatalf("unbounded ciphertext buffer: %d %v", n, err)
	}
}

func TestTelegramRejectsExpiringStorageChannel(t *testing.T) {
	for _, full := range []tg.ChatFullClass{
		&tg.ChannelFull{ID: 12, TTLPeriod: 86400},
		&tg.ChannelFull{ID: 12, TTLPeriod: -1},
		&tg.ChannelFull{ID: 13},
		&tg.ChatFull{ID: 12},
	} {
		if err := persistentChannel(full, 12); err == nil {
			t.Fatalf("unsafe retention accepted: %+v", full)
		}
	}
	if err := persistentChannel(&tg.ChannelFull{ID: 12}, 12); err != nil {
		t.Fatalf("non-expiring channel rejected: %v", err)
	}
}

type telegramTestInvoker func(context.Context, bin.Encoder, bin.Decoder) error

func (f telegramTestInvoker) Invoke(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
	return f(ctx, input, output)
}

type telegramTestDownloadConnection struct {
	telegramTestInvoker
	closed bool
}

func (c *telegramTestDownloadConnection) Close() error { c.closed = true; return nil }

func TestTelegramDownloadDocumentRoutesAndBounds(t *testing.T) {
	for _, fixture := range []struct {
		name       string
		dc         int
		payload    int
		sizeChange int
	}{
		{"same_dc_tiny", 2, 83, 0},
		{"same_dc_multiple_parts", 2, (512 << 10) + 17, 0},
		{"same_dc_truncated", 2, 83, 1},
		{"same_dc_oversized", 2, 83, -1},
		{"other_dc_tiny", 4, 83, 0},
		{"other_dc_truncated", 4, 83, 1},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			payload := bytes.Repeat([]byte("x"), fixture.payload)
			document := &tg.Document{ID: 7, AccessHash: 8, FileReference: []byte("fixture"), Size: int64(len(payload) + fixture.sizeChange), DCID: fixture.dc}
			requests := 0
			read := telegramTestInvoker(func(_ context.Context, input bin.Encoder, output bin.Decoder) error {
				request, ok := input.(*tg.UploadGetFileRequest)
				if !ok {
					t.Errorf("unexpected RPC, including forbidden same-DC auth export: %T", input)
					return errors.New("unexpected RPC")
				}
				location, ok := request.Location.(*tg.InputDocumentFileLocation)
				if !ok || location.ID != document.ID || location.AccessHash != document.AccessHash || !bytes.Equal(location.FileReference, document.FileReference) || location.ThumbSize != "" {
					t.Error("document location changed")
					return domain.ErrInvalid
				}
				if request.Offset != int64(requests*(512<<10)) || request.Limit != 512<<10 {
					t.Error("download offset or limit changed")
					return domain.ErrInvalid
				}
				requests++
				end := min(int(request.Offset)+request.Limit, len(payload))
				output.(*tg.UploadFileBox).File = &tg.UploadFile{Type: &tg.StorageFileUnknown{}, Bytes: payload[int(request.Offset):end]}
				return nil
			})
			primary := tg.NewClient(read)
			connection := &telegramTestDownloadConnection{telegramTestInvoker: read}
			mediaCalls := 0
			mediaOnly := func(_ context.Context, dc int, max int64) (telegram.CloseInvoker, error) {
				mediaCalls++
				if fixture.dc == 2 {
					t.Error("same-DC download attempted authorization export")
					return nil, tgerr.New(400, "DC_ID_INVALID")
				}
				if dc != fixture.dc || max != 1 {
					t.Error("cross-DC connection parameters changed")
					return nil, domain.ErrInvalid
				}
				return connection, nil
			}
			got, err := downloadTelegramDocument(context.Background(), document, primary, 2, mediaOnly, func() { t.Error("unexpected retry") })
			if fixture.sizeChange != 0 {
				if !errors.Is(err, domain.ErrIntegrity) || got != nil {
					t.Fatalf("invalid document length accepted: %d, %v", len(got), err)
				}
			} else if err != nil || !bytes.Equal(got, payload) {
				t.Fatalf("ciphertext roundtrip failed: %d, %v", len(got), err)
			}
			if requests != (len(payload)+(512<<10)-1)/(512<<10) {
				t.Fatalf("unexpected file request count: %d", requests)
			}
			wantMedia := fixture.dc != 2
			if (mediaCalls == 1) != wantMedia || connection.closed != wantMedia {
				t.Fatalf("incorrect connection lifecycle: calls=%d closed=%t", mediaCalls, connection.closed)
			}
		})
	}
}

func discoveryPage(final bool, channels ...*tg.Channel) tg.MessagesDialogsClass {
	var list []tg.DialogClass
	var messages []tg.MessageClass
	var chats []tg.ChatClass
	for i, channel := range channels {
		peer := &tg.PeerChannel{ChannelID: channel.ID}
		list = append(list, &tg.Dialog{Peer: peer, TopMessage: 1000 + i})
		messages = append(messages, &tg.Message{ID: 1000 + i, Date: 2000 + i, PeerID: peer})
		chats = append(chats, channel)
	}
	if final {
		return &tg.MessagesDialogs{Dialogs: list, Messages: messages, Chats: chats}
	}
	return &tg.MessagesDialogsSlice{Count: len(list) + 1, Dialogs: list, Messages: messages, Chats: chats}
}

func TestDiscoverTelegramChannelsPaginatesArchivesAndFilters(t *testing.T) {
	zeta := &tg.Channel{ID: 42, AccessHash: 142, Title: "Zeta", Creator: true, Broadcast: true}
	expiring := &tg.Channel{ID: 5, AccessHash: 105, Title: "Expiring", Creator: true, Broadcast: true}
	alpha := &tg.Channel{ID: 7, AccessHash: 107, Title: "alpha", Creator: true, Broadcast: true}
	archived := &tg.Channel{ID: 52, AccessHash: 152, Title: "ALPHA", Creator: true, Broadcast: true}
	public := &tg.Channel{ID: 13, AccessHash: 113, Creator: true, Broadcast: true, Username: "public"}
	notOwned := &tg.Channel{ID: 17, AccessHash: 117, Broadcast: true}
	supergroup := &tg.Channel{ID: 99, AccessHash: 199, Creator: true, Megagroup: true}
	pages := map[int]int{}
	inspected := map[int64]int{}
	api := tg.NewClient(telegramTestInvoker(func(_ context.Context, input bin.Encoder, output bin.Decoder) error {
		switch request := input.(type) {
		case *tg.MessagesGetDialogsRequest:
			folder, ok := request.GetFolderID()
			if !ok || (folder != 0 && folder != 1) || request.Limit != 100 {
				t.Fatalf("unbounded or missing folder request: %+v", request)
			}
			page := pages[folder]
			pages[folder]++
			var response tg.MessagesDialogsClass
			switch {
			case folder == 0 && page == 0:
				if request.ExcludePinned || request.OffsetID != 0 || request.OffsetDate != 0 {
					t.Fatalf("first page skipped pinned channels: %+v", request)
				}
				response = discoveryPage(false, zeta, expiring)
			case folder == 0 && page == 1:
				peer, ok := request.OffsetPeer.(*tg.InputPeerChannel)
				if !request.ExcludePinned || request.OffsetID != 1001 || request.OffsetDate != 2001 || !ok || peer.ChannelID != 5 || peer.AccessHash != 105 {
					t.Fatalf("pagination lost cursor or repeated pinned channels: %+v", request)
				}
				response = discoveryPage(true, zeta, alpha, public, notOwned)
			case folder == 1 && page == 0:
				if request.ExcludePinned || request.OffsetID != 0 || request.OffsetDate != 0 {
					t.Fatalf("archive did not reset pagination: %+v", request)
				}
				response = discoveryPage(true, archived, zeta, supergroup)
			default:
				t.Fatalf("unnecessary or repeated page: folder=%d page=%d", folder, page)
			}
			output.(*tg.MessagesDialogsBox).Dialogs = response
		case *tg.ChannelsGetFullChannelRequest:
			channel := request.Channel.(*tg.InputChannel)
			inspected[channel.ChannelID]++
			full := &tg.ChannelFull{ID: channel.ChannelID}
			if channel.ChannelID == expiring.ID {
				full.TTLPeriod = 86400
			}
			output.(*tg.MessagesChatFull).FullChat = full
		default:
			t.Fatalf("discovery performed unexpected RPC: %T", input)
		}
		return nil
	}))
	channels, err := discoverTelegramChannels(context.Background(), api)
	if err != nil || len(channels) != 3 {
		t.Fatalf("discovery: %+v %v", channels, err)
	}
	for i, want := range []*tg.Channel{alpha, archived, zeta} {
		if channels[i].ID != want.ID || channels[i].AccessHash != want.AccessHash || channels[i].Title != want.Title {
			t.Fatalf("channel details/sort at %d: %+v", i, channels[i])
		}
	}
	if pages[0] != 2 || pages[1] != 1 || len(inspected) != 4 {
		t.Fatalf("pagination/filter counts: pages=%v inspected=%v", pages, inspected)
	}
	for _, count := range inspected {
		if count != 1 {
			t.Fatalf("duplicate channel inspected twice: %v", inspected)
		}
	}
}

func TestDiscoverTelegramChannelsNoEligibleAndCancellation(t *testing.T) {
	calls := 0
	api := tg.NewClient(telegramTestInvoker(func(_ context.Context, input bin.Encoder, output bin.Decoder) error {
		calls++
		if _, ok := input.(*tg.MessagesGetDialogsRequest); !ok {
			t.Fatalf("unexpected RPC: %T", input)
		}
		output.(*tg.MessagesDialogsBox).Dialogs = &tg.MessagesDialogs{}
		return nil
	}))
	if _, err := discoverTelegramChannels(context.Background(), api); err == nil || !strings.Contains(err.Error(), "no eligible channels") {
		t.Fatalf("missing eligibility guidance: %v", err)
	}
	if calls != 2 {
		t.Fatalf("empty discovery did not scan both folders: %d", calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := discoverTelegramChannels(ctx, api); !errors.Is(err, context.Canceled) || calls != 2 {
		t.Fatalf("canceled discovery reached RPC: %d %v", calls, err)
	}
}

func TestTelegramChannelLinkParsingAndPreflight(t *testing.T) {
	for _, input := range []string{"https://t.me/+fixture_123-A", "https://t.me/joinchat/fixture_123-A", "t.me/+fixture_123-A", "t.me/joinchat/fixture_123-A", "https://T.ME/+fixture_123-A"} {
		invite, id, err := parseTelegramChannelLink(input)
		if err != nil || invite != "fixture_123-A" || id != 0 {
			t.Fatalf("invite parsing: %q %q %d %v", input, invite, id, err)
		}
	}
	for _, input := range []string{"https://t.me/c/12/34", "t.me/c/12/34"} {
		invite, id, err := parseTelegramChannelLink(input)
		if err != nil || invite != "" || id != 12 {
			t.Fatalf("message parsing: %q %q %d %v", input, invite, id, err)
		}
	}
	for _, input := range []string{
		"", " ", "@public", "public", "https://t.me/public", "https://t.me/public/34",
		"http://t.me/+private_fixture", "tg://join?invite=private_fixture", "https://example.com/+private_fixture", "https://t.me.evil/+private_fixture",
		"https://user:password@t.me/+private_fixture", "https://t.me:443/+private_fixture", "https://t.me/+private_fixture?x=1", "https://t.me/+private_fixture?", "https://t.me/+private_fixture#",
		"https://t.me/+private_fixture/extra", "https://t.me/+private_fixture/", "https://t.me/joinchat/private_fixture/extra", "https://t.me/+private%5ffixture", "https://t.me/+private.fixture",
		"https://t.me/+", "https://t.me/joinchat/", "https://t.me//+private_fixture", "https://t.me/+private_你好", "https://t.me/+" + strings.Repeat("x", 2048),
		"https://t.me/c/0/34", "https://t.me/c/-10012/34", "https://t.me/c/+12/34", "https://t.me/c/012/34", "https://t.me/c/9223372036854775808/34",
		"https://t.me/c/12/0", "https://t.me/c/12/-1", "https://t.me/c/12/2147483648", "https://t.me/c/12/34/extra", "https://t.me/c/12", "https://t.me/c/12/34?single",
	} {
		if _, _, err := parseTelegramChannelLink(input); err == nil || strings.Contains(err.Error(), "private_fixture") {
			t.Fatalf("unsafe link accepted or echoed: %q %v", input, err)
		}
	}
	for _, cfg := range []TelegramConfig{
		{ChannelLink: "https://t.me/+private_fixture", ChannelID: -12},
		{ChannelLink: "https://t.me/c/13/34", ChannelID: 12},
		{ChannelLink: "https://t.me/+private_fixture", ChannelAccessHash: 33},
		{ChannelLink: "https://t.me/+private_fixture?secret"},
		{ChannelLink: "https://t.me/public"},
		{ChannelID: 12}, {ChannelAccessHash: 33}, {ChannelID: -12, ChannelAccessHash: 33},
	} {
		if _, err := NewTelegram(context.Background(), cfg); err == nil || strings.Contains(err.Error(), "session unavailable") || strings.Contains(err.Error(), "private_fixture") {
			t.Fatalf("startup did not validate selector before session: %v", err)
		}
		if _, err := SetupTelegram(context.Background(), cfg, "", nil, nil); err == nil || errors.Is(err, domain.ErrInvalid) || strings.Contains(err.Error(), "private_fixture") {
			t.Fatalf("setup did not validate selector before auth/create: %v", err)
		}
	}
	if _, _, err := telegramChannelSelector(TelegramConfig{}, true); err != nil {
		t.Fatalf("empty setup must permit explicit creation flow: %v", err)
	}
	if _, _, err := telegramChannelSelector(TelegramConfig{}, false); err == nil {
		t.Fatal("startup allowed missing channel")
	}
	if _, id, err := telegramChannelSelector(TelegramConfig{ChannelID: 12, ChannelAccessHash: -33}, false); err != nil || id != 12 {
		t.Fatalf("legacy signed access hash rejected: %d %v", id, err)
	}
	for _, link := range []string{"https://t.me/+private_fixture", "https://t.me/c/12/34"} {
		if _, _, err := telegramChannelSelector(TelegramConfig{ChannelLink: link, ChannelID: 12}, false); err != nil {
			t.Fatalf("link with expected ID rejected: %v", err)
		}
	}
}

func TestTelegramPrivateInviteResolutionWithoutMutation(t *testing.T) {
	channel := &tg.Channel{ID: 12, AccessHash: 33, Title: "Private fixture", Creator: true, Broadcast: true}
	for _, link := range []string{"https://t.me/+private_fixture", "https://t.me/joinchat/private_fixture"} {
		checks, fullChecks := 0, 0
		api := tg.NewClient(telegramTestInvoker(func(_ context.Context, input bin.Encoder, output bin.Decoder) error {
			switch request := input.(type) {
			case *tg.MessagesCheckChatInviteRequest:
				checks++
				if request.Hash != "private_fixture" {
					t.Fatal("incorrect invite hash")
				}
				output.(*tg.ChatInviteBox).ChatInvite = &tg.ChatInviteAlready{Chat: channel}
			case *tg.ChannelsGetFullChannelRequest:
				fullChecks++
				peer, ok := request.Channel.(*tg.InputChannel)
				if !ok || peer.ChannelID != channel.ID || peer.AccessHash != channel.AccessHash {
					t.Fatal("retention check used incorrect channel")
				}
				output.(*tg.MessagesChatFull).FullChat = &tg.ChannelFull{ID: channel.ID}
			default:
				t.Fatalf("link resolution performed unexpected or mutating RPC: %T", input)
			}
			return nil
		}))
		resolved, err := configuredChannel(context.Background(), api, TelegramConfig{ChannelLink: link})
		if err != nil || resolved.ID != channel.ID || resolved.AccessHash != channel.AccessHash || resolved.Title != channel.Title || checks != 1 || fullChecks != 1 {
			t.Fatalf("private invite: %+v checks=%d full=%d %v", resolved, checks, fullChecks, err)
		}
		store := &TelegramStore{channel: &tg.InputChannel{ChannelID: resolved.ID, AccessHash: resolved.AccessHash}}
		if store.ChannelID() != channel.ID {
			t.Fatal("resolved channel ID not exposed for immutable binding")
		}
	}
}

func TestTelegramPrivateInviteExpectedIDFailsClosed(t *testing.T) {
	for _, expected := range []int64{12, 13} {
		fullChecks := 0
		api := tg.NewClient(telegramTestInvoker(func(_ context.Context, input bin.Encoder, output bin.Decoder) error {
			switch input.(type) {
			case *tg.MessagesCheckChatInviteRequest:
				output.(*tg.ChatInviteBox).ChatInvite = &tg.ChatInviteAlready{Chat: &tg.Channel{ID: 12, AccessHash: 33, Creator: true, Broadcast: true}}
			case *tg.ChannelsGetFullChannelRequest:
				fullChecks++
				output.(*tg.MessagesChatFull).FullChat = &tg.ChannelFull{ID: 12}
			default:
				t.Fatalf("expected ID validation performed unexpected or mutating RPC: %T", input)
			}
			return nil
		}))
		result, err := configuredChannel(context.Background(), api, TelegramConfig{ChannelLink: "https://t.me/+private_fixture", ChannelID: expected})
		if expected == 12 {
			if err != nil || result.ID != expected || fullChecks != 1 {
				t.Fatalf("matching expected ID rejected: %+v full=%d %v", result, fullChecks, err)
			}
		} else if err == nil || fullChecks != 0 || strings.Contains(err.Error(), "private_fixture") {
			t.Fatalf("mismatch did not fail closed: full=%d %v", fullChecks, err)
		}
	}
}

func TestTelegramPrivateInviteRejectsUnsafeChannelsAndRedactsErrors(t *testing.T) {
	valid := tg.Channel{ID: 12, AccessHash: 33, Creator: true, Broadcast: true}
	for _, fixture := range []struct {
		name     string
		response tg.ChatInviteClass
		change   func(*tg.Channel)
		full     tg.ChatFullClass
		checkErr error
		fullErr  error
		wantFull int
	}{
		{name: "not joined", response: &tg.ChatInvite{}},
		{name: "peek only", response: &tg.ChatInvitePeek{Chat: &valid}},
		{name: "group", response: &tg.ChatInviteAlready{Chat: &tg.Chat{ID: 12}}},
		{name: "not owner", change: func(c *tg.Channel) { c.Creator = false }},
		{name: "left", change: func(c *tg.Channel) { c.Left = true }},
		{name: "public", change: func(c *tg.Channel) { c.Username = "public" }},
		{name: "public aliases", change: func(c *tg.Channel) { c.Usernames = []tg.Username{{Username: "public"}} }},
		{name: "linked discussion", change: func(c *tg.Channel) { c.HasLink = true }},
		{name: "not broadcast", change: func(c *tg.Channel) { c.Broadcast = false }},
		{name: "restricted", change: func(c *tg.Channel) { c.Restricted = true }},
		{name: "missing access hash", change: func(c *tg.Channel) { c.AccessHash = 0 }},
		{name: "invalid ID", change: func(c *tg.Channel) { c.ID = 0 }},
		{name: "auto-delete", full: &tg.ChannelFull{ID: 12, TTLPeriod: 86400}, wantFull: 1},
		{name: "wrong retention ID", full: &tg.ChannelFull{ID: 13}, wantFull: 1},
		{name: "invite RPC error", checkErr: tgerr.New(400, "PRIVATE_FIXTURE_private_fixture")},
		{name: "retention RPC error", fullErr: tgerr.New(400, "PRIVATE_FIXTURE_private_fixture"), wantFull: 1},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			channel := valid
			if fixture.change != nil {
				fixture.change(&channel)
			}
			response := fixture.response
			if response == nil {
				response = &tg.ChatInviteAlready{Chat: &channel}
			}
			fullChecks := 0
			api := tg.NewClient(telegramTestInvoker(func(_ context.Context, input bin.Encoder, output bin.Decoder) error {
				switch input.(type) {
				case *tg.MessagesCheckChatInviteRequest:
					output.(*tg.ChatInviteBox).ChatInvite = response
					return fixture.checkErr
				case *tg.ChannelsGetFullChannelRequest:
					fullChecks++
					output.(*tg.MessagesChatFull).FullChat = fixture.full
					return fixture.fullErr
				default:
					t.Fatalf("unsafe selector performed unexpected or mutating RPC: %T", input)
				}
				return nil
			}))
			if _, err := configuredChannel(context.Background(), api, TelegramConfig{ChannelLink: "https://t.me/+private_fixture"}); err == nil || strings.Contains(err.Error(), "private_fixture") || fullChecks != fixture.wantFull {
				t.Fatalf("unsafe invite accepted, token echoed or wrong retention count: full=%d %v", fullChecks, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	api := tg.NewClient(telegramTestInvoker(func(context.Context, bin.Encoder, bin.Decoder) error {
		t.Fatal("canceled resolution reached RPC")
		return nil
	}))
	if _, err := configuredChannel(ctx, api, TelegramConfig{ChannelLink: "https://t.me/+private_fixture"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("invite cancellation lost: %v", err)
	}
}

func TestTelegramMessageLinkResolvesExistingOwnedChannel(t *testing.T) {
	for _, fixture := range []struct {
		name string
		edit func(*tg.Channel, *tg.ChannelFull)
		ok   bool
	}{
		{name: "archived owned channel", ok: true},
		{name: "not owner", edit: func(c *tg.Channel, _ *tg.ChannelFull) { c.Creator = false }},
		{name: "public channel", edit: func(c *tg.Channel, _ *tg.ChannelFull) { c.Username = "public" }},
		{name: "auto-delete", edit: func(_ *tg.Channel, c *tg.ChannelFull) { c.TTLPeriod = 86400 }},
		{name: "absent channel", edit: func(c *tg.Channel, full *tg.ChannelFull) { c.ID = 13; full.ID = 13 }},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			channel := tg.Channel{ID: 12, AccessHash: 33, Title: "Archived", Creator: true, Broadcast: true}
			full := tg.ChannelFull{ID: 12}
			if fixture.edit != nil {
				fixture.edit(&channel, &full)
			}
			pages := 0
			api := tg.NewClient(telegramTestInvoker(func(_ context.Context, input bin.Encoder, output bin.Decoder) error {
				switch request := input.(type) {
				case *tg.MessagesGetDialogsRequest:
					pages++
					folder, _ := request.GetFolderID()
					if folder == 1 {
						output.(*tg.MessagesDialogsBox).Dialogs = discoveryPage(true, &channel)
					} else {
						output.(*tg.MessagesDialogsBox).Dialogs = &tg.MessagesDialogs{}
					}
				case *tg.ChannelsGetFullChannelRequest:
					output.(*tg.MessagesChatFull).FullChat = &full
				default:
					t.Fatalf("message link performed unexpected or mutating RPC: %T", input)
				}
				return nil
			}))
			result, err := configuredChannel(context.Background(), api, TelegramConfig{ChannelLink: "https://t.me/c/12/34"})
			if (err == nil) != fixture.ok || pages != 2 {
				t.Fatalf("message link eligibility: %+v pages=%d %v", result, pages, err)
			}
			if fixture.ok && (result.ID != 12 || result.AccessHash != 33 || result.Title != "Archived") {
				t.Fatalf("resolved incorrect channel: %+v", result)
			}
		})
	}
}

func TestTelegramLegacyChannelAndInvalidSelectorDoNotMutate(t *testing.T) {
	channel := &tg.Channel{ID: 12, AccessHash: 33, Creator: true, Broadcast: true}
	gets := 0
	api := tg.NewClient(telegramTestInvoker(func(_ context.Context, input bin.Encoder, output bin.Decoder) error {
		switch request := input.(type) {
		case *tg.ChannelsGetChannelsRequest:
			gets++
			if len(request.ID) != 1 {
				t.Fatal("unexpected channel lookup count")
			}
			peer := request.ID[0].(*tg.InputChannel)
			if peer.ChannelID != channel.ID || peer.AccessHash != channel.AccessHash {
				t.Fatal("legacy lookup used wrong credentials")
			}
			output.(*tg.MessagesChatsBox).Chats = &tg.MessagesChats{Chats: []tg.ChatClass{channel}}
		case *tg.ChannelsGetFullChannelRequest:
			output.(*tg.MessagesChatFull).FullChat = &tg.ChannelFull{ID: channel.ID}
		default:
			t.Fatalf("legacy selector performed unexpected or mutating RPC: %T", input)
		}
		return nil
	}))
	result, err := configuredChannel(context.Background(), api, TelegramConfig{ChannelID: 12, ChannelAccessHash: 33})
	if err != nil || result.ID != 12 || gets != 1 {
		t.Fatalf("legacy resolution: %+v %v", result, err)
	}
	api = tg.NewClient(telegramTestInvoker(func(context.Context, bin.Encoder, bin.Decoder) error {
		t.Fatal("invalid selector reached RPC")
		return nil
	}))
	for _, cfg := range []TelegramConfig{{ChannelLink: "@public"}, {ChannelLink: "https://t.me/+private_fixture", ChannelID: -12}, {ChannelLink: "https://t.me/c/13/34", ChannelID: 12}, {ChannelID: 12}} {
		if _, err := configuredChannel(context.Background(), api, cfg); err == nil {
			t.Fatal("invalid selector accepted")
		}
	}
}
