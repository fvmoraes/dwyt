package brain

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Migration to the v5 vault layout (spec §60).
//
// The whole design here follows one constraint from §60: **preserve data and
// avoid regressions**. Concretely:
//
//   - Nothing is deleted. Pre-v5 notes are *copied* into the numbered areas and
//     the originals are left in place until the migration has been confirmed.
//   - Knowledge is extracted before anything is trimmed. A session that falls
//     outside the 100-session window is compiled first, so its decisions and
//     error patterns survive in canonical memory (spec §60.2).
//   - The pass is idempotent. Running it twice changes nothing the second time,
//     which is what makes it safe to run on every startup.
//   - A vault it cannot interpret is reported, not repaired. Guessing at
//     unfamiliar content is how migrations lose data.

// V5MigrationStatus is the outcome for one migrated item.
type V5MigrationStatus string

const (
	V5AlreadyMigrated V5MigrationStatus = "already_migrated"
	V5Migrated        V5MigrationStatus = "migrated"
	V5Compiled        V5MigrationStatus = "compiled"
	V5Skipped         V5MigrationStatus = "skipped"
	V5Failed          V5MigrationStatus = "failed"
)

// V5MigrationItem records what happened to one note or area.
type V5MigrationItem struct {
	Source string            `json:"source"`
	Target string            `json:"target,omitempty"`
	Status V5MigrationStatus `json:"status"`
	Reason string            `json:"reason,omitempty"`
}

// V5MigrationReport summarizes a migration pass.
type V5MigrationReport struct {
	VaultDir string `json:"vault_dir"`
	DryRun   bool   `json:"dry_run,omitempty"`

	// LayoutCreated is true when the numbered areas did not exist before.
	LayoutCreated bool `json:"layout_created"`
	// CanonicalSeeded counts canonical notes created from pre-v5 content.
	CanonicalSeeded int `json:"canonical_seeded"`
	// SessionsConverted counts pre-v5 context notes copied into 90-sessions.
	SessionsConverted int `json:"sessions_converted"`
	// SessionsCompiled counts sessions whose knowledge was promoted because they
	// fall outside the retention window.
	SessionsCompiled int `json:"sessions_compiled"`
	// KnowledgePromoted lists what was rescued.
	KnowledgePromoted []string `json:"knowledge_promoted,omitempty"`
	// LifecycleBackfilled counts pre-v5 notes given lifecycle metadata.
	LifecycleBackfilled int `json:"lifecycle_backfilled"`
	// RawMigrated counts legacy raw/ payloads registered for later pruning.
	RawMigrated int `json:"raw_migrated"`

	Items  []V5MigrationItem `json:"items,omitempty"`
	Errors []string          `json:"errors,omitempty"`
	// Confirmed is true when the pass completed without errors, which is the
	// signal a caller needs before it may consider removing legacy copies.
	Confirmed bool `json:"confirmed"`
}

// V5MigrationOptions tunes a pass.
type V5MigrationOptions struct {
	// DryRun computes the report without writing anything.
	DryRun bool
	// KeepLatestSessions is the retention window used to decide which pre-v5
	// sessions get compiled. Zero uses 100 (spec §21).
	KeepLatestSessions int
	// BackfillLifecycle gives pre-v5 notes lifecycle metadata.
	//
	// Off by default and opt-in, because it rewrites the frontmatter of notes the
	// user may have authored, and a migration should not silently take ownership
	// of user content.
	BackfillLifecycle bool
}

// legacyToCanonical maps pre-v5 root notes onto canonical keys. Only notes whose
// meaning is unambiguous are mapped; everything else is left where it is.
//
// decisions.md is deliberately absent: the decision log is append-only and lives
// at 20-decisions/index.md rather than in canonicalLayout, so it is handled by
// migrateLegacyDecisions instead of UpsertCanonical.
var legacyToCanonical = map[string]string{
	"tasks.md": "current-task",
}

// MigrateToV5 brings a vault to the v5 layout.
//
// It is safe to call on an already-migrated vault and on a vault that was never
// touched by DWYT: both are no-ops beyond creating the empty areas.
func (pb *ProjectObsidian) MigrateToV5(opts V5MigrationOptions) V5MigrationReport {
	report := V5MigrationReport{VaultDir: pb.GetBrainDir(), DryRun: opts.DryRun}
	keep := opts.KeepLatestSessions
	if keep <= 0 {
		keep = 100
	}

	// 1. The numbered layout. EnsureCanonicalLayout is additive and idempotent.
	if _, err := os.Stat(filepath.Join(pb.GetBrainDir(), string(AreaProject))); err != nil {
		report.LayoutCreated = true
	}
	if !opts.DryRun {
		if err := pb.EnsureCanonicalLayout(); err != nil {
			report.Errors = append(report.Errors, "layout: "+err.Error())
			return report
		}
	}

	pb.migrateLegacyRootNotes(&report, opts)
	pb.migrateLegacySessions(&report, opts, keep)
	if opts.BackfillLifecycle {
		pb.backfillLifecycle(&report, opts)
	}
	pb.registerLegacyRaw(&report, opts)

	report.Confirmed = len(report.Errors) == 0
	return report
}

