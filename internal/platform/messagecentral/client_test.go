package messagecentral_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	mc "github.com/medha/backend/internal/platform/messagecentral"
)

func TestVerifyOTPRequiresExplicitSuccessfulProviderOutcome(t *testing.T) {
	for _, tc := range []struct {
		name       string
		httpStatus int
		code       int
		outcome    string
		want       error
	}{
		{"completed", 200, 200, "VERIFICATION_COMPLETED", nil},
		{"explicit success", 200, 200, "SUCCESS", nil},
		{"normalized success", 200, 200, " verification_completed ", nil},
		{"empty outcome", 200, 200, "", mc.ErrProviderDown},
		{"unknown outcome", 200, 200, "PENDING", mc.ErrProviderDown},
		{"already verified outcome", 200, 200, "ALREADY_VERIFIED", mc.ErrAlreadyVerified},
		{"already verified code", 200, 703, "", mc.ErrAlreadyVerified},
		{"wrong code", 200, 702, "", mc.ErrWrongOTP},
		{"invalid outcome", 200, 200, "INVALID_OTP", mc.ErrWrongOTP},
		{"expired", 200, 200, "OTP_EXPIRED", mc.ErrOTPExpired},
		{"server error with success body", 503, 200, "SUCCESS", mc.ErrProviderDown},
		{"http rejection with success body", 403, 200, "SUCCESS", mc.ErrProviderDown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				var payload interface{}
				switch r.URL.Path {
				case "/auth/v1/authentication/token":
					payload = map[string]interface{}{"status": 200, "token": "local-test-token"}
				case "/verification/v3/validateOtp":
					w.WriteHeader(tc.httpStatus)
					payload = map[string]interface{}{"responseCode": tc.code, "data": map[string]string{"verificationStatus": tc.outcome}}
				default:
					t.Errorf("unexpected local request path: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				if err := json.NewEncoder(w).Encode(payload); err != nil {
					t.Errorf("encode fixture: %v", err)
				}
			}))
			defer server.Close()
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			client := mc.NewClient("fixture", "fixture", "demo@example.invalid", server.URL, nil, logger)
			err := client.VerifyOTP(context.Background(), "local-challenge", "000000")
			if !errors.Is(err, tc.want) {
				t.Fatalf("VerifyOTP error = %v, want %v", err, tc.want)
			}
		})
	}
}
