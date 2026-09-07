package security

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

// CleanHome removes all contents from dwytHome except protected paths.
func CleanHome(dwytHome string) {
	cfg := Load(dwytHome)
	entries, err := os.ReadDir(dwytHome)
	if err != nil {
		return
	}
	for _, entry := range entries {
		entryPath := filepath.Join(dwytHome, entry.Name())
		if cfg.IsProtected(dwytHome, entryPath) {
			continue
		}
		os.RemoveAll(entryPath)
	}
}

// IsSafeHome validates that a path looks like a legitimate DWYT home directory.
//
// The check exists because CleanHome removes everything under dwytHome that is
// not protected. It must therefore reject any directory whose wipe would
// destroy data far beyond DWYT: the user's home itself, any ancestor of it,
// the filesystem root, and top-level system directories such as /etc or /usr —
// including when they arrive through an explicit DWYT_HOME override.
func IsSafeHome(dwytHome string) bool {
	if dwytHome == "/" || dwytHome == "" {
		return false
	}
	abs, err := filepath.Abs(dwytHome)
	if err != nil {
		return false
	}
	// Top-level system directories (/etc, /usr, /tmp, /opt, ...) are never a
	// DWYT home.
	if filepath.Dir(abs) == "/" {
		return false
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		homeAbs, _ := filepath.Abs(home)
		if abs == homeAbs {
			// $HOME itself is exactly the misconfiguration this guard exists to
			// catch: DWYT would treat the entire home as its data directory.
			return false
		}
		if strings.HasPrefix(homeAbs, abs+string(os.PathSeparator)) {
			// abs is an ancestor of $HOME (/home, /Users, ...).
			return false
		}
		if strings.HasPrefix(abs, homeAbs+string(os.PathSeparator)) {
			return true
		}
	}
	// Outside $HOME: only an explicit DWYT_HOME override qualifies, and it has
	// already passed the root and ancestor checks above.
	if dwytHomeEnv := os.Getenv("DWYT_HOME"); dwytHomeEnv != "" {
		absOverride, _ := filepath.Abs(dwytHomeEnv)
		return abs == absOverride
	}
	return false
}

// InitObsidianConfig creates the Obsidian API config if it doesn't exist.
func InitObsidianConfig(dwytHome string) {
	dataDir := filepath.Join(dwytHome, "data", "obsidian")
	os.MkdirAll(dataDir, 0755)
	configFile := filepath.Join(dataDir, "obsidian.json")

	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		defaultConfig := map[string]interface{}{
			"api_url": "http://127.0.0.1:27123",
			"api_key": "",
			"port":    27123,
			"enabled": false,
			"note":    "Configure API key from Obsidian REST API plugin settings",
		}
		data, _ := json.MarshalIndent(defaultConfig, "", "  ")
		os.WriteFile(configFile, data, 0600)
		return
	}
	// The file may hold an API key the user pasted in later; a config world
	// readable on a multi-user machine has no upside. Tighten best-effort.
	os.Chmod(configFile, 0600)
}
