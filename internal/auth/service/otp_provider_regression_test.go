package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	mc "github.com/medha/backend/internal/platform/messagecentral"
)

// These identities previously selected a provider bypass. Construct them here
// solely as regression inputs; no fixed OTP or reviewer configuration is valid.
func TestV2_FormerReviewerPhonesRequireProvider(t *testing.T) {
	for _, digit := range []string{"9", "8"} {
		t.Run(digit, func(t *testing.T) {
			phone := "+91" + strings.Repeat(digit, 10)
			client := &mockMCClient{sendErr: mc.ErrProviderDown, verifyErr: mc.ErrWrongOTP}
			svc := createAuthServiceV2(t, newMockUserRepo(), newMockTokenRepo(), client)

			if _, err := svc.SendOTP(context.Background(), phone); !errors.Is(err, mc.ErrProviderDown) {
				t.Fatalf("SendOTP must propagate provider failure, got %v", err)
			}
			if _, err := svc.VerifyOTPWithID(context.Background(), phone, "test-provider-id", strings.Repeat("123", 2)); !errors.Is(err, mc.ErrWrongOTP) {
				t.Fatalf("VerifyOTPWithID must reject when provider rejects, got %v", err)
			}
		})
	}
}
