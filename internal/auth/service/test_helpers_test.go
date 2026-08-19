package service_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/google/uuid"

	authdomain "github.com/medha/backend/internal/auth/domain"
	userdomain "github.com/medha/backend/internal/user/domain"
)

// createTempKeys generates temporary RSA keys for JWT signing in tests
func createTempKeys(t *testing.T) (string, string) {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}

	privDir := t.TempDir()
	privPath := filepath.Join(privDir, "private.pem")
	pubPath := filepath.Join(privDir, "public.pem")

	// Write private key
	privBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	privBlock := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privBytes,
	}
	if err := os.WriteFile(privPath, pem.EncodeToMemory(privBlock), 0600); err != nil {
		t.Fatalf("write private key: %v", err)
	}

	// Write public key
	pubBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	pubBlock := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubBytes,
	}
	if err := os.WriteFile(pubPath, pem.EncodeToMemory(pubBlock), 0644); err != nil {
		t.Fatalf("write public key: %v", err)
	}

	return privPath, pubPath
}

// --- Mock User Repository ---

type mockUserRepo struct {
	mu    sync.RWMutex
	users map[uuid.UUID]*userdomain.User
}

func newMockUserRepo() *mockUserRepo {
	return &mockUserRepo{
		users: make(map[uuid.UUID]*userdomain.User),
	}
}

func (m *mockUserRepo) Create(ctx context.Context, user *userdomain.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Mirror the real users.can_authenticate column's DEFAULT TRUE: tests
	// construct User{} literals without setting this new field, same as a
	// real INSERT that doesn't specify it explicitly.
	user.CanAuthenticate = true

	for _, u := range m.users {
		if user.Username != "" && u.Username == user.Username {
			return userdomain.ErrUsernameTaken
		}
	}
	m.users[user.ID] = user
	return nil
}

func (m *mockUserRepo) GetByID(ctx context.Context, id uuid.UUID) (*userdomain.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	user, ok := m.users[id]
	if !ok {
		return nil, userdomain.ErrUserNotFound
	}
	return user, nil
}

func (m *mockUserRepo) FindByPhone(ctx context.Context, phone string) (*userdomain.User, error) {
	return m.GetByPhone(ctx, phone)
}

func (m *mockUserRepo) GetByPhone(ctx context.Context, phone string) (*userdomain.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, u := range m.users {
		if u.Phone == phone {
			return u, nil
		}
	}
	return nil, userdomain.ErrUserNotFound
}

func (m *mockUserRepo) Update(ctx context.Context, user *userdomain.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[user.ID]; !ok {
		return userdomain.ErrUserNotFound
	}
	m.users[user.ID] = user
	return nil
}

func (m *mockUserRepo) SoftDelete(ctx context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.users, id)
	return nil
}

func (m *mockUserRepo) Delete(ctx context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[id]; !ok {
		return userdomain.ErrUserNotFound
	}
	delete(m.users, id)
	return nil
}

func (m *mockUserRepo) UpdatePhoneVerified(ctx context.Context, userID uuid.UUID, verified bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u, ok := m.users[userID]; ok {
		u.PhoneVerified = verified
	}
	return nil
}

func (m *mockUserRepo) UpdateProfileComplete(ctx context.Context, userID uuid.UUID, complete bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u, ok := m.users[userID]; ok {
		u.ProfileComplete = complete
	}
	return nil
}

func (m *mockUserRepo) SetupProfile(ctx context.Context, userID uuid.UUID, firstName, lastName, role, username, email string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u, ok := m.users[userID]; ok {
		u.FirstName = firstName
		u.LastName = lastName
		u.Role = authdomain.UserRole(role)
		u.Username = username
		u.Email = email
		u.ProfileComplete = true
	}
	return nil
}

func (m *mockUserRepo) CheckUsernameExists(ctx context.Context, username string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, u := range m.users {
		if u.Username == username {
			return true, nil
		}
	}
	return false, nil
}

func (m *mockUserRepo) UpdateLocation(ctx context.Context, userID uuid.UUID, latitude, longitude float64, city, state, pincode string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Mock doesn't store location info on User struct currently
	return nil
}

func (m *mockUserRepo) ListAll(ctx context.Context) ([]*userdomain.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	users := make([]*userdomain.User, 0, len(m.users))
	for _, u := range m.users {
		users = append(users, u)
	}
	return users, nil
}

func (m *mockUserRepo) CanViewContactInfo(ctx context.Context, viewerID, panditID uuid.UUID) (bool, error) {
	return true, nil
}

func (m *mockUserRepo) UpdatePremium(ctx context.Context, userID uuid.UUID, isPremium bool, premiumUntil *int64, premiumBadge string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u, ok := m.users[userID]; ok {
		u.IsPremium = isPremium
		u.PremiumUntil = premiumUntil
		u.PremiumBadge = premiumBadge
		return nil
	}
	return userdomain.ErrUserNotFound
}

func (m *mockUserRepo) GetByUsername(ctx context.Context, username string) (*userdomain.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, u := range m.users {
		if u.Username == username {
			return u, nil
		}
	}
	return nil, userdomain.ErrUserNotFound
}

func (m *mockUserRepo) GetBadgeTier(ctx context.Context, userID uuid.UUID) (string, error) {
	return "puja_praveen", nil
}

// --- Mock Token Repository ---

type mockTokenRepo struct {
	mu     sync.RWMutex
	tokens map[string]*authdomain.RefreshToken
}

func newMockTokenRepo() *mockTokenRepo {
	return &mockTokenRepo{
		tokens: make(map[string]*authdomain.RefreshToken),
	}
}

func (m *mockTokenRepo) StoreRefreshToken(ctx context.Context, rt *authdomain.RefreshToken) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokens[rt.TokenHash] = rt
	return nil
}

func (m *mockTokenRepo) GetRefreshTokenByHash(ctx context.Context, tokenHash string) (*authdomain.RefreshToken, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if t, ok := m.tokens[tokenHash]; ok {
		return t, nil
	}
	return nil, authdomain.ErrInvalidRefreshToken
}

func (m *mockTokenRepo) RevokeRefreshToken(ctx context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := int64(1234567890) // Dummy time
	for _, t := range m.tokens {
		if t.ID == id {
			t.RevokedAt = &now
			break
		}
	}
	return nil
}

func (m *mockTokenRepo) RevokeAllUserTokens(ctx context.Context, userID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := int64(1234567890) // Dummy time
	for _, t := range m.tokens {
		if t.UserID == userID {
			t.RevokedAt = &now
		}
	}
	return nil
}
