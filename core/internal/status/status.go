package status

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fvmoraes/dwyt/internal/health"
	"github.com/fvmoraes/dwyt/internal/log"
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

var headroomDefaultPort = 8787

func SetHeadroomPort(port int) { headroomDefaultPort = port }

func PollAll(dwytBin string, hasObsidianVault ...bool) *SystemStatus {
	s := &SystemStatus{Timestamp: time.Now()}
	s.Tools = append(s.Tools, pollCBMCP(dwytBin))
	s.Tools = append(s.Tools, pollRTK(dwytBin))
	s.Tools = append(s.Tools, pollHeadroom(dwytBin))
	vault := false
	if len(hasObsidianVault) > 0 {
		vault = hasObsidianVault[0]
	}
	s.Tools = append(s.Tools, pollBrain(vault))
	return s
}

func pollCBMCP(dwytBin string) ToolStatus {
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

	bin := filepath.Join(dwytBin, "codebase-memory-mcp")
	if _, err := os.Stat(bin); err != nil {
		return ts
	}

	// Binary exists — verify it's functional with --version
	if _, err := exec.Command(bin, "--version").Output(); err != nil {
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
	ts := ToolStatus{Name: "rtk", Status: StateNotInstalled, State: StateNotInstalled}
	bin := filepath.Join(dwytBin, "rtk")
	if _, err := os.Stat(bin); err != nil {
		return ts
	}

	ts.Status = StateInstalled
	ts.State = StateInstalled
	if out, err := exec.Command(bin, "--version").Output(); err == nil {
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

func pollHeadroom(dwytBin string) ToolStatus {
	ts := ToolStatus{Name: "headroom", Port: headroomDefaultPort, Status: StateNotInstalled, State: StateNotInstalled}
	url := fmt.Sprintf("http://127.0.0.1:%d/health", headroomDefaultPort)
	if health.ProbeURL(url) {
		ts.Running = true
		ts.Healthy = true
		ts.Status = StateOnline
		ts.State = StateOnline
		ts.Details = fmt.Sprintf("proxy on port %d", headroomDefaultPort)
	} else {
		bin := filepath.Join(dwytBin, "headroom")
		if _, err := os.Stat(bin); err != nil {
			return ts
		}
		ts.Status = StateOffline
		ts.State = StateOffline
		ts.Details = "installed (start on demand)"
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
	bin := filepath.Join(dwytBin, "rtk")
	if _, err := os.Stat(bin); err != nil {
		return nil
	}
	out, err := exec.Command(bin, "gain").Output()
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
	m := &HeadroomMetrics{Port: headroomDefaultPort}
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/stats", headroomDefaultPort))
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
	bin := filepath.Join(dwytBin, "rtk")
	if _, err := os.Stat(bin); err != nil {
		return nil
	}

	// Check if RTK is initialized in this project
	if _, err := os.Stat(filepath.Join(projectPath, ".rtk")); err != nil {
		return nil // RTK not initialized in this project
	}

	cmd := exec.Command(bin, "gain", "--project")
	cmd.Dir = projectPath
	out, err := cmd.Output()
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

	// codebase: MCP server launched on-demand by clients, not a persistent service
	bin := filepath.Join(dwytBin, "codebase-memory-mcp")
	if health.ProbeURL("http://127.0.0.1:9749/health") {
		states["codebase-memory-mcp"] = StateOnline
	} else if _, err := os.Stat(bin); err != nil {
		states["codebase-memory-mcp"] = StateNotInstalled
	} else if _, err := exec.Command(bin, "--version").Output(); err == nil {
		states["codebase-memory-mcp"] = StateOffline
	} else {
		states["codebase-memory-mcp"] = StateError
	}

	// headroom
	bin = filepath.Join(dwytBin, "headroom")
	if health.ProbeURL(fmt.Sprintf("http://127.0.0.1:%d/health", headroomDefaultPort)) {
		states["headroom"] = StateOnline
	} else if _, err := os.Stat(bin); err != nil {
		states["headroom"] = StateNotInstalled
	} else {
		states["headroom"] = StateOffline
	}

	// rtk
	bin = filepath.Join(dwytBin, "rtk")
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

func pgrep(pattern string) bool {
	out, err := exec.Command("pgrep", "-f", pattern).Output()
	return err == nil && len(strings.TrimSpace(string(out))) > 0
}
