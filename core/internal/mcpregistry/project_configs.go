package mcpregistry

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// syncConfiguredTargets writes MCP configs only for the AI clients the user
// selected. An empty selection writes nothing — DWYT never touches configs for
// clients that were left unchecked in setup.
func (r *Registry) syncConfiguredTargets(projectPath string, clients []string) []string {
	sel := clientSet(clients)
	errors := []string{}

	// Global (machine-level) client configs.
	if sel["claude"] {
		if err := r.SyncClaudeDesktop(); err != nil {
			errors = append(errors, "claude: "+err.Error())
		}
	}
	if sel["codex"] {
		if err := r.SyncCodexGlobal(); err != nil {
			errors = append(errors, "codex: "+err.Error())
		}
	}

	if projectPath == "" {
		return errors
	}

	// .mcp.json at the project root is read by both Claude Code and Codex.
	if sel["claude"] || sel["codex"] {
		if err := r.SyncProjectMCP(projectPath); err != nil {
			errors = append(errors, "project: "+err.Error())
		}
	}

	for _, target := range []struct {
		client string
		name   string
		sync   func(string) error
	}{
		{"claude", "claude-project", r.SyncClaudeProject},
		{"copilot", "vscode", r.SyncVSCode},
		{"cursor", "cursor", r.SyncCursor},
		{"kiro", "kiro", r.SyncKiro},
		{"opencode", "opencode", r.SyncOpenCodeProject},
		{"windsurf", "windsurf", r.SyncWindsurf},
		{"continue", "continue", r.SyncContinue},
	} {
		if !sel[target.client] {
			continue
		}
		if err := target.sync(projectPath); err != nil {
			errors = append(errors, target.name+": "+err.Error())
		}
	}
	return errors
}

// SyncProjectMCP writes the root .mcp.json used by Codex, Claude Code, and
// other project-scoped MCP clients.
func (r *Registry) SyncProjectMCP(projectPath string) error {
	return writeMergedMCPJSON(filepath.Join(projectPath, ".mcp.json"), "mcpServers", r.projectStdioServers(false), false)
}

// SyncClaudeProject writes Claude Code's project-scoped MCP config.
func (r *Registry) SyncClaudeProject(projectPath string) error {
	return writeMergedMCPJSON(filepath.Join(projectPath, ".claude", "mcp.json"), "mcpServers", r.projectStdioServers(false), false)
}

// SyncWindsurf writes Windsurf's project-scoped MCP config.
func (r *Registry) SyncWindsurf(projectPath string) error {
	return writeMergedMCPJSON(filepath.Join(projectPath, ".windsurf", "mcp.json"), "mcpServers", r.projectStdioServers(false), false)
}

// SyncContinue writes Continue's project-scoped MCP config.
func (r *Registry) SyncContinue(projectPath string) error {
	return writeMergedMCPJSON(filepath.Join(projectPath, ".continue", "mcp.json"), "mcpServers", r.projectStdioServers(false), false)
}

// SyncOpenCodeProject writes OpenCode's project-scoped local MCP config.
func (r *Registry) SyncOpenCodeProject(projectPath string) error {
	path := filepath.Join(projectPath, "opencode.json")
	config := make(map[string]interface{})
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		json.Unmarshal(data, &config)
	}
	if _, ok := config["$schema"]; !ok {
		config["$schema"] = "https://opencode.ai/config.json"
	}
	config["instructions"] = ensureStringListItem(config["instructions"], "AGENTS.md")

	mcp, _ := config["mcp"].(map[string]interface{})
	if mcp == nil {
		mcp = make(map[string]interface{})
	}
	removeLegacyServerKeys(mcp)
	for name, entry := range r.MCPServers {
		if !entry.Enabled || !r.IsBinaryInstalled(name) {
			continue
		}
		server := map[string]interface{}{
			"type":    "local",
			"command": opencodeCommand(entry),
		}
		if env := mcpServerEnv(name, entry); len(env) > 0 {
			server["environment"] = env
		}
		mcp[name] = server
	}
	config["mcp"] = mcp

	permission, _ := config["permission"].(map[string]interface{})
	if permission == nil {
		permission = make(map[string]interface{})
	}
	for _, key := range []string{"bash", "edit", "webfetch", "skill"} {
		if _, ok := permission[key]; !ok {
			permission[key] = "allow"
		}
	}
	config["permission"] = permission

	return writeJSONFile(path, config)
}

func (r *Registry) projectStdioServers(includeType bool) map[string]interface{} {
	servers := make(map[string]interface{})
	for name, entry := range r.MCPServers {
		if !entry.Enabled || !r.IsBinaryInstalled(name) {
			continue
		}
		servers[name] = mcpServerConfig(entry, includeType)
	}
	return servers
}

func opencodeCommand(entry MCPServerEntry) []interface{} {
	command := []interface{}{entry.Command}
	for _, arg := range entry.Args {
		command = append(command, arg)
	}
	return command
}

func ensureStringListItem(value interface{}, item string) []interface{} {
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
