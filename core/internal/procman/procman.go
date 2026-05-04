package procman

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/fvmoraes/dwyt/internal/log"
)

type ServiceStatus struct {
	Name    string `json:"name"`
	Running bool   `json:"running"`
	Healthy bool   `json:"healthy"`
	PID     int    `json:"pid"`
	Port    int    `json:"port"`
	Uptime  string `json:"uptime,omitempty"`
	Error   string `json:"error,omitempty"`
}

type ManagedProcess struct {
	Name       string
	Bin        string
	Args       []string
	Port       int
	HealthURL  string
	PID        int
	StartedAt  time.Time
	LogDir     string
	mu         sync.Mutex
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
		return &ServiceStatus{Name: name, Error: fmt.Sprintf("binary not found: %s", binPath)}, err
	}

	freePort := findFreePort(mp.Port)
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
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil

	stdoutPath := filepath.Join(pm.logDir, name+"-stdout.log")
	stderrPath := filepath.Join(pm.logDir, name+"-stderr.log")
	os.MkdirAll(filepath.Dir(stdoutPath), 0755)

	stdout, _ := os.Create(stdoutPath)
	stderr, _ := os.Create(stderrPath)
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return &ServiceStatus{Name: name, Error: fmt.Sprintf("failed to start: %v", err)}, err
	}

	mp.PID = cmd.Process.Pid
	mp.StartedAt = time.Now()
	log.Info("process started", log.Fields{"service": name, "pid": mp.PID, "port": mp.Port})

	if mp.HealthURL != "" {
		healthURL := fmt.Sprintf("http://127.0.0.1:%d%s", mp.Port, mp.HealthURL)
		if err := waitForHealth(healthURL, 5, 10*time.Second); err != nil {
			mp.PID = 0
			log.Warn("process started but healthcheck failed", log.Fields{"service": name, "error": err.Error()})
			return &ServiceStatus{Name: name, Running: true, Healthy: false, PID: 0, Port: mp.Port, Error: err.Error()}, nil
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

	if err := proc.Kill(); err != nil {
		log.Warn("failed to kill process", log.Fields{"service": name, "pid": mp.PID, "error": err.Error()})
	}
	proc.Wait()

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
		s.Uptime = time.Since(mp.StartedAt).Round(time.Second).String()
		if mp.HealthURL != "" {
			healthURL := fmt.Sprintf("http://127.0.0.1:%d%s", mp.Port, mp.HealthURL)
			s.Healthy = probeURL(healthURL)
			if !s.Healthy {
				s.Error = "healthcheck failed"
			}
		} else {
			s.Healthy = true
		}
	}
	return s
}

func (mp *ManagedProcess) Running() bool {
	if mp.PID == 0 {
		return false
	}
	proc, err := os.FindProcess(mp.PID)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

func findFreePort(defaultPort int) int {
	for offset := 0; offset < 5; offset++ {
		port := defaultPort + offset
		if !probePort(port) {
			return port
		}
	}
	return defaultPort
}

func probePort(port int) bool {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	resp.Body.Close()
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
