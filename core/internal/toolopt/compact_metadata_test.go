package toolopt

import (
	"strings"
	"testing"
)

func TestApplyCompressionGateRecordsOnlyUnsentRecoveryMetadata(t *testing.T) {
	if got := EstimateCompressionMetadataTokens(""); got != 0 {
		t.Fatalf("metadata without a raw reference = %d, want 0", got)
	}

	large := Compact(strings.Repeat("go: downloading github.com/example/module v1.0.0\n", 200), Options{})
	large.RawRef = "dwyt://objects/large"
	large.SentTokensEst = estimateTokens(large.Render())
	large = ApplyCompressionGate(large, DefaultMinGainTokens)
	if large.PassedThrough {
		t.Fatal("large noisy output should pass the compression gate")
	}
	if large.CompressionMetadataTokens != RecoveryOverheadTokens {
		t.Fatalf("metadata = %d, want only recovery overhead %d", large.CompressionMetadataTokens, RecoveryOverheadTokens)
	}

	withoutRawRef := Compact(strings.Repeat("go: downloading github.com/example/module v1.0.0\n", 200), Options{})
	withoutRawRef = ApplyCompressionGate(withoutRawRef, DefaultMinGainTokens)
	if withoutRawRef.PassedThrough {
		t.Fatal("large output without a raw reference should still compress")
	}
	if withoutRawRef.CompressionMetadataTokens != 0 {
		t.Fatalf("metadata without raw reference = %d, want 0", withoutRawRef.CompressionMetadataTokens)
	}

	tiny := Compact("ok\n", Options{})
	tiny.RawRef = "dwyt://objects/tiny"
	tiny.SentTokensEst = estimateTokens(tiny.Render())
	tiny = ApplyCompressionGate(tiny, DefaultMinGainTokens)
	if !tiny.PassedThrough {
		t.Fatal("tiny output should pass through")
	}
	if tiny.CompressionMetadataTokens != 0 {
		t.Fatalf("passthrough metadata = %d, want 0", tiny.CompressionMetadataTokens)
	}
}
