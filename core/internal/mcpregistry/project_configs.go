package mcpregistry

import (
	"fmt"
	"path/filepath"
)

// syncConfiguredTargets writes MCP configs only for the AI clients the user
// selected. An empty selection writes nothing — DWYT never touches configs for
// clients that were left unchecked in setup.
func (r *Registry) syncConfiguredTargets(projectPath string, clients []string, names []string) []string {
	sel := clientSet(clients)
	errors := []string{}

	// Global (machine-level) client configs.
	if sel["claude"] {
		if err := r.syncClaudeDesktop(names); err != nil {
			errors = append(errors, "claude: "+err.Error())
		}
	}
	if sel["codex"] {
		if err := r.syncCodexGlobal(names); err != nil {
			errors = append(errors, "codex: "+err.Error())
		}
	}

	if projectPath == "" {
		return errors
	}

	// .mcp.json at the project root is read by both Claude Code and Codex.
	if sel["claude"] || sel["codex"] {
		if err := r.syncProjectMCP(projectPath, names); err != nil {
			errors = append(errors, "project: "+err.Error())
		}
	}

	for _, target := range []struct {
		client string
		name   string
		sync   func(string, []string) error
	}{
		{"claude", "claude-project", r.syncClaudeProject},
		{"copilot", "vscode", r.syncVSCode},
		{"cursor", "cursor", r.syncCursor},
		{"kiro", "kiro", r.syncKiro},
		{"opencode", "opencode", r.syncOpenCodeProject},
		{"windsurf", "windsurf", r.syncWindsurf},
		{"continue", "continue", r.syncContinue},
	} {
		if !sel[target.client] {
			continue
		}
		if err := target.sync(projectPath, names); err != nil {
			errors = append(errors, target.name+": "+err.Error())
		}
	}
	return errors
}

// SyncProjectMCP writes the root .mcp.json used by Codex, Claude Code, and
// other project-scoped MCP clients.
func (r *Registry) SyncProjectMCP(projectPath string) error {
	return r.syncProjectMCP(projectPath, nil)
}

func (r *Registry) syncProjectMCP(projectPath string, names []string) error {
	return writeMergedMCPJSON(filepath.Join(projectPath, ".mcp.json"), "mcpServers", r.projectStdioServersFor(names, false), canonicalNamesForSync(names), false)
}

// SyncClaudeProject writes Claude Code's project-scoped MCP config.
func (r *Registry) SyncClaudeProject(projectPath string) error {
	return r.syncClaudeProject(projectPath, nil)
}

func (r *Registry) syncClaudeProject(projectPath string, names []string) error {
	return writeMergedMCPJSON(filepath.Join(projectPath, ".claude", "mcp.json"), "mcpServers", r.projectStdioServersFor(names, false), canonicalNamesForSync(names), false)
}

// SyncWindsurf writes Windsurf's project-scoped MCP config.
func (r *Registry) SyncWindsurf(projectPath string) error {
	return r.syncWindsurf(projectPath, nil)
}

func (r *Registry) syncWindsurf(projectPath string, names []string) error {
	return writeMergedMCPJSON(filepath.Join(projectPath, ".windsurf", "mcp.json"), "mcpServers", r.projectStdioServersFor(names, false), canonicalNamesForSync(names), false)
}

// SyncContinue writes Continue's project-scoped MCP config.
func (r *Registry) SyncContinue(projectPath string) error {
	return r.syncContinue(projectPath, nil)
}

func (r *Registry) syncContinue(projectPath string, names []string) error {
	return writeMergedMCPJSON(filepath.Join(projectPath, ".continue", "mcp.json"), "mcpServers", r.projectStdioServersFor(names, false), canonicalNamesForSync(names), false)
}

// SyncOpenCodeProject writes OpenCode's project-scoped local MCP config.
func (r *Registry) SyncOpenCodeProject(projectPath string) error {
	return r.syncOpenCodeProject(projectPath, nil)
}

func (r *Registry) syncOpenCodeProject(projectPath string, names []string) error {
	path := filepath.Join(projectPath, "opencode.json")
	config, _, err := readJSONObject(path)
	if err != nil {
		return err
	}
	if _, ok := config["$schema"]; !ok {
		config["$schema"] = "https://opencode.ai/config.json"
	}
	config["instructions"] = ensureStringListItem(config["instructions"], "AGENTS.md")

	mcp, err := jsonObjectField(config, "mcp")
	if err != nil {
		return fmt.Errorf("invalid OpenCode config: %w", err)
	}
	removeLegacyServerKeysFor(mcp, names)
	removeCanonicalDWYTMCPEntries(mcp, canonicalNamesForSync(names))
	for name, entry := range r.entriesForSync(names) {
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
	return r.projectStdioServersFor(nil, includeType)
}

func (r *Registry) projectStdioServersFor(names []string, includeType bool) map[string]interface{} {
	servers := make(map[string]interface{})
	for name, entry := range r.entriesForSync(names) {
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
