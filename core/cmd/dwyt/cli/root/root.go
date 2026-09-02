package root

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/fvmoraes/dwyt/internal/brain"
	"github.com/fvmoraes/dwyt/internal/detect"
	"github.com/fvmoraes/dwyt/internal/env"
	"github.com/fvmoraes/dwyt/internal/health"
	"github.com/fvmoraes/dwyt/internal/integrate"
	"github.com/fvmoraes/dwyt/internal/log"
	"github.com/fvmoraes/dwyt/internal/platform"
	"github.com/fvmoraes/dwyt/internal/procutil"
	"github.com/fvmoraes/dwyt/internal/workspace"
	"github.com/spf13/cobra"
)

var (
	DwytBin  string
	DwytHome string
	DwytData string
	version  = "dev"
)

func SetVersion(v string) { version = v }

const (
	daemonHealthURL             = "http://127.0.0.1:2737/api/health"
	defaultDaemonHealthTimeout  = 60 * time.Second
	windowsDaemonHealthTimeout  = 120 * time.Second
	daemonHealthProbeTimeout    = 2 * time.Second
	daemonHealthPollingInterval = 500 * time.Millisecond
)

var Cmd = &cobra.Command{
	Use:   "dwyt [path]",
	Short: "DWYT \u2014 Don't Waste Your Tokens",
	Long:  "DWYT \u2014 Don't Waste Your Tokens. Use 'dwyt .' to open in current directory.",
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
	DwytBin = e.DwytBin
	DwytData = e.DwytData

	if h := os.Getenv("DWYT_HOME"); h != "" {
		DwytHome = h
		DwytBin = DwytHome + "/bin"
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

func runDefault(projectPath string) error {
	e := detect.Detect()
	dwytHome, dwytBin, dwytData := effectiveDWYTPaths(e)
	cwd := getCWD()
	if projectPath == "" {
		projectPath = cwd
	}
	log.Info("DWYT startup", log.Fields{"project": projectPath, "home": DwytHome})

	banner()
	fmt.Printf("  Project: %s\n", projectPath)

	env.Init(dwytHome, dwytBin, dwytData, e.ShellRC, e.LoginRC)

	if err := integrate.EnsureGitignoreBlock(projectPath); err != nil {
		log.Warn("gitignore block update failed", log.Fields{"error": err.Error()})
	}

	if !brain.ObsidianInstalled() {
		fmt.Println("  \u2192  obsidian               not found (install for visual navigation)")
		fmt.Println("     https://obsidian.md/download")
	} else {
		fmt.Println("  \u2192  obsidian               detected")
	}

	if daemon := probeDaemon(); daemon.OK {
		if daemonVersionNeedsRestart(daemon.Version) {
			if daemon.Version == "" {
				fmt.Printf("  \u2192  Dashboard daemon version unknown; restarting with %s\n", normalizeDaemonVersion(version))
			} else {
				fmt.Printf("  \u2192  Dashboard daemon %s found; restarting with %s\n", normalizeDaemonVersion(daemon.Version), normalizeDaemonVersion(version))
			}
			log.Info("daemon version mismatch, restarting", log.Fields{"daemon_version": daemon.Version, "cli_version": version})
			stopDaemonProcess()
			time.Sleep(300 * time.Millisecond)
		} else if err := switchProject(projectPath); err == nil {
			workspace.Touch(projectPath)
			fmt.Printf("  \u2713 Dashboard \u2192 http://localhost:2737  (already running)\n")
			fmt.Printf("  \u2713 Project context updated\n\n")
			ensureKiroPowerIfEnabled(projectPath)
			openBrowserURL("http://localhost:2737/#/dashboard?project=" + url.PathEscape(projectPath))
			return nil
		} else {
			log.Warn("daemon probe ok but switch failed, restarting")
			stopDaemonProcess()
			time.Sleep(300 * time.Millisecond)
		}
	}

	headroomPort := startServicesAsync(dwytBin)
	if err := env.SetHeadroomPort(dwytHome, headroomPort); err != nil {
		log.Warn("failed to publish selected Headroom port", log.Fields{"port": headroomPort, "error": err.Error()})
	}

	for _, bin := range []string{"rtk"} {
		if _, err := os.Stat(platform.DWYTLauncherPath(dwytBin, bin)); err == nil {
			fmt.Printf("  \u2192  %-25s available\n", bin)
		} else {
			fmt.Printf("  \u2192  %-25s not installed (install via UI)\n", bin)
		}
	}

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
		fmt.Printf("  \u2717 Dashboard failed to start: %v\n", err)
		return err
	}
	log.Info("daemon spawned", log.Fields{"pid": daemon.Process.Pid})

	healthcheck := waitForDaemon(daemonHealthcheckTimeout(), daemonHealthPollingInterval)
	if !healthcheck.OK {
		log.Error("daemon healthcheck timed out", log.Fields{"pid": daemon.Process.Pid, "url": healthcheck.URL, "last_error": healthcheck.LastError, "waited": healthcheck.Waited.Round(time.Millisecond).String()})
		terminateFailedDaemon(daemon)
		fmt.Printf("  \u2717 Dashboard failed to respond \u2014 see %s\n", filepath.Join(dwytHome, "dwyt.log"))
		return fmt.Errorf("daemon healthcheck timeout")
	}

	workspace.Touch(projectPath)
	fmt.Printf("  \u2713 Dashboard \u2192 http://localhost:2737\n")
	fmt.Printf("  Stop: dwyt stop\n\n")
	ensureKiroPowerIfEnabled(projectPath)
	openBrowserURL("http://localhost:2737/#/dashboard?project=" + url.PathEscape(projectPath))
	return nil
}

// effectiveDWYTPaths returns the process-wide paths established during root
// initialization. detect.Detect intentionally describes the platform default,
// while DwytHome/DwytBin/DwytData already incorporate DWYT_HOME when the user
// opted into an alternate state directory. Keep the detected shell settings
// separate from these runtime paths.
func effectiveDWYTPaths(e *detect.Env) (home, bin, data string) {
	home = DwytHome
	if home == "" && e != nil {
		home = e.DwytHome
	}
	bin = DwytBin
	if bin == "" {
		bin = filepath.Join(home, "bin")
	}
	data = DwytData
	if data == "" {
		data = filepath.Join(home, "data")
	}
	return home, bin, data
}

func banner() {
	fmt.Printf("\n  \u2554\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2557\n")
	fmt.Printf("  \u2551  DWYT \u2014 Don't Waste Your Tokens     \u2551\n")
	fmt.Printf("  \u255A\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u255D\n\n")
}

type daemonProbe struct {
	OK      bool
	Version string
	URL     string
	Error   string
}

func probeDaemon() daemonProbe {
	return probeDaemonURL(daemonHealthURL, daemonHealthProbeTimeout)
}

func probeDaemonURL(url string, timeout time.Duration) daemonProbe {
	probe := daemonProbe{URL: url}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(url)
	if err != nil {
		probe.Error = err.Error()
		return probe
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		probe.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
		return probe
	}
	probe.OK = true
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return probe
	}
	var payload struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(body, &payload) == nil {
		probe.Version = payload.Version
	}
	return probe
}

