package kiropow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type PowerStatus struct {
	Installed        bool            `json:"installed"`
	PowerDir         string          `json:"power_dir"`
	KiroLink         string          `json:"kiro_link"`
	ActivationStatus string          `json:"activation_status"`
	ActivationHint   string          `json:"activation_hint,omitempty"`
	MCPs             map[string]bool `json:"mcps"`
	UpdatedAt        string          `json:"updated_at"`
	Errors           []string        `json:"errors,omitempty"`
}

func EnsurePower(dwytHome, dwytBin, projectPath string) (*PowerStatus, error) {
	powerDir := filepath.Join(dwytHome, "powers", "dwyt-power")
	status := &PowerStatus{
		PowerDir:         powerDir,
		KiroLink:         kiroLinkPath(),
		ActivationStatus: "created",
		MCPs:             ValidateMCPBinaries(dwytBin),
		UpdatedAt:        time.Now().UTC().Format(time.RFC3339),
	}

	if err := os.MkdirAll(filepath.Join(powerDir, "steering"), 0755); err != nil {
		return status, err
	}
	if _, err := writeIfChanged(filepath.Join(powerDir, "POWER.md"), GeneratePowerMD(dwytBin, projectPath, status.MCPs)); err != nil {
		return status, err
	}
	mcpJSON, err := GenerateMCPJSON(dwytBin, status.MCPs)
	if err != nil {
		status.Errors = append(status.Errors, err.Error())
	} else if _, err := writeIfChanged(filepath.Join(powerDir, "mcp.json"), mcpJSON); err != nil {
		return status, err
	}
	if err := GenerateSteeringFiles(powerDir, projectPath); err != nil {
		return status, err
	}
	if err := RegisterWithKiro(powerDir); err != nil {
		status.Errors = append(status.Errors, err.Error())
		status.ActivationStatus = "manual_activation_required"
		status.ActivationHint = "Add power from local path: " + powerDir
		return status, nil
	}

	status.Installed = true
	status.ActivationStatus = "linked"
	return status, nil
}

func Status(dwytHome, dwytBin string) *PowerStatus {
	powerDir := filepath.Join(dwytHome, "powers", "dwyt-power")
	st := &PowerStatus{
		Installed:        fileExists(filepath.Join(powerDir, "POWER.md")) && fileExists(filepath.Join(powerDir, "mcp.json")),
		PowerDir:         powerDir,
		KiroLink:         kiroLinkPath(),
		ActivationStatus: "missing",
		MCPs:             ValidateMCPBinaries(dwytBin),
		UpdatedAt:        "",
	}
	if info, err := os.Stat(filepath.Join(powerDir, "POWER.md")); err == nil {
		st.UpdatedAt = info.ModTime().UTC().Format(time.RFC3339)
		st.ActivationStatus = "created"
	}
	if st.Installed && NeedsUpdate(powerDir, dwytBin) {
		st.Errors = append(st.Errors, "needs_update")
		st.ActivationStatus = "needs_update"
	}
	if target, err := os.Readlink(st.KiroLink); err != nil || target != powerDir {
		st.Installed = false
		if err != nil {
			st.Errors = append(st.Errors, err.Error())
		} else {
			st.Errors = append(st.Errors, "kiro symlink points to "+target)
		}
		if st.ActivationStatus != "missing" {
			st.ActivationStatus = "manual_activation_required"
			st.ActivationHint = "Add power from local path: " + powerDir
		}
	} else if st.Installed {
		st.ActivationStatus = "linked"
	}
	return st
}

func IsKiroEnabled(setupConfig map[string]interface{}) bool {
	for _, key := range []string{"ias", "clients"} {
		if values, ok := setupConfig[key].([]interface{}); ok {
			for _, value := range values {
				if s, ok := value.(string); ok && s == "kiro" {
					return true
				}
			}
		}
		if values, ok := setupConfig[key].([]string); ok {
			for _, value := range values {
				if value == "kiro" {
					return true
				}
			}
		}
	}
	return false
}

