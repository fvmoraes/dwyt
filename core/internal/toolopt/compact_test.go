package toolopt

import (
	"fmt"
	"strings"
	"testing"
)

func TestCompactExtractsTypeScriptDiagnostics(t *testing.T) {
	raw := strings.Join([]string{
		"> tsc --noEmit",
		"src/components/ResourceTable.tsx(84,12): error TS2345: Argument of type 'number' is not assignable to parameter of type 'string'.",
		"src/hooks/useResources.ts(12,3): warning TS6133: 'x' is declared but never read.",
		"Found 1 error.",
	}, "\n")

	got := Compact(raw, Options{})
	if got.Status != "fail" {
		t.Fatalf("expected fail, got %s", got.Status)
	}
	if len(got.Errors) != 1 || got.Errors[0].Code != "TS2345" {
		t.Fatalf("expected the TS2345 error, got %+v", got.Errors)
	}
	if got.Errors[0].File != "src/components/ResourceTable.tsx" || got.Errors[0].Line != 84 {
		t.Fatalf("file/line not extracted: %+v", got.Errors[0])
	}
	if len(got.Warnings) != 1 || got.Warnings[0].Code != "TS6133" {
		t.Fatalf("expected the TS6133 warning, got %+v", got.Warnings)
	}
}

func TestCompactDeduplicatesRepeatedErrors(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 40; i++ {
		// Same error, different line numbers: one finding, count 40.
		fmt.Fprintf(&b, "src/a.ts(%d,4): error TS2322: Type 'X' is not assignable to type 'Y'.\n", i+1)
	}
	got := Compact(b.String(), Options{})
	if len(got.Errors) != 1 {
		t.Fatalf("expected one deduplicated error, got %d", len(got.Errors))
	}
	if got.Errors[0].Count != 40 {
		t.Fatalf("expected count 40, got %d", got.Errors[0].Count)
	}
	if got.CompressionPct < 50 {
		t.Fatalf("40 identical errors should compress well, got %.1f%%", got.CompressionPct)
	}
}

func TestCompactCapsDiagnosticsAndReportsTruncation(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&b, "src/f%d.ts(1,1): error TS100%d: distinct failure %d\n", i, i, i)
	}
	got := Compact(b.String(), Options{MaxErrors: 5})
	if len(got.Errors) != 5 {
		t.Fatalf("expected the cap to apply, got %d", len(got.Errors))
	}
	if !got.Truncated {
		t.Fatal("hitting the cap must be reported as truncation")
	}
	if got.Suppressed["errors_over_cap"] != 25 {
		t.Fatalf("expected 25 suppressed, got %v", got.Suppressed)
	}
}

func TestCompactDropsProgressAndBannerNoise(t *testing.T) {
	raw := strings.Join([]string{
		"npm notice fetching metadata",
		"added 412 packages, and audited 413 packages in 5s",
		"[==============>            ] 55%",
		"go: downloading github.com/example/mod v1.2.3",
		"ok  	github.com/example/pkg	0.312s",
	}, "\n")

	got := Compact(raw, Options{})
	if got.Status != "pass" {
		t.Fatalf("expected pass, got %s (%+v)", got.Status, got)
	}
	if got.Suppressed["noise"] < 3 {
		t.Fatalf("expected banner/progress lines to be suppressed, got %v", got.Suppressed)
	}
	rendered := got.Render()
	for _, noise := range []string{"npm notice", "55%", "downloading"} {
		if strings.Contains(rendered, noise) {
			t.Fatalf("noise %q survived compaction:\n%s", noise, rendered)
		}
	}
}

func TestCompactKeepsGoTestFailures(t *testing.T) {
	raw := strings.Join([]string{
		"--- FAIL: TestSomething (0.00s)",
		"    x_test.go:12: expected 3, got 4",
		"FAIL",
		"FAIL	github.com/example/pkg	0.014s",
	}, "\n")

	got := Compact(raw, Options{})
	if got.Status != "fail" {
		t.Fatalf("expected fail, got %s", got.Status)
	}
	if !strings.Contains(got.Render(), "TestSomething") {
		t.Fatalf("the failing test name must survive:\n%s", got.Render())
	}
}

func TestCompactPassesThroughAlreadyMinimalOutput(t *testing.T) {
	got := Compact("some unrecognised single line", Options{})
	// Nothing was extracted and nothing suppressed, so compaction bought
	// nothing: the content must survive verbatim and no compression may be
	// claimed.
	if !strings.Contains(got.Render(), "some unrecognised single line") {
		t.Fatalf("original content lost:\n%s", got.Render())
	}
	if got.CompressionPct != 0 {
		t.Fatalf("must not claim compression it did not achieve: %.2f%%", got.CompressionPct)
	}
	if got.Summary != "output already minimal; passed through" {
		t.Fatalf("expected the pass-through summary, got %q", got.Summary)
	}
}

// Structured diagnostics must survive even when the compact envelope is not
// smaller than the four raw lines it replaced: they are what the agent acts on.
func TestCompactKeepsDiagnosticsEvenWhenNotSmaller(t *testing.T) {
	raw := "src/a.ts(1,1): error TS1: boom"
	got := Compact(raw, Options{})
	if len(got.Errors) != 1 {
		t.Fatalf("diagnostics were discarded to save tokens: %+v", got)
	}
}

func TestCompactHandlesEmptyOutput(t *testing.T) {
	got := Compact("   \n\n", Options{})
	if got.Status != "pass" {
		t.Fatalf("blank output is not a failure, got %s", got.Status)
	}
}

func TestCompactNormalizesCarriageReturnRedraws(t *testing.T) {
	// A progress bar rewriting one line arrives as a single huge \r-joined line.
	raw := "downloading 10%\rdownloading 50%\rdownloading 100%\rok\n"
	got := Compact(raw, Options{})
	if strings.Count(got.Render(), "downloading") > 0 {
		t.Fatalf("redraw frames should be suppressed:\n%s", got.Render())
	}
	if got.Status != "pass" {
		t.Fatalf("expected pass, got %s", got.Status)
	}
}

func TestCompactReportsCompressionHonestly(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 500; i++ {
		fmt.Fprintf(&b, "[INFO] compiling module %d of 500\n", i)
	}
	got := Compact(b.String(), Options{})
	if got.RawTokensEst <= got.SentTokensEst {
		t.Fatalf("expected real reduction: raw=%d sent=%d", got.RawTokensEst, got.SentTokensEst)
	}
	want := float64(got.RawTokensEst-got.SentTokensEst) / float64(got.RawTokensEst) * 100
	if diff := got.CompressionPct - want; diff > 0.01 || diff < -0.01 {
		t.Fatalf("compression_pct %.4f does not match the reported token counts (%.4f)", got.CompressionPct, want)
	}
}

func TestRenderIncludesRawReferenceWhenPresent(t *testing.T) {
	got := Compact("src/a.ts(1,1): error TS1: boom", Options{})
	got.RawRef = "dwyt://objects/abc"
	rendered := got.Render()
	if !strings.Contains(rendered, "raw_ref: dwyt://objects/abc") {
		t.Fatalf("a compaction must carry its raw reference:\n%s", rendered)
	}
}

func TestGenericSeverityPrefixIsCaptured(t *testing.T) {
	got := Compact("ERROR failed to connect to database", Options{})
	if len(got.Errors) != 1 {
		t.Fatalf("a bare severity prefix should be captured, got %+v", got)
	}
	if got.Status != "fail" {
		t.Fatalf("expected fail, got %s", got.Status)
	}
}
