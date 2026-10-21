//go:build telegram

package storage

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/auth/qrlogin"
	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"

	"private-drive/internal/domain"
)

type TelegramStore struct {
	client  *telegram.Client
	channel *tg.InputChannel
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	runErr  error

	statusMu      sync.Mutex
	status        domain.StorageStatus
	transport     telegram.ConnectionState
	verified      bool
	closed        bool
	activeRetries int
}

var _ BlobStore = (*TelegramStore)(nil)

var (
	errTelegramCredentials     = errors.New("valid Telegram app ID, app hash and session path required")
	errTelegramSession         = errors.New("Telegram session unavailable or invalid; run telegram-setup again")
	errTelegramChannelMismatch = errors.New("Telegram channel link does not match the expected channel ID")
	errTelegramChannelAccess   = errors.New("Telegram channel access check failed")
	errTelegramRetention       = errors.New("disable Telegram channel auto-delete before using it for storage")
	errTelegramConnection      = errors.New("Telegram connection closed before channel resolution completed")
	errTelegramAuthMethod      = errors.New("Telegram requested a login method unsupported by terminal authentication")
)

// TelegramSetupErrorCode returns only fixed classifications, never provider text.
func TelegramSetupErrorCode(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, errTelegramAuthMethod):
		return "auth_method_unsupported"
	case errors.Is(err, errTelegramCredentials), tgerr.Is(err, "API_ID_INVALID", "API_ID_PUBLISHED_FLOOD", "API_HASH_INVALID"):
		return "api_credentials"
	case tgerr.Is(err, "PHONE_NUMBER_INVALID", "PHONE_NUMBER_UNOCCUPIED"):
		return "phone_invalid"
	case tgerr.Is(err, "PHONE_NUMBER_BANNED", "USER_DEACTIVATED", "USER_DEACTIVATED_BAN"):
		return "phone_banned"
	case tgerr.Is(err, "PHONE_CODE_INVALID", "PHONE_CODE_EMPTY"):
		return "code_invalid"
	case tgerr.Is(err, "PHONE_CODE_EXPIRED", "PHONE_CODE_HASH_EMPTY", "PHONE_CODE_HASH_INVALID"):
		return "code_expired"
	case errors.Is(err, auth.ErrPasswordInvalid), tgerr.Is(err, "PASSWORD_HASH_INVALID"):
		return "password_invalid"
	case errors.Is(err, auth.ErrPasswordAuthNeeded), errors.Is(err, auth.ErrPasswordNotProvided), tgerr.Is(err, "SESSION_PASSWORD_NEEDED", "PASSWORD_EMPTY"):
		return "password_required"
	case tgerr.Is(err, "FLOOD_WAIT", "FLOOD_PREMIUM_WAIT", "PHONE_NUMBER_FLOOD", "PHONE_PASSWORD_FLOOD"):
		return "flood_wait"
	case errors.Is(err, errTelegramSession), errors.Is(err, session.ErrNotFound), tgerr.Is(err, "AUTH_KEY_UNREGISTERED", "AUTH_KEY_INVALID", "AUTH_KEY_DUPLICATED", "SESSION_EXPIRED", "SESSION_REVOKED"):
		return "session_invalid"
	case tgerr.Is(err, "AUTH_RESTART", "SRP_ID_INVALID"):
		return "auth_restart"
	case errors.Is(err, errTelegramChannelMismatch):
		return "channel_mismatch"
	case errors.Is(err, errTelegramRetention):
		return "channel_retention"
	case errors.Is(err, errTelegramChannelAccess), tgerr.Is(err, "CHANNEL_PRIVATE", "CHANNEL_INVALID", "CHAT_ADMIN_REQUIRED", "CHAT_WRITE_FORBIDDEN", "USER_BANNED_IN_CHANNEL", "USER_NOT_PARTICIPANT", "INVITE_HASH_INVALID", "INVITE_HASH_EXPIRED", "INVITE_HASH_EMPTY"):
		return "channel_access"
	case errors.Is(err, errTelegramConnection):
		return "connection"
	}
	var network net.Error
	if errors.As(err, &network) {
		if network.Timeout() {
			return "timeout"
		}
		return "connection"
	}
	return "unknown"
}

