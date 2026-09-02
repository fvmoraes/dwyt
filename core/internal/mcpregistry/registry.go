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

func configDir() (string, error) {
	d := filepath.Join(dwytHome(), "config")
	if err := os.MkdirAll(d, 0755); err != nil {
		return "", fmt.Errorf("create MCP registry config directory %s: %w", d, err)
	}
	return d, nil
}

func Load() (*Registry, error) {
	dir, err := configDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "mcp-registry.json")
	r := &Registry{
		MCPServers: make(map[string]MCPServerEntry),
		path:       path,
	}
	r.migrated = false

	if data, readErr := os.ReadFile(path); readErr == nil {
		if err := json.Unmarshal(data, r); err != nil {
			return nil, fmt.Errorf("read MCP registry %s: invalid JSON: %w", path, err)
		}
	} else if !os.IsNotExist(readErr) {
		return nil, fmt.Errorf("read MCP registry %s: %w", path, readErr)
	}
	// A syntactically valid `{ "mcpServers": null }` is still usable: treat
	// it as an empty registry so the canonical entries can be seeded below.
	if r.MCPServers == nil {
		r.MCPServers = make(map[string]MCPServerEntry)
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
	defaultCodebaseTarget := filepath.Join(binDir, exeName("codebase-memory-mcp"))
	codebaseTarget := defaultCodebaseTarget
	// A configured external source is stored as Target while Command remains
	// DWYT's accounting proxy. Preserve it on Load instead of "healing" it
	// back to the bundled binary; setup explicitly switches this field when
	// ownership changes back to DWYT-managed mode.
	if existing, ok := r.MCPServers["codebase"]; ok && strings.TrimSpace(existing.Target) != "" {
		codebaseTarget = existing.Target
	}

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
	codebaseEntry := newCodebaseEntry(dwytShim, codebaseTarget)
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
			return nil, fmt.Errorf("save migrated MCP registry: %w", err)
		}
	}

	return r, nil
}

func newCodebaseEntry(dwytShim, target string) MCPServerEntry {
	entry := MCPServerEntry{
		Command:   target,
		Port:      9749,
		HealthURL: "/health",
		Enabled:   true,
	}
	if fileExists(dwytShim) {
		entry = MCPServerEntry{
			Command:   dwytShim,
			Args:      []string{"mcp-proxy", "--target", target, "--name", "codebase"},
			Target:    target,
			Port:      9749,
			HealthURL: "/health",
			Enabled:   true,
		}
	}
	return entry
}

// SetCodebaseTarget changes only DWYT's registry wiring so the MCP accounting
// proxy launches the selected Codebase executable. It never copies, updates,
// deletes, or otherwise changes target itself; that target may be a user-owned
// external/local installation.
func (r *Registry) SetCodebaseTarget(target string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return fmt.Errorf("codebase target path is required")
	}
	if r.MCPServers == nil {
		r.MCPServers = make(map[string]MCPServerEntry)
	}
	binDir := filepath.Join(dwytHome(), "bin")
	entry := newCodebaseEntry(filepath.Join(binDir, exeName("dwyt")), target)
	if existing, ok := r.MCPServers["codebase"]; ok {
		entry.Enabled = existing.Enabled
	}
	r.MCPServers["codebase"] = entry
	return r.Save()
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
		dir, err := configDir()
		if err != nil {
			return err
		}
		r.path = filepath.Join(dir, "mcp-registry.json")
	}
	if err := os.MkdirAll(filepath.Dir(r.path), 0755); err != nil {
		return fmt.Errorf("create MCP registry directory %s: %w", filepath.Dir(r.path), err)
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("encode MCP registry: %w", err)
	}
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

// canonicalDWYTMCPNames are the only server keys DWYT owns in client config
// files. Sync may remove these stale entries when they are disabled or no
// longer installed, but it must never remove user-managed MCP servers.
var canonicalDWYTMCPNames = []string{"codebase", "obsidian"}

func isCanonicalDWYTMCP(name string) bool {
	for _, candidate := range canonicalDWYTMCPNames {
		if name == candidate {
			return true
		}
	}
	return false
}

