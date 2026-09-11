package brain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Lifecycle metadata for vault notes (spec §22, §25, §26).
//
// Every note DWYT writes carries enough metadata for the housekeeper to decide,
// deterministically and without reading the body, whether the note is permanent
// knowledge, expiring evidence, or already stale because the code it described
// changed. Notes written by the user (or by older DWYT versions) have no such
// metadata; they are treated as permanent, which is the only safe default —
// expiring a note DWYT cannot classify would silently destroy user content.

// RetentionClass is the retention policy of a note.
type RetentionClass string

const (
	// RetentionPermanent never expires automatically (spec §22 P0).
	RetentionPermanent RetentionClass = "permanent"
	// RetentionLongTerm defaults to 180 days or until superseded (P1).
	RetentionLongTerm RetentionClass = "long_term"
	// RetentionTemporary expires per category: task context, resolved errors,
	// build and test results (P2).
	RetentionTemporary RetentionClass = "temporary"
	// RetentionEphemeral is operational noise measured in hours (P3).
	RetentionEphemeral RetentionClass = "ephemeral"
)

// NoteState mirrors contextopt.State for persisted notes. It is duplicated as a
// string type rather than imported so the brain does not depend on the
// optimizer: the dependency runs the other way (the optimizer asks the brain for
// health).
type NoteState string

const (
	NoteActive      NoteState = "active"
	NoteReference   NoteState = "reference"
	NoteResolved    NoteState = "resolved"
	NoteStale       NoteState = "stale"
	NoteDiscardable NoteState = "discardable"
)

// Lifecycle is the DWYT-managed frontmatter block of a note.
type Lifecycle struct {
	Type      string         `json:"type,omitempty"`
	Retention RetentionClass `json:"retention,omitempty"`
	State     NoteState      `json:"state,omitempty"`

	CreatedAt time.Time `json:"created_at,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`

	LastAccessed time.Time `json:"last_accessed,omitempty"`
	AccessCount  int       `json:"access_count,omitempty"`

	// Importance is a 0..1 prior used by usage-aware retention.
	Importance float64 `json:"importance,omitempty"`

	// SourceFile and SourceHash tie a derived note back to the code it
	// describes, enabling stale detection (spec §26).
	SourceFile string `json:"source_file,omitempty"`
	SourceHash string `json:"source_hash,omitempty"`

	// RawRefs are raw object references cited by the note. The housekeeper
	// uses them so a cited object is never pruned (spec §29).
	RawRefs []string `json:"raw_refs,omitempty"`

	// StateHash is the identity of the state a snapshot captured, so an
	// unchanged state does not produce a second snapshot (spec §19.1).
	StateHash string `json:"state_hash,omitempty"`

	// Managed marks a note whose lifecycle DWYT owns. An unmanaged note is
	// never expired.
	Managed bool `json:"managed,omitempty"`
}

// DefaultRetentionTTLs is the recommended TTL table (spec §23). Keys are note
// types; the housekeeper config may override any of them.
func DefaultRetentionTTLs() map[string]time.Duration {
	return map[string]time.Duration{
		// P1 — long term.
		"lessons":                 180 * 24 * time.Hour,
		"known_issues":            180 * 24 * time.Hour,
		"reusable_error_patterns": 180 * 24 * time.Hour,
		"knowledge":               180 * 24 * time.Hour,
		// P2 — temporary.
		"task_context":    14 * 24 * time.Hour,
		"task":            14 * 24 * time.Hour,
		"context":         14 * 24 * time.Hour,
		"resolved_errors": 30 * 24 * time.Hour,
		"build_results":   7 * 24 * time.Hour,
		"test_results":    7 * 24 * time.Hour,
		// P3 — ephemeral.
		"operational_logs": 3 * 24 * time.Hour,
		"log":              3 * 24 * time.Hour,
		"command":          3 * 24 * time.Hour,
		"tool_outputs":     72 * time.Hour,
		"temporary_search": 24 * time.Hour,
		"debug_dump":       24 * time.Hour,
		"debug":            24 * time.Hour,
	}
}

