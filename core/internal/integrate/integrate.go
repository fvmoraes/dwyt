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
	clientList := normalizeClients(clients)

	// DWYT does not touch the project's .gitignore. Whether to commit MCP
	// configs is the team's call — paths are absolute per machine, so most
	// teams either ignore them or rewrite them at clone time.

	// ── Generated configs use absolute, runtime-resolved paths ─────────
	cbmcpBin := filepath.Join(dwytBin, "codebase-memory-mcp")
	obsidianMCPBin := filepath.Join(dwytBin, "dwyt-obsidian-mcp")
	rtkBin := filepath.Join(dwytBin, "rtk")
	if runtime.GOOS == "windows" {
		cbmcpBin += ".exe"
		obsidianMCPBin += ".exe"
		rtkBin += ".exe"
	}

	// .mcp.json at the project root is the de-facto standard; Codex,
	// Claude Code, and several others read it directly.
	writeOrMergeMCPJSON(filepath.Join(projectPath, ".mcp.json"), cbmcpBin, obsidianMCPBin)
	writeOrMergeVSCodeMCPJSON(filepath.Join(projectPath, ".vscode", "mcp.json"), cbmcpBin, obsidianMCPBin)
	writeOrMergeOpenCodeJSON(filepath.Join(projectPath, "opencode.json"), cbmcpBin, obsidianMCPBin, rtkBin)

	if containsClient(clientList, "claude") {
		cp := filepath.Join(projectPath, "CLAUDE.md")
		writeOrUpdateInstructionFile(cp, claudeMDTemplate())
		os.MkdirAll(filepath.Join(projectPath, ".claude"), 0755)
		// Claude Code also reads .claude/mcp.json
		writeOrMergeMCPJSON(filepath.Join(projectPath, ".claude", "mcp.json"), cbmcpBin, obsidianMCPBin)
	}

	if containsClient(clientList, "cursor") {
		cp := filepath.Join(projectPath, ".cursor", "rules", "dwyt.mdc")
		os.MkdirAll(filepath.Dir(cp), 0755)
		writeOrUpdateInstructionFile(cp, cursorRuleTemplate())
		writeOrMergeMCPJSON(filepath.Join(projectPath, ".cursor", "mcp.json"), cbmcpBin, obsidianMCPBin)
	}

	if containsClient(clientList, "kiro") {
		cp := filepath.Join(projectPath, ".kiro", "steering", "dwyt.md")
		os.MkdirAll(filepath.Dir(cp), 0755)
		writeOrUpdateInstructionFile(cp, kiroSteeringTemplate())
		writeOrMergeMCPJSON(filepath.Join(projectPath, ".kiro", "settings", "mcp.json"), cbmcpBin, obsidianMCPBin)
		writeOrMergeMCPJSON(filepath.Join(projectPath, ".kiro", "mcp.json"), cbmcpBin, obsidianMCPBin)
	}

	if containsClient(clientList, "copilot") {
		cp := filepath.Join(projectPath, ".github", "copilot-instructions.md")
		os.MkdirAll(filepath.Dir(cp), 0755)
		writeOrUpdateInstructionFile(cp, copilotMDTemplate())
	}

	if containsClient(clientList, "windsurf") {
		// Windsurf reads project-scoped MCP configs from .windsurf/.
		writeOrMergeMCPJSON(filepath.Join(projectPath, ".windsurf", "mcp.json"), cbmcpBin, obsidianMCPBin)
	}

	if containsClient(clientList, "continue") {
		// Continue (vscode/jetbrains) reads .continue/mcp.json at the project root.
		writeOrMergeMCPJSON(filepath.Join(projectPath, ".continue", "mcp.json"), cbmcpBin, obsidianMCPBin)
	}

	// AGENTS.md is the convention shared by Codex, OpenCode, and several
	// other agent tools — always emit it, regardless of selected clients.
	writeOrUpdateInstructionFile(filepath.Join(projectPath, "AGENTS.md"), agentsMDTemplate(rtkBin))

	// ── Per-project workspace state ─────────────────────────────────────
	workspace.Touch(projectPath)

	fmt.Printf("  ✓ Project integrated: %s\n", projectPath)
}

