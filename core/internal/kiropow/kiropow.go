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
	Installed bool            `json:"installed"`
	PowerDir  string          `json:"power_dir"`
	KiroLink  string          `json:"kiro_link"`
	MCPs      map[string]bool `json:"mcps"`
	UpdatedAt string          `json:"updated_at"`
	Errors    []string        `json:"errors,omitempty"`
}

func EnsurePower(dwytHome, dwytBin, projectPath string) (*PowerStatus, error) {
	powerDir := filepath.Join(dwytHome, "powers", "dwyt-power")
	status := &PowerStatus{
		PowerDir:  powerDir,
		KiroLink:  kiroLinkPath(),
		MCPs:      ValidateMCPBinaries(dwytBin),
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
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
		return status, err
	}

	status.Installed = true
	return status, nil
}

func Status(dwytHome, dwytBin string) *PowerStatus {
	powerDir := filepath.Join(dwytHome, "powers", "dwyt-power")
	st := &PowerStatus{
		Installed: fileExists(filepath.Join(powerDir, "POWER.md")) && fileExists(filepath.Join(powerDir, "mcp.json")),
		PowerDir:  powerDir,
		KiroLink:  kiroLinkPath(),
		MCPs:      ValidateMCPBinaries(dwytBin),
		UpdatedAt: "",
	}
	if info, err := os.Stat(filepath.Join(powerDir, "POWER.md")); err == nil {
		st.UpdatedAt = info.ModTime().UTC().Format(time.RFC3339)
	}
	if st.Installed && NeedsUpdate(powerDir, dwytBin) {
		st.Errors = append(st.Errors, "needs_update")
	}
	if target, err := os.Readlink(st.KiroLink); err != nil || target != powerDir {
		st.Installed = false
		if err != nil {
			st.Errors = append(st.Errors, err.Error())
		} else {
			st.Errors = append(st.Errors, "kiro symlink points to "+target)
		}
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
	return fmt.Sprintf(`# DWYT Power

DWYT (Don't Waste Your Tokens) is a local orchestrator that reduces AI token consumption by managing Obsidian memory, the Codebase graph, RTK terminal compression, and the Headroom API proxy.

## Keywords

dwyt, codebase, obsidian, rtk, headroom, mcp, memory, tokens, compression, vault, knowledge, graph, proxy, context, economia de tokens, memoria

## Tools

### Obsidian - Project Memory (ALWAYS FIRST)
Persistent markdown vault per project. Search before reading files.
- Search: GET http://localhost:2737/api/obsidian/search?q=<query>
- Save:   POST http://localhost:2737/api/obsidian/save
- Types:  decision, task, note, error, command, session

### Codebase - Code Knowledge Graph (ON DEMAND)
Structural exploration of the codebase. Use only for architecture questions.
- MCP tools: search_graph, trace_call_path, get_code_snippet
- Start: POST http://localhost:2737/api/services/codebase/start

### RTK - Terminal Compression (ALWAYS)
Prefix all shell commands with rtk to reduce output before it enters context.
- Usage: rtk git status, rtk go test ./...

### Headroom - API Proxy (AUTOMATIC)
Compresses API calls. Auto-detected via env vars.
- Active when: OPENAI_BASE_URL or ANTHROPIC_BASE_URL point to 127.0.0.1:8787

## Priority Order
1. Obsidian FIRST - check vault before any file read
2. Headroom - auto via env vars
3. RTK - prefix all shell commands
4. Codebase - structural exploration only

## Project

Path: %s
DWYT bin: %s

## MCP Availability

- codebase: %t
- obsidian: %t
`, projectPath, dwytBin, mcps["codebase"], mcps["obsidian"])
}

func GenerateMCPJSON(dwytBin string, mcps map[string]bool) (string, error) {
	servers := map[string]interface{}{}
	if mcps["codebase"] {
		servers["codebase"] = map[string]interface{}{
			"command": filepath.Join(dwytBin, executableName("codebase-memory-mcp")),
			"args":    []string{"--ui=true", "--port=9749"},
		}
	}
	if mcps["obsidian"] {
		servers["obsidian"] = map[string]interface{}{
			"command": filepath.Join(dwytBin, executableName("dwyt-obsidian-mcp")),
			"args":    []string{},
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
inclusion: auto
---

# DWYT Context Rules

## Priority Order

1. Obsidian FIRST - before reading files, search the project vault:
   GET http://localhost:2737/api/obsidian/search?q=<your query>
2. Headroom - auto-detected via OPENAI_BASE_URL / ANTHROPIC_BASE_URL.
3. RTK - prefix all shell commands with rtk.
4. Codebase MCP - only for structural code exploration.

After important work, save decisions to Obsidian:
POST http://localhost:2737/api/obsidian/save
{"type":"decision","content":"..."}
`
}

func steeringObsidian(projectPath string) string {
	return fmt.Sprintf(`---
inclusion: auto
---

# Obsidian - Project Memory

Project path: %s
Vault root: ~/.dwyt/projects/<id>/obsidian/

## API
- Search: GET http://localhost:2737/api/obsidian/search?q=<query>
- Save:   POST http://localhost:2737/api/obsidian/save
- Status: GET http://localhost:2737/api/obsidian/status

## Rules
- Always search Obsidian before reading project files.
- Always save important decisions after completing work.
- Never delete vault files.
`, projectPath)
}

func steeringCodebase() string {
	return `---
inclusion: manual
---

# Codebase - Code Knowledge Graph

Use only when you need to understand code structure. Prefer Obsidian context first.

## MCP Tools
- search_graph
- trace_call_path
- get_code_snippet

## API
- Start: POST http://localhost:2737/api/services/codebase/start
- Index: POST http://localhost:2737/api/codebase/index
- Status: GET http://localhost:2737/api/services/codebase/status
`
}

func steeringRTK() string {
	return `---
inclusion: auto
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
inclusion: auto
---

# Headroom - API Proxy

Headroom compresses AI API calls automatically.

## Detection
If OPENAI_BASE_URL or ANTHROPIC_BASE_URL point to 127.0.0.1:8787, Headroom is active.

## Status
GET http://localhost:2737/api/services/headroom/status
`
}
