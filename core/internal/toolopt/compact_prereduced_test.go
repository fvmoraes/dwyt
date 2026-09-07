package toolopt

import (
	"fmt"
	"strings"
	"testing"
)

// With AlreadyCompact the producer (RTK) already reduced the output: the
// aggressive pass must not run, so diagnostics are passed through verbatim in
// the tail instead of being re-classified.
func TestAlreadyCompactSkipsTheAggressivePass(t *testing.T) {
	raw := strings.Repeat("rtk kept line\n", 2000) + "FAIL: TestSomething\n"

	c := Compact(raw, Options{AlreadyCompact: true})

	if len(c.Errors) != 0 {
		t.Fatalf("pre-reduced output must not be re-classified, got errors: %v", c.Errors)
	}
	if c.Status != "fail" {
		t.Fatalf("verdict should still be detected, got %q", c.Status)
	}
	if c.SentTokensEst >= c.RawTokensEst {
		t.Fatalf("a large pre-reduced output should still be bounded: sent=%d raw=%d", c.SentTokensEst, c.RawTokensEst)
	}
	if len(c.Tail) == 0 {
		t.Fatal("tail must carry the passthrough evidence")
	}
}

func TestAlreadyCompactDetectsPassVerdict(t *testing.T) {
	c := Compact("some noise\nok  12 tests passed\n", Options{AlreadyCompact: true})
	if c.Status != "pass" {
		t.Fatalf("expected pass, got %q (summary %q)", c.Status, c.Summary)
	}
}

func TestAlreadyCompactKeepsTailAndDeclaresHead(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&b, "line %03d of producer output\n", i)
	}

	c := Compact(b.String(), Options{AlreadyCompact: true, MaxTokens: 20, TailLines: 4})

	if len(c.Tail) < 4 {
		t.Fatalf("at least TailLines lines must survive, got %d", len(c.Tail))
	}
	if got := c.Tail[len(c.Tail)-1]; !strings.Contains(got, "line 099") {
		t.Fatalf("the tail must keep the last lines, got %q", got)
	}
	if !c.Truncated || c.Suppressed["head_lines"] == 0 {
		t.Fatalf("dropped head must be declared: truncated=%v suppressed=%v", c.Truncated, c.Suppressed)
	}
}
