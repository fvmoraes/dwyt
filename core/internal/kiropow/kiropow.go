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
	codebase := "codebase-memory-mcp"
	obsidian := "dwyt-obsidian-mcp"
	if runtime.GOOS == "windows" {
		codebase += ".exe"
		obsidian += ".exe"
	}
	return map[string]bool{
		"codebase": fileExists(filepath.Join(dwytBin, codebase)),
		"obsidian": fileExists(filepath.Join(dwytBin, obsidian)),
	}
}

func GeneratePowerMD(dwytBin, projectPath string, mcps map[string]bool) string {
	return fmt.Sprintf(`---
name: dwyt-power
displayName: DWYT Project Context
description: DWYT integration for Codebase MCP, Obsidian memory, RTK command compression and compatible Headroom usage.
keywords:
  - dwyt
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

DWYT (Don't Waste Your Tokens) is a local orchestrator that reduces AI token consumption by combining RTK terminal compression, the Codebase graph, Obsidian project memory, and compatible Headroom API proxy usage.

## Priority Order

1. RTK - prefix shell commands with `+"`rtk`"+` when terminal output enters the conversation.
2. Codebase MCP - before technical diagnosis, refactors, or edits that depend on real code structure, validate/index the project and use `+"`search_graph`"+`, `+"`trace_path`"+`, and `+"`get_code_snippet`"+`.
3. Obsidian MCP - before relevant work, search/summarize the project vault; during work save decisions/tasks; at the end save complete context.
4. Headroom - use only when the client supports proxy/base-url configuration. Never route Codex through Headroom when Codex is authenticated through ChatGPT/OAuth.

## Codebase Law

When you need to understand, validate, diagnose, or alter code structure, use the Codebase MCP as the primary source of truth. Do not rely on file names or memory alone. Prefer graph tools over manual grep/glob for symbols, relationships, dependencies, calls, and impact analysis.

## Obsidian Law

The Obsidian vault is the official durable memory for this project. Save notes with internal links such as `+"`[[decisions]]`"+`, `+"`[[tasks]]`"+`, `+"`[[instructions/obsidian-law]]`"+`, and `+"`[[instructions/codebase-law]]`"+`.

Required completion payload for `+"`POST http://localhost:2737/api/obsidian/context`"+`: user request, summary, files, decisions, actions, commands, errors, outcome, next steps, and context for future agents.

## Tools

- RTK: `+"`rtk git status`"+`, `+"`rtk go test ./...`"+`, `+"`rtk npm run build`"+`
- Codebase MCP: `+"`search_graph`"+`, `+"`trace_path`"+`, `+"`get_code_snippet`"+`
- Obsidian MCP: `+"`/api/obsidian/search`"+`, `+"`/api/obsidian/summarize`"+`, `+"`/api/obsidian/save`"+`, `+"`/api/obsidian/context`"+`
- Headroom: active only when compatible env vars point to the local proxy.

## Project

Path: %s
DWYT bin: %s

## MCP Availability

- codebase: %t
- obsidian: %t

## Completion

Never finish a task without saving complete context to Obsidian with the user request, summary, files changed/read, decisions, actions, commands, errors, outcome, next steps, and context for future agents.
`, projectPath, dwytBin, mcps["codebase"], mcps["obsidian"])
}