// entriesForSync returns enabled, installed registry entries in the requested
// scope. A nil/empty scope means a full sync; a non-empty scope is used by a
// card-level reconfigure action and prevents it from changing other servers.
func (r *Registry) entriesForSync(names []string) map[string]MCPServerEntry {
	entries := make(map[string]MCPServerEntry)
	if len(names) == 0 {
		for name, entry := range r.MCPServers {
			if entry.Enabled && r.IsBinaryInstalled(name) {
				entries[name] = entry
			}
		}
		return entries
	}
	for _, name := range names {
		entry, ok := r.MCPServers[name]
		if ok && entry.Enabled && r.IsBinaryInstalled(name) {
			entries[name] = entry
		}
	}
	return entries
}

// canonicalNamesForSync tells a writer which DWYT-owned keys it may reconcile.
// Full sync reconciles both canonical keys; a scoped sync may only touch the
// named canonical key, preserving the other card's configuration verbatim.
func canonicalNamesForSync(names []string) []string {
	if len(names) == 0 {
		return append([]string(nil), canonicalDWYTMCPNames...)
	}
	result := make([]string, 0, len(names))
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if isCanonicalDWYTMCP(name) && !seen[name] {
			result = append(result, name)
			seen[name] = true
		}
	}
	return result
}

func removeCanonicalDWYTMCPEntries(servers map[string]interface{}, names []string) {
	for _, name := range names {
		delete(servers, name)
	}
}

func removeLegacyServerKeysFor(servers map[string]interface{}, names []string) {
	legacyByCanonical := map[string][]string{
		"codebase": {"dwyt", "dwyt-codebase"},
		"obsidian": {"dwyt-obsidian", "obsidian-mcp"},
	}
	for _, canonical := range canonicalNamesForSync(names) {
		for _, legacy := range legacyByCanonical[canonical] {
			delete(servers, legacy)
		}
	}
}

// SyncClaudeDesktop writes the Claude Desktop MCP config.
func (r *Registry) SyncClaudeDesktop() error { return r.syncClaudeDesktop(nil) }

func (r *Registry) syncClaudeDesktop(names []string) error {
	claudeConfig := make(map[string]interface{})
	for name, entry := range r.entriesForSync(names) {
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

	// Read existing config and merge. If there are no active DWYT entries and
	// no existing file, leave the user's filesystem untouched.
	existing, exists, err := readJSONObject(configPath)
	if err != nil {
		return fmt.Errorf("read Claude Desktop MCP config: %w", err)
	}
	if !exists && len(claudeConfig) == 0 {
		return nil
	}

	servers, err := jsonObjectField(existing, "mcpServers")
	if err != nil {
		return fmt.Errorf("invalid Claude Desktop MCP config: %w", err)
	}
	existing["mcpServers"] = servers
	removeLegacyServerKeysFor(servers, names)
	removeCanonicalDWYTMCPEntries(servers, canonicalNamesForSync(names))
	for name, entry := range claudeConfig {
		servers[name] = entry
	}

	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("create Claude Desktop config directory %s: %w", filepath.Dir(configPath), err)
	}
	return os.WriteFile(configPath, data, 0644)
}

// SyncVSCode writes or updates .vscode/mcp.json in the project directory.
func (r *Registry) SyncVSCode(projectPath string) error { return r.syncVSCode(projectPath, nil) }

func (r *Registry) syncVSCode(projectPath string, names []string) error {
	return writeMergedMCPJSON(filepath.Join(projectPath, ".vscode", "mcp.json"), "servers", r.projectStdioServersFor(names, true), canonicalNamesForSync(names), true)
}

// SyncCursor writes project-scoped MCP config for Cursor.
func (r *Registry) SyncCursor(projectPath string) error { return r.syncCursor(projectPath, nil) }

func (r *Registry) syncCursor(projectPath string, names []string) error {
	return writeMergedMCPJSON(filepath.Join(projectPath, ".cursor", "mcp.json"), "mcpServers", r.projectStdioServersFor(names, false), canonicalNamesForSync(names), false)
}

// SyncKiro writes both current and legacy Kiro workspace MCP config paths.
func (r *Registry) SyncKiro(projectPath string) error { return r.syncKiro(projectPath, nil) }

func (r *Registry) syncKiro(projectPath string, names []string) error {
	servers := r.projectStdioServersFor(names, false)
	managed := canonicalNamesForSync(names)
	if err := writeMergedMCPJSON(filepath.Join(projectPath, ".kiro", "settings", "mcp.json"), "mcpServers", servers, managed, false); err != nil {
		return err
	}
	return writeMergedMCPJSON(filepath.Join(projectPath, ".kiro", "mcp.json"), "mcpServers", servers, managed, false)
}

