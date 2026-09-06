package brain

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// legacyVault builds a pre-v5 vault: root notes, a decisions log and rich
// context snapshots, with none of the numbered areas.
func legacyVault(t *testing.T) *ProjectObsidian {
	t.Helper()
	pb := testVault(t)
	dir := pb.GetBrainDir()

	os.WriteFile(filepath.Join(dir, "decisions.md"),
		[]byte("---\ntype: decisions\n---\n\n# Decisions Log\n\nAdopted the three-MCP architecture.\n"), 0644)
	os.MkdirAll(filepath.Join(dir, "decisions"), 0755)
	os.WriteFile(filepath.Join(dir, "decisions", "index.md"), []byte(`---
type: decisions
---

# Decisions

Links: [[decisions/index]] [[maps/project-map]]

### 2026-01-02 10:00

- Destructive actions must require confirmation
- Switched the table to virtual scrolling

*2026-01-02T10:00:00Z*
`), 0644)
	return pb
}

func writeLegacyContext(t *testing.T, pb *ProjectObsidian, name string, mod time.Time, body string) string {
	t.Helper()
	dir := filepath.Join(pb.GetBrainDir(), "context")
	os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, name)
	content := "---\ntype: context\nclient: \"codex\"\n---\n\n# Conversation Context\n\n" + body
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMigrateToV5CreatesTheLayoutAndIsIdempotent(t *testing.T) {
	pb := legacyVault(t)

	first := pb.MigrateToV5(V5MigrationOptions{})
	if !first.LayoutCreated {
		t.Fatal("a pre-v5 vault has no numbered areas; the pass should report creating them")
	}
	if !first.Confirmed {
		t.Fatalf("expected a clean pass: %+v", first.Errors)
	}
	for _, area := range []CanonicalArea{AreaProject, AreaDecisions, AreaKnowledge, AreaSessions} {
		if _, err := os.Stat(filepath.Join(pb.GetBrainDir(), string(area))); err != nil {
			t.Fatalf("area %s missing after migration", area)
		}
	}

	second := pb.MigrateToV5(V5MigrationOptions{})
	if second.CanonicalSeeded != 0 || second.SessionsConverted != 0 {
		t.Fatalf("the second pass must be a no-op: %+v", second)
	}
	if second.LayoutCreated {
		t.Fatal("the layout already existed on the second pass")
	}
}

func TestMigrateFoldsLegacyDecisionsIntoTheCanonicalLog(t *testing.T) {
	pb := legacyVault(t)
	report := pb.MigrateToV5(V5MigrationOptions{})
	if report.CanonicalSeeded == 0 {
		t.Fatalf("expected decisions to be folded in: %+v", report)
	}

	// A constraint-shaped decision belongs with the constraints; a plain one in
	// the log. Both must survive.
	log, ok := pb.readCanonicalForMemory("decisions")
	if !ok {
		t.Fatal("the canonical decision log should exist")
	}
	if !strings.Contains(log.Body, "virtual scrolling") {
		t.Fatalf("a legacy decision was lost:\n%s", log.Body)
	}
	if !strings.Contains(log.Body, "Destructive actions must require confirmation") {
		t.Fatalf("a legacy decision was lost:\n%s", log.Body)
	}
	// The seed's link line and timestamp separators must not become decisions.
	if strings.Contains(log.Body, "[[maps/project-map]]") {
		t.Fatalf("a link line was promoted as a decision:\n%s", log.Body)
	}
	if strings.Contains(log.Body, "2026-01-02T10:00:00Z") {
		t.Fatalf("a timestamp separator was promoted as a decision:\n%s", log.Body)
	}
}

// The pre-v5 seeds are pure pointers with no knowledge; promoting them would
// pollute canonical memory.
func TestMigrateSkipsLegacyPointerNotes(t *testing.T) {
	pb := testVault(t)
	os.WriteFile(filepath.Join(pb.GetBrainDir(), "tasks.md"),
		[]byte("---\ntype: tasks\n---\n\n# Tasks\n\nThis legacy root note points to [[tasks/index]].\n"), 0644)

	report := pb.MigrateToV5(V5MigrationOptions{})
	if _, ok := pb.ReadCanonical("current-task"); ok {
		note, _ := pb.ReadCanonical("current-task")
		if strings.Contains(note.Body, "legacy root note points to") {
			t.Fatalf("a pointer note was promoted:\n%s", note.Body)
		}
	}
	found := false
	for _, item := range report.Items {
		if item.Source == "tasks.md" && item.Status == V5Skipped {
			found = true
		}
	}
	if !found {
		t.Fatalf("the skip should be reported: %+v", report.Items)
	}
}