func telegramClient(cfg TelegramConfig, onState func(telegram.ConnectionState), updates telegram.UpdateHandler) (*telegram.Client, error) {
	appHash, err := hex.DecodeString(cfg.AppHash)
	if cfg.AppID <= 0 || err != nil || len(appHash) != 16 || cfg.SessionFile == "" {
		return nil, errTelegramCredentials
	}
	if err := os.MkdirAll(filepath.Dir(cfg.SessionFile), 0700); err != nil {
		return nil, err
	}
	return telegram.NewClient(cfg.AppID, cfg.AppHash, telegram.Options{SessionStorage: &privateSession{path: cfg.SessionFile}, NoUpdates: updates == nil, UpdateHandler: updates, OnConnectionState: onState}), nil
}

func telegramQRUpdates() (tg.UpdateDispatcher, qrlogin.LoggedIn) {
	dispatcher := tg.NewUpdateDispatcher()
	accepted := make(chan struct{}, 1)
	dispatcher.OnLoginToken(func(context.Context, tg.Entities, *tg.UpdateLoginToken) error {
		select {
		case accepted <- struct{}{}:
		default:
		}
		return nil
	})
	return dispatcher, accepted
}

func telegramSetupClient(cfg TelegramConfig) (*telegram.Client, qrlogin.LoggedIn, error) {
	var updates telegram.UpdateHandler
	var accepted qrlogin.LoggedIn
	if cfg.QRCode != nil {
		updates, accepted = telegramQRUpdates()
	}
	client, err := telegramClient(cfg, nil, updates)
	return client, accepted, err
}

// NewTelegram uses an existing authenticated session; onboarding is separate.
func NewTelegram(ctx context.Context, cfg TelegramConfig) (*TelegramStore, error) {
	if _, _, err := telegramChannelSelector(cfg, false); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(cfg.SessionFile); err != nil {
		return nil, errTelegramSession
	}
	runCtx, cancel := context.WithCancel(ctx)
	s := &TelegramStore{ctx: runCtx, cancel: cancel, done: make(chan struct{}), status: domain.StorageStatus{State: "connecting"}}
	client, err := telegramClient(cfg, s.connectionState, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	s.client = client
	ready := make(chan error, 1)
	go func() {
		defer cancel()
		s.runErr = client.Run(runCtx, func(ctx context.Context) error {
			status, err := client.Auth().Status(ctx)
			if err == nil && !status.Authorized {
				err = errTelegramSession
			}
			if err == nil {
				var channel TelegramChannel
				channel, err = configuredChannel(ctx, client.API(), cfg)
				if err == nil {
					s.channel = &tg.InputChannel{ChannelID: channel.ID, AccessHash: channel.AccessHash}
					s.statusMu.Lock()
					s.verified = true
					s.transport = telegram.ConnectionStateReady
					s.refreshStatusLocked()
					s.statusMu.Unlock()
				}
			}
			ready <- err
			if err != nil {
				return err
			}
			<-ctx.Done()
			return ctx.Err()
		})
		s.statusMu.Lock()
		s.closed = true
		s.refreshStatusLocked()
		s.statusMu.Unlock()
		close(s.done)
	}()
	return s.awaitReady(ready)
}

func (s *TelegramStore) awaitReady(ready <-chan error) (*TelegramStore, error) {
	timer := time.NewTimer(45 * time.Second)
	defer timer.Stop()
	select {
	case err := <-ready:
		if err != nil {
			s.cancel()
			return nil, err
		}
		if err := s.ctx.Err(); err != nil {
			return nil, err
		}
		return s, nil
	case <-s.done:
		s.cancel()
		if s.runErr != nil {
			return nil, s.runErr
		}
		return nil, errTelegramConnection
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	case <-timer.C:
		s.cancel()
		return nil, fmt.Errorf("Telegram startup timed out: %w", context.DeadlineExceeded)
	}
}

func (s *TelegramStore) ChannelID() int64 { return s.channel.ChannelID }

func (s *TelegramStore) Status() domain.StorageStatus {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	status := s.status
	if status.LastCheckedAt != nil {
		checked := *status.LastCheckedAt
		status.LastCheckedAt = &checked
	}
	return status
}

func (s *TelegramStore) connectionState(state telegram.ConnectionState) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	if s.closed {
		return
	}
	if state == telegram.ConnectionStateConnecting && s.verified {
		s.status.RetryCount++
	}
	s.transport = state
	s.refreshStatusLocked()
}

