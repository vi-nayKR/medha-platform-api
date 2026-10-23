package messagecentral_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	mc "github.com/medha/backend/internal/platform/messagecentral"
	"github.com/redis/go-redis/v9"
)

type failedBindingWrite struct{}

func (failedBindingWrite) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		return next(ctx, network, address)
	}
}

func (failedBindingWrite) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func (failedBindingWrite) ProcessHook(_ redis.ProcessHook) redis.ProcessHook {
	return func(_ context.Context, cmd redis.Cmder) error {
		if cmd.Name() == "get" {
			cmd.(*redis.StringCmd).SetVal("0") // zero sends / a synthetic cached token
			return nil
		}
		return errors.New("fixture: binding write unavailable")
	}
}

func TestSendOTPDoesNotReportSuccessWhenBindingWriteFails(t *testing.T) {
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if _, err := io.WriteString(w, `{"responseCode":200,"data":{"verificationId":"fixture-challenge"}}`); err != nil {
			t.Errorf("write fixture response: %v", err)
		}
	}))
	defer provider.Close()
	store := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close fixture store: %v", err)
		}
	})
	store.AddHook(failedBindingWrite{})
	client := mc.NewClient("fixture", "fixture", "demo@example.invalid", provider.URL, store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	id, err := client.SendOTP(context.Background(), "+12025550101", "1")
	if id != "" || !errors.Is(err, mc.ErrProviderDown) || calls.Load() != 1 {
		t.Fatalf("unpersisted challenge reported as usable: calls=%d, err=%v", calls.Load(), err)
	}
}

func TestSendOTPRequiresBindingStorageBeforeContactingProvider(t *testing.T) {
	client := mc.NewClient("fixture", "fixture", "demo@example.invalid", "http://127.0.0.1:1", nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	id, err := client.SendOTP(context.Background(), "+12025550101", "1")
	if id != "" || !errors.Is(err, mc.ErrProviderDown) {
		t.Fatalf("OTP send succeeded without a usable server binding: %v", err)
	}
}
