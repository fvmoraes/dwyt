//go:build windows

package procutil

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

func windowsComSpec() string {
	if cs := os.Getenv("ComSpec"); cs != "" {
		return cs
	}
	return `C:\Windows\System32\cmd.exe`
}

// TestTerminateInvalidPID mirrors the Unix behaviour: a non-positive PID is
// a no-op, never an error.
func TestTerminateInvalidPID(t *testing.T) {
	if err := Terminate(0); err != nil {
		t.Fatalf("Terminate(0) = %v, want nil", err)
	}
	if err := Terminate(-1); err != nil {
		t.Fatalf("Terminate(-1) = %v, want nil", err)
	}
}

// TestTerminateAlreadyExitedProcessSucceeds is the regression test: a PID
// that has already exited by the time Terminate runs — the daemon finishing
// its own shutdown before the CLI's cleanup gets to it, for example — must be
// treated as already stopped, not surfaced as a "failed to terminate" error.
// Before this fix, taskkill's non-zero exit for "no such process" was
// returned verbatim.
func TestTerminateAlreadyExitedProcessSucceeds(t *testing.T) {
	cmd := exec.Command(windowsComSpec(), "/c", "exit 0")
	if err := cmd.Run(); err != nil {
		t.Fatalf("spawning a short-lived process failed: %v", err)
	}
	pid := cmd.Process.Pid

	// cmd.Run() already waited for exit, so the PID is gone (and, on a busy
	// CI box, may even have been reused by an unrelated process by the time
	// this runs — Terminate must not mistakenly kill that unrelated process,
	// which the Alive-gated taskkill call structurally cannot do here since
	// the test asserts success either way).
	if err := Terminate(pid); err != nil {
		t.Fatalf("Terminate on an already-exited PID = %v, want nil", err)
	}
}

// TestTerminateKillsRunningProcess confirms the happy path still works: a
// genuinely live process is force-killed and Alive reports it gone
// afterward.
func TestTerminateKillsRunningProcess(t *testing.T) {
	cmd := exec.Command(windowsComSpec(), "/c", "ping -n 30 127.0.0.1 >NUL")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start long-running process: %v", err)
	}
	pid := cmd.Process.Pid
	defer cmd.Wait()

	deadline := time.Now().Add(2 * time.Second)
	for !Alive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !Alive(pid) {
		t.Fatalf("spawned process %d never showed up as alive", pid)
	}

	if err := Terminate(pid); err != nil {
		t.Fatalf("Terminate on a live process = %v, want nil", err)
	}

	deadline = time.Now().Add(3 * time.Second)
	for Alive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if Alive(pid) {
		t.Fatalf("process %d still alive after Terminate", pid)
	}
}
