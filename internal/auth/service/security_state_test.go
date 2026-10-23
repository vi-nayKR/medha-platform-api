package service_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	authdomain "github.com/medha/backend/internal/auth/domain"
	authservice "github.com/medha/backend/internal/auth/service"
	userdomain "github.com/medha/backend/internal/user/domain"
)

func TestAccessTokenTracksAccountState(t *testing.T) {
	priv, pub := createTempKeys(t)
	key, _, err := authservice.LoadRSAKeys(priv, pub)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"active", "disabled", "deleted", "stale role", "no repository"} {
		t.Run(state, func(t *testing.T) {
			repo := newMockUserRepo()
			u := &userdomain.User{ID: uuid.New(), Role: authdomain.RoleYajman}
			if err := repo.Create(context.Background(), u); err != nil {
				t.Fatal(err)
			}
			claims := &authdomain.JWTClaims{RegisteredClaims: jwt.RegisteredClaims{Issuer: "medha-api", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}, UserID: u.ID, Role: u.Role.String()}
			token, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(key)
			if err != nil {
				t.Fatal(err)
			}
			var users userdomain.UserRepository = repo
			switch state {
			case "disabled":
				u.CanAuthenticate = false
			case "deleted":
				now := time.Now().Unix()
				u.DeletedAt = &now
			case "stale role":
				u.Role = authdomain.RolePandit
			case "no repository":
				users = nil
			}
			svc, err := authservice.NewAuthService(users, newMockTokenRepo(), priv, pub, 15, slog.Default())
			if err != nil {
				t.Fatal(err)
			}
			validated, err := svc.ValidateJWT(token)
			if state == "active" || state == "stale role" {
				if err != nil || validated.Role != u.Role.String() {
					t.Fatalf("current role not applied: %v", err)
				}
				return
			}
			if !errors.Is(err, authdomain.ErrInvalidToken) {
				t.Fatalf("stale JWT accepted: %v", err)
			}
		})
	}
}

func TestDevLoginIsOptInAndServiceGuarded(t *testing.T) {
	const phone = "+12025550101"
	for _, tc := range []struct {
		env, enabled, allowed string
		want                  bool
	}{
		{"production", "true", phone, false}, {"dev", "", phone, false},
		{"dev", "true", "+12025550102", false}, {"development", "true", phone, true},
	} {
		t.Run(tc.env+tc.enabled+tc.allowed, func(t *testing.T) {
			t.Setenv("ENVIRONMENT", tc.env)
			t.Setenv("ENABLE_DEV_LOGIN", tc.enabled)
			t.Setenv("DEV_PANDIT_PHONE", tc.allowed)
			t.Setenv("DEV_YAJMAN_PHONE", "")
			repo := newMockUserRepo()
			if err := repo.Create(context.Background(), &userdomain.User{ID: uuid.New(), Phone: phone, Role: authdomain.RoleYajman}); err != nil {
				t.Fatal(err)
			}
			svc := createAuthServiceV2(t, repo, newMockTokenRepo(), &mockMCClient{})
			_, err := svc.DevLoginByRole(context.Background(), phone)
			if tc.want && err != nil {
				t.Fatal(err)
			}
			if !tc.want && !errors.Is(err, authdomain.ErrAccountCannotAuthenticate) {
				t.Fatalf("dev policy bypassed: %v", err)
			}
		})
	}
}
