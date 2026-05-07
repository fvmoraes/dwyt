package codexauth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type authFile struct {
	AuthMode     string          `json:"auth_mode"`
	OpenAIAPIKey string          `json:"OPENAI_API_KEY"`
	Tokens       json.RawMessage `json:"tokens"`
}

// UsesChatGPTLogin reports whether Codex is currently authenticated through
// ChatGPT OAuth instead of an API key. It only inspects auth metadata.
func UsesChatGPTLogin() bool {
	usesChatGPT, err := UsesChatGPTLoginFromPath(authPath())
	if err != nil {
		return false
	}
	return usesChatGPT
}

func UsesChatGPTLoginFromPath(path string) (bool, error) {
	if os.Getenv("OPENAI_API_KEY") != "" {
		return false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	var auth authFile
	if err := json.Unmarshal(data, &auth); err != nil {
		return false, err
	}
	mode := strings.ToLower(strings.TrimSpace(auth.AuthMode))
	if mode == "chatgpt" {
		return true, nil
	}
	if mode == "api_key" || mode == "apikey" || mode == "api-key" {
		return false, nil
	}
	if strings.TrimSpace(auth.OpenAIAPIKey) != "" {
		return false, nil
	}
	return len(auth.Tokens) > 0 && string(auth.Tokens) != "null", nil
}

func authPath() string {
	if p := os.Getenv("CODEX_AUTH_FILE"); p != "" {
		return p
	}
	if home := os.Getenv("CODEX_HOME"); home != "" {
		return filepath.Join(home, "auth.json")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".codex", "auth.json")
	}
	return filepath.Join(".codex", "auth.json")
}
