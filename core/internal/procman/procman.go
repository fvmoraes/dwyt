package procman

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fvmoraes/dwyt/internal/health"
	"github.com/fvmoraes/dwyt/internal/log"
)

type ServiceStatus struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	State   string `json:"state,omitempty"`
	Running bool   `json:"running"`
	Healthy bool   `json:"healthy"`
	PID     int    `json:"pid"`
	Port    int    `json:"port"`
	Uptime  string `json:"uptime,omitempty"`
	Error   string `json:"error,omitempty"`
}

type ManagedProcess struct {
	Name      string
	Bin       string
	Args      []string
	Port      int
	HealthURL string
	PID       int
	StartedAt time.Time
	LogDir    string
	mu        sync.Mutex
}

type ProcessManager struct {
	processes map[string]*ManagedProcess
	mu        sync.RWMutex
	logDir    string
}

func New(dwytHome string) *ProcessManager {
	logDir := filepath.Join(dwytHome, "logs")
	os.MkdirAll(logDir, 0755)
	return &ProcessManager{
		processes: make(map[string]*ManagedProcess),
		logDir:    logDir,
	}
}

func (pm *ProcessManager) Register(name, bin, healthURL string, port int, args ...string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.processes[name] = &ManagedProcess{
		Name:      name,
		Bin:       bin,
		Args:      args,
		Port:      port,
		HealthURL: healthURL,
	}
}

func (pm *ProcessManager) get(name string) *ManagedProcess {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.processes[name]
}

func (pm *ProcessManager) Start(name string) (*ServiceStatus, error) {
	mp := pm.get(name)
	if mp == nil {
		return nil, fmt.Errorf("service %s not registered", name)
	}

	mp.mu.Lock()
	defer mp.mu.Unlock()

	if mp.Running() {
		return pm.statusLocked(mp), nil
	}

	binPath := mp.Bin
	if _, err := os.Stat(binPath); err != nil {
		return &ServiceStatus{Name: name, Status: "not_installed", State: "not_installed", Error: fmt.Sprintf("binary not found: %s", binPath)}, err
	}

	freePort := health.FindFreePort(mp.Port)
	if freePort != mp.Port {
		log.Info("port was occupied, using alternative", log.Fields{"service": name, "original": mp.Port, "new": freePort})
		mp.Port = freePort
	}

	args := make([]string, len(mp.Args))
	copy(args, mp.Args)
	for i, a := range args {
		if a == "{port}" {
			args[i] = fmt.Sprintf("%d", mp.Port)
		}
	}

	cmd := exec.Command(binPath, args...)
	// MCP servers that use stdio need stdin to stay alive.
	// For services with a healthURL (HTTP-based like codebase UI), we can close stdin.
	// For stdio-based services, we keep stdin open indefinitely.
	if mp.HealthURL != "" {
		stdinPipe, _ := cmd.StdinPipe()
		defer stdinPipe.Close()
	} else {
		cmd.Stdin = os.Stdin
	}

	stdoutPath := filepath.Join(pm.logDir, name+"-stdout.log")
	stderrPath := filepath.Join(pm.logDir, name+"-stderr.log")
	os.MkdirAll(filepath.Dir(stdoutPath), 0755)

	stdout, _ := os.Create(stdoutPath)
	stderr, _ := os.Create(stderrPath)
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return &ServiceStatus{Name: name, Status: "error", State: "error", Error: fmt.Sprintf("failed to start: %v", err)}, err
	}

	mp.PID = cmd.Process.Pid
	mp.StartedAt = time.Now()
	log.Info("process started", log.Fields{"service": name, "pid": mp.PID, "port": mp.Port})

	if mp.HealthURL != "" {
		healthURL := fmt.Sprintf("http://127.0.0.1:%d%s", mp.Port, mp.HealthURL)
		// codebase-memory-mcp takes longer to start — use a longer timeout
		timeout := 10 * time.Second
		retries := 5
		if strings.Contains(binPath, "codebase") {
			timeout = 30 * time.Second
			retries = 10
		}
		if err := waitForHealth(healthURL, retries, timeout); err != nil {
			// Kill process that failed healthcheck
			if proc, procErr := os.FindProcess(mp.PID); procErr == nil {
				proc.Signal(syscall.SIGKILL)
			}
			mp.PID = 0
			log.Warn("process started but healthcheck failed, killed", log.Fields{"service": name, "error": err.Error()})
			return &ServiceStatus{Name: name, Status: "error", State: "error", Running: false, Healthy: false, PID: 0, Port: mp.Port, Error: err.Error()}, err
		}
		log.Info("process healthy", log.Fields{"service": name, "port": mp.Port})
	}

	return pm.statusLocked(mp), nil
}

