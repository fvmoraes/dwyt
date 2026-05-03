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
	StateNotInstalled ServiceState = "not_installed"
	StateStarting    ServiceState = "starting"
	StateRunning     ServiceState = "running"
	StateFailed       ServiceState = "failed"
)

type ToolStatus struct {
	Name    string       `json:"name"`
	Running bool         `json:"running"`
	Healthy bool         `json:"healthy"`
	State   ServiceState `json:"state"`
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

func PollAll(dwytBin string) *SystemStatus {
	s := &SystemStatus{Timestamp: time.Now()}
	s.Tools = append(s.Tools, pollCBMCP(dwytBin))
	s.Tools = append(s.Tools, pollRTK(dwytBin))
	s.Tools = append(s.Tools, pollHeadroom())
	s.Tools = append(s.Tools, pollMemStack(dwytBin))
	return s
}

func pollCBMCP(dwytBin string) ToolStatus {
	ts := ToolStatus{Name: "codebase-memory-mcp", State: StateNotInstalled}
	bin := filepath.Join(dwytBin, "codebase-memory-mcp")
	if _, err := os.Stat(bin); err != nil {
		return ts
	}

	ts.State = StateRunning
	ts.Port = 9749

	if health.ProbeURL("http://127.0.0.1:9749/health") {
		ts.Running = true
		ts.Healthy = true
		ts.State = StateRunning
		ts.Details = "UI on port 9749"
		return ts
	}

	if out, err := exec.Command(bin, "--version").Output(); err == nil {
		ts.Running = true
		ts.Healthy = true
		ts.Details = strings.TrimSpace(string(out))
		ts.State = StateRunning
	} else {
		ts.State = StateFailed
		ts.Error = "process failed or not responding"
	}
	return ts
}

func pollRTK(dwytBin string) ToolStatus {
	ts := ToolStatus{Name: "rtk", State: StateNotInstalled}
	bin := filepath.Join(dwytBin, "rtk")
	if _, err := os.Stat(bin); err != nil {
		return ts
	}

	ts.State = StateRunning
	if out, err := exec.Command(bin, "--version").Output(); err == nil {
		ts.Running = true
		ts.Healthy = true
		ts.Details = strings.TrimSpace(string(out))
	} else {
		ts.State = StateFailed
		ts.Error = "binary is present but not responding"
	}
	return ts
}

func pollHeadroom() ToolStatus {
	ts := ToolStatus{Name: "headroom", Port: headroomDefaultPort}
	url := fmt.Sprintf("http://127.0.0.1:%d/health", headroomDefaultPort)
	if health.ProbeURL(url) {
		ts.Running = true
		ts.Healthy = true
		ts.State = StateRunning
		ts.Details = fmt.Sprintf("proxy on port %d", headroomDefaultPort)
	} else {
		ts.State = StateNotInstalled
	}
	return ts
}

func pollMemStack(dwytBin string) ToolStatus {
	ts := ToolStatus{Name: "memstack"}
	bin := filepath.Join(dwytBin, "memstack")
	if _, err := os.Stat(bin); err != nil {
		ts.State = StateNotInstalled
		return ts
	}

	ts.Running = true
	ts.Healthy = true
	ts.State = StateRunning
	ts.Details = "disponível"
	return ts
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
		var data map[string]interface{}
		if json.NewDecoder(resp.Body).Decode(&data) == nil {
			// Headroom v0.20+ nested: persistent_savings.lifetime.tokens_saved
			if ps, ok := data["persistent_savings"].(map[string]interface{}); ok {
				if lt, ok := ps["lifetime"].(map[string]interface{}); ok {
					if v, ok := lt["tokens_saved"].(float64); ok {
						m.TokensSaved = int64(v)
					}
				}
			}
			// requests.total
			if rq, ok := data["requests"].(map[string]interface{}); ok {
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
	if _, err := os.Stat(bin); err != nil {
		states["codebase-memory-mcp"] = StateNotInstalled
	} else if _, err := exec.Command(bin, "--version").Output(); err == nil {
		states["codebase-memory-mcp"] = StateRunning
	} else {
		states["codebase-memory-mcp"] = StateFailed
	}

	// headroom
	if health.ProbeURL(fmt.Sprintf("http://127.0.0.1:%d/health", headroomDefaultPort)) {
		states["headroom"] = StateRunning
	} else {
		states["headroom"] = StateNotInstalled
	}

	// rtk
	bin = filepath.Join(dwytBin, "rtk")
	if _, err := os.Stat(bin); err != nil {
		states["rtk"] = StateNotInstalled
	} else {
		states["rtk"] = StateRunning
	}

	// memstack
	bin = filepath.Join(dwytBin, "memstack")
	if _, err := os.Stat(bin); err != nil {
		states["memstack"] = StateNotInstalled
	} else {
		states["memstack"] = StateRunning
	}

	log.Debug("health status poll", log.Fields{"states": states})
	return states
}

func pgrep(pattern string) bool {
	out, err := exec.Command("pgrep", "-f", pattern).Output()
	return err == nil && len(strings.TrimSpace(string(out))) > 0
}