func normalizeClients(clients string) []string {
	if strings.TrimSpace(clients) == "" {
		return []string{"claude", "codex", "copilot", "kiro", "cursor", "opencode", "windsurf", "continue"}
	}
	seen := map[string]bool{}
	var result []string
	for _, c := range strings.Split(clients, ",") {
		c = strings.TrimSpace(c)
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		result = append(result, c)
	}
	return result
}

func containsClient(clients []string, client string) bool {
	for _, c := range clients {
		if c == client {
			return true
		}
	}
	return false
}

func writeIfMissing(path, content string) {
	if _, err := os.Stat(path); err == nil {
		return
	}
	os.MkdirAll(filepath.Dir(path), 0755)
	os.WriteFile(path, []byte(content), 0644)
}

func writeOrMergeMCPJSON(path, cbmcpBin, obsidianMCPBin string) {
	config := map[string]interface{}{}
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &config)
	}
	servers, _ := config["mcpServers"].(map[string]interface{})
	if servers == nil {
		servers = map[string]interface{}{}
	}
	removeLegacyMCPKeys(servers)
	servers["codebase"] = stdioMCPConfig(cbmcpBin, false)
	servers["obsidian"] = stdioMCPConfig(obsidianMCPBin, false)
	config["mcpServers"] = servers
	writeJSON(path, config)
}

func writeOrMergeVSCodeMCPJSON(path, cbmcpBin, obsidianMCPBin string) {
	config := map[string]interface{}{}
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &config)
	}
	servers, _ := config["servers"].(map[string]interface{})
	if servers == nil {
		servers = map[string]interface{}{}
	}
	removeLegacyMCPKeys(servers)
	servers["codebase"] = stdioMCPConfig(cbmcpBin, true)
	servers["obsidian"] = stdioMCPConfig(obsidianMCPBin, true)
	config["inputs"] = []interface{}{}
	config["servers"] = servers
	delete(config, "mcpServers")
	writeJSON(path, config)
}

func writeOrMergeOpenCodeJSON(path, cbmcpBin, obsidianMCPBin, _ string) {
	config := map[string]interface{}{}
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &config)
	}
	if _, ok := config["$schema"]; !ok {
		config["$schema"] = "https://opencode.ai/config.json"
	}
	config["instructions"] = ensureStringItem(config["instructions"], "AGENTS.md")

	mcp, _ := config["mcp"].(map[string]interface{})
	if mcp == nil {
		mcp = map[string]interface{}{}
	}
	removeLegacyMCPKeys(mcp)
	mcp["codebase"] = map[string]interface{}{
		"type":        "local",
		"command":     []interface{}{cbmcpBin},
		"environment": mcpEnvForCommand(cbmcpBin),
	}
	mcp["obsidian"] = map[string]interface{}{
		"type":        "local",
		"command":     []interface{}{obsidianMCPBin},
		"environment": mcpEnvForCommand(obsidianMCPBin),
	}
	config["mcp"] = mcp

	permission, _ := config["permission"].(map[string]interface{})
	if permission == nil {
		permission = map[string]interface{}{}
	}
	for _, k := range []string{"bash", "edit", "webfetch", "skill"} {
		if _, ok := permission[k]; !ok {
			permission[k] = "allow"
		}
	}
	config["permission"] = permission

	writeJSON(path, config)
}

func stdioMCPConfig(command string, includeType bool) map[string]interface{} {
	cfg := map[string]interface{}{
		"command": command,
		"args":    []interface{}{},
	}
	if includeType {
		cfg["type"] = "stdio"
	} else {
		cfg["type"] = "stdio"
	}
	if strings.Contains(filepath.Base(command), "dwyt-obsidian-mcp") {
		cfg["env"] = mcpEnvForCommand(command)
	}
	if strings.Contains(filepath.Base(command), "codebase-memory-mcp") {
		cfg["env"] = mcpEnvForCommand(command)
	}
	return cfg
}

