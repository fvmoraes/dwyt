package integrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/fvmoraes/dwyt/internal/log"
	"github.com/fvmoraes/dwyt/internal/workspace"
)

func Project(projectPath, clients, dwytBin string) {
	if dwytBin == "" {
		dwytBin = filepath.Join(os.Getenv("HOME"), ".dwyt", "bin")
	}

	log.Info("integrating project", log.Fields{"path": projectPath, "clients": clients})
	gitignore := filepath.Join(projectPath, ".gitignore")
	ensureDWYT(gitignore)
	removeDWYTLegacyIgnores(gitignore)

	cm := map[string][]string{
		"claude":   {"CLAUDE.md", ".claude/mcp.json"},
		"codex":    {".codex/", ".mcp.json"},
		"copilot":  {},
		"kiro":     {".kiro/mcp.json"},
		"cursor":   {".cursorrules"},
		"opencode": {"opencode.json", ".mcp.json"},
	}

	for _, c := range strings.Split(clients, ",") {
		c = strings.TrimSpace(c)
		if entries, ok := cm[c]; ok {
			for _, e := range entries {
				appendLine(gitignore, e)
			}
		}
	}

	appendLine(gitignore, ".mcp.json")
	appendLine(gitignore, ".vscode/mcp.json")
	appendLine(gitignore, ".cursorrules")
	appendLine(gitignore, "CLAUDE.md")
	appendLine(gitignore, ".claude/mcp.json")
	appendLine(gitignore, ".kiro/mcp.json")
	appendLine(gitignore, "opencode.json")
	// Note: .dwyt/ is no longer created inside projects — state lives in ~/.dwyt/projects/

	// ── Use absolute paths in generated configs ────────────────────────
	cbmcpBin := filepath.Join(dwytBin, "codebase-memory-mcp")
	obsidianMCPBin := filepath.Join(dwytBin, "dwyt-obsidian-mcp")
	rtkBin := filepath.Join(dwytBin, "rtk")
	if runtime.GOOS == "windows" {
		cbmcpBin += ".exe"
		obsidianMCPBin += ".exe"
		rtkBin += ".exe"
	}

	writeOrMergeMCPJSON(filepath.Join(projectPath, ".mcp.json"), cbmcpBin, obsidianMCPBin)
	writeOrMergeOpenCodeJSON(filepath.Join(projectPath, "opencode.json"), cbmcpBin, obsidianMCPBin, rtkBin)

	if strings.Contains(clients, "claude") {
		cp := filepath.Join(projectPath, "CLAUDE.md")
		writeIfMissing(cp, claudeMD)
		os.MkdirAll(filepath.Join(projectPath, ".claude"), 0755)
		// Claude also reads .claude/mcp.json
		writeOrMergeMCPJSON(filepath.Join(projectPath, ".claude", "mcp.json"), cbmcpBin, obsidianMCPBin)
	}

	if strings.Contains(clients, "cursor") {
		cp := filepath.Join(projectPath, ".cursor", "rules", "dwyt.mdc")
		os.MkdirAll(filepath.Dir(cp), 0755)
		writeIfMissing(cp, cursorRule)
	}

	if strings.Contains(clients, "kiro") {
		cp := filepath.Join(projectPath, ".kiro", "steering", "dwyt.md")
		os.MkdirAll(filepath.Dir(cp), 0755)
		writeIfMissing(cp, kiroSteering)
		// Kiro also reads .kiro/mcp.json
		writeOrMergeMCPJSON(filepath.Join(projectPath, ".kiro", "mcp.json"), cbmcpBin, obsidianMCPBin)
	}

	if strings.Contains(clients, "copilot") {
		cp := filepath.Join(projectPath, ".github", "copilot-instructions.md")
		os.MkdirAll(filepath.Dir(cp), 0755)
		writeIfMissing(cp, copilotMD)
	}

	writeIfMissing(filepath.Join(projectPath, "AGENTS.md"), agentsMDTemplate(rtkBin))

	// ── Per-project workspace state ─────────────────────────────────────
	workspace.Touch(projectPath)

	fmt.Printf("  ✓ Projeto integrado: %s\n", projectPath)
}

func ensureDWYT(path string) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		os.WriteFile(path, []byte("# dwyt\n"), 0644)
		return
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "# dwyt") {
		f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
		f.Write([]byte("\n# dwyt\n"))
		f.Close()
	}
}

func appendLine(path, line string) {
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), line) {
		return
	}
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	defer f.Close()
	f.Write([]byte(line + "\n"))
}

