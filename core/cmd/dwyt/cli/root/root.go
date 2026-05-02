package root

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/DeusData/core/internal/detect"
	"github.com/DeusData/core/internal/env"
	"github.com/DeusData/core/internal/install"
	"github.com/DeusData/core/internal/server"
	"github.com/DeusData/core/internal/status"

	"github.com/spf13/cobra"
)

var (
	DwytBin  string
	DwytHome string
	DwytData string
)

var Cmd = &cobra.Command{
	Use:   "dwyt [path]",
	Short: "DWYT — Don't Waste Your Tokens",
	Long:  "DWYT v3.1 — UI-First orchestrator. Use 'dwyt .' to open in current directory.",
	// Accept 0 or 1 positional arg (the project path, like `dwyt .` or `dwyt /path/to/repo`)
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		projectPath := ""
		if len(args) == 1 {
			// Resolve the path — "." becomes the absolute cwd
			abs, err := filepath.Abs(args[0])
			if err == nil {
				projectPath = abs
			} else {
				projectPath = args[0]
			}
		}
		return runDefault(projectPath)
	},
}

func init() {
	e := detect.Detect()
	DwytHome = e.DwytHome
	DwytBin  = e.DwytBin
	DwytData = e.DwytData

	if h := os.Getenv("DWYT_HOME"); h != "" {
		DwytHome = h
		DwytBin  = DwytHome + "/bin"
		DwytData = DwytHome + "/data"
	}

	Cmd.AddCommand(stopCmd)
	Cmd.AddCommand(statusCmd)
	Cmd.AddCommand(versionCmd)
	Cmd.AddCommand(reinstallCmd)
	Cmd.AddCommand(uninstallCmd)
	Cmd.AddCommand(daemonCmd)
}

func getHome() string {
	h, _ := os.UserHomeDir()
	if h == "" {
		h = os.Getenv("HOME")
	}
	return h
}

func runDefault(projectPath string) error {
	e := detect.Detect()

	// If no path argument, use cwd
	cwd := getCWD()
	if projectPath == "" {
		projectPath = cwd
	}

	// Ensure env.sh + shell RC + symlinks on PATH
	env.Init(e.DwytHome, e.DwytBin, e.DwytData, e.ShellRC, e.LoginRC)

	// No-op now — wrappers removed, all control via UI
	install.Wrappers(e.DwytBin, e.DwytHome)

	fmt.Printf("\n  ╔══════════════════════════════════════╗\n")
	fmt.Printf("  ║  DWYT — Don't Waste Your Tokens     ║\n")
	fmt.Printf("  ╚══════════════════════════════════════╝\n\n")
	fmt.Printf("  Projeto: %s\n\n", projectPath)

	// Start services (controlled by UI — just launch if already installed)
	startService("codebase-memory-mcp", filepath.Join(e.DwytBin, "codebase-memory-mcp"), "--ui=true", "--port=9749")
	startService("headroom", filepath.Join(e.DwytBin, "headroom"), "proxy", "--port", "8787")

	for _, bin := range []string{"rtk", "memstack"} {
		if _, err := os.Stat(filepath.Join(e.DwytBin, bin)); err == nil {
			fmt.Printf("  →  %-25s disponível\n", bin)
		}
	}

	// Fork daemon — pass both cwd and explicit project path
	exe, _ := os.Executable()
	daemon := exec.Command(exe, "daemon")
	daemon.Stdout = nil
	daemon.Stderr = nil
	daemon.Stdin = nil
	daemon.Env = append(os.Environ(),
		"DWYT_START_CWD="+cwd,
		"DWYT_PROJECT="+projectPath,
	)
	setDaemonAttr(daemon)
	daemon.Start()

	fmt.Printf("\n  ✓ Dashboard → http://localhost:2737\n")
	fmt.Printf("  Parar: dwyt stop\n\n")
	return nil
}

func getCWD() string {
	d, _ := os.Getwd()
	return d
}

func startService(name, bin string, args ...string) {
	if _, err := os.Stat(bin); err != nil {
		fmt.Printf("  →  %-25s não instalado (instale via UI)\n", name)
		return
	}
	cmd := exec.Command(bin, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	setProcAttr(cmd)
	cmd.Start()
	fmt.Printf("  →  %-25s iniciado\n", name)
}

var daemonCmd = &cobra.Command{
	Use:    "daemon",
	Short:  "Run dashboard server (internal)",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		srv := server.New(2737, DwytBin, DwytHome)
		return srv.Start()
	},
}

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop all DWYT services",
	RunE: func(cmd *cobra.Command, args []string) error {
		exe, _ := os.Executable()
		exec.Command("pkill", "-f", exe+" daemon").Run()
		exec.Command("pkill", "-f", "dwyt.*daemon").Run()
		exec.Command("pkill", "-f", "codebase-memory-mcp").Run()
		exec.Command("pkill", "-f", "headroom proxy").Run()
		fmt.Println("  ✓ Serviços parados")
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
			icon := "🔴"
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

var reinstallCmd = &cobra.Command{
	Use:   "reinstall",
	Short: "Remove data dir and reinstall everything",
	RunE: func(cmd *cobra.Command, args []string) error {
		e := detect.Detect()
		fmt.Printf("  Apagando %s...\n", e.DwytHome)
		os.RemoveAll(e.DwytHome)
		fmt.Printf("  ✓ Removido. Execute 'dwyt' para reinstalar via UI.\n")
		return nil
	},
}

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove all DWYT tools and config",
	RunE: func(cmd *cobra.Command, args []string) error {
		e := detect.Detect()
		os.RemoveAll(e.DwytHome)
		fmt.Println("  ✓ Desinstalação concluída.")
		return nil
	},
}
