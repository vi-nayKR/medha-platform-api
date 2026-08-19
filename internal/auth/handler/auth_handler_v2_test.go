package handler_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	authdomain "github.com/medha/backend/internal/auth/domain"
	authhandler "github.com/medha/backend/internal/auth/handler"
	authsvc "github.com/medha/backend/internal/auth/service"
	mc "github.com/medha/backend/internal/platform/messagecentral"
	userdomain "github.com/medha/backend/internal/user/domain"
)

// --- Mocks ---

type mockUserRepo struct {
	userdomain.UserRepository
	users   map[uuid.UUID]*userdomain.User
	byPhone map[string]*userdomain.User
}

func newMockUserRepo() *mockUserRepo {
	return &mockUserRepo{
		users:   make(map[uuid.UUID]*userdomain.User),
		byPhone: make(map[string]*userdomain.User),
	}
}

func (m *mockUserRepo) Create(_ context.Context, user *userdomain.User) error {
	// Mirror the real users.can_authenticate column's DEFAULT TRUE.
	user.CanAuthenticate = true
	m.users[user.ID] = user
	if user.Phone != "" {
		m.byPhone[user.Phone] = user
	}
	return nil
}

func (m *mockUserRepo) GetByPhone(_ context.Context, phone string) (*userdomain.User, error) {
	u, ok := m.byPhone[phone]
	if !ok {
		return nil, userdomain.ErrUserNotFound
	}
	return u, nil
}

func (m *mockUserRepo) UpdatePhoneVerified(_ context.Context, id uuid.UUID, verified bool) error {
	if u, ok := m.users[id]; ok {
		u.PhoneVerified = verified
		return nil
	}
	return userdomain.ErrUserNotFound
}

type mockTokenRepo struct {
	authdomain.TokenRepository
}

func (m *mockTokenRepo) StoreRefreshToken(_ context.Context, _ *authdomain.RefreshToken) error {
	return nil
}

type mockMCClient struct {
	mc.Client
	verificationID string
	sendErr        error
	verifyErr      error
}

func (m *mockMCClient) SendOTP(_ context.Context, _, _ string) (string, error) {
	return m.verificationID, m.sendErr
}

func (m *mockMCClient) VerifyOTP(_ context.Context, _, _ string) error {
	return m.verifyErr
}

// --- Helpers ---

func createTestAuthHandlerV2(t *testing.T, mcClient mc.Client, rdb *redis.Client) *authhandler.AuthHandlerV2 {
	t.Helper()
	priv, pub := generateRSAKeys(t)

	// Create temp key files for service initialization if needed,
	// but AuthServiceV2 takes keys directly.
	repo := newMockUserRepo()
	tokenRepo := &mockTokenRepo{}

	svc := authsvc.NewAuthServiceV2(
		repo,
		tokenRepo,
		mcClient,
		"",
		"",
		15,
		slog.Default(),
	)
	svc.SetTestKeys(priv, pub)

	return authhandler.NewAuthHandlerV2(svc, rdb, slog.Default(), "+910000000002", "+910000000003")
}

func generateRSAKeys(t *testing.T) (*rsa.PrivateKey, *rsa.PublicKey) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate rsa key: %v", err)
	}
	return priv, &priv.PublicKey
}

// --- Tests ---

func TestAuthHandlerV2_SendOTP(t *testing.T) {
	mcMock := &mockMCClient{verificationID: "test-v-id"}
	h := createTestAuthHandlerV2(t, mcMock, nil)

	t.Run("Success", func(t *testing.T) {
		reqBody, _ := json.Marshal(authhandler.SendOTPRequest{Phone: "+910000000002"})
		req := httptest.NewRequest(http.MethodPost, "/api/v2/auth/send-otp", bytes.NewBuffer(reqBody))
		w := httptest.NewRecorder()

		h.SendOTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}

		var resp struct {
			Data authdomain.SendOTPResponse `json:"data"`
		}
		json.NewDecoder(w.Body).Decode(&resp)
		if resp.Data.VerificationID != "test-v-id" {
			t.Errorf("expected v-id test-v-id, got %s", resp.Data.VerificationID)
		}
	})

	t.Run("InvalidPhone", func(t *testing.T) {
		reqBody, _ := json.Marshal(authhandler.SendOTPRequest{Phone: "12345"})
		req := httptest.NewRequest(http.MethodPost, "/api/v2/auth/send-otp", bytes.NewBuffer(reqBody))
		w := httptest.NewRecorder()

		h.SendOTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})
}

func TestAuthHandlerV2_VerifyOTP(t *testing.T) {
	// For VerifyOTP we need Redis because the handler fetches verificationId from it.
	// Since we don't want a real Redis in unit tests, we can use a mock or miniredis.
	// For now, let's try to pass a mock redis client if possible, or just test the failure path without it.
	// Actually, let's skip the redis check if redis is nil in the handler and see what happens.

	t.Run("NoOTPSent", func(t *testing.T) {
		h := createTestAuthHandlerV2(t, &mockMCClient{}, nil)
		req := httptest.NewRequest(http.MethodGet, "/api/v2/auth/verify-otp?phone=%2B910000000002&otp=123456", nil)
		w := httptest.NewRecorder()

		h.VerifyOTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 when no OTP sent, got %d", w.Code)
		}
	})

	t.Run("UsesVerificationIDFromQueryWhenProvided", func(t *testing.T) {
		h := createTestAuthHandlerV2(t, &mockMCClient{}, nil)
		req := httptest.NewRequest(
			http.MethodGet,
			"/api/v2/auth/verify-otp?phone=%2B910000000002&otp=123456&verification_id=test-v-id",
			nil,
		)
		w := httptest.NewRecorder()

		h.VerifyOTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200 when verification_id is provided, got %d", w.Code)
		}
	})
}
