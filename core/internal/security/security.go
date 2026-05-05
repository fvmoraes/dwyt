package security

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ProtectionConfig struct {
	ProtectedPaths []string `json:"protectedPaths"`
	LogAttempts    bool     `json:"logAttempts"`
}

var defaultProtected = []string{
	"projects", // ~/.dwyt/projects/ — Obsidian vaults
}

func configPath(dwytHome string) string {
	return filepath.Join(dwytHome, "data", "protection.json")
}

func logPath(dwytHome string) string {
	return filepath.Join(dwytHome, "logs", "security.log")
}

func Load(dwytHome string) *ProtectionConfig {
	path := configPath(dwytHome)
	cfg := &ProtectionConfig{
		ProtectedPaths: defaultProtected,
		LogAttempts:    true,
	}
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, cfg)
	}
	os.MkdirAll(filepath.Dir(path), 0755)
	data, _ := json.MarshalIndent(cfg, "", "  ")
	os.WriteFile(path, data, 0644)
	return cfg
}

func (pc *ProtectionConfig) IsProtected(dwytHome, target string) bool {
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	absHome, err := filepath.Abs(dwytHome)
	if err != nil {
		return false
	}

	for _, p := range pc.ProtectedPaths {
		absProtected := filepath.Join(absHome, p)
		if absTarget == absProtected || strings.HasPrefix(absTarget, absProtected+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

func (pc *ProtectionConfig) LogBlockedAttempt(dwytHome, operation, path, reason string) {
	if !pc.LogAttempts {
		return
	}
	os.MkdirAll(filepath.Dir(logPath(dwytHome)), 0755)
	f, err := os.OpenFile(logPath(dwytHome), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "[%s] BLOCKED op=%s path=%s reason=%s\n",
		time.Now().Format(time.RFC3339), operation, path, reason)
}

// ValidateDelete checks if a path is safe to delete. Returns error if protected.
func ValidateDelete(dwytHome, path string) error {
	cfg := Load(dwytHome)
	if cfg.IsProtected(dwytHome, path) {
		cfg.LogBlockedAttempt(dwytHome, "delete", path, "protected path")
		return fmt.Errorf("security: cannot delete protected path: %s", path)
	}
	return nil
}

// SafeRemove removes a path only if it's not protected.
func SafeRemove(dwytHome, path string) error {
	if err := ValidateDelete(dwytHome, path); err != nil {
		return err
	}
	return os.RemoveAll(path)
}

// InitObsidianConfig creates the Obsidian API config if it doesn't exist.
func InitObsidianConfig(dwytHome string) {
	dataDir := filepath.Join(dwytHome, "data", "obsidian")
	os.MkdirAll(dataDir, 0755)
	configFile := filepath.Join(dataDir, "obsidian.json")

	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		defaultConfig := map[string]interface{}{
			"api_url":  "http://127.0.0.1:27123",
			"api_key":  "",
			"port":     27123,
			"enabled":  false,
			"note":     "Configure API key from Obsidian REST API plugin settings",
		}
		data, _ := json.MarshalIndent(defaultConfig, "", "  ")
		os.WriteFile(configFile, data, 0644)
	}
}