func mcpEnvForCommand(command string) map[string]interface{} {
	env := map[string]interface{}{}
	base := filepath.Base(command)
	if strings.Contains(base, "codebase-memory-mcp") {
		env["CBM_CACHE_DIR"] = filepath.Join(projectDwytHome(), "codebase")
	}
	if strings.Contains(base, "dwyt-obsidian-mcp") {
		env["DWYT_API_URL"] = "http://localhost:2737/api"
	}
	return env
}

func projectDwytHome() string {
	if h := os.Getenv("DWYT_HOME"); h != "" {
		return h
	}
	return filepath.Join(os.Getenv("HOME"), ".dwyt")
}

func removeLegacyMCPKeys(m map[string]interface{}) {
	for _, key := range []string{"dwyt", "dwyt-codebase", "dwyt-obsidian", "obsidian-mcp"} {
		delete(m, key)
	}
}

func ensureStringItem(value interface{}, item string) []interface{} {
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

func writeJSON(path string, value interface{}) {
	os.MkdirAll(filepath.Dir(path), 0755)
	data, _ := json.MarshalIndent(value, "", "  ")
	os.WriteFile(path, append(data, '\n'), 0644)
}

// ── Templates with absolute binary paths ──────────────────────────────────────

func mcpJSONTemplate(cbmcpBin, obsidianMCPBin string) string {
	return fmt.Sprintf(`{
  "mcpServers": {
    "codebase": {
      "type": "stdio",
      "command": %q,
      "env": {
        "CBM_CACHE_DIR": %q
      }
    },
    "obsidian": {
      "type": "stdio",
      "command": %q,
      "env": {
        "DWYT_API_URL": "http://localhost:2737/api"
      }
    }
  }
}
`, cbmcpBin, filepath.Join(projectDwytHome(), "codebase"), obsidianMCPBin)
}

func opencodeJSONTemplate(cbmcpBin, obsidianMCPBin, rtkBin string) string {
	return fmt.Sprintf(`{
  "$schema": "https://opencode.ai/config.json",
  "instructions": ["AGENTS.md"],
  "mcp": {
    "codebase": {
      "type": "local",
      "command": [%q],
      "environment": {
        "CBM_CACHE_DIR": %q
      }
    },
    "obsidian": {
      "type": "local",
      "command": [%q],
      "environment": {
        "DWYT_API_URL": "http://localhost:2737/api"
      }
    }
  },
  "permission": {
    "bash": "allow",
    "edit": "allow",
    "webfetch": "allow",
    "skill": "allow"
  }
}
`, cbmcpBin, filepath.Join(projectDwytHome(), "codebase"), obsidianMCPBin)
}

var markerStart = "<!-- dwyt:headroom-proxy-start -->"
var markerEnd = "<!-- dwyt:headroom-proxy-end -->"

func WriteHeadroomProxyConfig(projectPath string, headroomPort int, clients string) error {
	// Store proxy state in ~/.dwyt/projects/<id>/ — never inside the project
	dwytDir := workspace.ProjectDir(projectPath)
	os.MkdirAll(dwytDir, 0755)

	proxyConfig := map[string]any{
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
	// Proxy state lives in ~/.dwyt/projects/<id>/
	proxyFile := filepath.Join(workspace.ProjectDir(projectPath), "headroom-proxy.json")
	if data, err := os.ReadFile(proxyFile); err == nil {
		var cfg map[string]any
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
	removeMarkedBlocks(filepath.Join(projectPath, "opencode.json"))

	return nil
}

func appendMarkedBlock(filePath, block string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		// Create file if it doesn't exist
		if os.IsNotExist(err) {
			os.MkdirAll(filepath.Dir(filePath), 0755)
			return os.WriteFile(filePath, []byte(block), 0644)
		}
		return err
	}
	content := string(data)
	if strings.Contains(content, markerStart) {
		return nil // Already injected
	}
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
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
	var m map[string]any
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
	var m map[string]any
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
