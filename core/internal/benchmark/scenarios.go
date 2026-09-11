package benchmark

import (
	_ "embed"
	"fmt"

	"github.com/fvmoraes/dwyt/internal/contextopt"
)

// Fixtures are embedded from testdata so the CLI keeps working from any
// directory while its corpus remains reviewable as deterministic files rather
// than hidden in generated Go strings.
var (
	//go:embed testdata/tsc-failure-large.txt
	tscFailureOutput string
	//go:embed testdata/go-test-failure-large.txt
	goTestFailureOutput string
	//go:embed testdata/build-failure-large.txt
	buildFailureOutput string
	//go:embed testdata/go-test-pass-large.txt
	goTestPassOutput string
	//go:embed testdata/payload-large.json
	largeJSONOutput string
	//go:embed testdata/server-large.log
	largeLogOutput string
	//go:embed testdata/compression-negative.txt
	compressionNegativeOutput string
)

// block builds a Block with a stable content hash so delta reuse and cache
// classification work exactly as they do in production.
func block(kind contextopt.Kind, ref string, tokens, fullFile int, relevance float64) Block {
	c := contextopt.ContextCandidate{
		Kind:        kind,
		Title:       ref,
		Tokens:      tokens,
		Relevance:   relevance,
		ContentRef:  ref,
		ContentHash: contextopt.HashContent(string(kind) + "|" + ref),
		Source:      sourceFor(kind),
	}
	c.Normalize()
	return Block{Candidate: c, FullFileTokens: fullFile}
}

func sourceFor(kind contextopt.Kind) string {
	switch kind {
	case contextopt.KindProjectIdentity, contextopt.KindArchitecture,
		contextopt.KindDecision, contextopt.KindConvention, contextopt.KindConstraint:
		return "obsidian"
	case contextopt.KindToolOutput, contextopt.KindError, contextopt.KindRawLog:
		return "tool"
	case contextopt.KindSessionState, contextopt.KindSessionHistory:
		return "session"
	default:
		return "codebase"
	}
}

// noiseBlocks is the part of the repository that is available but irrelevant.
// Every real task has one: the ungoverned arms pay for it, the optimizer ranks
// it out. Relevance is low but non-zero, because "irrelevant" is a judgement
// the ranker has to make rather than a fact it is told.
func noiseBlocks(prefix string, count, tokens, fullFile int) []Block {
	out := make([]Block, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, block(
			contextopt.KindFileSummary,
			fmt.Sprintf("%s/unrelated_%02d", prefix, i),
			tokens, fullFile, 0.12,
		))
	}
	return out
}

// sessionNotes generates the vault content a long-running project accumulates:
// mostly session snapshots, some raw logs, a few superseded notes. Search V2
// excludes raw and stale content and keeps a small canonical top-k; the broad
// pre-v5 search returned all of it by recency.
func sessionNotes(count, tokens int) []VaultNote {
	out := make([]VaultNote, 0, count)
	for i := 0; i < count; i++ {
		n := VaultNote{
			Key:    fmt.Sprintf("sessions/session-%03d", i),
			Tokens: tokens,
		}
		switch i % 5 {
		case 0:
			n.Key = fmt.Sprintf("errors/error-%03d", i)
			n.Raw = true
		case 3:
			n.Stale = true
		}
		out = append(out, n)
	}
	return out
}

func canonicalNotes(entries ...VaultNote) []VaultNote {
	out := make([]VaultNote, 0, len(entries))
	for _, e := range entries {
		e.Canonical = true
		out = append(out, e)
	}
	return out
}

// A — trivial question --------------------------------------------------------

