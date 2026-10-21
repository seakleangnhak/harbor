//go:build telegram

package storage

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"

	"private-drive/internal/domain"
)

// No client.Run call: every RPC is intercepted and no socket/session is opened.
func telegramOfflineClient(invoke telegramTestInvoker) *telegram.Client {
	return telegram.NewClient(1, strings.Repeat("0", 32), telegram.Options{Middlewares: []telegram.Middleware{
		telegram.MiddlewareFunc(func(tg.Invoker) telegram.InvokeFunc { return telegram.InvokeFunc(invoke) }),
	}})
}

func TestTelegramGetRefreshesReferenceAndRejectsMissingMessage(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprintf("missing=%t", missing), func(t *testing.T) {
			payload := []byte("generated ciphertext fixture")
			resolutions, reads := 0, 0
			client := telegramOfflineClient(func(_ context.Context, input bin.Encoder, output bin.Decoder) error {
				switch request := input.(type) {
				case *tg.ChannelsGetMessagesRequest:
					resolutions++
					var messages []tg.MessageClass
					if !missing {
						messages = []tg.MessageClass{&tg.Message{ID: 7, Media: &tg.MessageMediaDocument{Document: &tg.Document{ID: 8, AccessHash: 9, FileReference: []byte{byte(resolutions)}, Size: int64(len(payload))}}}}
					}
					output.(*tg.MessagesMessagesBox).Messages = &tg.MessagesChannelMessages{Messages: messages}
				case *tg.UploadGetFileRequest:
					reads++
					location := request.Location.(*tg.InputDocumentFileLocation)
					if location.FileReference[0] == 1 {
						return tgerr.New(400, "FILE_REFERENCE_EXPIRED")
					}
					if resolutions != 2 || request.Offset != 0 || location.ID != 8 || location.AccessHash != 9 {
						t.Fatal("retry reused stale document or changed location")
					}
					output.(*tg.UploadFileBox).File = &tg.UploadFile{Type: &tg.StorageFileUnknown{}, Bytes: payload}
				default:
					t.Fatalf("unexpected offline RPC: %T", input)
				}
				return nil
			})
			store := &TelegramStore{ctx: context.Background(), client: client, channel: &tg.InputChannel{ChannelID: 12}}
			got, err := store.Get(context.Background(), "tg:12:7")
			if missing {
				if !errors.Is(err, domain.ErrNotFound) || got != nil || resolutions != 1 || reads != 0 {
					t.Fatalf("missing message retried or returned bytes: resolutions=%d reads=%d err=%v", resolutions, reads, err)
				}
			} else if err != nil || !bytes.Equal(got, payload) || resolutions != 2 || reads != 2 || store.Status().RetryCount != 1 {
				t.Fatalf("expired reference failed recovery: resolutions=%d reads=%d err=%v", resolutions, reads, err)
			}
		})
	}
}

func TestTelegramGetFloodWaitAndInterruptedReadHonorDeadline(t *testing.T) {
	for _, flood := range []bool{false, true} {
		t.Run(fmt.Sprintf("flood=%t", flood), func(t *testing.T) {
			payload := bytes.Repeat([]byte{0xa5}, (512<<10)+17)
			reads := 0
			client := telegramOfflineClient(func(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
				switch request := input.(type) {
				case *tg.ChannelsGetMessagesRequest:
					output.(*tg.MessagesMessagesBox).Messages = &tg.MessagesChannelMessages{Messages: []tg.MessageClass{&tg.Message{ID: 7, Media: &tg.MessageMediaDocument{Document: &tg.Document{ID: 8, AccessHash: 9, Size: int64(len(payload))}}}}}
				case *tg.UploadGetFileRequest:
					reads++
					if flood {
						return tgerr.New(420, "FLOOD_WAIT_60")
					}
					if request.Offset == 0 {
						output.(*tg.UploadFileBox).File = &tg.UploadFile{Type: &tg.StorageFileUnknown{}, Bytes: payload[:512<<10]}
						return nil
					}
					<-ctx.Done()
					return ctx.Err()
				default:
					t.Fatalf("unexpected offline RPC: %T", input)
				}
				return nil
			})
			store := &TelegramStore{ctx: context.Background(), client: client, channel: &tg.InputChannel{ChannelID: 12}}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			got, err := store.Get(ctx, "tg:12:7")
			wantReads := 2
			if flood {
				wantReads = 1
			}
			if !errors.Is(err, context.DeadlineExceeded) || got != nil || reads != wantReads {
				t.Fatalf("deadline leaked partial ciphertext or retried: reads=%d err=%v", reads, err)
			}
			if flood && store.Status().RetryCount != 1 {
				t.Fatal("interrupted flood wait was absent from retry telemetry")
			}
		})
	}
}

