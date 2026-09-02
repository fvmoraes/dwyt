package status

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/fvmoraes/dwyt/internal/health"
	"github.com/fvmoraes/dwyt/internal/log"
	"github.com/fvmoraes/dwyt/internal/platform"
)

type ServiceState string

const (
	StateNotInstalled     ServiceState = "not_installed"
	StateStarting         ServiceState = "starting"
	StateOnline           ServiceState = "online"
	StateOffline          ServiceState = "offline"
	StateInstalled        ServiceState = "installed"
	StateInactive         ServiceState = "inactive"
	StatePortOpenNoHealth ServiceState = "port_open_no_health"
	StateError            ServiceState = "error"

	// Legacy aliases kept for older callers that still compare against these names.
	StateRunning ServiceState = StateOnline
	StateFailed  ServiceState = StateError
)

type ToolStatus struct {
	Name    string       `json:"name"`
	Running bool         `json:"running"`
	Healthy bool         `json:"healthy"`
	Status  ServiceState `json:"status"`
	State   ServiceState `json:"state,omitempty"` // legacy mirror of Status
	Port    int          `json:"port,omitempty"`
	Details string       `json:"details,omitempty"`
	Error   string       `json:"error,omitempty"`
}

type SystemStatus struct {
	Timestamp time.Time    `json:"timestamp"`
	Tools     []ToolStatus `json:"tools"`
}

type RTKMetrics struct {
	TotalCommands int64   `json:"total_commands"`
	TokensSaved   int64   `json:"tokens_saved"`
	PctSaved      float64 `json:"pct_saved"`
}

type HeadroomMetrics struct {
	Running      bool  `json:"running"`
	Port         int   `json:"port"`
	TokensSaved  int64 `json:"tokens_saved"`
	RequestsDone int64 `json:"requests_done"`
}

const defaultHeadroomPort = 8787

const toolProbeTimeout = 2 * time.Second

var headroomPort atomic.Int64

func init() { headroomPort.Store(defaultHeadroomPort) }

// SetHeadroomPort publishes the port selected by the daemon. Atomic access is
// required because startup chooses a fallback port in a goroutine while the
// dashboard can poll status and metrics concurrently.
func SetHeadroomPort(port int) {
	if port < 1 || port > 65535 {
		return
	}
	headroomPort.Store(int64(port))
}

// HeadroomPort returns a consistent snapshot of the port used by all status
// probes. It is intentionally exported for callers that need the same value
// without reading the mutable startup state directly.
func HeadroomPort() int {
	port := int(headroomPort.Load())
	if port < 1 || port > 65535 {
		return defaultHeadroomPort
	}
	return port
}

func PollAll(dwytBin string, hasObsidianVault ...bool) *SystemStatus {
	return PollAllWithPaths(
		platform.DWYTLauncherPath(dwytBin, "codebase-memory-mcp"),
		platform.DWYTLauncherPath(dwytBin, "rtk"),
		platform.DWYTLauncherPath(dwytBin, "headroom"), hasObsidianVault...)
}

// PollAllWithPaths is the ownership-aware variant used by the dashboard. The
// explicit paths ensure an external tool is reported as itself, never as a
// missing DWYT-managed copy.
func PollAllWithPaths(codebaseBin, rtkBin, headroomBin string, hasObsidianVault ...bool) *SystemStatus {
	s := &SystemStatus{Timestamp: time.Now()}
	headroomPort := HeadroomPort()
	s.Tools = append(s.Tools, pollCBMCPPath(codebaseBin))
	s.Tools = append(s.Tools, pollRTKPath(rtkBin))
	s.Tools = append(s.Tools, pollHeadroomPath(headroomBin, headroomPort))
	vault := false
	if len(hasObsidianVault) > 0 {
		vault = hasObsidianVault[0]
	}
	s.Tools = append(s.Tools, pollBrain(vault))
	return s
}

