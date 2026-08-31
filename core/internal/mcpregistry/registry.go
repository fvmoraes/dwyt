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
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	// Target is the real MCP server binary when Command is the DWYT stdio shim
	// (dwyt mcp-proxy). It is the path whose presence decides "installed", and
	// the binary the shim actually spawns. Empty for servers run directly.
	Target    string `json:"target,omitempty"`
	Port      int    `json:"port,omitempty"`
	HealthURL string `json:"healthURL,omitempty"`
	Enabled   bool   `json:"enabled"`
}

type Registry struct {
	MCPServers map[string]MCPServerEntry `json:"mcpServers"`
	path       string
	// migrated is set by Load when the registry rewrites legacy entries to
	// their canonical form (legacy keys removed, legacy command rewritten).
	// Handlers expose it so the UI can tell whether a reconfigure was a no-op
	// or actually healed an old install.
	migrated bool
}

// MigrationPerformed reports whether the most recent Load rewrote any
// legacy entries to their canonical form.
func (r *Registry) MigrationPerformed() bool {
	return r.migrated
}

func dwytHome() string {
	if h := os.Getenv("DWYT_HOME"); h != "" {
		return h
	}
	// Must mirror detect.Detect(): on Windows DWYT lives under %APPDATA%\dwyt,
	// not ~/.dwyt. If these diverge, the MCP command paths written to client
	// configs point at a directory where the binaries were never installed,
	// and the client fails to launch the server ("não é reconhecido como um
	// comando ...").
	if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		if appData == "" {
			home, _ := os.UserHomeDir()
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(appData, "dwyt")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".dwyt")
}

// exeName returns the platform-correct executable basename. On Windows the MCP
// binaries are installed with a ".exe" suffix, so the registry must store and
// look them up by that exact name — otherwise the configured command path is
// missing the extension and the client cannot spawn the process.
func exeName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
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
	r.migrated = false

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
	dwytShim := filepath.Join(binDir, exeName("dwyt"))
	codebaseTarget := filepath.Join(binDir, exeName("codebase-memory-mcp"))

	// Codebase runs through the DWYT stdio shim so its tool calls are countable
	// by the dashboard regardless of the IDE/harness; Obsidian is DWYT's own MCP
	// server (already dashboard-aware) and is invoked via the main `dwyt`
	// binary with the `obsidian-mcp` subcommand. This means a Windows install
	// no longer needs to copy/rename `dwyt.exe` to `dwyt-obsidian-mcp.exe` —
	// the canonical command always resolves to the real, installed DWYT.
	//
	// Safety net for codebase: the shim only routes when the dwyt binary
	// actually exists in the bin dir. If it does not (an unusual partial
	// install), codebase falls back to running the real binary directly — no
	// counting, but it still works. This is self-correcting: once the shim
	// appears, the next Load heals the entry to the proxied form (and
	// vice-versa).
	codebaseEntry := MCPServerEntry{
		Command:   codebaseTarget,
		Port:      9749,
		HealthURL: "/health",
		Enabled:   true,
	}
	if fileExists(dwytShim) {
		codebaseEntry = MCPServerEntry{
			Command:   dwytShim,
			Args:      []string{"mcp-proxy", "--target", codebaseTarget, "--name", "codebase"},
			Target:    codebaseTarget,
			Port:      9749,
			HealthURL: "/health",
			Enabled:   true,
		}
	}
	canonical := map[string]MCPServerEntry{
		"codebase": codebaseEntry,
		"obsidian": {
			Command: dwytShim,
			Args:    []string{"obsidian-mcp"},
			Enabled: true,
		},
	}

	for name, want := range canonical {
		existing, exists := r.MCPServers[name]
		if !exists {
			r.MCPServers[name] = want
			migrated = true
			continue
		}
		// Heal stale wiring written by older builds (e.g. codebase stored as
		// the raw binary with no shim, or — for obsidian — an entry pointing
		// at the legacy `dwyt-obsidian-mcp` copy that newer installs no longer
		// create). Only the command wiring is corrected; the user-tunable
		// Enabled flag is preserved.
		existing = migrateObsidianCommand(existing)
		// Always write the migrated entry back: the migration runs against a
		// local copy of the struct, so even when the result already matches
		// the canonical form we still need to persist it (e.g. the
		// `obsidian-mcp` subcommand args were rewritten by the migrator).
		if existing.Command != want.Command ||
			existing.Target != want.Target ||
			!equalArgs(existing.Args, want.Args) {
			healed := want
			healed.Enabled = existing.Enabled
			r.MCPServers[name] = healed
			migrated = true
		} else if existing.Command != r.MCPServers[name].Command ||
			!equalArgs(existing.Args, r.MCPServers[name].Args) {
			r.MCPServers[name] = existing
			migrated = true
		}
	}

	if migrated {
		r.migrated = true
		if err := r.Save(); err != nil {
			log.Warn("mcp registry migration save failed", log.Fields{"error": err.Error()})
		}
	}

	return r, nil
}

