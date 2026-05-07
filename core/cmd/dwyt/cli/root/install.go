package root

import (
	"fmt"
	"os"
	"strings"

	"github.com/fvmoraes/dwyt/internal/install"
	"github.com/fvmoraes/dwyt/internal/log"
	"github.com/spf13/cobra"
)

var installToolsFlag string

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install DWYT dependencies (cbmcp, rtk, headroom, obsidian)",
	Long: `Install DWYT runtime dependencies headlessly, without needing the dashboard UI.

Default installs everything: codebase-memory-mcp, rtk, headroom, obsidian-mcp,
and the Obsidian desktop app. Use --tools to limit the set.

Examples:
  dwyt install                          # all
  dwyt install --tools=cbmcp,rtk        # subset
  dwyt install --tools=headroom         # just headroom
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		tools := parseToolList(installToolsFlag)
		if len(tools) == 0 {
			tools = []string{"cbmcp", "rtk", "headroom", "obsidian"}
		}

		fmt.Printf("\n  Installing tools to %s\n\n", DwytBin)
		if err := os.MkdirAll(DwytBin, 0755); err != nil {
			return fmt.Errorf("create %s: %w", DwytBin, err)
		}

		var failures []string
		runStep := func(name string, fn func() error) {
			fmt.Printf("  → %-12s installing...\n", name)
			if err := fn(); err != nil {
				fmt.Printf("    ✗ %s: %v\n", name, err)
				log.Error("install step failed", log.Fields{"tool": name, "error": err.Error()})
				failures = append(failures, name)
				return
			}
			fmt.Printf("    ✓ %s ok\n", name)
		}

		for _, t := range tools {
			switch t {
			case "cbmcp", "codebase":
				runStep("cbmcp", func() error { return install.CBMCP(DwytBin) })
			case "rtk":
				runStep("rtk", func() error { return install.RTK(DwytBin) })
			case "headroom":
				runStep("headroom", func() error { return install.Headroom(DwytBin, DwytHome) })
			case "obsidian":
				runStep("obsidian-mcp", func() error { return install.ObsidianMCP(DwytBin) })
				runStep("obsidian-app", func() error {
					_, err := install.InstallObsidianApp()
					return err
				})
			case "obsidian-mcp":
				runStep("obsidian-mcp", func() error { return install.ObsidianMCP(DwytBin) })
			default:
				return fmt.Errorf("unknown tool: %s (valid: cbmcp, rtk, headroom, obsidian, obsidian-mcp)", t)
			}
		}

		fmt.Println()
		if len(failures) > 0 {
			return fmt.Errorf("%d tool(s) failed: %s", len(failures), strings.Join(failures, ", "))
		}
		fmt.Printf("  ✓ All tools installed. Run 'dwyt .' to start.\n\n")
		return nil
	},
}

func parseToolList(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func init() {
	installCmd.Flags().StringVar(&installToolsFlag, "tools", "",
		"Comma-separated list (cbmcp,rtk,headroom,obsidian); empty = all")
	Cmd.AddCommand(installCmd)
}
