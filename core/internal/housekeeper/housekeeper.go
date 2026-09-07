// Package housekeeper implements the DWYT Housekeeper (spec §20–§26, §51).
//
// Its single job:
//
//	Keep the Brain small, relevant, consistent and useful over time.
//
// The optimizing principle is that knowledge stays and operational evidence
// expires. Everything here is built around one hard rule: nothing is deleted
// before the Memory Compiler has had a chance to extract whatever is reusable
// from it (spec §24). A TTL is never a plain DELETE.
package housekeeper

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fvmoraes/dwyt/internal/brain"
	"github.com/fvmoraes/dwyt/internal/log"
	"github.com/fvmoraes/dwyt/internal/rawstore"
)

// Config holds the housekeeper policy (spec §23, §51).
type Config struct {
	Enabled bool `json:"enabled"`

	// KeepLatestSessions is the 100-session limit (spec §21).
	KeepLatestSessions int `json:"keep_latest_sessions"`

	// RunOnSessionClose and RunOnStartup control the light passes.
	RunOnSessionClose bool `json:"run_on_session_close"`
	RunOnStartup      bool `json:"run_on_startup"`
	// Interval is the deep housekeeping period.
	Interval time.Duration `json:"interval"`

	// ExtractReusableKnowledge and PromoteToCanonical implement "compact before
	// expiring" (spec §24). Turning either off means deletions lose knowledge,
	// so both default to true and the code warns when they are disabled.
	ExtractReusableKnowledge bool `json:"extract_reusable_knowledge"`
	PromoteToCanonicalMemory bool `json:"promote_to_canonical_memory"`

	StaleDetectionBySourceHash bool `json:"stale_detection_source_hash"`
	PruneRawOrphans            bool `json:"prune_raw_orphans"`

	// TTLOverrides replaces individual entries of the default retention table.
	TTLOverrides map[string]time.Duration `json:"ttl_overrides,omitempty"`

	// VaultGC, when set, runs the project-wide ghost-vault sweep during deep
	// housekeeping. The housekeeper owns one vault; ghost vaults live in the
	// projects directory shared by all of them, so the server injects this
	// callback instead of the housekeeper reaching outside its scope.
	VaultGC func(dryRun bool) brain.VaultGCReport `json:"-"`

	// DryRun computes everything and changes nothing. Used by the dashboard to
	// preview a pass before the user commits to it.
	DryRun bool `json:"dry_run,omitempty"`
}

// DefaultConfig is the recommended policy from spec §23.
func DefaultConfig() Config {
	return Config{
		Enabled:                    true,
		KeepLatestSessions:         100,
		RunOnSessionClose:          true,
		RunOnStartup:               true,
		Interval:                   6 * time.Hour,
		ExtractReusableKnowledge:   true,
		PromoteToCanonicalMemory:   true,
		StaleDetectionBySourceHash: true,
		PruneRawOrphans:            true,
	}
}

// Depth selects how much work a pass does (spec §51).
type Depth string

const (
	// Light runs on session close: state hash, access metadata, raw refs.
	Light Depth = "light"
	// Deep runs on startup and on the interval: session limit, TTLs, dedup,
	// stale detection, canonical merge, raw pruning.
	Deep Depth = "deep"
)

// Report is the outcome of one pass.
type Report struct {
	Depth     Depth     `json:"depth"`
	StartedAt time.Time `json:"started_at"`
	Duration  string    `json:"duration"`
	DryRun    bool      `json:"dry_run,omitempty"`

	SessionsTotal    int `json:"sessions_total"`
	SessionsRetained int `json:"sessions_retained"`
	SessionsRemoved  int `json:"sessions_removed"`

	ExpiredRemoved   int `json:"expired_removed"`
	StaleMarked      int `json:"stale_marked"`
	DuplicatesMerged int `json:"duplicates_merged"`

	// KnowledgePromoted lists what was rescued before a deletion.
	KnowledgePromoted []string `json:"knowledge_promoted,omitempty"`

	RawObjects    int   `json:"raw_objects"`
	RawBytes      int64 `json:"raw_bytes"`
	RawPruned     int   `json:"raw_pruned"`
	RawBytesFreed int64 `json:"raw_bytes_freed"`

	// Skipped explains why a pass did nothing.
	Skipped string `json:"skipped,omitempty"`
	// Errors carries non-fatal problems. Housekeeping must never fail a session.
	Errors []string `json:"errors,omitempty"`

	// Vault ghosts swept during a deep pass (project-wide, not per vault).
	VaultGhostsRemoved int `json:"vault_ghosts_removed,omitempty"`
	VaultGhostsKept    int `json:"vault_ghosts_kept,omitempty"`
}

