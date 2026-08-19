package handler

import (
	"testing"
)

func TestNormalizeCeremonyType(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Specific custom mappings
		{"shraddha", "shraadh"},
		{"griha-pravesh", "grihapravesh"},
		{"satyanarayan-puja", "satyanarayan"},
		{"vastu-shanti-puja", "vastu_shanti"},
		{"namakarana", "naamkaran"},
		// Hyphen to underscore replacements
		{"akshaya-tritiya", "akshaya_tritiya"},
		{"bhagwat-katha", "bhagwat_katha"},
		{"bhoomi-puja", "bhoomi_puja"},
		{"chandi-homa", "chandi_homa"},
		{"durga-puja", "durga_puja"},
		{"ganesh-chaturthi", "ganesh_chaturthi"},
		{"hanuman-jayanti", "hanuman_jayanti"},
		{"karthik-pournami", "karthik_pournami"},
		{"karva-chauth", "karva_chauth"},
		{"krishna-janmashtami", "krishna_janmashtami"},
		{"maha-shivaratri", "maha_shivaratri"},
		{"ram-navami", "ram_navami"},
		{"rudra-homa", "rudra_homa"},
		// Already normalized or no changes
		{"vivah", "vivah"},
		{"upanayana", "upanayana"},
		{"shraadh", "shraadh"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			actual := NormalizeCeremonyType(tc.input)
			if actual != tc.expected {
				t.Errorf("NormalizeCeremonyType(%q) = %q; want %q", tc.input, actual, tc.expected)
			}
		})
	}
}
