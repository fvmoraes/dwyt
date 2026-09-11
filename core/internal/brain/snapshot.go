package brain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Compact Snapshot v2 (spec §19).
//
// A pre-v5 snapshot carried the whole conversation context, which meant a
// hundred sessions in a vault were a hundred transcripts nobody would ever
// read. V2 carries meaning only: objective, affected files, decisions,
// validation, active errors, resolved items, next steps and raw references —
// typically 200–500 tokens.
//
// The other half of the change is the StateHash: a snapshot is only written when
// the state it describes actually changed. Persisting on every final response
// (the pre-v5 rule) generated near-identical notes whose only difference was a
// timestamp.

// CompactSnapshot is the v2 session record.
type CompactSnapshot struct {
	Client         string            `json:"client,omitempty"`
	ConversationID string            `json:"conversation_id,omitempty"`
	Objective      string            `json:"objective,omitempty"`
	Status         string            `json:"status,omitempty"`
	AffectedFiles  []string          `json:"affected_files,omitempty"`
	Decisions      []string          `json:"decisions,omitempty"`
	Validation     map[string]string `json:"validation,omitempty"`
	ActiveErrors   []string          `json:"active_errors,omitempty"`
	Resolved       []string          `json:"resolved,omitempty"`
	Blockers       []string          `json:"blockers,omitempty"`
	NextSteps      []string          `json:"next_steps,omitempty"`
	RawRefs        []string          `json:"raw_refs,omitempty"`
	// KnowledgePromoted names the canonical notes this session updated, so a
	// reader can follow the durable trail instead of the session itself.
	KnowledgePromoted []string `json:"knowledge_promoted,omitempty"`
	// Context is a short free-form note for a future agent. It is capped on
	// write: a snapshot is not a transcript.
	Context string `json:"context,omitempty"`
}

// maxSnapshotContextChars bounds the free-form section. ~1200 characters is
// roughly 300 tokens, which keeps a snapshot inside the spec's 200–500 token
// target even with every list populated.
const maxSnapshotContextChars = 1200