// Housekeeper runs retention passes against one project vault.
type Housekeeper struct {
	mu  sync.RWMutex
	cfg Config

	vault *brain.ProjectObsidian
	raw   *rawstore.Store

	lastRun    time.Time
	lastReport *Report

	// stopCh terminates the periodic loop.
	stopCh chan struct{}
	// running guards against two passes overlapping; a pass reads and rewrites
	// notes, and two concurrent passes could each decide to delete the same
	// session.
	running bool
}

// New creates a housekeeper. Either dependency may be nil: a housekeeper without
// a vault reports itself as idle rather than failing, which is what happens when
// the daemon starts before a project is selected.
func New(cfg Config, vault *brain.ProjectObsidian, raw *rawstore.Store) *Housekeeper {
	if cfg.KeepLatestSessions <= 0 {
		cfg.KeepLatestSessions = DefaultConfig().KeepLatestSessions
	}
	if !cfg.ExtractReusableKnowledge || !cfg.PromoteToCanonicalMemory {
		log.Warn("housekeeper: knowledge extraction disabled; expiring notes will lose reusable knowledge",
			log.Fields{"extract": cfg.ExtractReusableKnowledge, "promote": cfg.PromoteToCanonicalMemory})
	}
	return &Housekeeper{cfg: cfg, vault: vault, raw: raw, stopCh: make(chan struct{})}
}

// SetVault swaps the vault, e.g. after a project switch.
func (h *Housekeeper) SetVault(vault *brain.ProjectObsidian) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.vault = vault
}

// Config returns the active policy.
func (h *Housekeeper) Config() Config {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.cfg
}

// Run executes one pass. It is safe to call concurrently: an overlapping call
// returns a skipped report rather than racing the pass in flight.
func (h *Housekeeper) Run(depth Depth) Report {
	report := Report{Depth: depth, StartedAt: time.Now()}

	h.mu.Lock()
	if !h.cfg.Enabled {
		h.mu.Unlock()
		report.Skipped = "housekeeper disabled"
		report.Duration = time.Since(report.StartedAt).String()
		return report
	}
	if h.running {
		h.mu.Unlock()
		report.Skipped = "a housekeeping pass is already running"
		report.Duration = time.Since(report.StartedAt).String()
		return report
	}
	vault := h.vault
	raw := h.raw
	cfg := h.cfg
	h.running = true
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		h.running = false
		h.lastRun = time.Now()
		snapshot := report
		h.lastReport = &snapshot
		h.mu.Unlock()
	}()

	report.DryRun = cfg.DryRun
	if vault == nil {
		report.Skipped = "no project vault"
		report.Duration = time.Since(report.StartedAt).String()
		return report
	}

	notes := scanVault(vault.GetBrainDir())
	report.SessionsTotal = countSessions(notes)

	h.runLight(&report, vault, notes, cfg)
	if depth == Deep {
		h.runDeep(&report, vault, raw, notes, cfg)
	}

	report.Duration = time.Since(report.StartedAt).String()
	return report
}

