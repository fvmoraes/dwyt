package integrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fvmoraes/dwyt/internal/log"
	"github.com/fvmoraes/dwyt/internal/workspace"
)

// Project writes only DWYT-managed project instructions and workspace state.
// MCP client configuration belongs exclusively to mcpregistry: it owns the
// canonical server wiring, merges existing client JSON, and preserves the
// codebase proxy. Keeping it there prevents this integration pass from
// rewriting an already-configured client with stale direct-binary entries.
func Project(projectPath, clients, _ string) {
	log.Info("integrating project", log.Fields{"path": projectPath, "clients": clients})
	clientList := normalizeClients(clients)
	writeInstruction := func(path, content string) {
		if err := writeOrUpdateInstructionFile(path, content); err != nil {
			log.Warn("project instruction update failed", log.Fields{"path": path, "error": err.Error()})
		}
	}

	// DWYT does not touch the project's .gitignore. Whether to commit MCP
	// configs is the team's call — paths are absolute per machine, so most
	// teams either ignore them or rewrite them at clone time.

	if containsClient(clientList, "claude") {
		writeInstruction(filepath.Join(projectPath, "CLAUDE.md"), claudeMDTemplate())
	}

	if containsClient(clientList, "cursor") {
		writeInstruction(filepath.Join(projectPath, ".cursor", "rules", "dwyt.mdc"), cursorRuleTemplate())
	}

	if containsClient(clientList, "kiro") {
		writeInstruction(filepath.Join(projectPath, ".kiro", "steering", "dwyt.md"), kiroSteeringTemplate())
	}

	if containsClient(clientList, "copilot") {
		writeInstruction(filepath.Join(projectPath, ".github", "copilot-instructions.md"), copilotMDTemplate())
	}

	if containsClient(clientList, "windsurf") {
		writeInstruction(filepath.Join(projectPath, ".windsurf", "rules", "dwyt.md"), windsurfRuleTemplate())
	}

	if containsClient(clientList, "continue") {
		writeInstruction(filepath.Join(projectPath, ".continue", "rules", "dwyt.md"), continueRuleTemplate())
	}

	// AGENTS.md is the convention shared by Codex and OpenCode. Respect the
	// client toggles: only create/update it when one of those clients is on.
	// Disabling every AGENTS.md client means DWYT leaves the file untouched.
	if containsClient(clientList, "codex") || containsClient(clientList, "opencode") {
		writeInstruction(filepath.Join(projectPath, "AGENTS.md"), agentsMDTemplate(""))
	}

	// ── Per-project workspace state ─────────────────────────────────────
	workspace.Touch(projectPath)

	fmt.Printf("  ✓ Project integrated: %s\n", projectPath)
}

func normalizeClients(clients string) []string {
	// An empty selection means the user enabled no AI clients — DWYT must
	// install nothing client-specific. Never fall back to "all clients".
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

// ── Templates with absolute binary paths ──────────────────────────────────────

var markerStart = "<!-- dwyt:headroom-proxy-start -->"
var markerEnd = "<!-- dwyt:headroom-proxy-end -->"

func WriteHeadroomProxyConfig(projectPath string, headroomPort int, clients string) error {
	// Store proxy state in ~/.dwyt/projects/<id>/ — never inside the project
	dwytDir := workspace.ProjectDir(projectPath)
	if err := os.MkdirAll(dwytDir, 0755); err != nil {
		return fmt.Errorf("create headroom proxy state directory: %w", err)
	}

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
	var firstErr error
	appendBlock := func(path string) {
		if err := appendMarkedBlock(path, block); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("append headroom proxy block to %s: %w", path, err)
		}
	}

	for _, c := range strings.Split(clients, ",") {
		c = strings.TrimSpace(c)
		switch c {
		case "opencode":
			appendBlock(filepath.Join(projectPath, "AGENTS.md"))
		case "claude":
			appendBlock(filepath.Join(projectPath, "CLAUDE.md"))
			appendBlock(filepath.Join(projectPath, "AGENTS.md"))
		case "codex":
			appendBlock(filepath.Join(projectPath, "AGENTS.md"))
		case "copilot":
			appendBlock(filepath.Join(projectPath, ".github", "copilot-instructions.md"))
			appendBlock(filepath.Join(projectPath, "AGENTS.md"))
		case "kiro":
			appendBlock(filepath.Join(projectPath, ".kiro", "steering", "dwyt.md"))
			appendBlock(filepath.Join(projectPath, "AGENTS.md"))
		case "cursor":
			appendBlock(filepath.Join(projectPath, ".cursor", "rules", "dwyt.mdc"))
			appendBlock(filepath.Join(projectPath, "AGENTS.md"))
		}
	}

	return firstErr
}

func RemoveHeadroomProxyConfig(projectPath string, clients string) error {
	// Proxy state lives in ~/.dwyt/projects/<id>/
	proxyFile := filepath.Join(workspace.ProjectDir(projectPath), "headroom-proxy.json")
	var firstErr error
	recordError := func(action string, err error) {
		if err != nil && firstErr == nil {
			firstErr = fmt.Errorf("%s: %w", action, err)
		}
	}
	if data, err := os.ReadFile(proxyFile); err == nil {
		var cfg map[string]any
		if err := json.Unmarshal(data, &cfg); err != nil {
			recordError("decode headroom proxy state", err)
		} else {
			cfg["active"] = false
			if newData, err := json.MarshalIndent(cfg, "", "  "); err != nil {
				recordError("encode headroom proxy state", err)
			} else if err := os.WriteFile(proxyFile, newData, 0644); err != nil {
				recordError("write headroom proxy state", err)
			}
		}
	} else if !os.IsNotExist(err) {
		recordError("read headroom proxy state", err)
	}

	for _, filePath := range []string{
		filepath.Join(projectPath, "CLAUDE.md"),
		filepath.Join(projectPath, ".cursor", "rules", "dwyt.mdc"),
		filepath.Join(projectPath, ".kiro", "steering", "dwyt.md"),
		filepath.Join(projectPath, "AGENTS.md"),
		filepath.Join(projectPath, ".github", "copilot-instructions.md"),
		filepath.Join(projectPath, "opencode.json"),
	} {
		recordError("remove headroom proxy block from "+filePath, removeMarkedBlocks(filePath))
	}

	return firstErr
}

func appendMarkedBlock(filePath, block string) (retErr error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		// Create file if it doesn't exist
		if os.IsNotExist(err) {
			if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
				return err
			}
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
	defer func() {
		if closeErr := f.Close(); retErr == nil && closeErr != nil {
			retErr = closeErr
		}
	}()
	if len(content) > 0 && content[len(content)-1] != '\n' {
		if _, err := f.Write([]byte("\n")); err != nil {
			return err
		}
	}
	if _, err := f.Write([]byte(block)); err != nil {
		return err
	}
	return nil
}

func removeMarkedBlocks(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
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
		return os.WriteFile(filePath, []byte(content), 0644)
	}
	return nil
}
