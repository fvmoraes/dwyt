package integrate

import (
	"os"
	"path/filepath"
	"strings"
)

const instructionMarkerStart = "<!-- DWYT:START -->"
const instructionMarkerEnd = "<!-- DWYT:END -->"

const legacyInstructionMarkerStart = "<!-- dwyt:instructions:start -->"
const legacyInstructionMarkerEnd = "<!-- dwyt:instructions:end -->"

type markerPair struct {
	start string
	end   string
}

type blockSpan struct {
	start int
	end   int
}

var instructionMarkerPairs = []markerPair{
	{instructionMarkerStart, instructionMarkerEnd},
	{legacyInstructionMarkerStart, legacyInstructionMarkerEnd},
}

func writeOrUpdateInstructionFile(path, content string) error {
	managedBlock, fullBlock := dwytInstructionBlocks(content)
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		return os.WriteFile(path, []byte(fullBlock), 0644)
	}

	current := string(data)
	if hasNoManagedInstructionBlock(current) && strings.TrimSpace(current) == strings.TrimSpace(content) {
		current = ""
	}
	next := upsertManagedBlock(current, managedBlock, fullBlock)
	if next == current {
		return nil
	}
	return os.WriteFile(path, []byte(next), 0644)
}

func hasNoManagedInstructionBlock(content string) bool {
	for _, pair := range instructionMarkerPairs {
		if strings.Contains(content, pair.start) {
			return false
		}
	}
	return true
}

func dwytInstructionBlocks(content string) (managed string, full string) {
	body := strings.TrimSpace(content)
	frontmatter := ""
	if strings.HasPrefix(body, "---\n") {
		if endRel := strings.Index(body[4:], "\n---"); endRel >= 0 {
			endIdx := 4 + endRel + len("\n---")
			if endIdx < len(body) && body[endIdx] == '\n' {
				endIdx++
			}
			frontmatter = strings.TrimRight(body[:endIdx], "\n") + "\n"
			body = strings.TrimSpace(body[endIdx:])
		}
	}
	if !strings.Contains(body, "#dwyt") {
		body = "#dwyt\n\n" + body
	}
	managed = instructionMarkerStart + "\n" + body + "\n" + instructionMarkerEnd + "\n"
	return managed, frontmatter + managed
}

func upsertManagedBlock(content, managedBlock, fullBlock string) string {
	spans := managedInstructionSpans(content)
	if len(spans) > 0 {
		replacement := strings.TrimRight(managedBlock, "\n") + "\n"
		var b strings.Builder
		pos := 0
		for i, span := range spans {
			b.WriteString(content[pos:span.start])
			if i == 0 {
				b.WriteString(replacement)
			}
			pos = span.end
		}
		b.WriteString(content[pos:])
		return b.String()
	}

	if strings.TrimSpace(content) == "" {
		return fullBlock
	}
	separator := "\n\n"
	if strings.HasSuffix(content, "\n") {
		separator = "\n"
	}
	return content + separator + managedBlock
}

func managedInstructionSpans(content string) []blockSpan {
	var spans []blockSpan
	offset := 0
	for {
		bestStart, bestEnd := -1, -1
		for _, pair := range instructionMarkerPairs {
			startRel := strings.Index(content[offset:], pair.start)
			if startRel < 0 {
				continue
			}
			startIdx := offset + startRel
			endRel := strings.Index(content[startIdx:], pair.end)
			if endRel < 0 {
				continue
			}
			endIdx := startIdx + endRel + len(pair.end)
			if endIdx < len(content) && content[endIdx] == '\n' {
				endIdx++
			}
			if bestStart < 0 || startIdx < bestStart {
				bestStart, bestEnd = startIdx, endIdx
			}
		}
		if bestStart < 0 {
			return spans
		}
		spans = append(spans, blockSpan{start: bestStart, end: bestEnd})
		offset = bestEnd
	}
}

func agentsMDTemplate(rtkBin string) string {
	_ = rtkBin
	return dwytInstructions()
}

func claudeMDTemplate() string {
	return dwytInstructions()
}

func cursorRuleTemplate() string {
	return `---
description: DWYT project guidance
alwaysApply: true
---

` + dwytInstructions()
}

func kiroSteeringTemplate() string {
	return dwytInstructions()
}

func copilotMDTemplate() string {
	return dwytInstructions()
}

func windsurfRuleTemplate() string {
	return dwytInstructions()
}

func continueRuleTemplate() string {
	return dwytInstructions()
}

