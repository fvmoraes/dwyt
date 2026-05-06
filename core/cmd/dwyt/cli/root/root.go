package root

import (
	"encoding/json"
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
	cwd := getCWD()
	if projectPath == "" {
		projectPath = cwd
	}
	log.Info("DWYT startup", log.Fields{"project": projectPath, "home": DwytHome})

	banner()
	fmt.Printf("  Project: %s\n", projectPath)

	env.Init(e.DwytHome, e.DwytBin, e.DwytData, e.ShellRC, e.LoginRC)

	if !brain.ObsidianInstalled() {
		fmt.Println("  \u2192  obsidian               not found (install for visual navigation)")
		fmt.Println("     https://obsidian.md/download")
	} else {
		fmt.Println("  \u2192  obsidian               detected")
	}

	if daemonOK := probeDaemon(); daemonOK {
		if err := switchProject(projectPath); err == nil {
			workspace.Touch(projectPath)
			fmt.Printf("  \u2713 Dashboard \u2192 http://localhost:2737  (already running)\n")
			fmt.Printf("  \u2713 Project context updated\n\n")
			ensureKiroPowerIfEnabled(projectPath)
			openBrowserURL("http://localhost:2737/#/dashboard?project=" + url.PathEscape(projectPath))
			return nil
		}
		log.Warn("daemon probe ok but switch failed, restarting")
		exec.Command("pkill", "-f", "dwyt.*daemon").Run()
		time.Sleep(300 * time.Millisecond)
	}

	headroomPort := startServicesAsync(e.DwytBin)

	for _, bin := range []string{"rtk"} {
		if _, err := os.Stat(filepath.Join(e.DwytBin, bin)); err == nil {
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

	if !waitForDaemon(3*time.Second, 300*time.Millisecond) {
		log.Error("daemon healthcheck timed out", log.Fields{"pid": daemon.Process.Pid})
		daemon.Process.Kill()
		fmt.Printf("  \u2717 Dashboard failed to respond \u2014 see %s\n", filepath.Join(e.DwytHome, "dwyt.log"))
		return fmt.Errorf("daemon healthcheck timeout")
	}

	workspace.Touch(projectPath)
	fmt.Printf("  \u2713 Dashboard \u2192 http://localhost:2737\n")
	fmt.Printf("  Stop: dwyt stop\n\n")
	ensureKiroPowerIfEnabled(projectPath)
	openBrowserURL("http://localhost:2737/#/dashboard?project=" + url.PathEscape(projectPath))
	return nil
}

func banner() {
	fmt.Printf("\n  \u2554\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2557\n")
	fmt.Printf("  \u2551  DWYT \u2014 Don't Waste Your Tokens     \u2551\n")
	fmt.Printf("  \u255A\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u2550\u255D\n\n")
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
	codebaseBin := filepath.Join(dwytBin, "codebase-memory-mcp")
	if _, err := os.Stat(codebaseBin); err == nil {
		fmt.Printf("  \u2192  codebase-memory-mcp     available (index on demand)\n")
	} else {
		fmt.Printf("  \u2192  codebase-memory-mcp     not installed (install via UI)\n")
	}

	headroomPort := health.FindFreePort(8787)
	headroomBin := filepath.Join(dwytBin, "headroom")
	if _, err := os.Stat(headroomBin); err == nil {
		fmt.Printf("  \u2192  headroom                will start on port %d via daemon\n", headroomPort)
	} else {
		fmt.Printf("  \u2192  headroom                not installed (install via UI)\n")
	}

	fmt.Printf("  \u2192  obsidian                available (Obsidian vault)\n")
	return headroomPort
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