// migrateLegacyRootNotes copies the pre-v5 root notes into canonical memory.
//
// It copies rather than moves: the originals stay until the caller confirms the
// migration, and the pre-v5 notes already tell the reader they are legacy
// pointers.
func (pb *ProjectObsidian) migrateLegacyRootNotes(report *V5MigrationReport, opts V5MigrationOptions) {
	dir := pb.GetBrainDir()
	for legacy, key := range legacyToCanonical {
		source := filepath.Join(dir, legacy)
		content := readFileString(source)
		if strings.TrimSpace(content) == "" {
			continue
		}
		body := strings.TrimSpace(bodyOf(content))
		// The pre-v5 seeds are pure pointers ("This legacy root note points to
		// ..."), which carry no knowledge and must not become canonical content.
		if body == "" || strings.Contains(body, "legacy root note points to") {
			report.Items = append(report.Items, V5MigrationItem{
				Source: legacy, Status: V5Skipped, Reason: "legacy pointer note with no content",
			})
			continue
		}
		if existing, ok := pb.ReadCanonical(key); ok && strings.Contains(existing.Body, body) {
			report.Items = append(report.Items, V5MigrationItem{
				Source: legacy, Target: key, Status: V5AlreadyMigrated,
			})
			continue
		}
		if opts.DryRun {
			report.CanonicalSeeded++
			report.Items = append(report.Items, V5MigrationItem{Source: legacy, Target: key, Status: V5Migrated})
			continue
		}
		merged := body
		if existing, ok := pb.ReadCanonical(key); ok && strings.TrimSpace(existing.Body) != "" {
			merged = strings.TrimRight(existing.Body, "\n") + "\n\n## Migrated from " + legacy + "\n\n" + body
		}
		if _, err := pb.UpsertCanonical(key, "", merged, SourceRef{}); err != nil {
			report.Errors = append(report.Errors, legacy+": "+err.Error())
			report.Items = append(report.Items, V5MigrationItem{Source: legacy, Target: key, Status: V5Failed, Reason: err.Error()})
			continue
		}
		report.CanonicalSeeded++
		report.Items = append(report.Items, V5MigrationItem{Source: legacy, Target: key, Status: V5Migrated})
	}

	// Pre-v5 decisions lived in decisions.md and decisions/index.md. Both fold
	// into the append-only canonical log, deduplicated.
	for _, legacy := range []string{"decisions.md", filepath.Join("decisions", "index.md")} {
		pb.migrateLegacyDecisions(report, opts, legacy)
	}
}

// migrateLegacyDecisions folds the bullets of a pre-v5 decisions note into the
// canonical append-only log.
func (pb *ProjectObsidian) migrateLegacyDecisions(report *V5MigrationReport, opts V5MigrationOptions, legacy string) {
	content := readFileString(filepath.Join(pb.GetBrainDir(), legacy))
	if strings.TrimSpace(content) == "" {
		return
	}
	rel := filepath.ToSlash(legacy)

	promoted := 0
	for _, line := range strings.Split(bodyOf(content), "\n") {
		trimmed := strings.TrimSpace(line)
		// Accept bullets, "### <heading>" entries and plain prose lines, since
		// the pre-v5 format was never enforced.
		text := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(trimmed, "- "), "### "))
		switch {
		case text == "":
			continue
		case strings.HasPrefix(text, "[["), strings.HasPrefix(text, "Links:"):
			// The seed's navigation line is not a decision.
			continue
		case strings.HasPrefix(text, "#"), strings.HasPrefix(text, "---"):
			continue
		case looksLikeTimestamp(text):
			// The pre-v5 log used bare timestamps as separators.
			continue
		case strings.Contains(text, "legacy root note points to"):
			continue
		}
		if opts.DryRun {
			promoted++
			continue
		}
		added, err := pb.appendDecisionLog(text)
		if err != nil {
			report.Errors = append(report.Errors, rel+": "+err.Error())
			continue
		}
		if added {
			promoted++
		}
	}
	if promoted == 0 {
		report.Items = append(report.Items, V5MigrationItem{
			Source: rel, Status: V5AlreadyMigrated,
			Reason: "no new decisions to fold in",
		})
		return
	}
	report.CanonicalSeeded += promoted
	report.Items = append(report.Items, V5MigrationItem{
		Source: rel, Target: "decisions", Status: V5Migrated,
		Reason: fmt.Sprintf("%d decision(s) folded into the canonical log", promoted),
	})
}

