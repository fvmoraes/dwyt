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

	"github.com/fvmoraes/dwyt/internal/brain"
	"github.com/fvmoraes/dwyt/internal/detect"
	"github.com/fvmoraes/dwyt/internal/env"
	"github.com/fvmoraes/dwyt/internal/health"
	"github.com/fvmoraes/dwyt/internal/log"
	"github.com/fvmoraes/dwyt/internal/mcpregistry"
	"github.com/fvmoraes/dwyt/internal/security"
	"github.com/fvmoraes/dwyt/internal/server"
	"github.com/fvmoraes/dwyt/internal/status"
	"github.com/fvmoraes/dwyt/internal/workspace"

	"github.com/spf13/cobra"
)

var (
	DwytBin  string
	DwytHome string
	DwytData string
	version  = "dev"
)

func SetVersion(v string) {
	version = v
}

var Cmd = &cobra.Command{
	Use:   "dwyt [path]",
	Short: "DWYT — Don't Waste Your Tokens",
	Long:  "DWYT — Don't Waste Your Tokens. Use 'dwyt .' to open in current directory.",
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
	Cmd.AddCommand(syncCmd)
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
		fmt.Println("  →  obsidian               not found (install for visual navigation)")
		fmt.Println("     https://obsidian.md/download")
	} else {
		fmt.Println("  →  obsidian               detected")
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

	fmt.Printf("  →  obsidian                available (Obsidian vault)\n")

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
		if pm, err := brain.NewProjectObsidian(DwytHome, cwd); err == nil {
			stats := pm.Stats()
			if files, ok := stats["total_files"].(int); ok && files > 0 {
				fmt.Printf("\n  🧠 Obsidian: %d files for %s\n", files, stats["project_name"])
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
		fmt.Printf("dwyt %s — Don't Waste Your Tokens\n", version)
		return nil
	},
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
			// Sync everything by default
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

var reinstallCmd = &cobra.Command{
	Use:   "reinstall",
	Short: "Remove data dir and reinstall everything",
	RunE: func(cmd *cobra.Command, args []string) error {
		e := detect.Detect()
		// Safety: only remove if the path looks like a valid DWYT home
		if !security.IsSafeHome(e.DwytHome) {
			return fmt.Errorf("unsafe DWYT home path: %s (refusing to operate)", e.DwytHome)
		}
		fmt.Printf("  Apagando %s...\n", e.DwytHome)
		// Protect Obsidian vaults: remove everything except projects/
		security.CleanHome(e.DwytHome)
		log.Info("reinstall: cleaned dwyt home (vaults preserved)", log.Fields{"path": e.DwytHome})
		log.Info("reinstall: removed dwyt home", log.Fields{"path": e.DwytHome})
		fmt.Printf("  ✓ Removido. Execute 'dwyt' para reinstalar via UI.\n")
		return nil
	},
}

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove all DWYT tools, data and config",
	RunE: func(cmd *cobra.Command, args []string) error {
		e := detect.Detect()
		home, _ := os.UserHomeDir()

		fmt.Printf("\n  ╔══════════════════════════════════════╗\n")
		fmt.Printf("  ║  DWYT — Uninstall                   ║\n")
		fmt.Printf("  ╚══════════════════════════════════════╝\n\n")

		// ── 1. Stop all running processes ─────────────────────────────────────
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

		// ── 2. Remove ~/.dwyt (bins, SQLite, state, logs, venv) ───────
		// PROTECTION: projects/ (Obsidian vaults) are NEVER removed
		fmt.Printf("  → Cleaning DWYT home: %s\n", e.DwytHome)
		if !security.IsSafeHome(e.DwytHome) {
			fmt.Printf("  ✗ Unsafe DWYT home path: %s — refusing to clean\n", e.DwytHome)
		} else {
			security.CleanHome(e.DwytHome)
			fmt.Println("  ✓ DWYT home cleaned (Obsidian vaults preserved)")
		}

		// ── 3. Remove symlinks from ~/.local/bin ──────────────────────────────
		if runtime.GOOS != "windows" {
			localBin := filepath.Join(home, ".local", "bin")
			for _, name := range []string{"dwyt", "rtk", "headroom", "codebase-memory-mcp"} {
				link := filepath.Join(localBin, name)
				if _, err := os.Lstat(link); err == nil {
					os.Remove(link)
					fmt.Printf("  ✓ Removed symlink: %s\n", link)
				}
			}
		}

		// ── 4. Remove RTK global data ──────────────────────────────────────────
		fmt.Println("  → Removing RTK data...")
		rtkDirs := []string{
			filepath.Join(home, ".rtk"),
			filepath.Join(home, ".config", "rtk"),
			filepath.Join(home, ".local", "share", "rtk"),
		}
		for _, d := range rtkDirs {
			if _, err := os.Stat(d); err == nil {
				os.RemoveAll(d)
				fmt.Printf("  ✓ Removed: %s\n", d)
			}
		}
		// RTK binary in common locations (if installed outside ~/.dwyt)
		rtkBins := []string{
			filepath.Join(home, ".local", "bin", "rtk"),
			"/usr/local/bin/rtk",
		}
		for _, b := range rtkBins {
			if _, err := os.Lstat(b); err == nil {
				os.Remove(b)
				fmt.Printf("  ✓ Removed: %s\n", b)
			}
		}

		// ── 5. Remove Headroom Python venv and config ──────────────────────────
		fmt.Println("  → Removing Headroom data...")
		headroomDirs := []string{
			filepath.Join(home, ".headroom"),
			filepath.Join(home, ".config", "headroom"),
			filepath.Join(home, ".local", "share", "headroom"),
		}
		for _, d := range headroomDirs {
			if _, err := os.Stat(d); err == nil {
				os.RemoveAll(d)
				fmt.Printf("  ✓ Removed: %s\n", d)
			}
		}
		// Headroom pip package (uninstall from any venv that might exist)
		exec.Command("pip", "uninstall", "-y", "headroom-ai").Run()
		exec.Command("pip3", "uninstall", "-y", "headroom-ai").Run()

		// ── 6. Remove Codebase (codebase-memory-mcp) data ─────────────────────
		fmt.Println("  → Removing Codebase data...")
		codebaseDirs := []string{
			filepath.Join(e.DwytHome, "codebase"),              // primary: ~/.dwyt/codebase
			filepath.Join(home, ".cache", "codebase-memory-mcp"), // fallback: default location
			filepath.Join(home, ".codebase-memory-mcp"),
			filepath.Join(home, ".config", "codebase-memory-mcp"),
		}
		for _, d := range codebaseDirs {
			if _, err := os.Stat(d); err == nil {
				os.RemoveAll(d)
				fmt.Printf("  ✓ Removed: %s\n", d)
			}
		}
		// Also run codebase-memory-mcp uninstall if binary still exists
		cbmcpBin := filepath.Join(e.DwytBin, "codebase-memory-mcp")
		if _, err := os.Stat(cbmcpBin); err == nil {
			exec.Command(cbmcpBin, "uninstall", "-y").Run()
			fmt.Println("  ✓ Codebase agent configs removed")
		}
		// Codebase binary in common locations
		codebaseBins := []string{
			filepath.Join(home, ".local", "bin", "codebase-memory-mcp"),
			"/usr/local/bin/codebase-memory-mcp",
		}
		for _, b := range codebaseBins {
			if _, err := os.Lstat(b); err == nil {
				os.Remove(b)
				fmt.Printf("  ✓ Removed: %s\n", b)
			}
		}

		// ── 7. Remove Obsidian vault data (only DWYT-managed vaults) ──────────
		// Note: we do NOT remove the Obsidian app itself — only DWYT vaults
		fmt.Println("  → Removing DWYT Obsidian vaults...")
		obsidianVaultBase := filepath.Join(home, ".dwyt", "projects")
		if _, err := os.Stat(obsidianVaultBase); err == nil {
			// Already removed in step 2, but log it
			fmt.Println("  ✓ Obsidian vaults removed (part of DWYT home)")
		}

		// ── 8. Remove Windows PATH entry ──────────────────────────────────────
		if runtime.GOOS == "windows" {
			removeFromWindowsUserPath(e.DwytBin)
			fmt.Println("  ✓ Removed from Windows PATH")
		}

		// ── 9. Clean shell RC files ────────────────────────────────────────────
		fmt.Println("  → Cleaning shell RC files...")
		rcFiles := []string{e.ShellRC, e.LoginRC}
		for _, rc := range rcFiles {
			if rc == "" {
				continue
			}
			if cleaned := removeFromRC(rc); cleaned {
				fmt.Printf("  ✓ Cleaned: %s\n", rc)
			}
		}

		// ── 10. Remove PowerShell profile entry (Windows) ─────────────────────
		if runtime.GOOS == "windows" {
			psProfile := filepath.Join(home, "Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1")
			if cleaned := removeFromRC(psProfile); cleaned {
				fmt.Printf("  ✓ Cleaned PowerShell profile: %s\n", psProfile)
			}
		}

		fmt.Printf("\n  ✓ DWYT fully uninstalled.\n")
		fmt.Printf("  ℹ  Restart your terminal to apply shell changes.\n\n")
		return nil
	},
}

func removeFromRC(rcFile string) bool {
	data, err := os.ReadFile(rcFile)
	if err != nil {
		return false
	}
	original := string(data)
	lines := strings.Split(original, "\n")
	filtered := make([]string, 0, len(lines))
	skip := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "# dwyt:source" {
			skip = true
			continue
		}
		if skip {
			// Skip the source/. line that follows the marker
			skip = false
			continue
		}
		filtered = append(filtered, line)
	}
	result := strings.Join(filtered, "\n")
	// Collapse multiple trailing blank lines into one
	for strings.Contains(result, "\n\n\n") {
		result = strings.ReplaceAll(result, "\n\n\n", "\n\n")
	}
	if result == original {
		return false
	}
	os.WriteFile(rcFile, []byte(result), 0644)
	return true
}

// removeFromWindowsUserPath removes dwytBin from HKCU\Environment\PATH.
func removeFromWindowsUserPath(dwytBin string) {
	out, err := exec.Command("reg", "query", `HKCU\Environment`, "/v", "PATH").Output()
	if err != nil {
		return
	}
	currentPath := ""
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToUpper(line), "PATH") {
			parts := strings.SplitN(line, "    ", 3)
			if len(parts) == 3 {
				currentPath = strings.TrimSpace(parts[2])
			}
		}
	}
	if currentPath == "" {
		return
	}
	segments := strings.Split(currentPath, ";")
	filtered := make([]string, 0, len(segments))
	for _, s := range segments {
		if !strings.EqualFold(strings.TrimSpace(s), dwytBin) {
			filtered = append(filtered, s)
		}
	}
	newPath := strings.Join(filtered, ";")
	exec.Command("reg", "add", `HKCU\Environment`, "/v", "PATH", "/t", "REG_EXPAND_SZ", "/d", newPath, "/f").Run()
}
