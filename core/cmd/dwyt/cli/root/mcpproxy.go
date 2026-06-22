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
)

// mcpProxyCmd is the transparent stdio shim DWYT registers as the MCP command
// in every client config. It runs the real MCP server (--target) and forwards
// stdio byte-for-byte while counting tools/call requests, so the dashboard can
// attribute MCP usage regardless of which IDE/harness spawned it.
//
// Hidden: it is an internal plumbing command, not a user-facing one.
var mcpProxyCmd = &cobra.Command{
	Use:                "mcp-proxy",
	Short:              "Transparent MCP stdio shim (internal)",
	Hidden:             true,
	DisableFlagParsing: false,
	RunE: func(cmd *cobra.Command, args []string) error {
		if mcpProxyTarget == "" {
			// Without a target there is nothing to proxy; exit cleanly so a
			// misconfigured entry never hangs a client.
			return nil
		}

		reporter := mcpproxy.NewHTTPReporter(mcpUsageURL())
		code, err := mcpproxy.Run(mcpproxy.Config{
			Target:   mcpProxyTarget,
			Name:     mcpProxyName,
			Args:     args,
			Reporter: reporter,
		})
		if err != nil {
			// Surface the spawn/wait failure to the client and mirror a failed
			// process exit.
			os.Exit(1)
		}
		os.Exit(code)
		return nil
	},
}

// mcpUsageURL resolves the dashboard endpoint that records MCP usage, honoring
// the same DWYT_API_URL override the Obsidian MCP client uses.
func mcpUsageURL() string {
	base := os.Getenv("DWYT_API_URL")
	if base == "" {
		base = "http://localhost:2737/api"
	}
	base = strings.TrimRight(base, "/")
	return base + "/mcp/usage"
}

func init() {
	mcpProxyCmd.Flags().StringVar(&mcpProxyTarget, "target", "", "path to the real MCP server binary")
	mcpProxyCmd.Flags().StringVar(&mcpProxyName, "name", "", "logical MCP server name credited in usage reports")
	Cmd.AddCommand(mcpProxyCmd)
}
