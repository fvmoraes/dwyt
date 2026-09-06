package cacheintel

import (
	"strings"
	"testing"

	"github.com/fvmoraes/dwyt/internal/contextgov"
)

func blocks() []Block {
	return []Block{
		{ID: "user", Class: contextgov.CacheVolatile, Content: "fix the delete flow"},
		{ID: "task", Class: contextgov.CacheSession, Content: "task state: active"},
		{ID: "system", Class: contextgov.CacheImmutable, Content: "you are a coding agent"},
		{ID: "project", Class: contextgov.CacheLongLived, Content: "project: dwyt, Go plus React"},
	}
}

func TestBuildOrdersByCacheClass(t *testing.T) {
	p := Build(blocks())
	want := []string{"system", "project", "task", "user"}
	if len(p.Blocks) != len(want) {
		t.Fatalf("expected %d blocks, got %d", len(want), len(p.Blocks))
	}
	for i, id := range want {
		if p.Blocks[i].ID != id {
			t.Fatalf("position %d: want %s, got %s (order %v)", i, id, p.Blocks[i].ID, idsOf(p.Blocks))
		}
	}
}

func TestStablePrefixExcludesSessionAndVolatile(t *testing.T) {
	p := Build(blocks())
	if !strings.Contains(p.StablePrefix, "coding agent") || !strings.Contains(p.StablePrefix, "Go plus React") {
		t.Fatalf("the stable prefix should hold immutable and long-lived content: %q", p.StablePrefix)
	}
	if strings.Contains(p.StablePrefix, "task state") || strings.Contains(p.StablePrefix, "delete flow") {
		t.Fatalf("session and volatile content must stay out of the prefix: %q", p.StablePrefix)
	}
	if p.PrefixTokens <= 0 || p.PrefixTokens >= p.TotalTokens {
		t.Fatalf("prefix tokens %d should be a proper subset of total %d", p.PrefixTokens, p.TotalTokens)
	}
}

func TestPrefixHashIsStableAcrossVolatileChanges(t *testing.T) {
	first := Build(blocks())

	changed := blocks()
	for i := range changed {
		if changed[i].ID == "user" {
			changed[i].Content = "a completely different request"
		}
	}
	second := Build(changed)

	if first.PrefixHash != second.PrefixHash {
		t.Fatal("changing only volatile content must not change the prefix hash")
	}
	if first.ClassHashes[contextgov.CacheVolatile] == second.ClassHashes[contextgov.CacheVolatile] {
		t.Fatal("the volatile class hash should change")
	}
}

func TestPrefixHashChangesWhenStableContentChanges(t *testing.T) {
	first := Build(blocks())
	changed := blocks()
	for i := range changed {
		if changed[i].ID == "project" {
			changed[i].Content = "project: dwyt, Go plus React plus Wails"
		}
	}
	if Build(changed).PrefixHash == first.PrefixHash {
		t.Fatal("changing long-lived content must change the prefix hash")
	}
}

// A timestamp inside an "immutable" block would destroy the cache on every
// request. The builder must demote it and say so.
func TestBuildDemotesVolatileMetadataOutOfTheStablePrefix(t *testing.T) {
	p := Build([]Block{
		{ID: "system", Class: contextgov.CacheImmutable, Content: "you are an agent"},
		{ID: "meta", Class: contextgov.CacheImmutable, Content: "generated_at: 2026-09-06T12:00:00Z"},
		{ID: "user", Class: contextgov.CacheVolatile, Content: "hello"},
	})

	if len(p.Violations) != 1 {
		t.Fatalf("expected one reported violation, got %+v", p.Violations)
	}
	if !p.Violations[0].Fixed {
		t.Fatal("the violation should be corrected, not merely reported")
	}
	if strings.Contains(p.StablePrefix, "generated_at") {
		t.Fatalf("volatile metadata leaked into the stable prefix: %q", p.StablePrefix)
	}
	// And the demoted block must still be present, at the tail.
	if last := p.Blocks[len(p.Blocks)-1]; last.Class != contextgov.CacheVolatile {
		t.Fatalf("the demoted block should be volatile, got %s", last.Class)
	}
	if !containsID(p.Blocks, "meta") {
		t.Fatal("the demoted block must not be dropped")
	}
}

