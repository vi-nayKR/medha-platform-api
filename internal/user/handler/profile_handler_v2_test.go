package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authdomain "github.com/medha/backend/internal/auth/domain"
	"github.com/medha/backend/internal/config"
	"github.com/medha/backend/internal/server/middleware"
	userdomain "github.com/medha/backend/internal/user/domain"
	userhandler "github.com/medha/backend/internal/user/handler"
	userservice "github.com/medha/backend/internal/user/service"
)

// --- Mock UserService and related repositories ---

type mockUserRepoForHandler struct {
	userdomain.UserRepository
	users map[uuid.UUID]*userdomain.User
}

func (m *mockUserRepoForHandler) SetupProfile(_ context.Context, userID uuid.UUID, firstName, lastName, role, username, email string) error {
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

func (m *mockUserRepoForHandler) GetByID(_ context.Context, userID uuid.UUID) (*userdomain.User, error) {
	u, ok := m.users[userID]
	if !ok {
		return nil, userdomain.ErrUserNotFound
	}
	return u, nil
}

func (m *mockUserRepoForHandler) GetByUsername(_ context.Context, username string) (*userdomain.User, error) {
	for _, u := range m.users {
		if u.Username == username {
			return u, nil
		}
	}
	return nil, userdomain.ErrUserNotFound
}

func (m *mockUserRepoForHandler) GetBadgeTier(_ context.Context, userID uuid.UUID) (string, error) {
	return "puja_praveen", nil
}

func (m *mockUserRepoForHandler) EnsurePanditProfile(_ context.Context, _ uuid.UUID) error {
	return nil
}

type mockPanditRepoForHandler struct {
	userdomain.PanditProfileRepository
	profiles map[uuid.UUID]*userdomain.PanditProfile
}

func (m *mockPanditRepoForHandler) Create(_ context.Context, profile *userdomain.PanditProfile) error {
	if m.profiles == nil {
		m.profiles = make(map[uuid.UUID]*userdomain.PanditProfile)
	}
	m.profiles[profile.UserID] = profile
	return nil
}

func (m *mockPanditRepoForHandler) Update(_ context.Context, profile *userdomain.PanditProfile) error {
	if m.profiles == nil {
		m.profiles = make(map[uuid.UUID]*userdomain.PanditProfile)
	}
	m.profiles[profile.UserID] = profile
	return nil
}

func (m *mockPanditRepoForHandler) GetByUserID(_ context.Context, userID uuid.UUID) (*userdomain.PanditProfile, error) {
	if m.profiles == nil {
		return nil, userdomain.ErrPanditProfileNotFound
	}
	p, ok := m.profiles[userID]
	if !ok {
		return nil, userdomain.ErrPanditProfileNotFound
	}
	return p, nil
}

func (m *mockPanditRepoForHandler) VerifyCeremonySlugExists(_ context.Context, slug string) (bool, error) {
	return true, nil
}

func createTestProfileHandlerV2(t *testing.T, users map[uuid.UUID]*userdomain.User) *userhandler.ProfileHandlerV2 {
	t.Helper()
	repo := &mockUserRepoForHandler{users: users}
	panditRepo := &mockPanditRepoForHandler{profiles: make(map[uuid.UUID]*userdomain.PanditProfile)}
	svc := userservice.NewUserService(repo, panditRepo, nil, slog.Default())
	return userhandler.NewProfileHandlerV2(svc, &config.Config{}, slog.Default())
}

// --- Tests ---

func TestProfileHandlerV2_SetupProfile(t *testing.T) {
	userID := uuid.New()
	user := &userdomain.User{ID: userID, Phone: "+910000000002"}
	h := createTestProfileHandlerV2(t, map[uuid.UUID]*userdomain.User{userID: user})

	t.Run("Success", func(t *testing.T) {
		reqBody, _ := json.Marshal(userhandler.SetupProfileRequestDTO{
			FirstName: "Vinay",
			LastName:  "KR",
			Role:      "yajman",
		})

		req := httptest.NewRequest(http.MethodPost, "/api/v2/user/profile", bytes.NewBuffer(reqBody))
		// Inject userID into context manually
		ctx := context.WithValue(req.Context(), middleware.ContextKeyUserID, userID)
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()
		h.SetupProfile(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}

		var resp struct {
			Data userservice.ProfileResponse `json:"data"`
		}
		json.NewDecoder(w.Body).Decode(&resp)
		if resp.Data.FirstName != "Vinay" {
			t.Errorf("expected Vinay, got %s", resp.Data.FirstName)
		}
	})

	t.Run("ValidationFailure", func(t *testing.T) {
		reqBody, _ := json.Marshal(userhandler.SetupProfileRequestDTO{FirstName: "", LastName: "", Role: "foo"})
		req := httptest.NewRequest(http.MethodPost, "/api/v2/user/profile", bytes.NewBuffer(reqBody))
		ctx := context.WithValue(req.Context(), middleware.ContextKeyUserID, userID)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		h.SetupProfile(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for bad validation, got %d", w.Code)
		}
	})

	t.Run("Unauthorized", func(t *testing.T) {
		reqBody, _ := json.Marshal(userhandler.SetupProfileRequestDTO{FirstName: "A", LastName: "B", Role: "yajman"})
		req := httptest.NewRequest(http.MethodPost, "/api/v2/user/profile", bytes.NewBuffer(reqBody))
		// NO Context — should fail
		w := httptest.NewRecorder()

		h.SetupProfile(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})
}

func TestProfileHandlerV2_GetProfile(t *testing.T) {
	userID := uuid.New()
	user := &userdomain.User{
		ID:              userID,
		FirstName:       "Vinay",
		LastName:        "KR",
		Role:            authdomain.RoleYajman,
		ProfileComplete: true,
	}
	h := createTestProfileHandlerV2(t, map[uuid.UUID]*userdomain.User{userID: user})

	t.Run("Success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v2/user/profile", nil)
		ctx := context.WithValue(req.Context(), middleware.ContextKeyUserID, userID)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		h.GetProfile(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})
}

func TestProfileHandlerV2_GetPublicProfile(t *testing.T) {
	userID := uuid.New()
	user := &userdomain.User{
		ID:              userID,
		FirstName:       "Ramesh",
		LastName:        "Sharma",
		Username:        "sharma_ji",
		Role:            authdomain.RolePandit,
		ProfileComplete: true,
	}
	h := createTestProfileHandlerV2(t, map[uuid.UUID]*userdomain.User{userID: user})

	t.Run("Success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v2/public/profile/sharma_ji", nil)
		
		// Set up Chi URL param mock
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("username", "sharma_ji")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		h.GetPublicProfile(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}

		var resp struct {
			Data userservice.PublicProfileResponse `json:"data"`
		}
		json.NewDecoder(w.Body).Decode(&resp)
		if resp.Data.Username != "sharma_ji" || resp.Data.FirstName != "Ramesh" {
			t.Errorf("expected sharma_ji Ramesh, got %s %s", resp.Data.Username, resp.Data.FirstName)
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v2/public/profile/unknown", nil)
		
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("username", "unknown")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		h.GetPublicProfile(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", w.Code)
		}
	})
}

