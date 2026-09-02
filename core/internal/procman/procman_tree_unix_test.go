//go:build !windows

package procman

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fvmoraes/dwyt/internal/procutil"
)

// A failed healthcheck must stop the service's whole process group, not just
// its shell/launcher. This deliberately runs under the test runner's own
// process group, so it fails if managed services inherit that group: killing
// only the parent shell leaves the background sleep orphaned.
func TestProcessManagerHealthcheckFailureKillsServiceDescendants(t *testing.T) {
	t.Setenv("DWYT_DAEMON_HEALTHCHECK_TIMEOUT_SECONDS", "1")
	pidFile := filepath.Join(t.TempDir(), "service-child.pid")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	pm := New(t.TempDir())
	script := fmt.Sprintf("sleep 30 & echo $! > %q; wait", pidFile)
	pm.Register("tree", "/bin/sh", "/health", port, "-c", script)
	if _, err := pm.Start("tree"); err == nil {
		t.Fatal("expected healthcheck failure")
	}

	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read spawned child PID: %v", err)
	}
	childPID, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || childPID <= 0 {
		t.Fatalf("invalid child PID %q: %v", data, err)
	}
	t.Cleanup(func() { _ = procutil.TerminateTree(childPID) })

	deadline := time.Now().Add(3 * time.Second)
	for procutil.Alive(childPID) && time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
	}
	if procutil.Alive(childPID) {
		t.Fatalf("service descendant %d survived failed healthcheck cleanup", childPID)
	}
}