func GenerateMCPJSON(dwytBin string, mcps map[string]bool) (string, error) {
	servers := map[string]interface{}{}
	if mcps["codebase"] {
		servers["codebase"] = map[string]interface{}{
			"command": filepath.Join(dwytBin, executableName("codebase-memory-mcp")),
			"args":    []string{"--ui=true", "--port=9749"},
			"env":     map[string]string{"CBM_CACHE_DIR": filepath.Join(dwytBin, "..", "codebase")},
		}
	}
	if mcps["obsidian"] {
		servers["obsidian"] = map[string]interface{}{
			"command": filepath.Join(dwytBin, executableName("dwyt-obsidian-mcp")),
			"args":    []string{},
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
	return (mcps["codebase"] && !strings.Contains(text, executableName("codebase-memory-mcp"))) ||
		(mcps["obsidian"] && !strings.Contains(text, executableName("dwyt-obsidian-mcp")))
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

func steeringContext() string {
	return `---
inclusion: always
---

# DWYT Context Rules

## Priority Order

1. RTK - prefix shell commands with rtk whenever terminal output enters context.
2. Codebase MCP - use the graph before diagnosing, refactoring, or editing code structure.
3. Obsidian MCP - search/summarize memory before relevant work and save context through the task.
4. Headroom - use only when compatible env vars point to the local proxy.

## Codebase Law

When code structure matters, validate/index the project and use search_graph, trace_path, and get_code_snippet before proposing or applying changes.

## Obsidian Law

Before relevant work:
GET http://localhost:2737/api/obsidian/search?q=<your query>
POST http://localhost:2737/api/obsidian/summarize

During work, save decisions and task/status updates to Obsidian:
POST http://localhost:2737/api/obsidian/save
{"type":"decision","content":"..."}
{"type":"task","content":"..."}

At the end of every task, save the conversation context:
POST http://localhost:2737/api/obsidian/context
{"client":"kiro","user_request":"...","summary":"...","files":["..."],"decisions":["..."],"actions":["..."],"commands":["..."],"errors":["..."],"outcome":"...","next_steps":["..."],"context":"..."}

Keep the vault rich and navigable with folders, internal links, templates, and instructions.
`
}

func steeringObsidian(projectPath string) string {
	return fmt.Sprintf(`---
inclusion: always
---

# Obsidian - Project Memory

Project path: %s
Vault root: ~/.dwyt/projects/<id>/obsidian/

## API
- Search: GET http://localhost:2737/api/obsidian/search?q=<query>
- Summarize: POST http://localhost:2737/api/obsidian/summarize
- Save:   POST http://localhost:2737/api/obsidian/save
- Context: POST http://localhost:2737/api/obsidian/context
- Status: GET http://localhost:2737/api/obsidian/status

## Rules
- Search and summarize Obsidian before relevant work.
- Save important decisions as type `+"`decision`"+` during work.
- Save task/status updates as type `+"`task`"+` during work.
- Always save complete conversation context at the end of each task.
- Include user request, summary, files, decisions, actions, commands, errors, outcome, next steps, and future-agent context.
- Keep the vault rich, interlinked, and organized with folders, links such as [[index]], [[instructions/obsidian-law]], and [[instructions/codebase-law]], templates, and instructions.
- Never delete vault files.
`, projectPath)
}

func steeringCodebase() string {
	return `---
inclusion: manual
---

# Codebase - Code Knowledge Graph

Use Codebase MCP whenever you need to understand, validate, diagnose, or alter real code structure. It is the source of truth for symbols, calls, dependencies, relationships, and impact.

## MCP Tools
- search_graph
- trace_path
- get_code_snippet

## Rules
- Validate whether the project is indexed before structural analysis.
- Prefer search_graph over grep/glob for symbols, components, handlers, routes, and modules.
- Use trace_path for callers, dependencies, flows, and blast-radius checks.
- Use get_code_snippet before applying code edits.
- Do not remove, rename, or move important code without tracing impact.

## API
- Start: POST http://localhost:2737/api/services/codebase/start
- Index: POST http://localhost:2737/api/codebase/index
- Status: GET http://localhost:2737/api/services/codebase/status
`
}

func steeringRTK() string {
	return `---
inclusion: always
---

# RTK - Terminal Compression

RTK is a CLI tool. Prefix all shell commands with rtk.

## Usage
rtk git status
rtk go test ./...
rtk npm run build

## Metrics
GET http://localhost:2737/api/rtk/gain
`
}

func steeringHeadroom() string {
	return `---
inclusion: always
---

# Headroom - API Proxy

Headroom compresses AI API calls automatically.

## Detection
If OPENAI_BASE_URL or ANTHROPIC_BASE_URL point to 127.0.0.1:8787, Headroom is active.

Do not route Codex through Headroom when Codex is authenticated through ChatGPT/OAuth.

## Status
GET http://localhost:2737/api/services/headroom/status
`
}
