package benchmark

import (
	"fmt"
	"strings"

	"github.com/fvmoraes/dwyt/internal/contextopt"
)

// The five spec §69 scenarios, as deterministic fixtures.
//
// The fixtures are hand-shaped rather than sampled from a live repository on
// purpose: a benchmark whose input changes between runs cannot detect an
// optimizer regression. The shapes, though, are taken from what DWYT actually
// sees — a repository offers far more context than a task needs, most vault
// notes are session history rather than knowledge, and build output is mostly
// lines that carry no information.

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

// --- Scenario A: trivial ------------------------------------------------------

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
		ID:          "A",
		Name:        "typo in README/config",
		Complexity:  contextopt.ComplexityTrivial,
		Phase:       contextopt.PhaseFix,
		Description: "One-line documentation fix. Needs almost no context, and the whole point is not to load any.",
		Blocks:      blocks,
		VaultNotes:  notes,
		Turns:       1,
	}
}

// --- Scenario B: small frontend bug ------------------------------------------

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
		Name:        "frontend bug in 1-2 symbols",
		Complexity:  contextopt.ComplexitySimple,
		Phase:       contextopt.PhaseFix,
		Description: "A component renders a stale value. Two symbols matter; the rest of the component tree does not.",
		Blocks:      blocks,
		VaultNotes:  notes,
		ToolOutput:  tscOutput(),
		Turns:       2,
	}
}

// --- Scenario C: medium Go bug + tests ---------------------------------------

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
		Name:        "Go bug + tests, 2-4 files",
		Complexity:  contextopt.ComplexityMedium,
		Phase:       contextopt.PhaseFix,
		Description: "A search returns stale notes. The fix touches three files and has to keep two test files green.",
		Blocks:      blocks,
		VaultNotes:  notes,
		ToolOutput:  goTestOutput(),
		Turns:       4,
	}
}

// --- Scenario D: complex cross-module refactor -------------------------------

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
		Name:        "cross-module Go + TS refactor",
		Complexity:  contextopt.ComplexityComplex,
		Phase:       contextopt.PhaseFix,
		Description: "A rename that crosses the Go backend, the MCP registry and the React dashboard, with tests on both sides.",
		Blocks:      blocks,
		VaultNotes:  notes,
		ToolOutput:  buildOutput(),
		Turns:       6,
	}
}

// --- Scenario E: long agentic session ----------------------------------------

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
		Name:        "long agentic session",
		Complexity:  contextopt.ComplexityMedium,
		Phase:       contextopt.PhaseToolLoop,
		Description: "Fourteen turns of edit, build, fail, fix. This is where resending history dominates the bill.",
		Blocks:      blocks,
		VaultNotes:  notes,
		ToolOutput:  goTestOutput(),
		Turns:       14,
	}
}

// --- Tool output fixtures -----------------------------------------------------
//
// Real build and test output, in shape: a handful of lines that matter buried in
// progress meters, dependency chatter and the same diagnostic repeated once per
// call site.

func tscOutput() string {
	var b strings.Builder
	b.WriteString("> dwyt-web@0.0.0 build\n> tsc -b && vite build\n")
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "transforming (%d) node_modules/react/index.js\n", i)
	}
	for i := 0; i < 12; i++ {
		fmt.Fprintf(&b, "src/components/CardOptimizer.tsx(%d,17): error TS2339: Property 'ratio' does not exist on type 'OptimizerState'.\n", 40+i)
	}
	b.WriteString("src/api.ts(88,3): error TS2739: Type 'Partial<OptimizerState>' is missing properties.\n")
	b.WriteString("src/hooks/useOptimizer.ts(21,9): warning TS6133: 'prev' is declared but its value is never read.\n")
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&b, "[=========>          ] building %d%%\n", i*3)
	}
	b.WriteString("Found 13 errors.\n")
	return b.String()
}

func goTestOutput() string {
	var b strings.Builder
	for i := 0; i < 25; i++ {
		fmt.Fprintf(&b, "go: downloading github.com/example/mod%d v1.2.%d\n", i, i)
	}
	for i := 0; i < 18; i++ {
		fmt.Fprintf(&b, "ok  \tgithub.com/fvmoraes/dwyt/internal/pkg%02d\t0.0%ds\n", i, i%9)
	}
	b.WriteString("--- FAIL: TestSearchV2ExcludesStaleNotes (0.00s)\n")
	b.WriteString("    search_test.go:51: expected the default top-k of 5, got 9\n")
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&b, "    search_test.go:%d: note %d was not excluded\n", 60+i, i)
	}
	b.WriteString("--- FAIL: TestTemperatureClassifiesResolvedAsCold (0.00s)\n")
	b.WriteString("    temperature_test.go:33: got warm, want cold\n")
	b.WriteString("FAIL\tgithub.com/fvmoraes/dwyt/internal/brain\t0.014s\n")
	b.WriteString("FAIL\n")
	return b.String()
}

func buildOutput() string {
	var b strings.Builder
	for i := 0; i < 35; i++ {
		fmt.Fprintf(&b, "go: downloading github.com/example/dep%d v0.%d.0\n", i, i)
	}
	for i := 0; i < 15; i++ {
		fmt.Fprintf(&b, "internal/optimizer/optimizer.go:%d:9: undefined: contextgov.Rank\n", 120+i)
	}
	b.WriteString("internal/mcpregistry/registry.go:44:2: declared and not used: legacyName\n")
	b.WriteString("web/src/api.ts(12,10): error TS2305: Module './types' has no exported member 'GovernorState'.\n")
	for i := 0; i < 25; i++ {
		fmt.Fprintf(&b, "[####################] linking %d%%\n", i*4)
	}
	b.WriteString("build failed\n")
	return b.String()
}
