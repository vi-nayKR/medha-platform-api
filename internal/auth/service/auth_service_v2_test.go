package service_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	authdomain "github.com/medha/backend/internal/auth/domain"
	authservice "github.com/medha/backend/internal/auth/service"
	mc "github.com/medha/backend/internal/platform/messagecentral"
	userdomain "github.com/medha/backend/internal/user/domain"
)

// --- Mock MessageCentral client ---

type mockMCClient struct {
	sendErr        error
	verifyErr      error
	verificationID string
}

func (m *mockMCClient) SendOTP(_ context.Context, phone, countryCode string) (string, error) {
	if m.sendErr != nil {
		return "", m.sendErr
	}
	if m.verificationID == "" {
		return "mock-verification-id", nil
	}
	return m.verificationID, nil
}

func (m *mockMCClient) VerifyOTP(_ context.Context, verificationID, otp string) error {
	return m.verifyErr
}

// --- Mock user repository (same type from auth_service_test.go — reusing via package) ---

// createAuthServiceV2 builds a test AuthServiceV2 with in-memory mocks.
func createAuthServiceV2(t *testing.T, userRepo userdomain.UserRepository, tokenRepo authdomain.TokenRepository, mcClient mc.Client) *authservice.AuthServiceV2 {
	t.Helper()
	privPath, pubPath := createTempKeys(t)
	privKey, pubKey, err := authservice.LoadRSAKeys(privPath, pubPath)
	if err != nil {
		t.Fatalf("load rsa keys: %v", err)
	}

	svc := authservice.NewAuthServiceV2(
		userRepo,
		tokenRepo,
		mcClient,
		"",
		"",
		15,
		slog.Default(),
	)
	svc.SetTestKeys(privKey, pubKey)
	return svc
}

// --- SendOTP tests ---

func TestV2_SendOTP_ValidPhone(t *testing.T) {
	mcClient := &mockMCClient{}
	svc := createAuthServiceV2(t, newMockUserRepo(), newMockTokenRepo(), mcClient)

	result, err := svc.SendOTP(context.Background(), "+910000000002")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if result.VerificationID == "" {
		t.Error("expected non-empty verificationID")
	}
	if result.ExpiresInSeconds != 600 {
		t.Errorf("expected ExpiresInSeconds=600, got %d", result.ExpiresInSeconds)
	}
}

func TestV2_SendOTP_InvalidPhone(t *testing.T) {
	mcClient := &mockMCClient{}
	svc := createAuthServiceV2(t, newMockUserRepo(), newMockTokenRepo(), mcClient)

	cases := []string{"9876543210", "invalid", "+1", ""}
	for _, phone := range cases {
		t.Run(phone, func(t *testing.T) {
			_, err := svc.SendOTP(context.Background(), phone)
			if err == nil {
				t.Errorf("expected error for phone=%q, got nil", phone)
			}
		})
	}
}

func TestV2_SendOTP_RateLimitExceeded(t *testing.T) {
	mcClient := &mockMCClient{sendErr: mc.ErrRateLimitExceeded}
	svc := createAuthServiceV2(t, newMockUserRepo(), newMockTokenRepo(), mcClient)

	_, err := svc.SendOTP(context.Background(), "+910000000002")
	if !errors.Is(err, mc.ErrRateLimitExceeded) {
		t.Errorf("expected ErrRateLimitExceeded, got: %v", err)
	}
}

func TestV2_SendOTP_ProviderDown(t *testing.T) {
	mcClient := &mockMCClient{sendErr: mc.ErrProviderDown}
	svc := createAuthServiceV2(t, newMockUserRepo(), newMockTokenRepo(), mcClient)

	_, err := svc.SendOTP(context.Background(), "+910000000002")
	if !errors.Is(err, mc.ErrProviderDown) {
		t.Errorf("expected ErrProviderDown, got: %v", err)
	}
}

// --- VerifyOTP tests ---

func TestV2_VerifyOTP_NewUser(t *testing.T) {
	mcClient := &mockMCClient{verifyErr: nil}
	userRepo := newMockUserRepo()
	tokenRepo := newMockTokenRepo()
	svc := createAuthServiceV2(t, userRepo, tokenRepo, mcClient)

	resp, err := svc.VerifyOTPWithID(context.Background(), "+910000000002", "mock-verif-id", "123456")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !resp.IsNewUser {
		t.Error("expected IsNewUser=true for first-time phone login")
	}
	if resp.IsProfileComplete {
		t.Error("expected IsProfileComplete=false for new user")
	}
	if resp.IsPhoneVerified != true {
		t.Error("expected IsPhoneVerified=true")
	}
	if resp.AccessToken == "" {
		t.Error("expected non-empty access token")
	}
}