func trivialScenario() Scenario {
	blocks := []Block{
		block(contextopt.KindFileSummary, "README.md#install", 180, 2400, 0.95),
		block(contextopt.KindConvention, "conventions/docs-style", 140, 0, 0.6),
		block(contextopt.KindProjectIdentity, "project/identity", 160, 0, 0.5),
	}
	blocks = append(blocks, noiseBlocks("docs", 8, 320, 1800)...)

	notes := canonicalNotes(
		VaultNote{Key: "maps/project-map", Tokens: 260},
		VaultNote{Key: "conventions/docs", Tokens: 180},
	)
	notes = append(notes, sessionNotes(18, 210)...)

	return Scenario{
		ID:                     "A",
		Name:                   "trivial question",
		Complexity:             contextopt.ComplexityTrivial,
		Phase:                  contextopt.PhaseFix,
		Description:            "One-line documentation question. The honest benchmark must show that it does not load a repository for it.",
		Blocks:                 blocks,
		VaultNotes:             notes,
		Turns:                  1,
		CompressionExpectation: CompressionNotNeeded,
	}
}

// B — symbol lookup -----------------------------------------------------------

func smallFrontendScenario() Scenario {
	blocks := []Block{
		block(contextopt.KindSymbol, "web/src/components/CardOptimizer.tsx#CardOptimizer", 620, 3100, 0.95),
		block(contextopt.KindSymbol, "web/src/api.ts#fetchOptimizerState", 240, 4200, 0.9),
		block(contextopt.KindTest, "web/src/components/CardOptimizer.test.tsx", 380, 900, 0.6),
		block(contextopt.KindModuleSummary, "web/src/components", 300, 0, 0.7),
		block(contextopt.KindConvention, "conventions/react", 220, 0, 0.55),
	}
	blocks = append(blocks, noiseBlocks("web/src/components", 14, 480, 2600)...)

	notes := canonicalNotes(
		VaultNote{Key: "decisions/dashboard-cards", Tokens: 320},
		VaultNote{Key: "maps/frontend-map", Tokens: 280},
	)
	notes = append(notes, sessionNotes(24, 240)...)

	return Scenario{
		ID:          "B",
		Name:        "symbol lookup in a frontend bug",
		Complexity:  contextopt.ComplexitySimple,
		Phase:       contextopt.PhaseFix,
		Description: "A component renders a stale value. Two symbols matter; the rest of the component tree does not.",
		Blocks:      blocks,
		VaultNotes:  notes,
		ToolOutput:  tscFailureOutput,
		Turns:       2,
	}
}

// C — debug a large failing test output --------------------------------------

func mediumGoScenario() Scenario {
	blocks := []Block{
		block(contextopt.KindSymbol, "internal/brain/search.go#SearchV2", 780, 5200, 0.95),
		block(contextopt.KindSymbol, "internal/brain/temperature.go#Classify", 420, 2600, 0.85),
		block(contextopt.KindSymbol, "internal/db/db.go#Query", 360, 6100, 0.7),
		block(contextopt.KindTest, "internal/brain/search_test.go", 640, 1900, 0.8),
		block(contextopt.KindTest, "internal/brain/temperature_test.go", 520, 1500, 0.65),
		block(contextopt.KindModuleSummary, "internal/brain", 340, 0, 0.75),
		block(contextopt.KindDependency, "internal/brain -> internal/db", 180, 0, 0.6),
		block(contextopt.KindDecision, "decisions/search-v2-topk", 260, 0, 0.7),
	}
	blocks = append(blocks, noiseBlocks("internal", 22, 520, 3400)...)

	notes := canonicalNotes(
		VaultNote{Key: "decisions/search-v2", Tokens: 340},
		VaultNote{Key: "instructions/obsidian-law", Tokens: 420},
		VaultNote{Key: "maps/project-map", Tokens: 300},
	)
	notes = append(notes, sessionNotes(36, 260)...)

	return Scenario{
		ID:          "C",
		Name:        "debug large failed test output",
		Complexity:  contextopt.ComplexityMedium,
		Phase:       contextopt.PhaseFix,
		Description: "A search returns stale notes. The fix touches three files and must keep tests green despite a noisy failing test log.",
		Blocks:      blocks,
		VaultNotes:  notes,
		ToolOutput:  goTestFailureOutput,
		Turns:       4,
	}
}

// D — cross-file impact analysis ---------------------------------------------

