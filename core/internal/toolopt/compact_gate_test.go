package toolopt

import (
	"strings"
	"testing"
)

// The compression gate implements Law 6 (positive Token ROI) with the Law 7
// exception: structured diagnostics are the actionable artifact, so they are
// kept even when the net gain is marginal. The gate runs where all costs are
// known — after the raw reference is attached — and models metadata plus the
// recovery-handle overhead a caller pays for keeping the raw bytes around.

func gateFixture() Compacted {
	return Compacted{
		Status:        "fail",
		Summary:       "1 error, 2 warnings",
		RawTokensEst:  4000,
		SentTokensEst: 300,
		RawRef:        "dwyt://objects/abcdef12",
		CompressionPct: 92.5,
	}
}

func TestGateKeepsRealGain(t *testing.T) {
	c := ApplyCompressionGate(gateFixture(), DefaultMinGainTokens)
	if c.PassedThrough {
		t.Fatal("a 4000→300 reduction with metadata must not pass through")
	}
}

func TestGatePassesThroughMarginalGain(t *testing.T) {
	c := gateFixture()
	c.RawTokensEst = 100
	c.SentTokensEst = 90
	c.CompressionPct = 10

	out := ApplyCompressionGate(c, DefaultMinGainTokens)
	if !out.PassedThrough {
		t.Fatalf("net gain = %d tokens (100 − 90 − metadata), below the %d minimum: must pass through",
			100-90-(estimateTokens("dwyt://objects/abcdef12")+RecoveryOverheadTokens), DefaultMinGainTokens)
	}
	if out.SentTokensEst != out.RawTokensEst {
		t.Fatalf("passthrough must report raw cost as sent cost: got %d, want %d", out.SentTokensEst, out.RawTokensEst)
	}
	if out.CompressionPct != 0 {
		t.Fatalf("passthrough must not claim savings: got %.1f%%", out.CompressionPct)
	}
	if out.Errors != nil || out.Warnings != nil || out.Suppressed != nil {
		t.Fatal("passthrough must not carry a half-populated structured payload")
	}
}

// TestGateKeepsCriticalEvidence pins the Law 7 precedence: when structured
// diagnostics were extracted, they survive even a negative net gain —
// discarding actionable errors to win tokens trades correctness for size.
func TestGateKeepsCriticalEvidence(t *testing.T) {
	c := gateFixture()
	c.RawTokensEst = 100
	c.SentTokensEst = 140 // bigger than raw
	c.Errors = []Diagnostic{{Severity: SeverityError, File: "a.go", Line: 3, Message: "boom"}}

	out := ApplyCompressionGate(c, DefaultMinGainTokens)
	if out.PassedThrough {
		t.Fatal("structured evidence must survive the gate (Law 7 overrides Law 6)")
	}
	if len(out.Errors) != 1 {
		t.Fatal("evidence lost")
	}
}

func TestGateZeroGainSmallPayload(t *testing.T) {
	c := Compacted{Status: "pass", Summary: "ok", RawTokensEst: 12, SentTokensEst: 14, RawRef: "dwyt://objects/x1"}
	out := ApplyCompressionGate(c, DefaultMinGainTokens)
	if !out.PassedThrough {
		t.Fatal("a payload that GROWS under compression must pass through")
	}
}

func TestGateIdempotentOnOwnMarker(t *testing.T) {
	c := ApplyCompressionGate(gateFixture(), DefaultMinGainTokens)
	again := ApplyCompressionGate(c, DefaultMinGainTokens)
	if again.PassedThrough != c.PassedThrough || again.SentTokensEst != c.SentTokensEst {
		t.Fatal("applying the gate twice must be a no-op")
	}
}

// TestDoubleCompactNeverLosesStatus pins idempotence for DWYT's own payloads:
// running the compressor on its own rendered output must keep the verdict
// and must not inflate the payload (Law 12: compress once).
func TestDoubleCompactNeverLosesStatus(t *testing.T) {
	raw := strings.Repeat("ERROR a.go:10:1: something failed badly\n", 40) +
		"FAIL\n" + strings.Repeat("noise line with no structure at all here\n", 200)

	first := Compact(raw, Options{})
	first.SentTokensEst = estimateTokens(first.Render())

	second := Compact(first.Render(), Options{})

	if second.Status != first.Status {
		t.Fatalf("double compact changed the verdict: %s → %s", first.Status, second.Status)
	}
	if second.RawTokensEst > first.RawTokensEst {
		t.Fatalf("compact output re-expanded: %d → %d tokens", first.RawTokensEst, second.RawTokensEst)
	}
}
