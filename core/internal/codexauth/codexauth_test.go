package codexauth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUsesChatGPTLoginFromPath(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(path, []byte(`{"auth_mode":"chatgpt","tokens":{"id_token":"redacted"}}`), 0600); err != nil {
		t.Fatal(err)
	}

	usesChatGPT, err := UsesChatGPTLoginFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if !usesChatGPT {
		t.Fatal("expected ChatGPT auth to be detected")
	}
}

func TestUsesChatGPTLoginFromPathWithAPIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(path, []byte(`{"auth_mode":"api_key","OPENAI_API_KEY":"redacted"}`), 0600); err != nil {
		t.Fatal(err)
	}

	usesChatGPT, err := UsesChatGPTLoginFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if usesChatGPT {
		t.Fatal("expected API key auth to remain eligible for Headroom")
	}
}
