package integrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectGeneratesClientMCPConfigsAndIgnores(t *testing.T) {
	projectPath := t.TempDir()
	dwytHome := t.TempDir()
	t.Setenv("DWYT_HOME", dwytHome)
	dwytBin := filepath.Join(dwytHome, "bin")

	Project(projectPath, "claude,codex,copilot,kiro,cursor,opencode", dwytBin)

	gitignoreData, err := os.ReadFile(filepath.Join(projectPath, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	gitignore := string(gitignoreData)
	for _, entry := range dwytGeneratedIgnores() {
		if !strings.Contains(gitignore, entry) {
			t.Fatalf(".gitignore missing %s\n%s", entry, gitignore)
		}
	}

	var vscode map[string]interface{}
	readJSON(t, filepath.Join(projectPath, ".vscode", "mcp.json"), &vscode)
	if _, ok := vscode["servers"].(map[string]interface{}); !ok {
		t.Fatalf("expected VS Code MCP config to use servers: %#v", vscode)
	}
	if _, legacy := vscode["mcpServers"]; legacy {
		t.Fatalf("did not expect legacy mcpServers in VS Code config: %#v", vscode)
	}

	var cursor map[string]interface{}
	readJSON(t, filepath.Join(projectPath, ".cursor", "mcp.json"), &cursor)
	if _, ok := cursor["mcpServers"].(map[string]interface{}); !ok {
		t.Fatalf("expected Cursor MCP config: %#v", cursor)
	}

	var kiro map[string]interface{}
	readJSON(t, filepath.Join(projectPath, ".kiro", "settings", "mcp.json"), &kiro)
	if _, ok := kiro["mcpServers"].(map[string]interface{}); !ok {
		t.Fatalf("expected Kiro MCP config: %#v", kiro)
	}
}

func readJSON(t *testing.T, path string, out interface{}) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}
