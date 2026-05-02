package integrate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func Project(projectPath, clients string) {
	gitignore := filepath.Join(projectPath, ".gitignore")
	ensureDWYT(gitignore)

	cm := map[string][]string{
		"claude":  {".claude/"},
		"codex":   {".codex", "AGENTS.md"},
		"copilot": {".github/copilot-instructions.md"},
		"kiro":    {".kiro/"},
		"cursor":  {".cursor/"},
		"opencode": {"opencode.json"},
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
	writeIfMissing(filepath.Join(projectPath, ".mcp.json"), mcpJSON)
	writeIfMissing(filepath.Join(projectPath, "opencode.json"), opencodeJSON)

	// Generate CLAUDE.md
	if strings.Contains(clients, "claude") {
		cp := filepath.Join(projectPath, ".claude", "CLAUDE.md")
		os.MkdirAll(filepath.Dir(cp), 0755)
		writeIfMissing(cp, claudeMD)
	}

	// Generate .cursor/rules/dwyt.mdc
	if strings.Contains(clients, "cursor") {
		cp := filepath.Join(projectPath, ".cursor", "rules", "dwyt.mdc")
		os.MkdirAll(filepath.Dir(cp), 0755)
		writeIfMissing(cp, cursorRule)
	}

	// Generate .kiro/steering/dwyt.md
	if strings.Contains(clients, "kiro") {
		cp := filepath.Join(projectPath, ".kiro", "steering", "dwyt.md")
		os.MkdirAll(filepath.Dir(cp), 0755)
		writeIfMissing(cp, kiroSteering)
	}

	// Generate .github/copilot-instructions.md
	if strings.Contains(clients, "copilot") {
		cp := filepath.Join(projectPath, ".github", "copilot-instructions.md")
		os.MkdirAll(filepath.Dir(cp), 0755)
		writeIfMissing(cp, copilotMD)
	}

	// AGENTS.md
	writeIfMissing(filepath.Join(projectPath, "AGENTS.md"), agentsMD)

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
	os.WriteFile(path, []byte(content), 0644)
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
`

const cursorRule = `---
description: DWYT project guidance
alwaysApply: true
---

Follow AGENTS.md instructions.
- Prefer MCP tools over manual file search
- Use rtk prefix for shell commands
`

const kiroSteering = `# DWYT Steering
Follow AGENTS.md instructions.
- Prefer MCP tools over manual file search
- Use rtk prefix for shell commands
`

const copilotMD = `# DWYT — GitHub Copilot
Follow AGENTS.md instructions.
- Prefer MCP tools over manual file search
- Use rtk prefix for shell commands
`

const agentsMD = `# DWYT — Don't Waste Your Tokens

Este projeto usa um stack de ferramentas para reduzir consumo de tokens.
**Todas as integrações são automáticas: use-as sempre que disponíveis.**

## Regra geral (siga SEMPRE, sem hesitação):
- Se o MCP do codebase-memory-mcp estiver conectado, use as tools MCP como PRIMEIRA opção antes de grep/glob/read
- **SEMPRE** prefixe comandos de terminal com ` + "`rtk`" + `
- Se Headroom estiver ativo via wrapper, use Headroom (se não estiver, ignore)
- Se MemStack estiver disponível, consulte-o antes de começar tarefas novas
`
