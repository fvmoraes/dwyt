package brain

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Search V2 (spec §27).
//
// The pre-v5 search returned up to 30 notes ordered by modification time, which
// is the worst possible shape for a token budget: the newest 30 notes are
// mostly session transcripts, and the caller has no way to ask for the two
// architectural decisions it actually needed.
//
// V2 is selective by default: a small top-k, canonical types before session
// history, and raw/stale/resolved content excluded unless explicitly requested.
// Ranking is fully deterministic — no embeddings, no auxiliary LLM — because
// spec §27 requires determinism first and §46 forbids spending a model call on
// something rules can decide.

// DefaultSearchLimit is the v5 default top-k (spec §27).
const DefaultSearchLimit = 5

// DefaultSearchMaxTokens caps the total estimated size of a result set so a
// search cannot blow the memory slice of the context budget.
const DefaultSearchMaxTokens = 1200

// SearchOptions narrows and bounds a vault search.
type SearchOptions struct {
	Query string

	// Types restricts results to these note types. Empty means all types.
	Types []string
	// Limit is the top-k. Zero uses DefaultSearchLimit.
	Limit int
	// ExcludeState drops notes in these lifecycle states. Nil uses the v5
	// default (resolved, stale, discardable).
	ExcludeState []string
	// MaxTokens bounds the total estimated result size. Zero uses
	// DefaultSearchMaxTokens.
	MaxTokens int
	// PreferCurrent boosts canonical, active notes over historical ones.
	PreferCurrent bool
	// IncludeRaw allows log/debug/command notes into the results. Off by
	// default (spec §27: raw does not participate in the default search).
	IncludeRaw bool
	// IncludeExpired allows notes past their TTL. Off by default: an expired
	// note is evidence the housekeeper has not collected yet, not knowledge.
	IncludeExpired bool
	// CountAccess records a read on each returned note, feeding usage-aware
	// retention. Off for background callers (housekeeper, dashboard) so their
	// polling does not inflate access counts.
	CountAccess bool
}

func (o SearchOptions) limit() int {
	if o.Limit <= 0 {
		return DefaultSearchLimit
	}
	return o.Limit
}

func (o SearchOptions) maxTokens() int {
	if o.MaxTokens <= 0 {
		return DefaultSearchMaxTokens
	}
	return o.MaxTokens
}

// excludedStates is the effective exclusion set.
func (o SearchOptions) excludedStates() map[string]bool {
	states := o.ExcludeState
	if states == nil {
		states = []string{string(NoteResolved), string(NoteStale), string(NoteDiscardable)}
	}
	out := make(map[string]bool, len(states))
	for _, s := range states {
		out[strings.ToLower(strings.TrimSpace(s))] = true
	}
	return out
}

// rawTypes are the note types treated as raw evidence rather than knowledge.
var rawTypes = map[string]bool{
	"log":     true,
	"logs":    true,
	"debug":   true,
	"command": true,
	"session": true,
}

// canonicalTypes rank above everything else (spec §27: canonical type before
// body term frequency).
var canonicalTypes = map[string]bool{
	"project":      true,
	"architecture": true,
	"decision":     true,
	"convention":   true,
	"constraint":   true,
	"canonical":    true,
	"module":       true,
	"instruction":  true,
}

// SearchResult is one ranked hit.
type SearchResult struct {
	BrainEntry
	Score     float64 `json:"score"`
	TokensEst int     `json:"tokens_est"`
	MatchedIn string  `json:"matched_in,omitempty"`
	Retention string  `json:"retention,omitempty"`
	State     string  `json:"state,omitempty"`
}

