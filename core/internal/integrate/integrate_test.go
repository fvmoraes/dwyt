package integrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectGeneratesClientMCPConfigs(t *testing.T) {
	projectPath := t.TempDir()
	dwytHome := t.TempDir()
	t.Setenv("DWYT_HOME", dwytHome)
	dwytBin := filepath.Join(dwytHome, "bin")

	Project(projectPath, "claude,codex,copilot,kiro,cursor,opencode", dwytBin)

	// DWYT não deve criar nem alterar o .gitignore do projeto.
	if _, err := os.Stat(filepath.Join(projectPath, ".gitignore")); err == nil {
		t.Fatalf(".gitignore should not be created by Project(); decisão é do time")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
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

	agents, err := os.ReadFile(filepath.Join(projectPath, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(agents), instructionMarkerStart) != 1 {
		t.Fatalf("expected one DWYT instruction block, got:\n%s", string(agents))
	}
}

func TestProjectUpdatesInstructionBlockWithoutOverwritingUserContent(t *testing.T) {
	projectPath := t.TempDir()
	dwytBin := filepath.Join(t.TempDir(), "bin")
	agentsPath := filepath.Join(projectPath, "AGENTS.md")
	original := "# Team Rules\n\nKeep this paragraph.\n"
	if err := os.WriteFile(agentsPath, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	Project(projectPath, "codex", dwytBin)
	Project(projectPath, "codex", dwytBin)

	data, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, original) {
		t.Fatalf("user content was not preserved:\n%s", content)
	}
	if strings.Count(content, instructionMarkerStart) != 1 || strings.Count(content, "#dwyt") != 1 {
		t.Fatalf("expected exactly one DWYT block:\n%s", content)
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
