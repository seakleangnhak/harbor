//go:build telegram

package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"

	"private-drive/internal/domain"
)

func TestTelegram100MiBDocumentUsesBigPartsAndDownloadsIntact(t *testing.T) {
	payload := bytes.Repeat([]byte{0xa5}, int(maxCiphertextSize)+1)
	const partSize = 512 << 10
	wantParts := (int(maxCiphertextSize) + partSize - 1) / partSize
	var mu sync.Mutex
	parts := make(map[int]bool)
	var fileID int64
	var downloadedBytes int64
	resolutions, downloadRequests := 0, 0
	client := telegramOfflineClient(func(_ context.Context, input bin.Encoder, output bin.Decoder) error {
		switch request := input.(type) {
		case *tg.UploadSaveBigFilePartRequest:
			mu.Lock()
			defer mu.Unlock()
			if len(parts) == 0 {
				fileID = request.FileID
			}
			start := request.FilePart * partSize
			end := min(start+partSize, int(maxCiphertextSize))
			if request.FilePart < 0 || request.FilePart >= wantParts || request.FileID != fileID || request.FileTotalParts != wantParts || parts[request.FilePart] || start > end || !bytes.Equal(request.Bytes, payload[start:end]) {
				t.Error("large document had duplicate, missing or incorrect multipart bytes")
				return domain.ErrIntegrity
			}
			parts[request.FilePart] = true
			output.(*tg.BoolBox).Bool = &tg.BoolTrue{}
		case *tg.MessagesSendMediaRequest:
			media, ok := request.Media.(*tg.InputMediaUploadedDocument)
			if !ok {
				return domain.ErrInvalid
			}
			file, ok := media.File.(*tg.InputFileBig)
			mu.Lock()
			valid := ok && len(parts) == wantParts && file.Parts == wantParts && file.ID == fileID
			mu.Unlock()
			if !valid || !media.ForceFile || media.MimeType != "application/octet-stream" {
				t.Error("100 MiB document was published before all big-file parts completed")
				return domain.ErrIntegrity
			}
			output.(*tg.UpdatesBox).Updates = &tg.Updates{Updates: []tg.UpdateClass{&tg.UpdateMessageID{ID: 7, RandomID: request.RandomID}}}
		case *tg.ChannelsGetMessagesRequest:
			resolutions++
			output.(*tg.MessagesMessagesBox).Messages = &tg.MessagesChannelMessages{Messages: []tg.MessageClass{&tg.Message{ID: 7, Media: &tg.MessageMediaDocument{Document: &tg.Document{ID: 8, AccessHash: 9, Size: maxCiphertextSize}}}}}
		case *tg.UploadGetFileRequest:
			if request.Offset != downloadedBytes || request.Limit != partSize {
				return domain.ErrIntegrity
			}
			end := min(request.Offset+int64(request.Limit), maxCiphertextSize)
			downloadedBytes += end - request.Offset
			downloadRequests++
			output.(*tg.UploadFileBox).File = &tg.UploadFile{Type: &tg.StorageFileUnknown{}, Bytes: payload[request.Offset:end]}
		default:
			t.Errorf("unexpected large-file RPC: %T", input)
			return domain.ErrInvalid
		}
		return nil
	})
	store := &TelegramStore{ctx: context.Background(), client: client, channel: &tg.InputChannel{ChannelID: 12}}
	if _, err := store.Put(context.Background(), strings.Repeat("a", 32), payload); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("over-limit Telegram document accepted: %v", err)
	}
	ref, err := store.Put(context.Background(), strings.Repeat("a", 32), payload[:maxCiphertextSize])
	if err != nil || ref != "tg:12:7" {
		t.Fatalf("100 MiB multipart upload: %q %v", ref, err)
	}
	got, err := store.Get(context.Background(), ref)
	if err != nil || len(got) != int(maxCiphertextSize) || sha256.Sum256(got) != sha256.Sum256(payload[:maxCiphertextSize]) {
		t.Fatalf("100 MiB Telegram document checksum: bytes=%d error=%v", len(got), err)
	}
	if resolutions != 1 || downloadRequests != wantParts || downloadedBytes != maxCiphertextSize {
		t.Fatalf("read amplification changed: resolutions=%d requests=%d bytes=%d", resolutions, downloadRequests, downloadedBytes)
	}
	t.Logf("controlled Telegram read: resolutions=%d requests=%d bytes=%d", resolutions, downloadRequests, downloadedBytes)
	if _, err := downloadTelegramDocument(context.Background(), &tg.Document{Size: maxCiphertextSize + 1}, nil, 0, nil, nil); !errors.Is(err, domain.ErrIntegrity) {
		t.Fatalf("over-limit Telegram download attempted RPC: %v", err)
	}
}

func TestTelegramLargeTransferDeadlineBoundAndCancellation(t *testing.T) {
	store := &TelegramStore{ctx: context.Background()}
	for _, size := range []int64{0, maxCiphertextSize, 1 << 40} {
		ctx, cancel, _ := store.operationContext(context.Background(), size)
		deadline, ok := ctx.Deadline()
		remaining := time.Until(deadline)
		if !ok || remaining < 119*time.Second || remaining > 10*time.Minute {
			t.Fatalf("transfer deadline out of bounds: bytes=%d remaining=%s", size, remaining)
		}
		if size == maxCiphertextSize && remaining < 8*time.Minute {
			t.Fatal("100 MiB transfer still has small-chunk timeout")
		}
		cancel()
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatal("transfer cancellation did not interrupt operation")
		}
	}
	parent, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ctx, finish, _ := store.operationContext(parent, maxCiphertextSize)
	defer finish()
	deadline, _ := ctx.Deadline()
	parentDeadline, _ := parent.Deadline()
	if deadline != parentDeadline {
		t.Fatal("large transfer extended caller deadline")
	}
}
