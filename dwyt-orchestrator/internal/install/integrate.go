package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DeusData/dwyt-orchestrator/internal/detect"
)

func Integrate(e *detect.Env, projectPath, clientSelection, toolSelection string) {
	gitignore := filepath.Join(projectPath, ".gitignore")
	ensureDWYTSection(gitignore)

	// gitignore entries per client
	ci := map[string][]string{
		"c": {".claude/"},
		"x": {".codex", "AGENTS.md"},
		"p": {".github/copilot-instructions.md"},
		"k": {".kiro/"},
		"r": {".cursor/"},
		"o": {"opencode.json"},
	}
	for _, ch := range clientSelection {
		if entries, ok := ci[string(ch)]; ok {
			for _, entry := range entries {
				appendLine(gitignore, entry)
			}
		}
	}

	// .mcp.json
	if strings.ContainsRune(toolSelection, 'c') {
		appendLine(gitignore, ".mcp.json")
		writeJSON(filepath.Join(projectPath, ".mcp.json"), mcpJSON)
		writeJSON(filepath.Join(projectPath, "opencode.json"), opencodeJSON)
	}

	// AGENTS.md
	if strings.ContainsRune(toolSelection, 'c') {
		clients := []string{}
		for _, ch := range clientSelection {
			switch ch {
			case 'c':
				clients = append(clients, "Claude Code")
			case 'x':
				clients = append(clients, "Codex")
			case 'p':
				clients = append(clients, "GitHub Copilot")
			case 'k':
				clients = append(clients, "Kiro")
			case 'r':
				clients = append(clients, "Cursor")
			case 'o':
				clients = append(clients, "OpenCode")
			}
		}
		writeAgentsMD(filepath.Join(projectPath, "AGENTS.md"), clients)
	}

	// CLAUDE.md
	if strings.ContainsRune(clientSelection, 'c') {
		cPath := filepath.Join(projectPath, ".claude", "CLAUDE.md")
		os.MkdirAll(filepath.Dir(cPath), 0755)
		if _, err := os.Stat(cPath); os.IsNotExist(err) {
			os.WriteFile(cPath, []byte(claudeMD), 0644)
		}
	}

	// .cursor/rules/dwyt.mdc
	if strings.ContainsRune(clientSelection, 'r') {
		cPath := filepath.Join(projectPath, ".cursor", "rules", "dwyt.mdc")
		os.MkdirAll(filepath.Dir(cPath), 0755)
		os.WriteFile(cPath, []byte(cursorRule), 0644)
	}

	// .kiro/steering/dwyt.md
	if strings.ContainsRune(clientSelection, 'k') {
		cPath := filepath.Join(projectPath, ".kiro", "steering", "dwyt.md")
		os.MkdirAll(filepath.Dir(cPath), 0755)
		os.WriteFile(cPath, []byte(kiroSteering), 0644)
	}

	// .github/copilot-instructions.md
	if strings.ContainsRune(clientSelection, 'p') {
		cPath := filepath.Join(projectPath, ".github", "copilot-instructions.md")
		os.MkdirAll(filepath.Dir(cPath), 0755)
		os.WriteFile(cPath, []byte(copilotMD), 0644)
	}

	fmt.Printf("  ✓ .mcp.json, opencode.json, AGENTS.md criados\n")
	if strings.ContainsRune(clientSelection, 'c') {
		fmt.Printf("  ✓ CLAUDE.md criado\n")
	}
	if strings.ContainsRune(clientSelection, 'r') {
		fmt.Printf("  ✓ .cursor/rules/dwyt.mdc criado\n")
	}
	if strings.ContainsRune(clientSelection, 'k') {
		fmt.Printf("  ✓ .kiro/steering/dwyt.md criado\n")
	}
	if strings.ContainsRune(clientSelection, 'p') {
		fmt.Printf("  ✓ .github/copilot-instructions.md criado\n")
	}
	fmt.Printf("  ✓ .gitignore atualizado\n")
}

func ensureDWYTSection(path string) {
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

func writeJSON(path, content string) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		os.WriteFile(path, []byte(content), 0644)
	}
}

func writeAgentsMD(path string, clients []string) {
	if _, err := os.Stat(path); err == nil {
		return
	}
	h := "# DWYT — Don't Waste Your Tokens\n\n"
	h += "**Todas as integrações são automáticas. Use-as sempre que disponíveis.**\n\n"
	h += "Clientes: " + strings.Join(clients, ", ") + "\n\n"
	h += "## Regra geral\n"
	h += "- MCP do codebase-memory-mcp como PRIMEIRA opção antes de grep/glob/read\n"
	h += "- **SEMPRE** prefixe comandos com `rtk` (ex: `rtk git status`, `rtk ls`)\n"
	h += "- Headroom ativo via wrapper → use-o; senão ignore\n"
	h += "- MemStack disponível → consulte-o antes de tarefas novas\n"
	os.WriteFile(path, []byte(h), 0644)
}

const mcpJSON = `{
  "mcpServers": {
    "codebase-memory-mcp": {
      "type": "stdio",
      "command": "codebase-memory-mcp"
    }
  }
}
`

const opencodeJSON = `{
  "$schema": "https://opencode.ai/config.json",
  "instructions": ["AGENTS.md"],
  "mcp": {
    "codebase-memory-mcp": {
      "type": "local",
      "command": ["codebase-memory-mcp"]
    }
  },
  "permission": {
    "bash": "allow",
    "edit": "allow",
    "webfetch": "allow",
    "skill": "allow"
  }
}
`

const claudeMD = `# DWYT — Don't Waste Your Tokens
Claude Code integration.
- Use codebase-memory-mcp as first option before file search
- Always prefix shell commands with rtk
- Use Headroom only when session started with wrapper
- Consult MemStack for context between sessions
`

const cursorRule = `---
description: DWYT project guidance
alwaysApply: true
---

Follow AGENTS.md instructions.
- Prefer MCP tools over manual file search
- Use rtk prefix for shell commands
- Headroom/RTK/MemStack: use when available, fallback silently otherwise
`

const kiroSteering = `# DWYT Steering
Follow AGENTS.md instructions.
- Prefer MCP tools over manual file search
- Use rtk prefix for shell commands
- Fallback silently when integrations unavailable
`

const copilotMD = `# DWYT — GitHub Copilot
Follow AGENTS.md instructions.
- Prefer MCP tools (.mcp.json) over manual file search
- Use rtk prefix for shell commands to reduce output
- Fallback silently when integrations unavailable
`