func TestMigrateConvertsRecentSessionsAndCompilesOldOnes(t *testing.T) {
	pb := legacyVault(t)
	base := time.Now()

	// Three sessions with semantically distinct knowledge. They must differ by
	// more than a number: the deduplicator collapses numeric variance on
	// purpose (spec §28), so "decision 1" and "decision 2" are one fact.
	decisions := []string{
		"Secrets must never be written to logs",
		"Migrations must always run inside a transaction",
		"Destructive actions must require an explicit confirmation",
	}
	for i := 0; i < 3; i++ {
		writeLegacyContext(t, pb, fmt.Sprintf("s%d.md", i), base.Add(-time.Duration(i)*time.Hour),
			fmt.Sprintf(`## Summary

Fixed problem %d

## Files

- file%d.go

## Decisions

- %s

## Outcome

success
`, i, i, decisions[i]))
	}

	// Keep only the newest: the other two fall outside the window and must be
	// compiled rather than converted.
	report := pb.MigrateToV5(V5MigrationOptions{KeepLatestSessions: 1})
	if report.SessionsConverted != 1 {
		t.Fatalf("expected exactly one converted session: %+v", report)
	}
	if report.SessionsCompiled != 2 {
		t.Fatalf("expected two compiled sessions: %+v", report)
	}
	if len(report.KnowledgePromoted) == 0 {
		t.Fatal("knowledge must be promoted from sessions outside the window")
	}

	// The knowledge from the compiled sessions must be in canonical memory.
	constraints, _ := pb.ReadCanonical("active-constraints")
	for _, i := range []int{1, 2} {
		if !strings.Contains(constraints.Body, decisions[i]) {
			t.Fatalf("knowledge from session %d was lost:\n%s", i, constraints.Body)
		}
	}

	// Nothing was deleted: the originals are still on disk (spec §60).
	for i := 0; i < 3; i++ {
		if _, err := os.Stat(filepath.Join(pb.GetBrainDir(), "context", fmt.Sprintf("s%d.md", i))); err != nil {
			t.Fatalf("the migration deleted a legacy session: s%d.md", i)
		}
	}
}

func TestMigrateSkipsSessionsWithNoExtractableState(t *testing.T) {
	pb := testVault(t)
	writeLegacyContext(t, pb, "empty.md", time.Now(), "Some prose with no sections at all.\n")

	report := pb.MigrateToV5(V5MigrationOptions{})
	if report.SessionsConverted != 0 {
		t.Fatalf("a session with no state must not be converted: %+v", report)
	}
	found := false
	for _, item := range report.Items {
		if strings.Contains(item.Source, "empty.md") && item.Status == V5Skipped {
			found = true
		}
	}
	if !found {
		t.Fatalf("the skip should be reported: %+v", report.Items)
	}
}

func TestMigrateIgnoresTheContextIndex(t *testing.T) {
	pb := testVault(t)
	report := pb.MigrateToV5(V5MigrationOptions{})
	for _, item := range report.Items {
		if strings.HasSuffix(item.Source, "context/index.md") {
			t.Fatalf("the folder index is not a session: %+v", item)
		}
	}
}

func TestMigrateDryRunChangesNothing(t *testing.T) {
	pb := legacyVault(t)
	writeLegacyContext(t, pb, "s0.md", time.Now(), "## Summary\n\nwork\n\n## Decisions\n\n- a decision\n")

	before := vaultFingerprint(t, pb.GetBrainDir())
	report := pb.MigrateToV5(V5MigrationOptions{DryRun: true})
	if !report.DryRun {
		t.Fatal("the report must say it was a dry run")
	}
	if report.CanonicalSeeded == 0 && report.SessionsConverted == 0 {
		t.Fatalf("a dry run must still report what it would do: %+v", report)
	}
	if after := vaultFingerprint(t, pb.GetBrainDir()); after != before {
		t.Fatal("a dry run must not touch the vault")
	}
}