func pollCBMCP(dwytBin string) ToolStatus {
	return pollCBMCPPath(platform.DWYTLauncherPath(dwytBin, "codebase-memory-mcp"))
}

func pollCBMCPPath(bin string) ToolStatus {
	ts := ToolStatus{Name: "codebase-memory-mcp", Status: StateNotInstalled, State: StateNotInstalled}
	if health.ProbeURL("http://127.0.0.1:9749/health") {
		ts.Status = StateOnline
		ts.State = StateOnline
		ts.Running = true
		ts.Healthy = true
		ts.Port = 9749
		ts.Details = "UI on port 9749"
		return ts
	}
	if health.ProbePort(9749) {
		ts.Status = StatePortOpenNoHealth
		ts.State = StatePortOpenNoHealth
		ts.Running = false
		ts.Healthy = false
		ts.Port = 9749
		ts.Details = "port 9749 occupied but healthcheck failed"
		return ts
	}

	if _, err := os.Stat(bin); err != nil {
		return ts
	}

	// Binary exists — verify it's functional with --version
	if _, err := commandOutput(bin, "--version"); err != nil {
		ts.Status = StateError
		ts.State = StateError
		ts.Error = "binary is present but not responding"
		return ts
	}

	ts.Status = StateInstalled
	ts.State = StateInstalled
	ts.Running = false
	ts.Healthy = false
	ts.Port = 9749
	ts.Details = "installed (launch on demand)"
	return ts
}

func pollRTK(dwytBin string) ToolStatus {
	return pollRTKPath(platform.DWYTLauncherPath(dwytBin, "rtk"))
}

func pollRTKPath(bin string) ToolStatus {
	ts := ToolStatus{Name: "rtk", Status: StateNotInstalled, State: StateNotInstalled}
	if _, err := os.Stat(bin); err != nil {
		return ts
	}

	ts.Status = StateInstalled
	ts.State = StateInstalled
	if out, err := commandOutput(bin, "--version"); err == nil {
		ts.Running = true
		ts.Healthy = true
		ts.Details = strings.TrimSpace(string(out))
	} else {
		ts.Status = StateError
		ts.State = StateError
		ts.Error = "binary is present but not responding"
	}
	return ts
}

func pollHeadroom(dwytBin string, port int) ToolStatus {
	return pollHeadroomPath(platform.DWYTLauncherPath(dwytBin, "headroom"), port)
}

func pollHeadroomPath(bin string, port int) ToolStatus {
	ts := ToolStatus{Name: "headroom", Port: port, Status: StateNotInstalled, State: StateNotInstalled}
	url := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	if health.ProbeURL(url) {
		ts.Running = true
		ts.Healthy = true
		ts.Status = StateOnline
		ts.State = StateOnline
		ts.Details = fmt.Sprintf("proxy on port %d", port)
	} else {
		if _, err := os.Stat(bin); err != nil {
			return ts
		}
		ts.Status = StateOffline
		ts.State = StateOffline
		ts.Details = "installed (launch on demand)"
	}
	return ts
}

func pollBrain(hasVault bool) ToolStatus {
	ts := ToolStatus{Name: "obsidian", Status: StateInactive, State: StateInactive}

	// The vault (ProjectObsidian) is the primary indicator of obsidian state.
	// Desktop app installation is secondary — used only for "Open Vault" action.
	if !hasVault {
		ts.Status = StateInactive
		ts.State = StateInactive
		ts.Running = false
		ts.Healthy = false
		ts.Details = "no vault loaded"
		return ts
	}

	if !obsidianAppInstalled() {
		ts.Running = true
		ts.Healthy = true
		ts.Status = StateOnline
		ts.State = StateOnline
		ts.Details = "vault loaded (Obsidian app not installed)"
		return ts
	}

	ts.Running = true
	ts.Healthy = true
	ts.Status = StateOnline
	ts.State = StateOnline
	ts.Details = "Obsidian vault active"
	return ts
}

