package server

import "testing"

func TestNormalizeReleaseVersion(t *testing.T) {
	tests := map[string]string{
		"":       "dev",
		"dev":    "dev",
		"4.7.4":  "v4.7.4",
		"v4.7.4": "v4.7.4",
		"V4.7.4": "v4.7.4",
	}

	for in, want := range tests {
		if got := normalizeReleaseVersion(in); got != want {
			t.Fatalf("normalizeReleaseVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestVersionGreater(t *testing.T) {
	tests := []struct {
		name      string
		candidate string
		current   string
		want      bool
	}{
		{name: "patch", candidate: "v4.7.5", current: "v4.7.4", want: true},
		{name: "same", candidate: "v4.7.4", current: "v4.7.4", want: false},
		{name: "two digit patch", candidate: "v4.7.10", current: "v4.7.9", want: true},
		{name: "older major", candidate: "v3.9.0", current: "v4.0.0", want: false},
		{name: "dev current", candidate: "v4.7.5", current: "dev", want: false},
		{name: "invalid candidate", candidate: "latest", current: "v4.7.4", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := versionGreater(tt.candidate, tt.current); got != tt.want {
				t.Fatalf("versionGreater(%q, %q) = %v, want %v", tt.candidate, tt.current, got, tt.want)
			}
		})
	}
}
