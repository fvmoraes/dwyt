package root

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/fvmoraes/dwyt/internal/detect"
	"github.com/fvmoraes/dwyt/internal/env"
	"github.com/fvmoraes/dwyt/internal/health"
	"github.com/fvmoraes/dwyt/internal/install"
	"github.com/fvmoraes/dwyt/internal/log"
	"github.com/fvmoraes/dwyt/internal/server"
	"github.com/fvmoraes/dwyt/internal/status"

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

	// ── Phase 1: env init (fast, always safe) ─────────────────────────────────
	fmt.Printf("  Phase 1/3: environment\n")
	env.Init(e.DwytHome, e.DwytBin, e.DwytData, e.ShellRC, e.LoginRC)
	install.Wrappers(e.DwytBin, e.DwytHome)

	// ── Phase 2: check if daemon is already running ───────────────────────────
	fmt.Printf("  Phase 2/3: daemon\n")
	if daemonOK := waitForDaemon(3 * time.Second); daemonOK {
		fmt.Printf("  ✓ Daemon already running — switching project\n")
		if err := switchProject(projectPath); err == nil {
			fmt.Printf("  Project: %s\n\n", projectPath)
			fmt.Printf("  ✓ Dashboard → http://localhost:2737\n\n")
			openBrowserURL("http://localhost:2737")
			return nil
		}
		fmt.Printf("  ! Project switch failed, restarting daemon\n")
	}

	// ── Phase 3: start services with healthchecks ────────────────────────────
	fmt.Printf("  Phase 3/3: services\n")
	startServicesWithHealth(e.DwytBin)

	// ── Spawn daemon process ─────────────────────────────────────────────────
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
	)
	setDaemonAttr(daemon)
	if err := daemon.Start(); err != nil {
		log.Error("daemon failed to start", log.Fields{"error": err.Error()})
		fmt.Printf("\n  ✗ Dashboard failed to start: %v\n", err)
		return err
	}
	log.Info("daemon spawned", log.Fields{"pid": daemon.Process.Pid})

	// ── Wait for daemon readiness ────────────────────────────────────────────
	if waitForDaemon(15 * time.Second) {
		fmt.Printf("\n  ✓ Dashboard → http://localhost:2737\n")
		fmt.Printf("  Stop: dwyt stop\n\n")
		openBrowserURL("http://localhost:2737")
	} else {
		fmt.Printf("\n  ⚠ Dashboard may still be starting at http://localhost:2737\n")
		fmt.Printf("  Stop: dwyt stop\n\n")
		openBrowserURL("http://localhost:2737")
	}

	return nil
}

func banner() {
	fmt.Printf("\n  ╔══════════════════════════════════════╗\n")
	fmt.Printf("  ║  DWYT — Don't Waste Your Tokens     ║\n")
	fmt.Printf("  ╚══════════════════════════════════════╝\n\n")
}

func waitForDaemon(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 500 * time.Millisecond}
	url := "http://127.0.0.1:2737/api/status"

	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return true
			}
		}
		time.Sleep(500 * time.Millisecond)
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

func startServicesWithHealth(dwytBin string) {
	log.Info("Phase 3: starting services")

	codebaseBin := filepath.Join(dwytBin, "codebase-memory-mcp")
	if _, err := os.Stat(codebaseBin); err == nil {
		fmt.Printf("  →  codebase-memory-mcp     starting...\n")
		check, err := health.StartService(
			"codebase-memory-mcp",
			codebaseBin,
			"http://127.0.0.1:9749/health",
			"--ui=true", "--port=9749",
		)
		if err != nil || !check.Healthy {
			log.Warn("codebase-memory-mcp failed healthcheck", log.Fields{"error": fmt.Sprintf("%v", err)})
			fmt.Printf("  →  codebase-memory-mcp     ⚠ started but not healthy\n")
		} else {
			fmt.Printf("  →  codebase-memory-mcp     ✓ ready (port 9749)\n")
		}
	} else {
		fmt.Printf("  →  codebase-memory-mcp     not installed (install via UI)\n")
	}

	headroomBin := filepath.Join(dwytBin, "headroom")
	if _, err := os.Stat(headroomBin); err == nil {
		fmt.Printf("  →  headroom                starting...\n")
		check, err := health.StartService(
			"headroom",
			headroomBin,
			"http://127.0.0.1:8787/health",
			"proxy", "--port", "8787",
		)
		if err != nil || !check.Healthy {
			log.Warn("headroom failed healthcheck", log.Fields{"error": fmt.Sprintf("%v", err)})
			fmt.Printf("  →  headroom                ⚠ started but not healthy\n")
		} else {
			fmt.Printf("  →  headroom                ✓ ready (port 8787)\n")
		}
	} else {
		fmt.Printf("  →  headroom                not installed (install via UI)\n")
	}

	for _, bin := range []string{"rtk", "memstack"} {
		if _, err := os.Stat(filepath.Join(dwytBin, bin)); err == nil {
			fmt.Printf("  →  %-24s ✓ available\n", bin)
		} else {
			fmt.Printf("  →  %-24s not installed (install via UI)\n", bin)
		}
	}
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