// looksLikeTimestamp reports whether a line is only a date/time heading, which
// the pre-v5 decision log used as a separator.
func looksLikeTimestamp(s string) bool {
	trimmed := strings.Trim(s, "*_ ")
	if len(trimmed) < 8 || len(trimmed) > 32 {
		return false
	}
	digits := 0
	for _, r := range trimmed {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case r == '-' || r == ':' || r == ' ' || r == 'T' || r == 'Z' || r == '+' || r == '.':
		default:
			return false
		}
	}
	return digits >= 6
}

// migrateLegacySessions converts pre-v5 context notes into the v5 session area
// and compiles the ones outside the retention window (spec §60.2).
func (pb *ProjectObsidian) migrateLegacySessions(report *V5MigrationReport, opts V5MigrationOptions, keep int) {
	legacyDir := filepath.Join(pb.GetBrainDir(), "context")
	entries, err := os.ReadDir(legacyDir)
	if err != nil {
		return
	}

	type legacySession struct {
		name    string
		path    string
		modTime time.Time
		content string
	}
	var sessions []legacySession
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || e.Name() == "index.md" {
			continue
		}
		path := filepath.Join(legacyDir, e.Name())
		info, err := e.Info()
		if err != nil {
			continue
		}
		sessions = append(sessions, legacySession{
			name: e.Name(), path: path, modTime: info.ModTime(), content: readFileString(path),
		})
	}
	if len(sessions) == 0 {
		return
	}
	// Newest first: the retention window keeps the most recent.
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].modTime.After(sessions[j].modTime) })

	targetDir := filepath.Join(pb.GetBrainDir(), filepath.FromSlash(snapshotDir))
	for i, s := range sessions {
		snapshot := legacySnapshotFromNote(s.content)
		if snapshot.Empty() {
			report.Items = append(report.Items, V5MigrationItem{
				Source: "context/" + s.name, Status: V5Skipped,
				Reason: "no extractable task state",
			})
			continue
		}

		// Outside the window: compile the knowledge, do not convert the note.
		// The original stays on disk, so nothing is lost even though it will not
		// appear as a v5 session.
		if i >= keep {
			if opts.DryRun {
				report.SessionsCompiled++
				report.Items = append(report.Items, V5MigrationItem{
					Source: "context/" + s.name, Status: V5Compiled,
					Reason: "beyond the retention window",
				})
				continue
			}
			result := pb.Compile(snapshot)
			report.KnowledgePromoted = append(report.KnowledgePromoted, result.Promoted...)
			report.Errors = append(report.Errors, result.Errors...)
			report.SessionsCompiled++
			report.Items = append(report.Items, V5MigrationItem{
				Source: "context/" + s.name, Status: V5Compiled,
				Reason: "beyond the retention window; knowledge promoted",
			})
			continue
		}

		if opts.DryRun {
			report.SessionsConverted++
			report.Items = append(report.Items, V5MigrationItem{
				Source: "context/" + s.name, Target: snapshotDir, Status: V5Migrated,
			})
			continue
		}
		outcome, err := pb.SaveCompactSnapshot(snapshot)
		if err != nil {
			report.Errors = append(report.Errors, "context/"+s.name+": "+err.Error())
			report.Items = append(report.Items, V5MigrationItem{
				Source: "context/" + s.name, Status: V5Failed, Reason: err.Error(),
			})
			continue
		}
		status := V5Migrated
		if !outcome.Written {
			// An identical state already exists in v5 form: the note was already
			// migrated on a previous pass.
			status = V5AlreadyMigrated
		} else {
			report.SessionsConverted++
		}
		report.Items = append(report.Items, V5MigrationItem{
			Source: "context/" + s.name, Target: strings.TrimPrefix(outcome.Path, targetDir), Status: status,
		})
	}
}