func TestTelegramDownloadFloodWaitReportsBeforeSleepAndCountsOnce(t *testing.T) {
	for _, cancelWait := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%t", cancelWait), func(t *testing.T) {
			payload := []byte("generated ciphertext fixture")
			document := &tg.Document{ID: 7, AccessHash: 8, Size: int64(len(payload)), DCID: 2}
			reads, retries := 0, 0
			api := tg.NewClient(telegramTestInvoker(func(_ context.Context, _ bin.Encoder, output bin.Decoder) error {
				reads++
				if reads == 1 {
					if cancelWait {
						return tgerr.New(420, "FLOOD_WAIT_60")
					}
					return tgerr.New(420, "FLOOD_WAIT_0")
				}
				output.(*tg.UploadFileBox).File = &tg.UploadFile{Type: &tg.StorageFileUnknown{}, Bytes: payload}
				return nil
			}))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			notified := make(chan struct{}, 1)
			finished := make(chan error, 1)
			go func() {
				got, err := downloadTelegramDocument(ctx, document, api, 2, nil, func() {
					retries++
					select {
					case notified <- struct{}{}:
					default:
					}
				})
				if err == nil && !bytes.Equal(got, payload) {
					err = domain.ErrIntegrity
				}
				finished <- err
			}()
			select {
			case <-notified:
			case <-time.After(time.Second):
				t.Fatal("flood wait was not reported before its delay")
			}
			if cancelWait {
				select {
				case err := <-finished:
					t.Fatalf("flood wait ended before cancellation: %v", err)
				default:
				}
				cancel()
			}
			select {
			case err := <-finished:
				if cancelWait && !errors.Is(err, context.Canceled) || !cancelWait && err != nil || retries != 1 {
					t.Fatalf("flood wait outcome or count incorrect: retries=%d err=%v", retries, err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("download did not finish after short wait or cancellation")
			}
		})
	}
}

func TestTelegramPutRetriesKeepMessageIdentity(t *testing.T) {
	payload := bytes.Repeat([]byte{0xa5}, (1<<20)+32)
	var firstID int64
	sends, parts, uploadedBytes := 0, 0, 0
	client := telegramOfflineClient(func(_ context.Context, input bin.Encoder, output bin.Decoder) error {
		switch request := input.(type) {
		case *tg.UploadSaveFilePartRequest:
			wantBytes := 512 << 10
			if parts%3 == 2 {
				wantBytes = 32
			}
			if request.FilePart != parts%3 || len(request.Bytes) != wantBytes {
				t.Fatal("production upload changed part size or part order")
			}
			parts++
			uploadedBytes += len(request.Bytes)
			output.(*tg.BoolBox).Bool = &tg.BoolTrue{}
		case *tg.MessagesSendMediaRequest:
			sends++
			if sends == 1 {
				firstID = request.RandomID
				return tgerr.New(500, "INTERNAL")
			}
			if request.RandomID != firstID || uploadedBytes != 2*len(payload) {
				t.Fatal("retry changed persistent message identity or lost parts")
			}
			output.(*tg.UpdatesBox).Updates = &tg.Updates{Updates: []tg.UpdateClass{&tg.UpdateMessageID{ID: 7, RandomID: firstID}}}
		default:
			t.Fatalf("unexpected offline RPC: %T", input)
		}
		return nil
	})
	store := &TelegramStore{ctx: context.Background(), client: client, channel: &tg.InputChannel{ChannelID: 12}}
	ref, err := store.Put(context.Background(), strings.Repeat("a", 32), payload)
	if err != nil || ref != "tg:12:7" || sends != 2 || parts != 6 || store.Status().RetryCount != 1 {
		t.Fatalf("send retry failed: sends=%d parts=%d err=%v", sends, parts, err)
	}
}