func ValidateMCPBinaries(dwytBin string) map[string]bool {
	// Both DWYT-owned MCPs — the Governor (`dwyt governor-mcp`) and the Brain
	// (`dwyt obsidian-mcp`) — are served by the main `dwyt` binary, so its
	// presence is the canonical signal for both. A legacy
	// `dwyt-obsidian-mcp` copy (left over from older installs) is also
	// accepted so the Kiro Power keeps listing Obsidian during the migration
	// window.
	dwyt := executableName("dwyt")
	obsidianLegacy := executableName("dwyt-obsidian-mcp")
	mainPresent := fileExists(filepath.Join(dwytBin, dwyt))
	return map[string]bool{
		"dwyt":     mainPresent,
		"codebase": fileExists(filepath.Join(dwytBin, executableName("codebase-memory-mcp"))),
		"obsidian": mainPresent || fileExists(filepath.Join(dwytBin, obsidianLegacy)),
	}
}

// GeneratePowerMD renders the Kiro Power description.
//
// Like the instruction files, this is the v5 *entry contract* and not the
// policy itself (spec §4, §60.3). Shipping the full policy here as well as in
// the DWYT MCP would duplicate thousands of tokens per request and reduce cache
// reuse — the exact failure the spec calls out. The Power points Kiro at the
// Governor; the Governor supplies the rules on demand.
func GeneratePowerMD(dwytBin, projectPath string, mcps map[string]bool) string {
	return fmt.Sprintf(`---
name: dwyt-power
displayName: DWYT Project Context
description: DWYT context governor plus Obsidian project memory and the Codebase graph, with RTK terminal compression.
keywords:
  - dwyt
  - context governor
  - codebase
  - obsidian
  - mcp
  - memory
  - project memory
  - token savings
  - repo analysis
  - arquitetura
  - refatoracao
  - debugging
  - documentacao
  - contexto do projeto
author: DWYT
---

# DWYT Project Context

DWYT (Don't Waste Your Tokens) governs how much context a task loads, so the
model receives the minimum sufficient context instead of the maximum available.

## Three MCPs

- **dwyt** — the Context Governor: budgets, retrieval boundaries, output
  profiles, cache guidance, tool-output compaction and raw retrieval.
- **obsidian** — the Brain: canonical project knowledge and compact sessions.
- **codebase** — Code Intelligence: symbols, references, dependencies, ranges.

RTK compresses terminal output. It is a CLI tool, not a fourth MCP.

## Entry Contract

Before broad repository or memory retrieval, call `+"`dwyt_context_plan`"+` and
stay inside the returned budget and retrieval level. Prefer canonical memory
over old sessions, symbols and ranges over full files, summaries over raw
output, and reuse over re-retrieval. Compact large tool output with
`+"`dwyt_compact_tool_output`"+` and pull the full bytes back only with
`+"`dwyt_get_raw`"+` when the summary is insufficient.

At the end of a task, persist a compact snapshot with
`+"`obsidian_save_context`"+` (client: kiro) when the task state changed.

Keep operational answers to status, changed files, validation and blockers.
Never truncate an artifact the user asked for.

## Project

Path: %s
DWYT bin: %s

## MCP Availability

- dwyt: %t
- codebase: %t
- obsidian: %t
`, projectPath, dwytBin, mcps["dwyt"], mcps["codebase"], mcps["obsidian"])
}

