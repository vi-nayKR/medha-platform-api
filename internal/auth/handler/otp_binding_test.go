package handler_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/redis/go-redis/v9"
)

func TestVerifyOTPRequiresServerSidePhoneBinding(t *testing.T) {
	const phone = "+12025550101"
	const otherPhone = "+12025550102"
	for _, tc := range []struct {
		name       string
		stored     fakeOTPStore
		providedID string
		wantStatus int
		wantCalls  int
	}{
		{"matching challenge", fakeOTPStore{"mc:otp:" + phone: "bound-id"}, "bound-id", http.StatusOK, 1},
		{"server lookup without client ID", fakeOTPStore{"mc:otp:" + phone: "bound-id"}, "", http.StatusOK, 1},
		{"challenge belongs to another phone", fakeOTPStore{"mc:otp:" + otherPhone: "other-id"}, "other-id", http.StatusBadRequest, 0},
		{"substituted challenge with target binding", fakeOTPStore{"mc:otp:" + phone: "bound-id"}, "other-id", http.StatusBadRequest, 0},
		{"empty stored challenge", fakeOTPStore{"mc:otp:" + phone: ""}, "bound-id", http.StatusBadRequest, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := &mockMCClient{}
			h := createTestAuthHandlerV2(t, provider, tc.stored)
			query := url.Values{"phone": {phone}, "otp": {"000000"}}
			if tc.providedID != "" {
				query.Set("verification_id", tc.providedID)
			}
			w := httptest.NewRecorder()
			h.VerifyOTP(w, httptest.NewRequest(http.MethodGet, "/api/v2/auth/verify-otp?"+query.Encode(), nil))
			if w.Code != tc.wantStatus || provider.verifyCalls != tc.wantCalls {
				t.Fatalf("status/calls = %d/%d, want %d/%d", w.Code, provider.verifyCalls, tc.wantStatus, tc.wantCalls)
			}
			if tc.wantStatus == http.StatusOK {
				if _, exists := tc.stored["mc:otp:"+phone]; exists {
					t.Fatal("successful verification must consume the stored challenge")
				}
			}
		})
	}
}

func TestVerifyOTPTypedNilRedisFailsClosed(t *testing.T) {
	var client *redis.Client
	provider := &mockMCClient{}
	h := createTestAuthHandlerV2(t, provider, client)
	query := url.Values{"phone": {"+12025550101"}, "otp": {"000000"}, "verification_id": {"unbound-id"}}
	w := httptest.NewRecorder()
	h.VerifyOTP(w, httptest.NewRequest(http.MethodGet, "/api/v2/auth/verify-otp?"+query.Encode(), nil))
	if w.Code != http.StatusBadRequest || provider.verifyCalls != 0 {
		t.Fatalf("nil Redis must fail closed before provider call, got %d/%d", w.Code, provider.verifyCalls)
	}
}