func (s *TelegramStore) refreshStatusLocked() {
	checked := time.Now().UTC()
	s.status.LastCheckedAt = &checked
	switch {
	case s.closed || s.transport == telegram.ConnectionStateDisconnected:
		s.status.State = "disconnected"
	case s.activeRetries > 0 || s.verified && s.transport == telegram.ConnectionStateConnecting:
		s.status.State = "retrying"
	case s.verified && s.transport == telegram.ConnectionStateReady:
		s.status.State = "connected"
	default:
		s.status.State = "connecting"
	}
}

func (s *TelegramStore) Close() error {
	s.cancel()
	<-s.done
	if errors.Is(s.runErr, context.Canceled) {
		return nil
	}
	return s.runErr
}

func (s *TelegramStore) operationContext(ctx context.Context, transferBytes int64) (context.Context, func(), func()) {
	// Allow 100 MiB chunks on slower links without unbounded retries/waits.
	timeout := min(10*time.Minute, 2*time.Minute+time.Duration(transferBytes/(256<<10))*time.Second)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	stop := context.AfterFunc(s.ctx, cancel)
	retried := false
	onRetry := func() {
		s.statusMu.Lock()
		defer s.statusMu.Unlock()
		s.status.RetryCount++
		if !retried {
			retried = true
			s.activeRetries++
		}
		s.refreshStatusLocked()
	}
	return ctx, func() {
		stop()
		cancel()
		s.statusMu.Lock()
		defer s.statusMu.Unlock()
		if retried {
			s.activeRetries--
			s.refreshStatusLocked()
		}
	}, onRetry
}

func (s *TelegramStore) Put(ctx context.Context, key string, ciphertext []byte) (string, error) {
	if !isHexID(key) || len(ciphertext) == 0 || int64(len(ciphertext)) > maxCiphertextSize {
		return "", domain.ErrInvalid
	}
	ctx, cancel, onRetry := s.operationContext(ctx, int64(len(ciphertext)))
	defer cancel()
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	randomID := int64(binary.LittleEndian.Uint64(random[:]))
	// Keep one random ID across retries: Telegram deduplicates sendMedia.
	return retryTelegram(ctx, func() (string, error) {
		uploadAPI := tg.NewClient(telegram.InvokeFunc(func(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
			err := s.client.Invoke(ctx, input, output)
			if _, flood := tgerr.AsFloodWait(err); flood {
				onRetry()
			}
			return err
		}))
		file, err := uploader.NewUploader(uploadAPI).WithThreads(2).WithPartSize(512<<10).FromBytes(ctx, key+".harbor", ciphertext)
		if err != nil {
			return "", err
		}
		updates, err := s.client.API().MessagesSendMedia(ctx, &tg.MessagesSendMediaRequest{
			Peer: &tg.InputPeerChannel{ChannelID: s.channel.ChannelID, AccessHash: s.channel.AccessHash},
			Media: &tg.InputMediaUploadedDocument{File: file, MimeType: "application/octet-stream", ForceFile: true,
				Attributes: []tg.DocumentAttributeClass{&tg.DocumentAttributeFilename{FileName: key + ".harbor"}}},
			RandomID: randomID, Silent: true,
		})
		if err != nil {
			return "", err
		}
		messageID := uploadedMessageID(updates, randomID, s.channel.ChannelID, key+".harbor")
		if messageID <= 0 {
			return "", errors.New("Telegram upload returned no persistent message reference")
		}
		return fmt.Sprintf("tg:%d:%d", s.channel.ChannelID, messageID), nil
	}, onRetry)
}

