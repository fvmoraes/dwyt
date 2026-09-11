package procman

import (
	"context"
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
	Name          string `json:"name"`
	Status        string `json:"status"`
	State         string `json:"state,omitempty"`
	Running       bool   `json:"running"`
	Healthy       bool   `json:"healthy"`
	PID           int    `json:"pid"`
	Port          int    `json:"port"` // compatibility alias for EffectivePort
	RequestedPort int    `json:"requested_port,omitempty"`
	EffectivePort int    `json:"effective_port,omitempty"`
	Uptime        string `json:"uptime,omitempty"`
	Error         string `json:"error,omitempty"`
}

type ManagedProcess struct {
	Name          string
	Bin           string
	Args          []string
	RequestedPort int
	Port          int // effective port selected for the current/next spawn; 0 until a spawn is in flight
	HealthURL     string
	PID           int
	StartedAt     time.Time
	LogDir        string
	cmd           *exec.Cmd     // handle to the running child, for reaping
	done          chan struct{} // closed by the reaper after it clears PID/cmd on child exit
	logFiles      []*os.File    // stdout/stderr handles, closed when the child exits
	logMu         sync.Mutex    // guards logFiles; separate from mu to avoid deadlock between Stop (holds opMu, waits for done) and the reaper
	// opMu serializes lifecycle operations (Start/Stop/Restart) for this one
	// service. It is held for the whole operation, but never while probing
	// HTTP, waiting for health, terminating a tree, or waiting for the child
	// to exit — those either release it or run without it. mu guards the
	// mutable state fields and is only ever held for short snapshot/mutation
	// critical sections.
	opMu sync.Mutex
	mu   sync.Mutex
}

type ProcessManager struct {
	processes            map[string]*ManagedProcess
	mu                   sync.RWMutex
	logDir               string
	dwytHome             string
	terminateTree        func(int) error // optional test override
	terminateTreeContext func(context.Context, int) error
	stopTimeout          time.Duration
	prober               *probeCoalescer
}

const defaultProcessStopTimeout = 6 * time.Second

// defaultProbeCacheTTL is how long a health-probe result (positive or
// negative) is reused before a fresh GET is issued. Kept short so Status
// reflects reality quickly while still collapsing bursts of concurrent
// Status calls into a single request.
const defaultProbeCacheTTL = 1 * time.Second

func New(dwytHome string) *ProcessManager {
	logDir := filepath.Join(dwytHome, "logs")
	os.MkdirAll(logDir, 0755)
	return &ProcessManager{
		processes:            make(map[string]*ManagedProcess),
		logDir:               logDir,
		dwytHome:             dwytHome,
		terminateTreeContext: procutil.TerminateTreeContext,
		stopTimeout:          defaultProcessStopTimeout,
		prober:               newProbeCoalescer(defaultProbeCacheTTL),
	}
}