// permanentTypes never expire automatically (spec §22 P0).
var permanentTypes = map[string]bool{
	"project":              true,
	"architecture":         true,
	"decision":             true,
	"decisions":            true,
	"adr":                  true,
	"convention":           true,
	"conventions":          true,
	"constraint":           true,
	"constraints":          true,
	"security_decisions":   true,
	"api_contracts":        true,
	"deployment_decisions": true,
	"instruction":          true,
	"index":                true,
	"map":                  true,
	"template":             true,
	"canonical":            true,
	"module":               true,
}

// RetentionFor classifies a note type. Unknown types are permanent: DWYT must
// not invent an expiry for content it cannot classify.
func RetentionFor(noteType string) RetentionClass {
	t := normalizeNoteType(noteType)
	if permanentTypes[t] {
		return RetentionPermanent
	}
	ttl, ok := DefaultRetentionTTLs()[t]
	if !ok {
		return RetentionPermanent
	}
	switch {
	case ttl >= 180*24*time.Hour:
		return RetentionLongTerm
	case ttl >= 7*24*time.Hour:
		return RetentionTemporary
	default:
		return RetentionEphemeral
	}
}

// TTLFor returns the retention period for a note type, or zero for permanent.
func TTLFor(noteType string) time.Duration {
	if RetentionFor(noteType) == RetentionPermanent {
		return 0
	}
	return DefaultRetentionTTLs()[normalizeNoteType(noteType)]
}

// ImportanceFor is the structural importance prior for a note type.
func ImportanceFor(noteType string) float64 {
	switch normalizeNoteType(noteType) {
	case "project", "constraint", "constraints", "architecture":
		return 0.95
	case "decision", "decisions", "adr":
		return 0.9
	case "convention", "conventions", "module":
		return 0.8
	case "knowledge", "lessons", "known_issues":
		return 0.7
	case "task", "context", "task_context":
		return 0.5
	case "debug", "log", "command", "session":
		return 0.25
	default:
		return 0.5
	}
}

// NewLifecycle builds the lifecycle block for a note DWYT is about to write.
func NewLifecycle(noteType string, now time.Time) Lifecycle {
	lc := Lifecycle{
		Type:       normalizeNoteType(noteType),
		Retention:  RetentionFor(noteType),
		State:      NoteActive,
		CreatedAt:  now,
		UpdatedAt:  now,
		Importance: ImportanceFor(noteType),
		Managed:    true,
	}
	if ttl := TTLFor(noteType); ttl > 0 {
		lc.ExpiresAt = now.Add(ttl)
	}
	return lc
}

// Expired reports whether the note is past its TTL. Permanent and unmanaged
// notes never expire.
func (lc Lifecycle) Expired(now time.Time) bool {
	if !lc.Managed || lc.Retention == RetentionPermanent || lc.ExpiresAt.IsZero() {
		return false
	}
	return now.After(lc.ExpiresAt)
}

// Stale reports whether the note derives from source code that has since
// changed (spec §26). currentHash is the hash of the note's SourceFile today.
func (lc Lifecycle) Stale(currentHash string) bool {
	if lc.State == NoteStale {
		return true
	}
	if lc.SourceHash == "" || currentHash == "" {
		return false
	}
	return lc.SourceHash != currentHash
}

// RetentionScore combines importance, usage and recency for P1/P2 retention
// decisions (spec §25). Permanent notes are never scored: they are kept
// regardless of use.
func (lc Lifecycle) RetentionScore(now time.Time) float64 {
	if lc.Retention == RetentionPermanent {
		return 1
	}
	importance := lc.Importance
	if importance <= 0 {
		importance = 0.5
	}
	// Usage saturates quickly: the difference between 1 and 5 accesses matters,
	// between 50 and 60 does not.
	usage := 1.0
	if lc.AccessCount > 0 {
		usage = 1 + float64(lc.AccessCount)/(float64(lc.AccessCount)+5)
	}
	recency := 0.25
	reference := lc.LastAccessed
	if reference.IsZero() {
		reference = lc.UpdatedAt
	}
	if !reference.IsZero() {
		ageDays := now.Sub(reference).Hours() / 24
		switch {
		case ageDays <= 1:
			recency = 1
		case ageDays <= 7:
			recency = 0.8
		case ageDays <= 30:
			recency = 0.5
		case ageDays <= 90:
			recency = 0.35
		}
	}
	return importance * usage * recency
}

