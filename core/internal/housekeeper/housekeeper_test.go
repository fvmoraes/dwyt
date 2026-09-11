package housekeeper

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/fvmoraes/dwyt/internal/brain"
	"github.com/fvmoraes/dwyt/internal/rawstore"
)

func testVault(t *testing.T) *brain.ProjectObsidian {
	t.Helper()
	dwytHome := t.TempDir()
	projectPath := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatal(err)
	}
	pb, err := brain.NewProjectObsidian(dwytHome, projectPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := pb.EnsureCanonicalLayout(); err != nil {
		t.Fatal(err)
	}
	return pb
}

// writeSession drops a compact session note with a controlled activity time.
func writeSession(t *testing.T, pb *brain.ProjectObsidian, name string, activity time.Time, expires time.Time, body string) string {
	t.Helper()
	dir := filepath.Join(pb.GetBrainDir(), "90-sessions", "compact")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	fm := "---\ntags: [dwyt, session, compact]\ntype: context\nretention: temporary\nstate: active\n" +
		"created_at: " + activity.Format(time.RFC3339) + "\n" +
		"updated_at: " + activity.Format(time.RFC3339) + "\n" +
		"dwyt_managed: true\n"
	if !expires.IsZero() {
		fm += "expires_at: " + expires.Format(time.RFC3339) + "\n"
	}
	fm += "---\n\n"
	if err := os.WriteFile(path, []byte(fm+body), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func sessionBody(objective string, extra ...string) string {
	return "# " + objective + "\n\n" + strings.Join(extra, "\n") + "\n"
}

func TestDisabledHousekeeperDoesNothing(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = false
	h := New(cfg, testVault(t), nil)

	report := h.Run(Deep)
	if report.Skipped == "" {
		t.Fatal("a disabled housekeeper must report why it did nothing")
	}
	if report.ExpiredRemoved != 0 || report.SessionsRemoved != 0 {
		t.Fatalf("a disabled housekeeper must not touch anything: %+v", report)
	}
}

func TestHousekeeperWithoutVaultIsIdle(t *testing.T) {
	report := New(DefaultConfig(), nil, nil).Run(Deep)
	if report.Skipped != "no project vault" {
		t.Fatalf("expected an idle report, got %+v", report)
	}
}

func TestEnforcesTheSessionLimit(t *testing.T) {
	pb := testVault(t)
	cfg := DefaultConfig()
	cfg.KeepLatestSessions = 3
	h := New(cfg, pb, nil)

	base := time.Now()
	for i := 0; i < 6; i++ {
		writeSession(t, pb, fmt.Sprintf("s%d.md", i),
			base.Add(-time.Duration(i)*time.Hour), time.Time{},
			sessionBody(fmt.Sprintf("session %d", i)))
	}

	report := h.Run(Deep)
	if report.SessionsRemoved != 3 {
		t.Fatalf("expected 3 sessions removed, got %d (%+v)", report.SessionsRemoved, report)
	}
	remaining := sessionFiles(t, pb)
	if len(remaining) != 3 {
		t.Fatalf("expected 3 sessions retained, got %d: %v", len(remaining), remaining)
	}
	// The newest must be the ones kept.
	for _, name := range []string{"s0.md", "s1.md", "s2.md"} {
		if !contains(remaining, name) {
			t.Fatalf("the newest sessions should survive, got %v", remaining)
		}
	}
}

func TestPromotesKnowledgeBeforeDeletingASession(t *testing.T) {
	pb := testVault(t)
	cfg := DefaultConfig()
	cfg.KeepLatestSessions = 1
	h := New(cfg, pb, nil)

	base := time.Now()
	// The newest session survives; the older one must have its knowledge
	// rescued before it is deleted (spec §21, §24).
	writeSession(t, pb, "new.md", base, time.Time{}, sessionBody("recent work"))
	writeSession(t, pb, "old.md", base.Add(-72*time.Hour), time.Time{},
		sessionBody("old work",
			"## Decisions", "- Destructive actions must require confirmation",
			"## Resolved", "- TS2345 in a.tsx — narrowed the type",
			"## Blockers", "- installer fails on Windows"))

	report := h.Run(Deep)
	if report.SessionsRemoved != 1 {
		t.Fatalf("expected the old session to be removed: %+v", report)
	}
	if len(report.KnowledgePromoted) == 0 {
		t.Fatal("knowledge must be promoted before a session is deleted")
	}

	constraints, _ := pb.ReadCanonical("active-constraints")
	if !strings.Contains(constraints.Body, "require confirmation") {
		t.Fatalf("the constraint did not survive the deletion:\n%s", constraints.Body)
	}
	errMemory, _ := pb.ReadCanonical("error-memory")
	if !strings.Contains(errMemory.Body, "narrowed the type") {
		t.Fatalf("the error pattern did not survive:\n%s", errMemory.Body)
	}
	issues, _ := pb.ReadCanonical("known-issues")
	if !strings.Contains(issues.Body, "installer fails on Windows") {
		t.Fatalf("the known issue did not survive:\n%s", issues.Body)
	}
	if contains(sessionFiles(t, pb), "old.md") {
		t.Fatal("the old session should be gone once its knowledge was promoted")
	}
}

func TestExpiredNotesAreRemovedButPermanentOnesAreNot(t *testing.T) {
	pb := testVault(t)
	h := New(DefaultConfig(), pb, nil)

	past := time.Now().Add(-48 * time.Hour)
	// An expired operational note.
	writeManaged(t, pb, "logs/old.md", "log", "ephemeral", past, past.Add(time.Hour))
	// A permanent note with an (invalid) expiry: retention wins.
	writeManaged(t, pb, "20-decisions/adr-1.md", "decision", "permanent", past, past.Add(time.Hour))
	// A note DWYT does not manage.
	unmanaged := filepath.Join(pb.GetBrainDir(), "40-knowledge", "mine.md")
	os.MkdirAll(filepath.Dir(unmanaged), 0755)
	os.WriteFile(unmanaged, []byte("# My own note\n\ncontent\n"), 0644)

	h.Run(Deep)

	if _, err := os.Stat(filepath.Join(pb.GetBrainDir(), "logs", "old.md")); err == nil {
		t.Fatal("an expired operational note should be removed")
	}
	if _, err := os.Stat(filepath.Join(pb.GetBrainDir(), "20-decisions", "adr-1.md")); err != nil {
		t.Fatal("a permanent note must never be removed by a TTL")
	}
	if _, err := os.Stat(unmanaged); err != nil {
		t.Fatal("an unmanaged note must never be touched")
	}
}

func TestDryRunChangesNothing(t *testing.T) {
	pb := testVault(t)
	cfg := DefaultConfig()
	cfg.KeepLatestSessions = 1
	h := New(cfg, pb, nil)

	base := time.Now()
	writeSession(t, pb, "a.md", base, time.Time{}, sessionBody("keep"))
	writeSession(t, pb, "b.md", base.Add(-time.Hour), time.Time{}, sessionBody("drop"))

	report := h.RunDry(Deep)
	if !report.DryRun {
		t.Fatal("the report must say it was a dry run")
	}
	if report.SessionsRemoved != 1 {
		t.Fatalf("a dry run must still report what it would remove: %+v", report)
	}
	if len(sessionFiles(t, pb)) != 2 {
		t.Fatal("a dry run must not delete anything")
	}
	// The config must be restored afterwards.
	if h.Config().DryRun {
		t.Fatal("RunDry must not leave the housekeeper permanently in dry-run mode")
	}
}

func TestStaleDetectionMarksNotesWhoseSourceChanged(t *testing.T) {
	pb := testVault(t)
	h := New(DefaultConfig(), pb, nil)

	sourceDir := t.TempDir()
	source := filepath.Join(sourceDir, "client.go")
	os.WriteFile(source, []byte("package a\n"), 0644)

	note, err := pb.UpsertCanonical("architecture", "", "described from client.go\n", brain.SourceOf(source))
	if err != nil {
		t.Fatal(err)
	}

	// Unchanged source: nothing to mark.
	if report := h.Run(Light); report.StaleMarked != 0 {
		t.Fatalf("an unchanged source must not mark anything stale: %+v", report)
	}

	os.WriteFile(source, []byte("package a\n\nfunc New() {}\n"), 0644)
	report := h.Run(Light)
	if report.StaleMarked != 1 {
		t.Fatalf("a changed source should mark the derived note stale: %+v", report)
	}
	data, _ := os.ReadFile(note.Path)
	if brain.ParseLifecycle(string(data)).State != brain.NoteStale {
		t.Fatalf("state not persisted:\n%s", string(data))
	}
	// The body must be untouched.
	if !strings.Contains(string(data), "described from client.go") {
		t.Fatal("stale marking must not rewrite the note body")
	}
}

func TestDeduplicatesSnapshotsWithTheSameStateHash(t *testing.T) {
	pb := testVault(t)
	h := New(DefaultConfig(), pb, nil)

	base := time.Now()
	for i, name := range []string{"newer.md", "older.md"} {
		path := writeSession(t, pb, name, base.Add(-time.Duration(i)*time.Hour), time.Time{}, sessionBody("same state"))
		data, _ := os.ReadFile(path)
		updated, _ := brain.ReplaceFrontmatterField(string(data), "state_hash", "sha256:identical")
		os.WriteFile(path, []byte(updated), 0644)
	}

	report := h.Run(Deep)
	if report.DuplicatesMerged != 1 {
		t.Fatalf("expected one duplicate removed: %+v", report)
	}
	remaining := sessionFiles(t, pb)
	if len(remaining) != 1 || remaining[0] != "newer.md" {
		t.Fatalf("the newest copy should survive, got %v", remaining)
	}
}

func TestRawPruningRespectsVaultReferences(t *testing.T) {
	pb := testVault(t)
	raw, err := rawstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := New(DefaultConfig(), pb, raw)

	cited, _ := raw.Put("evidence still cited by a note", rawstore.PutOptions{TTL: time.Nanosecond})
	orphan, _ := raw.Put("evidence nobody references", rawstore.PutOptions{TTL: time.Nanosecond})
	time.Sleep(2 * time.Millisecond)

	writeSession(t, pb, "s.md", time.Now(), time.Time{},
		sessionBody("work", "## Raw Refs", "- "+cited.Ref()))

	report := h.Run(Deep)
	if report.RawPruned != 1 {
		t.Fatalf("expected exactly the orphan to be pruned: %+v", report)
	}
	if _, err := raw.Stat(cited.Ref()); err != nil {
		t.Fatal("an object still cited by a note must not be pruned")
	}
	if _, err := raw.Stat(orphan.Ref()); err == nil {
		t.Fatal("the unreferenced expired object should be gone")
	}
}

func TestConcurrentRunsDoNotOverlap(t *testing.T) {
	pb := testVault(t)
	h := New(DefaultConfig(), pb, nil)

	// Force the "already running" branch deterministically.
	h.mu.Lock()
	h.running = true
	h.mu.Unlock()

	report := h.Run(Deep)
	if report.Skipped == "" {
		t.Fatalf("an overlapping pass must be skipped, not raced: %+v", report)
	}

	h.mu.Lock()
	h.running = false
	h.mu.Unlock()
}

func TestStatusReportsRetentionHealth(t *testing.T) {
	pb := testVault(t)
	raw, _ := rawstore.New(t.TempDir())
	cfg := DefaultConfig()
	cfg.KeepLatestSessions = 10
	h := New(cfg, pb, raw)

	writeSession(t, pb, "s.md", time.Now(), time.Now().Add(6*time.Hour), sessionBody("work"))
	raw.Put("some evidence", rawstore.PutOptions{})

	status := h.HousekeeperStatus()
	if enabled, _ := status["enabled"].(bool); !enabled {
		t.Fatal("status should report the housekeeper as enabled")
	}
	if status["sessions_limit"] != 10 {
		t.Fatalf("the limit should be reported: %v", status["sessions_limit"])
	}
	if got, _ := status["sessions_retained"].(int); got != 1 {
		t.Fatalf("expected 1 retained session, got %v", status["sessions_retained"])
	}
	if got, _ := status["expiring_within_24h"].(int); got != 1 {
		t.Fatalf("expected 1 note expiring within 24h, got %v", status["expiring_within_24h"])
	}
	if got, _ := status["raw_objects"].(int); got != 1 {
		t.Fatalf("expected the raw object to be counted, got %v", status["raw_objects"])
	}
	if promote, _ := status["promote_before_delete"].(bool); !promote {
		t.Fatal("promote-before-delete must be reported so the user knows knowledge is safe")
	}
	if status["last_run"] != nil {
		t.Fatalf("no pass has run yet, expected nil, got %v", status["last_run"])
	}

	h.Run(Deep)
	if h.HousekeeperStatus()["last_run"] == nil {
		t.Fatal("last_run should be populated after a pass")
	}
}

func TestTTLOverrideIsHonoured(t *testing.T) {
	pb := testVault(t)
	cfg := DefaultConfig()
	// Make knowledge notes expire after a nanosecond.
	cfg.TTLOverrides = map[string]time.Duration{"knowledge": time.Nanosecond}
	h := New(cfg, pb, nil)

	past := time.Now().Add(-time.Hour)
	writeManaged(t, pb, "40-knowledge/k.md", "knowledge", "long_term", past, time.Time{})

	h.Run(Deep)
	if _, err := os.Stat(filepath.Join(pb.GetBrainDir(), "40-knowledge", "k.md")); err == nil {
		t.Fatal("the TTL override should have expired the note")
	}
}

func TestTTLOverrideOfZeroDisablesExpiry(t *testing.T) {
	pb := testVault(t)
	cfg := DefaultConfig()
	cfg.TTLOverrides = map[string]time.Duration{"log": 0}
	h := New(cfg, pb, nil)

	past := time.Now().Add(-100 * 24 * time.Hour)
	writeManaged(t, pb, "logs/keep.md", "log", "ephemeral", past, past.Add(time.Hour))

	h.Run(Deep)
	if _, err := os.Stat(filepath.Join(pb.GetBrainDir(), "logs", "keep.md")); err != nil {
		t.Fatal("a zero TTL override must disable expiry, not force it")
	}
}

func TestStartRunsLightThenPeriodicDeep(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Interval = time.Hour
		h := New(cfg, nil, nil)
		h.Start()
		synctest.Wait()

		startup, ok := h.HousekeeperStatus()["last_report"].(*Report)
		if !ok {
			t.Fatalf("expected a startup report, got %#v", h.HousekeeperStatus()["last_report"])
		}
		if startup.Depth != Light {
			t.Fatalf("startup depth = %q, want %q", startup.Depth, Light)
		}

		time.Sleep(cfg.Interval)
		synctest.Wait()
		periodic, ok := h.HousekeeperStatus()["last_report"].(*Report)
		if !ok {
			t.Fatalf("expected a periodic report, got %#v", h.HousekeeperStatus()["last_report"])
		}
		if periodic.Depth != Deep {
			t.Fatalf("periodic depth = %q, want %q", periodic.Depth, Deep)
		}

		if err := h.StopContext(context.Background()); err != nil {
			t.Fatalf("StopContext() error = %v", err)
		}
	})
}

