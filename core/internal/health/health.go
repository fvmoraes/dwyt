package health

import (
	"fmt"
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

	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return &Check{Running: true, Healthy: true}
			}
		}
		time.Sleep(interval)
	}

	return &Check{
		Running: false,
		Healthy: false,
		Error:   fmt.Sprintf("healthcheck timeout after %s", timeout),
	}
}

func ProbePort(port int) bool {
	url := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	client := &http.Client{Timeout: 1 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

func ProbeURL(url string) bool {
	client := &http.Client{Timeout: 1 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

func IsPortOccupied(port int) bool {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
}

func FindFreePort(defaultPort int) int {
	for offset := 0; offset < 5; offset++ {
		port := defaultPort + offset
		if !IsPortOccupied(port) {
			return port
		}
	}
	return defaultPort
}

func StopAll() {
	for _, p := range activeProcesses {
		if p.Cmd != nil && p.Cmd.Process != nil {
			log.Info("stopping service", log.Fields{"name": p.Name})
			p.Cmd.Process.Kill()
		}
	}
	activeProcesses = nil
}
