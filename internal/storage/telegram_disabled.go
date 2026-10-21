//go:build !telegram

package storage

import (
	"context"
	"errors"
	"net"

	"private-drive/internal/domain"
)

var errTelegramDisabled = errors.New("Telegram support requires a build with -tags telegram")

func TelegramSetupErrorCode(err error) string {
	switch {
	case errors.Is(err, errTelegramDisabled):
		return "build_disabled"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
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

type TelegramStore struct{}

func (*TelegramStore) ChannelID() int64 { return 0 }

func (*TelegramStore) Status() domain.StorageStatus {
	return domain.StorageStatus{State: "disconnected"}
}

func NewTelegram(context.Context, TelegramConfig) (*TelegramStore, error) {
	return nil, errTelegramDisabled
}

func SetupTelegram(context.Context, TelegramConfig, string, func(context.Context) (string, error), func(context.Context) (string, error)) (TelegramChannel, error) {
	return TelegramChannel{}, errTelegramDisabled
}

func DiscoverTelegramChannels(context.Context, TelegramConfig, string, func(context.Context) (string, error), func(context.Context) (string, error)) ([]TelegramChannel, error) {
	return nil, errTelegramDisabled
}

func (*TelegramStore) Put(context.Context, string, []byte) (string, error) {
	return "", errTelegramDisabled
}

func (*TelegramStore) Get(context.Context, string) ([]byte, error) {
	return nil, errTelegramDisabled
}

func (*TelegramStore) Delete(context.Context, string) error { return errTelegramDisabled }
func (*TelegramStore) Close() error                         { return nil }