// obsidianAppInstalled checks if the Obsidian desktop app is installed.
func obsidianAppInstalled() bool {
	if _, err := exec.LookPath("obsidian"); err == nil {
		return true
	}
	home, _ := os.UserHomeDir()
	locations := []string{
		filepath.Join(home, ".local", "bin", "obsidian"),
		filepath.Join(home, ".local", "share", "applications", "obsidian.desktop"),
		"/usr/bin/obsidian",
		"/usr/local/bin/obsidian",
		"/opt/obsidian/obsidian",
		"/opt/Obsidian/obsidian",
		filepath.Join(home, "AppData", "Local", "obsidian", "obsidian.exe"),
		"/Applications/Obsidian.app/Contents/MacOS/Obsidian",
	}
	for _, loc := range locations {
		if _, err := os.Stat(loc); err == nil {
			return true
		}
	}
	// Also check for AppImage in common locations
	entries, _ := os.ReadDir(filepath.Join(home, ".local", "bin"))
	for _, e := range entries {
		if strings.Contains(strings.ToLower(e.Name()), "obsidian") {
			return true
		}
	}
	return false
}

func GetRTKMetrics(dwytBin string) *RTKMetrics {
	return GetRTKMetricsForBinary(platform.DWYTLauncherPath(dwytBin, "rtk"))
}

func GetRTKMetricsForBinary(bin string) *RTKMetrics {
	if _, err := os.Stat(bin); err != nil {
		return nil
	}
	out, err := commandOutput(bin, "gain")
	if err != nil {
		return nil
	}
	output := string(out)
	m := &RTKMetrics{}
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total commands:") {
			fmt.Sscanf(line, "Total commands: %d", &m.TotalCommands)
		}
		if strings.HasPrefix(line, "Tokens saved:") {
			parts := strings.Split(line, "(")
			val := strings.TrimPrefix(strings.TrimSpace(parts[0]), "Tokens saved:")
			m.TokensSaved = parseTokenCount(strings.TrimSpace(val))
			if len(parts) > 1 {
				fmt.Sscanf(strings.TrimRight(parts[1], ")%"), "%f", &m.PctSaved)
			}
		}
	}
	return m
}

func GetHeadroomMetrics() *HeadroomMetrics {
	port := HeadroomPort()
	return getHeadroomMetrics(port, headroomMetricsHTTPClient)
}

const headroomMetricsTimeout = 2 * time.Second

var headroomMetricsHTTPClient = &http.Client{Timeout: headroomMetricsTimeout}

func getHeadroomMetrics(port int, client *http.Client) *HeadroomMetrics {
	m := &HeadroomMetrics{Port: port}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/stats", port))
	if err != nil {
		return m
	}
	defer resp.Body.Close()
	if resp.StatusCode == 200 {
		m.Running = true
		var data map[string]any
		if json.NewDecoder(resp.Body).Decode(&data) == nil {
			// Headroom v0.20+ nested: persistent_savings.lifetime.tokens_saved
			if ps, ok := data["persistent_savings"].(map[string]any); ok {
				if lt, ok := ps["lifetime"].(map[string]any); ok {
					if v, ok := lt["tokens_saved"].(float64); ok {
						m.TokensSaved = int64(v)
					}
				}
			}
			// requests.total
			if rq, ok := data["requests"].(map[string]any); ok {
				if v, ok := rq["total"].(float64); ok {
					m.RequestsDone = int64(v)
				}
			}
			// Fallback: top-level fields
			if m.TokensSaved == 0 {
				if v, ok := data["tokens_saved"].(float64); ok {
					m.TokensSaved = int64(v)
				}
			}
			if m.RequestsDone == 0 {
				if v, ok := data["requests"].(float64); ok {
					m.RequestsDone = int64(v)
				}
			}
		}
	}
	return m
}

