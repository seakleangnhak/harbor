package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/term"
	"private-drive/internal/storage"
	"rsc.io/qr"
)

func telegramConfig(dataDir string) (storage.TelegramConfig, error) {
	id, err := strconv.Atoi(os.Getenv("TELEGRAM_APP_ID"))
	if err != nil || id <= 0 || os.Getenv("TELEGRAM_APP_HASH") == "" {
		return storage.TelegramConfig{}, errors.New("set server-side TELEGRAM_APP_ID and TELEGRAM_APP_HASH from my.telegram.org")
	}
	var channel, access int64
	if value := os.Getenv("TELEGRAM_CHANNEL_ID"); value != "" {
		channel, err = strconv.ParseInt(value, 10, 64)
		if err == nil && channel < -1000000000000 && channel > -2000000000000 {
			channel = -channel - 1000000000000
		}
		if err != nil || channel <= 0 {
			return storage.TelegramConfig{}, errors.New("TELEGRAM_CHANNEL_ID must be a positive MTProto ID or valid Bot API channel ID")
		}
	}
	if value := os.Getenv("TELEGRAM_CHANNEL_ACCESS_HASH"); value != "" {
		access, err = strconv.ParseInt(value, 10, 64)
		if err != nil || access == 0 {
			return storage.TelegramConfig{}, errors.New("TELEGRAM_CHANNEL_ACCESS_HASH must be a nonzero signed 64-bit integer")
		}
	}
	link := strings.TrimSpace(os.Getenv("USERNAME_OR_CHANNEL_LINK"))
	if link == "" && os.Getenv("USERNAME_OR_CHANNEL_LINK") != "" {
		return storage.TelegramConfig{}, errors.New("USERNAME_OR_CHANNEL_LINK must contain a private channel link")
	}
	if link != "" && access != 0 {
		return storage.TelegramConfig{}, errors.New("use a channel link or a manual access hash, not both; channel ID may verify the link target")
	}
	if link == "" && (channel == 0) != (access == 0) {
		return storage.TelegramConfig{}, errors.New("set both TELEGRAM_CHANNEL_ID and TELEGRAM_CHANNEL_ACCESS_HASH, or use USERNAME_OR_CHANNEL_LINK")
	}
	return storage.TelegramConfig{AppID: id, AppHash: os.Getenv("TELEGRAM_APP_HASH"), SessionFile: env("TELEGRAM_SESSION_FILE", filepath.Join(dataDir, "telegram.session")), ChannelID: channel, ChannelAccessHash: access, ChannelLink: link}, nil
}

func showPassword(dataDir string) error {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return errors.New("password display requires an interactive terminal")
	}
	b, err := readPrivateFile(env("DRIVE_OWNER_PASSWORD_FILE", filepath.Join(dataDir, "owner-password")))
	if err != nil {
		return err
	}
	fmt.Print(string(b))
	return nil
}

func setupTelegram(ctx context.Context, dataDir string, discover, qrLogin bool) error {
	cfg, err := telegramConfig(dataDir)
	if err != nil {
		return err
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return errors.New("Telegram setup requires an interactive terminal for input and output")
	}
	var phone string
	if qrLogin {
		fmt.Println("Use the dedicated account in Telegram: Settings > Devices > Link Desktop Device. Scan and approve the QR shown here. Never share its screenshot.")
		cfg.QRCode = func(ctx context.Context, url string) error { return printTelegramQR(os.Stdout, url) }
	} else {
		fmt.Print("Use a dedicated Telegram account and a private storage channel.\nPhone number (visible in this Terminal; include +country code): ")
		phone, err = bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil {
			return errors.New("phone input stopped; restart setup when ready")
		}
	}
	cfg.CodeDelivery = func(method string) { fmt.Println(telegramCodeGuidance(method)) }
	code := func(ctx context.Context) (string, error) {
		fmt.Print("Telegram login code: ")
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		return strings.TrimSpace(string(b)), err
	}
	password := func(ctx context.Context) (string, error) {
		fmt.Print("Telegram two-step verification password: ")
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		return string(b), err
	}
	if discover {
		channels, err := storage.DiscoverTelegramChannels(ctx, cfg, strings.TrimSpace(phone), code, password)
		if err != nil {
			return telegramFailure(err)
		}
		fmt.Println("Owned private channels. Keep these values server-side; use the pair for your chosen channel:")
		for _, channel := range channels {
			fmt.Printf("\nChannel %q\nTELEGRAM_CHANNEL_ID=%d\nTELEGRAM_CHANNEL_ACCESS_HASH=%d\n", channel.Title, channel.ID, channel.AccessHash)
		}
		return nil
	}
	channel, err := storage.SetupTelegram(ctx, cfg, strings.TrimSpace(phone), code, password)
	if err != nil {
		return telegramFailure(err)
	}
	if cfg.ChannelLink != "" {
		fmt.Println("Private channel verified. Keep USERNAME_OR_CHANNEL_LINK set; access hash resolves server-side and configured channel ID is checked.")
		return nil
	}
	fmt.Printf("Private channel configured. Store these server-side values securely:\nTELEGRAM_CHANNEL_ID=%d\nTELEGRAM_CHANNEL_ACCESS_HASH=%d\n", channel.ID, channel.AccessHash)
	return nil
}