func removeDWYTLegacyIgnores(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	remove := map[string]bool{
		"AGENTS.md":                       true,
		".cursor/":                        true,
		".kiro/":                          true,
		".github/copilot-instructions.md": true,
		".cursor/rules/dwyt.mdc":          true,
		".kiro/steering/dwyt.md":          true,
	}
	lines := strings.Split(string(data), "\n")
	kept := make([]string, 0, len(lines))
	changed := false
	for _, line := range lines {
		if remove[strings.TrimSpace(line)] {
			changed = true
			continue
		}
		kept = append(kept, line)
	}
	if changed {
		os.WriteFile(path, []byte(strings.Join(kept, "\n")), 0644)
	}
}

func writeIfMissing(path, content string) {
	if _, err := os.Stat(path); err == nil {
		return
	}
	os.MkdirAll(filepath.Dir(path), 0755)
	os.WriteFile(path, []byte(content), 0644)
}

func writeOrMergeMCPJSON(path, cbmcpBin, obsidianMCPBin string) {
	config := map[string]interface{}{}
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &config)
	}
	servers, _ := config["mcpServers"].(map[string]interface{})
	if servers == nil {
		servers = map[string]interface{}{}
	}
	removeLegacyMCPKeys(servers)
	servers["codebase"] = map[string]interface{}{
		"type":    "stdio",
		"command": cbmcpBin,
	}
	servers["obsidian"] = map[string]interface{}{
		"type":    "stdio",
		"command": obsidianMCPBin,
	}
	config["mcpServers"] = servers
	writeJSON(path, config)
}

func writeOrMergeOpenCodeJSON(path, cbmcpBin, obsidianMCPBin, _ string) {
	config := map[string]interface{}{}
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &config)
	}
	if _, ok := config["$schema"]; !ok {
		config["$schema"] = "https://opencode.ai/config.json"
	}
	config["instructions"] = ensureStringItem(config["instructions"], "AGENTS.md")

	mcp, _ := config["mcp"].(map[string]interface{})
	if mcp == nil {
		mcp = map[string]interface{}{}
	}
	removeLegacyMCPKeys(mcp)
	mcp["codebase"] = map[string]interface{}{
		"type":    "local",
		"command": []interface{}{cbmcpBin},
	}
	mcp["obsidian"] = map[string]interface{}{
		"type":    "local",
		"command": []interface{}{obsidianMCPBin},
	}
	config["mcp"] = mcp

	permission, _ := config["permission"].(map[string]interface{})
	if permission == nil {
		permission = map[string]interface{}{}
	}
	for _, k := range []string{"bash", "edit", "webfetch", "skill"} {
		if _, ok := permission[k]; !ok {
			permission[k] = "allow"
		}
	}
	config["permission"] = permission

	writeJSON(path, config)
}

func removeLegacyMCPKeys(m map[string]interface{}) {
	for _, key := range []string{"dwyt", "dwyt-codebase", "dwyt-obsidian", "obsidian-mcp"} {
		delete(m, key)
	}
}

func ensureStringItem(value interface{}, item string) []interface{} {
	list := []interface{}{}
	if existing, ok := value.([]interface{}); ok {
		list = append(list, existing...)
	}
	for _, v := range list {
		if s, ok := v.(string); ok && s == item {
			return list
		}
	}
	return append(list, item)
}

func writeJSON(path string, value interface{}) {
	os.MkdirAll(filepath.Dir(path), 0755)
	data, _ := json.MarshalIndent(value, "", "  ")
	os.WriteFile(path, append(data, '\n'), 0644)
}

// ── Templates with absolute binary paths ──────────────────────────────────────

func mcpJSONTemplate(cbmcpBin, obsidianMCPBin string) string {
	return fmt.Sprintf(`{
  "mcpServers": {
    "codebase": {
      "type": "stdio",
      "command": %q
    },
    "obsidian": {
      "type": "stdio",
      "command": %q
    }
  }
}
`, cbmcpBin, obsidianMCPBin)
}

func opencodeJSONTemplate(cbmcpBin, obsidianMCPBin, rtkBin string) string {
	return fmt.Sprintf(`{
  "$schema": "https://opencode.ai/config.json",
  "instructions": ["AGENTS.md"],
  "mcp": {
    "codebase": {
      "type": "local",
      "command": [%q]
    },
    "obsidian": {
      "type": "local",
      "command": [%q]
    }
  },
  "permission": {
    "bash": "allow",
    "edit": "allow",
    "webfetch": "allow",
    "skill": "allow"
  }
}
`, cbmcpBin, obsidianMCPBin)
}

