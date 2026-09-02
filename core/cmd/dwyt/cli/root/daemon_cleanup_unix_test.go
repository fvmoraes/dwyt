//go:build !windows

package root

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fvmoraes/dwyt/internal/procman"
	"github.com/fvmoraes/dwyt/internal/procutil"
)

func TestTerminateFailedDaemonStopsChildProcessTree(t *testing.T) {
	useTestDWYTHome(t)
	childPIDFile := filepath.Join(t.TempDir(), "child.pid")
	cmd := exec.Command("/bin/sh", "-c", "sleep 30 & echo $! > "+childPIDFile+"; wait")
	setDaemonAttr(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	var childPID int
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(childPIDFile)
		if err == nil {
			childPID, _ = strconv.Atoi(strings.TrimSpace(string(data)))
			if childPID > 0 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if childPID == 0 {
		terminateFailedDaemon(cmd)
		t.Fatal("child PID was not recorded")
	}

	terminateFailedDaemon(cmd)
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && procutil.Alive(childPID) {
		time.Sleep(20 * time.Millisecond)
	}
	if procutil.Alive(childPID) {
		t.Fatalf("child process %d survived daemon cleanup", childPID)
	}
}

// A normal version-mismatch restart follows stopDaemonProcess rather than the
// startup-time failure path above. Keep that path tree-aware too: otherwise a
// locally installed Headroom binary (outside DwytBin, so pkill misses it)
// survives the old daemon and forces later launches onto new ports.
func TestStopDaemonProcessStopsTrackedProcessTree(t *testing.T) {
	useTestDWYTHome(t)

	childPIDFile := filepath.Join(t.TempDir(), "restart-child.pid")
	cmd := exec.Command("/bin/sh", "-c", fmt.Sprintf("sleep 30 & echo $! > %q; wait", childPIDFile))
	setDaemonAttr(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = procutil.TerminateTree(cmd.Process.Pid)
		_ = cmd.Wait()
	})
	if err := procutil.WritePID(DwytHome, "daemon", cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}

	var childPID int
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(childPIDFile)
		if err == nil {
			childPID, _ = strconv.Atoi(strings.TrimSpace(string(data)))
			if childPID > 0 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if childPID == 0 {
		t.Fatal("child PID was not recorded")
	}

	stopDaemonProcess()
	_ = cmd.Wait() // reap the daemon so the liveness probe is not seeing a zombie
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && procutil.Alive(childPID) {
		time.Sleep(20 * time.Millisecond)
	}
	if procutil.Alive(childPID) {
		t.Fatalf("child process %d survived tracked daemon restart", childPID)
	}
	if got := procutil.ReadPID(filepath.Join(procutil.PIDDir(DwytHome), "daemon.pid")); got != 0 {
		t.Fatalf("daemon PID file was not removed, got %d", got)
	}
}

// TestTerminateFailedDaemonStopsManagedServiceTree guards the actual daemon
// topology. The helper process is a session-leading daemon and ProcessManager
// launches a service in its own process group below it. A failed daemon startup
// must first stop the PID-tracked service group, then kill the daemon session;
// otherwise the service survives as an orphaned Headroom process on Unix.
func TestTerminateFailedDaemonStopsManagedServiceTree(t *testing.T) {
	useTestDWYTHome(t)
	pidFile := filepath.Join(t.TempDir(), "managed.pid")
	daemon := exec.Command(os.Args[0], "-test.run=^TestDaemonCleanupHelper$", "--")
	daemon.Env = append(os.Environ(),
		"DWYT_DAEMON_CLEANUP_HELPER=1",
		"DWYT_MANAGED_PID_FILE="+pidFile,
		"DWYT_MANAGED_HOME="+DwytHome,
	)
	setDaemonAttr(daemon)
	if err := daemon.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = procutil.TerminateTree(daemon.Process.Pid) })

	var childPID int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(pidFile)
		if err == nil {
			childPID, err = strconv.Atoi(strings.TrimSpace(string(data)))
			if err == nil && childPID > 0 {
				break
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	if childPID <= 0 {
		terminateFailedDaemon(daemon)
		t.Fatalf("managed helper pid was not written to %s", pidFile)
	}
	t.Cleanup(func() { _ = procutil.TerminateTree(childPID) })

	terminateFailedDaemon(daemon)
	deadline = time.Now().Add(3 * time.Second)
	for procutil.Alive(childPID) && time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
	}
	if procutil.Alive(childPID) {
		t.Fatalf("managed child %d survived daemon process-tree termination", childPID)
	}
}

func TestDaemonCleanupHelper(t *testing.T) {
	if os.Getenv("DWYT_DAEMON_CLEANUP_HELPER") != "1" {
		return
	}
	pidFile := os.Getenv("DWYT_MANAGED_PID_FILE")
	if pidFile == "" {
		t.Fatal("DWYT_MANAGED_PID_FILE was not set")
	}

	managedHome := os.Getenv("DWYT_MANAGED_HOME")
	if managedHome == "" {
		t.Fatal("DWYT_MANAGED_HOME was not set")
	}
	pm := procman.New(managedHome)
	pm.Register("managed", os.Args[0], "", 0, "-test.run=^TestDaemonCleanupManagedChild$", "--")
	status, err := pm.Start("managed")
	if err != nil {
		t.Fatalf("start managed child: %v", err)
	}
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", status.PID)), 0644); err != nil {
		t.Fatalf("write managed pid: %v", err)
	}
	select {}
}

func TestDaemonCleanupManagedChild(t *testing.T) {
	if os.Getenv("DWYT_DAEMON_CLEANUP_HELPER") != "1" {
		return
	}
	select {}
}

func useTestDWYTHome(t *testing.T) string {
	t.Helper()
	oldHome, oldBin, oldData := DwytHome, DwytBin, DwytData
	DwytHome = t.TempDir()
	DwytBin = filepath.Join(DwytHome, "bin")
	DwytData = filepath.Join(DwytHome, "data")
	t.Cleanup(func() { DwytHome, DwytBin, DwytData = oldHome, oldBin, oldData })
	return DwytHome
}
