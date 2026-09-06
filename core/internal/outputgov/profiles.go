// Package outputgov governs the *output* side of a request (spec §33–§36).
//
// Reducing input and then letting the model emit ten thousand tokens of
// narration is not a saving. The output governor assigns a visible-token
// target per phase, states the Output Efficiency Contract in a form small
// enough to inject into a prompt, and — critically — encodes the artifact
// exception so a requested document is never truncated to satisfy an
// operational rule.
package outputgov

import (
	"strings"

	"github.com/fvmoraes/dwyt/internal/contextgov"
)

// Profile is the output allowance for one phase of the agent loop.
type Profile struct {
	Phase contextgov.Phase `json:"phase"`
	// TargetTokens is the visible-token target. Zero means "no operational
	// cap" — used by the artifact phase.
	TargetTokens int `json:"target_tokens"`
	// MaxTokens is the safe operational ceiling. Zero means unbounded.
	MaxTokens int `json:"max_tokens"`
	// Structured is true when the response should be a structured operational
	// payload rather than prose.
	Structured bool `json:"structured"`
	// ArtifactException marks a phase whose output *is* the deliverable.
	ArtifactException bool `json:"artifact_exception"`
	// SuppressNarration asks the model not to narrate routine reasoning or
	// tool use.
	SuppressNarration bool `json:"suppress_narration"`
}

// Operational defaults from spec §33: a normal operational answer targets
// ~300 visible tokens with a safe ceiling around 500.
const (
	OperationalTarget = 300
	OperationalMax    = 500
)

// ProfileFor returns the output profile for a phase.
func ProfileFor(phase contextgov.Phase) Profile {
	target := contextgov.OutputTargetForPhase(phase)
	p := Profile{
		Phase:             phase,
		TargetTokens:      target,
		Structured:        true,
		SuppressNarration: true,
	}
	switch phase {
	case contextgov.PhaseArtifact:
		// The artifact exception: the requested output is the product.
		p.TargetTokens = 0
		p.MaxTokens = 0
		p.Structured = false
		p.ArtifactException = true
	case contextgov.PhaseReview:
		p.MaxTokens = 2000
		p.Structured = false
	case contextgov.PhasePlan:
		p.MaxTokens = 6000
		p.Structured = false
	default:
		p.MaxTokens = OperationalMax
		if target > OperationalMax {
			p.MaxTokens = target
		}
	}
	return p
}

// ProfileForTaskType maps a task type string to a profile. It is the entry
// point used by the MCP tool, where the caller says "review" or "document"
// rather than naming an internal phase.
func ProfileForTaskType(taskType string, phase contextgov.Phase) Profile {
	if phase == "" {
		phase = phaseFromTaskType(taskType)
	}
	return ProfileFor(phase)
}

// phaseFromTaskType maps user-facing task vocabulary onto phases. Anything
// that produces a document for the user maps to the artifact phase so it is
// never truncated.
func phaseFromTaskType(taskType string) contextgov.Phase {
	switch strings.ToLower(strings.TrimSpace(taskType)) {
	case "classify", "classification", "route":
		return contextgov.PhaseClassify
	case "retrieve", "search", "explore":
		return contextgov.PhaseRetrieve
	case "tool", "tools", "build", "test", "execute":
		return contextgov.PhaseToolLoop
	case "fix", "bug", "patch", "edit", "implement":
		return contextgov.PhaseFix
	case "review", "audit":
		return contextgov.PhaseReview
	case "plan", "spec", "design":
		return contextgov.PhasePlan
	case "artifact", "document", "documentation", "readme", "report", "explain":
		return contextgov.PhaseArtifact
	}
	return contextgov.PhaseFix
}

// Contract is the Output Efficiency Contract (spec §34) in the exact compact
// form meant to be injected into a prompt. It is a constant so it is
// byte-identical on every request, which is what lets it live inside a
// cacheable immutable prefix.
const Contract = `OUTPUT EFFICIENCY

- Do not restate the request or repository context.
- Do not narrate routine reasoning or tool usage.
- Do not paste code already written to files.
- Do not repeat plans already persisted.
- Prefer tools/actions over prose when the next action is clear.
- Operational final response: status, changed files, validation, blockers.
- Be concise by default.
- Expand when the requested output itself is an artifact, explanation,
  review, plan or documentation.`

// Instructions renders the phase-specific output guidance. It stays short on
// purpose: the full contract is already in the immutable prefix, so this only
// adds what changes per phase.
func (p Profile) Instructions() string {
	var b strings.Builder
	if p.ArtifactException {
		b.WriteString("Output is the requested artifact: do not truncate it. ")
		b.WriteString("Still avoid restating the request or narrating tool usage.")
		return b.String()
	}
	b.WriteString("Target ")
	b.WriteString(itoa(p.TargetTokens))
	b.WriteString(" visible tokens")
	if p.MaxTokens > 0 {
		b.WriteString(" (max ")
		b.WriteString(itoa(p.MaxTokens))
		b.WriteString(")")
	}
	b.WriteString(". ")
	if p.Structured {
		b.WriteString("Respond with status, changed files, validation and blockers only. ")
	}
	if p.SuppressNarration {
		b.WriteString("Analyze internally and execute; do not narrate routine reasoning.")
	}
	return strings.TrimSpace(b.String())
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