func agentsMDTemplate(rtkBin string) string {
	backtick := "`"
	return "# DWYT — Don't Waste Your Tokens\n\n" +
		"Este projeto usa ferramentas automáticas para reduzir consumo de tokens.\n" +
		"**Todas as integrações são transparentes: você não precisa configurar nada.**\n\n" +
		"## Ordem de prioridade (siga sempre esta ordem):\n\n" +
		"### 1. Obsidian — VAULT DO PROJETO (PRIMEIRA FONTE DE CONTEXTO)\n" +
		"The project vault is an Obsidian vault with markdown files.\n" +
		"Search: GET http://localhost:2737/api/obsidian/search?q=<query>\n" +
		"Save: POST http://localhost:2737/api/obsidian/save\n\n" +
		"### 2. Headroom — Compressão de API (AUTOMÁTICO)\n" +
		"- Se `OPENAI_BASE_URL` ou `ANTHROPIC_BASE_URL` estiverem setadas, use-as.\n" +
		"- O Headroom comprime chamadas automaticamente — você não precisa fazer nada.\n" +
		"- Se não estiver ativo, use os endpoints padrão das APIs.\n\n" +
		"### 3. RTK — Compressão de Terminal\n" +
		"- **SEMPRE** prefixe comandos shell com " + backtick + "rtk" + backtick + ". Ex: `rtk git status`\n" +
		"- Isto reduz o output em 60-90% antes de entrar no contexto.\n" +
		"- Em comandos encadeados, prefixe cada segmento: `rtk git add . && rtk git commit -m \"msg\"`\n\n" +
		"### 4. Codebase — Mapa do Código (SOB DEMANDA)\n" +
		"- **APENAS** use o MCP codebase-memory-mcp quando precisar entender estrutura real.\n" +
		"- Prefira consultar o Obsidian/contexto do projeto antes de indexar ou navegar no código.\n" +
		"- Use `search_graph`, `trace_call_path`, `get_code_snippet` ao invés de grep/glob.\n"
}

const claudeMD = `# DWYT — Don't Waste Your Tokens

## Priority order (follow strictly):
1. **Obsidian FIRST** — before any file read or context rebuild:
   - Search: GET http://localhost:2737/api/obsidian/search?q=your+query
   - Save: POST http://localhost:2737/api/obsidian/save {"type":"decision","content":"..."}
   - Types: command, decision, action, error, note
   - Never mix vaults between projects — isolation is automatic.

2. **Headroom** — auto-detected via OPENAI_BASE_URL / ANTHROPIC_BASE_URL env vars.
   If set, use them. If not, use default API endpoints. No manual config needed.

3. **RTK** — always prefix shell commands with rtk. Reduces output 60-90%.

4. **Codebase MCP** — ONLY when you need structural code understanding.
   Prefer Obsidian context first. Use search_graph, trace_call_path, get_code_snippet.
`

const cursorRule = `---
description: DWYT project guidance
alwaysApply: true
---

## DWYT Priority Order:
1. **Obsidian FIRST**: consult project vault before any file operation.
   Search: GET http://localhost:2737/api/obsidian/search?q=<query>
   Save: POST http://localhost:2737/api/obsidian/save
2. **Headroom**: auto via OPENAI_BASE_URL / ANTHROPIC_BASE_URL env vars.
3. **RTK**: prefix shell commands with rtk.
4. **Codebase MCP**: use ONLY for structural code exploration.
Save important decisions to Obsidian after completion.
`

const kiroSteering = `# DWYT Steering

## Priority:
1. **Obsidian FIRST**: check project vault before reading files.
   Search: GET http://localhost:2737/api/obsidian/search?q=<query>
   Save: POST http://localhost:2737/api/obsidian/save {"type":"decision","content":"..."}
2. **Headroom**: auto-detected via env vars OPENAI_BASE_URL / ANTHROPIC_BASE_URL
3. **RTK**: prefix all shell commands with rtk
4. **Codebase MCP**: structural exploration only — use after Obsidian

Save important decisions to Obsidian after completion.
`

const copilotMD = `# DWYT — GitHub Copilot

## Priority:
1. **Obsidian FIRST**: check project vault before heavy file reads.
   Search: GET http://localhost:2737/api/obsidian/search?q=<query>
   Save: POST http://localhost:2737/api/obsidian/save
2. **Headroom**: compression auto-detected via OPENAI_BASE_URL / ANTHROPIC_BASE_URL
3. **RTK**: prefix shell commands with rtk
4. **Codebase MCP**: structural exploration only when needed

Save summaries after important changes via Obsidian API.
`

var markerStart = "<!-- dwyt:headroom-proxy-start -->"
var markerEnd = "<!-- dwyt:headroom-proxy-end -->"