func uploadedMessageID(updates tg.UpdatesClass, randomID, channelID int64, fileName string) int {
	var list []tg.UpdateClass
	switch u := updates.(type) {
	case *tg.Updates:
		list = u.Updates
	case *tg.UpdatesCombined:
		list = u.Updates
	case *tg.UpdateShort:
		list = []tg.UpdateClass{u.Update}
	}
	for _, update := range list {
		if u, ok := update.(*tg.UpdateMessageID); ok && u.RandomID == randomID {
			return u.ID
		}
	}
	candidate := 0
	for _, update := range list {
		u, ok := update.(*tg.UpdateNewChannelMessage)
		if !ok {
			continue
		}
		message, ok := u.Message.(*tg.Message)
		if !ok {
			continue
		}
		peer, ok := message.PeerID.(*tg.PeerChannel)
		if !ok || peer.ChannelID != channelID {
			continue
		}
		media, ok := message.Media.(*tg.MessageMediaDocument)
		if !ok {
			continue
		}
		document, ok := media.Document.(*tg.Document)
		if !ok || document.MimeType != "application/octet-stream" {
			continue
		}
		for _, attribute := range document.Attributes {
			if name, ok := attribute.(*tg.DocumentAttributeFilename); ok && name.FileName == fileName {
				if candidate != 0 && candidate != message.ID {
					return 0
				}
				candidate = message.ID
			}
		}
	}
	return candidate
}

func (s *TelegramStore) messageID(ref string) (int, error) {
	parts := strings.Split(ref, ":")
	if len(parts) != 3 || parts[0] != "tg" || parts[1] != strconv.FormatInt(s.channel.ChannelID, 10) {
		return 0, domain.ErrInvalid
	}
	id, err := strconv.Atoi(parts[2])
	if err != nil || id <= 0 || id > 1<<31-1 {
		return 0, domain.ErrInvalid
	}
	return id, nil
}

func (s *TelegramStore) Get(ctx context.Context, ref string) ([]byte, error) {
	id, err := s.messageID(ref)
	if err != nil {
		return nil, err
	}
	ctx, cancel, onRetry := s.operationContext(ctx, maxCiphertextSize)
	defer cancel()
	return retryTelegram(ctx, func() ([]byte, error) {
		// Resolve the message on every attempt to refresh expiring file_reference.
		response, err := s.client.API().ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{Channel: s.channel, ID: []tg.InputMessageClass{&tg.InputMessageID{ID: id}}})
		if err != nil {
			return nil, err
		}
		modified, ok := response.AsModified()
		if !ok {
			return nil, domain.ErrNotFound
		}
		var document *tg.Document
		for _, item := range modified.GetMessages() {
			message, ok := item.(*tg.Message)
			if !ok || message.ID != id {
				continue
			}
			if media, ok := message.Media.(*tg.MessageMediaDocument); ok {
				document, _ = media.Document.(*tg.Document)
			}
		}
		if document == nil {
			return nil, domain.ErrNotFound
		}
		return downloadTelegramDocument(ctx, document, s.client.API(), s.client.Config().ThisDC, s.client.MediaOnly, onRetry)
	}, onRetry)
}

func downloadTelegramDocument(ctx context.Context, document *tg.Document, api *tg.Client, currentDC int, mediaOnly func(context.Context, int, int64) (telegram.CloseInvoker, error), onRetry func()) ([]byte, error) {
	if document.Size <= 0 || document.Size > maxCiphertextSize {
		return nil, domain.ErrIntegrity
	}
	primary := api.Invoker()
	media := document.DCID != currentDC
	// Same-DC media pools try to export authorization to themselves (DC_ID_INVALID).
	if media {
		connection, err := mediaOnly(ctx, document.DCID, 1)
		if err != nil {
			return nil, err
		}
		defer connection.Close()
		api = tg.NewClient(connection)
	}
	output := &ciphertextBuffer{remaining: document.Size}
	output.Grow(int(document.Size))
	invoker := api.Invoker()
	api = tg.NewClient(telegram.InvokeFunc(func(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
		err := invoker.Invoke(ctx, input, output)
		if media && tgerr.Is(err, "FILE_MIGRATE") {
			// Native primary routing transfers authorization and caches the target DC.
			media, invoker = false, primary
			onRetry()
			err = invoker.Invoke(ctx, input, output)
		}
		if _, flood := tgerr.AsFloodWait(err); flood {
			onRetry()
		}
		return err
	}))
	_, err := downloader.NewDownloader().WithRetryHandler(func(event downloader.RetryEvent) {
		if _, flood := tgerr.AsFloodWait(event.Err); !flood {
			onRetry()
		}
	}).Download(api, document.AsInputDocumentFileLocation("")).Stream(ctx, output)
	if err != nil {
		return nil, err
	}
	if output.remaining != 0 {
		return nil, domain.ErrIntegrity
	}
	return output.Bytes(), nil
}

type ciphertextBuffer struct {
	bytes.Buffer
	remaining int64
}

