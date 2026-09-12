package optimizer

import (
	"strings"
	"testing"

	"github.com/fvmoraes/dwyt/internal/contextopt"
	"github.com/fvmoraes/dwyt/internal/toolopt"
)

func TestCompactToolOutputAccountsForRawRefOnce(t *testing.T) {
	g := newOptimizer(t)
	raw := strings.Repeat("[INFO] compiling something\n", 300)

	compacted, err := g.CompactToolOutput(CompactRequest{Content: raw, Kind: "build"})
	if err != nil {
		t.Fatal(err)
	}
	if compacted.RawRef == "" || compacted.PassedThrough {
		t.Fatalf("expected archived compacted payload, got %+v", compacted)
	}
	if want := contextopt.EstimateTokens(compacted.Render()); compacted.SentTokensEst != want {
		t.Fatalf("sent tokens = %d, want rendered payload %d", compacted.SentTokensEst, want)
	}
	if compacted.CompressionMetadataTokens != toolopt.RecoveryOverheadTokens {
		t.Fatalf("metadata = %d, want recovery overhead %d", compacted.CompressionMetadataTokens, toolopt.RecoveryOverheadTokens)
	}
}
