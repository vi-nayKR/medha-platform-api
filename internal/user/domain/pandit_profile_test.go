package domain_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/medha/backend/internal/platform/epoch"
	"github.com/medha/backend/internal/user/domain"
)

// --- AvailabilityStatus tests ---

func TestAvailabilityStatus_IsValid(t *testing.T) {
	tests := []struct {
		name   string
		status domain.AvailabilityStatus
		want   bool
	}{
		{"available is valid", domain.AvailabilityAvailable, true},
		{"unavailable is valid", domain.AvailabilityUnavailable, true},
		{"empty string is invalid", domain.AvailabilityStatus(""), false},
		{"random string is invalid", domain.AvailabilityStatus("busy"), false},
		{"mixed case is invalid", domain.AvailabilityStatus("Available"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.status.IsValid()
			if got != tt.want {
				t.Errorf("AvailabilityStatus(%q).IsValid() = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestAvailabilityStatus_String(t *testing.T) {
	if domain.AvailabilityAvailable.String() != "available" {
		t.Errorf("expected 'available', got %q", domain.AvailabilityAvailable.String())
	}
	if domain.AvailabilityUnavailable.String() != "unavailable" {
		t.Errorf("expected 'unavailable', got %q", domain.AvailabilityUnavailable.String())
	}
}

// --- CeremonyType tests ---

func TestIsValidCeremonyType(t *testing.T) {
	tests := []struct {
		name string
		ct   string
		want bool
	}{
		{"shraadh is valid", domain.CeremonyShraadh, true},
		{"grihapravesh is valid", domain.CeremonyGrihapravesh, true},
		{"vivah is valid", domain.CeremonyVivah, true},
		{"satyanarayan is valid", domain.CeremonySatyanarayan, true},
		{"mundan is valid", domain.CeremonyMundan, true},
		{"antim_sanskar is valid", domain.CeremonyAntimSanskar, true},
		{"vastu_shanti is valid", domain.CeremonyVastuShanti, true},
		{"naamkaran is valid", domain.CeremonyNaamkaran, true},
		{"upanayana is valid", domain.CeremonyUpanayana, true},
		{"unknown type is invalid", "havan_kund", false},
		{"empty string is invalid", "", false},
		{"uppercase is invalid", "VIVAH", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := domain.IsValidCeremonyType(tt.ct)
			if got != tt.want {
				t.Errorf("IsValidCeremonyType(%q) = %v, want %v", tt.ct, got, tt.want)
			}
		})
	}
}

func TestValidCeremonyTypes_Count(t *testing.T) {
	types := domain.ValidCeremonyTypes()
	if len(types) != 9 {
		t.Errorf("expected 9 ceremony types, got %d", len(types))
	}
}

// --- PanditProfile entity tests ---

func TestPanditProfile_IsDeleted(t *testing.T) {
	t.Run("active profile", func(t *testing.T) {
		p := &domain.PanditProfile{
			ID:        uuid.New(),
			UserID:    uuid.New(),
			DeletedAt: nil,
		}
		if p.IsDeleted() {
			t.Error("expected IsDeleted() = false for active profile")
		}
	})

	t.Run("deleted profile", func(t *testing.T) {
		now := epoch.Now()
		p := &domain.PanditProfile{
			ID:        uuid.New(),
			UserID:    uuid.New(),
			DeletedAt: epoch.Pointer(now),
		}
		if !p.IsDeleted() {
			t.Error("expected IsDeleted() = true for deleted profile")
		}
	})
}

func TestPanditProfile_CreateWithAllFields(t *testing.T) {
	id := uuid.New()
	userID := uuid.New()
	now := time.Now()

	p := &domain.PanditProfile{
		ID:                      id,
		UserID:                  userID,
		Parampara:               "Shaiva",
		VedaAffiliation:         "Rigveda",
		CeremonySpecializations: []string{domain.CeremonyVivah, domain.CeremonyShraadh},
		Languages:               []string{"Hindi", "Sanskrit"},
		ServiceRadiusKM:         50,
		AvailabilityStatus:      domain.AvailabilityAvailable,
		About:                   "Experienced Pandit ji",
		CreatedAt:               epoch.FromTime(now),
		UpdatedAt:               epoch.FromTime(now),
		DeletedAt:               nil,
	}

	if p.ID != id {
		t.Errorf("expected ID %s, got %s", id, p.ID)
	}
	if p.UserID != userID {
		t.Errorf("expected UserID %s, got %s", userID, p.UserID)
	}
	if p.Parampara != "Shaiva" {
		t.Errorf("expected Parampara 'Shaiva', got %q", p.Parampara)
	}
	if p.VedaAffiliation != "Rigveda" {
		t.Errorf("expected VedaAffiliation 'Rigveda', got %q", p.VedaAffiliation)
	}
	if len(p.CeremonySpecializations) != 2 {
		t.Errorf("expected 2 ceremony specializations, got %d", len(p.CeremonySpecializations))
	}
	if len(p.Languages) != 2 {
		t.Errorf("expected 2 languages, got %d", len(p.Languages))
	}
	if p.ServiceRadiusKM != 50 {
		t.Errorf("expected ServiceRadiusKM 50, got %d", p.ServiceRadiusKM)
	}
	if !p.AvailabilityStatus.IsValid() {
		t.Error("expected valid AvailabilityStatus")
	}
	if p.About != "Experienced Pandit ji" {
		t.Errorf("expected About text, got %q", p.About)
	}
	if p.IsDeleted() {
		t.Error("expected non-deleted profile")
	}
}

// --- PanditFilters tests ---

func TestPanditFilters_EmptyFilters(t *testing.T) {
	f := domain.PanditFilters{}
	if f.Parampara != "" {
		t.Error("expected empty parampara by default")
	}
	if f.AvailableOnly {
		t.Error("expected AvailableOnly false by default")
	}
	if len(f.CeremonyTypes) != 0 {
		t.Error("expected empty CeremonyTypes by default")
	}
}

func TestPanditFilters_WithValues(t *testing.T) {
	f := domain.PanditFilters{
		CeremonyTypes: []string{domain.CeremonyVivah, domain.CeremonyShraadh},
		Parampara:     "Shaiva",
		AvailableOnly: true,
	}

	if len(f.CeremonyTypes) != 2 {
		t.Errorf("expected 2 ceremony types, got %d", len(f.CeremonyTypes))
	}
	if f.Parampara != "Shaiva" {
		t.Errorf("expected parampara 'Shaiva', got %q", f.Parampara)
	}
	if !f.AvailableOnly {
		t.Error("expected AvailableOnly true")
	}
}

// --- Sentinel error tests ---

func TestSentinelErrors(t *testing.T) {
	if domain.ErrPanditProfileNotFound == nil {
		t.Error("ErrPanditProfileNotFound should not be nil")
	}
	if domain.ErrPanditProfileAlreadyExists == nil {
		t.Error("ErrPanditProfileAlreadyExists should not be nil")
	}
	if domain.ErrPanditProfileNotFound.Error() != "pandit profile not found" {
		t.Errorf("unexpected error message: %s", domain.ErrPanditProfileNotFound.Error())
	}
	if domain.ErrPanditProfileAlreadyExists.Error() != "pandit profile already exists for this user" {
		t.Errorf("unexpected error message: %s", domain.ErrPanditProfileAlreadyExists.Error())
	}
}