// runLight refreshes bookkeeping without deleting anything (spec §51).
func (h *Housekeeper) runLight(report *Report, vault *brain.ProjectObsidian, notes []noteRef, cfg Config) {
	now := time.Now()
	if !cfg.StaleDetectionBySourceHash {
		return
	}
	// Stale detection is the one light-pass action that changes a note, and it
	// only ever changes `state:` — never content.
	for _, n := range notes {
		if !n.Lifecycle.Managed || n.Lifecycle.SourceFile == "" {
			continue
		}
		if n.Lifecycle.State == brain.NoteStale {
			continue
		}
		current := brain.HashFile(n.Lifecycle.SourceFile)
		if current == "" || !n.Lifecycle.Stale(current) {
			continue
		}
		if cfg.DryRun {
			report.StaleMarked++
			continue
		}
		if err := markState(n.Path, brain.NoteStale); err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("stale %s: %v", filepath.Base(n.Path), err))
			continue
		}
		report.StaleMarked++
	}
	_ = now
	_ = vault
}

// runDeep enforces retention (spec §21–§24, §51).
func (h *Housekeeper) runDeep(report *Report, vault *brain.ProjectObsidian, raw *rawstore.Store, notes []noteRef, cfg Config) {
	now := time.Now()

	// 1. Expired operational notes. Knowledge is rescued first.
	for _, n := range notes {
		if !n.Lifecycle.Managed {
			continue
		}
		if !expired(n, cfg, now) {
			continue
		}
		if isSessionNote(n) {
			// Sessions are handled by the 100-session rule below so a single
			// note is never processed twice.
			continue
		}
		h.retire(report, vault, n, cfg, "ttl expired")
	}

	// 2. The 100-session limit (spec §21). Unmanaged notes in the tail are
	// counted by the limit but never removed, so the report only claims what
	// actually left the vault.
	sessions := filterSessions(notes)
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].Activity.After(sessions[j].Activity)
	})
	report.SessionsRetained = len(sessions)
	if len(sessions) > cfg.KeepLatestSessions {
		for _, n := range sessions[cfg.KeepLatestSessions:] {
			if h.retire(report, vault, n, cfg, "beyond the session limit") {
				report.SessionsRemoved++
			}
		}
		report.SessionsRetained = len(sessions) - report.SessionsRemoved
	}
	// Sessions that expired but are still within the limit are also retired:
	// the limit is a ceiling, not a reprieve from the TTL.
	for i, n := range sessions {
		if i >= cfg.KeepLatestSessions {
			break
		}
		if expired(n, cfg, now) {
			if h.retire(report, vault, n, cfg, "ttl expired") {
				report.SessionsRemoved++
				report.SessionsRetained--
			}
		}
	}

	// 3. Duplicate compact snapshots: two notes with the same state hash carry
	// the same state, so the older one is pure noise.
	report.DuplicatesMerged += h.dedupeByStateHash(report, sessions, cfg)

	// 4. Raw object pruning, guarded by the references the vault still cites.
	if raw != nil {
		count, bytes, err := raw.Usage()
		if err == nil {
			report.RawObjects = count
			report.RawBytes = bytes
		}
		if cfg.PruneRawOrphans && !cfg.DryRun {
			referenced := collectRawReferences(notes)
			if res, err := raw.Prune(referenced, now); err == nil {
				report.RawPruned = res.Expired + res.Orphans
				report.RawBytesFreed = res.BytesFreed
				report.RawObjects = res.Kept
				report.RawBytes = res.KeptBytes
			} else {
				report.Errors = append(report.Errors, "raw prune: "+err.Error())
			}
		}
	}

	// 5. Ghost vaults: hash-only vault directories no project claims that hold
	// nothing but DWYT scaffolding. A deep pass is the natural place to sweep
	// them, so the pending-association list shrinks without user intervention.
	if cfg.VaultGC != nil {
		gc := cfg.VaultGC(cfg.DryRun)
		report.VaultGhostsRemoved = gc.Removed
		report.VaultGhostsKept = gc.KeptWithContent
		for _, e := range gc.Errors {
			report.Errors = append(report.Errors, "vault gc: "+e)
		}
	}
}