func GenerateMCPJSON(dwytBin string, mcps map[string]bool) (string, error) {
	servers := map[string]interface{}{}
	if mcps["dwyt"] {
		servers["dwyt"] = map[string]interface{}{
			"command": filepath.Join(dwytBin, executableName("dwyt")),
			"args":    []string{"governor-mcp"},
			"env":     map[string]string{"DWYT_API_URL": "http://localhost:2737/api"},
		}
	}
	if mcps["codebase"] {
		servers["codebase"] = map[string]interface{}{
			"command": filepath.Join(dwytBin, executableName("codebase-memory-mcp")),
			"args":    []string{"--ui=true", "--port=9749"},
			"env":     map[string]string{"CBM_CACHE_DIR": filepath.Join(dwytBin, "..", "codebase")},
		}
	}
	if mcps["obsidian"] {
		servers["obsidian"] = map[string]interface{}{
			"command": filepath.Join(dwytBin, executableName("dwyt")),
			"args":    []string{"obsidian-mcp"},
			"env":     map[string]string{"DWYT_API_URL": "http://localhost:2737/api"},
		}
	}
	data, err := json.MarshalIndent(map[string]interface{}{"mcpServers": servers}, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}

func GenerateSteeringFiles(powerDir, projectPath string) error {
	files := map[string]string{
		"dwyt-context.md": steeringContext(),
		"obsidian.md":     steeringObsidian(projectPath),
		"codebase.md":     steeringCodebase(),
		"rtk.md":          steeringRTK(),
		"headroom.md":     steeringHeadroom(),
	}
	for name, content := range files {
		if _, err := writeIfChanged(filepath.Join(powerDir, "steering", name), content); err != nil {
			return err
		}
	}
	return nil
}

func RegisterWithKiro(powerDir string) error {
	link := kiroLinkPath()
	if existing, err := os.Readlink(link); err == nil {
		if existing == powerDir {
			return nil
		}
		if err := os.Remove(link); err != nil {
			return err
		}
	} else if info, statErr := os.Stat(link); statErr == nil && info.IsDir() {
		return fmt.Errorf("kiro power link path is a directory: %s", link)
	}
	if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
		return err
	}
	return os.Symlink(powerDir, link)
}

func NeedsUpdate(powerDir, dwytBin string) bool {
	for _, rel := range []string{"POWER.md", "mcp.json", filepath.Join("steering", "dwyt-context.md"), filepath.Join("steering", "obsidian.md"), filepath.Join("steering", "codebase.md"), filepath.Join("steering", "rtk.md"), filepath.Join("steering", "headroom.md")} {
		if !fileExists(filepath.Join(powerDir, rel)) {
			return true
		}
	}
	mcps := ValidateMCPBinaries(dwytBin)
	data, err := os.ReadFile(filepath.Join(powerDir, "mcp.json"))
	if err != nil {
		return true
	}
	text := string(data)
	// The canonical Obsidian MCP command is `dwyt obsidian-mcp` (one shared
	// binary, two subcommand args). An older `dwyt-obsidian-mcp` reference is
	// still accepted while a migration is in flight, so we update when both
	// are missing.
	return (mcps["codebase"] && !strings.Contains(text, executableName("codebase-memory-mcp"))) ||
		(mcps["obsidian"] && !containsObsidianMCPCommand(text)) ||
		// A Power generated before v5 has no Governor entry; regenerate so the
		// three official MCPs are all present.
		(mcps["dwyt"] && !strings.Contains(text, "\"governor-mcp\""))
}

// containsObsidianMCPCommand reports whether the generated Kiro Power
// mcp.json still uses the canonical (`dwyt` + `obsidian-mcp` subcommand) or
// the legacy `dwyt-obsidian-mcp` filename to launch the Obsidian MCP.
func containsObsidianMCPCommand(text string) bool {
	if strings.Contains(text, "\"obsidian-mcp\"") && strings.Contains(text, executableName("dwyt")) {
		return true
	}
	return strings.Contains(text, executableName("dwyt-obsidian-mcp"))
}

func writeIfChanged(path, content string) (bool, error) {
	if existing, err := os.ReadFile(path); err == nil {
		if contentHash(string(existing)) == contentHash(content) {
			return false, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, []byte(content), 0644)
}

func contentHash(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:8])
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func executableName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func kiroLinkPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".kiro", "powers", "dwyt-power")
}

// steeringContext is the always-included steering file. It must stay small and
// stable for the same reason the instruction contract does: it is injected into
// every Kiro turn, so any growth here is multiplied across every request.
func steeringContext() string {
	return `---
inclusion: always
---

# DWYT Context Rules

Three MCPs: **dwyt** (context governor), **obsidian** (project memory),
**codebase** (code structure). RTK compresses terminal output and is not an MCP.

## Order of Operations

1. Call ` + "`dwyt_context_plan`" + ` before broad repository or memory retrieval.
   Stay inside the returned budget, retrieval level and exclusions.
2. Load canonical memory from Obsidian before old session notes.
3. Retrieve code from Codebase progressively: project map → module → symbol →
   range. A full file is exceptional and needs a reason.
4. Compact large tool output with ` + "`dwyt_compact_tool_output`" + `; resolve the
   full bytes with ` + "`dwyt_get_raw`" + ` only when the summary is insufficient.
5. Prefix shell commands with ` + "`rtk`" + `.

## Stop Rules

Stop retrieving as soon as the context is sufficient to act safely. Reuse
context already obtained instead of fetching it again. Prefer diffs over full
state.

## Completion

When the task state changed, save a compact snapshot with
` + "`obsidian_save_context`" + ` (client: kiro). Report status, changed files,
validation and blockers. Do not narrate routine reasoning. Do not truncate an
artifact the user asked for.
`
}

