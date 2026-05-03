package integrate

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

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
		"Este projeto usa um stack de ferramentas para reduzir consumo de tokens.\n" +
		"**Todas as integrações são automáticas: use-as sempre que disponíveis.**\n\n" +
		"## Prioridades (siga esta ordem):\n\n" +
		"### 1. MemStack — Memória Persistente do Projeto\n" +
		"- **SEMPRE** consulte a memória DWYT do projeto antes de começar tarefas novas.\n" +
		"- Antes de pedir arquivos grandes ao usuário, busque contexto salvo.\n" +
		"- Antes de reconstruir contexto, busque decisões anteriores.\n" +
		"- Após concluir uma mudança relevante, salve um resumo curto:\n" +
		"  - Use a API: POST /api/memory/save com type=\"decision\" ou type=\"action\"\n" +
		"- **NUNCA** misture memória entre projetos diferentes.\n" +
		"- A memória é isolada por projeto e carregada automaticamente.\n\n" +
		"### 2. Codebase — Grafo Estrutural do Código\n" +
		"- Use as tools MCP do codebase-memory-mcp para navegação estrutural.\n" +
		"- Use search_graph, trace_call_path, get_code_snippet ao invés de grep/glob/read.\n" +
		"- **APENAS** use codebase quando precisar entender a estrutura real do código.\n\n" +
		"### 3. RTK — Compressão de Terminal\n" +
		"- **SEMPRE** prefixe comandos de terminal com " + backtick + "rtk" + backtick + "\n" +
		"- Isto reduz o contexto em 60-90%\n\n" +
		"### 4. Headroom — Compressão de API\n" +
		"- Se o Headroom estiver ativo (verifique variáveis *_BASE_URL), use-o automaticamente.\n" +
		"- Não precisa de configuração manual.\n"
}

const claudeMD = `# DWYT — Don't Waste Your Tokens
Claude Code integration.

## Priority order:
1. **MemStack first** — consult project memory before reading large files or rebuilding context
2. **Codebase MCP** — use codebase-memory-mcp tools for structural code exploration
3. **RTK prefix** — always prefix shell commands with rtk
4. **Headroom** — auto-detected via *_BASE_URL env vars

To save context: POST http://127.0.0.1:2737/api/memory/save
To search memory: GET http://127.0.0.1:2737/api/memory/search?q=your+query
`

const cursorRule = `---
description: DWYT project guidance
alwaysApply: true
---

## DWYT Priority Order:
1. MemStack: consult project memory before file operations. API at http://127.0.0.1:2737/api/memory/
2. Codebase MCP: use for structural code exploration
3. RTK: prefix shell commands with rtk
4. Headroom: auto via env vars
`

const kiroSteering = `# DWYT Steering
## Priority:
1. Check MemStack (project memory) — http://127.0.0.1:2737/api/memory/search?q=<query>
2. Use Codebase MCP for structural exploration
3. Prefix shell commands with rtk
4. Headroom auto-detected
Save important decisions to MemStack after completion.
`

const copilotMD = `# DWYT — GitHub Copilot
## Priority:
1. Check project memory (MemStack) at http://127.0.0.1:2737/api/memory/ before heavy file reads
2. Use Codebase MCP tools for structural exploration  
3. Prefix shell commands with rtk
4. Headroom compression is automatic
Save summaries after important changes.
`
