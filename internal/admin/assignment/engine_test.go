package assignment

import (
	"testing"
)

func TestScore(t *testing.T) {
	tests := []struct {
		name         string
		candidate    candidateRow
		ceremonyType string
		yajmanState  string
		wantMin      float64 // minimum expected total score
		wantMax      float64 // maximum expected total score
		wantExpert   bool
		wantAvail    bool
	}{
		{
			name: "perfect match: close, expert, available, same state, in radius, top badge",
			candidate: candidateRow{
				DistanceKM:              2.0,
				CeremonySpecializations: []string{"vivah", "shraadh"},
				AvailabilityStatus:      "available",
				ServiceRadiusKM:         25,
				BadgeTier:               "dharma_ratna",
				State:                   "Karnataka",
			},
			ceremonyType: "vivah",
			yajmanState:  "Karnataka",
			wantMin:      85.0,
			wantMax:      100.0,
			wantExpert:   true,
			wantAvail:    true,
		},
		{
			name: "far away, no expertise, unavailable",
			candidate: candidateRow{
				DistanceKM:              95.0,
				CeremonySpecializations: []string{"mundan"},
				AvailabilityStatus:      "unavailable",
				ServiceRadiusKM:         10,
				BadgeTier:               "pandit_ji",
				State:                   "Tamil Nadu",
			},
			ceremonyType: "vivah",
			yajmanState:  "Karnataka",
			wantMin:      0.0,
			wantMax:      20.0,
			wantExpert:   false,
			wantAvail:    false,
		},
		{
			name: "moderate distance, has expertise, available, different state",
			candidate: candidateRow{
				DistanceKM:              30.0,
				CeremonySpecializations: []string{"grihapravesh", "vivah"},
				AvailabilityStatus:      "available",
				ServiceRadiusKM:         50,
				BadgeTier:               "karma_kandi",
				State:                   "Maharashtra",
			},
			ceremonyType: "vivah",
			yajmanState:  "Karnataka",
			wantMin:      50.0,
			wantMax:      80.0,
			wantExpert:   true,
			wantAvail:    true,
		},
		{
			name: "empty ceremony type — expertise score should be 0",
			candidate: candidateRow{
				DistanceKM:              10.0,
				CeremonySpecializations: []string{"vivah"},
				AvailabilityStatus:      "available",
				ServiceRadiusKM:         25,
				BadgeTier:               "puja_praveen",
				State:                   "Karnataka",
			},
			ceremonyType: "",
			yajmanState:  "Karnataka",
			wantMin:      40.0,
			wantMax:      70.0,
			wantExpert:   false,
			wantAvail:    true,
		},
		{
			name: "beyond 100km — distance score should be 0",
			candidate: candidateRow{
				DistanceKM:              150.0,
				CeremonySpecializations: []string{"vivah"},
				AvailabilityStatus:      "available",
				ServiceRadiusKM:         200,
				BadgeTier:               "yajna_maharshi",
				State:                   "Karnataka",
			},
			ceremonyType: "vivah",
			yajmanState:  "Karnataka",
			wantMin:      30.0,
			wantMax:      70.0,
			wantExpert:   true,
			wantAvail:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := score(tt.candidate, tt.ceremonyType, tt.yajmanState)

			if result.TotalScore < tt.wantMin || result.TotalScore > tt.wantMax {
				t.Errorf("TotalScore = %.2f, want [%.2f, %.2f]", result.TotalScore, tt.wantMin, tt.wantMax)
			}
			if result.HasExpertise != tt.wantExpert {
				t.Errorf("HasExpertise = %v, want %v", result.HasExpertise, tt.wantExpert)
			}
			if result.IsAvailable != tt.wantAvail {
				t.Errorf("IsAvailable = %v, want %v", result.IsAvailable, tt.wantAvail)
			}
			if result.DistanceKM != tt.candidate.DistanceKM {
				t.Errorf("DistanceKM = %.2f, want %.2f", result.DistanceKM, tt.candidate.DistanceKM)
			}
			// Verify breakdown has all expected keys
			expectedKeys := []string{"distance", "expertise", "availability", "language", "service_radius", "badge"}
			for _, key := range expectedKeys {
				if _, ok := result.Breakdown[key]; !ok {
					t.Errorf("missing breakdown key: %s", key)
				}
			}
		})
	}
}

func TestBadgeTierScore(t *testing.T) {
	tests := []struct {
		tier string
		want float64
	}{
		{"dharma_ratna", 100},
		{"yajna_maharshi", 80},
		{"karma_kandi", 60},
		{"puja_praveen", 40},
		{"pandit_ji", 20},
		{"unknown", 10},
		{"", 10},
	}

	for _, tt := range tests {
		t.Run(tt.tier, func(t *testing.T) {
			got := badgeTierScore(tt.tier)
			if got != tt.want {
				t.Errorf("badgeTierScore(%q) = %.0f, want %.0f", tt.tier, got, tt.want)
			}
		})
	}
}
