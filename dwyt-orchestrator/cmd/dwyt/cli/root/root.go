package root

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/DeusData/dwyt-orchestrator/internal/detect"
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
	Long:  "DWYT v3.1 — Starts all services in background and opens the web dashboard.",
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
	Cmd.AddCommand(daemonCmd)
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

	// Start services in background
	startService("codebase-memory-mcp", filepath.Join(DwytBin, "codebase-memory-mcp"), "--ui=true", "--port=9749")
	startService("headroom", filepath.Join(DwytBin, "headroom"), "proxy", "--port", "8787")

	for _, bin := range []string{"rtk", "memstack"} {
		p := filepath.Join(DwytBin, bin)
		if _, err := os.Stat(p); err == nil {
			fmt.Printf("  →  %-25s disponível\n", bin)
		}
	}

	// Fork daemon to background
	exe, _ := os.Executable()
	daemon := exec.Command(exe, "daemon")
	daemon.Stdout = nil
	daemon.Stderr = nil
	daemon.Stdin = nil
	daemon.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	daemon.Start()

	fmt.Printf("\n  ✓ Dashboard → http://localhost:2737\n")
	fmt.Printf("  Parar: dwyt stop\n\n")
	return nil
}

func startService(name, bin string, args ...string) {
	if _, err := os.Stat(bin); err != nil {
		fmt.Printf("  →  %-25s não instalado\n", name)
		return
	}
	cmd := exec.Command(bin, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Start()
	fmt.Printf("  →  %-25s iniciado\n", name)
}

var daemonCmd = &cobra.Command{
	Use:    "daemon",
	Short:  "Run dashboard server (internal)",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		e := detect.Detect()
		DwytBin = e.DwytBin
		DwytHome = e.DwytHome
		srv := server.New(2737, DwytBin, DwytHome)
		return srv.Start()
	},
}

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop all DWYT services",
	RunE: func(cmd *cobra.Command, args []string) error {
		exec.Command("pkill", "-f", "dwyt daemon").Run()
		exec.Command("pkill", "-f", "codebase-memory-mcp").Run()
		exec.Command("pkill", "-f", "headroom proxy").Run()
		fmt.Println("  ✓ All services stopped")
		return nil
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Quick status of all tools",
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
	Short: "Show version",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("dwyt v3.1.0 — Don't Waste Your Tokens")
		return nil
	},
}
