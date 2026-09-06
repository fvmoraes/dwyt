package outputgov

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/fvmoraes/dwyt/internal/contextgov"
)

func TestArtifactPhaseIsNeverCapped(t *testing.T) {
	p := ProfileFor(contextgov.PhaseArtifact)
	if !p.ArtifactException {
		t.Fatal("the artifact phase must carry the exception")
	}
	if p.TargetTokens != 0 || p.MaxTokens != 0 {
		t.Fatalf("a requested artifact must not be capped: %+v", p)
	}
	if p.Structured {
		t.Fatal("an artifact is prose or code, not a structured operational payload")
	}
	if !strings.Contains(p.Instructions(), "do not truncate") {
		t.Fatalf("instructions must state the exception: %q", p.Instructions())
	}
}

func TestOperationalPhasesTargetSmallOutput(t *testing.T) {
	for _, phase := range []contextgov.Phase{
		contextgov.PhaseClassify, contextgov.PhaseRetrieve,
		contextgov.PhaseToolLoop, contextgov.PhaseFix,
	} {
		p := ProfileFor(phase)
		if p.TargetTokens <= 0 || p.TargetTokens > OperationalTarget+100 {
			t.Fatalf("%s: target %d is outside the operational range", phase, p.TargetTokens)
		}
		if p.MaxTokens < p.TargetTokens {
			t.Fatalf("%s: max %d below target %d", phase, p.MaxTokens, p.TargetTokens)
		}
		if !p.Structured || !p.SuppressNarration {
			t.Fatalf("%s: operational phases should be structured and quiet: %+v", phase, p)
		}
	}
}

func TestReviewAndPlanGetRoomToBeUseful(t *testing.T) {
	review := ProfileFor(contextgov.PhaseReview)
	plan := ProfileFor(contextgov.PhasePlan)
	if review.TargetTokens <= OperationalTarget {
		t.Fatalf("a review needs more than an operational answer: %d", review.TargetTokens)
	}
	if plan.TargetTokens <= review.TargetTokens {
		t.Fatalf("a plan needs more room than a review: %d vs %d", plan.TargetTokens, review.TargetTokens)
	}
	if review.Structured || plan.Structured {
		t.Fatal("reviews and plans are prose deliverables, not status payloads")
	}
}

func TestProfileForTaskTypeMapsDocumentationToArtifact(t *testing.T) {
	for _, taskType := range []string{"document", "documentation", "readme", "report", "explain", "artifact"} {
		p := ProfileForTaskType(taskType, "")
		if !p.ArtifactException {
			t.Fatalf("%q must map to the artifact exception, got phase %s", taskType, p.Phase)
		}
	}
}

func TestExplicitPhaseOverridesTaskType(t *testing.T) {
	p := ProfileForTaskType("document", contextgov.PhaseFix)
	if p.ArtifactException {
		t.Fatal("an explicit phase must win over the inferred task type")
	}
}

func TestContractIsStableAndComplete(t *testing.T) {
	// The contract lives in a cacheable immutable prefix, so it must be a
	// constant. These are the clauses spec §34 requires verbatim.
	for _, clause := range []string{
		"Do not restate the request",
		"Do not narrate routine reasoning",
		"Do not paste code already written to files",
		"status, changed files, validation, blockers",
		"Expand when the requested output itself is an artifact",
	} {
		if !strings.Contains(Contract, clause) {
			t.Fatalf("output contract is missing %q", clause)
		}
	}
}

func TestResponseNormalizesStatus(t *testing.T) {
	cases := map[string]string{
		"completed": StatusDone,
		"OK":        StatusDone,
		"blocked":   StatusBlocked,
		"error":     StatusFailed,
		"whatever":  StatusInProgress,
	}
	for in, want := range cases {
		if got := NormalizeStatus(in); got != want {
			t.Fatalf("NormalizeStatus(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResponseJSONAlwaysHasTheRequiredShape(t *testing.T) {
	raw, err := NewResponse("done").JSON()
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatal(err)
	}
	// A consumer must never have to distinguish nil from empty.
	for _, key := range []string{"status", "changed", "validation", "blockers", "next"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("required key %q missing from %s", key, raw)
		}
	}
	if decoded["next"] != nil {
		t.Fatalf("next should be null when there is no next action: %s", raw)
	}
}

func TestResponseBuildersDeduplicateAndSort(t *testing.T) {
	r := NewResponse("done").
		WithChanged("b.go", "a.go", "b.go", "  ").
		WithBlockers("x", "x").
		WithValidation("tests", "PASS").
		WithNext("run the migration")

	if len(r.Changed) != 2 || r.Changed[0] != "a.go" {
		t.Fatalf("changed files must be deduplicated and sorted: %v", r.Changed)
	}
	if len(r.Blockers) != 1 {
		t.Fatalf("blockers must be deduplicated: %v", r.Blockers)
	}
	if r.Validation["tests"] != "pass" {
		t.Fatalf("validation results are normalized to lower case: %v", r.Validation)
	}
	if r.Next == nil || *r.Next != "run the migration" {
		t.Fatalf("next action not recorded: %+v", r.Next)
	}
	if r.WithNext("").Next != nil {
		t.Fatal("an empty next action must clear the field")
	}
}

func TestResponseTextIsOperationalNotProse(t *testing.T) {
	text := NewResponse("done").
		WithChanged("a.go").
		WithValidation("build", "pass").
		WithBlockers("waiting on review").
		Text()

	for _, want := range []string{"status: done", "a.go", "build=pass", "waiting on review"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in:\n%s", want, text)
		}
	}
	if len(strings.Split(strings.TrimSpace(text), "\n")) > 8 {
		t.Fatalf("operational text should stay tiny:\n%s", text)
	}
}

func TestSchemaDescribesTheResponse(t *testing.T) {
	s := Schema()
	props, ok := s["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("schema has no properties: %#v", s)
	}
	for _, key := range []string{"status", "changed", "validation", "blockers", "next"} {
		if _, ok := props[key]; !ok {
			t.Fatalf("schema missing property %q", key)
		}
	}
	if s["additionalProperties"] != false {
		t.Fatal("the operational schema must be closed so a model cannot pad it")
	}
}