func parseTokenCount(s string) int64 {
	s = strings.TrimSpace(s)
	mul := int64(1)
	if strings.HasSuffix(s, "M") || strings.HasSuffix(s, "m") {
		mul = 1_000_000
		s = s[:len(s)-1]
	} else if strings.HasSuffix(s, "K") || strings.HasSuffix(s, "k") {
		mul = 1_000
		s = s[:len(s)-1]
	}
	var v float64
	fmt.Sscanf(s, "%f", &v)
	return int64(v * float64(mul))
}

func GetRTKMetricsForPath(dwytBin, projectPath string) *RTKMetrics {
	return GetRTKMetricsForPathBinary(platform.DWYTLauncherPath(dwytBin, "rtk"), projectPath)
}

func GetRTKMetricsForPathBinary(bin, projectPath string) *RTKMetrics {
	if _, err := os.Stat(bin); err != nil {
		return nil
	}

	// Check if RTK is initialized in this project
	if _, err := os.Stat(filepath.Join(projectPath, ".rtk")); err != nil {
		return nil // RTK not initialized in this project
	}

	out, err := commandOutputInDir(projectPath, bin, "gain", "--project")
	if err != nil {
		return nil
	}
	output := string(out)
	m := &RTKMetrics{}
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total commands:") {
			fmt.Sscanf(line, "Total commands: %d", &m.TotalCommands)
		}
		if strings.HasPrefix(line, "Tokens saved:") {
			parts := strings.Split(line, "(")
			val := strings.TrimPrefix(strings.TrimSpace(parts[0]), "Tokens saved:")
			m.TokensSaved = parseTokenCount(strings.TrimSpace(val))
			if len(parts) > 1 {
				fmt.Sscanf(strings.TrimRight(parts[1], ")%"), "%f", &m.PctSaved)
			}
		}
	}
	if m.TotalCommands == 0 && m.TokensSaved == 0 {
		return nil
	}
	return m
}

// HealthStatus returns a summary of all tool health suitable for quick polling.
func HealthStatus(dwytBin string) map[string]ServiceState {
	states := make(map[string]ServiceState)
	headroomPort := HeadroomPort()

	// codebase: MCP server launched on-demand by clients, not a persistent service
	bin := platform.DWYTLauncherPath(dwytBin, "codebase-memory-mcp")
	if health.ProbeURL("http://127.0.0.1:9749/health") {
		states["codebase-memory-mcp"] = StateOnline
	} else if _, err := os.Stat(bin); err != nil {
		states["codebase-memory-mcp"] = StateNotInstalled
	} else if _, err := commandOutput(bin, "--version"); err == nil {
		states["codebase-memory-mcp"] = StateOffline
	} else {
		states["codebase-memory-mcp"] = StateError
	}

	// headroom
	bin = platform.DWYTLauncherPath(dwytBin, "headroom")
	if health.ProbeURL(fmt.Sprintf("http://127.0.0.1:%d/health", headroomPort)) {
		states["headroom"] = StateOnline
	} else if _, err := os.Stat(bin); err != nil {
		states["headroom"] = StateNotInstalled
	} else {
		states["headroom"] = StateOffline
	}

	// rtk
	bin = platform.DWYTLauncherPath(dwytBin, "rtk")
	if _, err := os.Stat(bin); err != nil {
		states["rtk"] = StateNotInstalled
	} else {
		states["rtk"] = StateInstalled
	}

	// /api/status carries vault state; /api/health only reports that the
	// Obsidian integration is available without claiming an active vault.
	states["obsidian"] = StateInactive

	log.Debug("health status poll", log.Fields{"states": states})
	return states
}

// commandOutput bounds local binary probes used by dashboard polling. A
// damaged wrapper or a tool waiting for network/input must not make `/status`
// or metrics requests hang indefinitely.
func commandOutput(bin string, args ...string) ([]byte, error) {
	return commandOutputInDir("", bin, args...)
}

func commandOutputInDir(dir, bin string, args ...string) ([]byte, error) {
	return commandOutputWithTimeout(toolProbeTimeout, dir, bin, args...)
}

func commandOutputWithTimeout(timeout time.Duration, dir, bin string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	return cmd.Output()
}
