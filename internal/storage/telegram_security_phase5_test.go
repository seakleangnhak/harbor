//go:build telegram

package storage

import (
	"context"
	"errors"
	"testing"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"

	"private-drive/internal/domain"
)

func TestPhase5TelegramTerminalAccessFailuresReturnNoContent(t *testing.T) {
	for _, kind := range []string{"SESSION_EXPIRED", "SESSION_REVOKED", "AUTH_KEY_UNREGISTERED", "AUTH_KEY_DUPLICATED", "CHANNEL_PRIVATE", "CHAT_ADMIN_REQUIRED", "CHAT_WRITE_FORBIDDEN"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			code := 400
			if kind == "SESSION_EXPIRED" || kind == "SESSION_REVOKED" || kind == "AUTH_KEY_UNREGISTERED" || kind == "AUTH_KEY_DUPLICATED" {
				code = 401
			}
			want := tgerr.New(code, kind)
			client := telegramOfflineClient(func(_ context.Context, input bin.Encoder, _ bin.Decoder) error {
				calls++
				if _, ok := input.(*tg.ChannelsGetMessagesRequest); !ok {
					t.Fatalf("denied read attempted unrelated RPC: %T", input)
				}
				return want
			})
			store := &TelegramStore{ctx: context.Background(), client: client, channel: &tg.InputChannel{ChannelID: 12}}
			content, err := store.Get(context.Background(), "tg:12:7")
			if !errors.Is(err, want) || content != nil || calls != 1 || store.Status().RetryCount != 0 {
				t.Fatalf("terminal denial retried or returned content: calls=%d bytes=%d err=%v", calls, len(content), err)
			}
		})
	}
}

func TestPhase5TelegramForeignAndInvalidReferencesNeverReachRPC(t *testing.T) {
	client := telegramOfflineClient(func(context.Context, bin.Encoder, bin.Decoder) error {
		t.Fatal("invalid channel/message reference reached RPC")
		return nil
	})
	store := &TelegramStore{ctx: context.Background(), client: client, channel: &tg.InputChannel{ChannelID: 12}}
	for _, ref := range []string{"tg:13:7", "tg:12:0", "tg:12:-1", "tg:12:2147483648", "tg:12:7:extra", "https://t.me/c/12/7", "tg:12:../../private"} {
		content, err := store.Get(context.Background(), ref)
		if !errors.Is(err, domain.ErrInvalid) || content != nil {
			t.Fatal("invalid reference returned content or wrong classification")
		}
		if err := store.Delete(context.Background(), ref); !errors.Is(err, domain.ErrInvalid) {
			t.Fatal("invalid deletion reference was accepted")
		}
	}
}
