package brain

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Canonical Project Memory (spec §14, §15, §16).
//
// The point of canonical memory is that a structural fact exists in exactly one
// place. The pre-v5 vault accumulated the same fact in dozens of session notes:
//
//	Go 1.24
//	Go 1.25
//	Go updated to 1.25
//	Current Go: 1.25
//
// A canonical note holds `Go: 1.25` and is *updated* when the fact changes, with
// an ADR recording the change only when the change itself matters.

// CanonicalArea is one of the v5 numbered vault areas (spec §14).
type CanonicalArea string

const (
	AreaProject      CanonicalArea = "00-project"
	AreaArchitecture CanonicalArea = "10-architecture"
	AreaDecisions    CanonicalArea = "20-decisions"
	AreaModules      CanonicalArea = "30-modules"
	AreaKnowledge    CanonicalArea = "40-knowledge"
	AreaState        CanonicalArea = "80-state"
	AreaSessions     CanonicalArea = "90-sessions"
)

// canonicalLayout maps a canonical key to its file and note type. Keys are the
// stable identifiers the DWYT MCP returns in a context plan's `memory` list, so
// they are part of the governor's contract and must not be renamed casually.
var canonicalLayout = map[string]struct {
	Area CanonicalArea
	File string
	Type string
}{
	"project":            {AreaProject, "project.md", "project"},
	"active-constraints": {AreaProject, "active-constraints.md", "constraint"},
	"architecture":       {AreaArchitecture, "architecture.md", "architecture"},
	"frontend":           {AreaArchitecture, "frontend.md", "architecture"},
	"backend":            {AreaArchitecture, "backend.md", "architecture"},
	"integrations":       {AreaArchitecture, "integrations.md", "architecture"},
	"conventions":        {AreaKnowledge, "conventions.md", "convention"},
	"known-issues":       {AreaKnowledge, "known-issues.md", "known_issues"},
	"lessons":            {AreaKnowledge, "lessons.md", "lessons"},
	"error-memory":       {AreaKnowledge, "error-memory.md", "reusable_error_patterns"},
	"current-task":       {AreaState, "current-task.md", "task_context"},
	"active-errors":      {AreaState, "active-errors.md", "task_context"},
}