func printTelegramQR(w io.Writer, loginURL string) error {
	code, err := qr.Encode(loginURL, qr.M)
	if err != nil {
		return errors.New("cannot render Telegram login QR")
	}
	var out strings.Builder
	out.WriteString("\x1b[2J\x1b[HScan with dedicated account: Telegram > Settings > Devices > Link Desktop Device.\nDo not share this QR. Two-step password stays hidden if requested.\n\x1b[30;47m")
	for y := -4; y < code.Size+4; y += 2 {
		for x := -4; x < code.Size+4; x++ {
			switch top, bottom := code.Black(x, y), code.Black(x, y+1); {
			case top && bottom:
				out.WriteRune('█')
			case top:
				out.WriteRune('▀')
			case bottom:
				out.WriteRune('▄')
			default:
				out.WriteByte(' ')
			}
		}
		out.WriteByte('\n')
	}
	out.WriteString("\x1b[0m\nWaiting for account approval (up to 3 minutes)...\n")
	if _, err := io.WriteString(w, out.String()); err != nil {
		return errors.New("cannot display Telegram login QR")
	}
	return nil
}

func telegramCodeGuidance(method string) string {
	switch method {
	case "app":
		return "Code delivery: Telegram app. Check the verified Telegram service chat on every device already signed in to this dedicated account; the code may not arrive by SMS."
	case "sms", "sms_word", "sms_phrase":
		return "Code delivery: SMS. Check text messages for this phone number; enter the code, word or phrase exactly as sent."
	case "call":
		return "Code delivery: phone call. Answer Telegram's call and enter the spoken code."
	case "missed_call":
		return "Code delivery: missed call. This Terminal flow does not support caller-ID login yet."
	case "email_code", "email_setup":
		return "Code delivery: email verification. This Terminal flow does not support Telegram's email verification method yet."
	default:
		return "Code delivery: unsupported login method. No code will be requested by this Terminal flow."
	}
}

func telegramFailure(err error) error {
	code := storage.TelegramSetupErrorCode(err)
	messages := map[string]string{
		"build_disabled":          "build with -tags telegram before setup",
		"api_credentials":         "Telegram rejected application credentials; verify the server-side API ID/hash from my.telegram.org",
		"phone_invalid":           "phone number rejected; enter the dedicated account number with country code in Terminal",
		"phone_banned":            "Telegram restricted this account; resolve account access in Telegram before retrying",
		"code_invalid":            "login code rejected; start setup again and enter the latest code only in Terminal",
		"code_expired":            "login code expired; start setup again and enter the new code only in Terminal",
		"password_invalid":        "two-step password rejected; use the account's Telegram two-step password in Terminal",
		"password_required":       "two-step verification required; complete its password prompt in Terminal",
		"flood_wait":              "Telegram rate limit reached; wait before retrying and avoid repeated code requests",
		"session_invalid":         "session unavailable or rejected; verify private regular-file permissions and run local setup again",
		"auth_restart":            "Telegram requested a fresh authorization; restart local setup once",
		"auth_method_unsupported": "Telegram requires a login method this Terminal flow does not support; review its delivery notice and avoid repeated code requests",
		"channel_mismatch":        "channel link and configured ID identify different channels; verify the intended target before retrying",
		"channel_access":          "channel unavailable; use its existing owner account and a valid private link, without public aliases or linked discussion",
		"channel_retention":       "channel auto-delete must be disabled before storage can start",
		"connection":              "connection to Telegram failed; check network connectivity before retrying",
		"timeout":                 "Telegram request timed out; check connectivity before retrying",
		"canceled":                "local setup canceled; rerun when ready",
	}
	message, ok := messages[code]
	if !ok {
		code, message = "unknown", "setup failed; verify network, account access and private-channel configuration before retrying"
	}
	return fmt.Errorf("Telegram [%s]: %s", code, message)
}