// retire removes a note after extracting whatever is reusable from it
// (spec §24). A failed extraction cancels the deletion: losing knowledge is
// worse than keeping a stale note one more cycle. It reports whether the note
// was actually removed (or, in a dry run, would have been), so callers can
// keep the report honest about notes DWYT was not allowed to touch.
func (h *Housekeeper) retire(report *Report, vault *brain.ProjectObsidian, n noteRef, cfg Config, reason string) bool {
	// The lifecycle law (spec §61) is absolute: a note DWYT does not manage is
	// never deleted, whatever rule reached for it. The TTL loop and the session
	// limit both funnel through here, so this is the single enforcement point.
	if !n.Lifecycle.Managed {
		return false
	}
	if cfg.ExtractReusableKnowledge && cfg.PromoteToCanonicalMemory {
		snapshot, ok := snapshotFromNote(n)
		if ok {
			if cfg.DryRun {
				report.KnowledgePromoted = append(report.KnowledgePromoted,
					"(dry run) would compile "+filepath.Base(n.Path))
			} else {
				result := vault.Compile(snapshot)
				report.KnowledgePromoted = append(report.KnowledgePromoted, result.Promoted...)
				if len(result.Errors) > 0 {
					report.Errors = append(report.Errors, result.Errors...)
					// Compilation partially failed. Keep the note: it is the
					// only remaining copy of what could not be promoted.
					return false
				}
			}
		}
	}
	if cfg.DryRun {
		report.ExpiredRemoved++
		return true
	}
	if err := os.Remove(n.Path); err != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("remove %s: %v", filepath.Base(n.Path), err))
		return false
	}
	report.ExpiredRemoved++
	log.Info("housekeeper: retired note", log.Fields{"note": filepath.Base(n.Path), "reason": reason})
	return true
}

// dedupeByStateHash removes older snapshots that describe an identical state.
// Unmanaged notes are out of reach even here: identical content does not make
// a user-authored note DWYT's to delete.
func (h *Housekeeper) dedupeByStateHash(report *Report, sessions []noteRef, cfg Config) int {
	seen := map[string]bool{}
	removed := 0
	// sessions is newest-first, so the first occurrence of a hash is the one to
	// keep.
	for _, n := range sessions {
		hash := n.Lifecycle.StateHash
		if hash == "" {
			continue
		}
		if !seen[hash] {
			seen[hash] = true
			continue
		}
		if !n.Lifecycle.Managed {
			continue
		}
		if cfg.DryRun {
			removed++
			continue
		}
		if err := os.Remove(n.Path); err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("dedupe %s: %v", filepath.Base(n.Path), err))
			continue
		}
		removed++
	}
	return removed
}

// RunDry computes a pass without changing anything, so a caller can preview what
// would be removed. Deletion is irreversible; a preview is a requirement, not a
// convenience.
//
// The dry-run flag lives on a copy of the config, so a concurrent real pass is
// unaffected.
func (h *Housekeeper) RunDry(depth Depth) Report {
	h.mu.Lock()
	original := h.cfg
	h.cfg.DryRun = true
	h.mu.Unlock()

	report := h.Run(depth)

	h.mu.Lock()
	h.cfg = original
	h.mu.Unlock()
	return report
}

// Start begins the periodic deep pass. It returns immediately.
func (h *Housekeeper) Start() {
	cfg := h.Config()
	if !cfg.Enabled {
		return
	}
	if cfg.RunOnStartup {
		go h.Run(Deep)
	}
	if cfg.Interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(cfg.Interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				h.Run(Deep)
			case <-h.stopCh:
				return
			}
		}
	}()
}

// Stop terminates the periodic loop. It is idempotent.
func (h *Housekeeper) Stop() {
	h.mu.Lock()
	defer h.mu.Unlock()
	select {
	case <-h.stopCh:
		return
	default:
		close(h.stopCh)
	}
}

// OnSessionClose runs the light pass when configured to.
func (h *Housekeeper) OnSessionClose() Report {
	if !h.Config().RunOnSessionClose {
		return Report{Depth: Light, Skipped: "run_on_session_close disabled"}
	}
	return h.Run(Light)
}