// migrateObsidianCommand rewrites a legacy `dwyt-obsidian-mcp`/`dwyt-obsidian`
// command to the canonical `dwyt obsidian-mcp` form. It is a no-op when the
// entry already uses the canonical command or has no command at all.
func migrateObsidianCommand(entry MCPServerEntry) MCPServerEntry {
	base := strings.ToLower(filepath.Base(entry.Command))
	if base != "dwyt-obsidian-mcp" && base != "dwyt-obsidian" && base != "obsidian-mcp" {
		return entry
	}
	binDir := filepath.Join(dwytHome(), "bin")
	entry.Command = filepath.Join(binDir, exeName("dwyt"))
	entry.Args = []string{"obsidian-mcp"}
	entry.Target = ""
	return entry
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
	// For shimmed servers the real binary is Target (Command is the dwyt shim,
	// which always exists). "Installed" must reflect the real server's presence
	// so a config is never written pointing the shim at a missing target.
	path := entry.Command
	if entry.Target != "" {
		path = entry.Target
	}
	if _, err := os.Stat(path); err == nil {
		return true
	}
	// On Windows tolerate an entry whose command lacks the ".exe" suffix, so a
	// legacy registry still resolves the real binary on disk.
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(path), ".exe") {
		if _, err := os.Stat(path + ".exe"); err == nil {
			return true
		}
	}
	// The Obsidian MCP runs through the main `dwyt` binary via the
	// `obsidian-mcp` subcommand, so the canonical entry always reports as
	// installed as long as the main `dwyt` binary is present in the bin dir.
	// A legacy `dwyt-obsidian-mcp` copy (from older installs) also satisfies
	// the check so the registry does not flip back to "not installed" while a
	// migration is still in flight.
	if isObsidianEntry(name, entry) {
		if fileExists(filepath.Join(dwytHome(), "bin", exeName("dwyt"))) {
			return true
		}
		if fileExists(filepath.Join(dwytHome(), "bin", exeName("dwyt-obsidian-mcp"))) {
			return true
		}
	}
	return false
}

// equalArgs reports whether two argument slices are element-wise equal.
func equalArgs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// fileExists reports whether path exists on disk, tolerating a missing ".exe"
// suffix on Windows so a shim recorded without the extension still resolves.
func fileExists(path string) bool {
	if _, err := os.Stat(path); err == nil {
		return true
	}
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(path), ".exe") {
		if _, err := os.Stat(path + ".exe"); err == nil {
			return true
		}
	}
	return false
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
		config := map[string]interface{}{
			"command": entry.Command,
			"args":    args,
		}
		if env := mcpServerEnv(name, entry); len(env) > 0 {
			config["env"] = env
		}
		claudeConfig[name] = config
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
	servers := make(map[string]interface{})
	for name, entry := range r.MCPServers {
		if !entry.Enabled || !r.IsBinaryInstalled(name) {
			continue
		}
		servers[name] = mcpServerConfig(entry, true)
	}
	return writeMergedMCPJSON(filepath.Join(projectPath, ".vscode", "mcp.json"), "servers", servers, true)
}

// SyncCursor writes project-scoped MCP config for Cursor.
func (r *Registry) SyncCursor(projectPath string) error {
	servers := make(map[string]interface{})
	for name, entry := range r.MCPServers {
		if !entry.Enabled || !r.IsBinaryInstalled(name) {
			continue
		}
		servers[name] = mcpServerConfig(entry, false)
	}
	return writeMergedMCPJSON(filepath.Join(projectPath, ".cursor", "mcp.json"), "mcpServers", servers, false)
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
	if err := writeMergedMCPJSON(filepath.Join(projectPath, ".kiro", "settings", "mcp.json"), "mcpServers", servers, false); err != nil {
		return err
	}
	return writeMergedMCPJSON(filepath.Join(projectPath, ".kiro", "mcp.json"), "mcpServers", servers, false)
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
		if env := mcpServerEnv(name, entry); len(env) > 0 {
			b.WriteString(fmt.Sprintf("[mcp_servers.%s.env]\n", name))
			for key, value := range env {
				b.WriteString(fmt.Sprintf("%s = %q\n", key, value))
			}
			b.WriteString("\n")
		}
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
		"type":    "stdio",
	}
	_ = includeType
	if env := mcpServerEnv("", entry); len(env) > 0 {
		cfg["env"] = env
	}
	return cfg
}

