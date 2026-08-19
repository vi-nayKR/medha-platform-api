package svg

import (
	"strings"
	"testing"
)

func TestInlineStyles(t *testing.T) {
	inliner := NewInliner()

	tests := []struct {
		name     string
		svg      string
		contains []string
		excludes []string
	}{
		{
			name: "single class",
			svg: `
<svg>
  <style>.cls-1 { fill: #eadfc7; }</style>
  <path class="cls-1" d="M0 0h10v10H0z"/>
</svg>`,
			contains: []string{`fill="#eadfc7"`, `path`},
			excludes: []string{`<style>`, `.cls-1`, `class="cls-1"`},
		},
		{
			name: "multiple classes",
			svg: `
<svg>
  <style>
    .cls-1 { fill: #111; }
    .cls-2 { stroke: #222; }
  </style>
  <path class="cls-1 cls-2" d="M0 0h10v10H0z"/>
</svg>`,
			contains: []string{`fill="#111"`, `stroke="#222"`},
			excludes: []string{`<style>`, `class="cls-1 cls-2"`},
		},
		{
			name: "unsupported class",
			svg: `
<svg>
  <style>.cls-1 { fill: #111; }</style>
  <path class="unknown" d="M0 0h10v10H0z"/>
</svg>`,
			contains: []string{`class="unknown"`},
			excludes: []string{`<style>`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inliner.InlineStyles(tt.svg)

			for _, s := range tt.contains {
				if !strings.Contains(got, s) {
					t.Errorf("got %s, want to contain %s", got, s)
				}
			}

			for _, s := range tt.excludes {
				if strings.Contains(got, s) {
					t.Errorf("got %s, want to exclude %s", got, s)
				}
			}
		})
	}
}