// SyncCodexGlobal writes MCP servers to Codex's shared config file.
func (r *Registry) SyncCodexGlobal() error { return r.syncCodexGlobal(nil) }

// syncCodexGlobal reconciles the whole DWYT-owned block for a full sync. A
// card-level sync changes only its requested table, retaining the other DWYT
// table exactly as it was, so reconfiguring Obsidian never adds or rewrites
// Codebase behind the user's back.
func (r *Registry) syncCodexGlobal(names []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	configPath := filepath.Join(home, ".codex", "config.toml")
	data, readErr := os.ReadFile(configPath)
	if readErr != nil && !os.IsNotExist(readErr) {
		return fmt.Errorf("read Codex MCP config %s: %w", configPath, readErr)
	}
	original := string(data)
	if len(names) > 0 {
		return r.syncScopedCodexGlobal(configPath, original, names)
	}
	lineEnding := preferredLineEnding(original)
	content := removeManagedBlock(original, "# dwyt:mcp:start", "# dwyt:mcp:end")

	block := r.codexTOMLBlock()
	if block == "" {
		if content == original {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
			return fmt.Errorf("create Codex config directory %s: %w", filepath.Dir(configPath), err)
		}
		return os.WriteFile(configPath, []byte(content), 0644)
	}
	if strings.TrimSpace(content) != "" && !strings.HasSuffix(content, "\n") {
		content += lineEnding
	}
	if strings.TrimSpace(content) != "" {
		content += lineEnding
	}
	content += withLineEnding(block, lineEnding)

	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("create Codex config directory %s: %w", filepath.Dir(configPath), err)
	}
	return os.WriteFile(configPath, []byte(content), 0644)
}

func (r *Registry) codexTOMLBlock() string {
	tables := r.codexTOMLTables(nil)
	if tables == "" {
		return ""
	}
	return "# dwyt:mcp:start\n" + tables + "# dwyt:mcp:end\n"
}

func (r *Registry) codexTOMLTables(names []string) string {
	var b strings.Builder
	entries := r.entriesForSync(names)
	for _, name := range canonicalNamesForSync(names) {
		entry, ok := entries[name]
		if !ok {
			continue
		}
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
	return b.String()
}

func (r *Registry) syncScopedCodexGlobal(configPath, original string, names []string) error {
	startMarker := "# dwyt:mcp:start"
	endMarker := "# dwyt:mcp:end"
	lineEnding := preferredLineEnding(original)
	start := strings.Index(original, startMarker)
	end := -1
	if start >= 0 {
		if relative := strings.Index(original[start:], endMarker); relative >= 0 {
			end = start + relative + len(endMarker)
			end = consumeLineEnding(original, end)
		}
	}

	// No previous DWYT block: only create one when the requested server is
	// currently enabled and installed. A disabled card must not create noise.
	if start < 0 || end < 0 {
		tables := r.codexTOMLTables(names)
		if tables == "" {
			return nil
		}
		content := original
		if strings.TrimSpace(content) != "" && !strings.HasSuffix(content, "\n") {
			content += lineEnding
		}
		if strings.TrimSpace(content) != "" {
			content += lineEnding
		}
		content += startMarker + lineEnding + withLineEnding(tables, lineEnding) + endMarker + lineEnding
		if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
			return fmt.Errorf("create Codex config directory %s: %w", filepath.Dir(configPath), err)
		}
		return os.WriteFile(configPath, []byte(content), 0644)
	}

	block := original[start:end]
	for _, name := range canonicalNamesForSync(names) {
		block = removeCodexMCPEntry(block, name)
	}
	if tables := r.codexTOMLTables(names); tables != "" {
		marker := strings.Index(block, endMarker)
		if marker >= 0 {
			block = block[:marker] + withLineEnding(tables, lineEnding) + block[marker:]
		}
	}

	content := original[:start] + block + original[end:]
	if !strings.Contains(block, "[mcp_servers.") {
		content = removeManagedBlock(original, startMarker, endMarker)
	}
	if content == original {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("create Codex config directory %s: %w", filepath.Dir(configPath), err)
	}
	return os.WriteFile(configPath, []byte(content), 0644)
}