func TestBlocksWithoutAClassAreTreatedAsVolatile(t *testing.T) {
	p := Build([]Block{{ID: "mystery", Content: "who knows"}})
	if p.Blocks[0].Class != contextgov.CacheVolatile {
		t.Fatalf("an unclassified block must default to volatile, got %s", p.Blocks[0].Class)
	}
	if p.StablePrefix != "" {
		t.Fatal("an unclassified block must not enter the stable prefix")
	}
}

func TestBuildDerivesHashesAndTokens(t *testing.T) {
	p := Build([]Block{{ID: "a", Class: contextgov.CacheImmutable, Content: "some content"}})
	if p.Blocks[0].Hash == "" {
		t.Fatal("the block hash should be derived from its content")
	}
	if p.Blocks[0].Tokens == 0 {
		t.Fatal("the token estimate should be derived from its content")
	}
}

func TestNewIdentityIsStableAndCarriesNoSensitiveData(t *testing.T) {
	in := IdentityInput{
		Provider:       "openai",
		ProjectHash:    "d5960d64640d",
		PolicyVersion:  "5.0.0",
		System:         "you are an agent",
		ToolSchema:     `{"tools":[]}`,
		ProjectContext: "project: dwyt",
	}
	first := NewIdentity(in)
	second := NewIdentity(in)
	if first.Key != second.Key || first.PrefixHash != second.PrefixHash {
		t.Fatal("the identity must be deterministic")
	}
	if !strings.HasPrefix(first.Key, "dwyt:d5960d64640d:5.0.0:") {
		t.Fatalf("unexpected key format: %s", first.Key)
	}
	// No raw content may leak into the key.
	for _, secret := range []string{"you are an agent", "project: dwyt", "/home/"} {
		if strings.Contains(first.Key, secret) {
			t.Fatalf("the cache key leaked content: %s", first.Key)
		}
	}
}

func TestIdentityChangesWithThePolicyVersion(t *testing.T) {
	in := IdentityInput{ProjectHash: "abc", PolicyVersion: "5.0.0", ToolSchema: "{}"}
	a := NewIdentity(in)
	in.PolicyVersion = "5.1.0"
	b := NewIdentity(in)
	if a.Key == b.Key {
		t.Fatal("a prefix built under different rules is not interchangeable; the key must change")
	}
}

func TestIdentityOmitsHashesForEmptySpans(t *testing.T) {
	id := NewIdentity(IdentityInput{ProjectHash: "abc", PolicyVersion: "1"})
	if id.SystemHash != "" || id.ToolSchemaHash != "" || id.ProjectContextHash != "" {
		t.Fatalf("empty spans must not produce hashes: %+v", id)
	}
}

func TestDiagnoseFirstRequest(t *testing.T) {
	d := Diagnose(Prompt{}, Build(blocks()), Observation{})
	if d.State != "unknown" {
		t.Fatalf("without provider data the state must be unknown, got %s", d.State)
	}
	if d.HitRatio != nil {
		t.Fatal("no hit ratio may be reported when nothing was observed")
	}
	if !strings.Contains(strings.Join(d.Findings, " "), "first request") {
		t.Fatalf("the first request should be explained: %v", d.Findings)
	}
}

func TestDiagnoseNeverClaimsUnobservedHits(t *testing.T) {
	p := Build(blocks())
	d := Diagnose(p, p, Observation{Reported: false})
	if d.HitRatio != nil {
		t.Fatal("an unobserved request must not carry a hit ratio")
	}
	if d.State != "unknown" {
		t.Fatalf("state must be unknown, got %s", d.State)
	}
	if !strings.Contains(strings.Join(d.Findings, " "), "will not claim") {
		t.Fatalf("the refusal must be explicit: %v", d.Findings)
	}
}

func TestDiagnoseReportsObservedHitRatio(t *testing.T) {
	p := Build(blocks())
	d := Diagnose(p, p, Observation{Reported: true, InputTokens: 1000, CachedInputTokens: 700})
	if d.State != "observed" {
		t.Fatalf("expected observed, got %s", d.State)
	}
	if d.HitRatio == nil || *d.HitRatio < 0.69 || *d.HitRatio > 0.71 {
		t.Fatalf("unexpected hit ratio: %v", d.HitRatio)
	}
	if !d.PrefixStable {
		t.Fatal("an identical prompt has a stable prefix")
	}
}

