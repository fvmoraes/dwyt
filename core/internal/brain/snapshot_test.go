package brain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStateHashIgnoresClientAndProse(t *testing.T) {
	base := CompactSnapshot{
		Objective:     "fix the delete flow",
		Status:        "done",
		AffectedFiles: []string{"a.go", "b.go"},
		Decisions:     []string{"confirm destructive actions"},
		Validation:    map[string]string{"tests": "pass"},
	}
	other := base
	other.Client = "kiro"
	other.ConversationID = "abc-123"
	other.Context = "some different prose for a future agent"

	if base.StateHash() != other.StateHash() {
		t.Fatal("client, conversation id and prose must not change the state hash")
	}

	// List order must not matter either: the same state described in a
	// different order is the same state.
	reordered := base
	reordered.AffectedFiles = []string{"b.go", "a.go"}
	if reordered.StateHash() != base.StateHash() {
		t.Fatal("list ordering must not change the state hash")
	}

	changed := base
	changed.Validation = map[string]string{"tests": "fail"}
	if changed.StateHash() == base.StateHash() {
		t.Fatal("a validation change must change the state hash")
	}
}

func TestSaveCompactSnapshotSkipsUnchangedState(t *testing.T) {
	pb := testVault(t)
	s := CompactSnapshot{
		Objective:     "fix the delete flow",
		Status:        "done",
		AffectedFiles: []string{"src/ResourceTable.tsx"},
		Validation:    map[string]string{"tests": "pass"},
	}

	first, err := pb.SaveCompactSnapshot(s)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Written {
		t.Fatalf("the first snapshot must be written: %+v", first)
	}

	// Same state, different conversation: no second note.
	s.Client = "codex"
	s.ConversationID = "another-session"
	second, err := pb.SaveCompactSnapshot(s)
	if err != nil {
		t.Fatal(err)
	}
	if second.Written {
		t.Fatalf("an unchanged state must not produce a second snapshot: %+v", second)
	}
	if second.Skipped == "" {
		t.Fatal("the skip must be explained")
	}
	if second.Path != first.Path {
		t.Fatalf("the skip should point at the existing note: %s vs %s", second.Path, first.Path)
	}

	// A real change writes a new note.
	s.Status = "blocked"
	third, err := pb.SaveCompactSnapshot(s)
	if err != nil {
		t.Fatal(err)
	}
	if !third.Written || third.Path == first.Path {
		t.Fatalf("a changed state must produce a new snapshot: %+v", third)
	}

	files := snapshotFiles(t, pb)
	if len(files) != 2 {
		t.Fatalf("expected exactly 2 snapshots on disk, got %d: %v", len(files), files)
	}
}