type daemonHealthcheckResult struct {
	OK        bool
	URL       string
	LastError string
	Waited    time.Duration
}

// daemonHealthcheckTimeout reads the total startup budget. Windows receives a
// larger default because spawning the launcher, Python environment and proxy
// routinely takes longer there; users can override either default explicitly.
func daemonHealthcheckTimeout() time.Duration {
	defaultTimeout := defaultDaemonHealthTimeout
	if runtime.GOOS == "windows" {
		defaultTimeout = windowsDaemonHealthTimeout
	}
	raw := strings.TrimSpace(os.Getenv("DWYT_DAEMON_HEALTHCHECK_TIMEOUT_SECONDS"))
	if raw == "" {
		return defaultTimeout
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		log.Warn("invalid daemon healthcheck timeout; using default", log.Fields{"value": raw, "default": defaultTimeout.String()})
		return defaultTimeout
	}
	return time.Duration(seconds) * time.Second
}

// waitForDaemon probes immediately, then keeps polling until the total
// deadline. Each HTTP request is bounded independently and never extends the
// caller's overall startup budget.
func waitForDaemon(timeout, interval time.Duration) daemonHealthcheckResult {
	return waitForDaemonURL(daemonHealthURL, timeout, interval)
}

func waitForDaemonURL(url string, timeout, interval time.Duration) daemonHealthcheckResult {
	started := time.Now()
	deadline := started.Add(timeout)
	result := daemonHealthcheckResult{URL: url}

	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			result.Waited = time.Since(started)
			return result
		}
		probeTimeout := daemonHealthProbeTimeout
		if remaining < probeTimeout {
			probeTimeout = remaining
		}
		probe := probeDaemonURL(url, probeTimeout)
		result.URL = probe.URL
		result.LastError = probe.Error
		if probe.OK {
			result.OK = true
			result.Waited = time.Since(started)
			return result
		}

		remaining = time.Until(deadline)
		if remaining <= 0 {
			result.Waited = time.Since(started)
			return result
		}
		sleep := interval
		if remaining < sleep {
			sleep = remaining
		}
		time.Sleep(sleep)
	}
}