func TestStopIsIdempotent(t *testing.T) {
	h := New(DefaultConfig(), nil, nil)
	h.Stop()
	h.Stop() // must not panic on a double close
}

// writeManaged writes a DWYT-managed note with explicit lifecycle fields.
func writeManaged(t *testing.T, pb *brain.ProjectObsidian, rel, noteType, retention string, updated, expires time.Time) string {
	t.Helper()
	path := filepath.Join(pb.GetBrainDir(), filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	fm := "---\ntags: [dwyt]\ntype: " + noteType + "\nretention: " + retention +
		"\nstate: active\nupdated_at: " + updated.Format(time.RFC3339) + "\ndwyt_managed: true\n"
	if !expires.IsZero() {
		fm += "expires_at: " + expires.Format(time.RFC3339) + "\n"
	}
	fm += "---\n\n# Note\n\nbody\n"
	if err := os.WriteFile(path, []byte(fm), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func sessionFiles(t *testing.T, pb *brain.ProjectObsidian) []string {
	t.Helper()
	dir := filepath.Join(pb.GetBrainDir(), "90-sessions", "compact")
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

func contains(list []string, value string) bool {
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}

// The vault's own navigation must survive retention. A `debug/index.md`
// classifies as an ephemeral "debug" note by type; expiring it would delete the
// structure the rest of the vault links to.
func TestStructuralNotesNeverExpire(t *testing.T) {
	pb := testVault(t)
	h := New(DefaultConfig(), pb, nil)

	past := time.Now().Add(-100 * 24 * time.Hour)
	structural := []string{
		"debug/index.md",
		"context/index.md",
		"templates/decision-template.md",
		"instructions/obsidian-law.md",
		"maps/project-map.md",
	}
	for _, rel := range structural {
		writeManaged(t, pb, rel, "debug", "ephemeral", past, past.Add(time.Hour))
	}

	h.Run(Deep)

	for _, rel := range structural {
		if _, err := os.Stat(filepath.Join(pb.GetBrainDir(), filepath.FromSlash(rel))); err != nil {
			t.Fatalf("structural note %s was deleted by retention", rel)
		}
	}
}

// A session whose knowledge cannot be fully promoted must be kept: losing
// knowledge is worse than keeping one stale note for another cycle (spec §24).
func TestSessionSurvivesWhenPromotionIsDisabled(t *testing.T) {
	pb := testVault(t)
	cfg := DefaultConfig()
	cfg.KeepLatestSessions = 1
	cfg.ExtractReusableKnowledge = false
	h := New(cfg, pb, nil)

	base := time.Now()
	writeSession(t, pb, "new.md", base, time.Time{}, sessionBody("recent"))
	writeSession(t, pb, "old.md", base.Add(-time.Hour), time.Time{},
		sessionBody("old", "## Decisions", "- something must be true"))

	report := h.Run(Deep)
	// With extraction disabled the note is still removed (the operator asked
	// for that), but nothing is reported as promoted — the report must not
	// claim knowledge was saved when it was not.
	if len(report.KnowledgePromoted) != 0 {
		t.Fatalf("nothing may be reported as promoted when extraction is off: %v", report.KnowledgePromoted)
	}
	constraints, _ := pb.ReadCanonical("active-constraints")
	if strings.Contains(constraints.Body, "something must be true") {
		t.Fatal("extraction was disabled, so nothing should have been written")
	}
}

// TestMarkStateIsAtomic pins the crash-safety contract (Fine-Tuning §32):
// the state flip must never leave a truncated or half-written note behind, and
// must preserve a user's existing restrictive permissions.
func TestMarkStateIsAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session-note.md")
	body := "---\ntype: session\nstate: active\n---\n\nbody line\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := markState(path, brain.NoteStale); err != nil {
		t.Fatalf("markState failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "state: stale") || !strings.Contains(string(data), "body line") {
		t.Fatalf("state flip lost content: %s", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Fatalf("mode = %#o, want %#o", got, want)
	}
}

func TestRunContextParticipatesInVaultLease(t *testing.T) {
	h := New(DefaultConfig(), nil, nil)
	leaseEntered := make(chan struct{})
	leaseRelease := make(chan struct{})
	h.SetRunLease(func() func() {
		close(leaseEntered)
		<-leaseRelease
		return func() {}
	})

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan Report, 1)
	go func() { result <- h.RunContext(ctx, Deep) }()
	select {
	case <-leaseEntered:
	case <-time.After(time.Second):
		t.Fatal("housekeeper pass did not acquire the vault lease")
	}
	select {
	case <-result:
		t.Fatal("housekeeper pass returned before the lease was granted")
	default:
	}
	cancel()
	close(leaseRelease)
	select {
	case report := <-result:
		if report.Skipped != "cancelled" {
			t.Fatalf("cancelled report skipped = %q, want cancelled", report.Skipped)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled housekeeper pass did not return")
	}
}

func TestStopContextWaitsForStartupPass(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RunOnStartup = true
	h := New(cfg, nil, nil)
	leaseEntered := make(chan struct{})
	leaseRelease := make(chan struct{})
	h.SetRunLease(func() func() {
		close(leaseEntered)
		<-leaseRelease
		return func() {}
	})
	h.Start()
	select {
	case <-leaseEntered:
	case <-time.After(time.Second):
		t.Fatal("startup pass did not begin")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- h.StopContext(ctx) }()
	select {
	case err := <-stopped:
		t.Fatalf("StopContext returned before startup pass drained: %v", err)
	default:
	}
	close(leaseRelease)
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("StopContext error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("StopContext did not finish after the startup pass exited")
	}
}

func TestOnSessionCloseWithLeaseHeldSkipsRunLease(t *testing.T) {
	h := New(DefaultConfig(), nil, nil)
	leaseCalls := 0
	h.SetRunLease(func() func() {
		leaseCalls++
		return func() {}
	})

	report := h.OnSessionCloseWithLeaseHeld()
	if report.Depth != Light {
		t.Fatalf("report depth = %q, want %q", report.Depth, Light)
	}
	if leaseCalls != 0 {
		t.Fatalf("a caller-held lease must not be acquired again; calls = %d", leaseCalls)
	}
}
