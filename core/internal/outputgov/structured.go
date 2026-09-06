package outputgov

import (
	"encoding/json"
	"sort"
	"strings"
)

// Structured operational responses (spec §35).
//
// A structured payload lets the dashboard render cards, badges and validation
// state without the model spending tokens "drawing" the UI in prose.

// Response is the canonical structured operational answer.
type Response struct {
	// Status is one of "done", "blocked", "in_progress", "failed".
	Status string `json:"status"`
	// Changed lists the files the turn modified.
	Changed []string `json:"changed"`
	// Validation maps a check name to its result ("pass", "fail", "pending",
	// "skipped").
	Validation map[string]string `json:"validation"`
	// Blockers lists what prevents completion. Always present (possibly
	// empty) so a consumer never has to distinguish nil from empty.
	Blockers []string `json:"blockers"`
	// Next is the recommended next action, or nil when the task is finished.
	Next *string `json:"next"`
	// Notes carries at most a few short remarks. It is not a place for prose.
	Notes []string `json:"notes,omitempty"`
}

// Statuses accepted by NormalizeStatus.
const (
	StatusDone       = "done"
	StatusInProgress = "in_progress"
	StatusBlocked    = "blocked"
	StatusFailed     = "failed"
)

// NormalizeStatus maps free-form status text onto the canonical set. An
// unrecognised value becomes "in_progress" rather than an error, because a
// mislabelled status must not fail a response.
func NormalizeStatus(s string) string {
	switch strings.ToLower(strings.TrimSpace(strings.ReplaceAll(s, "-", "_"))) {
	case "done", "complete", "completed", "success", "ok", "pass":
		return StatusDone
	case "blocked", "waiting", "needs_input":
		return StatusBlocked
	case "failed", "fail", "error":
		return StatusFailed
	case "in_progress", "active", "running", "partial":
		return StatusInProgress
	}
	return StatusInProgress
}

// NewResponse builds a normalized structured response.
func NewResponse(status string) Response {
	return Response{
		Status:     NormalizeStatus(status),
		Changed:    []string{},
		Validation: map[string]string{},
		Blockers:   []string{},
	}
}

// WithChanged sets the changed-file list, deduplicated and sorted so two runs
// that touched the same files produce identical output (which matters for
// caching and for diffing dashboard state).
func (r Response) WithChanged(files ...string) Response {
	r.Changed = sortedUnique(append(r.Changed, files...))
	return r
}

// WithValidation records a validation result.
func (r Response) WithValidation(check, result string) Response {
	if r.Validation == nil {
		r.Validation = map[string]string{}
	}
	r.Validation[check] = strings.ToLower(strings.TrimSpace(result))
	return r
}

// WithBlockers sets the blocker list.
func (r Response) WithBlockers(blockers ...string) Response {
	r.Blockers = sortedUnique(append(r.Blockers, blockers...))
	return r
}

// WithNext sets the recommended next action. An empty string clears it.
func (r Response) WithNext(next string) Response {
	next = strings.TrimSpace(next)
	if next == "" {
		r.Next = nil
		return r
	}
	r.Next = &next
	return r
}

// JSON renders the response as compact JSON.
func (r Response) JSON() (string, error) {
	if r.Changed == nil {
		r.Changed = []string{}
	}
	if r.Validation == nil {
		r.Validation = map[string]string{}
	}
	if r.Blockers == nil {
		r.Blockers = []string{}
	}
	data, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Text renders the response as the minimal human-readable form: one line for
// status, one bullet per changed file, one line for validation, one for
// blockers. It exists so a client that cannot consume JSON still gets an
// operational answer instead of prose.
func (r Response) Text() string {
	var b strings.Builder
	b.WriteString("status: ")
	b.WriteString(r.Status)
	b.WriteString("\n")
	if len(r.Changed) > 0 {
		b.WriteString("changed:\n")
		for _, f := range r.Changed {
			b.WriteString("  - ")
			b.WriteString(f)
			b.WriteString("\n")
		}
	}
	if len(r.Validation) > 0 {
		b.WriteString("validation: ")
		keys := make([]string, 0, len(r.Validation))
		for k := range r.Validation {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, k+"="+r.Validation[k])
		}
		b.WriteString(strings.Join(parts, " "))
		b.WriteString("\n")
	}
	if len(r.Blockers) > 0 {
		b.WriteString("blockers:\n")
		for _, blocker := range r.Blockers {
			b.WriteString("  - ")
			b.WriteString(blocker)
			b.WriteString("\n")
		}
	}
	if r.Next != nil {
		b.WriteString("next: ")
		b.WriteString(*r.Next)
		b.WriteString("\n")
	}
	return b.String()
}

// Schema is the JSON schema of Response, for providers that support structured
// output enforcement. Returning it as a Go map (not a string) lets a provider
// adapter embed it directly in a request body.
func Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"status": map[string]interface{}{
				"type": "string",
				"enum": []string{StatusDone, StatusInProgress, StatusBlocked, StatusFailed},
			},
			"changed": map[string]interface{}{
				"type":  "array",
				"items": map[string]interface{}{"type": "string"},
			},
			"validation": map[string]interface{}{
				"type":                 "object",
				"additionalProperties": map[string]interface{}{"type": "string"},
			},
			"blockers": map[string]interface{}{
				"type":  "array",
				"items": map[string]interface{}{"type": "string"},
			},
			"next": map[string]interface{}{
				"type": []string{"string", "null"},
			},
		},
		"required":             []string{"status", "changed", "validation", "blockers"},
		"additionalProperties": false,
	}
}

func sortedUnique(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
