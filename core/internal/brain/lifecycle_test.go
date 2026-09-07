package brain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRetentionClassification(t *testing.T) {
	cases := map[string]RetentionClass{
		"decision":     RetentionPermanent,
		"architecture": RetentionPermanent,
		"constraint":   RetentionPermanent,
		"knowledge":    RetentionLongTerm,
		"context":      RetentionTemporary,
		"task":         RetentionTemporary,
		"log":          RetentionEphemeral,
		"debug":        RetentionEphemeral,
	}
	for noteType, want := range cases {
		if got := RetentionFor(noteType); got != want {
			t.Fatalf("RetentionFor(%q) = %s, want %s", noteType, got, want)
		}
	}
}

// An unknown type must never acquire an expiry: DWYT would otherwise delete
// user content it could not classify.
func TestUnknownTypesArePermanent(t *testing.T) {
	if RetentionFor("some-user-invented-type") != RetentionPermanent {
		t.Fatal("an unclassifiable note must be treated as permanent")
	}
	if TTLFor("some-user-invented-type") != 0 {
		t.Fatal("an unclassifiable note must have no TTL")
	}
}

func TestNewLifecycleSetsExpiryOnlyForExpiringTypes(t *testing.T) {
	now := time.Now()

	permanent := NewLifecycle("decision", now)
	if !permanent.ExpiresAt.IsZero() {
		t.Fatalf("a decision must not expire: %v", permanent.ExpiresAt)
	}
	if permanent.Expired(now.Add(10 * 365 * 24 * time.Hour)) {
		t.Fatal("a permanent note must never report as expired")
	}

	ephemeral := NewLifecycle("debug", now)
	if ephemeral.ExpiresAt.IsZero() {
		t.Fatal("a debug note must carry an expiry")
	}
	if !ephemeral.Expired(now.Add(48 * time.Hour)) {
		t.Fatal("a 24h debug note must be expired after 48h")
	}
}

func TestUnmanagedNotesNeverExpire(t *testing.T) {
	lc := Lifecycle{Retention: RetentionEphemeral, ExpiresAt: time.Now().Add(-time.Hour)}
	if lc.Expired(time.Now()) {
		t.Fatal("a note DWYT does not manage must never be expired by DWYT")
	}
}

func TestRenderAndParseRoundTrip(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	lc := NewLifecycle("knowledge", now)
	lc.SourceFile = "internal/db/db.go"
	lc.SourceHash = "sha256:abc"
	lc.StateHash = "sha256:def"
	lc.AccessCount = 3
	lc.LastAccessed = now
	lc.RawRefs = []string{"dwyt://objects/aaa"}

	doc := "---\ntags: [dwyt]\n" + lc.Render() + "---\n\n# Title\n\nbody\n"
	got := ParseLifecycle(doc)

	if got.Type != "knowledge" || got.Retention != RetentionLongTerm {
		t.Fatalf("type/retention lost: %+v", got)
	}
	if !got.Managed {
		t.Fatal("dwyt_managed must survive the round trip")
	}
	if got.SourceHash != "sha256:abc" || got.StateHash != "sha256:def" {
		t.Fatalf("hashes lost: %+v", got)
	}
	if got.AccessCount != 3 {
		t.Fatalf("access count lost: %d", got.AccessCount)
	}
	if len(got.RawRefs) != 1 || got.RawRefs[0] != "dwyt://objects/aaa" {
		t.Fatalf("raw refs lost: %v", got.RawRefs)
	}
	if !got.CreatedAt.Equal(now) {
		t.Fatalf("created_at lost: %v vs %v", got.CreatedAt, now)
	}
}

func TestParseLifecycleTreatsUnmanagedNotesAsSuch(t *testing.T) {
	// A user's own note, or one written by a pre-v5 DWYT.
	lc := ParseLifecycle("# My note\n\nSome content\n")
	if lc.Managed {
		t.Fatal("a note without frontmatter must not be reported as managed")
	}
	legacy := ParseLifecycle("---\ntype: decision\ndate: 2026-01-02T03:04:05Z\n---\n\n# Old\n")
	if legacy.Managed {
		t.Fatal("a pre-v5 note must not be reported as managed")
	}
	// But its date must still feed recency ranking.
	if legacy.CreatedAt.IsZero() {
		t.Fatal("the legacy date field should populate created_at")
	}
	if legacy.Retention != RetentionPermanent {
		t.Fatalf("a legacy decision should still classify as permanent, got %s", legacy.Retention)
	}
}

