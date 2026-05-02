package root

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/DeusData/dwyt-orchestrator/internal/server"
	"github.com/DeusData/dwyt-orchestrator/internal/status"

	"github.com/spf13/cobra"
)

var (
	DwytBin  string
	DwytHome string
)

var Cmd = &cobra.Command{
	Use:   "dwyt",
	Short: "DWYT — Don't Waste Your Tokens",
	Long: `DWYT — Don't Waste Your Tokens v3.1

Starts all services and opens the web dashboard at localhost:2737.
All configuration is done via the web UI.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDefault()
	},
}

func init() {
	home := getHome()
	DwytHome = home + "/.dwyt"
	if h := os.Getenv("DWYT_HOME"); h != "" {
		DwytHome = h
	}
	DwytBin = DwytHome + "/bin"

	Cmd.AddCommand(stopCmd)
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

func runDefault() error {
	fmt.Printf("\n  ╔══════════════════════════════════════╗\n")
	fmt.Printf("  ║  DWYT — Don't Waste Your Tokens     ║\n")
	fmt.Printf("  ╚══════════════════════════════════════╝\n\n")

	// Start ALL services
	startService("codebase-memory-mcp", filepath.Join(DwytBin, "codebase-memory-mcp"), "--ui=true", "--port=9749")
	startService("headroom", filepath.Join(DwytBin, "headroom"), "proxy", "--port", "8787")

	// RTK and MemStack are CLI tools, just verify they exist
	for _, bin := range []string{"rtk", "memstack"} {
		p := filepath.Join(DwytBin, bin)
		if _, err := os.Stat(p); err == nil {
			fmt.Printf("  →  %-25s disponível\n", bin)
		}
	}

	srv := server.New(2737, DwytBin, DwytHome)
	return srv.Start()
}

func startService(name, bin string, args ...string) {
	if _, err := os.Stat(bin); err != nil {
		fmt.Printf("  →  %-25s não instalado\n", name)
		return
	}
	fmt.Printf("  →  %-25s iniciando...\n", name)
	cmd := exec.Command(bin, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Start()
}

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop all DWYT services",
	RunE: func(cmd *cobra.Command, args []string) error {
		exec.Command("pkill", "-f", "codebase-memory-mcp").Run()
		exec.Command("pkill", "-f", "headroom proxy").Run()
		fmt.Println("  ✓ All services stopped")
		return nil
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show quick status of all tools",
	RunE: func(cmd *cobra.Command, args []string) error {
		s := status.PollAll(DwytBin)
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
		fmt.Println("dwyt v3.1.0 — Don't Waste Your Tokens")
		return nil
	},
}
