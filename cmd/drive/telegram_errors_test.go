//go:build telegram

package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tgerr"
)

func TestTelegramFailuresAreActionableWithoutProviderText(t *testing.T) {
	const secret = "private-fixture-do-not-log"
	for _, tc := range []struct {
		err  error
		code string
	}{
		{tgerr.New(400, "PHONE_CODE_INVALID"), "code_invalid"},
		{tgerr.New(400, "PHONE_CODE_EXPIRED"), "code_expired"},
		{auth.ErrPasswordInvalid, "password_invalid"},
		{tgerr.New(420, "FLOOD_WAIT_120"), "flood_wait"},
		{context.Canceled, "canceled"},
		{errors.New(secret), "unknown"},
	} {
		message := telegramFailure(fmt.Errorf("%s: %w", secret, tc.err)).Error()
		if !strings.Contains(message, "["+tc.code+"]") || strings.Contains(message, secret) {
			t.Fatalf("unsafe or unhelpful setup error for %s", tc.code)
		}
	}
}

func TestTelegramCodeGuidanceUsesFixedDeliveryNames(t *testing.T) {
	const secret = "private-phone-email-url-code"
	for _, tc := range []struct{ method, contains string }{
		{"app", "Telegram service chat"},
		{"sms", "SMS"},
		{"sms_word", "word or phrase"},
		{"sms_phrase", "word or phrase"},
		{"call", "spoken code"},
		{"missed_call", "does not support caller-ID"},
		{"email_code", "does not support"},
		{"email_setup", "does not support"},
		{secret, "unsupported login method"},
	} {
		message := telegramCodeGuidance(tc.method)
		if !strings.Contains(message, tc.contains) || strings.Contains(message, secret) {
			t.Fatalf("unsafe or incorrect delivery guidance for fixture")
		}
	}
}