// Render writes the lifecycle block as YAML frontmatter lines (without the
// surrounding "---" markers) so callers can compose a full frontmatter block.
// Zero values are omitted, keeping notes readable in Obsidian.
func (lc Lifecycle) Render() string {
	var b strings.Builder
	if lc.Type != "" {
		fmt.Fprintf(&b, "type: %s\n", lc.Type)
	}
	if lc.Retention != "" {
		fmt.Fprintf(&b, "retention: %s\n", lc.Retention)
	}
	if lc.State != "" {
		fmt.Fprintf(&b, "state: %s\n", lc.State)
	}
	writeTimeField(&b, "created_at", lc.CreatedAt)
	writeTimeField(&b, "updated_at", lc.UpdatedAt)
	writeTimeField(&b, "expires_at", lc.ExpiresAt)
	writeTimeField(&b, "last_accessed", lc.LastAccessed)
	if lc.AccessCount > 0 {
		fmt.Fprintf(&b, "access_count: %d\n", lc.AccessCount)
	}
	if lc.Importance > 0 {
		fmt.Fprintf(&b, "importance: %.2f\n", lc.Importance)
	}
	if lc.SourceFile != "" {
		fmt.Fprintf(&b, "source_file: %q\n", lc.SourceFile)
	}
	if lc.SourceHash != "" {
		fmt.Fprintf(&b, "source_hash: %s\n", lc.SourceHash)
	}
	if lc.StateHash != "" {
		fmt.Fprintf(&b, "state_hash: %s\n", lc.StateHash)
	}
	if len(lc.RawRefs) > 0 {
		fmt.Fprintf(&b, "raw_refs: [%s]\n", strings.Join(lc.RawRefs, ", "))
	}
	if lc.Managed {
		b.WriteString("dwyt_managed: true\n")
	}
	return b.String()
}

func writeTimeField(b *strings.Builder, key string, t time.Time) {
	if t.IsZero() {
		return
	}
	fmt.Fprintf(b, "%s: %s\n", key, t.Format(time.RFC3339))
}

// ParseLifecycle reads the lifecycle block from a note's frontmatter. A note
// without frontmatter, or without DWYT fields, yields a zero Lifecycle with
// Managed=false — the signal that the housekeeper must leave it alone.
func ParseLifecycle(content string) Lifecycle {
	lc := Lifecycle{}
	fm, ok := frontmatterOf(content)
	if !ok {
		return lc
	}
	for _, line := range strings.Split(fm, "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"`)
		if value == "" {
			continue
		}
		switch key {
		case "type":
			lc.Type = normalizeNoteType(value)
		case "retention":
			lc.Retention = RetentionClass(value)
		case "state":
			lc.State = NoteState(value)
		case "created_at", "date":
			// "date" is the pre-v5 field name; accepting it lets an old note
			// participate in recency ranking without a rewrite.
			if lc.CreatedAt.IsZero() {
				lc.CreatedAt = parseTimeField(value)
			}
		case "updated_at":
			lc.UpdatedAt = parseTimeField(value)
		case "expires_at":
			lc.ExpiresAt = parseTimeField(value)
		case "last_accessed":
			lc.LastAccessed = parseTimeField(value)
		case "access_count":
			if n, err := strconv.Atoi(value); err == nil {
				lc.AccessCount = n
			}
		case "importance":
			if f, err := strconv.ParseFloat(value, 64); err == nil {
				lc.Importance = f
			}
		case "source_file":
			lc.SourceFile = value
		case "source_hash":
			lc.SourceHash = value
		case "state_hash":
			lc.StateHash = value
		case "raw_refs":
			lc.RawRefs = parseInlineList(value)
		case "dwyt_managed":
			lc.Managed = value == "true"
		}
	}
	if lc.Retention == "" && lc.Type != "" {
		lc.Retention = RetentionFor(lc.Type)
	}
	return lc
}

// frontmatterOf extracts the YAML frontmatter body of a markdown document.
func frontmatterOf(content string) (string, bool) {
	trimmed := strings.TrimLeft(content, "\ufeff \t\r\n")
	if !strings.HasPrefix(trimmed, "---") {
		return "", false
	}
	rest := trimmed[3:]
	rest = strings.TrimPrefix(rest, "\r")
	rest = strings.TrimPrefix(rest, "\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", false
	}
	return rest[:end], true
}

func parseTimeField(value string) time.Time {
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, value); err == nil {
			return t
		}
	}
	return time.Time{}
}

func parseInlineList(value string) []string {
	value = strings.Trim(value, "[]")
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(strings.Trim(strings.TrimSpace(p), `"`))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func normalizeNoteType(t string) string {
	out := strings.ToLower(strings.TrimSpace(t))
	out = strings.ReplaceAll(out, "-", "_")
	out = strings.ReplaceAll(out, " ", "_")
	return out
}

// HashFile returns the canonical content hash of a file on disk, used to record
// and later verify a derived note's source (spec §26). A missing or unreadable
// file yields an empty string, which ParseLifecycle/Stale treat as "no signal"
// rather than as "changed" — a note must not be marked stale because the file
// was temporarily unavailable.
func HashFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return HashContent(string(data))
}

