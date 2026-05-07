package root

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/fvmoraes/dwyt/internal/brain"
	"github.com/fvmoraes/dwyt/internal/detect"
	"github.com/fvmoraes/dwyt/internal/health"
	"github.com/fvmoraes/dwyt/internal/log"
	"github.com/fvmoraes/dwyt/internal/mcpregistry"
	"github.com/fvmoraes/dwyt/internal/security"
	"github.com/fvmoraes/dwyt/internal/server"
	"github.com/fvmoraes/dwyt/internal/status"
	"github.com/spf13/cobra"
)

var daemonCmd = &cobra.Command{
	Use:    "daemon",
	Short:  "Run dashboard server (internal)",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		log.Info("daemon process starting")
		srv := server.New(2737, DwytBin, DwytHome, version)
		return srv.Start()
	},
}

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop all DWYT services",
	RunE: func(cmd *cobra.Command, args []string) error {
		health.StopAll()
		exe, _ := os.Executable()
		exec.Command("pkill", "-f", exe+" daemon").Run()
		exec.Command("pkill", "-f", "dwyt.*daemon").Run()
		exec.Command("pkill", "-f", "codebase-memory-mcp").Run()
		exec.Command("pkill", "-f", "headroom proxy").Run()
		log.Info("all services stopped")
		fmt.Println("  \u2713 Servi\u00E7os parados")
		return nil
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Quick status of all tools",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		var pm *brain.ProjectObsidian
		if projectMemory, err := brain.NewProjectObsidian(DwytHome, cwd); err == nil {
			pm = projectMemory
		}

		s := status.PollAll(DwytBin, pm != nil)
		fmt.Printf("\n  DWYT Status:\n")
		for _, t := range s.Tools {
			fmt.Printf("  %s %-22s %s\n", toolStatusIcon(t), t.Name, t.Details)
		}

		if pm != nil {
			stats := pm.Stats()
			if files, ok := stats["total_files"].(int); ok && files > 0 {
				fmt.Printf("\n  \U0001F9E0 Obsidian: %d files for %s\n", files, stats["project_name"])
				if summary, ok := stats["summary"].(string); ok && summary != "" {
					if len(summary) > 120 {
						summary = summary[:117] + "..."
					}
					fmt.Printf("     %s\n", summary)
				}
			}
		}
		return nil
	},
}

func toolStatusIcon(t status.ToolStatus) string {
	if t.Healthy {
		return "\U0001F7E2"
	}

	state := t.Status
	if state == "" {
		state = t.State
	}

	switch state {
	case status.StateOnline, status.StateInstalled:
		return "\U0001F7E2"
	case status.StateStarting, status.StateOffline, status.StateInactive, status.StatePortOpenNoHealth:
		return "\U0001F7E1"
	}

	if t.Running {
		return "\U0001F7E1"
	}
	return "\U0001F534"
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("dwyt %s \u2014 Don't Waste Your Tokens\n", version)
		return nil
	},
}

var reinstallCmd = &cobra.Command{
	Use:   "reinstall",
	Short: "Remove data dir and reinstall everything",
	RunE: func(cmd *cobra.Command, args []string) error {
		e := detect.Detect()
		if !security.IsSafeHome(e.DwytHome) {
			return fmt.Errorf("unsafe DWYT home path: %s (refusing to operate)", e.DwytHome)
		}
		fmt.Printf("  Apagando %s...\n", e.DwytHome)
		security.CleanHome(e.DwytHome)
		log.Info("reinstall: cleaned dwyt home (vaults preserved)", log.Fields{"path": e.DwytHome})
		fmt.Printf("  \u2713 Removido. Execute 'dwyt' para reinstalar via UI.\n")
		return nil
	},
}

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove all DWYT tools, data and config",
	RunE: func(cmd *cobra.Command, args []string) error {
		e := detect.Detect()
		home, _ := os.UserHomeDir()
		fmt.Printf("\n  \u2554\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2557\n")
		fmt.Printf("  \u2551  DWYT \u2014 Uninstall                   \u2551\n")
		fmt.Printf("  \u255A\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u255D\n\n")
		stopAllProcesses()
		cleanDWYTHome(e)
		removeSymlinks(home)
		removeRTKData(home)
		removeHeadroomData(home)
		removeCodebaseData(home, e)
		if runtime.GOOS == "windows" {
			removeFromWindowsUserPath(e.DwytBin)
			fmt.Println("  \u2713 Removed from Windows PATH")
		}
		cleanShellRC(e)
		if runtime.GOOS == "windows" {
			cleanPowerShellProfile(home)
		}
		fmt.Printf("\n  \u2713 DWYT fully uninstalled.\n")
		fmt.Printf("  \u2139  Restart your terminal to apply shell changes.\n\n")
		return nil
	},
}

func stopAllProcesses() {
	fmt.Println("  → Stopping all processes...")
	health.StopAll()
	exe, _ := os.Executable()
	exec.Command("pkill", "-f", exe+" daemon").Run()
	exec.Command("pkill", "-f", "dwyt.*daemon").Run()
	exec.Command("pkill", "-f", "codebase-memory-mcp").Run()
	exec.Command("pkill", "-f", "headroom proxy").Run()
	exec.Command("pkill", "-f", "headroom").Run()
	exec.Command("pkill", "-f", "rtk").Run()
	time.Sleep(500 * time.Millisecond)
	fmt.Println("  ✓ Processes stopped")
}

var syncCmd = &cobra.Command{
	Use:   "sync [what]",
	Short: "Sync configurations to AI agents",
	Long:  "Sync tool configurations. Use 'dwyt sync mcp' to configure MCP for Claude Desktop and VSCode.",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		what := ""
		if len(args) > 0 {
			what = args[0]
		}
		switch what {
		case "mcp":
			return syncMCPAll()
		case "":
			return syncMCPAll()
		default:
			return fmt.Errorf("unknown sync target: %s (use 'mcp')", what)
		}
	},
}

func syncMCPAll() error {
	reg, err := mcpregistry.Load()
	if err != nil {
		return fmt.Errorf("mcp registry: %w", err)
	}
	cwd, _ := os.Getwd()
	if err := reg.ConfigureMCP(cwd); err != nil {
		return fmt.Errorf("mcp configure: %w", err)
	}
	fmt.Println("\n  ✓ MCP configs synced for Claude Desktop and VSCode")
	fmt.Printf("  Registry: %s\n\n", filepath.Join(os.Getenv("HOME"), ".dwyt", "config", "mcp-registry.json"))
	return nil
}
