package status

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestComponentStatusOmitsZeroTimestamps(t *testing.T) {
	encoded, err := json.Marshal(ComponentStatus{})
	if err != nil {
		t.Fatal(err)
	}
	payload := string(encoded)
	for _, field := range []string{
		"last_activity_at",
		"last_health_at",
		"last_healthy_at",
		"last_transition_at",
	} {
		if strings.Contains(payload, `"`+field+`"`) {
			t.Fatalf("zero ComponentStatus serialized %s: %s", field, payload)
		}
	}
}

func TestComponentStatusIncludesRealTimestamps(t *testing.T) {
	at := time.Date(2026, time.September, 11, 12, 30, 0, 0, time.UTC)
	encoded, err := json.Marshal(ComponentStatus{
		LastActivityAt:   at,
		LastHealthAt:     at,
		LastHealthyAt:    at,
		LastTransitionAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]string
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{
		"last_activity_at",
		"last_health_at",
		"last_healthy_at",
		"last_transition_at",
	} {
		if got, want := payload[field], at.Format(time.RFC3339Nano); got != want {
			t.Fatalf("%s = %q, want %q", field, got, want)
		}
	}
}
