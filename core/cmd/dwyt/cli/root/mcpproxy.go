package root

import (
	"os"
	"strings"

	"github.com/fvmoraes/dwyt/internal/mcpproxy"
	"github.com/spf13/cobra"
)

var (
	mcpProxyTarget string
	mcpProxyName   string
	mcpProxyMode   string
)

// mcpProxyCmd is the stdio shim DWYT registers as the MCP command in every
// client config. It runs the real MCP server (--target) and forwards stdio while
// counting tools/call requests, so the dashboard can attribute MCP usage
// regardless of which IDE/harness spawned it.
//
// Two modes (spec §32):
//
//   - transparent (default): byte-exact passthrough, passive counting only.
//   - governed (opt-in): additionally compacts large responses of known tools,
//     archiving the full bytes and returning a dwyt:// reference. Every failure
//     path bypasses to the original bytes.
//
// Hidden: it is an internal plumbing command, not a user-facing one.
var mcpProxyCmd = &cobra.Command{
	Use:                "mcp-proxy",
	Short:              "MCP stdio shim (internal)",
	Hidden:             true,
	DisableFlagParsing: false,
	RunE: func(cmd *cobra.Command, args []string) error {
		if mcpProxyTarget == "" {
			// Without a target there is nothing to proxy; exit cleanly so a
			// misconfigured entry never hangs a client.
			return nil
		}

		mode := mcpproxy.ParseMode(resolveProxyMode())
		cfg := mcpproxy.Config{
			Target:   mcpProxyTarget,
			Name:     mcpProxyName,
			Args:     args,
			Reporter: mcpproxy.NewHTTPReporter(mcpAPIURL("/mcp/usage")),
			Mode:     mode,
		}
		if mode == mcpproxy.ModeGoverned {
			cfg.Compactor = mcpproxy.NewHTTPCompactClient(mcpAPIURL("/governor/compact"))
		}

		code, err := mcpproxy.Run(cfg)
		if err != nil {
			// Surface the spawn/wait failure to the client and mirror a failed
			// process exit.
			os.Exit(1)
		}
		os.Exit(code)
		return nil
	},
}

// resolveProxyMode prefers the explicit flag, then the environment. The
// environment override exists so a user can enable governed mode for an already
// written client config without DWYT rewriting it.
func resolveProxyMode() string {
	if strings.TrimSpace(mcpProxyMode) != "" {
		return mcpProxyMode
	}
	return os.Getenv("DWYT_MCP_PROXY_MODE")
}

// mcpAPIURL resolves a dashboard endpoint, honoring the same DWYT_API_URL
// override the MCP servers use.
func mcpAPIURL(path string) string {
	base := os.Getenv("DWYT_API_URL")
	if base == "" {
		base = "http://localhost:2737/api"
	}
	return strings.TrimRight(base, "/") + path
}

// mcpUsageURL is retained for callers/tests that reference it by name.
func mcpUsageURL() string { return mcpAPIURL("/mcp/usage") }

func init() {
	mcpProxyCmd.Flags().StringVar(&mcpProxyTarget, "target", "", "path to the real MCP server binary")
	mcpProxyCmd.Flags().StringVar(&mcpProxyName, "name", "", "logical MCP server name credited in usage reports")
	mcpProxyCmd.Flags().StringVar(&mcpProxyMode, "mode", "",
		"transparent (default, byte-exact) or governed (opt-in response compaction)")
	Cmd.AddCommand(mcpProxyCmd)
}
