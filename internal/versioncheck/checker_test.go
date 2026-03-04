package versioncheck

import "testing"

func TestSemverLessThan(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"27.5.0", "27.5.1", true},
		{"27.5.1", "27.5.0", false},
		{"27.5.1", "27.5.1", false},
		{"0.18.0", "0.18.2", true},
		{"1.0.0", "2.0.0", true},
		{"2.0.0", "1.0.0", false},
		{"1.2.3", "1.3.0", true},
		{"v1.2.3", "v1.2.4", true},
		{"1.2.3-beta", "1.2.3", false}, // same base version, not less
		{"1.2.2-beta", "1.2.3", true},
		{"27.5.1", "28.0.0", true},
	}
	for _, tt := range tests {
		got := semverLessThan(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("semverLessThan(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}