// steeringObsidian documents the Brain. It is manual-inclusion in v5: the
// always-included context file already states the workflow, so loading this
// detail on every turn would be duplicated context.
func steeringObsidian(projectPath string) string {
	return fmt.Sprintf(`---
inclusion: manual
---

# Obsidian - Project Brain

Project path: %s
Vault root: ~/.dwyt/projects/<id>_<project-name>/  (e.g. 1597b5fc9bfb_dwyt)

The Brain stores *meaning*: project identity, architecture, decisions,
conventions, constraints, module summaries, reusable knowledge and compact
sessions. Raw evidence lives in the raw object store, not here.

## Tools
- `+"`obsidian_search`"+` — top-k search; raw and stale notes are excluded by default.
- `+"`obsidian_summarize`"+` — rebuild and read the vault summary.
- `+"`obsidian_save`"+` — save a decision, task or knowledge note.
- `+"`obsidian_save_context`"+` — persist a compact task snapshot.
- `+"`dwyt_memory_health`"+` — note counts per area and vault size.

## Rules
- Prefer canonical memory (project, architecture, decisions, conventions,
  constraints) over historical session notes.
- Update canonical knowledge instead of appending a second copy of a changed
  fact.
- Save a snapshot only when the task state changed.
- Never delete vault files, projects or history as a repair step.
`, projectPath)
}

func steeringCodebase() string {
	return `---
inclusion: manual
---

# Codebase - Code Knowledge Graph

The Codebase MCP is the source of truth for symbols, calls, dependencies,
relationships and impact. Retrieve progressively; never start by reading the
repository.

	project map → module map → file summary → symbol → range → dependencies/tests

A full file is exceptional: justify why symbol or range is insufficient.

## MCP Tools
- search_graph — symbols, components, handlers, routes, modules
- trace_path — callers, dependencies, flows, blast radius
- get_code_snippet — the exact range before applying an edit

## Rules
- Validate whether the project is indexed before structural analysis.
- Prefer search_graph over grep/glob as the first strategy.
- Do not remove, rename or move code without tracing impact.
- Report retrieved blocks to ` + "`dwyt_register_context`" + ` so unchanged symbols
  are reused instead of re-retrieved on the next turn.

## API
- Start: POST http://localhost:2737/api/services/codebase/start
- Index: POST http://localhost:2737/api/codebase/index
- Status: GET http://localhost:2737/api/services/codebase/status
`
}

func steeringRTK() string {
	return `---
inclusion: manual
---

# RTK - Terminal Compression

RTK is a CLI tool, not an MCP. Prefix shell commands with ` + "`rtk`" + `; in a
command chain, prefix each segment.

## Usage
rtk git status
rtk go test ./...
rtk npm run build

When RTK has already reduced the output, pass ` + "`already_compact: true`" + ` to
` + "`dwyt_compact_tool_output`" + ` so DWYT does not compress it twice.

## Metrics
GET http://localhost:2737/api/rtk/gain
`
}

func steeringHeadroom() string {
	return `---
inclusion: manual
---

# Headroom - API Proxy

Headroom is an auxiliary integration, not a source of DWYT policy.

## Detection
Active when OPENAI_BASE_URL or ANTHROPIC_BASE_URL point to the local proxy.
Never route Codex through Headroom when Codex is authenticated through
ChatGPT/OAuth. If Headroom is inactive, use the standard endpoints.

## Status
GET http://localhost:2737/api/services/headroom/status
`
}
