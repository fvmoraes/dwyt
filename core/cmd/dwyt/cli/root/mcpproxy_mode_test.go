package root

import (
	"testing"

	"github.com/fvmoraes/dwyt/internal/dwytconfig"
)

// withProxyDefaults isolates the package state resolveProxyMode reads.
func withProxyDefaults(t *testing.T, dwytHome, flagValue, envValue string) {
	t.Helper()
	oldHome, oldFlag := DwytHome, mcpProxyMode
	t.Cleanup(func() { DwytHome, mcpProxyMode = oldHome, oldFlag })
	t.Setenv("DWYT_MCP_PROXY_MODE", envValue)
	DwytHome = dwytHome
	mcpProxyMode = flagValue
}

// The `mcp_proxy.default_mode` block in the consolidated config (spec §57) is
// the last fallback of the resolution chain: without it the knob was dead
// config and a user pinning it in dwyt.json silently got transparent mode.
func TestResolveProxyModeFallsBackToConfig(t *testing.T) {
	dwytHome := t.TempDir()
	withProxyDefaults(t, dwytHome, "", "")

	if got := resolveProxyMode(); got != "transparent" {
		t.Fatalf("missing config must default to transparent, got %q", got)
	}

	cfg := dwytconfig.Default()
	cfg.MCPProxy.DefaultMode = "optimized"
	if err := dwytconfig.Save(dwytHome, cfg); err != nil {
		t.Fatal(err)
	}
	if got := resolveProxyMode(); got != "optimized" {
		t.Fatalf("config default_mode should win when flag and env are unset, got %q", got)
	}
}

func TestResolveProxyModeFlagAndEnvWin(t *testing.T) {
	dwytHome := t.TempDir()
	cfg := dwytconfig.Default()
	cfg.MCPProxy.DefaultMode = "optimized"
	if err := dwytconfig.Save(dwytHome, cfg); err != nil {
		t.Fatal(err)
	}

	// The explicit flag beats everything, even a config asking for optimized.
	withProxyDefaults(t, dwytHome, "transparent", "optimized")
	if got := resolveProxyMode(); got != "transparent" {
		t.Fatalf("flag should have the final say, got %q", got)
	}

	// The env override beats the config.
	withProxyDefaults(t, dwytHome, "", "optimized")
	if got := resolveProxyMode(); got != "optimized" {
		t.Fatalf("env should beat the config default, got %q", got)
	}
}
