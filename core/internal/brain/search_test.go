package brain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testVault(t *testing.T) *ProjectObsidian {
	t.Helper()
	dwytHome := t.TempDir()
	projectPath := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatal(err)
	}
	pb, err := NewProjectObsidian(dwytHome, projectPath)
	if err != nil {
		t.Fatal(err)
	}
	return pb
}

// writeNote drops a note straight into the vault so a test can control its
// frontmatter exactly.
func writeNote(t *testing.T, pb *ProjectObsidian, rel, frontmatter, title, body string) string {
	t.Helper()
	path := filepath.Join(pb.GetBrainDir(), filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	content := "---\n" + frontmatter + "---\n\n# " + title + "\n\n" + body + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSearchV2DefaultsToSmallTopK(t *testing.T) {
	pb := testVault(t)
	for i := 0; i < 20; i++ {
		writeNote(t, pb,
			"40-knowledge/note"+string(rune('a'+i))+".md",
			"type: knowledge\ndwyt_managed: true\n",
			"Note about caching "+string(rune('a'+i)),
			"prompt caching details")
	}
	results := pb.SearchV2(SearchOptions{Query: "caching"})
	if len(results) != DefaultSearchLimit {
		t.Fatalf("expected the default top-k of %d, got %d", DefaultSearchLimit, len(results))
	}
}

func TestSearchV2ExcludesRawByDefault(t *testing.T) {
	pb := testVault(t)
	writeNote(t, pb, "logs/run.md", "type: log\ndwyt_managed: true\n", "Build log", "unique-token everywhere")
	writeNote(t, pb, "debug/inv.md", "type: debug\ndwyt_managed: true\n", "Debug dump", "unique-token everywhere")
	writeNote(t, pb, "20-decisions/adr.md", "type: decision\ndwyt_managed: true\n", "ADR", "unique-token everywhere")

	results := pb.SearchV2(SearchOptions{Query: "unique-token"})
	for _, r := range results {
		if r.Type == "log" || r.Type == "debug" {
			t.Fatalf("raw note %s leaked into the default search", r.ID)
		}
	}
	if len(results) == 0 {
		t.Fatal("the decision should still be found")
	}

	// Explicit opt-in must bring them back.
	withRaw := pb.SearchV2(SearchOptions{Query: "unique-token", IncludeRaw: true})
	if len(withRaw) <= len(results) {
		t.Fatalf("IncludeRaw should widen the result set: %d vs %d", len(withRaw), len(results))
	}
}

func TestSearchV2ExcludesStaleAndResolvedByDefault(t *testing.T) {
	pb := testVault(t)
	writeNote(t, pb, "40-knowledge/stale.md", "type: knowledge\nstate: stale\ndwyt_managed: true\n", "Stale", "widget behaviour")
	writeNote(t, pb, "40-knowledge/resolved.md", "type: knowledge\nstate: resolved\ndwyt_managed: true\n", "Resolved", "widget behaviour")
	writeNote(t, pb, "40-knowledge/live.md", "type: knowledge\nstate: active\ndwyt_managed: true\n", "Live", "widget behaviour")

	results := pb.SearchV2(SearchOptions{Query: "widget behaviour"})
	if len(results) != 1 {
		t.Fatalf("expected only the active note, got %d: %+v", len(results), titlesOf(results))
	}
	if results[0].Title != "Live" {
		t.Fatalf("wrong note returned: %s", results[0].Title)
	}

	// An explicit empty exclusion set means "give me everything".
	all := pb.SearchV2(SearchOptions{Query: "widget behaviour", ExcludeState: []string{}})
	if len(all) != 3 {
		t.Fatalf("an empty exclusion set should return all three, got %d", len(all))
	}
}

func TestSearchV2ExcludesExpiredByDefault(t *testing.T) {
	pb := testVault(t)
	past := time.Now().Add(-48 * time.Hour).Format(time.RFC3339)
	writeNote(t, pb, "40-knowledge/old.md",
		"type: context\nretention: temporary\nexpires_at: "+past+"\ndwyt_managed: true\n",
		"Expired context", "orphan marker")
	writeNote(t, pb, "40-knowledge/new.md", "type: knowledge\ndwyt_managed: true\n", "Live", "orphan marker")

	results := pb.SearchV2(SearchOptions{Query: "orphan marker"})
	for _, r := range results {
		if r.Title == "Expired context" {
			t.Fatal("an expired note must not appear in the default search")
		}
	}
	if got := pb.SearchV2(SearchOptions{Query: "orphan marker", IncludeExpired: true}); len(got) <= len(results) {
		t.Fatalf("IncludeExpired should widen the set: %d vs %d", len(got), len(results))
	}
}

func TestSearchV2RanksTitleAboveBodyAndCanonicalAboveHistory(t *testing.T) {
	pb := testVault(t)
	writeNote(t, pb, "90-sessions/compact/s1.md",
		"type: context\ndwyt_managed: true\n",
		"Session note",
		strings.Repeat("prompt cache ", 50))
	writeNote(t, pb, "20-decisions/adr.md",
		"type: decision\ndwyt_managed: true\n",
		"prompt cache",
		"we decided to preserve the stable prefix")

	results := pb.SearchV2(SearchOptions{Query: "prompt cache", PreferCurrent: true})
	if len(results) == 0 {
		t.Fatal("no results")
	}
	// An exact title match on canonical content must beat 50 body repetitions
	// in a session note. That inversion was the pre-v5 failure mode.
	if results[0].Type != "decision" {
		t.Fatalf("expected the decision first, got %s (%v)", results[0].Type, titlesOf(results))
	}
	if results[0].MatchedIn != "title_exact" {
		t.Fatalf("expected an exact title match, got %s", results[0].MatchedIn)
	}
}

func TestSearchV2FiltersByType(t *testing.T) {
	pb := testVault(t)
	writeNote(t, pb, "20-decisions/adr.md", "type: decision\ndwyt_managed: true\n", "Alpha", "shared term")
	writeNote(t, pb, "40-knowledge/k.md", "type: knowledge\ndwyt_managed: true\n", "Beta", "shared term")

	results := pb.SearchV2(SearchOptions{Query: "shared term", Types: []string{"decision"}})
	if len(results) != 1 || results[0].Type != "decision" {
		t.Fatalf("type filter ignored: %+v", titlesOf(results))
	}
}

func TestSearchV2RespectsStrictTokenBudget(t *testing.T) {
	pb := testVault(t)
	big := strings.Repeat("a very long body ", 500)
	writeNote(t, pb, "40-knowledge/big.md", "type: knowledge\ndwyt_managed: true\n", "needle", big)
	writeNote(t, pb, "40-knowledge/big2.md", "type: knowledge\ndwyt_managed: true\n", "also needle", big)

	results := pb.SearchV2(SearchOptions{Query: "needle", MaxTokens: 10})
	if len(results) != 0 {
		t.Fatalf("a strict budget must not admit an oversized top hit, got %d", len(results))
	}
}

func TestSearchV2NeverExceedsTokenBudgetAndAllowsExplicitZeroCap(t *testing.T) {
	pb := testVault(t)
	body := strings.Repeat("budget marker ", 20)
	writeNote(t, pb, "40-knowledge/one.md", "type: knowledge\ndwyt_managed: true\n", "Budget one", body)
	writeNote(t, pb, "40-knowledge/two.md", "type: knowledge\ndwyt_managed: true\n", "Budget two", body)

	results := pb.SearchV2(SearchOptions{Query: "budget marker", MaxTokens: 100, MaxTokensSet: true})
	if len(results) == 0 {
		t.Fatal("a result that fits the budget should be returned")
	}
	spent := 0
	for _, result := range results {
		spent += result.TokensEst
	}
	if spent > 100 {
		t.Fatalf("returned %d tokens with a 100-token cap", spent)
	}

	if got := pb.SearchV2(SearchOptions{Query: "budget marker", MaxTokens: 0, MaxTokensSet: true}); len(got) != 0 {
		t.Fatalf("an explicit zero cap must return no results, got %d", len(got))
	}
}

func TestSearchV2IsDeterministic(t *testing.T) {
	pb := testVault(t)
	for i := 0; i < 6; i++ {
		writeNote(t, pb,
			"40-knowledge/n"+string(rune('a'+i))+".md",
			"type: knowledge\ndwyt_managed: true\n",
			"Note", "identical body about determinism")
	}
	first := titlesOf(pb.SearchV2(SearchOptions{Query: "determinism"}))
	for i := 0; i < 3; i++ {
		got := titlesOf(pb.SearchV2(SearchOptions{Query: "determinism"}))
		if strings.Join(got, "|") != strings.Join(first, "|") {
			t.Fatalf("ranking is not reproducible:\n%v\n%v", first, got)
		}
	}
}

func TestSearchV2CountsAccessOnlyWhenAsked(t *testing.T) {
	pb := testVault(t)
	path := writeNote(t, pb, "40-knowledge/k.md",
		"type: knowledge\ndwyt_managed: true\n", "Counted", "access marker")

	pb.SearchV2(SearchOptions{Query: "access marker"})
	data, _ := os.ReadFile(path)
	if ParseLifecycle(string(data)).AccessCount != 0 {
		t.Fatal("a background search must not inflate access counts")
	}

	pb.SearchV2(SearchOptions{Query: "access marker", CountAccess: true})
	data, _ = os.ReadFile(path)
	if ParseLifecycle(string(data)).AccessCount != 1 {
		t.Fatal("an agent-facing search should record the access")
	}
}

func TestSearchV2EmptyQueryReturnsNothing(t *testing.T) {
	if got := testVault(t).SearchV2(SearchOptions{Query: "   "}); len(got) != 0 {
		t.Fatalf("an empty query must not return the whole vault, got %d", len(got))
	}
}

func titlesOf(results []SearchResult) []string {
	out := make([]string, 0, len(results))
	for _, r := range results {
		out = append(out, r.Title)
	}
	return out
}
