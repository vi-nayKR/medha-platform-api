package domain_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/medha/backend/internal/story/domain"
)

func TestParseAudienceRules_RejectsUnknownFields(t *testing.T) {
	_, err := domain.ParseAudienceRules(json.RawMessage(`{"roles":["pandit"],"exec":"rm -rf /"}`))
	if err == nil {
		t.Fatal("expected error for unknown field, got nil")
	}
}

func TestParseAudienceRules_Empty(t *testing.T) {
	rules, err := domain.ParseAudienceRules(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules.Roles) != 0 || len(rules.Cities) != 0 {
		t.Errorf("expected zero-value rules, got %+v", rules)
	}
}

func TestAudienceRules_Matches(t *testing.T) {
	viewer := domain.ViewerAttributes{
		UserID:    uuid.New(),
		Role:      "pandit",
		City:      "Bengaluru",
		State:     "Karnataka",
		CreatedAt: 1000,
	}

	tests := []struct {
		name  string
		rules domain.AudienceRules
		want  bool
	}{
		{"empty rules match everyone", domain.AudienceRules{}, true},
		{"matching role", domain.AudienceRules{Roles: []string{"pandit"}}, true},
		{"non-matching role", domain.AudienceRules{Roles: []string{"yajman"}}, false},
		{"matching city", domain.AudienceRules{Cities: []string{"Bengaluru"}}, true},
		{"non-matching city", domain.AudienceRules{Cities: []string{"Mysuru"}}, false},
		{"matching state", domain.AudienceRules{States: []string{"Karnataka"}}, true},
		{"non-matching state", domain.AudienceRules{States: []string{"Kerala"}}, false},
		{"created after — eligible", domain.AudienceRules{CreatedAfter: ptr(int64(500))}, true},
		{"created after — too old", domain.AudienceRules{CreatedAfter: ptr(int64(2000))}, false},
		{"created before — eligible", domain.AudienceRules{CreatedBefore: ptr(int64(2000))}, true},
		{"created before — too new", domain.AudienceRules{CreatedBefore: ptr(int64(500))}, false},
		{"explicit user match", domain.AudienceRules{ExplicitUserIDs: []uuid.UUID{viewer.UserID}}, true},
		{"explicit user no match", domain.AudienceRules{ExplicitUserIDs: []uuid.UUID{uuid.New()}}, false},
		{
			"multiple rules all satisfied",
			domain.AudienceRules{Roles: []string{"pandit"}, Cities: []string{"Bengaluru"}},
			true,
		},
		{
			"multiple rules, one fails",
			domain.AudienceRules{Roles: []string{"pandit"}, Cities: []string{"Mysuru"}},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.rules.Matches(viewer); got != tt.want {
				t.Errorf("Matches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }
