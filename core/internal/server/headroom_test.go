package server

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCodexAuth(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestShouldInstallHeadroomSkipsCodexChatGPTOnly(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	writeCodexAuth(t, codexHome, `{"auth_mode":"chatgpt","tokens":{"id_token":"redacted"}}`)

	cfg := Config{Tools: []string{"headroom", "obsidian"}, Ias: []string{"codex"}}
	if shouldInstallHeadroom(cfg) {
		t.Fatal("expected Codex ChatGPT login to skip Headroom install")
	}
}

func TestShouldInstallHeadroomAllowsCodexAPIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	writeCodexAuth(t, codexHome, `{"auth_mode":"api_key","OPENAI_API_KEY":"redacted"}`)

	cfg := Config{Tools: []string{"headroom", "obsidian"}, Ias: []string{"codex"}}
	if !shouldInstallHeadroom(cfg) {
		t.Fatal("expected Codex API key login to allow Headroom install")
	}
}

func TestShouldInstallHeadroomKeepsOtherClients(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	writeCodexAuth(t, codexHome, `{"auth_mode":"chatgpt","tokens":{"id_token":"redacted"}}`)

	cfg := Config{Tools: []string{"headroom", "obsidian"}, Ias: []string{"codex", "claude"}}
	if !shouldInstallHeadroom(cfg) {
		t.Fatal("expected non-Codex clients to keep Headroom install")
	}
}