func (pm *ProcessManager) Register(name, bin, healthURL string, port int, args ...string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.processes[name] = &ManagedProcess{
		Name: name, Bin: bin, Args: args,
		// RequestedPort remembers what the caller asked for; the effective
		// Port stays 0 until StartContext selects a free port for a spawn.
		RequestedPort: port, Port: 0, HealthURL: healthURL,
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
	return pm.StartContext(context.Background(), name)
}

func (pm *ProcessManager) StartContext(ctx context.Context, name string) (*ServiceStatus, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return &ServiceStatus{Name: name, Status: "cancelled", State: "cancelled", Error: err.Error()}, err
	}
	mp := pm.get(name)
	if mp == nil {
		return nil, fmt.Errorf("service %s not registered", name)
	}

	// Serialize lifecycle operations for this service. opMu is released before
	// any blocking wait, so a concurrent Status can still take mp.mu.
	mp.opMu.Lock()
	defer mp.opMu.Unlock()

	if err := ctx.Err(); err != nil {
		return &ServiceStatus{Name: name, Status: "cancelled", State: "cancelled", Error: err.Error()}, err
	}
	if pm.runningSnapshot(mp) {
		return pm.status(mp), nil
	}

	binPath := mp.Bin
	if _, err := os.Stat(binPath); err != nil {
		requested := pm.requestedPortSnapshot(mp)
		return &ServiceStatus{
			Name: name, Status: "not_installed", State: "not_installed",
			RequestedPort: requested, EffectivePort: 0,
			Error: fmt.Sprintf("binary not found: %s", binPath),
		}, err
	}

	requestedPort := pm.requestedPortSnapshot(mp)
	effectivePort := requestedPort
	if requestedPort > 0 {
		freePort, err := health.FindFreePortE(requestedPort)
		if err != nil {
			mp.mu.Lock()
			mp.Port = 0
			mp.mu.Unlock()
			return &ServiceStatus{
				Name: name, Status: "port_conflict", State: "port_conflict",
				RequestedPort: requestedPort, EffectivePort: 0,
				Error: err.Error(),
			}, fmt.Errorf("select port for service %s: %w", name, err)
		}
		if freePort != requestedPort {
			log.Info("port was occupied, using alternative", log.Fields{"service": name, "requested": requestedPort, "effective": freePort})
		}
		effectivePort = freePort
	}
	// Publish the effective port so Status reflects the spawn in flight.
	mp.mu.Lock()
	mp.Port = effectivePort
	mp.mu.Unlock()

	args := make([]string, len(mp.Args))
	copy(args, mp.Args)
	for i, a := range args {
		if a == "{port}" {
			args[i] = fmt.Sprintf("%d", effectivePort)
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
		return &ServiceStatus{
			Name: name, Status: "error", State: "error",
			RequestedPort: requestedPort, EffectivePort: effectivePort, Port: effectivePort,
			Error: fmt.Sprintf("open stdout log: %v", err),
		}, err
	}
	stderr, err := os.Create(stderrPath)
	if err != nil {
		stdout.Close()
		return &ServiceStatus{
			Name: name, Status: "error", State: "error",
			RequestedPort: requestedPort, EffectivePort: effectivePort, Port: effectivePort,
			Error: fmt.Sprintf("open stderr log: %v", err),
		}, err
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	// Track the handles so they can be closed as soon as the child exits —
	// on Windows an open handle makes the log file (and its directory)
	// undeletable, which breaks TempDir cleanup in tests and log rotation.
	mp.logMu.Lock()
	mp.logFiles = []*os.File{stdout, stderr}
	mp.logMu.Unlock()

	if err := ctx.Err(); err != nil {
		mp.closeLogFiles()
		return &ServiceStatus{
			Name: name, Status: "cancelled", State: "cancelled",
			RequestedPort: requestedPort, EffectivePort: effectivePort, Port: effectivePort,
			Error: err.Error(),
		}, err
	}
	if err := cmd.Start(); err != nil {
		mp.closeLogFiles()
		return &ServiceStatus{
			Name: name, Status: "error", State: "error",
			RequestedPort: requestedPort, EffectivePort: effectivePort, Port: effectivePort,
			Error: fmt.Sprintf("failed to start: %v", err),
		}, err
	}

	// Publish cmd/PID/done and launch the reaper before any health wait, so a
	// concurrent Status/Stop sees a fully tracked process and the child is
	// never observable without a reaper attached.
	pid := cmd.Process.Pid
	done := make(chan struct{})
	mp.mu.Lock()
	mp.PID = pid
	mp.StartedAt = time.Now()
	mp.cmd = cmd
	mp.done = done
	mp.mu.Unlock()
	pm.startReaper(mp, cmd, name, done)

	// WritePID is mandatory: the PID record is the cross-process authority the
	// daemon and CLI rely on to stop this service. If it cannot be published we
	// must not leave an untracked child running — terminate it and let the
	// reaper collect it before returning the error.
	if err := procutil.WritePID(pm.dwytHome, name, pid); err != nil {
		pm.terminateAndReap(cmd, done)
		log.Warn("failed to record PID, killed untracked child", log.Fields{"service": name, "pid": pid, "error": err.Error()})
		return &ServiceStatus{
			Name: name, Status: "error", State: "error", Running: false, Healthy: false, PID: 0,
			RequestedPort: requestedPort, EffectivePort: effectivePort, Port: effectivePort,
			Error: fmt.Sprintf("record PID: %v", err),
		}, fmt.Errorf("record PID for service %s: %w", name, err)
	}

	log.Info("process started", log.Fields{"service": name, "pid": pid, "port": effectivePort})

	if mp.HealthURL != "" {
		healthURL := fmt.Sprintf("http://127.0.0.1:%d%s", effectivePort, mp.HealthURL)
		timeout := managedHealthcheckTimeout()
		// Health wait runs without any state mutex held (opMu is not a state
		// lock and is intentionally kept — it only serializes this service's
		// lifecycle, and Status never takes it).
		if err := waitForHealthContext(ctx, healthURL, timeout); err != nil {
			// Kill the process that failed its healthcheck (or was cancelled);
			// reap it so no untracked child survives.
			pm.terminateAndReap(cmd, done)
			log.Warn("process started but healthcheck failed, killed", log.Fields{"service": name, "pid": pid, "url": healthURL, "waited": timeout.String(), "error": err.Error()})
			return &ServiceStatus{
				Name: name, Status: "error", State: "error", Running: false, Healthy: false, PID: 0,
				RequestedPort: requestedPort, EffectivePort: effectivePort, Port: effectivePort,
				Error: err.Error(),
			}, err
		}
		log.Info("process healthy", log.Fields{"service": name, "port": effectivePort})
	}

	return pm.status(mp), nil
}

// startReaper waits on the child so it never lingers as a zombie when it exits
// on its own (crash) or is signalled by Stop(). It clears PID/cmd and releases
// the log handles BEFORE closing done, so any waiter that observes done sees a
// process already scrubbed from state.
func (pm *ProcessManager) startReaper(mp *ManagedProcess, cmd *exec.Cmd, name string, done chan struct{}) {
	go func() {
		cmd.Wait()
		mp.closeLogFiles()
		mp.mu.Lock()
		if mp.cmd == cmd {
			mp.PID = 0
			mp.cmd = nil
			procutil.RemovePID(pm.dwytHome, name)
		}
		mp.mu.Unlock()
		close(done)
	}()
}

// terminateAndReap forces the given child down and waits for its reaper to
// finish, guaranteeing no untracked process survives an aborted start. It
// holds no state mutex while blocking.
func (pm *ProcessManager) terminateAndReap(cmd *exec.Cmd, done <-chan struct{}) {
	if cmd != nil && cmd.Process != nil {
		if err := procutil.TerminateTree(cmd.Process.Pid); err != nil {
			log.Warn("failed to terminate process tree during aborted start", log.Fields{
				"pid":   cmd.Process.Pid,
				"error": err.Error(),
			})
		}
	}
	if done != nil {
		<-done
	}
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

func (pm *ProcessManager) terminateProcessTreeContext(ctx context.Context, pid int) error {
	if pm.terminateTree != nil {
		return pm.terminateTree(pid)
	}
	if pm.terminateTreeContext != nil {
		return pm.terminateTreeContext(ctx, pid)
	}
	return procutil.TerminateTreeContext(ctx, pid)
}

func (pm *ProcessManager) processStopTimeout() time.Duration {
	if pm.stopTimeout > 0 {
		return pm.stopTimeout
	}
	return defaultProcessStopTimeout
}

func waitForProcessExitContext(ctx context.Context, done <-chan struct{}, timeout time.Duration) bool {
	if done == nil {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	case <-timer.C:
		return false
	}
}

func (pm *ProcessManager) Stop(name string) (*ServiceStatus, error) {
	return pm.StopContext(context.Background(), name)
}

func (pm *ProcessManager) StopContext(ctx context.Context, name string) (*ServiceStatus, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	mp := pm.get(name)
	if mp == nil {
		return nil, fmt.Errorf("service %s not registered", name)
	}

	// Serialize with other lifecycle operations for this service, but take a
	// short state snapshot rather than holding mp.mu across the blocking waits.
	mp.opMu.Lock()
	defer mp.opMu.Unlock()

	mp.mu.Lock()
	running := mp.PID != 0 && mp.cmd != nil
	pid := mp.PID
	done := mp.done
	mp.mu.Unlock()

	if !running {
		return pm.status(mp), nil
	}

	// Coordinate with the reaper goroutine, which owns the single Wait() and
	// scrubs PID/cmd before closing done. Keep the process registered until
	// that goroutine confirms its exit.
	if done == nil {
		return pm.status(mp), fmt.Errorf("stopping service %s: process %d has no exit notification", name, pid)
	}
	if err := pm.terminateProcessTreeContext(ctx, pid); err != nil {
		return pm.status(mp), fmt.Errorf("stopping service %s: terminate process tree %d: %w", name, pid, err)
	}
	timeout := pm.processStopTimeout()
	if !waitForProcessExitContext(ctx, done, timeout) {
		if err := ctx.Err(); err != nil {
			return pm.status(mp), fmt.Errorf("stopping service %s: wait for process %d: %w", name, pid, err)
		}
		if err := pm.terminateProcessTreeContext(ctx, pid); err != nil {
			return pm.status(mp), fmt.Errorf("stopping service %s after %s timeout: terminate process tree %d: %w", name, timeout, pid, err)
		}
		if !waitForProcessExitContext(ctx, done, timeout) {
			if err := ctx.Err(); err != nil {
				return pm.status(mp), fmt.Errorf("stopping service %s: wait for process %d: %w", name, pid, err)
			}
			return pm.status(mp), fmt.Errorf("stopping service %s: process %d did not exit after two %s waits", name, pid, timeout)
		}
		log.Warn("process required termination retry", log.Fields{"service": name, "pid": pid})
	}
	// The reaper already cleared PID/cmd and removed the PID record before
	// closing done; nothing else to scrub here.
	log.Info("process stopped", log.Fields{"service": name})
	return pm.status(mp), nil
}

func (pm *ProcessManager) Restart(name string) (*ServiceStatus, error) {
	return pm.RestartContext(context.Background(), name)
}

func (pm *ProcessManager) RestartContext(ctx context.Context, name string) (*ServiceStatus, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	status, err := pm.StopContext(ctx, name)
	if err != nil {
		return status, fmt.Errorf("stopping service %s before restart: %w", name, err)
	}
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return status, ctx.Err()
	case <-timer.C:
	}
	return pm.StartContext(ctx, name)
}

func (pm *ProcessManager) Status(name string) *ServiceStatus {
	mp := pm.get(name)
	if mp == nil {
		return &ServiceStatus{Name: name}
	}
	return pm.status(mp)
}

func (pm *ProcessManager) AllStatus() map[string]*ServiceStatus {
	// Snapshot the process set under the manager lock, then build each status
	// (which may probe HTTP) without holding any lock.
	pm.mu.RLock()
	procs := make([]*ManagedProcess, 0, len(pm.processes))
	for _, mp := range pm.processes {
		procs = append(procs, mp)
	}
	pm.mu.RUnlock()

	result := make(map[string]*ServiceStatus, len(procs))
	for _, mp := range procs {
		result[mp.Name] = pm.status(mp)
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

// stateSnapshot is a copy of the mutable fields taken under mp.mu, so the
// (potentially blocking) health probe in status() runs with no lock held.
type stateSnapshot struct {
	name          string
	bin           string
	healthURL     string
	requestedPort int
	effectivePort int
	pid           int
	running       bool
	startedAt     time.Time
}

func (pm *ProcessManager) snapshot(mp *ManagedProcess) stateSnapshot {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	return stateSnapshot{
		name:          mp.Name,
		bin:           mp.Bin,
		healthURL:     mp.HealthURL,
		requestedPort: mp.RequestedPort,
		effectivePort: mp.Port,
		pid:           mp.PID,
		running:       mp.PID != 0 && mp.cmd != nil,
		startedAt:     mp.StartedAt,
	}
}

func (pm *ProcessManager) runningSnapshot(mp *ManagedProcess) bool {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	return mp.PID != 0 && mp.cmd != nil
}

func (pm *ProcessManager) requestedPortSnapshot(mp *ManagedProcess) int {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	return mp.RequestedPort
}

// status snapshots the mutable state under a short lock, then runs the HTTP
// health probe (if any) OUTSIDE the lock via the coalescer, so concurrent
// Status calls for the same service issue a single GET and mp.mu stays free
// while the probe is in flight.
func (pm *ProcessManager) status(mp *ManagedProcess) *ServiceStatus {
	return pm.statusContext(context.Background(), mp)
}

func (pm *ProcessManager) statusContext(ctx context.Context, mp *ManagedProcess) *ServiceStatus {
	snap := pm.snapshot(mp)
	s := &ServiceStatus{
		Name:          snap.name,
		Port:          snap.effectivePort,
		RequestedPort: snap.requestedPort,
		EffectivePort: snap.effectivePort,
		PID:           snap.pid,
		Running:       snap.running,
	}
	if snap.running {
		s.Status = "online"
		s.State = "online"
		s.Uptime = time.Since(snap.startedAt).Round(time.Second).String()
		if snap.healthURL != "" {
			healthURL := fmt.Sprintf("http://127.0.0.1:%d%s", snap.effectivePort, snap.healthURL)
			s.Healthy = pm.prober.probe(ctx, healthURL)
			if !s.Healthy {
				s.Status = "port_open_no_health"
				s.State = "port_open_no_health"
				s.Error = "healthcheck failed"
			}
		} else {
			s.Healthy = true
		}
	} else if _, err := os.Stat(snap.bin); err != nil {
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
// The read is guarded by mu so it is safe to call concurrently.
func (mp *ManagedProcess) Running() bool {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	return mp.PID != 0 && mp.cmd != nil
}

func probeHealthURLContext(ctx context.Context, url string, timeout time.Duration) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return true, nil
}

// managedHealthcheckTimeout is this package's own startup budget for a single
// managed service (e.g. Codebase, Headroom). It is intentionally a separate
// knob from the CLI's daemonHealthcheckTimeout (cmd/dwyt/cli/root): the two
// used to share DWYT_DAEMON_HEALTHCHECK_TIMEOUT_SECONDS and start counting at
// nearly the same instant, so raising one to give a slow machine more room
// silently widened both — including the CLI's outer wait for the daemon's own
// dashboard port, which is unrelated to any one service's readiness. The
// Windows default accounts for slower launcher and Python environment startup.
func managedHealthcheckTimeout() time.Duration {
	defaultSeconds := 60
	if runtime.GOOS == "windows" {
		defaultSeconds = 120
	}
	if raw := strings.TrimSpace(os.Getenv("DWYT_SERVICE_HEALTHCHECK_TIMEOUT_SECONDS")); raw != "" {
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
	return waitForHealthContext(context.Background(), url, timeout)
}

func waitForHealthContext(ctx context.Context, url string, timeout time.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}
	started := time.Now()
	deadline := started.Add(timeout)
	lastError := "not attempted"
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("healthcheck timeout: url=%s last_error=%s waited=%s", url, lastError, time.Since(started).Round(time.Millisecond))
		}
		requestTimeout := 2 * time.Second
		if remaining < requestTimeout {
			requestTimeout = remaining
		}
		if ok, err := probeHealthURLContext(ctx, url, requestTimeout); ok {
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
		timer := time.NewTimer(sleep)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
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
