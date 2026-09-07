package brain

import (
	"strings"
	"testing"
)

func compilerVault(t *testing.T) *ProjectObsidian {
	t.Helper()
	pb := testVault(t)
	if err := pb.EnsureCanonicalLayout(); err != nil {
		t.Fatal(err)
	}
	return pb
}

func TestCompileRoutesConstraintsAwayFromTheDecisionLog(t *testing.T) {
	pb := compilerVault(t)
	res := pb.Compile(CompactSnapshot{
		Objective: "harden the delete flow",
		Decisions: []string{
			"Destructive actions must require explicit confirmation",
			"Adopted Wails for the desktop shell",
		},
	})
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}

	constraints, _ := pb.ReadCanonical("active-constraints")
	if !strings.Contains(constraints.Body, "explicit confirmation") {
		t.Fatalf("the constraint-shaped decision should land in active-constraints:\n%s", constraints.Body)
	}
	if strings.Contains(constraints.Body, "Wails") {
		t.Fatalf("a plain decision must not be recorded as a constraint:\n%s", constraints.Body)
	}

	decisions, ok := pb.readCanonicalForMemory("decisions")
	if !ok {
		t.Fatal("the decision log should exist")
	}
	if !strings.Contains(decisions.Body, "Wails") {
		t.Fatalf("the decision should be in the decision log:\n%s", decisions.Body)
	}
}

func TestCompilePromotesResolvedErrorsAsReusablePatterns(t *testing.T) {
	pb := compilerVault(t)
	pb.Compile(CompactSnapshot{
		Objective: "fix the table",
		Resolved: []string{
			"TS2345 in ResourceTable.tsx:84:12 — narrowed the id type to string",
			"fixed the bug", // no resolution stated: teaches nothing
		},
	})

	memory, _ := pb.ReadCanonical("error-memory")
	if !strings.Contains(memory.Body, "narrowed the id type") {
		t.Fatalf("the resolution should be promoted:\n%s", memory.Body)
	}
	if strings.Contains(memory.Body, ":84:12") {
		t.Fatalf("line coordinates are noise in a reusable pattern:\n%s", memory.Body)
	}
	if strings.Contains(memory.Body, "fixed the bug") {
		t.Fatalf("an entry with no stated resolution must not be promoted:\n%s", memory.Body)
	}
}

func TestCompilePromotesBlockersAsKnownIssues(t *testing.T) {
	pb := compilerVault(t)
	pb.Compile(CompactSnapshot{
		Objective: "ship the release",
		Blockers:  []string{"Windows installer fails when Obsidian holds the vault open"},
	})
	issues, _ := pb.ReadCanonical("known-issues")
	if !strings.Contains(issues.Body, "Windows installer fails") {
		t.Fatalf("the blocker should be promoted as a known issue:\n%s", issues.Body)
	}
}

func TestCompilePromotesLessonsByPhrasing(t *testing.T) {
	pb := compilerVault(t)
	pb.Compile(CompactSnapshot{
		Objective: "anything",
		NextSteps: []string{
			"Lesson: run the frontend build before committing",
			"Open a PR",
		},
	})
	lessons, _ := pb.ReadCanonical("lessons")
	if !strings.Contains(lessons.Body, "run the frontend build before committing") {
		t.Fatalf("the lesson should be promoted:\n%s", lessons.Body)
	}
	if strings.Contains(lessons.Body, "Lesson:") {
		t.Fatalf("the marker prefix should be stripped:\n%s", lessons.Body)
	}
	if strings.Contains(lessons.Body, "Open a PR") {
		t.Fatalf("a plain next step is not a lesson:\n%s", lessons.Body)
	}
}

func TestCompileIsIdempotentAcrossSessions(t *testing.T) {
	pb := compilerVault(t)
	s := CompactSnapshot{
		Objective: "same work twice",
		Decisions: []string{"Adopted Wails for the desktop shell"},
		Resolved:  []string{"TS2345 in a.tsx — narrowed the type"},
		Blockers:  []string{"flaky test on CI"},
	}
	first := pb.Compile(s)
	if !first.Any() {
		t.Fatalf("the first compile should promote something: %+v", first)
	}
	second := pb.Compile(s)
	if second.Any() {
		t.Fatalf("re-compiling the same session must promote nothing: %+v", second.Promoted)
	}
	if second.Skipped == 0 {
		t.Fatal("the skip should be counted so the caller can see dedup happened")
	}

	issues, _ := pb.ReadCanonical("known-issues")
	if strings.Count(issues.Body, "flaky test") != 1 {
		t.Fatalf("the known issue was duplicated:\n%s", issues.Body)
	}
}

func TestCompileReportsTheKeysItTouched(t *testing.T) {
	pb := compilerVault(t)
	res := pb.Compile(CompactSnapshot{
		Objective: "x",
		Decisions: []string{"Never log secrets"},
		Blockers:  []string{"missing fixture"},
	})
	joined := strings.Join(res.Keys, ",")
	for _, want := range []string{"active-constraints", "known-issues"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected %s in touched keys %v", want, res.Keys)
		}
	}
}

func TestCompileIgnoresNonDurableFields(t *testing.T) {
	pb := compilerVault(t)
	res := pb.Compile(CompactSnapshot{
		Objective:     "run the tests",
		AffectedFiles: []string{"a.go", "b.go"},
		Validation:    map[string]string{"tests": "pass"},
		Context:       "a lot of prose that has no six-month value",
	})
	// Files, validation and prose are per-session facts, not knowledge.
	if res.Any() {
		t.Fatalf("nothing here deserves promotion: %+v", res.Promoted)
	}
}

func TestLooksLikeConstraint(t *testing.T) {
	positive := []string{
		"Destructive actions must require confirmation",
		"Never route Codex through the proxy",
		"Secrets are forbidden in logs",
		"Always prefix commands with rtk",
	}
	for _, s := range positive {
		if !looksLikeConstraint(s) {
			t.Fatalf("expected %q to read as a constraint", s)
		}
	}
	negative := []string{
		"Adopted Wails for the desktop shell",
		"Switched the table to virtual scrolling",
	}
	for _, s := range negative {
		if looksLikeConstraint(s) {
			t.Fatalf("expected %q to read as a plain decision", s)
		}
	}
}

func TestSplitResolutionHandlesTheSeparatorsAgentsUse(t *testing.T) {
	cases := map[string]string{
		"TS2345 — narrowed the type":       "narrowed the type",
		"race in WatchResources -> locked": "locked",
		"panic resolved by nil check":      "nil check",
		"just a symptom":                   "",
	}
	for in, want := range cases {
		_, got := splitResolution(in)
		if got != want {
			t.Fatalf("splitResolution(%q) resolution = %q, want %q", in, got, want)
		}
	}
}

func TestStripLineCoordinates(t *testing.T) {
	if got := stripLineCoordinates("src/a.ts:84:12 TS2345"); strings.Contains(got, "84") {
		t.Fatalf("coordinates survived: %q", got)
	}
	// A colon that is not a coordinate must be preserved.
	if got := stripLineCoordinates("note: something"); got != "note: something" {
		t.Fatalf("non-coordinate colon was mangled: %q", got)
	}
}