func WriteHeadroomProxyConfig(projectPath string, headroomPort int, clients string) error {
	// Store proxy state in ~/.dwyt/projects/<id>/ — never inside the project
	dwytDir := workspace.ProjectDir(projectPath)
	os.MkdirAll(dwytDir, 0755)

	proxyConfig := map[string]any{
		"active":     true,
		"port":       headroomPort,
		"started_at": time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(proxyConfig, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dwytDir, "headroom-proxy.json"), data, 0644); err != nil {
		return err
	}

	block := fmt.Sprintf("%s\n**Headroom proxy is ACTIVE** on http://127.0.0.1:%d — use OPENAI_BASE_URL and ANTHROPIC_BASE_URL env vars automatically.\n%s\n", markerStart, headroomPort, markerEnd)

	for _, c := range strings.Split(clients, ",") {
		c = strings.TrimSpace(c)
		switch c {
		case "opencode":
			appendMarkedBlock(filepath.Join(projectPath, "AGENTS.md"), block)
		case "claude":
			appendMarkedBlock(filepath.Join(projectPath, "CLAUDE.md"), block)
			appendMarkedBlock(filepath.Join(projectPath, "AGENTS.md"), block)
		case "codex":
			appendMarkedBlock(filepath.Join(projectPath, "AGENTS.md"), block)
		case "copilot":
			cp := filepath.Join(projectPath, ".github", "copilot-instructions.md")
			os.MkdirAll(filepath.Dir(cp), 0755)
			appendMarkedBlock(cp, block)
			appendMarkedBlock(filepath.Join(projectPath, "AGENTS.md"), block)
		case "kiro":
			cp := filepath.Join(projectPath, ".kiro", "steering", "dwyt.md")
			os.MkdirAll(filepath.Dir(cp), 0755)
			appendMarkedBlock(cp, block)
			appendMarkedBlock(filepath.Join(projectPath, "AGENTS.md"), block)
		case "cursor":
			cp := filepath.Join(projectPath, ".cursor", "rules", "dwyt.mdc")
			os.MkdirAll(filepath.Dir(cp), 0755)
			appendMarkedBlock(cp, block)
			appendMarkedBlock(filepath.Join(projectPath, "AGENTS.md"), block)
		}
	}

	return nil
}

func RemoveHeadroomProxyConfig(projectPath string, clients string) error {
	// Proxy state lives in ~/.dwyt/projects/<id>/
	proxyFile := filepath.Join(workspace.ProjectDir(projectPath), "headroom-proxy.json")
	if data, err := os.ReadFile(proxyFile); err == nil {
		var cfg map[string]any
		if json.Unmarshal(data, &cfg) == nil {
			cfg["active"] = false
			if newData, err := json.MarshalIndent(cfg, "", "  "); err == nil {
				os.WriteFile(proxyFile, newData, 0644)
			}
		}
	}

	removeMarkedBlocks(filepath.Join(projectPath, "CLAUDE.md"))
	removeMarkedBlocks(filepath.Join(projectPath, ".cursor", "rules", "dwyt.mdc"))
	removeMarkedBlocks(filepath.Join(projectPath, ".kiro", "steering", "dwyt.md"))
	removeMarkedBlocks(filepath.Join(projectPath, "AGENTS.md"))
	removeMarkedBlocks(filepath.Join(projectPath, ".github", "copilot-instructions.md"))
	removeMarkedBlocks(filepath.Join(projectPath, "opencode.json"))

	return nil
}

func appendMarkedBlock(filePath, block string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		// Create file if it doesn't exist
		if os.IsNotExist(err) {
			os.MkdirAll(filepath.Dir(filePath), 0755)
			return os.WriteFile(filePath, []byte(block), 0644)
		}
		return err
	}
	content := string(data)
	if strings.Contains(content, markerStart) {
		return nil // Already injected
	}
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	if len(content) > 0 && content[len(content)-1] != '\n' {
		f.Write([]byte("\n"))
	}
	f.Write([]byte(block))
	return nil
}

func removeMarkedBlocks(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil
	}
	content := string(data)

	for {
		startIdx := strings.Index(content, markerStart)
		if startIdx == -1 {
			break
		}
		endIdx := strings.Index(content, markerEnd)
		if endIdx == -1 {
			break
		}
		end := endIdx + len(markerEnd)
		if end < len(content) && content[end] == '\n' {
			end++
		}
		if startIdx > 0 && content[startIdx-1] == '\n' {
			startIdx--
		}
		content = content[:startIdx] + content[end:]
	}

	if string(data) != content {
		os.WriteFile(filePath, []byte(content), 0644)
	}
	return nil
}

func setOpenCodeBaseURL(filePath string, port int) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return
	}
	m["baseUrl"] = fmt.Sprintf("http://127.0.0.1:%d/v1", port)
	newData, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(filePath, newData, 0644)
}

func removeOpenCodeBaseURL(filePath string) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return
	}
	if url, ok := m["baseUrl"].(string); ok && strings.Contains(url, "127.0.0.1") {
		delete(m, "baseUrl")
		newData, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			return
		}
		os.WriteFile(filePath, newData, 0644)
	}
}