func TestSaveCompactSnapshotRejectsEmptyState(t *testing.T) {
	pb := testVault(t)
	out, err := pb.SaveCompactSnapshot(CompactSnapshot{Client: "kiro"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Written {
		t.Fatal("a snapshot with no state must not be written")
	}
	if len(snapshotFiles(t, pb)) != 0 {
		t.Fatal("no file should have been created")
	}
}

func TestSnapshotStaysCompact(t *testing.T) {
	pb := testVault(t)
	out, err := pb.SaveCompactSnapshot(CompactSnapshot{
		Objective:     "improve ResourceTable",
		Status:        "done",
		AffectedFiles: []string{"ResourceTable.tsx", "useResources.ts"},
		Decisions:     []string{"destructive actions require confirmation"},
		Validation:    map[string]string{"typecheck": "pass", "tests": "pass"},
		Resolved:      []string{"TS2345 in ResourceTable"},
		NextSteps:     []string{"add a regression test"},
		RawRefs:       []string{"dwyt://objects/832bf1"},
		Context:       strings.Repeat("prose ", 2000),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Spec §19 targets ~200–500 tokens. The generous ceiling here allows for
	// wording changes while still failing if a transcript sneaks back in.
	if out.TokensEst > 900 {
		t.Fatalf("snapshot grew to %d tokens; it must stay compact", out.TokensEst)
	}
	data, _ := os.ReadFile(out.Path)
	if strings.Count(string(data), "prose") > 400 {
		t.Fatal("the free-form context section was not capped")
	}
}

func TestSnapshotCarriesLifecycleAndStateHash(t *testing.T) {
	pb := testVault(t)
	out, err := pb.SaveCompactSnapshot(CompactSnapshot{
		Objective: "anything",
		RawRefs:   []string{"dwyt://objects/abc"},
	})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(out.Path)
	lc := ParseLifecycle(string(data))
	if !lc.Managed {
		t.Fatal("a snapshot must be DWYT-managed so the housekeeper can retain it")
	}
	if lc.StateHash != out.StateHash {
		t.Fatalf("state hash not persisted: %q vs %q", lc.StateHash, out.StateHash)
	}
	if lc.ExpiresAt.IsZero() {
		t.Fatal("a session snapshot is temporary and must carry an expiry")
	}
	if len(lc.RawRefs) != 1 {
		t.Fatalf("raw refs must be recorded so the objects are not pruned: %v", lc.RawRefs)
	}
}

func TestCompactFromContextSnapshotReducesTheRichPayload(t *testing.T) {
	in := ContextSnapshot{
		Client:      "kiro",
		UserRequest: "please fix the table",
		Summary:     "Fixed the delete confirmation flow\nwith extra detail on a second line",
		Outcome:     "success",
		Files:       []string{"a.tsx"},
		Decisions:   []string{"confirm before delete"},
		Actions:     []string{"edited a.tsx", "ran the tests"},
		Commands:    []string{"rtk npm test"},
		Errors:      []string{"TS2345"},
		NextSteps:   []string{"add a test"},
		Context:     "raw evidence at dwyt://objects/ABC123 and dwyt://objects/abc123",
	}
	out := CompactFromContextSnapshot(in)

	if out.Objective != "Fixed the delete confirmation flow" {
		t.Fatalf("objective should be the first summary line, got %q", out.Objective)
	}
	if out.Status != "done" {
		t.Fatalf("outcome 'success' should normalize to done, got %q", out.Status)
	}
	if len(out.AffectedFiles) != 1 || len(out.Decisions) != 1 || len(out.ActiveErrors) != 1 {
		t.Fatalf("durable fields lost: %+v", out)
	}
	// Raw references must survive so the housekeeper does not prune the
	// objects the snapshot cites — and duplicates must collapse.
	if len(out.RawRefs) != 1 || out.RawRefs[0] != "dwyt://objects/abc123" {
		t.Fatalf("raw refs not extracted/deduplicated: %v", out.RawRefs)
	}
}

func TestCompactFromContextSnapshotFallsBackToUserRequest(t *testing.T) {
	out := CompactFromContextSnapshot(ContextSnapshot{UserRequest: "do the thing"})
	if out.Objective != "do the thing" {
		t.Fatalf("expected the user request as the objective, got %q", out.Objective)
	}
}

func TestSnapshotStatusNormalization(t *testing.T) {
	cases := map[string]string{
		"":                   "",
		"SUCCESS":            "done",
		"completed ok":       "done",
		"blocked on review":  "blocked",
		"build failed":       "failed",
		"partially finished": "in_progress",
		"something else":     "in_progress",
	}
	for in, want := range cases {
		if got := normalizeSnapshotStatus(in); got != want {
			t.Fatalf("normalizeSnapshotStatus(%q) = %q, want %q", in, got, want)
		}
	}
}

func snapshotFiles(t *testing.T, pb *ProjectObsidian) []string {
	t.Helper()
	dir := filepath.Join(pb.GetBrainDir(), filepath.FromSlash(snapshotDir))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			out = append(out, e.Name())
		}
	}
	return out
}
