package server

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/fvmoraes/dwyt/internal/procman"
)

// warmTestFailingCmd returns a process that exits immediately with a
// non-zero code and never opens a health port, on any platform.
func warmTestFailingCmd() (string, []string) {
	if runtime.GOOS == "windows" {
		cs := os.Getenv("ComSpec")
		if cs == "" {
			cs = `C:\Windows\System32\cmd.exe`
		}
		return cs, []string{"/c", "exit 1"}
	}
	return "/bin/false", nil
}

// freeTCPPort binds an ephemeral port, closes the listener, and returns the
// port number so a test can register a service against an address nothing
// is listening on.
func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not reserve a free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// TestWarmCodebaseDoesNotBlockCaller is the regression test for the Windows
// daemon-startup race: warmCodebase used to run procman.Start synchronously
// inside server.New(), which waits out the full managed healthcheck budget
// (60-120s) when the service never answers /health. That blocked New() long
// enough for the CLI's own, similarly-sized wait for the dashboard port to
// expire first and kill the daemon before it ever bound that port. warmCodebase
// must return immediately regardless of how long the service takes to (fail
// to) become healthy.
func TestWarmCodebaseDoesNotBlockCaller(t *testing.T) {
	t.Setenv("DWYT_SERVICE_HEALTHCHECK_TIMEOUT_SECONDS", "1")

	home := t.TempDir()
	pm := procman.New(home)
	bin, args := warmTestFailingCmd()
	// A registered service that exits immediately and never opens its health
	// port stands in for a Codebase build with an incompatible /health route:
	// procman.Start will spend the whole configured budget retrying before
	// giving up.
	pm.Register("codebase", bin, "/health", freeTCPPort(t), args...)

	// A closed local port (not a privileged/reserved one like :1, whose
	// connect() behavior on a client varies by platform and can itself take
	// close to the OS's connect timeout) so the fast-path probe fails fast
	// and consistently everywhere.
	unreachable := fmt.Sprintf("http://127.0.0.1:%d/health", freeTCPPort(t))

	started := time.Now()
	done := warmCodebase(pm, unreachable)
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("warmCodebase must return without waiting for the service healthcheck, took %s", elapsed)
	}

	// Synchronize on the completion channel rather than polling the
	// filesystem or procman's mutex-guarded status for evidence the
	// background attempt ran: real process/log-file timing proved flaky
	// across CI runners (observed on macOS), while this ties the assertion
	// directly to warmCodebase's own goroutine finishing. A generous bound
	// still comfortably exceeds the configured 1s service-healthcheck
	// budget even under CI load.
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("warmCodebase's background attempt never completed")
	}
}

// TestWarmCodebaseSkipsStartWhenAlreadyHealthy preserves the fast path: a
// service that is already answering 200 on its health endpoint (e.g. a
// process left over from a previous daemon) must not be probed via procman
// or restarted.
func TestWarmCodebaseSkipsStartWhenAlreadyHealthy(t *testing.T) {
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer healthy.Close()

	home := t.TempDir()
	pm := procman.New(home)
	// Intentionally left unregistered: if warmCodebase falls through to
	// pm.Status/Start despite the healthy probe, that call panics/errors
	// loudly rather than silently passing.
	done := warmCodebase(pm, healthy.URL+"/health")

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("warmCodebase never completed for an already-healthy service")
	}
	if status := pm.Status("codebase"); status != nil && status.PID != 0 {
		t.Fatalf("warmCodebase must not start an already-healthy service, got %+v", status)
	}
}
