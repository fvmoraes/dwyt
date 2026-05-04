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

	cm := map[string][]string{
		"claude":   {".claude/"},
		"codex":    {".codex", "AGENTS.md", ".mcp.json"},
		"copilot":  {".github/copilot-instructions.md"},
		"kiro":     {".kiro/"},
		"cursor":   {".cursor/"},
		"opencode": {"opencode.json", "AGENTS.md", ".mcp.json"},
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
	appendLine(gitignore, ".dwyt/")

	// ── Use absolute paths in generated configs ────────────────────────
	cbmcpBin := filepath.Join(dwytBin, "codebase-memory-mcp")
	rtkBin    := filepath.Join(dwytBin, "rtk")
	if runtime.GOOS == "windows" {
		cbmcpBin += ".exe"
		rtkBin += ".exe"
	}

	writeIfMissing(filepath.Join(projectPath, ".mcp.json"), mcpJSONTemplate(cbmcpBin))
	writeIfMissing(filepath.Join(projectPath, "opencode.json"), opencodeJSONTemplate(cbmcpBin, rtkBin))

	if strings.Contains(clients, "claude") {
		cp := filepath.Join(projectPath, "CLAUDE.md")
		writeIfMissing(cp, claudeMD)
		os.MkdirAll(filepath.Join(projectPath, ".claude"), 0755)
		// Claude also reads .claude/mcp.json
		writeIfMissing(filepath.Join(projectPath, ".claude", "mcp.json"), mcpJSONTemplate(cbmcpBin))
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
		writeIfMissing(filepath.Join(projectPath, ".kiro", "mcp.json"), mcpJSONTemplate(cbmcpBin))
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

func writeIfMissing(path, content string) {
	if _, err := os.Stat(path); err == nil {
		return
	}
	os.MkdirAll(filepath.Dir(path), 0755)
	os.WriteFile(path, []byte(content), 0644)
}

// ── Templates with absolute binary paths ──────────────────────────────────────

func mcpJSONTemplate(cbmcpBin string) string {
	return fmt.Sprintf(`{
  "mcpServers": {
    "dwyt": {
      "type": "stdio",
      "command": %q
    }
  }
}
`, cbmcpBin)
}

func opencodeJSONTemplate(cbmcpBin, rtkBin string) string {
	return fmt.Sprintf(`{
  "$schema": "https://opencode.ai/config.json",
  "instructions": ["AGENTS.md"],
  "mcp": {
    "dwyt": {
      "type": "local",
      "command": [%q]
    }
  },
  "permission": {
    "bash": "allow",
    "edit": "allow",
    "webfetch": "allow",
    "skill": "allow"
  },
  "rtkBin": %q
}
`, cbmcpBin, rtkBin)
}

func agentsMDTemplate(rtkBin string) string {
	backtick := "`"
	return "# DWYT — Don't Waste Your Tokens\n\n" +
		"Este projeto usa ferramentas automáticas para reduzir consumo de tokens.\n" +
		"**Todas as integrações são transparentes: você não precisa configurar nada.**\n\n" +
		"## Ordem de prioridade (siga sempre esta ordem):\n\n" +
		"### 1. Project Brain (Obsidian) — PRIMEIRA FONTE DE CONTEXTO\n" +
		"The project brain is an Obsidian vault with markdown files.\n" +
		"Search: GET http://127.0.0.1:2737/api/brain/search?q=<query>\n" +
		"Save: POST http://127.0.0.1:2737/api/brain/save\n\n" +
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
		"- Prefira consultar o brain/contexto do projeto antes de indexar ou navegar no código.\n" +
		"- Use `search_graph`, `trace_call_path`, `get_code_snippet` ao invés de grep/glob.\n"
}

const claudeMD = `# DWYT — Don't Waste Your Tokens

## Priority order (follow strictly):
1. **Project Brain (Obsidian) FIRST** — before any file read or context rebuild:
   - Search: GET http://127.0.0.1:2737/api/brain/search?q=your+query
   - Save: POST http://127.0.0.1:2737/api/brain/save {"type":"decision","content":"..."}
   - Types: command, decision, action, error, note
   - Never mix memory between projects — isolation is automatic.

2. **Headroom** — auto-detected via OPENAI_BASE_URL / ANTHROPIC_BASE_URL env vars.
   If set, use them. If not, use default API endpoints. No manual config needed.

3. **RTK** — always prefix shell commands with rtk. Reduces output 60-90%.

4. **Codebase MCP** — ONLY when you need structural code understanding.
   Prefer Project Brain (Obsidian) context first. Use search_graph, trace_call_path, get_code_snippet.
`

const cursorRule = `---
description: DWYT project guidance
alwaysApply: true
---

## DWYT Priority Order:
1. **Project Brain (Obsidian) FIRST**: consult project memory before any file operation.
   Search: GET http://127.0.0.1:2737/api/brain/search?q=<query>
   Save: POST http://127.0.0.1:2737/api/brain/save
2. **Headroom**: auto via OPENAI_BASE_URL / ANTHROPIC_BASE_URL env vars.
3. **RTK**: prefix shell commands with rtk.
4. **Codebase MCP**: use ONLY for structural code exploration.
Save important decisions to Project Brain (Obsidian) after completion.
`

const kiroSteering = `# DWYT Steering

## Priority:
1. **Project Brain (Obsidian) FIRST**: check project memory before reading files.
   Search: GET http://127.0.0.1:2737/api/brain/search?q=<query>
   Save: POST http://127.0.0.1:2737/api/brain/save {"type":"decision","content":"..."}
2. **Headroom**: auto-detected via env vars OPENAI_BASE_URL / ANTHROPIC_BASE_URL
3. **RTK**: prefix all shell commands with rtk
4. **Codebase MCP**: structural exploration only — use after Project Brain (Obsidian)

Save important decisions to Project Brain (Obsidian) after completion.
`

const copilotMD = `# DWYT — GitHub Copilot

## Priority:
1. **Project Brain (Obsidian) FIRST**: check project memory before heavy file reads.
   Search: GET http://127.0.0.1:2737/api/brain/search?q=<query>
   Save: POST http://127.0.0.1:2737/api/brain/save
2. **Headroom**: compression auto-detected via OPENAI_BASE_URL / ANTHROPIC_BASE_URL
3. **RTK**: prefix shell commands with rtk
4. **Codebase MCP**: structural exploration only when needed

Save summaries after important changes via Project Brain (Obsidian) API.
`

var markerStart = "<!-- dwyt:headroom-proxy-start -->"
var markerEnd = "<!-- dwyt:headroom-proxy-end -->"

func WriteHeadroomProxyConfig(projectPath string, headroomPort int, clients string) error {
	dwytDir := filepath.Join(projectPath, ".dwyt")
	os.MkdirAll(dwytDir, 0755)

	proxyConfig := map[string]interface{}{
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
			setOpenCodeBaseURL(filepath.Join(projectPath, "opencode.json"), headroomPort)
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
	proxyFile := filepath.Join(projectPath, ".dwyt", "headroom-proxy.json")
	if data, err := os.ReadFile(proxyFile); err == nil {
		var cfg map[string]interface{}
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
	removeOpenCodeBaseURL(filepath.Join(projectPath, "opencode.json"))

	return nil
}

func appendMarkedBlock(filePath, block string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil
	}
	content := string(data)
	if strings.Contains(content, markerStart) {
		return nil
	}
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil
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
	var m map[string]interface{}
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
	var m map[string]interface{}
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