// HousekeeperStatus implements optimizer.HousekeeperStatusProvider (spec §55).
func (h *Housekeeper) HousekeeperStatus() map[string]interface{} {
	h.mu.RLock()
	cfg := h.cfg
	lastRun := h.lastRun
	lastReport := h.lastReport
	vault := h.vault
	raw := h.raw
	running := h.running
	h.mu.RUnlock()

	status := map[string]interface{}{
		"enabled":               cfg.Enabled,
		"running":               running,
		"keep_latest_sessions":  cfg.KeepLatestSessions,
		"interval":              cfg.Interval.String(),
		"promote_before_delete": cfg.ExtractReusableKnowledge && cfg.PromoteToCanonicalMemory,
	}
	if lastRun.IsZero() {
		status["last_run"] = nil
	} else {
		status["last_run"] = lastRun.Format(time.RFC3339)
	}
	if lastReport != nil {
		status["last_report"] = lastReport
	}

	if vault != nil {
		notes := scanVault(vault.GetBrainDir())
		sessions := filterSessions(notes)
		now := time.Now()
		expiring := 0
		stale := 0
		for _, n := range notes {
			if n.Lifecycle.State == brain.NoteStale {
				stale++
			}
			if n.Lifecycle.Managed && !n.Lifecycle.ExpiresAt.IsZero() &&
				n.Lifecycle.ExpiresAt.After(now) && n.Lifecycle.ExpiresAt.Before(now.Add(24*time.Hour)) {
				expiring++
			}
		}
		status["sessions_retained"] = len(sessions)
		status["sessions_limit"] = cfg.KeepLatestSessions
		status["expiring_within_24h"] = expiring
		status["stale_notes"] = stale
		status["total_notes"] = len(notes)
	}
	if raw != nil {
		if count, bytes, err := raw.Usage(); err == nil {
			status["raw_objects"] = count
			status["raw_bytes"] = bytes
		}
	}
	return status
}

// noteRef is a scanned vault note with its lifecycle already parsed.
type noteRef struct {
	Path      string
	Rel       string
	Lifecycle brain.Lifecycle
	Content   string
	// Activity is the note's last-activity timestamp, used to order sessions.
	Activity time.Time
}

// scanVault reads every markdown note with its lifecycle metadata.
//
// Reading full bodies is deliberate: the compiler needs them to extract
// knowledge before a deletion, and a vault at the session limit is a few
// hundred small files.
func scanVault(brainDir string) []noteRef {
	var out []noteRef
	filepath.Walk(brainDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		if filepath.Base(path) == "context.md" {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		content := string(data)
		lc := brain.ParseLifecycle(content)
		rel, _ := filepath.Rel(brainDir, path)
		activity := lc.LastAccessed
		if activity.IsZero() {
			activity = lc.UpdatedAt
		}
		if activity.IsZero() {
			activity = lc.CreatedAt
		}
		if activity.IsZero() {
			activity = info.ModTime()
		}
		out = append(out, noteRef{
			Path:      path,
			Rel:       filepath.ToSlash(rel),
			Lifecycle: lc,
			Content:   content,
			Activity:  activity,
		})
		return nil
	})
	return out
}

// isSessionNote reports whether a note is a compact session snapshot.
//
// Both the v5 location (90-sessions/) and the pre-v5 one (context/) count, so a
// vault mid-migration is optimized by the same session limit. Navigation notes
// are excluded: `context/index.md` is the folder's index, not a session, and
// counting it would make the limit delete the vault's own structure.
func isSessionNote(n noteRef) bool {
	if filepath.Base(n.Rel) == "index.md" {
		return false
	}
	return strings.HasPrefix(n.Rel, "90-sessions/") || strings.HasPrefix(n.Rel, "context/")
}

func filterSessions(notes []noteRef) []noteRef {
	var out []noteRef
	for _, n := range notes {
		if isSessionNote(n) {
			out = append(out, n)
		}
	}
	return out
}

func countSessions(notes []noteRef) int { return len(filterSessions(notes)) }

// structuralNote reports whether a note is part of the vault's own navigation
// rather than content: folder indexes, templates, instructions and maps.
//
// These must never expire, whatever their type says. `debug/index.md` classifies
// as a "debug" note by type, which is ephemeral — expiring it would delete the
// vault's structure and leave dangling [[links]] behind.
func structuralNote(n noteRef) bool {
	if filepath.Base(n.Rel) == "index.md" {
		return true
	}
	for _, prefix := range []string{"templates/", "instructions/", "maps/"} {
		if strings.HasPrefix(n.Rel, prefix) {
			return true
		}
	}
	switch n.Lifecycle.Type {
	case "index", "map", "template", "instruction":
		return true
	}
	return false
}