// CanonicalKeys returns the known canonical keys, sorted.
func CanonicalKeys() []string {
	keys := make([]string, 0, len(canonicalLayout))
	for k := range canonicalLayout {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// canonicalPath resolves a canonical key to its absolute path, or "" for an
// unknown key. Module notes are addressed as "module:<name>".
func (pb *ProjectObsidian) canonicalPath(key string) (path string, noteType string, ok bool) {
	if name, isModule := strings.CutPrefix(key, "module:"); isModule {
		safe := safeDirName(name)
		if safe == "" {
			return "", "", false
		}
		return filepath.Join(pb.brainDir, string(AreaModules), safe+".md"), "module", true
	}
	entry, found := canonicalLayout[key]
	if !found {
		return "", "", false
	}
	return filepath.Join(pb.brainDir, string(entry.Area), entry.File), entry.Type, true
}

// CanonicalNote is a canonical memory note.
type CanonicalNote struct {
	Key       string    `json:"key"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Lifecycle Lifecycle `json:"lifecycle"`
	Path      string    `json:"path,omitempty"`
	TokensEst int       `json:"tokens_est,omitempty"`
}

// ReadCanonical loads a canonical note. A missing note is not an error: an empty
// note with ok=false lets the caller decide whether to create it.
func (pb *ProjectObsidian) ReadCanonical(key string) (CanonicalNote, bool) {
	pb.mu.RLock()
	defer pb.mu.RUnlock()
	return pb.readCanonicalLocked(key)
}

func (pb *ProjectObsidian) readCanonicalLocked(key string) (CanonicalNote, bool) {
	path, _, ok := pb.canonicalPath(key)
	if !ok {
		return CanonicalNote{Key: key}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return CanonicalNote{Key: key, Path: path}, false
	}
	content := string(data)
	return CanonicalNote{
		Key:       key,
		Title:     extractTitle(content),
		Body:      bodyOf(content),
		Lifecycle: ParseLifecycle(content),
		Path:      path,
		TokensEst: estimateTokens(content),
	}, true
}

// UpsertCanonical writes or updates a canonical note.
//
// The write is *idempotent on content*: when the rendered body is unchanged the
// file is left alone, so re-running a compile pass does not churn mtimes and
// does not make every note look freshly edited in Obsidian.
func (pb *ProjectObsidian) UpsertCanonical(key, title, body string, source SourceRef) (CanonicalNote, error) {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	defer pb.invalidateStats()

	path, noteType, ok := pb.canonicalPath(key)
	if !ok {
		return CanonicalNote{}, fmt.Errorf("brain: unknown canonical key %q", key)
	}
	if strings.TrimSpace(body) == "" {
		return CanonicalNote{}, fmt.Errorf("brain: refusing to write an empty canonical note %q", key)
	}
	if title == "" {
		title = defaultCanonicalTitle(key)
	}

	now := time.Now()
	existing, existed := pb.readCanonicalLocked(key)

	lc := NewLifecycle(noteType, now)
	if existed {
		// Preserve the durable history of the note: creation time and usage
		// stats belong to the fact, not to this particular write.
		if !existing.Lifecycle.CreatedAt.IsZero() {
			lc.CreatedAt = existing.Lifecycle.CreatedAt
		}
		lc.AccessCount = existing.Lifecycle.AccessCount
		lc.LastAccessed = existing.Lifecycle.LastAccessed
	}
	lc.UpdatedAt = now
	lc.State = NoteActive
	if source.File != "" {
		lc.SourceFile = source.File
		lc.SourceHash = source.Hash
	}

	rendered := renderCanonical(key, title, body, lc, pb)
	if existed && strings.TrimSpace(existing.Body) == strings.TrimSpace(body) &&
		existing.Lifecycle.SourceHash == lc.SourceHash {
		// Same fact, same source: nothing to write. Returning the note as it is
		// on disk (rather than the freshly built one) keeps the caller's view
		// consistent with the file.
		return existing, nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return CanonicalNote{}, fmt.Errorf("brain: create canonical dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(rendered), 0644); err != nil {
		return CanonicalNote{}, fmt.Errorf("brain: write canonical %q: %w", key, err)
	}
	pb.UpdatedAt = now

	return CanonicalNote{
		Key:       key,
		Title:     title,
		Body:      body,
		Lifecycle: lc,
		Path:      path,
		TokensEst: estimateTokens(rendered),
	}, nil
}

// SourceRef ties a derived canonical note back to the code it describes so
// stale detection can work (spec §26).
type SourceRef struct {
	File string
	Hash string
}

// SourceOf builds a SourceRef by hashing a file on disk. A missing file yields
// an empty ref rather than an error: recording no source is better than
// recording a wrong one.
func SourceOf(path string) SourceRef {
	hash := HashFile(path)
	if hash == "" {
		return SourceRef{}
	}
	return SourceRef{File: path, Hash: hash}
}

// AppendCanonicalBullet adds a bullet to a canonical list note, skipping
// semantically equivalent entries (spec §28: deterministic dedup first).
//
// This is the workhorse of knowledge promotion: lessons, known issues and error
// patterns are lists, and the compiler must be able to add to them repeatedly
// across sessions without producing four phrasings of the same lesson.
func (pb *ProjectObsidian) AppendCanonicalBullet(key, bullet string) (added bool, err error) {
	bullet = strings.TrimSpace(strings.ReplaceAll(bullet, "\n", " "))
	if bullet == "" {
		return false, nil
	}

	note, existed := pb.ReadCanonical(key)
	body := note.Body
	if !existed || strings.TrimSpace(body) == "" {
		body = ""
	}
	if containsEquivalentBullet(body, bullet) {
		return false, nil
	}
	next := strings.TrimRight(body, "\n")
	if next != "" {
		next += "\n"
	}
	next += "- " + bullet + "\n"

	if _, err := pb.UpsertCanonical(key, "", next, SourceRef{}); err != nil {
		return false, err
	}
	return true, nil
}

// containsEquivalentBullet reports whether a list already carries a semantically
// equivalent line. Equivalence is deterministic: case, punctuation, digits and
// stop words are normalized away. It is not semantic understanding — it catches
// the actual duplication pattern (the same sentence saved again, possibly with
// a different number) without an embedding model (spec §28).
func containsEquivalentBullet(body, bullet string) bool {
	target := normalizeForDedup(bullet)
	if target == "" {
		return false
	}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "-")
		line = strings.TrimPrefix(line, "*")
		if normalizeForDedup(line) == target {
			return true
		}
	}
	return false
}

// dedupStopWords are words whose presence or absence does not change what a
// bullet asserts.
var dedupStopWords = map[string]bool{
	"a": true, "an": true, "the": true, "is": true, "are": true, "was": true,
	"were": true, "be": true, "to": true, "of": true, "in": true, "on": true,
	"for": true, "and": true, "that": true, "this": true, "it": true,
	"we": true, "should": true, "must": true, "now": true,
}

// normalizeForDedup reduces a line to a comparable signature: lowercase,
// punctuation stripped, digits collapsed, stop words removed, tokens sorted.
// Sorting makes word order irrelevant, which is what catches "confirm before
// delete" vs "before delete, confirm".
func normalizeForDedup(s string) string {
	lower := strings.ToLower(s)
	var cleaned strings.Builder
	cleaned.Grow(len(lower))
	lastDigit := false
	for _, r := range lower {
		switch {
		case r >= 'a' && r <= 'z', r == '_':
			cleaned.WriteRune(r)
			lastDigit = false
		case r >= '0' && r <= '9':
			if !lastDigit {
				cleaned.WriteByte('#')
				lastDigit = true
			}
		default:
			cleaned.WriteByte(' ')
			lastDigit = false
		}
	}
	tokens := strings.Fields(cleaned.String())
	kept := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		if dedupStopWords[tok] || len(tok) < 2 {
			continue
		}
		kept = append(kept, tok)
	}
	if len(kept) == 0 {
		return ""
	}
	sort.Strings(kept)
	return strings.Join(kept, " ")
}

// MarkCanonicalStale flags a canonical note whose source changed, so the next
// reader knows to regenerate it instead of trusting it (spec §26).
func (pb *ProjectObsidian) MarkCanonicalStale(key string) (bool, error) {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	defer pb.invalidateStats()

	path, _, ok := pb.canonicalPath(key)
	if !ok {
		return false, fmt.Errorf("brain: unknown canonical key %q", key)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, nil
	}
	updated, ok := replaceFrontmatterFields(string(data), map[string]string{
		"state": string(NoteStale),
	})
	if !ok {
		return false, nil
	}
	return true, os.WriteFile(path, []byte(updated), 0644)
}

// EnsureCanonicalLayout creates the v5 numbered directories and seeds the
// canonical notes that do not exist yet.
//
// Seeding is additive only: an existing note is never overwritten, because the
// user (or a previous compile pass) may already have put real knowledge in it.
func (pb *ProjectObsidian) EnsureCanonicalLayout() error {
	pb.mu.Lock()
	defer pb.mu.Unlock()

	for _, area := range []CanonicalArea{
		AreaProject, AreaArchitecture, AreaDecisions, AreaModules,
		AreaKnowledge, AreaState, AreaSessions,
	} {
		if err := os.MkdirAll(filepath.Join(pb.brainDir, string(area)), 0755); err != nil {
			return fmt.Errorf("brain: create %s: %w", area, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(pb.brainDir, filepath.FromSlash(snapshotDir)), 0755); err != nil {
		return fmt.Errorf("brain: create snapshot dir: %w", err)
	}

	now := time.Now()
	seeds := map[string]string{
		"project": fmt.Sprintf(
			"Canonical identity of this project. Keep it current; do not append a second copy of a changed fact.\n\n"+
				"- name: %s\n- path: %s\n- stack: (fill in)\n- purpose: (fill in)\n",
			pb.ProjectName, pb.ProjectPath),
		"active-constraints": "Constraints that currently apply. Remove an entry when it stops applying.\n",
		"architecture":       "Current architecture. Update in place; record *why* it changed in 20-decisions.\n",
		"conventions":        "Conventions and patterns this project follows.\n",
		"known-issues":       "Known issues that are still relevant.\n",
		"lessons":            "Reusable lessons learned. One bullet per lesson.\n",
		"error-memory":       "Reusable error patterns: cause and resolution, not stack traces.\n",
	}
	for key, body := range seeds {
		path, noteType, ok := pb.canonicalPath(key)
		if !ok {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			continue
		}
		lc := NewLifecycle(noteType, now)
		content := renderCanonical(key, defaultCanonicalTitle(key), body, lc, pb)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Errorf("brain: seed canonical %q: %w", key, err)
		}
	}

	// A navigable index for the new areas, so a human opening the vault in
	// Obsidian can find them.
	indexPath := filepath.Join(pb.brainDir, "maps", "canonical-map.md")
	if _, err := os.Stat(indexPath); err != nil {
		os.MkdirAll(filepath.Dir(indexPath), 0755)
		os.WriteFile(indexPath, []byte(canonicalMapNote(now)), 0644)
	}
	return nil
}

func canonicalMapNote(now time.Time) string {
	return `---
type: map
updated_at: ` + now.Format(time.RFC3339) + `
tags: [dwyt, map, canonical]
---

# Canonical Memory Map

Canonical memory holds each structural fact once. Update a note in place; record
why it changed in a decision note.

- [[00-project/project|Project identity]]
- [[00-project/active-constraints|Active constraints]]
- [[10-architecture/architecture|Architecture]]
- [[20-decisions/index|Decisions]]
- [[40-knowledge/conventions|Conventions]]
- [[40-knowledge/known-issues|Known issues]]
- [[40-knowledge/lessons|Lessons learned]]
- [[40-knowledge/error-memory|Reusable error patterns]]
- [[80-state/current-task|Current task]]
- **30-modules/** — one summary per module
- **90-sessions/compact/** — compact session snapshots

Raw evidence lives outside the vault, in the DWYT raw object store, and is
referenced as ` + "`dwyt://objects/<id>`" + `.
`
}

func renderCanonical(key, title, body string, lc Lifecycle, pb *ProjectObsidian) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "tags: [dwyt, canonical, %s]\n", lc.Type)
	fmt.Fprintf(&b, "canonical_key: %s\n", key)
	b.WriteString(lc.Render())
	fmt.Fprintf(&b, "project: %q\n", pb.ProjectName)
	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "# %s\n\n", title)
	b.WriteString("Links: [[index]] [[maps/canonical-map]]\n\n")
	b.WriteString(strings.TrimRight(body, "\n"))
	b.WriteString("\n")
	return b.String()
}

func defaultCanonicalTitle(key string) string {
	if name, ok := strings.CutPrefix(key, "module:"); ok {
		return "Module: " + name
	}
	switch key {
	case "project":
		return "Project Identity"
	case "active-constraints":
		return "Active Constraints"
	case "architecture":
		return "Architecture"
	case "frontend":
		return "Frontend Architecture"
	case "backend":
		return "Backend Architecture"
	case "integrations":
		return "Integrations"
	case "conventions":
		return "Conventions"
	case "known-issues":
		return "Known Issues"
	case "lessons":
		return "Lessons Learned"
	case "error-memory":
		return "Error Memory"
	case "current-task":
		return "Current Task"
	case "active-errors":
		return "Active Errors"
	}
	return key
}

// bodyOf returns the markdown body of a note: frontmatter, the H1 title and the
// generated Links line removed, so a round trip through Upsert does not
// accumulate duplicate headers.
func bodyOf(content string) string {
	if fm, ok := frontmatterOf(content); ok {
		if idx := strings.Index(content, fm); idx >= 0 {
			rest := content[idx+len(fm):]
			rest = strings.TrimPrefix(rest, "\n")
			rest = strings.TrimPrefix(rest, "---")
			content = rest
		}
	}
	var kept []string
	titleDropped := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		// Drop the first H1 only. Counting kept lines would not work: the blank
		// lines that precede the title are themselves kept, so by the time the
		// title arrives the slice is no longer empty — and the title would
		// survive into the body and be re-rendered on the next write.
		if !titleDropped && strings.HasPrefix(trimmed, "# ") {
			titleDropped = true
			continue
		}
		if strings.HasPrefix(trimmed, "Links: [[") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

// joinVault builds a path inside the vault from slash-separated parts, creating
// the parent directory. Used by the compiler, which writes into the numbered
// areas without going through UpsertCanonical.
func joinVault(brainDir string, parts ...string) string {
	segments := make([]string, 0, len(parts)+1)
	segments = append(segments, brainDir)
	for _, p := range parts {
		segments = append(segments, filepath.FromSlash(p))
	}
	path := filepath.Join(segments...)
	os.MkdirAll(filepath.Dir(path), 0755)
	return path
}

// readFileString reads a file, returning "" when it does not exist. Callers use
// the empty string as "not there yet", which is always a valid state for a
// canonical note.
func readFileString(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// osReadDir lists the file names in a directory, ignoring subdirectories. A
// missing directory returns an error the caller treats as "nothing there yet",
// which is a valid state for a vault that has no module summaries.
func osReadDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out, nil
}
