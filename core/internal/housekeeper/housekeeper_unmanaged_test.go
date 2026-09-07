package housekeeper

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fvmoraes/dwyt/internal/brain"
)

// writeRawSession drops a session note with fully controlled frontmatter, so a
// test can decide whether the note is DWYT-managed at all.
func writeRawSession(t *testing.T, pb *brain.ProjectObsidian, name, frontmatter, body string) string {
	t.Helper()
	dir := filepath.Join(pb.GetBrainDir(), "90-sessions", "compact")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("---\n"+frontmatter+"---\n\n"+body), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// A hand-written or imported note without dwyt_managed sits under the same
// folders the housekeeper sweeps. The lifecycle law (spec §61) says notes
// DWYT does not manage are never deleted — the session limit and the dedupe
// pass included.
func TestUnmanagedSessionsSurviveTheLimit(t *testing.T) {
	pb := testVault(t)
	cfg := DefaultConfig()
	cfg.KeepLatestSessions = 2
	h := New(cfg, pb, nil)

	base := time.Now()
	for i := 0; i < 3; i++ {
		writeSession(t, pb, fmt.Sprintf("m%d.md", i),
			base.Add(-time.Duration(i)*time.Hour), time.Time{},
			sessionBody(fmt.Sprintf("managed %d", i)))
	}
	stamp := base.Add(-72 * time.Hour).Format(time.RFC3339)
	writeRawSession(t, pb, "user-note.md",
		"type: context\nretention: temporary\nstate: active\n"+
			"created_at: "+stamp+"\nupdated_at: "+stamp+"\n",
		sessionBody("user authored"))
	writeRawSession(t, pb, "legacy-import.md",
		"type: context\nstate: active\ncreated_at: "+stamp+"\nupdated_at: "+stamp+"\n",
		sessionBody("imported before v5"))

	report := h.Run(Deep)

	// Five sessions, limit two: only the one managed note beyond the limit
	// leaves; both unmanaged notes stay, whatever the arithmetic says.
	if report.SessionsRemoved != 1 {
		t.Fatalf("expected exactly 1 removed session, got %d (%+v)", report.SessionsRemoved, report)
	}
	remaining := sessionFiles(t, pb)
	if !contains(remaining, "user-note.md") || !contains(remaining, "legacy-import.md") {
		t.Fatalf("unmanaged sessions must never be removed, got %v", remaining)
	}
	if !contains(remaining, "m0.md") || !contains(remaining, "m1.md") {
		t.Fatalf("the newest managed sessions should survive, got %v", remaining)
	}
	if contains(remaining, "m2.md") {
		t.Fatalf("the managed session beyond the limit should be gone, got %v", remaining)
	}
}

func TestUnmanagedDuplicateIsNotDeduped(t *testing.T) {
	pb := testVault(t)
	cfg := DefaultConfig()
	h := New(cfg, pb, nil)

	base := time.Now()
	hash := "aaaaaaaaaaaaaaaaaaaa"
	shared := "state_hash: " + hash + "\n"
	stamp := base.Format(time.RFC3339)

	// Newest first: the managed note is kept, its duplicates carry the same
	// state hash. One duplicate is managed (removable), one is not (untouchable).
	writeRawSession(t, pb, "newest.md",
		"type: context\nretention: temporary\nstate: active\ndwyt_managed: true\n"+
			shared+"created_at: "+stamp+"\nupdated_at: "+stamp+"\n",
		sessionBody("same state"))
	writeRawSession(t, pb, "unmanaged-dup.md",
		"type: context\nretention: temporary\nstate: active\n"+
			shared+"created_at: "+base.Add(-time.Hour).Format(time.RFC3339)+"\n"+
			"updated_at: "+base.Add(-time.Hour).Format(time.RFC3339)+"\n",
		sessionBody("same state"))
	writeRawSession(t, pb, "managed-dup.md",
		"type: context\nretention: temporary\nstate: active\ndwyt_managed: true\n"+
			shared+"created_at: "+base.Add(-2*time.Hour).Format(time.RFC3339)+"\n"+
			"updated_at: "+base.Add(-2*time.Hour).Format(time.RFC3339)+"\n",
		sessionBody("same state"))

	report := h.Run(Deep)

	if report.DuplicatesMerged != 1 {
		t.Fatalf("expected 1 duplicate merged, got %d (%+v)", report.DuplicatesMerged, report)
	}
	remaining := sessionFiles(t, pb)
	if !contains(remaining, "newest.md") || !contains(remaining, "unmanaged-dup.md") {
		t.Fatalf("the newest note and the unmanaged duplicate must survive, got %v", remaining)
	}
	if contains(remaining, "managed-dup.md") {
		t.Fatalf("the managed duplicate should be gone, got %v", remaining)
	}
}