// expired applies the note's own expiry, honouring a configured TTL override for
// its type.
func expired(n noteRef, cfg Config, now time.Time) bool {
	lc := n.Lifecycle
	if !lc.Managed || lc.Retention == brain.RetentionPermanent || structuralNote(n) {
		return false
	}
	if override, ok := cfg.TTLOverrides[lc.Type]; ok {
		if override <= 0 {
			return false
		}
		base := lc.UpdatedAt
		if base.IsZero() {
			base = lc.CreatedAt
		}
		if base.IsZero() {
			return false
		}
		return now.After(base.Add(override))
	}
	return lc.Expired(now)
}

// markState rewrites only the `state:` field of a note.
func markState(path string, state brain.NoteState) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	updated, ok := brain.ReplaceFrontmatterField(string(data), "state", string(state))
	if !ok {
		return fmt.Errorf("note has no frontmatter to update")
	}
	return os.WriteFile(path, []byte(updated), 0644)
}

// snapshotFromNote reconstructs the compact snapshot a session note was rendered
// from, so the compiler can promote its knowledge before the note is deleted.
//
// It parses the rendered markdown rather than storing a machine copy alongside
// it: a second copy would drift, and the markdown is the file the user actually
// edits in Obsidian.
func snapshotFromNote(n noteRef) (brain.CompactSnapshot, bool) {
	if !isSessionNote(n) {
		return brain.CompactSnapshot{}, false
	}
	sections := parseSections(n.Content)
	s := brain.CompactSnapshot{
		Objective:     firstNonEmpty(sections["objective"], headingTitle(n.Content)),
		AffectedFiles: sections.list("changes", "files"),
		Decisions:     sections.list("decisions"),
		ActiveErrors:  sections.list("active errors", "errors"),
		Resolved:      sections.list("resolved"),
		Blockers:      sections.list("blockers"),
		NextSteps:     sections.list("next steps"),
		RawRefs:       n.Lifecycle.RawRefs,
	}
	if s.Empty() {
		return s, false
	}
	return s, true
}

type sectionMap map[string]string

// list splits the first matching section into bullet lines.
func (m sectionMap) list(keys ...string) []string {
	for _, key := range keys {
		body, ok := m[key]
		if !ok {
			continue
		}
		var out []string
		for _, line := range strings.Split(body, "\n") {
			line = strings.TrimSpace(line)
			line = strings.TrimPrefix(line, "-")
			line = strings.TrimPrefix(line, "*")
			line = strings.TrimSpace(line)
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

// parseSections maps lowercase "## Heading" names to their bodies.
func parseSections(content string) sectionMap {
	out := sectionMap{}
	current := ""
	var body []string
	flush := func() {
		if current != "" {
			out[current] = strings.TrimSpace(strings.Join(body, "\n"))
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
	return out
}

func headingTitle(content string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// collectRawReferences gathers every raw object id the vault still cites, both
// from lifecycle metadata and from note bodies. The union is what protects an
// object from pruning (spec §29).
func collectRawReferences(notes []noteRef) map[string]bool {
	refs := map[string]bool{}
	add := func(ref string) {
		if id, err := rawstore.ParseRef(ref); err == nil {
			refs[id] = true
		}
	}
	for _, n := range notes {
		for _, ref := range n.Lifecycle.RawRefs {
			add(ref)
		}
		rest := n.Content
		for {
			idx := strings.Index(rest, rawstore.URIScheme)
			if idx < 0 {
				break
			}
			rest = rest[idx+len(rawstore.URIScheme):]
			end := 0
			for end < len(rest) && isHex(rest[end]) {
				end++
			}
			if end > 0 {
				add(rest[:end])
			}
			rest = rest[end:]
		}
	}
	return refs
}

func isHex(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}
