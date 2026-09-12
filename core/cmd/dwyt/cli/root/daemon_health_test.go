package root

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaitForDaemonAllowsSlowStartupPastThreeSeconds(t *testing.T) {
	var ready atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ready.Load() {
			http.Error(w, "starting", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	go func() {
		time.Sleep(3200 * time.Millisecond)
		ready.Store(true)
	}()

	result := waitForDaemonURL(server.URL, 5*time.Second, 100*time.Millisecond)
	if !result.OK {
		t.Fatalf("slow daemon should become healthy: %+v", result)
	}
	if result.Waited < 3*time.Second {
		t.Fatalf("healthcheck returned too early after %s", result.Waited)
	}
}

func TestWaitForDaemonTimesOutWithLastProbeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "starting", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	// LastError reports the final probe, not the last HTTP response. Under
	// loaded CI scheduling, a client deadline can legitimately follow an
	// earlier 503 while the local server is waiting to run.
	result := waitForDaemonURL(server.URL, 3*time.Second, 50*time.Millisecond)
	if result.OK {
		t.Fatal("daemon that never becomes ready must time out")
	}
	if result.LastError == "" {
		t.Fatal("timeout result must retain the last probe error")
	}
	if result.Waited < 2*time.Second {
		t.Fatalf("waited %s, expected to use the total startup budget", result.Waited)
	}
}

func TestDaemonHealthcheckTimeoutReadsEnvironment(t *testing.T) {
	t.Setenv("DWYT_DAEMON_HEALTHCHECK_TIMEOUT_SECONDS", "73")
	if got := daemonHealthcheckTimeout(); got != 73*time.Second {
		t.Fatalf("timeout = %s, want 73s", got)
	}

	t.Setenv("DWYT_DAEMON_HEALTHCHECK_TIMEOUT_SECONDS", "invalid")
	defaultTimeout := defaultDaemonHealthTimeout
	if runtime.GOOS == "windows" {
		defaultTimeout = windowsDaemonHealthTimeout
	}
	if got := daemonHealthcheckTimeout(); got != defaultTimeout {
		t.Fatalf("invalid timeout = %s, want default %s", got, defaultTimeout)
	}
}

func TestRequestedHeadroomPortReadsEnvironment(t *testing.T) {
	t.Setenv("DWYT_HEADROOM_PORT", "8788")
	if got := requestedHeadroomPort(); got != 8788 {
		t.Fatalf("requested port = %d, want 8788", got)
	}

	t.Setenv("DWYT_HEADROOM_PORT", "not-a-port")
	if got := requestedHeadroomPort(); got != 8787 {
		t.Fatalf("invalid requested port = %d, want default 8787", got)
	}
}
