package optimizer

import (
	"context"

	"github.com/fvmoraes/dwyt/internal/contextopt"
	"github.com/fvmoraes/dwyt/internal/outputopt"
	"github.com/fvmoraes/dwyt/internal/telemetry"
)

// Routing surface of the Optimizer (spec §46, §50).
//
// The Optimizer exposes routing as a *recommendation*, not an instruction. DWYT
// does not control which model an IDE sends a request to, so pretending to
// dictate it would be the same overreach as claiming enforced cache control. What
// it can do — and does here — is classify deterministically and say why.

// RouteRequest is the input to `dwyt_route`.
type RouteRequest struct {
	TaskID string `json:"task_id,omitempty"`
	Task   string `json:"task,omitempty"`
	Phase  string `json:"phase,omitempty"`

	Files     int `json:"files,omitempty"`
	Modules   int `json:"modules,omitempty"`
	Languages int `json:"languages,omitempty"`
	DiffLines int `json:"diff_lines,omitempty"`

	TouchesArchitecture   bool `json:"touches_architecture,omitempty"`
	TouchesSecurity       bool `json:"touches_security,omitempty"`
	TouchesInfrastructure bool `json:"touches_infrastructure,omitempty"`
	Destructive           bool `json:"destructive,omitempty"`
	HasTests              bool `json:"has_tests,omitempty"`

	PriorFailures int     `json:"prior_failures,omitempty"`
	Confidence    float64 `json:"confidence,omitempty"`
}

// RouteResponse is the routing recommendation.
type RouteResponse struct {
	TaskID   string                   `json:"task_id"`
	Decision contextopt.RouteDecision `json:"decision"`
	// Budget is the context budget implied by the classification, so a caller
	// gets one coherent answer rather than having to ask twice.
	Budget contextopt.Budget `json:"budget"`
	// Output is the output profile for the routed phase.
	Output outputopt.Profile `json:"output"`
	// Limits are the cost/token stop limits adapted to the routed tier.
	Limits contextopt.StopLimits `json:"limits"`
	// Note states plainly that this is a recommendation.
	Note          string `json:"note"`
	PolicyVersion string `json:"policy_version"`
}

// Route classifies a task and recommends a tier (`dwyt_route`).
//
// Signals the caller does not supply are simply absent: an unknown task routes to
// the mid tier, because guessing "cheap" for something DWYT knows nothing about
// risks a failure that costs more than the saving.
func (g *Optimizer) Route(req RouteRequest) RouteResponse {
	_, span := g.Tracer().Start(context.Background(), telemetry.SpanClassification, map[string]interface{}{
		"phase": req.Phase,
		"files": req.Files,
	})
	defer span.End()

	phase := contextopt.ParsePhase(req.Phase)
	signals := contextopt.Signals{
		Files:                 req.Files,
		Modules:               req.Modules,
		Languages:             req.Languages,
		DiffLines:             req.DiffLines,
		TouchesArchitecture:   req.TouchesArchitecture,
		TouchesSecurity:       req.TouchesSecurity,
		TouchesInfrastructure: req.TouchesInfrastructure,
		Destructive:           req.Destructive,
		HasTests:              req.HasTests,
		PriorFailures:         req.PriorFailures,
		Confidence:            req.Confidence,
		Phase:                 phase,
	}
	// Failure history the optimizer already observed outranks what the caller
	// reported: the session state is measured, the argument is claimed.
	if sess, ok := g.Session(req.TaskID); ok {
		state := sess.State()
		for _, e := range state.ActiveErrors {
			if e.Count > signals.RepeatedError {
				signals.RepeatedError = e.Count
			}
		}
		if len(state.AffectedFiles) > signals.Files {
			signals.Files = len(state.AffectedFiles)
		}
	}

	cfg := g.Config()
	decision := contextopt.Route(signals, cfg.Routing)

	budget := contextopt.ComputeBudget(contextopt.BudgetProfile{
		Phase:          phase,
		Complexity:     decision.Classification,
		DefaultBudget:  cfg.DefaultBudget,
		MaxBudget:      cfg.MaxBudget,
		ReservePercent: cfg.ReservePercent,
	})

	span.SetAll(map[string]interface{}{
		"tier":       string(decision.Tier),
		"complexity": decision.Complexity.Value,
		"risk":       decision.Risk.Value,
		"escalate":   decision.Escalate,
	})

	return RouteResponse{
		TaskID:   req.TaskID,
		Decision: decision,
		Budget:   budget,
		Output:   g.OutputProfile("", string(decision.OutputPhase)),
		Limits:   contextopt.AdaptiveBudget(cfg.StopLimits, decision),
		Note: "A recommendation, not an instruction: DWYT does not control which " +
			"model the client sends the request to. Tiers are deliberately not " +
			"mapped to model names, which change too often to hardcode.",
		PolicyVersion: PolicyVersion,
	}
}