func (b *ciphertextBuffer) Write(p []byte) (int, error) {
	if int64(len(p)) > b.remaining {
		return 0, domain.ErrIntegrity
	}
	n, err := b.Buffer.Write(p)
	b.remaining -= int64(n)
	return n, err
}

func (s *TelegramStore) Delete(ctx context.Context, ref string) error {
	id, err := s.messageID(ref)
	if err != nil {
		return err
	}
	ctx, cancel, onRetry := s.operationContext(ctx, 0)
	defer cancel()
	_, err = retryTelegram(ctx, func() (*tg.MessagesAffectedMessages, error) {
		return s.client.API().ChannelsDeleteMessages(ctx, &tg.ChannelsDeleteMessagesRequest{Channel: s.channel, ID: []int{id}})
	}, onRetry)
	return err
}

func retryTelegram[T any](ctx context.Context, fn func() (T, error), onRetry ...func()) (T, error) {
	var zero T
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		value, err := fn()
		if err == nil {
			return value, nil
		}
		if attempt == 5 || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, domain.ErrIntegrity) || errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrInvalid) {
			return zero, err
		}
		delay, flood := tgerr.AsFloodWait(err)
		if !flood {
			if rpc, ok := tgerr.As(err); ok && rpc.Code < 500 && rpc.Type != "FILE_REFERENCE_EXPIRED" {
				return zero, err
			}
			delay = time.Duration(1<<attempt) * 250 * time.Millisecond
		}
		for _, notify := range onRetry {
			notify()
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return zero, ctx.Err()
		case <-timer.C:
		}
	}
}

func telegramChannelSelector(cfg TelegramConfig, allowCreate bool) (string, int64, error) {
	if cfg.ChannelLink != "" {
		if cfg.ChannelID < 0 || cfg.ChannelAccessHash != 0 {
			return "", 0, errors.New("a Telegram channel link accepts only an optional positive expected channel ID, without an access hash")
		}
		invite, id, err := parseTelegramChannelLink(cfg.ChannelLink)
		if err == nil && id != 0 && cfg.ChannelID != 0 && id != cfg.ChannelID {
			err = errTelegramChannelMismatch
		}
		return invite, id, err
	}
	if allowCreate && cfg.ChannelID == 0 && cfg.ChannelAccessHash == 0 {
		return "", 0, nil
	}
	if cfg.ChannelID <= 0 || cfg.ChannelAccessHash == 0 {
		return "", 0, errors.New("private Telegram channel link or channel ID and access hash required; run telegram-setup first")
	}
	return "", cfg.ChannelID, nil
}

func parseTelegramChannelLink(value string) (string, int64, error) {
	invalid := errors.New("use a private Telegram invite link (https://t.me/+HASH or https://t.me/joinchat/HASH) or message link (https://t.me/c/CHANNEL_ID/MESSAGE_ID); public usernames are unsupported")
	value = strings.TrimSpace(value)
	if len(value) > 2048 || strings.ContainsAny(value, "?#") {
		return "", 0, invalid
	}
	if strings.HasPrefix(strings.ToLower(value), "t.me/") {
		value = "https://" + value
	}
	u, err := url.Parse(value)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || !strings.EqualFold(u.Host, "t.me") || u.User != nil || u.RawPath != "" {
		return "", 0, invalid
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
		message, messageErr := strconv.ParseInt(parts[2], 10, 32)
		if idErr != nil || messageErr != nil || id <= 0 || message <= 0 || strconv.FormatInt(id, 10) != parts[1] || strconv.FormatInt(message, 10) != parts[2] {
			return "", 0, invalid
		}
		return "", id, nil
	}
	if invite == "" || len(invite) > 256 {
		return "", 0, invalid
	}
	for _, c := range invite {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return "", 0, invalid
		}
	}
	return invite, 0, nil
}

