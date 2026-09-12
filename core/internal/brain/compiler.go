package brain

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Memory Compiler (spec §18).
//
// The compiler answers one question about a session:
//
//	What here deserves to exist six months from now?
//
// Everything that does deserve it is promoted into canonical memory, where it
// exists once and is updated in place. Everything else stays in the compact
// snapshot until its TTL expires. This is what lets the housekeeper delete a
// session note without deleting the knowledge that session produced (spec §21,
// §24).
//
// The compiler is deterministic. It classifies by structure (is this a decision?
// a constraint? a reusable error pattern?), not by asking a model — spec §46 is
// explicit that an auxiliary LLM must not be spent on what rules can decide.

// PromotionResult reports what a compile pass promoted.
type PromotionResult struct {
	// Promoted lists "canonical-key: bullet" for each fact actually added.
	Promoted []string `json:"promoted,omitempty"`
	// Skipped counts facts already present in canonical memory.
	Skipped int `json:"skipped"`
	// Keys lists the canonical notes that were touched, for the snapshot's
	// knowledge_promoted field.
	Keys []string `json:"keys,omitempty"`
	// Errors carries non-fatal problems. A promotion failure must not fail the
	// session it was compiled from.
	Errors []string `json:"errors,omitempty"`
}

// Any reports whether anything was promoted.
func (r PromotionResult) Any() bool { return len(r.Promoted) > 0 }

// Compile promotes the reusable knowledge in a compact snapshot into canonical
// memory and returns what it did.
//
// Mapping (spec §18):
//   - decisions        → 20-decisions (an append-only log, one entry per decision)
//   - constraints      → 00-project/active-constraints
//   - resolved errors  → 40-knowledge/error-memory (cause + resolution, no traces)
//   - lessons          → 40-knowledge/lessons
//   - known issues     → 40-knowledge/known-issues (from unresolved blockers)
//
// Files, commands, validation output and prose are *not* promoted: they are
// per-session facts with no six-month value.
func (pb *ProjectObsidian) Compile(s CompactSnapshot) PromotionResult {
	res := PromotionResult{}

	promote := func(key, bullet string) {
		added, err := pb.AppendCanonicalBullet(key, bullet)
		switch {
		case err != nil:
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", key, err))
		case added:
			res.Promoted = append(res.Promoted, key+": "+bullet)
			res.Keys = appendUniqueString(res.Keys, key)
		default:
			res.Skipped++
		}
	}

	for _, decision := range s.Decisions {
		decision = strings.TrimSpace(decision)
		if decision == "" {
			continue
		}
		// A decision that reads like a constraint belongs with the constraints:
		// an agent asks "what constrains me?" far more often than "what did we
		// decide in March?".
		if looksLikeConstraint(decision) {
			promote("active-constraints", decision)
			continue
		}
		added, err := pb.appendDecisionLog(decision)
		switch {
		case err != nil:
			res.Errors = append(res.Errors, "decisions: "+err.Error())
		case added:
			res.Promoted = append(res.Promoted, "decisions: "+decision)
			res.Keys = appendUniqueString(res.Keys, "decisions")
		default:
			res.Skipped++
		}
	}

	// Resolved items carry the most reusable knowledge in a session: a cause and
	// a fix that will recur.
	for _, resolved := range s.Resolved {
		if bullet := errorPatternBullet(resolved); bullet != "" {
			promote("error-memory", bullet)
		}
	}

	// An unresolved blocker is a known issue until it is fixed.
	for _, blocker := range s.Blockers {
		blocker = strings.TrimSpace(blocker)
		if blocker == "" {
			continue
		}
		promote("known-issues", blocker)
	}

	// A lesson is an explicit statement about how to work in this project. The
	// compiler recognises it by its phrasing rather than guessing.
	for _, candidate := range append(append([]string{}, s.Decisions...), s.NextSteps...) {
		if bullet := lessonBullet(candidate); bullet != "" {
			promote("lessons", bullet)
		}
	}

	return res
}

