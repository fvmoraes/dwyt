package toolopt

import (
	"strings"
	"testing"
)

// benchmarkCompressionGateResult prevents the compiler from eliminating the
// complete compaction-and-gate pipeline measured below.
var benchmarkCompressionGateResult Compacted

// BenchmarkCompressionGate measures the same sequence the optimizer uses:
// compact, attach a raw reference, recalculate the rendered cost, then decide
// whether the net token gain justifies sending the compacted envelope. The
// sub-benchmarks are serial by design; run with -benchmem -count=10 -cpu=1
// before comparing like-for-like results with benchstat.
func BenchmarkCompressionGate(b *testing.B) {
	b.Run("small_bypass", func(b *testing.B) {
		raw := "ok\n2 tests passed\n"
		if out := compactAndGateForBenchmark(raw); !out.PassedThrough || out.SentTokensEst != out.RawTokensEst || out.CompressionPct != 0 {
			b.Fatalf("small payload must pass through unchanged: %+v", out)
		}

		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			benchmarkCompressionGateResult = compactAndGateForBenchmark(raw)
		}
	})

	b.Run("large_compress", func(b *testing.B) {
		raw := benchmarkLargeFailureLog()
		if out := compactAndGateForBenchmark(raw); out.PassedThrough || out.Status != "fail" || len(out.Errors) == 0 || out.SentTokensEst >= out.RawTokensEst {
			b.Fatalf("large failing log must compress while retaining evidence: %+v", out)
		}

		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			benchmarkCompressionGateResult = compactAndGateForBenchmark(raw)
		}
	})
}

func compactAndGateForBenchmark(raw string) Compacted {
	out := Compact(raw, Options{})
	out.RawRef = "dwyt://objects/benchmark"
	out.SentTokensEst = estimateTokens(out.Render())
	return ApplyCompressionGate(out, DefaultMinGainTokens)
}

func benchmarkLargeFailureLog() string {
	var b strings.Builder
	for range 300 {
		b.WriteString("=== RUN TestThing\n")
		b.WriteString("    a.go:42: mismatch: got 1 want 2\n")
		b.WriteString("--- FAIL: TestThing (0.00s)\n")
		b.WriteString(strings.Repeat("    log noise line with no structure at all\n", 5))
	}
	b.WriteString("FAIL\n")
	return b.String()
}