func TestBackfillLifecycleIsOptInAndSkipsStructuralNotes(t *testing.T) {
	pb := testVault(t)
	// A pre-v5 knowledge note with frontmatter but no DWYT lifecycle.
	knowledge := filepath.Join(pb.GetBrainDir(), "knowledge", "old.md")
	os.MkdirAll(filepath.Dir(knowledge), 0755)
	os.WriteFile(knowledge, []byte("---\ntype: knowledge\n---\n\n# Old\n\nbody\n"), 0644)

	// Without the flag, nothing is rewritten: the note might be the user's.
	pb.MigrateToV5(V5MigrationOptions{})
	if ParseLifecycle(readFileString(knowledge)).Managed {
		t.Fatal("lifecycle backfill must be opt-in")
	}

	report := pb.MigrateToV5(V5MigrationOptions{BackfillLifecycle: true})
	if report.LifecycleBackfilled == 0 {
		t.Fatalf("expected a backfill: %+v", report)
	}
	lc := ParseLifecycle(readFileString(knowledge))
	if !lc.Managed {
		t.Fatal("the note should now be DWYT-managed")
	}
	if lc.CreatedAt.IsZero() {
		t.Fatal("the note's mtime should become its creation time")
	}

	// Structural notes must be left alone: giving an index a TTL is a footgun.
	for _, rel := range []string{
		filepath.Join("instructions", "obsidian-law.md"),
		filepath.Join("templates", "decision-template.md"),
		filepath.Join("maps", "project-map.md"),
		filepath.Join("context", "index.md"),
	} {
		path := filepath.Join(pb.GetBrainDir(), rel)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		if ParseLifecycle(readFileString(path)).Managed {
			t.Fatalf("structural note %s must not be given a lifecycle", rel)
		}
	}
}

func TestBackfillCreatesFrontmatterWhenAbsent(t *testing.T) {
	pb := testVault(t)
	bare := filepath.Join(pb.GetBrainDir(), "knowledge", "bare.md")
	os.MkdirAll(filepath.Dir(bare), 0755)
	os.WriteFile(bare, []byte("# Just a heading\n\nbody\n"), 0644)

	pb.MigrateToV5(V5MigrationOptions{BackfillLifecycle: true})
	content := readFileString(bare)
	if !ParseLifecycle(content).Managed {
		t.Fatalf("a note without frontmatter should get one:\n%s", content)
	}
	if !strings.Contains(content, "# Just a heading") {
		t.Fatalf("the body must be preserved:\n%s", content)
	}
}

func TestLegacyRawIsLeftInPlaceAndReported(t *testing.T) {
	pb := testVault(t)
	rawDir := filepath.Join(pb.GetBrainDir(), "raw")
	os.MkdirAll(rawDir, 0755)
	os.WriteFile(filepath.Join(rawDir, "sha256-abc"), []byte("old evidence"), 0644)

	report := pb.MigrateToV5(V5MigrationOptions{})
	if report.RawMigrated != 1 {
		t.Fatalf("legacy raw should be counted: %+v", report)
	}
	// Left in place: moving bytes during a migration risks losing a citation.
	if _, err := os.Stat(filepath.Join(rawDir, "sha256-abc")); err != nil {
		t.Fatal("legacy raw must stay readable")
	}
}

func TestLegacySnapshotParsingExtractsTheDurableFields(t *testing.T) {
	s := legacySnapshotFromNote(`---
type: context
---

# Conversation Context

## User Request

please fix the table

## Summary

Fixed the delete confirmation flow

## Files

- a.tsx
- b.ts

## Decisions

- confirm before delete

## Errors

- TS2345

## Next Steps

- add a test

## Commands

- rtk npm test

## Context For Future Agents

raw evidence at dwyt://objects/abc123
`)
	if s.Objective != "Fixed the delete confirmation flow" {
		t.Fatalf("objective: %q", s.Objective)
	}
	if len(s.AffectedFiles) != 2 || len(s.Decisions) != 1 || len(s.ActiveErrors) != 1 || len(s.NextSteps) != 1 {
		t.Fatalf("durable fields lost: %+v", s)
	}
	if len(s.RawRefs) != 1 {
		t.Fatalf("raw references must survive so the objects are not pruned: %v", s.RawRefs)
	}
	// Commands are deliberately dropped: that is the point of the conversion.
	if strings.Contains(strings.Join(s.Decisions, " "), "rtk npm test") {
		t.Fatal("commands must not leak into the compact snapshot")
	}
}

func TestLooksLikeTimestamp(t *testing.T) {
	positive := []string{"2026-01-02 10:00", "2026-01-02T10:00:00Z", "*2026-01-02T10:00:00Z*"}
	for _, s := range positive {
		if !looksLikeTimestamp(s) {
			t.Fatalf("expected %q to read as a timestamp", s)
		}
	}
	negative := []string{"Adopted Wails", "fix bug 123 in module", "", "-"}
	for _, s := range negative {
		if looksLikeTimestamp(s) {
			t.Fatalf("expected %q not to read as a timestamp", s)
		}
	}
}

// vaultFingerprint captures every file's path and size, so a dry run can be
// proven not to have written anything.
func vaultFingerprint(t *testing.T, dir string) string {
	t.Helper()
	var parts []string
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		parts = append(parts, fmt.Sprintf("%s:%d", filepath.ToSlash(rel), info.Size()))
		return nil
	})
	return strings.Join(parts, "|")
}
