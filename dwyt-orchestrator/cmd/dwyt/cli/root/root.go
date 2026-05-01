package root

import (
	"os"

	"github.com/spf13/cobra"
)

var (
	DwytBin  string
	DwytHome string
)

var Cmd = &cobra.Command{
	Use:   "dwyt",
	Short: "DWYT — Don't Waste Your Tokens",
	Long: `DWYT Orchestrator v3.0 — CLI + Dashboard for:
  codebase-memory-mcp + RTK + Headroom + MemStack

Flags (standalone):
  dwyt -d                    Start dashboard at localhost:2737
  dwyt -t <tools>            Install specific tools (c=codebase, r=rtk, h=headroom, m=memstack)
  dwyt -l <clients>          Integrate specific clients (c=claude, x=codex, p=copilot, k=kiro, r=cursor, o=opencode)
  dwyt -R <path>             Repository path for integration

Commands:
  dwyt install               Interactive installation
  dwyt repo <path>           Integrate and index a repository
  dwyt dashboard             Start the web dashboard
  dwyt status                Quick status of all tools
  dwyt version               Show version`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if dashboardFlag {
			return runDashboard()
		}
		if toolFlag != "" || clientFlag != "" {
			return runInstall()
		}
		return cmd.Help()
	},
}

var (
	dashboardFlag bool
	toolFlag      string
	clientFlag    string
	repoFlag      string
)

func init() {
	home := getHome()
	DwytHome = home + "/.dwyt"
	if h := os.Getenv("DWYT_HOME"); h != "" {
		DwytHome = h
	}
	DwytBin = DwytHome + "/bin"

	Cmd.Flags().BoolVarP(&dashboardFlag, "dashboard", "d", false, "Start dashboard at localhost:2737")
	Cmd.Flags().StringVarP(&toolFlag, "tool", "t", "", "Tools to install (c=codebase, r=rtk, h=headroom, m=memstack)")
	Cmd.Flags().StringVarP(&clientFlag, "client", "l", "", "Clients to integrate (c=claude, x=codex, p=copilot, k=kiro, r=cursor, o=opencode)")
	Cmd.Flags().StringVarP(&repoFlag, "repo", "R", "", "Repository path for integration")

	Cmd.AddCommand(installCmd)
	Cmd.AddCommand(repoCmd)
	Cmd.AddCommand(reinstallCmd)
	Cmd.AddCommand(uninstallCmd)
	Cmd.AddCommand(dashboardCmd)
	Cmd.AddCommand(statusCmd)
	Cmd.AddCommand(versionCmd)
}

func getHome() string {
	h, _ := os.UserHomeDir()
	if h == "" {
		h = os.Getenv("HOME")
	}
	return h
}