func configuredChannel(ctx context.Context, api *tg.Client, cfg TelegramConfig) (TelegramChannel, error) {
	invite, id, err := telegramChannelSelector(cfg, false)
	if err != nil {
		return TelegramChannel{}, err
	}
	var channel *tg.Channel
	if invite != "" {
		response, err := retryTelegram(ctx, func() (tg.ChatInviteClass, error) {
			return api.MessagesCheckChatInvite(ctx, invite)
		})
		if err != nil {
			if ctx.Err() != nil {
				return TelegramChannel{}, ctx.Err()
			}
			return TelegramChannel{}, fmt.Errorf("%w: Telegram private invite unavailable; verify the active link and existing owner membership", errTelegramChannelAccess)
		}
		already, ok := response.(*tg.ChatInviteAlready)
		if !ok {
			return TelegramChannel{}, fmt.Errorf("%w: Telegram private invite requires existing owner membership; sign in with the account that already owns this channel", errTelegramChannelAccess)
		}
		channel, _ = already.Chat.(*tg.Channel)
	} else if cfg.ChannelLink != "" {
		// ponytail: scan eligible dialogs; use a targeted iterator if unrelated retention checks become an availability issue.
		channels, err := discoverTelegramChannels(ctx, api)
		if err != nil && ctx.Err() != nil {
			return TelegramChannel{}, ctx.Err()
		}
		if err == nil {
			for _, candidate := range channels {
				if candidate.ID == id {
					return candidate, nil
				}
			}
		}
		return TelegramChannel{}, fmt.Errorf("%w: private message link channel unavailable; use an owned private channel with auto-delete disabled already joined by this account", errTelegramChannelAccess)
	} else {
		response, err := retryTelegram(ctx, func() (tg.MessagesChatsClass, error) {
			return api.ChannelsGetChannels(ctx, []tg.InputChannelClass{&tg.InputChannel{ChannelID: id, AccessHash: cfg.ChannelAccessHash}})
		})
		if err != nil {
			return TelegramChannel{}, err
		}
		for _, chat := range response.GetChats() {
			if candidate, ok := chat.(*tg.Channel); ok && candidate.ID == id {
				channel = candidate
				break
			}
		}
	}
	if channel == nil {
		return TelegramChannel{}, errTelegramChannelAccess
	}
	result, err := privateChannel(channel)
	if err != nil {
		return TelegramChannel{}, err
	}
	if cfg.ChannelID != 0 && result.ID != cfg.ChannelID {
		return TelegramChannel{}, errTelegramChannelMismatch
	}
	full, err := retryTelegram(ctx, func() (*tg.MessagesChatFull, error) {
		return api.ChannelsGetFullChannel(ctx, &tg.InputChannel{ChannelID: result.ID, AccessHash: result.AccessHash})
	})
	if err != nil {
		if ctx.Err() != nil {
			return TelegramChannel{}, ctx.Err()
		}
		return TelegramChannel{}, fmt.Errorf("%w: Telegram channel retention details unavailable; verify existing owner access", errTelegramChannelAccess)
	}
	if err := persistentChannel(full.FullChat, result.ID); err != nil {
		return TelegramChannel{}, err
	}
	return result, nil
}

func persistentChannel(full tg.ChatFullClass, id int64) error {
	channel, ok := full.(*tg.ChannelFull)
	if !ok || channel.ID != id {
		return fmt.Errorf("%w: Telegram channel retention details unavailable", errTelegramChannelAccess)
	}
	if channel.TTLPeriod != 0 {
		return errTelegramRetention
	}
	return nil
}

func privateChannel(channel *tg.Channel) (TelegramChannel, error) {
	if !channel.Creator || channel.Left || !channel.Broadcast || channel.Username != "" || len(channel.Usernames) != 0 || channel.Restricted || channel.HasLink {
		return TelegramChannel{}, fmt.Errorf("%w: storage requires an owned private channel without public usernames or linked discussion", errTelegramChannelAccess)
	}
	if channel.ID <= 0 || channel.AccessHash == 0 {
		return TelegramChannel{}, errTelegramChannelAccess
	}
	return TelegramChannel{ID: channel.ID, AccessHash: channel.AccessHash, Title: channel.Title}, nil
}

// DiscoverTelegramChannels lists existing eligible channels; it never creates one.
func DiscoverTelegramChannels(ctx context.Context, cfg TelegramConfig, phone string, code func(context.Context) (string, error), password func(context.Context) (string, error)) ([]TelegramChannel, error) {
	if cfg.QRCode == nil && (strings.TrimSpace(phone) == "" || code == nil) || password == nil {
		return nil, domain.ErrInvalid
	}
	if cfg.QRCode != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()
	}
	client, accepted, err := telegramSetupClient(cfg)
	if err != nil {
		return nil, err
	}
	var result []TelegramChannel
	err = client.Run(ctx, func(ctx context.Context) error {
		a := terminalAuth{phone: phone, code: code, password: password, delivery: cfg.CodeDelivery}
		if err := a.authenticate(ctx, client.Auth(), client.QR(), accepted, cfg.QRCode); err != nil {
			return err
		}
		result, err = discoverTelegramChannels(ctx, client.API())
		return err
	})
	return result, telegramResolutionError(ctx, err, len(result) > 0)
}

