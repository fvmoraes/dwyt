package mcpregistry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/fvmoraes/dwyt/internal/log"
)

type MCPServerEntry struct {
	Command   string   `json:"command"`
	Args      []string `json:"args,omitempty"`
	Port      int      `json:"port,omitempty"`
	HealthURL string   `json:"healthURL,omitempty"`
	Enabled   bool     `json:"enabled"`
}

type Registry struct {
	MCPServers map[string]MCPServerEntry `json:"mcpServers"`
	path       string
}

func dwytHome() string {
	if h := os.Getenv("DWYT_HOME"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".dwyt")
}

func configDir() string {
	d := filepath.Join(dwytHome(), "config")
	os.MkdirAll(d, 0755)
	return d
}

func Load() (*Registry, error) {
	path := filepath.Join(configDir(), "mcp-registry.json")
	r := &Registry{
		MCPServers: make(map[string]MCPServerEntry),
		path:       path,
	}

	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, r)
	}

	migrated := false
	legacyNames := map[string]string{
		"dwyt":          "codebase",
		"dwyt-codebase": "codebase",
		"dwyt-obsidian": "obsidian",
		"obsidian-mcp":  "obsidian",
	}
	for legacy, canonical := range legacyNames {
		if entry, ok := r.MCPServers[legacy]; ok {
			if _, exists := r.MCPServers[canonical]; !exists {
				r.MCPServers[canonical] = entry
			}
			delete(r.MCPServers, legacy)
			migrated = true
		}
	}

	// Ensure default entries
	binDir := filepath.Join(dwytHome(), "bin")
	defaults := map[string]MCPServerEntry{
		"codebase": {
			Command:   filepath.Join(binDir, "codebase-memory-mcp"),
			Port:      9749,
			HealthURL: "/health",
			Enabled:   true,
		},
		"obsidian": {
			Command: filepath.Join(binDir, "dwyt-obsidian-mcp"),
			Enabled: true,
		},
	}

	for name, entry := range defaults {
		if _, exists := r.MCPServers[name]; !exists {
			r.MCPServers[name] = entry
			migrated = true
		}
	}

	if migrated {
		if err := r.Save(); err != nil {
			log.Warn("mcp registry migration save failed", log.Fields{"error": err.Error()})
		}
	}

	return r, nil
}

func (r *Registry) Save() error {
	if r.path == "" {
		r.path = filepath.Join(configDir(), "mcp-registry.json")
	}
	os.MkdirAll(filepath.Dir(r.path), 0755)
	data, _ := json.MarshalIndent(r, "", "  ")
	return os.WriteFile(r.path, data, 0644)
}

func (r *Registry) Get(name string) (MCPServerEntry, bool) {
	entry, ok := r.MCPServers[name]
	return entry, ok
}

func (r *Registry) Set(name string, entry MCPServerEntry) {
	r.MCPServers[name] = entry
}

func (r *Registry) IsBinaryInstalled(name string) bool {
	entry, ok := r.MCPServers[name]
	if !ok {
		return false
	}
	_, err := os.Stat(entry.Command)
	return err == nil
}

// SyncClaudeDesktop writes the Claude Desktop MCP config.
func (r *Registry) SyncClaudeDesktop() error {
	claudeConfig := make(map[string]interface{})

	for name, entry := range r.MCPServers {
		if !entry.Enabled || !r.IsBinaryInstalled(name) {
			continue
		}
		args := entry.Args
		if args == nil {
			args = []string{}
		}
		claudeConfig[name] = map[string]interface{}{
			"command": entry.Command,
			"args":    args,
		}
	}

	if len(claudeConfig) == 0 {
		return nil
	}

	home, _ := os.UserHomeDir()
	var configPath string
	switch runtime.GOOS {
	case "darwin":
		configPath = filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")
	case "windows":
		configPath = filepath.Join(home, "AppData", "Roaming", "Claude", "claude_desktop_config.json")
	default:
		configPath = filepath.Join(home, ".config", "claude-desktop", "claude_desktop_config.json")
	}

	os.MkdirAll(filepath.Dir(configPath), 0755)

	// Read existing config and merge
	existing := make(map[string]interface{})
	if data, err := os.ReadFile(configPath); err == nil {
		json.Unmarshal(data, &existing)
	}

	if _, ok := existing["mcpServers"]; !ok {
		existing["mcpServers"] = make(map[string]interface{})
	}
	servers, ok := existing["mcpServers"].(map[string]interface{})
	if !ok {
		servers = make(map[string]interface{})
		existing["mcpServers"] = servers
	}
	for name, entry := range claudeConfig {
		servers[name] = entry
	}

	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0644)
}

