package root

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/fvmoraes/dwyt/internal/detect"
	"github.com/fvmoraes/dwyt/internal/env"
	"github.com/fvmoraes/dwyt/internal/health"
	"github.com/fvmoraes/dwyt/internal/log"
	"github.com/fvmoraes/dwyt/internal/brain"
	"github.com/fvmoraes/dwyt/internal/server"
	"github.com/fvmoraes/dwyt/internal/status"
	"github.com/fvmoraes/dwyt/internal/workspace"

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
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		projectPath := ""
		if len(args) == 1 {
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

	log.SetOutput(filepath.Join(DwytHome, "dwyt.log"))

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

	cwd := getCWD()
	if projectPath == "" {
		projectPath = cwd
	}

	log.Info("DWYT startup", log.Fields{"project": projectPath, "home": DwytHome})

	banner()
	fmt.Printf("  Project: %s\n", projectPath)

	// ── Phase 1: env init (fast, always safe) ─────────────────────────────────
	env.Init(e.DwytHome, e.DwytBin, e.DwytData, e.ShellRC, e.LoginRC)

	if !brain.ObsidianInstalled() {
		fmt.Println("\n  ✗ Obsidian is required but not found.")
		fmt.Println("  Install from: https://obsidian.md/download")
		fmt.Println("  After installing, run 'dwyt .' again.\n")
		os.Exit(1)
	}

	// ── Check if daemon is already running ────────────────────────────────────
	// Quick probe (500ms) — if daemon responds, just switch project context
	if daemonOK := probeDaemon(); daemonOK {
		if err := switchProject(projectPath); err == nil {
			workspace.Touch(projectPath)
			fmt.Printf("  ✓ Dashboard → http://127.0.0.1:2737  (already running)\n")
			fmt.Printf("  ✓ Project context updated\n\n")
			openBrowserURL("http://127.0.0.1:2737/#/dashboard?project=" + url.PathEscape(projectPath))
			return nil
		}
		// Switch failed — kill stale daemon and restart
		log.Warn("daemon probe ok but switch failed, restarting")
		exec.Command("pkill", "-f", "dwyt.*daemon").Run()
		time.Sleep(300 * time.Millisecond)
	}

	// ── Phase 2: start services (fire-and-forget in background) ───────────────
	headroomPort := startServicesAsync(e.DwytBin)

	// ── Marks available tools ──────────────────────────────────────────────────
	for _, bin := range []string{"rtk"} {
		if _, err := os.Stat(filepath.Join(e.DwytBin, bin)); err == nil {
			fmt.Printf("  →  %-25s available\n", bin)
		} else {
			fmt.Printf("  →  %-25s not installed (install via UI)\n", bin)
		}
	}

	// ── Spawn daemon process (detached, non-blocking) ────────────────────────
	exe, _ := os.Executable()
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	daemon := exec.Command(exe, "daemon")
	daemon.Stdout = nil
	daemon.Stderr = nil
	daemon.Stdin = nil
	daemon.Env = append(os.Environ(),
		"DWYT_START_CWD="+cwd,
		"DWYT_PROJECT="+projectPath,
		fmt.Sprintf("DWYT_HEADROOM_PORT=%d", headroomPort),
	)
	setDaemonAttr(daemon)
	if err := daemon.Start(); err != nil {
		log.Error("daemon failed to start", log.Fields{"error": err.Error()})
		fmt.Printf("  ✗ Dashboard failed to start: %v\n", err)
		return err
	}
	log.Info("daemon spawned", log.Fields{"pid": daemon.Process.Pid})

	// ── Phase 3: healthcheck daemon before declaring success ──────────────────
	if !waitForDaemon(3*time.Second, 300*time.Millisecond) {
		log.Error("daemon healthcheck timed out", log.Fields{"pid": daemon.Process.Pid})
		daemon.Process.Kill()
		fmt.Printf("  ✗ Dashboard failed to respond — see %s\n", filepath.Join(e.DwytHome, "dwyt.log"))
		return fmt.Errorf("daemon healthcheck timeout")
	}

	workspace.Touch(projectPath)
	fmt.Printf("  ✓ Dashboard → http://127.0.0.1:2737\n")
	fmt.Printf("  Stop: dwyt stop\n\n")
	openBrowserURL("http://127.0.0.1:2737/#/dashboard?project=" + url.PathEscape(projectPath))
	return nil
}

func banner() {
	fmt.Printf("\n  ╔══════════════════════════════════════╗\n")
	fmt.Printf("  ║  DWYT — Don't Waste Your Tokens     ║\n")
	fmt.Printf("  ╚══════════════════════════════════════╝\n\n")
}

func probeDaemon() bool {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get("http://127.0.0.1:2737/api/health")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

func waitForDaemon(timeout, interval time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if probeDaemon() {
			return true
		}
		time.Sleep(interval)
	}
	return false
}

func switchProject(projectPath string) error {
	body := fmt.Sprintf(`{"path":%q}`, projectPath)
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Post(
		"http://127.0.0.1:2737/api/project/switch",
		"application/json",
		strings.NewReader(body),
	)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("switch failed: %d", resp.StatusCode)
	}
	return nil
}

func startServicesAsync(dwytBin string) int {
	codebaseBin := filepath.Join(dwytBin, "codebase-memory-mcp")
	if _, err := os.Stat(codebaseBin); err == nil {
		fmt.Printf("  →  codebase-memory-mcp     available (index on demand)\n")
	} else {
		fmt.Printf("  →  codebase-memory-mcp     not installed (install via UI)\n")
	}

	headroomPort := findFreePort(8787)
	headroomBin := filepath.Join(dwytBin, "headroom")
	if _, err := os.Stat(headroomBin); err == nil {
		fmt.Printf("  →  headroom                will start on port %d via daemon\n", headroomPort)
	} else {
		fmt.Printf("  →  headroom                not installed (install via UI)\n")
	}

	fmt.Printf("  →  brain                   available (Obsidian)\n")

	return headroomPort
}

func findFreePort(defaultPort int) int {
	port := defaultPort
	for i := 0; i < 5; i++ {
		if !health.ProbePort(port) {
			return port
		}
		port++
	}
	return defaultPort
}

func openBrowserURL(url string) {
	switch runtime.GOOS {
	case "linux":
		exec.Command("xdg-open", url).Start()
	case "darwin":
		exec.Command("open", url).Start()
	case "windows":
		exec.Command("cmd", "/c", "start", url).Start()
	}
}

func getCWD() string {
	d, _ := os.Getwd()
	return d
}

var daemonCmd = &cobra.Command{
	Use:    "daemon",
	Short:  "Run dashboard server (internal)",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		log.Info("daemon process starting")
		srv := server.New(2737, DwytBin, DwytHome)
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

		// Show memory status for current directory
		cwd, _ := os.Getwd()
		if pm, err := brain.NewProjectBrain(DwytHome, cwd); err == nil {
			stats := pm.Stats()
			if files, ok := stats["total_files"].(int); ok && files > 0 {
				fmt.Printf("\n  🧠 Brain: %d files for %s\n", files, stats["project_name"])
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
		log.Info("reinstall: removed dwyt home", log.Fields{"path": e.DwytHome})
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
		log.Info("uninstall: removed dwyt home", log.Fields{"path": e.DwytHome})
		fmt.Println("  ✓ Desinstalação concluída.")
		return nil
	},
}
