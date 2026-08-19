package ws

import "testing"

func TestOriginAllowedSeparatesEnvironments(t *testing.T) {
	tests := []struct {
		name   string
		origin string
		isDev  bool
		want   bool
	}{
		{"native client", "", false, true},
		{"production admin", "https://admin.medha.dev", false, true},
		{"development admin", "https://admin-dev.medha.dev", true, true},
		{"development rejected in production", "https://admin-dev.medha.dev", false, false},
		{"production rejected in development", "https://admin.medha.dev", true, false},
		{"unrelated subdomain rejected", "https://attacker.medha.dev", false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := originAllowed(tc.origin, tc.isDev); got != tc.want {
				t.Fatalf("originAllowed() = %v, want %v", got, tc.want)
			}
		})
	}
}