func telegramResolutionError(ctx context.Context, err error, resolved bool) error {
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !resolved {
		return errTelegramConnection
	}
	return nil
}

func discoverTelegramChannels(ctx context.Context, api *tg.Client) ([]TelegramChannel, error) {
	result := []TelegramChannel{}
	seen := make(map[int64]bool)
	for _, folder := range []int{0, 1} {
		finished := false
		iter := dialogs.NewIterator(dialogs.QueryFunc(func(ctx context.Context, offset dialogs.Request) (tg.MessagesDialogsClass, error) {
			if finished {
				return &tg.MessagesDialogs{}, nil
			}
			_, first := offset.OffsetPeer.(*tg.InputPeerEmpty)
			request := &tg.MessagesGetDialogsRequest{ExcludePinned: !first, OffsetDate: offset.OffsetDate, OffsetID: offset.OffsetID, OffsetPeer: offset.OffsetPeer, Limit: offset.Limit}
			request.SetFolderID(folder)
			response, err := retryTelegram(ctx, func() (tg.MessagesDialogsClass, error) {
				return api.MessagesGetDialogs(ctx, request)
			})
			_, finished = response.(*tg.MessagesDialogs)
			return response, err
		}), 100)
		for iter.Next(ctx) {
			elem := iter.Value()
			peer, ok := elem.Peer.(*tg.InputPeerChannel)
			if !ok || seen[peer.ChannelID] {
				continue
			}
			channel, ok := elem.Entities.Channel(peer.ChannelID)
			if !ok {
				continue
			}
			candidate, err := privateChannel(channel)
			if err != nil {
				continue
			}
			seen[peer.ChannelID] = true
			full, err := retryTelegram(ctx, func() (*tg.MessagesChatFull, error) {
				return api.ChannelsGetFullChannel(ctx, &tg.InputChannel{ChannelID: candidate.ID, AccessHash: candidate.AccessHash})
			})
			if err != nil {
				return nil, fmt.Errorf("inspect Telegram channel retention: %w", err)
			}
			if persistentChannel(full.FullChat, candidate.ID) == nil {
				result = append(result, candidate)
			}
		}
		if err := iter.Err(); err != nil {
			return nil, fmt.Errorf("list Telegram channels: %w", err)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		a, b := strings.ToLower(result[i].Title), strings.ToLower(result[j].Title)
		if a == b {
			return result[i].ID < result[j].ID
		}
		return a < b
	})
	if len(result) == 0 {
		return nil, fmt.Errorf("%w: no eligible channels found; use a private channel owned by this account with auto-delete disabled", errTelegramChannelAccess)
	}
	return result, nil
}

// SetupTelegram authenticates only through terminal callbacks and creates a
// private channel when no existing channel is configured. Account signup refused.
func SetupTelegram(ctx context.Context, cfg TelegramConfig, phone string, code func(context.Context) (string, error), password func(context.Context) (string, error)) (TelegramChannel, error) {
	if _, _, err := telegramChannelSelector(cfg, true); err != nil {
		return TelegramChannel{}, err
	}
	if cfg.QRCode == nil && (phone == "" || code == nil) || password == nil {
		return TelegramChannel{}, domain.ErrInvalid
	}
	if cfg.QRCode != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()
	}
	client, accepted, err := telegramSetupClient(cfg)
	if err != nil {
		return TelegramChannel{}, err
	}
	var result TelegramChannel
	err = client.Run(ctx, func(ctx context.Context) error {
		a := terminalAuth{phone: phone, code: code, password: password, delivery: cfg.CodeDelivery}
		if err := a.authenticate(ctx, client.Auth(), client.QR(), accepted, cfg.QRCode); err != nil {
			return err
		}
		if cfg.ChannelLink != "" || cfg.ChannelID != 0 {
			result, err = configuredChannel(ctx, client.API(), cfg)
			return err
		}
		updates, err := client.API().ChannelsCreateChannel(ctx, &tg.ChannelsCreateChannelRequest{Broadcast: true, Title: "Harbor private storage", About: "Encrypted private drive chunks. Do not delete messages or invite members."})
		if err != nil {
			return err
		}
		withChats, ok := updates.(interface{ GetChats() []tg.ChatClass })
		if !ok {
			return errors.New("Telegram channel creation returned no channel")
		}
		for _, chat := range withChats.GetChats() {
			if channel, ok := chat.(*tg.Channel); ok {
				result, err = privateChannel(channel)
				if err != nil {
					return err
				}
				cfg.ChannelID, cfg.ChannelAccessHash = result.ID, result.AccessHash
				result, err = configuredChannel(ctx, client.API(), cfg)
				return err
			}
		}
		return errors.New("Telegram channel creation returned no channel")
	})
	return result, telegramResolutionError(ctx, err, result.ID > 0 && result.AccessHash != 0)
}