// SyncVSCode writes or updates .vscode/mcp.json in the project directory.
func (r *Registry) SyncVSCode(projectPath string) error {
	vscodeDir := filepath.Join(projectPath, ".vscode")
	os.MkdirAll(vscodeDir, 0755)
	mcpPath := filepath.Join(vscodeDir, "mcp.json")

	servers := make(map[string]interface{})
	for name, entry := range r.MCPServers {
		if !entry.Enabled || !r.IsBinaryInstalled(name) {
			continue
		}
		servers[name] = mcpServerConfig(entry, true)
	}

	config := map[string]interface{}{
		"inputs":  []interface{}{},
		"servers": servers,
	}

	data, _ := json.MarshalIndent(config, "", "  ")
	return os.WriteFile(mcpPath, data, 0644)
}

// SyncCursor writes project-scoped MCP config for Cursor.
func (r *Registry) SyncCursor(projectPath string) error {
	mcpPath := filepath.Join(projectPath, ".cursor", "mcp.json")
	servers := make(map[string]interface{})
	for name, entry := range r.MCPServers {
		if !entry.Enabled || !r.IsBinaryInstalled(name) {
			continue
		}
		servers[name] = mcpServerConfig(entry, false)
	}
	return writeJSONFile(mcpPath, map[string]interface{}{"mcpServers": servers})
}

// SyncKiro writes both current and legacy Kiro workspace MCP config paths.
func (r *Registry) SyncKiro(projectPath string) error {
	servers := make(map[string]interface{})
	for name, entry := range r.MCPServers {
		if !entry.Enabled || !r.IsBinaryInstalled(name) {
			continue
		}
		servers[name] = mcpServerConfig(entry, false)
	}
	config := map[string]interface{}{"mcpServers": servers}
	if err := writeJSONFile(filepath.Join(projectPath, ".kiro", "settings", "mcp.json"), config); err != nil {
		return err
	}
	return writeJSONFile(filepath.Join(projectPath, ".kiro", "mcp.json"), config)
}

// SyncCodexGlobal writes MCP servers to Codex's shared config file.
func (r *Registry) SyncCodexGlobal() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	configPath := filepath.Join(home, ".codex", "config.toml")
	data, _ := os.ReadFile(configPath)
	original := string(data)
	content := removeManagedBlock(original, "# dwyt:mcp:start", "# dwyt:mcp:end")

	block := r.codexTOMLBlock()
	if block == "" {
		if content == original {
			return nil
		}
		os.MkdirAll(filepath.Dir(configPath), 0755)
		return os.WriteFile(configPath, []byte(content), 0644)
	}
	if strings.TrimSpace(content) != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if strings.TrimSpace(content) != "" {
		content += "\n"
	}
	content += block

	os.MkdirAll(filepath.Dir(configPath), 0755)
	return os.WriteFile(configPath, []byte(content), 0644)
}

func (r *Registry) codexTOMLBlock() string {
	var b strings.Builder
	b.WriteString("# dwyt:mcp:start\n")
	wrote := false
	for _, name := range []string{"codebase", "obsidian"} {
		entry, ok := r.MCPServers[name]
		if !ok || !entry.Enabled || !r.IsBinaryInstalled(name) {
			continue
		}
		wrote = true
		b.WriteString(fmt.Sprintf("[mcp_servers.%s]\n", name))
		b.WriteString(fmt.Sprintf("command = %q\n", entry.Command))
		b.WriteString("args = [")
		for i, arg := range entry.Args {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(fmt.Sprintf("%q", arg))
		}
		b.WriteString("]\n")
		b.WriteString("startup_timeout_sec = 20\n")
		b.WriteString("tool_timeout_sec = 120\n\n")
	}
	b.WriteString("# dwyt:mcp:end\n")
	if !wrote {
		return ""
	}
	return b.String()
}

