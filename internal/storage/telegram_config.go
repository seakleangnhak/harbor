package storage

import "context"

type TelegramConfig struct {
	AppID             int
	AppHash           string
	SessionFile       string
	ChannelLink       string
	ChannelID         int64
	ChannelAccessHash int64
	CodeDelivery      func(string)                        // Fixed delivery-method name only; no destination or code.
	QRCode            func(context.Context, string) error // Temporary login URL; render only in private local Terminal.
}

type TelegramChannel struct {
	ID         int64
	AccessHash int64
	Title      string
}
