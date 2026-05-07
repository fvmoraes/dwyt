package root

import (
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
