package root

import (
	"fmt"

	"github.com/DeusData/dwyt-orchestrator/internal/detect"
	"github.com/DeusData/dwyt-orchestrator/internal/server"
	"github.com/DeusData/dwyt-orchestrator/internal/status"

	"github.com/spf13/cobra"
)

var dashboardPort int

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Start the DWYT web dashboard",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDashboard()
	},
}

func init() {
	dashboardCmd.Flags().IntVarP(&dashboardPort, "port", "p", 2737, "Dashboard port")
}

func runDashboard() error {
	e := detect.Detect()
	if dashboardPort == 0 {
		dashboardPort = 2737
	}
	srv := server.New(dashboardPort, e.DwytBin, e.DwytHome)
	fmt.Printf("\n  ╔══════════════════════════════════════╗\n")
	fmt.Printf("  ║  DWYT Dashboard v3.0                ║\n")
	fmt.Printf("  ╚══════════════════════════════════════╝\n\n")
	return srv.Start()
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show quick status of all tools",
	RunE: func(cmd *cobra.Command, args []string) error {
		e := detect.Detect()
		s := status.PollAll(e.DwytBin)
		fmt.Printf("\n  DWYT Status:\n")
		for _, t := range s.Tools {
			icon := "⚫"
			if t.Healthy {
				icon = "🟢"
			} else if t.Running {
				icon = "🟡"
			}
			fmt.Printf("  %s %-22s %s\n", icon, t.Name, t.Details)
		}
		return nil
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show DWYT version",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("dwyt v3.0.0 — Don't Waste Your Tokens")
		return nil
	},
}