func complexRefactorScenario() Scenario {
	blocks := []Block{
		block(contextopt.KindArchitecture, "architecture/optimizer-pipeline", 620, 0, 0.9),
		block(contextopt.KindDecision, "decisions/rename-governor-optimizer", 280, 0, 0.85),
		block(contextopt.KindConstraint, "constraints/no-breaking-mcp-names", 200, 0, 0.95),
		block(contextopt.KindSymbol, "internal/optimizer/optimizer.go#Optimizer", 1100, 7400, 0.95),
		block(contextopt.KindSymbol, "internal/contextopt/ranker.go#Rank", 680, 4900, 0.9),
		block(contextopt.KindSymbol, "internal/mcpregistry/registry.go#ServerName", 320, 3300, 0.9),
		block(contextopt.KindSymbol, "web/src/api.ts#optimizerState", 300, 4200, 0.85),
		block(contextopt.KindSymbol, "web/src/i18n.ts#optimizerKeys", 260, 5600, 0.7),
		block(contextopt.KindTest, "internal/optimizer/optimizer_test.go", 720, 2100, 0.75),
		block(contextopt.KindTest, "internal/mcpregistry/registry_test.go", 480, 1400, 0.7),
		block(contextopt.KindModuleSummary, "internal/optimizer", 360, 0, 0.8),
		block(contextopt.KindModuleSummary, "web/src", 340, 0, 0.7),
		block(contextopt.KindDependency, "internal/optimizer -> internal/contextopt", 200, 0, 0.75),
	}
	blocks = append(blocks, noiseBlocks("internal", 30, 560, 3600)...)
	blocks = append(blocks, noiseBlocks("web/src", 18, 500, 2800)...)

	notes := canonicalNotes(
		VaultNote{Key: "decisions/mcp-namespacing", Tokens: 380},
		VaultNote{Key: "architecture/v5-overview", Tokens: 520},
		VaultNote{Key: "instructions/codebase-law", Tokens: 400},
		VaultNote{Key: "maps/project-map", Tokens: 320},
	)
	notes = append(notes, sessionNotes(48, 280)...)

	return Scenario{
		ID:          "D",
		Name:        "cross-file impact analysis",
		Complexity:  contextopt.ComplexityComplex,
		Phase:       contextopt.PhaseFix,
		Description: "A rename crosses the Go backend, MCP registry and React dashboard. Impacted symbols and tests matter more than whole files.",
		Blocks:      blocks,
		VaultNotes:  notes,
		ToolOutput:  buildFailureOutput,
		Turns:       6,
	}
}

// E — long session with repeated retrieval -----------------------------------

func longAgenticScenario() Scenario {
	blocks := []Block{
		block(contextopt.KindConstraint, "constraints/no-regressions", 180, 0, 0.95),
		block(contextopt.KindArchitecture, "architecture/daemon-and-mcps", 560, 0, 0.85),
		block(contextopt.KindSymbol, "internal/server/handlers_optimizer.go#handlePlan", 840, 6200, 0.9),
		block(contextopt.KindSymbol, "internal/housekeeper/housekeeper.go#Run", 720, 5100, 0.85),
		block(contextopt.KindSymbol, "internal/telemetry/telemetry.go#RecordRequest", 480, 4300, 0.8),
		block(contextopt.KindSymbol, "internal/brain/compiler.go#Compile", 640, 4800, 0.8),
		block(contextopt.KindTest, "internal/server/server_test.go", 900, 2600, 0.7),
		block(contextopt.KindTest, "internal/housekeeper/housekeeper_test.go", 620, 1800, 0.7),
		block(contextopt.KindModuleSummary, "internal/server", 400, 0, 0.75),
		block(contextopt.KindDependency, "internal/server -> internal/optimizer", 220, 0, 0.7),
		block(contextopt.KindDecision, "decisions/state-lives-in-daemon", 300, 0, 0.8),
	}
	blocks = append(blocks, noiseBlocks("internal", 40, 540, 3500)...)

	notes := canonicalNotes(
		VaultNote{Key: "architecture/v5-overview", Tokens: 520},
		VaultNote{Key: "decisions/state-lives-in-daemon", Tokens: 340},
		VaultNote{Key: "decisions/otlp-without-sdk", Tokens: 300},
		VaultNote{Key: "instructions/obsidian-law", Tokens: 420},
		VaultNote{Key: "maps/project-map", Tokens: 320},
	)
	notes = append(notes, sessionNotes(72, 300)...)

	return Scenario{
		ID:          "E",
		Name:        "long session with repeated retrieval",
		Complexity:  contextopt.ComplexityMedium,
		Phase:       contextopt.PhaseToolLoop,
		Description: "Fourteen turns of edit, build, fail, fix. This is where resending history dominates the bill.",
		Blocks:      blocks,
		VaultNotes:  notes,
		ToolOutput:  goTestFailureOutput,
		Turns:       14,
	}
}

