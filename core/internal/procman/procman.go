package procman

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fvmoraes/dwyt/internal/health"
	"github.com/fvmoraes/dwyt/internal/log"
	"github.com/fvmoraes/dwyt/internal/procutil"
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
	cmd       *exec.Cmd     // handle to the running child, for reaping
	done      chan struct{} // closed by the reaper when the child exits
	logFiles  []*os.File    // stdout/stderr handles, closed when the child exits
	logMu     sync.Mutex    // guards logFiles; separate from mu to avoid deadlock between Stop (holds mu, waits for done) and the reaper
	mu        sync.Mutex
}

type ProcessManager struct {
	processes map[string]*ManagedProcess
	mu        sync.RWMutex
	logDir    string
	dwytHome  string
}

func New(dwytHome string) *ProcessManager {
	logDir := filepath.Join(dwytHome, "logs")
	os.MkdirAll(logDir, 0755)
	return &ProcessManager{
		processes: make(map[string]*ManagedProcess),
		logDir:    logDir,
		dwytHome:  dwytHome,
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

// buildServiceCommand constructs the exec.Cmd for a managed binary. On Windows
// a ".bat" shim (e.g. the headroom launcher) cannot be executed directly by
// CreateProcess, so it is run through "cmd /c". Everywhere else the binary is
// invoked directly.
func buildServiceCommand(binPath string, args []string) *exec.Cmd {
	if runtime.GOOS == "windows" && strings.EqualFold(filepath.Ext(binPath), ".bat") {
		return exec.Command("cmd", append([]string{"/c", binPath}, args...)...)
	}
	return exec.Command(binPath, args...)
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

	cmd := buildServiceCommand(binPath, args)
	setManagedProcessAttr(cmd)
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

	stdout, err := os.Create(stdoutPath)
	if err != nil {
		return &ServiceStatus{Name: name, Status: "error", State: "error", Error: fmt.Sprintf("open stdout log: %v", err)}, err
	}
	stderr, err := os.Create(stderrPath)
	if err != nil {
		stdout.Close()
		return &ServiceStatus{Name: name, Status: "error", State: "error", Error: fmt.Sprintf("open stderr log: %v", err)}, err
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	// Track the handles so they can be closed as soon as the child exits —
	// on Windows an open handle makes the log file (and its directory)
	// undeletable, which breaks TempDir cleanup in tests and log rotation.
	mp.logFiles = []*os.File{stdout, stderr}

	if err := cmd.Start(); err != nil {
		mp.closeLogFiles()
		return &ServiceStatus{Name: name, Status: "error", State: "error", Error: fmt.Sprintf("failed to start: %v", err)}, err
	}

	mp.PID = cmd.Process.Pid
	mp.StartedAt = time.Now()
	mp.cmd = cmd
	procutil.WritePID(pm.dwytHome, name, mp.PID)
	// Reaper: wait on the child so it never lingers as a zombie when it exits
	// on its own (crash) or is signalled by Stop(). Clears PID once reaped
	// and releases the log file handles.
	done := make(chan struct{})
	mp.done = done
	go func() {
		cmd.Wait()
		mp.closeLogFiles()
		close(done)
		mp.mu.Lock()
		if mp.cmd == cmd {
			mp.PID = 0
			mp.cmd = nil
			procutil.RemovePID(pm.dwytHome, name)
		}
		mp.mu.Unlock()
	}()
	log.Info("process started", log.Fields{"service": name, "pid": mp.PID, "port": mp.Port})

	if mp.HealthURL != "" {
		healthURL := fmt.Sprintf("http://127.0.0.1:%d%s", mp.Port, mp.HealthURL)
		timeout := managedHealthcheckTimeout()
		if err := waitForHealth(healthURL, timeout); err != nil {
			// Kill the process that failed its healthcheck; the reaper collects it.
			if cmd.Process != nil {
				procutil.TerminateTree(cmd.Process.Pid)
			}
			log.Warn("process started but healthcheck failed, killed", log.Fields{"service": name, "pid": mp.PID, "url": healthURL, "waited": timeout.String(), "error": err.Error()})
			return &ServiceStatus{Name: name, Status: "error", State: "error", Running: false, Healthy: false, PID: 0, Port: mp.Port, Error: err.Error()}, err
		}
		log.Info("process healthy", log.Fields{"service": name, "port": mp.Port})
	}

	return pm.statusLocked(mp), nil
}

// closeLogFiles releases the stdout/stderr handles for the last spawn.
// Safe to call multiple times and from both the reaper goroutine and Stop;
// guarded by its own mutex so neither caller can deadlock the other.
func (mp *ManagedProcess) closeLogFiles() {
	mp.logMu.Lock()
	defer mp.logMu.Unlock()
	for _, f := range mp.logFiles {
		if f != nil {
			f.Close()
		}
	}
	mp.logFiles = nil
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

	// Prefer the tracked child handle so we coordinate with the reaper
	// goroutine (which owns the single Wait()). procutil.Terminate is
	// cross-platform (SIGTERM→SIGKILL on Unix, taskkill /F /T on Windows).
	pid := mp.PID
	done := mp.done
	procutil.TerminateTree(pid)
	if done != nil {
		select {
		case <-done:
		case <-time.After(6 * time.Second):
			log.Warn("process didn't exit after terminate", log.Fields{"service": name, "pid": pid})
			procutil.TerminateTree(pid)
			<-done
		}
	}
	mp.closeLogFiles()

	mp.PID = 0
	mp.cmd = nil
	procutil.RemovePID(pm.dwytHome, name)
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

// Running reports whether the managed child is alive. It relies on the reaper
// goroutine, which clears PID/cmd the instant the process exits — so this is
// accurate and cross-platform without probing /proc or sending signals.
func (mp *ManagedProcess) Running() bool {
	return mp.PID != 0 && mp.cmd != nil
}

func probeURL(url string) bool {
	ok, _ := probeHealthURL(url, 2*time.Second)
	return ok
}

func probeHealthURL(url string, timeout time.Duration) (bool, error) {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(url)
	if err != nil {
		return false, err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return true, nil
}

// managedHealthcheckTimeout shares the startup budget with the daemon. The
// Windows default accounts for slower launcher and Python environment startup.
func managedHealthcheckTimeout() time.Duration {
	defaultSeconds := 60
	if runtime.GOOS == "windows" {
		defaultSeconds = 120
	}
	if raw := strings.TrimSpace(os.Getenv("DWYT_DAEMON_HEALTHCHECK_TIMEOUT_SECONDS")); raw != "" {
		if seconds, err := strconv.Atoi(raw); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	return time.Duration(defaultSeconds) * time.Second
}

// waitForHealth probes immediately and retries at a stable cadence until the
// total deadline. HTTP 200 is intentionally sufficient: Headroom may expose
// optional components (such as kompress) as degraded while it is ready.
func waitForHealth(url string, timeout time.Duration) error {
	started := time.Now()
	deadline := started.Add(timeout)
	lastError := "not attempted"
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("healthcheck timeout: url=%s last_error=%s waited=%s", url, lastError, time.Since(started).Round(time.Millisecond))
		}
		requestTimeout := 2 * time.Second
		if remaining < requestTimeout {
			requestTimeout = remaining
		}
		if ok, err := probeHealthURL(url, requestTimeout); ok {
			return nil
		} else if err != nil {
			lastError = err.Error()
		}
		remaining = time.Until(deadline)
		if remaining <= 0 {
			continue
		}
		sleep := 500 * time.Millisecond
		if remaining < sleep {
			sleep = remaining
		}
		time.Sleep(sleep)
	}
}

func tailBytes(data []byte, n int) []byte {
	if n <= 0 {
		return nil
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}