// StateHash is the identity of the meaningful state (spec §19.1). Client,
// conversation id and the free-form context are excluded: a new conversation
// describing the same state is the same state, and prose phrasing must not
// force a new snapshot.
func (s CompactSnapshot) StateHash() string {
	h := sha256.New()
	write := func(values ...string) {
		for _, v := range values {
			h.Write([]byte(strings.TrimSpace(v)))
			h.Write([]byte{0})
		}
	}
	write(s.Objective, s.Status)
	for _, list := range [][]string{
		s.AffectedFiles, s.Decisions, s.ActiveErrors, s.Resolved,
		s.Blockers, s.NextSteps, s.RawRefs, s.KnowledgePromoted,
	} {
		sorted := append([]string(nil), list...)
		sort.Strings(sorted)
		write(sorted...)
	}
	keys := make([]string, 0, len(s.Validation))
	for k := range s.Validation {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		write(k, s.Validation[k])
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// Empty reports whether the snapshot carries no state worth persisting.
func (s CompactSnapshot) Empty() bool {
	return strings.TrimSpace(s.Objective) == "" &&
		len(s.AffectedFiles) == 0 && len(s.Decisions) == 0 &&
		len(s.ActiveErrors) == 0 && len(s.Resolved) == 0 &&
		len(s.Blockers) == 0 && len(s.NextSteps) == 0 &&
		len(s.Validation) == 0 && strings.TrimSpace(s.Context) == ""
}

// SnapshotOutcome reports what SaveCompactSnapshot did.
type SnapshotOutcome struct {
	// Written is false when the snapshot was skipped as a no-op.
	Written bool `json:"written"`
	// Skipped explains a skip in one line.
	Skipped string `json:"skipped,omitempty"`
	Path    string `json:"path,omitempty"`
	// StateHash is the hash of the state, whether written or skipped, so the
	// caller can record it.
	StateHash string `json:"state_hash"`
	TokensEst int    `json:"tokens_est,omitempty"`
}

// snapshotDir is the v5 location for compact sessions (spec §14).
const snapshotDir = "90-sessions/compact"

// SaveCompactSnapshot writes a compact session snapshot, skipping the write when
// an identical state was already persisted (spec §19.1).
//
// The scan for an existing hash reads only frontmatter-sized prefixes of the
// existing snapshots, so the check stays cheap even in a vault at the
// 100-session limit.
func (pb *ProjectObsidian) SaveCompactSnapshot(s CompactSnapshot) (SnapshotOutcome, error) {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	defer pb.invalidateStats()

	hash := s.StateHash()
	outcome := SnapshotOutcome{StateHash: hash}

	if s.Empty() {
		outcome.Skipped = "no task state to persist"
		return outcome, nil
	}

	dir := filepath.Join(pb.brainDir, filepath.FromSlash(snapshotDir))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return outcome, fmt.Errorf("obsidian snapshot: %w", err)
	}

	if existing, err := findSnapshotByStateHash(dir, hash); err == nil && existing != "" {
		// Same state as an existing snapshot: refresh its access metadata so
		// the housekeeper sees the session is still live, but do not create a
		// second near-identical note.
		TouchAccess(existing, time.Now())
		outcome.Skipped = "state unchanged since the last snapshot"
		outcome.Path = existing
		return outcome, nil
	}

	now := time.Now()
	pb.UpdatedAt = now

	if len(s.Context) > maxSnapshotContextChars {
		s.Context = s.Context[:maxSnapshotContextChars-3] + "..."
	}

	name := fmt.Sprintf("session-%s-%s.md", now.Format("20060102-150405"), shortHash(hash))
	path := filepath.Join(dir, name)

	lc := NewLifecycle("context", now)
	lc.StateHash = hash
	lc.RawRefs = s.RawRefs
	body := renderCompactSnapshot(s, lc, now, pb)

	if err := atomicWriteFile(path, []byte(body), 0o644); err != nil {
		return outcome, fmt.Errorf("obsidian snapshot: %w", err)
	}
	outcome.Written = true
	outcome.Path = path
	outcome.TokensEst = estimateTokens(body)
	return outcome, nil
}

// findSnapshotByStateHash looks for an existing snapshot with the given state
// hash. It reads only the first 1 KiB of each file (frontmatter lives there),
// so the scan cost is bounded regardless of snapshot size.
func findSnapshotByStateHash(dir, hash string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	needle := "state_hash: " + hash
	buf := make([]byte, 1024)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		n, _ := f.Read(buf)
		f.Close()
		if n > 0 && strings.Contains(string(buf[:n]), needle) {
			return path, nil
		}
	}
	return "", nil
}

func shortHash(hash string) string {
	trimmed := strings.TrimPrefix(hash, "sha256:")
	if len(trimmed) > 8 {
		return trimmed[:8]
	}
	return trimmed
}

func renderCompactSnapshot(s CompactSnapshot, lc Lifecycle, now time.Time, pb *ProjectObsidian) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("tags: [dwyt, session, compact]\n")
	b.WriteString(lc.Render())
	if s.Client != "" {
		fmt.Fprintf(&b, "client: %q\n", s.Client)
	}
	if s.ConversationID != "" {
		fmt.Fprintf(&b, "conversation_id: %q\n", s.ConversationID)
	}
	fmt.Fprintf(&b, "project: %q\n", pb.ProjectName)
	b.WriteString("---\n\n")

	title := strings.TrimSpace(s.Objective)
	if title == "" {
		title = "Session " + now.Format("2006-01-02 15:04")
	}
	fmt.Fprintf(&b, "# %s\n\n", title)
	b.WriteString("Links: [[index]] [[maps/project-map]] [[20-decisions/index]]\n\n")

	if s.Status != "" {
		fmt.Fprintf(&b, "status: %s\n\n", s.Status)
	}
	writeSnapshotList(&b, "Changes", s.AffectedFiles)
	writeSnapshotList(&b, "Decisions", s.Decisions)
	if len(s.Validation) > 0 {
		b.WriteString("## Validation\n\n")
		keys := make([]string, 0, len(s.Validation))
		for k := range s.Validation {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "- %s: %s\n", k, s.Validation[k])
		}
		b.WriteString("\n")
	}
	writeSnapshotList(&b, "Active Errors", s.ActiveErrors)
	writeSnapshotList(&b, "Resolved", s.Resolved)
	writeSnapshotList(&b, "Blockers", s.Blockers)
	writeSnapshotList(&b, "Next Steps", s.NextSteps)
	writeSnapshotList(&b, "Knowledge Promoted", s.KnowledgePromoted)
	writeSnapshotList(&b, "Raw Refs", s.RawRefs)
	if strings.TrimSpace(s.Context) != "" {
		fmt.Fprintf(&b, "## Context\n\n%s\n", strings.TrimSpace(s.Context))
	}
	return b.String()
}