// constraintMarkers are phrasings that assert a rule rather than record a
// choice. Kept small and explicit: a wrong classification is cheap (the fact is
// still in canonical memory, just in the neighbouring note) but a long fuzzy
// list would be unpredictable.
var constraintMarkers = []string{
	"must ", "must not", "never ", "always ", "required", "forbidden",
	"do not ", "cannot ", "may not", "requires ", "only when", "is mandatory",
}

func looksLikeConstraint(s string) bool {
	lower := " " + strings.ToLower(strings.TrimSpace(s)) + " "
	for _, marker := range constraintMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

var lessonMarkers = []string{
	"lesson:", "learned:", "learning:", "takeaway:", "next time",
	"in future", "remember to", "avoid ",
}

// lessonBullet returns the lesson text when the candidate is phrased as one,
// with the marker prefix stripped so the canonical list reads cleanly.
func lessonBullet(candidate string) string {
	trimmed := strings.TrimSpace(candidate)
	lower := strings.ToLower(trimmed)
	for _, marker := range lessonMarkers {
		idx := strings.Index(lower, marker)
		if idx < 0 {
			continue
		}
		if strings.HasSuffix(marker, ":") {
			return strings.TrimSpace(trimmed[idx+len(marker):])
		}
		return trimmed
	}
	return ""
}

// errorPatternBullet turns a resolved-error line into a reusable pattern.
//
// The session records lines like "TS2345 in ResourceTable.tsx — narrowed the
// type". The reusable knowledge is the pair (symptom, resolution); the file and
// line will differ next time, so the compiler keeps the component but drops the
// coordinates.
func errorPatternBullet(resolved string) string {
	line := strings.TrimSpace(resolved)
	if line == "" {
		return ""
	}
	// Only promote entries that actually state a resolution. A bare "fixed the
	// bug" teaches nothing and would only pollute the canonical list.
	symptom, resolution := splitResolution(line)
	if resolution == "" {
		return ""
	}
	symptom = stripLineCoordinates(symptom)
	return symptom + " → " + resolution
}

// splitResolution separates "symptom — resolution" on any of the separators
// agents actually use.
func splitResolution(line string) (symptom, resolution string) {
	for _, sep := range []string{" — ", " -- ", " => ", " -> ", ": fixed by ", " fixed by ", " resolved by "} {
		if idx := strings.Index(line, sep); idx > 0 {
			return strings.TrimSpace(line[:idx]), strings.TrimSpace(line[idx+len(sep):])
		}
	}
	return line, ""
}

// stripLineCoordinates removes ":123:4" style positions, which are noise in a
// reusable pattern.
func stripLineCoordinates(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		if s[i] == ':' && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9' {
			i++
			for i < len(s) && s[i] >= '0' && s[i] <= '9' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return strings.TrimSpace(b.String())
}

// appendDecisionLog appends an entry to the canonical decision log, reporting
// whether it actually added anything.
//
// Decisions are append-only by nature — superseding one is itself a decision —
// so this is the one canonical area that grows rather than being updated in
// place. The `added` return is what lets the compiler distinguish "promoted" from
// "already known"; without it every re-compile would report the same decision as
// newly promoted.
func (pb *ProjectObsidian) appendDecisionLog(decision string) (bool, error) {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	defer pb.invalidateStats()

	path := joinVault(pb.brainDir, string(AreaDecisions), "index.md")

	existing := readFileString(path)
	if containsEquivalentBullet(existing, decision) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return false, err
	}
	if strings.TrimSpace(existing) == "" {
		// Seed the note header so the log is a proper canonical note in
		// Obsidian rather than a bare bullet list.
		lc := NewLifecycle("decision", time.Now())
		header := renderCanonical("decisions", "Decisions",
			"Append-only log of decisions that still apply.\n", lc, pb)
		if err := os.WriteFile(path, []byte(header), 0644); err != nil {
			return false, err
		}
	}
	entry := "- " + strings.TrimSpace(strings.ReplaceAll(decision, "\n", " ")) + "\n"
	if err := appendFile(path, entry); err != nil {
		return false, err
	}
	return true, nil
}

func appendUniqueString(list []string, value string) []string {
	for _, existing := range list {
		if existing == value {
			return list
		}
	}
	return append(list, value)
}