// dwytInstructions returns the DWYT entry contract that is injected into
// client instruction files (AGENTS.md, CLAUDE.md, Cursor rules, Kiro steering,
// Copilot, Windsurf, Continue).
//
// From v5.0.0 this is deliberately *small and stable* (spec §4). The detailed
// efficiency policy — budgets, TTLs, retrieval ladders, cache classes, output
// profiles — lives inside the DWYT Optimizer MCP and is fetched on demand, not
// pasted into every request. Two reasons:
//
//   - Duplicating the full policy here would cost thousands of tokens on every
//     single call, which is exactly the waste DWYT exists to remove.
//   - This block must change rarely. A stable block sits in the cacheable
//     prefix of a prompt; a block that changes every release does not.
//
// Any change to this text invalidates provider prefix caches for every user,
// so treat it as a versioned contract, not as a place to document features.
func dwytInstructions() string {
	return strings.Join([]string{
		"# DWYT - Don't Waste Your Tokens",
		"",
		"DWYT exposes three local MCPs. **Mandatory flow:** Optimizer → Codebase → Obsidian → targeted shell/file access.",
		"Always report concise telemetry promptly with `dwyt_report_usage` after meaningful work so the UI remains accurate; mark `observed` only for provider-reported values.",
		"Models used must always be informed and populated via `dwyt_report_usage`: model and variant, plus effort — never leave the models-used view empty when you ran requests.",
		"",
		"## 1. Optimizer first",
		"`dwyt_optimizer` — context optimizer for context planning, token budget, output profile, routing, cache guidance, output compaction and raw-data retrieval.",
		"Before broad retrieval call `dwyt_context_plan`; use `dwyt_context_status` to reuse state and `dwyt_register_context` after obtaining reusable source or memory.",
		"Use `dwyt_output_profile` for response budget, `dwyt_route` for effort, and `dwyt_cache_guidance` for cache order. Use `dwyt_compact_tool_output` for verbose output; call `dwyt_get_raw` only when its compact reference is insufficient.",
		"",
		"## 2. Codebase first",
		"`dwyt_codebase` — structural code retrieval and the **PRIMARY** repository layer for architecture, symbols, implementations, references, dependencies, execution paths and blast-radius analysis.",
		"**Codebase first. Shell discovery only as fallback.**",
		"`get_architecture` maps the repository; `search_graph` finds definitions, implementations, symbols and relationships; `query_graph` answers advanced graph relationships.",
		"`trace_path` follows callers/callees, data flow and cross-service paths; `get_code_snippet` reads one already-located implementation.",
		"Use `detect_changes` for Git blast radius; `check_index_coverage`/`index_status` for completeness; `index_repository` only when a refresh is necessary; `manage_adr` to record an architectural decision.",
		"Use `search_code` only when graph lookup is insufficient. Examples: implementation → `search_graph`; callers/dependencies → `trace_path`; one implementation → `get_code_snippet`; architecture → `get_architecture`; change impact → `detect_changes`.",
		"Do not begin with `grep`, `rg`, `find`, recursive scans, filename guesses, or opening many files manually while Codebase can answer structurally.",
		"Direct shell/file access is allowed only for exact raw text, non-indexed files, generated/config files outside the graph, incomplete coverage, or physical final verification.",
		"",
		"## 3. Obsidian after Codebase",
		"`dwyt_obsidian` — persistent project memory and canonical knowledge for architecture, decisions, conventions, constraints, lessons and reusable knowledge.",
		"Read `obsidian_canonical` first; use `obsidian_search` only when canonical knowledge is insufficient. Use `obsidian_save_context` at meaningful task end, `obsidian_upsert_canonical` for current facts, `obsidian_compile` to promote reusable knowledge, and `obsidian_summarize` for compact history.",
		"Codebase = repository now. Obsidian = what the project knows and decided. If they disagree, current source verified through Codebase wins; update canonical memory when appropriate.",
		"",
		"## Output discipline",
		"Use `rtk` for supported test/build/git commands; it reduces shell output and never replaces Codebase. Use Headroom for verbose logs when available; otherwise compact with `dwyt_compact_tool_output` and retain raw output by reference.",
		"Keep updates concise: status, files, validation, blockers. Do not truncate requested artifacts.",
	}, "\n") + "\n"
}

// InstructionBlock exposes the entry contract for audits that need the
// exact bytes clients receive (MCP startup-tax measurement). It is the same
// text dwytInstructions returns; the alias documents intent at call sites.
func InstructionBlock() string {
	return dwytInstructions()
}
