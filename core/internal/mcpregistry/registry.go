package mcpregistry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/fvmoraes/dwyt/internal/log"
)

type MCPServerEntry struct {
	Command    string   `json:"command"`
	Args       []string `json:"args,omitempty"`
	Port       int      `json:"port,omitempty"`
	HealthURL  string   `json:"healthURL,omitempty"`
	Enabled    bool     `json:"enabled"`
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

	// Ensure default entries
	binDir := filepath.Join(dwytHome(), "bin")
	defaults := map[string]MCPServerEntry{
		"dwyt-codebase": {
			Command:   filepath.Join(binDir, "codebase-memory-mcp"),
			Port:      9749,
			HealthURL: "/health",
			Enabled:   true,
		},
		"dwyt-obsidian": {
			Command:   filepath.Join(binDir, "dwyt-obsidian-mcp"),
			Enabled:   true,
		},
	}

	for name, entry := range defaults {
		if _, exists := r.MCPServers[name]; !exists {
			r.MCPServers[name] = entry
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
	servers := existing["mcpServers"].(map[string]interface{})
	for name, entry := range claudeConfig {
		servers[name] = entry
	}
	existing["mcpServers"] = servers

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
		args := entry.Args
		if args == nil {
			args = []string{}
		}
		servers[name] = map[string]interface{}{
			"command": entry.Command,
			"args":    args,
		}
	}

	config := map[string]interface{}{
		"inputs":     []interface{}{},
		"mcpServers": servers,
	}

	data, _ := json.MarshalIndent(config, "", "  ")
	return os.WriteFile(mcpPath, data, 0644)
}

// ConfigureMCP writes MCP configurations to all supported agents.
func (r *Registry) ConfigureMCP(projectPath string) error {
	// Save the updated registry first
	if err := r.Save(); err != nil {
		return fmt.Errorf("mcp registry save failed: %w", err)
	}

	// Sync to global agent configs
	errors := []string{}
	if err := r.SyncClaudeDesktop(); err != nil {
		errors = append(errors, "claude: "+err.Error())
	}

	// Sync per-project MCP configs
	if projectPath != "" {
		if err := r.SyncVSCode(projectPath); err != nil {
			errors = append(errors, "vscode: "+err.Error())
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("sync errors: %v", errors)
	}

	log.Info("mcp configs synced", log.Fields{"project": projectPath})
	return nil
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
