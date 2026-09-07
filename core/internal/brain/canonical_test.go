package brain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnsureCanonicalLayoutIsIdempotentAndAdditive(t *testing.T) {
	pb := testVault(t)
	if err := pb.EnsureCanonicalLayout(); err != nil {
		t.Fatal(err)
	}
	for _, area := range []CanonicalArea{
		AreaProject, AreaArchitecture, AreaDecisions, AreaModules,
		AreaKnowledge, AreaState, AreaSessions,
	} {
		if info, err := os.Stat(filepath.Join(pb.GetBrainDir(), string(area))); err != nil || !info.IsDir() {
			t.Fatalf("area %s not created: %v", area, err)
		}
	}

	// User content in a seeded note must survive a second pass.
	projectPath, _, _ := pb.canonicalPath("project")
	marker := "\n- my own note about this project\n"
	data, _ := os.ReadFile(projectPath)
	os.WriteFile(projectPath, append(data, []byte(marker)...), 0644)

	if err := pb.EnsureCanonicalLayout(); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(projectPath)
	if !strings.Contains(string(after), "my own note about this project") {
		t.Fatalf("seeding overwrote user content:\n%s", string(after))
	}
}

func TestUpsertCanonicalWritesAndUpdatesInPlace(t *testing.T) {
	pb := testVault(t)
	if err := pb.EnsureCanonicalLayout(); err != nil {
		t.Fatal(err)
	}

	first, err := pb.UpsertCanonical("architecture", "", "Go backend, React frontend.\n", SourceRef{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := pb.UpsertCanonical("architecture", "", "Go backend, React frontend, Wails desktop.\n", SourceRef{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Path != second.Path {
		t.Fatal("updating a canonical fact must reuse the same note, not create a second one")
	}

	note, ok := pb.ReadCanonical("architecture")
	if !ok {
		t.Fatal("note should exist")
	}
	if strings.Count(note.Body, "Go backend") != 1 {
		t.Fatalf("the old copy of the fact survived:\n%s", note.Body)
	}
	if !strings.Contains(note.Body, "Wails desktop") {
		t.Fatalf("the update was not applied:\n%s", note.Body)
	}
}

func TestUpsertCanonicalIsIdempotentOnContent(t *testing.T) {
	pb := testVault(t)
	pb.EnsureCanonicalLayout()

	note, err := pb.UpsertCanonical("conventions", "", "Use table-driven tests.\n", SourceRef{})
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(note.Path)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)

	if _, err := pb.UpsertCanonical("conventions", "", "Use table-driven tests.\n", SourceRef{}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(note.Path)
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("an unchanged fact must not rewrite the file")
	}
}

func TestUpsertCanonicalPreservesCreationTimeAndUsage(t *testing.T) {
	pb := testVault(t)
	pb.EnsureCanonicalLayout()

	first, _ := pb.UpsertCanonical("lessons", "", "- always run the build\n", SourceRef{})
	TouchAccess(first.Path, time.Now())

	second, err := pb.UpsertCanonical("lessons", "", "- always run the build\n- and the tests\n", SourceRef{})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Lifecycle.CreatedAt.Equal(first.Lifecycle.CreatedAt) {
		t.Fatalf("creation time belongs to the fact, not the write: %v vs %v",
			second.Lifecycle.CreatedAt, first.Lifecycle.CreatedAt)
	}
	if second.Lifecycle.AccessCount != 1 {
		t.Fatalf("usage stats were reset by the update: %d", second.Lifecycle.AccessCount)
	}
}

func TestUpsertCanonicalRejectsUnknownKeyAndEmptyBody(t *testing.T) {
	pb := testVault(t)
	if _, err := pb.UpsertCanonical("not-a-key", "", "x", SourceRef{}); err == nil {
		t.Fatal("an unknown canonical key must be rejected")
	}
	if _, err := pb.UpsertCanonical("lessons", "", "   ", SourceRef{}); err == nil {
		t.Fatal("an empty body must be rejected, not written")
	}
}

func TestModuleCanonicalKeys(t *testing.T) {
	pb := testVault(t)
	pb.EnsureCanonicalLayout()

	if _, err := pb.UpsertCanonical("module:contextopt", "", "The context optimizer.\n", SourceRef{}); err != nil {
		t.Fatal(err)
	}
	note, ok := pb.ReadCanonical("module:contextopt")
	if !ok {
		t.Fatal("module note should exist")
	}
	if !strings.Contains(note.Path, filepath.Join(string(AreaModules), "contextopt.md")) {
		t.Fatalf("module note in the wrong place: %s", note.Path)
	}
	if note.Title != "Module: contextopt" {
		t.Fatalf("unexpected title %q", note.Title)
	}
}

func TestAppendCanonicalBulletDeduplicates(t *testing.T) {
	pb := testVault(t)
	pb.EnsureCanonicalLayout()

	added, err := pb.AppendCanonicalBullet("lessons", "Always confirm destructive actions")
	if err != nil || !added {
		t.Fatalf("first bullet should be added: added=%v err=%v", added, err)
	}
	// Same assertion, different wording and punctuation.
	added, err = pb.AppendCanonicalBullet("lessons", "always confirm the destructive actions.")
	if err != nil {
		t.Fatal(err)
	}
	if added {
		t.Fatal("a semantically equivalent bullet must not be added twice")
	}
	// Different word order must also dedupe.
	if added, _ := pb.AppendCanonicalBullet("lessons", "Destructive actions: confirm always"); added {
		t.Fatal("word order must not defeat dedup")
	}
	// A genuinely different lesson is added.
	if added, _ := pb.AppendCanonicalBullet("lessons", "Prefer symbols over full files"); !added {
		t.Fatal("a distinct lesson must be added")
	}

	note, _ := pb.ReadCanonical("lessons")
	if strings.Count(note.Body, "confirm") > 1 {
		t.Fatalf("duplicate bullets accumulated:\n%s", note.Body)
	}
}

func TestNormalizeForDedupCollapsesNumbersAndStopWords(t *testing.T) {
	a := normalizeForDedup("The build failed after 3 retries")
	b := normalizeForDedup("build failed after 7 retries")
	if a != b {
		t.Fatalf("numeric and stop-word variance should collapse: %q vs %q", a, b)
	}
	if normalizeForDedup("!!! ???") != "" {
		t.Fatal("a line with no words must normalize to empty so it is never treated as a match")
	}
}

func TestMarkCanonicalStale(t *testing.T) {
	pb := testVault(t)
	pb.EnsureCanonicalLayout()
	pb.UpsertCanonical("architecture", "", "described from code\n", SourceRef{File: "a.go", Hash: "sha256:v1"})

	changed, err := pb.MarkCanonicalStale("architecture")
	if err != nil || !changed {
		t.Fatalf("marking stale should succeed: changed=%v err=%v", changed, err)
	}
	note, _ := pb.ReadCanonical("architecture")
	if note.Lifecycle.State != NoteStale {
		t.Fatalf("state not persisted: %s", note.Lifecycle.State)
	}
	if _, err := pb.MarkCanonicalStale("nope"); err == nil {
		t.Fatal("an unknown key must be rejected")
	}
}

func TestSourceOfRecordsHashOfExistingFileOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.go")
	os.WriteFile(path, []byte("package x\n"), 0644)

	ref := SourceOf(path)
	if ref.File != path || ref.Hash == "" {
		t.Fatalf("expected a populated ref, got %+v", ref)
	}
	if missing := SourceOf(filepath.Join(dir, "nope.go")); missing.File != "" || missing.Hash != "" {
		t.Fatalf("a missing file must produce an empty ref, got %+v", missing)
	}
}

func TestBodyOfStripsGeneratedHeaders(t *testing.T) {
	pb := testVault(t)
	pb.EnsureCanonicalLayout()
	pb.UpsertCanonical("conventions", "Conventions", "line one\nline two\n", SourceRef{})

	// A round trip must not accumulate titles or Links lines.
	note, _ := pb.ReadCanonical("conventions")
	pb.UpsertCanonical("conventions", "Conventions", note.Body+"\nline three\n", SourceRef{})
	again, _ := pb.ReadCanonical("conventions")

	raw, _ := os.ReadFile(again.Path)
	if strings.Count(string(raw), "# Conventions") != 1 {
		t.Fatalf("title duplicated:\n%s", string(raw))
	}
	if strings.Count(string(raw), "Links: [[") != 1 {
		t.Fatalf("links line duplicated:\n%s", string(raw))
	}
	if !strings.Contains(again.Body, "line three") {
		t.Fatalf("content lost:\n%s", again.Body)
	}
}