// TestV2_VerifyOTP_SystemAccountCannotAuthenticate is the core regression
// test for the auth hardening fix: a platform system account (can_authenticate
// = false, e.g. the Medha App official user) must never receive a token,
// even if something manages to match it by phone.
func TestV2_VerifyOTP_SystemAccountCannotAuthenticate(t *testing.T) {
	mcClient := &mockMCClient{verifyErr: nil}
	userRepo := newMockUserRepo()

	systemUser := &userdomain.User{
		ID:              uuid.New(),
		Phone:           "+910000000000",
		AuthProvider:    "system",
		Role:            authdomain.RoleSystem,
		CanAuthenticate: false,
	}
	if err := userRepo.Create(context.Background(), systemUser); err != nil {
		t.Fatalf("seed system user: %v", err)
	}
	// mockUserRepo.Create defaults CanAuthenticate to true (mirroring the DB
	// column default) — explicitly override after seeding, the same way a
	// real hardened stub row does via migration 00047.
	systemUser.CanAuthenticate = false

	tokenRepo := newMockTokenRepo()
	svc := createAuthServiceV2(t, userRepo, tokenRepo, mcClient)

	_, err := svc.VerifyOTPWithID(context.Background(), "+910000000000", "mock-verif-id", "123456")
	if !errors.Is(err, authdomain.ErrAccountCannotAuthenticate) {
		t.Fatalf("expected ErrAccountCannotAuthenticate, got: %v", err)
	}
}

func TestV2_VerifyOTP_WrongOTP(t *testing.T) {
	mcClient := &mockMCClient{verifyErr: mc.ErrWrongOTP}
	svc := createAuthServiceV2(t, newMockUserRepo(), newMockTokenRepo(), mcClient)

	_, err := svc.VerifyOTPWithID(context.Background(), "+910000000002", "verif-id", "999999")
	if !errors.Is(err, mc.ErrWrongOTP) {
		t.Errorf("expected ErrWrongOTP, got: %v", err)
	}
}

func TestV2_VerifyOTP_OTPExpired(t *testing.T) {
	mcClient := &mockMCClient{verifyErr: mc.ErrOTPExpired}
	svc := createAuthServiceV2(t, newMockUserRepo(), newMockTokenRepo(), mcClient)

	_, err := svc.VerifyOTPWithID(context.Background(), "+910000000002", "verif-id", "123456")
	if !errors.Is(err, mc.ErrOTPExpired) {
		t.Errorf("expected ErrOTPExpired, got: %v", err)
	}
}

func TestV2_VerifyOTP_ReturningUser(t *testing.T) {
	mcClient := &mockMCClient{verifyErr: nil}
	userRepo := newMockUserRepo()

	// Pre-seed an existing user
	existingUser := &userdomain.User{
		ID:              uuid.New(),
		Phone:           "+910000000002",
		PhoneVerified:   false,
		ProfileComplete: false,
	}
	_ = userRepo.Create(context.Background(), existingUser)

	tokenRepo := newMockTokenRepo()
	svc := createAuthServiceV2(t, userRepo, tokenRepo, mcClient)

	resp, err := svc.VerifyOTPWithID(context.Background(), "+910000000002", "mock-verif-id", "123456")
	if err != nil {
		t.Fatalf("expected no error for returning user, got: %v", err)
	}
	if resp.IsNewUser {
		t.Error("expected IsNewUser=false for returning user")
	}
}

func TestV2_VerifyOTP_ReturningUser_WithCompleteProfile(t *testing.T) {
	mcClient := &mockMCClient{verifyErr: nil}
	userRepo := newMockUserRepo()

	existingUser := &userdomain.User{
		ID:              uuid.New(),
		Phone:           "+910000000002",
		PhoneVerified:   true,
		FirstName:       "Vinay",
		LastName:        "Kumar",
		Role:            authdomain.RoleYajman,
		ProfileComplete: true,
	}
	_ = userRepo.Create(context.Background(), existingUser)

	svc := createAuthServiceV2(t, userRepo, newMockTokenRepo(), mcClient)

	resp, err := svc.VerifyOTPWithID(context.Background(), "+910000000002", "mock-verif-id", "123456")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.IsProfileComplete {
		t.Error("expected IsProfileComplete=true for user with complete profile")
	}
}
