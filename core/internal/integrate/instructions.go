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

func writeOrUpdateInstructionFile(path, content string) {
	managedBlock, fullBlock := dwytInstructionBlocks(content)
	data, err := os.ReadFile(path)
	if err != nil {
		os.MkdirAll(filepath.Dir(path), 0755)
		os.WriteFile(path, []byte(fullBlock), 0644)
		return
	}

	current := string(data)
	if hasNoManagedInstructionBlock(current) && strings.TrimSpace(current) == strings.TrimSpace(content) {
		current = ""
	}
	next := upsertManagedBlock(current, managedBlock, fullBlock)
	if next == current {
		return
	}
	os.WriteFile(path, []byte(next), 0644)
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

// dwytInstructions returns the DWYT entry contract that is injected into
// client instruction files (AGENTS.md, CLAUDE.md, Cursor rules, Kiro steering,
// Copilot, Windsurf).
//
// From v5.0.0 this is deliberately *small and stable* (spec §4). The detailed
// efficiency policy — budgets, TTLs, retrieval ladders, cache classes, output
// profiles — lives inside the DWYT MCP Governor and is fetched on demand, not
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
	return `# DWYT - Don't Waste Your Tokens

This project uses DWYT for context and token optimization.

## Available MCPs

- **DWYT MCP** — context governor and efficiency policy.
- **Obsidian MCP** — persistent project memory and canonical knowledge.
- **Codebase MCP** — structural code retrieval.

## Entry Contract

Before broad repository or memory retrieval, call the DWYT MCP
(` + "`dwyt_context_plan`" + `) to obtain a context plan, then stay inside its
budget and retrieval boundaries.

Prefer:

- canonical memory over old sessions;
- symbols and line ranges over full files;
- summaries over raw output;
- incremental retrieval over bulk context loading;
- reusing context already obtained over retrieving it again.

Use Obsidian as the project brain and Codebase as the source of current code
structure. Request raw or full data only when the compact context is
insufficient; raw output stays retrievable by reference
(` + "`dwyt_get_raw`" + `).

Prefix shell commands with ` + "`rtk`" + ` where supported
(` + "`rtk go test ./...`" + `). RTK reduces terminal output; it is not an MCP.

At the end of a task, persist a compact context snapshot with
` + "`obsidian_save_context`" + ` when the task state changed. Set ` + "`client`" + `
to the current client (codex, opencode, claude, cursor, kiro, copilot,
windsurf, continue). If saving fails, say so in the final response.

Keep operational answers short: status, changed files, validation, blockers.
Do not truncate an artifact the user asked for.
`
}