type terminalAuth struct {
	phone          string
	code, password func(context.Context) (string, error)
	delivery       func(string)
}

func (a terminalAuth) authenticate(ctx context.Context, client *auth.Client, qr qrlogin.QR, accepted qrlogin.LoggedIn, show func(context.Context, string) error) error {
	if show == nil {
		return client.IfNecessary(ctx, auth.NewFlow(a, auth.SendCodeOptions{}))
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	status, err := client.Status(ctx)
	if err != nil || status.Authorized {
		return err
	}
	_, err = qr.Auth(ctx, accepted, func(ctx context.Context, token qrlogin.Token) error {
		return show(ctx, token.URL())
	})
	if tgerr.Is(err, "SESSION_PASSWORD_NEEDED") {
		password, err := a.Password(ctx)
		if err != nil {
			return err
		}
		_, err = client.Password(ctx, password)
		return err
	}
	return err
}

func (a terminalAuth) Phone(context.Context) (string, error)        { return a.phone, nil }
func (a terminalAuth) Password(ctx context.Context) (string, error) { return a.password(ctx) }
func (a terminalAuth) Code(ctx context.Context, sentCode *tg.AuthSentCode) (string, error) {
	method := "unsupported"
	if sentCode != nil {
		switch sentCode.Type.(type) {
		case *tg.AuthSentCodeTypeApp:
			method = "app"
		case *tg.AuthSentCodeTypeSMS:
			method = "sms"
		case *tg.AuthSentCodeTypeCall:
			method = "call"
		case *tg.AuthSentCodeTypeMissedCall:
			method = "missed_call"
		case *tg.AuthSentCodeTypeSMSWord:
			method = "sms_word"
		case *tg.AuthSentCodeTypeSMSPhrase:
			method = "sms_phrase"
		case *tg.AuthSentCodeTypeEmailCode:
			method = "email_code"
		case *tg.AuthSentCodeTypeSetUpEmailRequired:
			method = "email_setup"
		case *tg.AuthSentCodeTypeFirebaseSMS:
			method = "firebase_sms"
		case *tg.AuthSentCodeTypeFragmentSMS:
			method = "fragment_sms"
		case *tg.AuthSentCodeTypeFlashCall:
			method = "flash_call"
		}
	}
	if a.delivery != nil {
		a.delivery(method)
	}
	switch method {
	case "app", "sms", "call", "sms_word", "sms_phrase":
		return a.code(ctx)
	default:
		return "", errTelegramAuthMethod
	}
}
func (terminalAuth) AcceptTermsOfService(context.Context, tg.HelpTermsOfService) error {
	return errors.New("create a dedicated Telegram account before setup")
}
func (terminalAuth) SignUp(context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, errors.New("account signup unavailable; create a dedicated Telegram account before setup")
}

// Session data is a credential. Writes are atomic and owner-readable only.
type privateSession struct {
	path string
	mu   sync.Mutex
}

func (s *privateSession) LoadSession(ctx context.Context) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := os.Lstat(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, session.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("%w: Telegram session must be a regular file with mode 0600", errTelegramSession)
	}
	return os.ReadFile(s.path)
}

func (s *privateSession) StoreSession(ctx context.Context, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".telegram-session-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(f.Name(), s.path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(s.path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
