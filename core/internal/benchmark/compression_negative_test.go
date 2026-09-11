package benchmark

import (
	"strings"
	"testing"

	"github.com/fvmoraes/dwyt/internal/toolopt"
)

// The compression-negative scenario is the MANDATORY benchmark case
// (Fine-Tuning §12.2): a payload where compression makes things worse. DWYT
// must prove — in the benchmark suite, not in prose — that the pipeline
// chooses passthrough, and that it still compresses when compression
// genuinely pays.

func TestCompressionNegativeScenarioChoosesPassthrough(t *testing.T) {
	// A tiny, already-minimal payload: an envelope would only add metadata.
	tiny := "ok\n2 tests passed\n"

	out := toolopt.Compact(tiny, toolopt.Options{})
	out.RawRef = "dwyt://objects/tiny"
	out.SentTokensEst = 0 // force recompute below like the optimizer does
	rendered := out.Render()
	out.SentTokensEst = estimateForTest(rendered)
	out = toolopt.ApplyCompressionGate(out, toolopt.DefaultMinGainTokens)

	if !out.PassedThrough {
		t.Fatalf("tiny payload: pipeline compressed anyway (raw=%d sent=%d) — the mandatory negative scenario failed",
			out.RawTokensEst, out.SentTokensEst)
	}
	if out.CompressionPct != 0 {
		t.Fatalf("passthrough must not claim savings, got %.1f%%", out.CompressionPct)
	}
}

func TestCompressionStillAppliesWhenItPays(t *testing.T) {
	// A large failing test log: the pipeline must reduce it hard while
	// keeping the verdict and the diagnostics.
	var b strings.Builder
	for i := 0; i < 300; i++ {
		b.WriteString("=== RUN TestThing\n")
		b.WriteString("    a.go:42: mismatch: got 1 want 2\n")
		b.WriteString("--- FAIL: TestThing (0.00s)\n")
		b.WriteString(strings.Repeat("    log noise line with no structure at all\n", 5))
	}
	b.WriteString("FAIL\n")

	out := toolopt.Compact(b.String(), toolopt.Options{})
	out.RawRef = "dwyt://objects/biglog"
	out.SentTokensEst = estimateForTest(out.Render())
	out = toolopt.ApplyCompressionGate(out, toolopt.DefaultMinGainTokens)

	if out.PassedThrough {
		t.Fatal("a large failing log must be compressed, not passed through")
	}
	if out.Status != "fail" {
		t.Fatalf("verdict lost: %q", out.Status)
	}
	if len(out.Errors) == 0 {
		t.Fatal("diagnostics lost — Law 7 violation")
	}
	if out.CompressionPct < 50 {
		t.Fatalf("expected meaningful reduction on log noise, got %.1f%%", out.CompressionPct)
	}
}

// estimateForTest mirrors the optimizer's EstimateTokens call so the gate
// math in this test matches production wiring.
func estimateForTest(s string) int {
	return len(s) / 4
}