func TestStaleDetectionBySourceHash(t *testing.T) {
	lc := Lifecycle{SourceFile: "a.go", SourceHash: "sha256:v1"}
	if lc.Stale("sha256:v1") {
		t.Fatal("an unchanged source must not mark the note stale")
	}
	if !lc.Stale("sha256:v2") {
		t.Fatal("a changed source must mark the note stale")
	}
	// A missing signal is not evidence of change.
	if lc.Stale("") {
		t.Fatal("an unreadable source file must not mark the note stale")
	}
	if (Lifecycle{}).Stale("sha256:v2") {
		t.Fatal("a note with no recorded source cannot be stale by hash")
	}
}

func TestRetentionScorePrefersImportantRecentUsedNotes(t *testing.T) {
	now := time.Now()
	hot := Lifecycle{
		Retention: RetentionLongTerm, Importance: 0.9,
		AccessCount: 10, LastAccessed: now,
	}
	cold := Lifecycle{
		Retention: RetentionLongTerm, Importance: 0.3,
		AccessCount: 0, LastAccessed: now.Add(-200 * 24 * time.Hour),
	}
	if hot.RetentionScore(now) <= cold.RetentionScore(now) {
		t.Fatalf("hot %f should outrank cold %f", hot.RetentionScore(now), cold.RetentionScore(now))
	}
	// Permanent content is never scored down by low usage.
	permanent := Lifecycle{Retention: RetentionPermanent, Importance: 0.1}
	if permanent.RetentionScore(now) != 1 {
		t.Fatalf("permanent notes must score 1 regardless of usage, got %f", permanent.RetentionScore(now))
	}
}

func TestTouchAccessUpdatesOnlyLifecycleFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	original := "---\ntags: [dwyt, knowledge]\n" +
		NewLifecycle("knowledge", time.Now()).Render() +
		"custom_field: keep me\n---\n\n# Title\n\nbody text\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	if !TouchAccess(path, time.Now()) {
		t.Fatal("touching a managed note should succeed")
	}
	data, _ := os.ReadFile(path)
	updated := string(data)

	lc := ParseLifecycle(updated)
	if lc.AccessCount != 1 {
		t.Fatalf("access count not incremented: %d", lc.AccessCount)
	}
	if lc.LastAccessed.IsZero() {
		t.Fatal("last_accessed not recorded")
	}
	// Everything else must be preserved byte-for-byte.
	if !strings.Contains(updated, "custom_field: keep me") {
		t.Fatalf("a user field was destroyed:\n%s", updated)
	}
	if !strings.Contains(updated, "# Title\n\nbody text") {
		t.Fatalf("the body was modified:\n%s", updated)
	}

	// Repeated touches accumulate rather than duplicating the field.
	TouchAccess(path, time.Now())
	data, _ = os.ReadFile(path)
	if got := ParseLifecycle(string(data)).AccessCount; got != 2 {
		t.Fatalf("expected count 2, got %d", got)
	}
	if strings.Count(string(data), "access_count:") != 1 {
		t.Fatalf("access_count was duplicated:\n%s", string(data))
	}
}

func TestTouchAccessLeavesUnmanagedNotesAlone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "user.md")
	original := "# My own note\n\ncontent\n"
	os.WriteFile(path, []byte(original), 0644)

	if TouchAccess(path, time.Now()) {
		t.Fatal("an unmanaged note must not be rewritten")
	}
	data, _ := os.ReadFile(path)
	if string(data) != original {
		t.Fatalf("the file was modified:\n%s", string(data))
	}
}

func TestHashFileAndHashContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.go")
	os.WriteFile(path, []byte("package main\n"), 0644)

	if HashFile(path) != HashContent("package main\n") {
		t.Fatal("HashFile must agree with HashContent")
	}
	if HashFile(filepath.Join(dir, "missing.go")) != "" {
		t.Fatal("a missing file must produce no signal, not a hash of nothing")
	}
	if !strings.HasPrefix(HashContent("x"), "sha256:") {
		t.Fatal("hashes must carry the algorithm prefix")
	}
}