// SearchV2 runs a selective, deterministic vault search.
func (pb *ProjectObsidian) SearchV2(opts SearchOptions) []SearchResult {
	query := strings.ToLower(strings.TrimSpace(opts.Query))
	if query == "" {
		return nil
	}
	terms := strings.Fields(query)

	pb.mu.RLock()
	brainDir := pb.brainDir
	pb.mu.RUnlock()

	typeFilter := map[string]bool{}
	for _, t := range opts.Types {
		typeFilter[normalizeNoteType(t)] = true
	}
	excluded := opts.excludedStates()
	now := time.Now()

	var candidates []SearchResult
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
		lower := strings.ToLower(content)

		entryType := detectType(brainDir, path)
		lc := ParseLifecycle(content)
		if lc.Type != "" {
			entryType = lc.Type
		}

		if len(typeFilter) > 0 && !typeFilter[normalizeNoteType(entryType)] {
			return nil
		}
		if !opts.IncludeRaw && rawTypes[normalizeNoteType(entryType)] {
			return nil
		}
		state := string(lc.State)
		if state == "" {
			state = string(NoteActive)
		}
		if excluded[strings.ToLower(state)] {
			return nil
		}
		if !opts.IncludeExpired && lc.Expired(now) {
			return nil
		}

		title := extractTitle(content)
		score, matchedIn := scoreNote(terms, query, title, lower, entryType, lc, info.ModTime(), now, opts.PreferCurrent)
		if score <= 0 {
			return nil
		}

		body := extractContent(content)
		candidates = append(candidates, SearchResult{
			BrainEntry: BrainEntry{
				ID:        info.Name(),
				Type:      entryType,
				Content:   body,
				Title:     title,
				CreatedAt: firstNonZeroTime(lc.UpdatedAt, lc.CreatedAt, info.ModTime()),
				FilePath:  path,
			},
			Score:     score,
			TokensEst: estimateTokens(title + "\n" + body),
			MatchedIn: matchedIn,
			Retention: string(lc.Retention),
			State:     state,
		})
		return nil
	})

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		if !candidates[i].CreatedAt.Equal(candidates[j].CreatedAt) {
			return candidates[i].CreatedAt.After(candidates[j].CreatedAt)
		}
		// Final tiebreak on ID keeps the ordering reproducible.
		return candidates[i].ID < candidates[j].ID
	})

	limit := opts.limit()
	budget := opts.maxTokens()
	out := make([]SearchResult, 0, limit)
	spent := 0
	for _, r := range candidates {
		if len(out) >= limit {
			break
		}
		// Always admit the top hit: returning nothing because the single best
		// note is large would be worse than returning it.
		if len(out) > 0 && spent+r.TokensEst > budget {
			continue
		}
		spent += r.TokensEst
		out = append(out, r)
	}

	if opts.CountAccess {
		for _, r := range out {
			TouchAccess(r.FilePath, now)
		}
	}
	return out
}

// scoreNote computes the deterministic relevance score. The ranking order from
// spec §27 is encoded as additive tiers whose magnitudes cannot overlap, so a
// title match always outranks a body match no matter how many times a term
// appears in the body.
func scoreNote(terms []string, query, title, lowerContent, entryType string,
	lc Lifecycle, modTime, now time.Time, preferCurrent bool) (float64, string) {

	lowerTitle := strings.ToLower(title)
	score := 0.0
	matchedIn := ""

	switch {
	case lowerTitle != "" && lowerTitle == query:
		score += 1000
		matchedIn = "title_exact"
	case lowerTitle != "" && strings.Contains(lowerTitle, query):
		score += 600
		matchedIn = "title"
	}

	// Tag matches: a note tagged with the query term is about the query.
	if fm, ok := frontmatterOf(lowerContent); ok && strings.Contains(fm, "tags:") {
		for _, term := range terms {
			if strings.Contains(tagLineOf(fm), term) {
				score += 200
				if matchedIn == "" {
					matchedIn = "tags"
				}
				break
			}
		}
	}

	// Body term frequency, capped so a huge note cannot dominate on repetition
	// alone.
	bodyHits := 0
	for _, term := range terms {
		bodyHits += strings.Count(lowerContent, term)
	}
	if bodyHits == 0 && matchedIn == "" {
		return 0, ""
	}
	if bodyHits > 0 {
		capped := bodyHits
		if capped > 20 {
			capped = 20
		}
		score += float64(capped) * 4
		if matchedIn == "" {
			matchedIn = "body"
		}
	}

	// Canonical type bonus, then active-state bonus.
	if canonicalTypes[normalizeNoteType(entryType)] {
		score += 120
		if preferCurrent {
			score += 80
		}
	}
	if lc.State == "" || lc.State == NoteActive {
		score += 40
	}

	// Recency and access history are the last tiebreakers, deliberately small:
	// they must nudge, not decide (that was the pre-v5 failure mode).
	reference := firstNonZeroTime(lc.UpdatedAt, lc.CreatedAt, modTime)
	if !reference.IsZero() {
		ageDays := now.Sub(reference).Hours() / 24
		switch {
		case ageDays <= 1:
			score += 30
		case ageDays <= 7:
			score += 20
		case ageDays <= 30:
			score += 10
		}
	}
	if lc.AccessCount > 0 {
		bonus := float64(lc.AccessCount) * 2
		if bonus > 20 {
			bonus = 20
		}
		score += bonus
	}
	return score, matchedIn
}

// tagLineOf returns the "tags:" line of a frontmatter block, lowercased.
func tagLineOf(frontmatter string) string {
	for _, line := range strings.Split(frontmatter, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "tags:") {
			return strings.ToLower(line)
		}
	}
	return ""
}

func firstNonZeroTime(values ...time.Time) time.Time {
	for _, v := range values {
		if !v.IsZero() {
			return v
		}
	}
	return time.Time{}
}

// estimateTokens mirrors contextgov.EstimateTokens. Duplicated to keep the
// brain independent of the governor package.
func estimateTokens(content string) int {
	if content == "" {
		return 0
	}
	byChars := (len(content) + 3) / 4
	byWords := len(strings.Fields(content))
	if byWords > byChars {
		return byWords
	}
	return byChars
}
