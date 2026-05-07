package root

import (
	"strings"
	"testing"

	"github.com/fvmoraes/dwyt/internal/status"
)

func TestToolStatusIconTreatsInstalledAsHealthy(t *testing.T) {
	got := toolStatusIcon(status.ToolStatus{Status: status.StateInstalled})
	if got != "\U0001F7E2" {
		t.Fatalf("installed tool should be green, got %q", got)
	}
}

func TestToolStatusIconTreatsInactiveAsWarning(t *testing.T) {
	got := toolStatusIcon(status.ToolStatus{Status: status.StateInactive})
	if got != "\U0001F7E1" {
		t.Fatalf("inactive tool should be yellow, got %q", got)
	}
}

func TestToolStatusIconTreatsErrorAsFailure(t *testing.T) {
	got := toolStatusIcon(status.ToolStatus{Status: status.StateError})
	if got != "\U0001F534" {
		t.Fatalf("error tool should be red, got %q", got)
	}
}

func TestDaemonVersionNeedsRestart(t *testing.T) {
	oldVersion := version
	t.Cleanup(func() { version = oldVersion })

	version = "v4.8.0"
	tests := []struct {
		name          string
		daemonVersion string
		want          bool
	}{
		{name: "same", daemonVersion: "v4.8.0", want: false},
		{name: "missing from old daemon", daemonVersion: "", want: true},
		{name: "old release", daemonVersion: "v4.7.6", want: true},
		{name: "unprefixed same", daemonVersion: "4.8.0", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := daemonVersionNeedsRestart(tt.daemonVersion); got != tt.want {
				t.Fatalf("daemonVersionNeedsRestart(%q) = %v, want %v", tt.daemonVersion, got, tt.want)
			}
		})
	}
}

func TestDaemonVersionNeedsRestartSkipsDevCLI(t *testing.T) {
	oldVersion := version
	t.Cleanup(func() { version = oldVersion })

	version = "dev"
	if daemonVersionNeedsRestart("v4.7.6") {
		t.Fatal("dev CLI should not restart release daemon based on version mismatch")
	}
	if got := normalizeDaemonVersion("4.8.0"); got != "v4.8.0" || strings.Contains(got, "vv") {
		t.Fatalf("unexpected normalized version: %q", got)
	}
}