func TestDiagnoseExplainsAStablePrefixWithNoHits(t *testing.T) {
	p := Build(blocks())
	d := Diagnose(p, p, Observation{Reported: true, InputTokens: 1000, CachedInputTokens: 0})
	joined := strings.Join(d.Findings, " ")
	if !strings.Contains(joined, "prefix unchanged but the provider reported no cached tokens") {
		t.Fatalf("the diagnosis should point away from DWYT's ordering: %v", d.Findings)
	}
}

func TestDiagnoseNamesTheChangedClass(t *testing.T) {
	first := Build(blocks())
	changed := blocks()
	for i := range changed {
		if changed[i].ID == "project" {
			changed[i].Content = "project: something else entirely"
		}
	}
	d := Diagnose(first, Build(changed), Observation{})
	if d.PrefixStable {
		t.Fatal("the prefix changed")
	}
	if !containsString(d.ChangedClasses, string(contextgov.CacheLongLived)) {
		t.Fatalf("the changed class should be named: %v", d.ChangedClasses)
	}
	if containsString(d.ChangedClasses, string(contextgov.CacheImmutable)) {
		t.Fatalf("the immutable class did not change: %v", d.ChangedClasses)
	}
}

func TestTrimOrderMatchesTheSpec(t *testing.T) {
	order := TrimOrder()
	if order[0] != "discardable" {
		t.Fatalf("noise must be cut first: %v", order)
	}
	if order[len(order)-1] != "stable_last_resort" {
		t.Fatalf("cacheable content must be the last casualty: %v", order)
	}
}

func TestPlanLongContextNoOpBelowThreshold(t *testing.T) {
	p := Build(blocks())
	plan := PlanLongContext(p, p.TotalTokens+1000)
	if plan.NeedToRemove != 0 || len(plan.Candidates) != 0 || !plan.Achievable {
		t.Fatalf("nothing to do below the threshold: %+v", plan)
	}
	// A zero threshold means "no threshold".
	if got := PlanLongContext(p, 0); got.NeedToRemove != 0 || !got.Achievable {
		t.Fatalf("a zero threshold must be a no-op: %+v", got)
	}
}

func TestPlanLongContextDropsVolatileBeforeSessionAndNeverTheStablePrefix(t *testing.T) {
	p := Build([]Block{
		{ID: "system", Class: contextgov.CacheImmutable, Content: strings.Repeat("s", 4000)},
		{ID: "project", Class: contextgov.CacheLongLived, Content: strings.Repeat("p", 4000)},
		{ID: "task", Class: contextgov.CacheSession, Content: strings.Repeat("t", 4000)},
		{ID: "log", Class: contextgov.CacheVolatile, Content: strings.Repeat("l", 4000)},
	})

	plan := PlanLongContext(p, p.TotalTokens-1200)
	if len(plan.Candidates) == 0 {
		t.Fatalf("expected candidates: %+v", plan)
	}
	if plan.Candidates[0].Class != contextgov.CacheVolatile {
		t.Fatalf("volatile content must be dropped first, got %s", plan.Candidates[0].Class)
	}
	for _, c := range plan.Candidates {
		if c.Class == contextgov.CacheImmutable || c.Class == contextgov.CacheLongLived {
			t.Fatalf("the stable prefix must never be offered up to dodge a multiplier: %s", c.ID)
		}
	}
}

func TestPlanLongContextReportsWhenUnachievable(t *testing.T) {
	p := Build([]Block{
		{ID: "system", Class: contextgov.CacheImmutable, Content: strings.Repeat("s", 40000)},
		{ID: "log", Class: contextgov.CacheVolatile, Content: strings.Repeat("l", 400)},
	})
	plan := PlanLongContext(p, 100)
	if plan.Achievable {
		t.Fatal("dropping every eligible block is not enough; the caller must be told")
	}
}

func idsOf(bs []Block) []string {
	out := make([]string, 0, len(bs))
	for _, b := range bs {
		out = append(out, b.ID)
	}
	return out
}

func containsID(bs []Block, id string) bool {
	for _, b := range bs {
		if b.ID == id {
			return true
		}
	}
	return false
}

func containsString(list []string, value string) bool {
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}