func TestTelegramPutCancellationDoesNotSendIncompleteDocument(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	parts := 0
	client := telegramOfflineClient(func(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
		request, ok := input.(*tg.UploadSaveFilePartRequest)
		if !ok {
			t.Fatalf("incomplete upload attempted publication: %T", input)
		}
		parts++
		if request.FilePart == 0 {
			output.(*tg.BoolBox).Bool = &tg.BoolTrue{}
			return nil
		}
		cancel()
		<-ctx.Done()
		return ctx.Err()
	})
	store := &TelegramStore{ctx: context.Background(), client: client, channel: &tg.InputChannel{ChannelID: 12}}
	ref, err := store.Put(ctx, strings.Repeat("a", 32), bytes.Repeat([]byte{0xa5}, (1<<20)+32))
	if !errors.Is(err, context.Canceled) || ref != "" || parts != 2 {
		t.Fatalf("canceled upload retried or returned published reference: parts=%d err=%v", parts, err)
	}
}

func TestTelegramUploaderPartSizesPreserveBytesAndChecksum(t *testing.T) {
	payload := bytes.Repeat([]byte{0xa5}, (4<<20)+32)
	digest := md5.Sum(payload) // Telegram small-file protocol checksum, not content security.
	for _, partSize := range []int{128 << 10, 512 << 10} {
		t.Run(fmt.Sprintf("%d", partSize), func(t *testing.T) {
			var reconstructed bytes.Buffer
			parts := 0
			api := tg.NewClient(telegramTestInvoker(func(_ context.Context, input bin.Encoder, output bin.Decoder) error {
				request, ok := input.(*tg.UploadSaveFilePartRequest)
				if !ok || request.FilePart != parts || len(request.Bytes) > partSize {
					t.Fatalf("unexpected or unordered file part: %T", input)
				}
				parts++
				reconstructed.Write(request.Bytes)
				output.(*tg.BoolBox).Bool = &tg.BoolTrue{}
				return nil
			}))
			file, err := uploader.NewUploader(api).WithThreads(2).WithPartSize(partSize).FromBytes(context.Background(), "fixture.harbor", payload)
			if err != nil {
				t.Fatal(err)
			}
			result, ok := file.(*tg.InputFile)
			if !ok || result.Parts != (len(payload)+partSize-1)/partSize || result.MD5Checksum != hex.EncodeToString(digest[:]) || !bytes.Equal(reconstructed.Bytes(), payload) {
				t.Fatal("part size changed file bytes, ordering or protocol checksum")
			}
		})
	}
}

func BenchmarkTelegramUploadPartSize(b *testing.B) {
	for _, size := range []int{(1 << 20) + 32, (4 << 20) + 32} {
		payload := bytes.Repeat([]byte{0xa5}, size)
		for _, partSize := range []int{128 << 10, 512 << 10} {
			for _, latency := range []time.Duration{0, time.Millisecond} {
				b.Run(fmt.Sprintf("bytes=%d/part=%d/rpc_delay=%s", size, partSize, latency), func(b *testing.B) {
					requests := 0
					api := tg.NewClient(telegramTestInvoker(func(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
						if _, ok := input.(*tg.UploadSaveFilePartRequest); !ok {
							b.Fatalf("unexpected RPC: %T", input)
						}
						requests++
						if latency != 0 {
							timer := time.NewTimer(latency)
							defer timer.Stop()
							select {
							case <-ctx.Done():
								return ctx.Err()
							case <-timer.C:
							}
						}
						output.(*tg.BoolBox).Bool = &tg.BoolTrue{}
						return nil
					}))
					b.SetBytes(int64(size))
					b.ReportAllocs()
					b.ResetTimer()
					for range b.N {
						if _, err := uploader.NewUploader(api).WithThreads(2).WithPartSize(partSize).FromBytes(context.Background(), "fixture.harbor", payload); err != nil {
							b.Fatal(err)
						}
					}
					b.ReportMetric(float64(requests)/float64(b.N), "RPCs/op")
				})
			}
		}
	}
}

func BenchmarkTelegramDownloadChunk(b *testing.B) {
	payload := bytes.Repeat([]byte{0xa5}, (4<<20)+32)
	document := &tg.Document{ID: 7, AccessHash: 8, Size: int64(len(payload)), DCID: 2}
	api := tg.NewClient(telegramTestInvoker(func(_ context.Context, input bin.Encoder, output bin.Decoder) error {
		request := input.(*tg.UploadGetFileRequest)
		end := min(int(request.Offset)+request.Limit, len(payload))
		output.(*tg.UploadFileBox).File = &tg.UploadFile{Type: &tg.StorageFileUnknown{}, Bytes: payload[int(request.Offset):end]}
		return nil
	}))
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		got, err := downloadTelegramDocument(context.Background(), document, api, 2, nil, func() { b.Fatal("unexpected retry") })
		if err != nil || len(got) != len(payload) {
			b.Fatalf("download failed: %v", err)
		}
	}
}