func mcpServerConfig(entry MCPServerEntry, includeType bool) map[string]interface{} {
	args := entry.Args
	if args == nil {
		args = []string{}
	}
	cfg := map[string]interface{}{
		"command": entry.Command,
		"args":    args,
	}
	if includeType {
		cfg["type"] = "stdio"
	}
	return cfg
}

func writeJSONFile(path string, value interface{}) error {
	os.MkdirAll(filepath.Dir(path), 0755)
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}

func removeManagedBlock(content, start, end string) string {
	for {
		startIdx := strings.Index(content, start)
		if startIdx == -1 {
			return content
		}
		endIdx := strings.Index(content[startIdx:], end)
		if endIdx == -1 {
			return content
		}
		endPos := startIdx + endIdx + len(end)
		if endPos < len(content) && content[endPos] == '\n' {
			endPos++
		}
		if startIdx > 0 && content[startIdx-1] == '\n' {
			startIdx--
		}
		content = content[:startIdx] + content[endPos:]
	}
}

// ConfigureMCPByName writes MCP configuration for a specific MCP server only.
func (r *Registry) ConfigureMCPByName(projectPath, name string) error {
	if _, ok := r.MCPServers[name]; !ok {
		return fmt.Errorf("mcp server %s not found in registry", name)
	}
	if err := r.Save(); err != nil {
		return fmt.Errorf("mcp registry save failed: %w", err)
	}

	errors := r.syncConfiguredTargets(projectPath)
	if len(errors) > 0 {
		return fmt.Errorf("sync errors: %v", errors)
	}
	log.Info("mcp configs synced for server", log.Fields{"project": projectPath, "server": name})
	return nil
}

// ConfigureMCP writes MCP configurations to all supported agents.
func (r *Registry) ConfigureMCP(projectPath string) error {
	// Save the updated registry first
	if err := r.Save(); err != nil {
		return fmt.Errorf("mcp registry save failed: %w", err)
	}

	// Save a backup before modifying external configs
	backup := make(map[string]MCPServerEntry, len(r.MCPServers))
	for k, v := range r.MCPServers {
		backup[k] = v
	}

	errors := r.syncConfiguredTargets(projectPath)

	if len(errors) > 0 {
		// Rollback: restore registry to pre-sync state
		r.MCPServers = backup
		r.Save()
		return fmt.Errorf("sync errors (registry rolled back): %v", errors)
	}

	log.Info("mcp configs synced", log.Fields{"project": projectPath})
	return nil
}

func (r *Registry) syncConfiguredTargets(projectPath string) []string {
	errors := []string{}
	if err := r.SyncClaudeDesktop(); err != nil {
		errors = append(errors, "claude: "+err.Error())
	}
	if err := r.SyncCodexGlobal(); err != nil {
		errors = append(errors, "codex: "+err.Error())
	}
	if projectPath != "" {
		if err := r.SyncVSCode(projectPath); err != nil {
			errors = append(errors, "vscode: "+err.Error())
		}
		if err := r.SyncCursor(projectPath); err != nil {
			errors = append(errors, "cursor: "+err.Error())
		}
		if err := r.SyncKiro(projectPath); err != nil {
			errors = append(errors, "kiro: "+err.Error())
		}
	}
	return errors
}

// SyncAll syncs MCP config for all agents using the given project path.
func (r *Registry) SyncAll(projectPath string) error {
	return r.ConfigureMCP(projectPath)
}

// Toggle enables or disables an MCP server by name.
func (r *Registry) Toggle(name string, enabled bool) error {
	entry, ok := r.MCPServers[name]
	if !ok {
		return fmt.Errorf("mcp server %s not found", name)
	}
	entry.Enabled = enabled
	r.MCPServers[name] = entry
	return r.Save()
}
