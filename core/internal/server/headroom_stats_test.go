package server

import "testing"

// Mirrors the headroom /stats payload (numbers arrive as float64 after JSON
// decode). The real savings live under summary.compression, which the old
// parser missed — leaving the card empty.
func TestHeadroomTokensSavedFromSummaryCompression(t *testing.T) {
	stats := map[string]interface{}{
		"summary": map[string]interface{}{
			"api_requests": float64(12),
			"compression": map[string]interface{}{
				"avg_compression_pct":                    float64(37.5),
				"total_tokens_saved_with_cli_filtering":  float64(53798),
				"total_tokens_saved_with_rtk":            float64(53798),
			},
		},
	}
	if got := headroomTokensSaved(stats); got != 53798 {
		t.Fatalf("tokens saved = %d, want 53798", got)
	}
	if got := headroomCompressionPct(stats); got != 37.5 {
		t.Fatalf("compression = %.1f, want 37.5", got)
	}
	if got := headroomRequests(stats); got != 12 {
		t.Fatalf("requests = %d, want 12", got)
	}
}

func TestHeadroomTokensSavedFromPersistentLifetime(t *testing.T) {
	stats := map[string]interface{}{
		"persistent_savings": map[string]interface{}{
			"lifetime": map[string]interface{}{"tokens_saved": float64(99000)},
		},
		"requests": map[string]interface{}{"total": float64(5)},
	}
	if got := headroomTokensSaved(stats); got != 99000 {
		t.Fatalf("tokens saved = %d, want 99000 (legacy schema)", got)
	}
	if got := headroomRequests(stats); got != 5 {
		t.Fatalf("requests = %d, want 5", got)
	}
}

func TestHeadroomTokensSavedEmpty(t *testing.T) {
	if got := headroomTokensSaved(map[string]interface{}{}); got != 0 {
		t.Fatalf("empty stats should yield 0, got %d", got)
	}
}