// legacySnapshotFromNote parses a pre-v5 rich context note into a compact
// snapshot. Sections the pre-v5 format used are mapped onto the v2 fields;
// commands and free prose are dropped, which is the point of the conversion.
func legacySnapshotFromNote(content string) CompactSnapshot {
	sections := map[string]string{}
	current := ""
	var body []string
	flush := func() {
		if current != "" {
			sections[current] = strings.TrimSpace(strings.Join(body, "\n"))
		}
		body = nil
	}
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "## ") {
			flush()
			current = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "## ")))
			continue
		}
		if current != "" {
			body = append(body, line)
		}
	}
	flush()

	bullets := func(keys ...string) []string {
		for _, key := range keys {
			raw, ok := sections[key]
			if !ok {
				continue
			}
			var out []string
			for _, line := range strings.Split(raw, "\n") {
				line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "-"))
				if line != "" {
					out = append(out, line)
				}
			}
			if len(out) > 0 {
				return out
			}
		}
		return nil
	}

	objective := firstLine(firstNonEmptyString(sections["summary"], sections["user request"]))
	return CompactSnapshot{
		Objective:     objective,
		Status:        normalizeSnapshotStatus(sections["outcome"]),
		AffectedFiles: bullets("files"),
		Decisions:     bullets("decisions"),
		ActiveErrors:  bullets("errors"),
		NextSteps:     bullets("next steps"),
		// Raw references must survive the conversion or the housekeeper will
		// prune objects the snapshot still cites. Scan every free-form section,
		// since the pre-v5 format put them wherever the agent chose.
		RawRefs: extractRawRefs(
			sections["context for future agents"],
			sections["context"],
			sections["summary"],
			sections["outcome"],
		),
		Context: sections["context for future agents"],
	}
}

// backfillLifecycle gives pre-v5 notes lifecycle metadata so the housekeeper can
// govern them.
//
// Opt-in on purpose: it rewrites frontmatter, and a note without DWYT metadata
// might be one the user wrote. Marking it managed hands its retention to the
// housekeeper, which must be a deliberate choice.
func (pb *ProjectObsidian) backfillLifecycle(report *V5MigrationReport, opts V5MigrationOptions) {
	dir := pb.GetBrainDir()
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		if filepath.Base(path) == "context.md" {
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		// Structural notes are excluded: they must never expire, and giving them
		// a TTL-bearing lifecycle would be a footgun.
		if filepath.Base(rel) == "index.md" ||
			strings.HasPrefix(rel, "templates/") ||
			strings.HasPrefix(rel, "instructions/") ||
			strings.HasPrefix(rel, "maps/") {
			return nil
		}

		content := readFileString(path)
		if ParseLifecycle(content).Managed {
			return nil
		}
		if opts.DryRun {
			report.LifecycleBackfilled++
			return nil
		}
		noteType := detectType(dir, path)
		lc := NewLifecycle(noteType, info.ModTime())
		// The note predates v5; treat its mtime as its creation time so recency
		// ranking and TTLs are measured from something real.
		lc.CreatedAt = info.ModTime()
		lc.UpdatedAt = info.ModTime()
		if ttl := TTLFor(noteType); ttl > 0 {
			lc.ExpiresAt = info.ModTime().Add(ttl)
		}

		updated, ok := injectFrontmatter(content, lc.Render())
		if !ok {
			report.Items = append(report.Items, V5MigrationItem{
				Source: rel, Status: V5Skipped, Reason: "no frontmatter to extend",
			})
			return nil
		}
		if writeErr := os.WriteFile(path, []byte(updated), 0644); writeErr != nil {
			report.Errors = append(report.Errors, rel+": "+writeErr.Error())
			return nil
		}
		report.LifecycleBackfilled++
		return nil
	})
}

// injectFrontmatter adds lifecycle lines to a note's frontmatter, or creates a
// frontmatter block when the note has none.
func injectFrontmatter(content, lifecycle string) (string, bool) {
	if strings.TrimSpace(lifecycle) == "" {
		return content, false
	}
	fm, ok := frontmatterOf(content)
	if !ok {
		// A note with no frontmatter at all: give it one rather than skipping,
		// since the caller explicitly asked for a backfill.
		return "---\n" + lifecycle + "---\n\n" + strings.TrimLeft(content, "\n"), true
	}
	idx := strings.Index(content, fm)
	if idx < 0 {
		return content, false
	}
	merged := strings.TrimRight(fm, "\n") + "\n" + strings.TrimRight(lifecycle, "\n")
	return content[:idx] + merged + content[idx+len(fm):], true
}

// registerLegacyRaw reports pre-v5 raw/ payloads.
//
// They are counted and left in place, never moved: spec §29 requires legacy raw
// to stay readable and to be excluded from the default search, both of which are
// already true (the search filters by note type, and raw/ holds no .md notes).
// Moving bytes during a migration buys nothing and risks losing a citation.
func (pb *ProjectObsidian) registerLegacyRaw(report *V5MigrationReport, _ V5MigrationOptions) {
	rawDir := filepath.Join(pb.GetBrainDir(), "raw")
	entries, err := os.ReadDir(rawDir)
	if err != nil {
		return
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			count++
		}
	}
	if count == 0 {
		return
	}
	report.RawMigrated = count
	report.Items = append(report.Items, V5MigrationItem{
		Source: "raw/", Status: V5Skipped,
		Reason: fmt.Sprintf("%d legacy raw payload(s) left in place and excluded from search", count),
	})
}