func (pm *ProcessManager) Stop(name string) (*ServiceStatus, error) {
	mp := pm.get(name)
	if mp == nil {
		return nil, fmt.Errorf("service %s not registered", name)
	}

	mp.mu.Lock()
	defer mp.mu.Unlock()

	if !mp.Running() {
		return pm.statusLocked(mp), nil
	}

	proc, err := os.FindProcess(mp.PID)
	if err != nil {
		mp.PID = 0
		return pm.statusLocked(mp), nil
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		log.Info("SIGTERM failed, using SIGKILL", log.Fields{"service": name, "pid": mp.PID})
		proc.Signal(syscall.SIGKILL)
	} else {
		done := make(chan struct{})
		go func() {
			proc.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			log.Warn("process didn't exit on SIGTERM, sending SIGKILL", log.Fields{"service": name, "pid": mp.PID})
			proc.Signal(syscall.SIGKILL)
			<-done
		}
	}

	mp.PID = 0
	log.Info("process stopped", log.Fields{"service": name})
	return pm.statusLocked(mp), nil
}

func (pm *ProcessManager) Restart(name string) (*ServiceStatus, error) {
	pm.Stop(name)
	time.Sleep(500 * time.Millisecond)
	return pm.Start(name)
}

func (pm *ProcessManager) Status(name string) *ServiceStatus {
	mp := pm.get(name)
	if mp == nil {
		return &ServiceStatus{Name: name}
	}
	mp.mu.Lock()
	defer mp.mu.Unlock()
	return pm.statusLocked(mp)
}

func (pm *ProcessManager) AllStatus() map[string]*ServiceStatus {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	result := make(map[string]*ServiceStatus, len(pm.processes))
	for name, mp := range pm.processes {
		mp.mu.Lock()
		result[name] = pm.statusLocked(mp)
		mp.mu.Unlock()
	}
	return result
}

func (pm *ProcessManager) Logs(name string, tail int) string {
	stdoutPath := filepath.Join(pm.logDir, name+"-stdout.log")
	stderrPath := filepath.Join(pm.logDir, name+"-stderr.log")

	var result string
	if data, err := os.ReadFile(stdoutPath); err == nil {
		result += fmt.Sprintf("=== STDOUT ===\n%s\n", tailBytes(data, tail))
	}
	if data, err := os.ReadFile(stderrPath); err == nil {
		result += fmt.Sprintf("=== STDERR ===\n%s\n", tailBytes(data, tail))
	}
	if result == "" {
		result = "(no logs yet)"
	}
	return result
}

func (pm *ProcessManager) statusLocked(mp *ManagedProcess) *ServiceStatus {
	s := &ServiceStatus{
		Name:    mp.Name,
		Port:    mp.Port,
		PID:     mp.PID,
		Running: mp.Running(),
	}
	if s.Running {
		s.Status = "online"
		s.State = "online"
		s.Uptime = time.Since(mp.StartedAt).Round(time.Second).String()
		if mp.HealthURL != "" {
			healthURL := fmt.Sprintf("http://127.0.0.1:%d%s", mp.Port, mp.HealthURL)
			s.Healthy = probeURL(healthURL)
			if !s.Healthy {
				s.Status = "port_open_no_health"
				s.State = "port_open_no_health"
				s.Error = "healthcheck failed"
			}
		} else {
			s.Healthy = true
		}
	} else if _, err := os.Stat(mp.Bin); err != nil {
		s.Status = "not_installed"
		s.State = "not_installed"
	} else {
		s.Status = "offline"
		s.State = "offline"
	}
	return s
}

func (mp *ManagedProcess) Running() bool {
	if mp.PID == 0 {
		return false
	}
	proc, err := os.FindProcess(mp.PID)
	if err != nil {
		mp.PID = 0
		return false
	}

	// Check if process is not a zombie (Linux only)
	if runtime.GOOS == "linux" {
		statPath := fmt.Sprintf("/proc/%d/stat", mp.PID)
		data, err := os.ReadFile(statPath)
		if err != nil {
			mp.PID = 0
			return false
		}
		fields := strings.Fields(string(data))
		if len(fields) > 2 && fields[2] == "Z" {
			mp.PID = 0
			return false
		}
	}

	err = proc.Signal(syscall.Signal(0))
	if err != nil {
		mp.PID = 0
		return false
	}
	return true
}

func probeURL(url string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

func waitForHealth(url string, maxRetries int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	delay := 500 * time.Millisecond

	for attempt := 0; attempt < maxRetries; attempt++ {
		if time.Now().After(deadline) {
			return fmt.Errorf("healthcheck timeout after %s", timeout)
		}
		if probeURL(url) {
			return nil
		}
		time.Sleep(delay)
		delay *= 2
	}
	return fmt.Errorf("healthcheck failed after %d retries", maxRetries)
}

func tailBytes(data []byte, n int) []byte {
	lines := splitLines(string(data))
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	result := ""
	for _, l := range lines {
		result += l + "\n"
	}
	return []byte(result)
}

func splitLines(s string) []string {
	var lines []string
	current := ""
	for _, c := range s {
		if c == '\n' {
			lines = append(lines, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}