func writeSnapshotList(b *strings.Builder, title string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "## %s\n\n", title)
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		fmt.Fprintf(b, "- %s\n", strings.ReplaceAll(item, "\n", " "))
	}
	b.WriteString("\n")
}

// CompactFromContextSnapshot converts the pre-v5 rich payload into a v2 compact
// snapshot. It is what keeps the existing `obsidian_save_context` tool and the
// `/api/obsidian/context` endpoint working unchanged: callers keep sending what
// they always sent, and DWYT reduces it on the way in.
//
// The mapping is deliberately lossy on purpose. Commands and long prose are the
// bulk of a pre-v5 snapshot and the least reusable part of it; the durable
// value is in decisions, files, errors and next steps.
func CompactFromContextSnapshot(in ContextSnapshot) CompactSnapshot {
	out := CompactSnapshot{
		Client:         in.Client,
		ConversationID: in.ConversationID,
		Objective:      firstLine(firstNonEmptyString(in.Summary, in.UserRequest)),
		Status:         normalizeSnapshotStatus(in.Outcome),
		AffectedFiles:  in.Files,
		Decisions:      in.Decisions,
		ActiveErrors:   in.Errors,
		NextSteps:      in.NextSteps,
		Context:        in.Context,
	}
	// The pre-v5 payload has no validation map; derive the one signal that is
	// reliably present rather than inventing entries.
	if in.Outcome != "" {
		out.Validation = map[string]string{"outcome": normalizeSnapshotStatus(in.Outcome)}
	}
	// Raw references embedded in the free-form fields must survive so the
	// housekeeper does not prune the objects they cite.
	out.RawRefs = extractRawRefs(in.Context, in.Summary, strings.Join(in.Actions, "\n"), strings.Join(in.Commands, "\n"))
	return out
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.IndexAny(s, "\r\n"); idx >= 0 {
		s = s[:idx]
	}
	const maxObjective = 160
	if len(s) > maxObjective {
		s = s[:maxObjective-3] + "..."
	}
	return s
}

func normalizeSnapshotStatus(outcome string) string {
	lower := strings.ToLower(strings.TrimSpace(outcome))
	switch {
	case lower == "":
		return ""
	case strings.Contains(lower, "block"):
		return "blocked"
	case strings.Contains(lower, "fail"), strings.Contains(lower, "error"):
		return "failed"
	case strings.Contains(lower, "progress"), strings.Contains(lower, "partial"):
		return "in_progress"
	case strings.Contains(lower, "success"), strings.Contains(lower, "done"),
		strings.Contains(lower, "complete"), strings.Contains(lower, "ok"):
		return "done"
	}
	return "in_progress"
}

// extractRawRefs finds `dwyt://objects/<id>` references in free text.
func extractRawRefs(sources ...string) []string {
	const scheme = "dwyt://objects/"
	seen := map[string]bool{}
	var out []string
	for _, src := range sources {
		rest := src
		for {
			idx := strings.Index(rest, scheme)
			if idx < 0 {
				break
			}
			rest = rest[idx+len(scheme):]
			end := 0
			for end < len(rest) && isHexByte(rest[end]) {
				end++
			}
			if end == 0 {
				continue
			}
			ref := scheme + strings.ToLower(rest[:end])
			if !seen[ref] {
				seen[ref] = true
				out = append(out, ref)
			}
			rest = rest[end:]
		}
	}
	sort.Strings(out)
	return out
}

func isHexByte(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}
