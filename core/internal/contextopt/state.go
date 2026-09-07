package contextopt

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Session state (spec §12) and the delta/hash store (spec §11).
//
// The session is the optimizer's memory of "what this agent already has". It is
// what makes "reuse before retrieve" and "delta before full state" possible:
// without it, every turn would have to re-send the same symbols to be safe.

// Validation is the compact pass/fail/pending map carried in session state and
// in compact snapshots.
type Validation map[string]string

// ErrorSignature identifies an error without carrying its stack trace, so the
// same failure can be recognised across turns and deduplicated.
type ErrorSignature struct {
	Code      string `json:"code,omitempty"`
	File      string `json:"file,omitempty"`
	Line      int    `json:"line,omitempty"`
	Message   string `json:"message,omitempty"`
	Component string `json:"component,omitempty"`
	Count     int    `json:"count,omitempty"`
}

// Fingerprint is the stable identity of an error signature. Message text is
// normalized (lowercased, digits collapsed) so two runs of the same failure
// with different line numbers in the trace still collapse into one.
func (e ErrorSignature) Fingerprint() string {
	norm := func(s string) string {
		s = strings.ToLower(strings.TrimSpace(s))
		var b strings.Builder
		lastDigit := false
		for _, r := range s {
			if r >= '0' && r <= '9' {
				if !lastDigit {
					b.WriteByte('#')
					lastDigit = true
				}
				continue
			}
			lastDigit = false
			b.WriteRune(r)
		}
		return b.String()
	}
	h := sha256.New()
	h.Write([]byte(norm(e.Code)))
	h.Write([]byte{0})
	h.Write([]byte(norm(e.File)))
	h.Write([]byte{0})
	h.Write([]byte(norm(e.Component)))
	h.Write([]byte{0})
	h.Write([]byte(norm(e.Message)))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// SessionState is the compact, explicit state of the current task. It replaces
// long conversational history: a fresh agent can resume from this struct alone.
type SessionState struct {
	TaskID    string `json:"task_id"`
	Objective string `json:"objective,omitempty"`
	Status    string `json:"status,omitempty"`
	Phase     Phase  `json:"phase,omitempty"`

	AffectedFiles []string         `json:"affected_files,omitempty"`
	Decisions     []string         `json:"decisions,omitempty"`
	Validation    Validation       `json:"validation,omitempty"`
	ActiveErrors  []ErrorSignature `json:"active_errors,omitempty"`
	Resolved      []string         `json:"resolved,omitempty"`
	Blockers      []string         `json:"blockers,omitempty"`
	NextSteps     []string         `json:"next_steps,omitempty"`
	RawRefs       []string         `json:"raw_refs,omitempty"`

	Turn      int       `json:"turn,omitempty"`
	StartedAt time.Time `json:"started_at,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// StateHash is the identity of the *meaningful* state (spec §19.1). Timestamps
// and turn counters are excluded on purpose: a snapshot must not be persisted
// merely because time passed.
func (s SessionState) StateHash() string {
	h := sha256.New()
	write := func(parts ...string) {
		for _, p := range parts {
			h.Write([]byte(p))
			h.Write([]byte{0})
		}
	}
	write(s.TaskID, s.Objective, s.Status, string(s.Phase))
	writeSorted(h, s.AffectedFiles)
	writeSorted(h, s.Decisions)
	writeSorted(h, s.Resolved)
	writeSorted(h, s.Blockers)
	writeSorted(h, s.NextSteps)
	writeSorted(h, s.RawRefs)

	keys := make([]string, 0, len(s.Validation))
	for k := range s.Validation {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		write(k, s.Validation[k])
	}

	fps := make([]string, 0, len(s.ActiveErrors))
	for _, e := range s.ActiveErrors {
		fps = append(fps, e.Fingerprint())
	}
	writeSorted(h, fps)

	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func writeSorted(h interface{ Write([]byte) (int, error) }, values []string) {
	sorted := make([]string, len(values))
	copy(sorted, values)
	sort.Strings(sorted)
	for _, v := range sorted {
		h.Write([]byte(v))
		h.Write([]byte{0})
	}
}

// SymbolState records what the session has already seen for one symbol or file
// (spec §11), so the next turn can send a delta instead of the whole body.
type SymbolState struct {
	Path         string `json:"path"`
	Symbol       string `json:"symbol,omitempty"`
	ContentHash  string `json:"content_hash,omitempty"`
	SummaryHash  string `json:"summary_hash,omitempty"`
	Tokens       int    `json:"tokens,omitempty"`
	LastSeenTurn int    `json:"last_seen_turn,omitempty"`
	// ChangedRange is the region that differs from what was last delivered,
	// expressed as "L420-L450". Empty when unchanged.
	ChangedRange string `json:"changed_range,omitempty"`
}

// Key is the delta store index for a symbol state.
func (s SymbolState) Key() string {
	if s.Symbol == "" {
		return s.Path
	}
	return s.Path + "#" + s.Symbol
}

// DeltaDecision is what the optimizer tells the agent to do about a symbol it is
// about to retrieve.
type DeltaDecision string

const (
	// DeltaReuse: the session already has identical content. Do not retrieve.
	DeltaReuse DeltaDecision = "reuse"
	// DeltaRange: the content changed; retrieve only the changed region plus
	// its dependency context.
	DeltaRange DeltaDecision = "range"
	// DeltaFull: never seen before, retrieve it.
	DeltaFull DeltaDecision = "retrieve"
)

// Session is the mutable per-task optimizer state. It is safe for concurrent
// use because the MCP server and the HTTP API can both touch it.
type Session struct {
	mu sync.RWMutex

	state   SessionState
	symbols map[string]SymbolState
	// delivered maps content hash → candidate ID for everything already sent.
	delivered map[string]string
	// cached holds hashes the provider reported as cache reads. It is only
	// populated from observed usage, never from an assumption (spec §39).
	cached map[string]bool
	// errors deduplicates active errors by fingerprint.
	errors map[string]ErrorSignature
	// budget is the current allowance, grown by progressive expansion.
	budget Budget
	// expansions counts how many times the budget was expanded, used by the
	// stop conditions to detect a retrieval loop.
	expansions int
	// lastSnapshotHash is the StateHash of the last persisted snapshot, so a
	// no-change snapshot is skipped (spec §19.1).
	lastSnapshotHash string
}

// NewSession creates a optimizer session for a task.
func NewSession(taskID string, budget Budget) *Session {
	now := time.Now()
	return &Session{
		state: SessionState{
			TaskID:     taskID,
			Status:     "active",
			Validation: Validation{},
			StartedAt:  now,
			UpdatedAt:  now,
		},
		symbols:   map[string]SymbolState{},
		delivered: map[string]string{},
		cached:    map[string]bool{},
		errors:    map[string]ErrorSignature{},
		budget:    budget,
	}
}

// State returns a copy of the compact session state.
func (s *Session) State() SessionState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stateLocked()
}

func (s *Session) stateLocked() SessionState {
	out := s.state
	out.AffectedFiles = append([]string(nil), s.state.AffectedFiles...)
	out.Decisions = append([]string(nil), s.state.Decisions...)
	out.Resolved = append([]string(nil), s.state.Resolved...)
	out.Blockers = append([]string(nil), s.state.Blockers...)
	out.NextSteps = append([]string(nil), s.state.NextSteps...)
	out.RawRefs = append([]string(nil), s.state.RawRefs...)
	out.Validation = Validation{}
	for k, v := range s.state.Validation {
		out.Validation[k] = v
	}
	fps := make([]string, 0, len(s.errors))
	for fp := range s.errors {
		fps = append(fps, fp)
	}
	sort.Strings(fps)
	for _, fp := range fps {
		out.ActiveErrors = append(out.ActiveErrors, s.errors[fp])
	}
	return out
}

// Budget returns the current allowance.
func (s *Session) Budget() Budget {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.budget
}

// ExpandBudget grows the allowance for progressive expansion and reports how
// much was actually granted.
func (s *Session) ExpandBudget(additional int) (Budget, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next, granted := s.budget.Expand(additional)
	s.budget = next
	if granted > 0 {
		s.expansions++
	}
	return next, granted
}

// Expansions reports how many budget expansions this session has performed.
func (s *Session) Expansions() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.expansions
}

// NextTurn advances the turn counter and returns the new value.
func (s *Session) NextTurn() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Turn++
	s.state.UpdatedAt = time.Now()
	return s.state.Turn
}

// RegisterDelivered records that a set of candidates was sent to the model, so
// later turns can reuse them by hash instead of re-retrieving.
func (s *Session) RegisterDelivered(candidates []ContextCandidate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range candidates {
		if c.ContentHash == "" {
			continue
		}
		s.delivered[c.ContentHash] = c.ID
	}
	s.state.UpdatedAt = time.Now()
}

// SeenHashes returns the set of content hashes already delivered.
func (s *Session) SeenHashes() map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]bool, len(s.delivered))
	for h := range s.delivered {
		out[h] = true
	}
	return out
}

// MarkCached records hashes the provider reported as cache reads. Only observed
// data belongs here.
func (s *Session) MarkCached(hashes []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, h := range hashes {
		if h != "" {
			s.cached[h] = true
		}
	}
}

// CachedHashes returns the observed cached hash set.
func (s *Session) CachedHashes() map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]bool, len(s.cached))
	for h := range s.cached {
		out[h] = true
	}
	return out
}

// RankInput builds the ranker input from the current session.
func (s *Session) RankInput(cost CostModel) RankInput {
	return RankInput{
		Cost:         cost,
		CachedHashes: s.CachedHashes(),
		SeenHashes:   s.SeenHashes(),
		PrefixStable: len(s.CachedHashes()) > 0,
	}
}

// ObserveSymbol records the current hash of a symbol/file and returns what the
// agent should do about it (spec §11). It is the single decision point for
// "reuse, delta, or full retrieve".
func (s *Session) ObserveSymbol(next SymbolState) (DeltaDecision, SymbolState) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := next.Key()
	prev, existed := s.symbols[key]
	next.LastSeenTurn = s.state.Turn

	switch {
	case !existed:
		s.symbols[key] = next
		return DeltaFull, next
	case prev.ContentHash != "" && prev.ContentHash == next.ContentHash:
		// Unchanged: keep the previously recorded range info and do not
		// resend the body.
		prev.LastSeenTurn = s.state.Turn
		prev.ChangedRange = ""
		s.symbols[key] = prev
		return DeltaReuse, prev
	default:
		s.symbols[key] = next
		if next.ChangedRange != "" {
			return DeltaRange, next
		}
		return DeltaFull, next
	}
}

// Symbols returns a snapshot of the delta store, sorted by key for stable
// output.
func (s *Session) Symbols() []SymbolState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keys := make([]string, 0, len(s.symbols))
	for k := range s.symbols {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]SymbolState, 0, len(keys))
	for _, k := range keys {
		out = append(out, s.symbols[k])
	}
	return out
}

// SetObjective updates the task objective and phase.
func (s *Session) SetObjective(objective string, phase Phase) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if objective != "" {
		s.state.Objective = objective
	}
	if phase != "" {
		s.state.Phase = phase
	}
	s.state.UpdatedAt = time.Now()
}

// TouchFiles adds affected files, deduplicated and sorted.
func (s *Session) TouchFiles(files ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.AffectedFiles = mergeUnique(s.state.AffectedFiles, files)
	s.state.UpdatedAt = time.Now()
}

// AddDecision records a decision made during the task.
func (s *Session) AddDecision(decisions ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Decisions = mergeUnique(s.state.Decisions, decisions)
	s.state.UpdatedAt = time.Now()
}

// SetValidation records a validation result (typecheck/tests/build → status).
func (s *Session) SetValidation(kind, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Validation == nil {
		s.state.Validation = Validation{}
	}
	s.state.Validation[kind] = status
	s.state.UpdatedAt = time.Now()
}

// SetBlockers replaces the blocker list.
func (s *Session) SetBlockers(blockers []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Blockers = append([]string(nil), blockers...)
	s.state.UpdatedAt = time.Now()
}

// SetNextSteps replaces the next-step list.
func (s *Session) SetNextSteps(steps []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.NextSteps = append([]string(nil), steps...)
	s.state.UpdatedAt = time.Now()
}

// AddRawRef records a raw object reference produced during the task.
func (s *Session) AddRawRef(refs ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.RawRefs = mergeUnique(s.state.RawRefs, refs)
	s.state.UpdatedAt = time.Now()
}

// RecordError adds or increments an error by fingerprint. Repeated identical
// errors bump a counter instead of accumulating duplicates (spec §30).
// It returns the fingerprint and whether this was the first occurrence.
func (s *Session) RecordError(sig ErrorSignature) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fp := sig.Fingerprint()
	existing, ok := s.errors[fp]
	if ok {
		existing.Count++
		s.errors[fp] = existing
		s.state.UpdatedAt = time.Now()
		return fp, false
	}
	if sig.Count < 1 {
		sig.Count = 1
	}
	s.errors[fp] = sig
	s.state.UpdatedAt = time.Now()
	return fp, true
}

// ErrorOccurrences reports how many times an error signature was seen. Used by
// the stop conditions to detect "same error retried N times".
func (s *Session) ErrorOccurrences(sig ErrorSignature) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if e, ok := s.errors[sig.Fingerprint()]; ok {
		return e.Count
	}
	return 0
}

// ResolveError moves an error from active to resolved, keeping a one-line
// record so the knowledge that it happened is not lost.
func (s *Session) ResolveError(sig ErrorSignature, resolution string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	fp := sig.Fingerprint()
	existing, ok := s.errors[fp]
	if !ok {
		return false
	}
	delete(s.errors, fp)
	label := existing.Code
	if label == "" {
		label = existing.Message
	}
	line := strings.TrimSpace(label)
	if existing.File != "" {
		line += " in " + existing.File
	}
	if resolution != "" {
		line += " — " + resolution
	}
	s.state.Resolved = mergeUnique(s.state.Resolved, []string{line})
	s.state.UpdatedAt = time.Now()
	return true
}

// SetStatus updates the task status ("active", "done", "blocked").
func (s *Session) SetStatus(status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if status != "" {
		s.state.Status = status
	}
	s.state.UpdatedAt = time.Now()
}

// SnapshotNeeded reports whether the state changed meaningfully since the last
// persisted snapshot (spec §19.1). It returns the current state hash so the
// caller can pass it back to MarkSnapshotted.
func (s *Session) SnapshotNeeded() (bool, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	hash := s.stateLocked().StateHash()
	return hash != s.lastSnapshotHash, hash
}

// MarkSnapshotted records that a snapshot with the given state hash was
// persisted.
func (s *Session) MarkSnapshotted(hash string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastSnapshotHash = hash
}

// Describe renders the compact session state as the small YAML-ish block from
// spec §12. Kept deliberately terse: it is meant to be injected into a prompt.
func (s SessionState) Describe() string {
	var b strings.Builder
	b.WriteString("task:\n")
	fmt.Fprintf(&b, "  id: %s\n", s.TaskID)
	if s.Objective != "" {
		fmt.Fprintf(&b, "  objective: %s\n", s.Objective)
	}
	if s.Status != "" {
		fmt.Fprintf(&b, "  status: %s\n", s.Status)
	}
	if s.Phase != "" {
		fmt.Fprintf(&b, "  phase: %s\n", s.Phase)
	}
	writeYAMLList(&b, "affected_files", s.AffectedFiles)
	if len(s.Validation) > 0 {
		b.WriteString("validation:\n")
		keys := make([]string, 0, len(s.Validation))
		for k := range s.Validation {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "  %s: %s\n", k, s.Validation[k])
		}
	}
	writeYAMLList(&b, "resolved", s.Resolved)
	if len(s.ActiveErrors) > 0 {
		b.WriteString("active_errors:\n")
		for _, e := range s.ActiveErrors {
			label := e.Code
			if label == "" {
				label = e.Message
			}
			if e.File != "" {
				label += " (" + e.File + ")"
			}
			if e.Count > 1 {
				label = fmt.Sprintf("%s x%d", label, e.Count)
			}
			fmt.Fprintf(&b, "  - %s\n", label)
		}
	}
	writeYAMLList(&b, "blockers", s.Blockers)
	writeYAMLList(&b, "next_steps", s.NextSteps)
	writeYAMLList(&b, "raw_refs", s.RawRefs)
	return b.String()
}

func writeYAMLList(b *strings.Builder, key string, values []string) {
	if len(values) == 0 {
		return
	}
	fmt.Fprintf(b, "%s:\n", key)
	for _, v := range values {
		fmt.Fprintf(b, "  - %s\n", v)
	}
}

func mergeUnique(existing []string, additions []string) []string {
	seen := make(map[string]bool, len(existing))
	for _, v := range existing {
		seen[v] = true
	}
	for _, v := range additions {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		existing = append(existing, v)
	}
	return existing
}
