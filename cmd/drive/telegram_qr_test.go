package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestTelegramQRDoesNotPrintLoginURL(t *testing.T) {
	const token = "synthetic-private-login-token"
	var output bytes.Buffer
	if err := printTelegramQR(&output, "tg://login?token="+token); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), token) || strings.Contains(output.String(), "tg://") || !strings.Contains(output.String(), "Link Desktop Device") {
		t.Fatal("QR display leaked login text or omitted approval guidance")
	}
	if err := printTelegramQR(rejectedQRWriter{}, "tg://login?token="+token); err == nil || strings.Contains(err.Error(), token) {
		t.Fatal("QR output failure was ignored or leaked login text")
	}
	if err := printTelegramQR(&bytes.Buffer{}, strings.Repeat(token, 1000)); err == nil || strings.Contains(err.Error(), token) {
		t.Fatal("QR encoding failure was ignored or leaked login text")
	}
}

type rejectedQRWriter struct{}

func (rejectedQRWriter) Write([]byte) (int, error) { return 0, errors.New("private-writer-detail") }
