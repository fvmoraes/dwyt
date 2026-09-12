package health

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"time"

	"github.com/fvmoraes/dwyt/internal/log"
)

type Check struct {
	Name    string `json:"name"`
	Running bool   `json:"running"`
	Healthy bool   `json:"healthy"`
	Port    int    `json:"port,omitempty"`
	Details string `json:"details,omitempty"`
	Error   string `json:"error,omitempty"`
}

type Process struct {
	Cmd     *exec.Cmd
	Bin     string
	Name    string
	Port    int
	Started time.Time
}

var activeProcesses []*Process

func StartService(name, bin, healthURL string, args ...string) (*Check, error) {
	log.Info("starting service", log.Fields{"name": name, "bin": bin})
	cmd := exec.Command(bin, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil

	if err := cmd.Start(); err != nil {
		return &Check{
			Name:    name,
			Running: false,
			Healthy: false,
			Error:   fmt.Sprintf("failed to start: %v", err),
		}, err
	}

	p := &Process{Cmd: cmd, Bin: bin, Name: name, Started: time.Now()}
	activeProcesses = append(activeProcesses, p)

	if healthURL != "" {
		check := WaitForHTTP(healthURL, 30*time.Second, 500*time.Millisecond)
		if !check.Healthy {
			log.Warn("service started but not healthy", log.Fields{"name": name, "url": healthURL})
			return check, fmt.Errorf("service %s unhealthy after start", name)
		}
	}

	if healthURL == "" {
		time.Sleep(200 * time.Millisecond)
		if err := cmd.Process.Signal(nil); err != nil {
			return &Check{
				Name:    name,
				Running: false,
				Healthy: false,
				Error:   fmt.Sprintf("process died immediately: %v", err),
			}, fmt.Errorf("service %s died immediately", name)
		}
	}

	log.Info("service started successfully", log.Fields{"name": name})
	return &Check{
		Name:    name,
		Running: true,
		Healthy: true,
		Port:    p.Port,
	}, nil
}

func WaitForHTTP(url string, timeout, interval time.Duration) *Check {
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)
	var lastCloseErr error

	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			statusCode := resp.StatusCode
			if closeErr := resp.Body.Close(); closeErr != nil {
				lastCloseErr = closeErr
			} else if statusCode == http.StatusOK {
				return &Check{Running: true, Healthy: true}
			}
		}
		time.Sleep(interval)
	}

	check := &Check{
		Running: false,
		Healthy: false,
		Error:   fmt.Sprintf("healthcheck timeout after %s", timeout),
	}
	if lastCloseErr != nil {
		check.Error = fmt.Sprintf("healthcheck timeout after %s: response body close failed: %v", timeout, lastCloseErr)
	}
	return check
}

func ProbePort(port int) bool {
	url := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	client := &http.Client{Timeout: 1 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	statusCode := resp.StatusCode
	if err := resp.Body.Close(); err != nil {
		log.Warn("health probe response body close failed", log.Fields{"port": port, "error": err})
		return false
	}
	return statusCode == http.StatusOK
}

func ProbeURL(url string) bool {
	return ProbeURLContext(context.Background(), url)
}

func ProbeURLContext(ctx context.Context, url string) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	client := &http.Client{Timeout: 1 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

func IsPortOccupied(port int) bool {
	// A health endpoint is not a port-availability check: a stale process or
	// an unrelated local service may own the TCP port without serving HTTP.
	// Binding and immediately closing the exact loopback address Headroom uses
	// is the portable way to decide whether a new listener can start there.
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return true
	}
	_ = listener.Close()
	return false
}

// ErrPortConflict reports that none of the bounded candidate ports can be
// reserved. Callers must surface/degrade this condition instead of retrying an
// already-occupied requested port through a full health timeout.
var ErrPortConflict = fmt.Errorf("no free port in bounded candidate range")

func FindFreePortE(defaultPort int) (int, error) {
	if defaultPort <= 0 || defaultPort > 65535 {
		return defaultPort, nil
	}
	for offset := 0; offset < 5 && defaultPort+offset <= 65535; offset++ {
		port := defaultPort + offset
		if !IsPortOccupied(port) {
			return port, nil
		}
	}
	return 0, fmt.Errorf("%w starting at %d", ErrPortConflict, defaultPort)
}

// FindFreePort is the compatibility helper for advisory callers. Lifecycle
// code must use FindFreePortE so exhaustion is never converted into a port.
func FindFreePort(defaultPort int) int {
	port, err := FindFreePortE(defaultPort)
	if err != nil {
		return defaultPort
	}
	return port
}

func StopAll() {
	for _, p := range activeProcesses {
		if p.Cmd != nil && p.Cmd.Process != nil {
			log.Info("stopping service", log.Fields{"name": p.Name})
			if err := p.Cmd.Process.Kill(); err != nil {
				log.Warn("failed to stop service", log.Fields{"name": p.Name, "error": err})
			}
		}
	}
	activeProcesses = nil
}