// F — large successful test output -------------------------------------------

func passingTestOutputScenario() Scenario {
	s := mediumGoScenario()
	s.ID = "F"
	s.Name = "large passing test output"
	s.Description = "A successful test run still produces dependency chatter and result lines. It must preserve the pass verdict without pretending there was an error."
	s.ToolOutput = goTestPassOutput
	s.Turns = 3
	return s
}

// G — large JSON payload ------------------------------------------------------

func largeJSONScenario() Scenario {
	s := complexRefactorScenario()
	s.ID = "G"
	s.Name = "large JSON tool payload"
	s.Description = "A deterministic indexing response has many records but only a small operational summary is useful in the next turn."
	s.ToolOutput = largeJSONOutput
	s.Turns = 3
	return s
}

// H — large log ---------------------------------------------------------------

func largeLogScenario() Scenario {
	s := mediumGoScenario()
	s.ID = "H"
	s.Name = "large service log"
	s.Description = "Repeated server log lines must retain the fatal root cause and raw reference while suppressing redundant progress chatter."
	s.ToolOutput = largeLogOutput
	s.Turns = 3
	return s
}

// I — historical Obsidian recall ---------------------------------------------

func historicalRecallScenario() Scenario {
	blocks := []Block{
		block(contextopt.KindDecision, "decisions/migration-ownership", 360, 0, 0.95),
		block(contextopt.KindArchitecture, "architecture/data-lifecycle", 520, 0, 0.9),
		block(contextopt.KindSymbol, "internal/brain/canonical.go#Upsert", 620, 4200, 0.85),
		block(contextopt.KindSymbol, "internal/brain/search.go#Search", 580, 3900, 0.8),
		block(contextopt.KindTest, "internal/brain/canonical_test.go", 520, 1700, 0.7),
	}
	blocks = append(blocks, noiseBlocks("internal/legacy", 35, 460, 3000)...)
	notes := canonicalNotes(
		VaultNote{Key: "decisions/vault-migration", Tokens: 480},
		VaultNote{Key: "architecture/lifecycle", Tokens: 430},
		VaultNote{Key: "project/constraints", Tokens: 300},
		VaultNote{Key: "lessons/atomic-writes", Tokens: 260},
	)
	notes = append(notes, sessionNotes(90, 320)...)
	return Scenario{
		ID:                     "I",
		Name:                   "historical Obsidian recall",
		Complexity:             contextopt.ComplexityMedium,
		Phase:                  contextopt.PhaseRetrieve,
		Description:            "A decision depends on durable historical memory amid many session snapshots, raw logs and stale notes.",
		Blocks:                 blocks,
		VaultNotes:             notes,
		Turns:                  3,
		CompressionExpectation: CompressionNotNeeded,
	}
}

// J — mandatory negative compression case -----------------------------------

func compressionNegativeScenario() Scenario {
	s := trivialScenario()
	s.ID = "J"
	s.Name = "compression worsens payload"
	s.Description = "An already-minimal test result must pass through unchanged because a compact envelope and recovery handle would cost more."
	s.ToolOutput = compressionNegativeOutput
	s.CompressionExpectation = CompressionExpectedPassthrough
	return s
}

// K — explicit no-compression case -------------------------------------------

func noCompressionScenario() Scenario {
	s := trivialScenario()
	s.ID = "K"
	s.Name = "no compression needed"
	s.Description = "The task does not produce a tool payload. The benchmark records a known zero compression overhead rather than inventing one."
	s.ToolOutput = ""
	s.CompressionExpectation = CompressionNotNeeded
	return s
}
