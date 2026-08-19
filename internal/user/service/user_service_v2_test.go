package service_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	authdomain "github.com/medha/backend/internal/auth/domain"
	userdomain "github.com/medha/backend/internal/user/domain"
	userservice "github.com/medha/backend/internal/user/service"
)

// --- Mock UserRepository and PanditProfileRepository ---

type mockUserRepo struct {
	userdomain.UserRepository
	users map[uuid.UUID]*userdomain.User
}

func (m *mockUserRepo) SetupProfile(_ context.Context, userID uuid.UUID, firstName, lastName, role, username, email string) error {
	u, ok := m.users[userID]
	if !ok {
		return userdomain.ErrUserNotFound
	}
	u.FirstName = firstName
	u.LastName = lastName
	u.Role = authdomain.UserRole(role)
	u.ProfileComplete = true
	return nil
}

func (m *mockUserRepo) GetByID(_ context.Context, userID uuid.UUID) (*userdomain.User, error) {
	u, ok := m.users[userID]
	if !ok {
		return nil, userdomain.ErrUserNotFound
	}
	return u, nil
}

func (m *mockUserRepo) GetByUsername(_ context.Context, username string) (*userdomain.User, error) {
	for _, u := range m.users {
		if u.Username == username {
			return u, nil
		}
	}
	return nil, userdomain.ErrUserNotFound
}

func (m *mockUserRepo) GetBadgeTier(_ context.Context, userID uuid.UUID) (string, error) {
	return "puja_praveen", nil
}

func (m *mockUserRepo) EnsurePanditProfile(_ context.Context, _ uuid.UUID) error {
	return nil
}

type mockPanditRepo struct {
	userdomain.PanditProfileRepository
}

func (m *mockPanditRepo) GetByUserID(_ context.Context, _ uuid.UUID) (*userdomain.PanditProfile, error) {
	return nil, userdomain.ErrUserNotFound
}

// --- Tests ---

func TestUserServiceV2_SetupProfile(t *testing.T) {
	userID := uuid.New()
	user := &userdomain.User{ID: userID, Phone: "+910000000002"}

	repo := &mockUserRepo{users: map[uuid.UUID]*userdomain.User{userID: user}}
	panditRepo := &mockPanditRepo{}
	svc := userservice.NewUserService(repo, panditRepo, nil, slog.Default())

	t.Run("Success", func(t *testing.T) {
		input := userservice.SetupProfileInput{
			FirstName: "Vinay",
			LastName:  "KR",
			Role:      "yajman",
		}

		resp, err := svc.SetupProfile(context.Background(), userID, input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if resp.FirstName != "Vinay" || resp.LastName != "KR" || resp.Role != "yajman" {
			t.Errorf("expected Vinay KR yajman, got %s %s %s", resp.FirstName, resp.LastName, resp.Role)
		}
		if !resp.IsProfileComplete {
			t.Error("expected IsProfileComplete=true")
		}
	})

	t.Run("UserNotFound", func(t *testing.T) {
		input := userservice.SetupProfileInput{FirstName: "A", LastName: "B", Role: "yajman"}
		_, err := svc.SetupProfile(context.Background(), uuid.New(), input)
		if err == nil || err.Error() != userdomain.ErrUserNotFound.Error() {
			t.Errorf("expected ErrUserNotFound, got %v", err)
		}
	})

	t.Run("RejectsReservedUsername", func(t *testing.T) {
		for _, reserved := range []string{"medhaapp", "MedhaApp", "medha", "medha-app", "admin", "support"} {
			input := userservice.SetupProfileInput{FirstName: "A", LastName: "B", Role: "yajman", Username: reserved}
			_, err := svc.SetupProfile(context.Background(), userID, input)
			if err == nil || err.Error() != userdomain.ErrUsernameTaken.Error() {
				t.Errorf("username %q: expected ErrUsernameTaken, got %v", reserved, err)
			}
		}
	})
}

func TestUserServiceV2_GetProfile(t *testing.T) {
	userID := uuid.New()
	user := &userdomain.User{
		ID:              userID,
		FirstName:       "Vinay",
		LastName:        "KR",
		Role:            authdomain.RoleYajman,
		ProfileComplete: true,
	}

	repo := &mockUserRepo{users: map[uuid.UUID]*userdomain.User{userID: user}}
	svc := userservice.NewUserService(repo, &mockPanditRepo{}, nil, slog.Default())

	t.Run("Success", func(t *testing.T) {
		resp, err := svc.GetProfile(context.Background(), userID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.ID != userID || resp.FirstName != "Vinay" {
			t.Errorf("expected userID Vinay, got %v %s", resp.ID, resp.FirstName)
		}
	})
}

func TestUserServiceV2_GetPublicProfileByUsername(t *testing.T) {
	userID := uuid.New()
	user := &userdomain.User{
		ID:              userID,
		FirstName:       "Ramesh",
		LastName:        "Sharma",
		Username:        "sharma_ji",
		Role:            authdomain.RolePandit,
		ProfileComplete: true,
	}

	repo := &mockUserRepo{users: map[uuid.UUID]*userdomain.User{userID: user}}
	svc := userservice.NewUserService(repo, &mockPanditRepo{}, nil, slog.Default())

	t.Run("Success Pandit Profile", func(t *testing.T) {
		resp, err := svc.GetPublicProfileByUsername(context.Background(), "sharma_ji", "https://dev.medha.dev")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Username != "sharma_ji" || resp.FirstName != "Ramesh" || resp.Role != "pandit" {
			t.Errorf("unexpected results: %+v", resp)
		}
		if resp.BadgeTier != "puja_praveen" || resp.BadgeLabel != "Puja Praveen" {
			t.Errorf("unexpected badge: %s %s", resp.BadgeTier, resp.BadgeLabel)
		}
		if resp.ShareURL != "https://dev.medha.dev/p/sharma_ji" {
			t.Errorf("unexpected share URL: %s", resp.ShareURL)
		}
	})

	t.Run("Success System Profile (medhaapp)", func(t *testing.T) {
		sysUserID := uuid.New()
		sysUser := &userdomain.User{
			ID:              sysUserID,
			FirstName:       "Medha",
			LastName:        "App",
			Username:        "medhaapp",
			Role:            authdomain.RoleSystem,
			ProfilePhotoURL: "https://medha.dev/favicon.svg",
			ProfileComplete: true,
		}
		sysRepo := &mockUserRepo{users: map[uuid.UUID]*userdomain.User{sysUserID: sysUser}}
		sysSvc := userservice.NewUserService(sysRepo, &mockPanditRepo{}, nil, slog.Default())

		resp, err := sysSvc.GetPublicProfileByUsername(context.Background(), "medhaapp", "https://dev.medha.dev")
		if err != nil {
			t.Fatalf("unexpected error fetching system user profile: %v", err)
		}
		if resp.Username != "medhaapp" || resp.FirstName != "Medha" || resp.Role != "system" {
			t.Errorf("unexpected system user response: %+v", resp)
		}
		if resp.ShareURL != "https://dev.medha.dev/p/medhaapp" {
			t.Errorf("unexpected share URL: %s", resp.ShareURL)
		}
	})

	t.Run("UserNotFound", func(t *testing.T) {
		_, err := svc.GetPublicProfileByUsername(context.Background(), "unknown_user", "https://dev.medha.dev")
		if err == nil || err.Error() != userdomain.ErrUserNotFound.Error() {
			t.Errorf("expected ErrUserNotFound, got %v", err)
		}
	})
}