// removeCodexMCPEntry removes just the requested table and its optional env
// table from DWYT's managed Codex block. The rest of the TOML (including the
// other DWYT MCP and all user tables) is left byte-for-byte intact.
func removeCodexMCPEntry(block, name string) string {
	for {
		start := findCodexMCPTable(block, name)
		if start < 0 {
			return block
		}
		end := len(block)
		if next := strings.Index(block[start+1:], "\n["); next >= 0 {
			end = start + 1 + next + 1
		}
		if marker := strings.Index(block[start:], "# dwyt:mcp:end"); marker >= 0 {
			markerStart := start + marker
			if markerStart < end {
				end = markerStart
			}
		}
		block = block[:start] + block[end:]
	}
}

func findCodexMCPTable(block, name string) int {
	for _, header := range []string{
		"[mcp_servers." + name + "]",
		"[mcp_servers." + name + ".env]",
	} {
		searchFrom := 0
		for {
			idx := strings.Index(block[searchFrom:], header)
			if idx < 0 {
				break
			}
			idx += searchFrom
			if idx == 0 || block[idx-1] == '\n' {
				return idx
			}
			searchFrom = idx + len(header)
		}
	}
	return -1
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

// readJSONObject loads a user-managed JSON configuration without silently
// replacing malformed data. Callers can safely merge into the returned map;
// a missing file is distinct from an unreadable or invalid one.
func readJSONObject(path string) (map[string]interface{}, bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return make(map[string]interface{}), false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", path, err)
	}
	config := make(map[string]interface{})
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, false, fmt.Errorf("invalid JSON in %s: %w", path, err)
	}
	if config == nil {
		return nil, false, fmt.Errorf("invalid JSON in %s: expected an object", path)
	}
	return config, true, nil
}

// jsonObjectField returns an object-valued field, creating it only when the
// field is absent. A present array/string/null is a user configuration error:
// replacing it would silently destroy a valid (but unexpected) configuration.
func jsonObjectField(config map[string]interface{}, key string) (map[string]interface{}, error) {
	raw, exists := config[key]
	if !exists {
		return make(map[string]interface{}), nil
	}
	value, ok := raw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("field %q must be a JSON object, got %T", key, raw)
	}
	return value, nil
}

func writeJSONFile(path string, value interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create config directory %s: %w", filepath.Dir(path), err)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}

// writeMergedMCPJSON reconciles only the DWYT server names supplied in
// managedNames. This lets a full sync remove stale disabled/uninstalled DWYT
// entries while a per-card sync leaves every other server (including the other
// DWYT card and all user servers) untouched.
func writeMergedMCPJSON(path, serverKey string, managedServers map[string]interface{}, managedNames []string, ensureInputs bool) error {
	config, _, err := readJSONObject(path)
	if err != nil {
		return err
	}
	servers, err := jsonObjectField(config, serverKey)
	if err != nil {
		return fmt.Errorf("invalid MCP config %s: %w", path, err)
	}
	removeLegacyServerKeysFor(servers, managedNames)
	removeCanonicalDWYTMCPEntries(servers, managedNames)
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
	removeLegacyServerKeysFor(servers, nil)
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
		endPos = consumeLineEnding(content, endPos)
		startIdx = trimPreviousLineEnding(content, startIdx)
		content = content[:startIdx] + content[endPos:]
	}
}

func preferredLineEnding(content string) string {
	newline := strings.IndexByte(content, '\n')
	if newline > 0 && content[newline-1] == '\r' {
		return "\r\n"
	}
	return "\n"
}

func withLineEnding(content, lineEnding string) string {
	if lineEnding == "\n" {
		return content
	}
	return strings.ReplaceAll(content, "\n", lineEnding)
}

func consumeLineEnding(content string, position int) int {
	if position >= len(content) {
		return position
	}
	if strings.HasPrefix(content[position:], "\r\n") {
		return position + 2
	}
	if content[position] == '\r' || content[position] == '\n' {
		return position + 1
	}
	return position
}

func trimPreviousLineEnding(content string, position int) int {
	if position >= 2 && content[position-2:position] == "\r\n" {
		return position - 2
	}
	if position > 0 && (content[position-1] == '\r' || content[position-1] == '\n') {
		return position - 1
	}
	return position
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

	errors := r.syncConfiguredTargets(projectPath, clients, []string{name})
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

	errors := r.syncConfiguredTargets(projectPath, clients, nil)

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