func mcpServerEnv(name string, entry MCPServerEntry) map[string]interface{} {
	env := map[string]interface{}{}
	if isCodebaseEntry(name, entry) {
		env["CBM_CACHE_DIR"] = filepath.Join(dwytHome(), "codebase")
	}
	if isObsidianEntry(name, entry) {
		env["DWYT_API_URL"] = "http://localhost:2737/api"
	}
	return env
}

func isCodebaseEntry(name string, entry MCPServerEntry) bool {
	if name == "codebase" {
		return true
	}
	// With the stdio shim the codebase binary appears as Target, not Command.
	if strings.Contains(filepath.Base(entry.Target), "codebase-memory-mcp") {
		return true
	}
	return strings.Contains(filepath.Base(entry.Command), "codebase-memory-mcp")
}

func isObsidianEntry(name string, entry MCPServerEntry) bool {
	if name == "obsidian" {
		return true
	}
	// Logical name fallback for legacy keys being migrated.
	if name == "dwyt-obsidian" || name == "obsidian-mcp" {
		return true
	}
	// The canonical command runs `dwyt obsidian-mcp` — match by args.
	for _, a := range entry.Args {
		if a == "obsidian-mcp" {
			return true
		}
	}
	// Tolerate a registry that still points at the legacy `dwyt-obsidian-mcp`
	// copy while a migration is pending.
	base := strings.ToLower(filepath.Base(entry.Command))
	return strings.Contains(base, "dwyt-obsidian-mcp") || base == "dwyt-obsidian"
}

func writeJSONFile(path string, value interface{}) error {
	os.MkdirAll(filepath.Dir(path), 0755)
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}

func writeMergedMCPJSON(path, serverKey string, managedServers map[string]interface{}, ensureInputs bool) error {
	config := make(map[string]interface{})
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		json.Unmarshal(data, &config)
	}
	servers, _ := config[serverKey].(map[string]interface{})
	if servers == nil {
		servers = make(map[string]interface{})
	}
	removeLegacyServerKeys(servers)
	for name, entry := range managedServers {
		servers[name] = entry
	}
	config[serverKey] = servers
	if ensureInputs {
		if _, ok := config["inputs"]; !ok {
			config["inputs"] = []interface{}{}
		}
		delete(config, "mcpServers")
	}
	return writeJSONFile(path, config)
}

func removeLegacyServerKeys(servers map[string]interface{}) {
	for _, key := range []string{"dwyt", "dwyt-codebase", "dwyt-obsidian", "obsidian-mcp"} {
		delete(servers, key)
	}
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

// ConfigureMCPByName writes MCP configuration for a specific MCP server only,
// limited to the AI clients the user selected.
func (r *Registry) ConfigureMCPByName(projectPath, name string, clients []string) error {
	if _, ok := r.MCPServers[name]; !ok {
		return fmt.Errorf("mcp server %s not found in registry", name)
	}
	if err := r.Save(); err != nil {
		return fmt.Errorf("mcp registry save failed: %w", err)
	}

	errors := r.syncConfiguredTargets(projectPath, clients)
	if len(errors) > 0 {
		return fmt.Errorf("sync errors: %v", errors)
	}
	log.Info("mcp configs synced for server", log.Fields{"project": projectPath, "server": name})
	return nil
}

// ConfigureMCP writes MCP configurations to the AI clients the user selected.
func (r *Registry) ConfigureMCP(projectPath string, clients []string) error {
	// Save the updated registry first
	if err := r.Save(); err != nil {
		return fmt.Errorf("mcp registry save failed: %w", err)
	}

	// Save a backup before modifying external configs
	backup := make(map[string]MCPServerEntry, len(r.MCPServers))
	for k, v := range r.MCPServers {
		backup[k] = v
	}

	errors := r.syncConfiguredTargets(projectPath, clients)

	if len(errors) > 0 {
		// Rollback: restore registry to pre-sync state
		r.MCPServers = backup
		r.Save()
		return fmt.Errorf("sync errors (registry rolled back): %v", errors)
	}

	log.Info("mcp configs synced", log.Fields{"project": projectPath})
	return nil
}

// SyncAll syncs MCP config for the selected agents using the given project path.
func (r *Registry) SyncAll(projectPath string, clients []string) error {
	return r.ConfigureMCP(projectPath, clients)
}

// clientSet builds a lookup set from a list of selected client ids, trimming
// blanks. An empty input yields an empty set, so nothing gets synced.
func clientSet(clients []string) map[string]bool {
	set := make(map[string]bool, len(clients))
	for _, c := range clients {
		c = strings.TrimSpace(c)
		if c != "" {
			set[c] = true
		}
	}
	return set
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