func TestProfileHandlerV2_RenderPublicProfilePage(t *testing.T) {
	userID := uuid.New()
	user := &userdomain.User{
		ID:              userID,
		FirstName:       "Ramesh",
		LastName:        "Sharma",
		Username:        "sharma_ji",
		Role:            authdomain.RolePandit,
		ProfileComplete: true,
	}
	h := createTestProfileHandlerV2(t, map[uuid.UUID]*userdomain.User{userID: user})

	t.Run("Success HTML", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/p/sharma_ji", nil)
		
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("username", "sharma_ji")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		h.RenderPublicProfilePage(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}

		body := w.Body.String()
		if !strings.Contains(body, "Ramesh Sharma") {
			t.Error("expected body to contain name 'Ramesh Sharma'")
		}
		if !strings.Contains(body, "@sharma_ji") {
			t.Error("expected body to contain username '@sharma_ji'")
		}
		if !strings.Contains(body, "medha://profile/sharma_ji") {
			t.Error("expected body to contain deep link")
		}
	})

	t.Run("NotFound HTML", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/p/unknown", nil)
		
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("username", "unknown")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		h.RenderPublicProfilePage(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", w.Code)
		}

		body := w.Body.String()
		if !strings.Contains(body, "Profile Not Found") {
			t.Error("expected body to contain error message")
		}
	})
}

func TestProfileHandlerV2_PanditProfile(t *testing.T) {
	userID := uuid.New()
	user := &userdomain.User{
		ID:              userID,
		FirstName:       "Ramesh",
		LastName:        "Sharma",
		Role:            authdomain.RolePandit,
		ProfileComplete: true,
	}
	h := createTestProfileHandlerV2(t, map[uuid.UUID]*userdomain.User{userID: user})

	t.Run("Setup Pandit Profile Success", func(t *testing.T) {
		reqBody, _ := json.Marshal(userhandler.SetupPanditProfileRequestDTO{
			Parampara:               "Smartha",
			VedaAffiliation:         "Rigveda",
			CeremonySpecializations: []string{"vivah"},
			Languages:               []string{"Hindi"},
			ServiceRadiusKM:         30,
			AvailabilityStatus:      "available",
			About:                   "Experienced pandit.",
		})

		req := httptest.NewRequest(http.MethodPut, "/api/v2/pandit/profile", bytes.NewBuffer(reqBody))
		ctx := context.WithValue(req.Context(), middleware.ContextKeyUserID, userID)
		ctx = context.WithValue(ctx, middleware.ContextKeyUserRole, "pandit")
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()
		h.SetupPanditProfile(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})

	t.Run("Get Pandit Profile Success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v2/pandit/profile", nil)
		ctx := context.WithValue(req.Context(), middleware.ContextKeyUserID, userID)
		ctx = context.WithValue(ctx, middleware.ContextKeyUserRole, "pandit")
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()
		h.GetPanditProfile(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})
}
