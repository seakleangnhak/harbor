//go:build telegram

package storage

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

func TestTelegramMediaMigrationUsesNativePrimaryRouting(t *testing.T) {
	payload := bytes.Repeat([]byte{0xa5}, (1<<20)+17)
	document := &tg.Document{ID: 7, AccessHash: 8, FileReference: []byte("fixture"), Size: int64(len(payload)), DCID: 4}
	mediaReads, primaryReads, retries := 0, 0, 0
	read := func(input bin.Encoder, output bin.Decoder, offset int64) {
		request, ok := input.(*tg.UploadGetFileRequest)
		if !ok || request.Offset != offset || request.Limit != 512<<10 {
			t.Fatalf("migration changed download request: %T", input)
		}
		location, ok := request.Location.(*tg.InputDocumentFileLocation)
		if !ok || location.ID != document.ID || location.AccessHash != document.AccessHash || !bytes.Equal(location.FileReference, document.FileReference) || location.ThumbSize != "" {
			t.Fatal("migration changed document identity or reference")
		}
		end := min(int(offset)+request.Limit, len(payload))
		output.(*tg.UploadFileBox).File = &tg.UploadFile{Type: &tg.StorageFileUnknown{}, Bytes: payload[int(offset):end]}
	}
	primary := tg.NewClient(telegramTestInvoker(func(_ context.Context, input bin.Encoder, output bin.Decoder) error {
		primaryReads++
		read(input, output, int64(primaryReads*(512<<10)))
		return nil
	}))
	connection := &telegramTestDownloadConnection{telegramTestInvoker: func(_ context.Context, input bin.Encoder, output bin.Decoder) error {
		mediaReads++
		if mediaReads == 1 {
			read(input, output, 0)
			return nil
		}
		return tgerr.New(303, "FILE_MIGRATE_5")
	}}
	mediaOnly := func(_ context.Context, dc int, max int64) (telegram.CloseInvoker, error) {
		if dc != 4 || max != 1 {
			t.Fatal("changed document-directed media pool")
		}
		return connection, nil
	}
	got, err := downloadTelegramDocument(context.Background(), document, primary, 2, mediaOnly, func() { retries++ })
	if err != nil || !bytes.Equal(got, payload) || mediaReads != 2 || primaryReads != 2 || retries != 1 || !connection.closed {
		t.Fatalf("media redirect did not recover exact bytes: media=%d primary=%d retries=%d closed=%t err=%v", mediaReads, primaryReads, retries, connection.closed, err)
	}
}

func TestTelegramMediaMigrationFailureIsBoundedAndClosesConnection(t *testing.T) {
	for _, kind := range []string{"FILE_MIGRATE_5", "AUTH_KEY_UNREGISTERED", "CHANNEL_PRIVATE", "FILE_REFERENCE_INVALID"} {
		t.Run(kind, func(t *testing.T) {
			mediaReads, primaryReads := 0, 0
			want := tgerr.New(400, kind)
			if kind == "FILE_MIGRATE_5" {
				want = tgerr.New(303, kind)
			}
			primary := tg.NewClient(telegramTestInvoker(func(context.Context, bin.Encoder, bin.Decoder) error {
				primaryReads++
				return want
			}))
			connection := &telegramTestDownloadConnection{telegramTestInvoker: func(context.Context, bin.Encoder, bin.Decoder) error {
				mediaReads++
				return want
			}}
			document := &tg.Document{ID: 7, Size: 17, DCID: 4}
			got, err := downloadTelegramDocument(context.Background(), document, primary, 2, func(context.Context, int, int64) (telegram.CloseInvoker, error) {
				return connection, nil
			}, func() {})
			wantPrimary := 0
			if kind == "FILE_MIGRATE_5" {
				wantPrimary = 1
			}
			if !errors.Is(err, want) || got != nil || mediaReads != 1 || primaryReads != wantPrimary || !connection.closed {
				t.Fatalf("terminal error leaked bytes or looped: media=%d primary=%d closed=%t err=%v", mediaReads, primaryReads, connection.closed, err)
			}
		})
	}
}

func TestTelegramMediaMigrationPreservesCancellation(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		primaryReads := 0
		primary := tg.NewClient(telegramTestInvoker(func(requestCtx context.Context, _ bin.Encoder, _ bin.Decoder) error {
			primaryReads++
			<-requestCtx.Done()
			return requestCtx.Err()
		}))
		connection := &telegramTestDownloadConnection{telegramTestInvoker: func(context.Context, bin.Encoder, bin.Decoder) error {
			if !deadline {
				cancel()
			}
			return tgerr.New(303, "FILE_MIGRATE_5")
		}}
		got, err := downloadTelegramDocument(ctx, &tg.Document{ID: 7, Size: 17, DCID: 4}, primary, 2, func(context.Context, int, int64) (telegram.CloseInvoker, error) {
			return connection, nil
		}, func() {})
		want := ctx.Err()
		cancel()
		if want == nil || !errors.Is(err, want) || got != nil || primaryReads != 1 || !connection.closed {
			t.Fatalf("migration ignored cancellation: deadline=%t primary=%d closed=%t err=%v", deadline, primaryReads, connection.closed, err)
		}
	}
}

func TestTelegramGetRefreshesReferenceAfterPartialDownload(t *testing.T) {
	payload := bytes.Repeat([]byte{0xa5}, (512<<10)+17)
	resolutions, reads := 0, 0
	client := telegramOfflineClient(func(_ context.Context, input bin.Encoder, output bin.Decoder) error {
		switch request := input.(type) {
		case *tg.ChannelsGetMessagesRequest:
			resolutions++
			output.(*tg.MessagesMessagesBox).Messages = &tg.MessagesChannelMessages{Messages: []tg.MessageClass{&tg.Message{ID: 7, Media: &tg.MessageMediaDocument{Document: &tg.Document{ID: 8, AccessHash: 9, FileReference: []byte{byte(resolutions)}, Size: int64(len(payload))}}}}}
		case *tg.UploadGetFileRequest:
			reads++
			location := request.Location.(*tg.InputDocumentFileLocation)
			if location.FileReference[0] != byte(resolutions) || request.Offset != int64((reads-1)%2)*(512<<10) {
				t.Fatal("reference refresh reused old bytes or offset")
			}
			if resolutions == 1 && request.Offset != 0 {
				return tgerr.New(400, "FILE_REFERENCE_EXPIRED")
			}
			end := min(int(request.Offset)+request.Limit, len(payload))
			output.(*tg.UploadFileBox).File = &tg.UploadFile{Type: &tg.StorageFileUnknown{}, Bytes: payload[int(request.Offset):end]}
		default:
			t.Fatalf("unexpected offline RPC: %T", input)
		}
		return nil
	})
	store := &TelegramStore{ctx: context.Background(), client: client, channel: &tg.InputChannel{ChannelID: 12}}
	got, err := store.Get(context.Background(), "tg:12:7")
	if err != nil || !bytes.Equal(got, payload) || resolutions != 2 || reads != 4 || store.Status().RetryCount != 1 {
		t.Fatalf("partial expired-reference retry changed ciphertext: resolutions=%d reads=%d err=%v", resolutions, reads, err)
	}
}