// terminateFailedDaemon reaps the daemon after terminating its process tree.
// This is essential on Windows: the daemon can already have launched the
// Headroom .bat/Python descendants while the dashboard is still starting.
func terminateFailedDaemon(daemon *exec.Cmd) {
	if daemon == nil || daemon.Process == nil {
		return
	}
	pid := daemon.Process.Pid
	// Managed services use their own Unix process groups so a service health
	// failure can kill only that service tree. Tear down their PID-tracked
	// groups first, then terminate the daemon's session as a final catch-all.
	// On Windows the same ordering uses taskkill /T for each tracked tree.
	procutil.StopAllTracked(DwytHome)
	if err := procutil.TerminateTree(pid); err != nil {
		log.Warn("failed to terminate daemon process tree", log.Fields{"pid": pid, "error": err.Error()})
	}
	done := make(chan error, 1)
	go func() { done <- daemon.Wait() }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		log.Warn("daemon did not exit after process-tree termination", log.Fields{"pid": pid})
	}
}

func daemonVersionNeedsRestart(daemonVersion string) bool {
	current := normalizeDaemonVersion(version)
	if current == "dev" {
		return false
	}
	if strings.TrimSpace(daemonVersion) == "" {
		return true
	}
	return normalizeDaemonVersion(daemonVersion) != current
}

func normalizeDaemonVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "dev"
	}
	lower := strings.ToLower(v)
	if lower == "dev" || lower == "development" {
		return "dev"
	}
	v = strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V")
	return "v" + v
}

func stopDaemonProcess() {
	// Cross-platform: prefer the recorded daemon PID.
	stoppedTrackedDaemon := false
	if pid := procutil.ReadPID(filepath.Join(procutil.PIDDir(DwytHome), "daemon.pid")); pid > 0 {
		// Services own separate Unix process groups for safe service-local
		// cleanup. Stop their tracked trees before ending the daemon session;
		// this also preserves Windows taskkill /T semantics for every child.
		procutil.StopAllTracked(DwytHome)
		// Keep the daemon group/tree kill as a final catch-all for untracked
		// descendants or stale PID files from older releases.
		procutil.TerminateTree(pid)
		procutil.RemovePID(DwytHome, "daemon")
		stoppedTrackedDaemon = true
	}
	// Older releases did not write PID files, so retain the Unix best-effort
	// fallback only when there was no tracked daemon. Once a recorded daemon
	// tree has been stopped, these broad pkill patterns are unnecessary and
	// could affect an unrelated DWYT invocation.
	if runtime.GOOS != "windows" && !stoppedTrackedDaemon {
		exe, _ := os.Executable()
		if exe != "" {
			exec.Command("pkill", "-f", exe+" daemon").Run()
		}
		exec.Command("pkill", "-f", "dwyt.*daemon").Run()
	}
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

func ensureKiroPowerIfEnabled(projectPath string) {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://localhost:2737/api/setup/load")
	if err != nil {
		return
	}
	defer resp.Body.Close()
	var cfg map[string]interface{}
	if json.NewDecoder(resp.Body).Decode(&cfg) != nil || !kiroEnabledInConfig(cfg) {
		return
	}
	req, err := http.NewRequest("POST", "http://localhost:2737/api/kiro/power/refresh", nil)
	if err != nil {
		return
	}
	q := req.URL.Query()
	q.Set("project", projectPath)
	req.URL.RawQuery = q.Encode()
	if refreshResp, err := client.Do(req); err == nil {
		refreshResp.Body.Close()
		if refreshResp.StatusCode < 300 {
			fmt.Printf("  \u2713 Kiro Power ready\n")
		}
	}
}

func kiroEnabledInConfig(cfg map[string]interface{}) bool {
	for _, key := range []string{"ias", "clients"} {
		if values, ok := cfg[key].([]interface{}); ok {
			for _, value := range values {
				if s, ok := value.(string); ok && s == "kiro" {
					return true
				}
			}
		}
	}
	return false
}

func startServicesAsync(dwytBin string) int {
	codebaseBin := platform.DWYTLauncherPath(dwytBin, "codebase-memory-mcp")
	if _, err := os.Stat(codebaseBin); err == nil {
		fmt.Printf("  \u2192  codebase-memory-mcp     available (index on demand)\n")
	} else {
		fmt.Printf("  \u2192  codebase-memory-mcp     not installed (install via UI)\n")
	}

	headroomPort := health.FindFreePort(requestedHeadroomPort())
	headroomBin := platform.DWYTLauncherPath(dwytBin, "headroom")
	if _, err := os.Stat(headroomBin); err == nil {
		fmt.Printf("  \u2192  headroom                will start on port %d via daemon\n", headroomPort)
	} else {
		fmt.Printf("  \u2192  headroom                not installed (install via UI)\n")
	}

	fmt.Printf("  \u2192  obsidian                available (Obsidian vault)\n")
	return headroomPort
}

func requestedHeadroomPort() int {
	const defaultPort = 8787
	raw := strings.TrimSpace(os.Getenv("DWYT_HEADROOM_PORT"))
	if raw == "" {
		return defaultPort
	}
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		log.Warn("invalid requested Headroom port; using default", log.Fields{"value": raw, "default": defaultPort})
		return defaultPort
	}
	return port
}

func openBrowserURL(url string) {
	platform.OpenURL(url)
}

func getCWD() string {
	d, _ := os.Getwd()
	return d
}