// HashContent is the canonical content hash format shared across DWYT v5.
func HashContent(content string) string {
	sum := sha256.Sum256([]byte(content))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// TouchAccess records a read of a note by rewriting only its lifecycle counters
// in place. It is best-effort: a note that cannot be rewritten (read-only file,
// unmanaged note, missing frontmatter) is left untouched and reported as
// unchanged, because usage bookkeeping must never fail a search.
func TouchAccess(path string, now time.Time) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	content := string(data)
	lc := ParseLifecycle(content)
	if !lc.Managed {
		return false
	}
	lc.AccessCount++
	lc.LastAccessed = now

	updated, ok := replaceFrontmatterFields(content, map[string]string{
		"access_count":  strconv.Itoa(lc.AccessCount),
		"last_accessed": now.Format(time.RFC3339),
	})
	if !ok {
		return false
	}
	return atomicWriteFile(path, []byte(updated), 0o644) == nil
}

// replaceFrontmatterFields upserts scalar fields inside a note's frontmatter,
// preserving every other line byte-for-byte. Returns false when the document
// has no frontmatter to update.
func replaceFrontmatterFields(content string, fields map[string]string) (string, bool) {
	fm, ok := frontmatterOf(content)
	if !ok {
		return content, false
	}
	lines := strings.Split(fm, "\n")
	seen := map[string]bool{}
	for i, line := range lines {
		key, _, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		trimmed := strings.TrimSpace(key)
		if value, want := fields[trimmed]; want {
			lines[i] = trimmed + ": " + value
			seen[trimmed] = true
		}
	}
	// Append fields that were not present yet, in a stable order.
	missing := make([]string, 0, len(fields))
	for key := range fields {
		if !seen[key] {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	for _, key := range missing {
		lines = append(lines, key+": "+fields[key])
	}

	newFM := strings.Join(lines, "\n")
	// Rebuild the document around the replaced frontmatter.
	idx := strings.Index(content, fm)
	if idx < 0 {
		return content, false
	}
	return content[:idx] + newFM + content[idx+len(fm):], true
}

// ReplaceFrontmatterField upserts a single scalar field in a note's
// frontmatter, preserving every other line byte-for-byte. It is exported for
// the housekeeper, which needs to flip `state:` without rewriting notes.
//
// Returns false when the document has no frontmatter to update — the signal that
// the note is not DWYT-shaped and must be left alone.
func ReplaceFrontmatterField(content, key, value string) (string, bool) {
	return replaceFrontmatterFields(content, map[string]string{key: value})
}
