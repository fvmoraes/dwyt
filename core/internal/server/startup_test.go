package server

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/fvmoraes/dwyt/internal/toolsource"
)

// These tests pin the dashboard-first startup contract: heavy non-critical
// work (vault migrations, MCP config sync, vault stats) must never delay the
// dashboard bind, and a failing startup task must never take the daemon down
// with it. They synchronize on channels and the server's own completion
// signal — never on sleeps — following the same approach that stabilized the
// warmCodebase (PR #24) tests across CI runners.

func waitForDashboard(t *testing.T, port int, deadline time.Duration) error {
	t.Helper()
	url := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	client := &http.Client{Timeout: 500 * time.Millisecond}
	timer := time.NewTimer(deadline)
	defer timer.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			err = fmt.Errorf("health returned %d", resp.StatusCode)
		}
		select {
		case <-timer.C:
			return fmt.Errorf("dashboard did not answer %s in %s: %w", url, deadline, err)
		case <-ticker.C:
		}
	}
}

func cleanupDashboard(t *testing.T, ds *DashboardServer) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := ds.Shutdown(ctx); err != nil {
			t.Errorf("dashboard cleanup: %v", err)
		}
	})
}

// blockingStartupTask builds a task whose run blocks until release is closed
// (or ctx is cancelled). The returned channel closes when the task body is
// entered, giving the test a sleep-free "it actually started" signal.
func blockingStartupTask(name string, release <-chan struct{}) (startupTask, <-chan struct{}) {
	entered := make(chan struct{})
	task := startupTask{
		name: name,
		run: func(ctx context.Context) error {
			close(entered)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}
	return task, entered
}

// The optional Headroom probe used to execute synchronously before r.Run bound
// the dashboard. This test blocks that probe after its TCP connection is
// accepted and then asks the dashboard for /health. The request can succeed
// only when the Core listener was created before optional work began.
func TestDashboardBindsBeforeOptionalHeadroomProbeCompletes(t *testing.T) {
	probeListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer probeListener.Close()

	probeAccepted := make(chan struct{})
	releaseProbe := make(chan struct{})
	probeDone := make(chan struct{})
	go func() {
		defer close(probeDone)
		conn, acceptErr := probeListener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		close(probeAccepted)
		<-releaseProbe
		_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n")
	}()
	defer func() {
		select {
		case <-releaseProbe:
		default:
			close(releaseProbe)
		}
		<-probeDone
	}()

	binDir := t.TempDir()
	headroomBin := toolsource.ManagedPath(binDir, toolsource.ToolHeadroom)
	if err := os.MkdirAll(filepath.Dir(headroomBin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(headroomBin, []byte("test launcher"), 0o755); err != nil {
		t.Fatal(err)
	}
	probePort := probeListener.Addr().(*net.TCPAddr).Port
	t.Setenv("DWYT_HEADROOM_PORT", strconv.Itoa(probePort))

	dashboardPort := freeTCPPort(t)
	ds := New(dashboardPort, binDir, t.TempDir(), "test")
	cleanupDashboard(t, ds)
	ds.startupTasksOverride = []startupTask{}
	serverErr := make(chan error, 1)
	go func() { serverErr <- ds.Start() }()

	select {
	case <-probeAccepted:
	case <-time.After(5 * time.Second):
		t.Fatal("optional Headroom probe never started")
	}

	client := &http.Client{Timeout: time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", dashboardPort))
	if err != nil {
		t.Fatalf("dashboard was not serving while optional probe was blocked: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dashboard health status = %d, want 200", resp.StatusCode)
	}

	close(releaseProbe)
	<-probeDone
}

func TestDashboardServesWhileStartupTasksRun(t *testing.T) {
	home := t.TempDir()
	port := freeTCPPort(t)
	ds := New(port, "dwyt-test-bin", home, "test")
	cleanupDashboard(t, ds)

	release := make(chan struct{})
	blocking, blockingEntered := blockingStartupTask("blocking", release)
	var ran []string
	ds.startupTasksOverride = []startupTask{blocking, {
		name: "followup",
		run: func(ctx context.Context) error {
			ran = append(ran, "followup")
			return nil
		},
	}}

	serverErr := make(chan error, 1)
	go func() { serverErr <- ds.Start() }()

	// The dashboard must answer while the first startup task is still
	// blocked. Before the dashboard-first refactor, synchronous work in
	// New() delayed the bind; this assertion is the regression pin.
	if err := waitForDashboard(t, port, 10*time.Second); err != nil {
		t.Fatalf("dashboard did not become available while startup tasks were in flight: %v", err)
	}
	select {
	case <-blockingEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("blocking task never started")
	}
	// Tasks run sequentially: the followup cannot have run while the
	// blocking task holds. No sleep needed — mutual exclusion is structural.
	if len(ran) != 0 {
		t.Fatalf("followup ran while the first task was still blocked: %v", ran)
	}

	close(release)
	select {
	case <-ds.startupDone:
	case <-time.After(5 * time.Second):
		t.Fatal("startup tasks never completed")
	}
	if len(ran) != 1 || ran[0] != "followup" {
		t.Fatalf("followup task did not run after release: ran=%v", ran)
	}
}

func TestStartupTaskFailureDoesNotKillDashboard(t *testing.T) {
	home := t.TempDir()
	port := freeTCPPort(t)
	ds := New(port, "dwyt-test-bin", home, "test")
	cleanupDashboard(t, ds)

	failing := startupTask{
		name: "always_fails",
		run: func(ctx context.Context) error {
			return fmt.Errorf("boom")
		},
	}
	followupRan := make(chan struct{})
	ds.startupTasksOverride = []startupTask{failing, {
		name: "followup",
		run: func(ctx context.Context) error {
			close(followupRan)
			return nil
		},
	}}

	go func() { _ = ds.Start() }()

	if err := waitForDashboard(t, port, 10*time.Second); err != nil {
		t.Fatalf("dashboard unavailable after a failing startup task: %v", err)
	}
	select {
	case <-ds.startupDone:
	case <-time.After(5 * time.Second):
		t.Fatal("startup did not finish after a task failure")
	}
	select {
	case <-followupRan:
	case <-time.After(5 * time.Second):
		t.Fatal("tasks after a failure were not executed")
	}
	snapshot := ds.RuntimeState.Snapshot()
	toolErrors, ok := snapshot["tool_errors"].(map[string]string)
	if !ok || toolErrors["startup_always_fails"] != "boom" {
		t.Fatalf("startup failure was not published in ToolErrors: %#v", snapshot["tool_errors"])
	}
}

func TestStartupTasksStopOnCancelledContext(t *testing.T) {
	home := t.TempDir()
	port := freeTCPPort(t)
	ds := New(port, "dwyt-test-bin", home, "test")
	cleanupDashboard(t, ds)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	neverRan := make(chan struct{})
	ds.startupTasksOverride = []startupTask{{
		name: "must_not_run",
		run: func(ctx context.Context) error {
			close(neverRan)
			return nil
		},
	}}

	done := ds.runStartupTasks(ctx, ds.startupTasksOverride)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runStartupTasks ignored the cancelled context")
	}
	select {
	case <-neverRan:
		t.Fatal("task ran despite a cancelled context")
	default:
	}
}

func TestDashboardShutdownCancelsAndWaitsForBackgroundTasks(t *testing.T) {
	home := t.TempDir()
	port := freeTCPPort(t)
	ds := New(port, "dwyt-test-bin", home, "test")
	cleanupDashboard(t, ds)

	entered := make(chan struct{})
	exited := make(chan struct{})
	ds.startupTasksOverride = []startupTask{{
		name: "wait_for_shutdown",
		run: func(ctx context.Context) error {
			close(entered)
			<-ctx.Done()
			close(exited)
			return ctx.Err()
		},
	}}

	serverErr := make(chan error, 1)
	go func() { serverErr <- ds.Start() }()

	if err := waitForDashboard(t, port, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("background task never started")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := ds.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	select {
	case <-exited:
	default:
		t.Fatal("Shutdown returned before the background task observed cancellation")
	}
	select {
	case err := <-serverErr:
		if err != nil {
			t.Fatalf("Start() after graceful shutdown error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after Shutdown")
	}

	// Shutdown is intentionally idempotent: signal handling and deferred
	// cleanup may race to invoke it during process termination.
	if err := ds.Shutdown(ctx); err != nil {
		t.Fatalf("second Shutdown() error = %v", err)
	}
}

func TestDashboardShutdownBeforeStartIsLatched(t *testing.T) {
	ds := New(freeTCPPort(t), "dwyt-test-bin", t.TempDir(), "test")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := ds.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown before Start error = %v", err)
	}

	started := make(chan error, 1)
	go func() { started <- ds.Start() }()
	select {
	case err := <-started:
		if err != nil {
			t.Fatalf("latched Start error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Start served after a pre-bind Shutdown request")
	}
}

func TestVaultEndpointsFailFastDuringStructuralMigration(t *testing.T) {
	port := freeTCPPort(t)
	ds := New(port, "dwyt-test-bin", t.TempDir(), "test")
	cleanupDashboard(t, ds)

	entered := make(chan struct{})
	release := make(chan struct{})
	ds.startupTasksOverride = []startupTask{{
		name: "structural_vault_migration",
		run: func(ctx context.Context) error {
			return ds.withVaultMigration(ctx, func(ctx context.Context) error {
				close(entered)
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
		},
	}}

	serverErr := make(chan error, 1)
	go func() { serverErr <- ds.Start() }()
	if err := waitForDashboard(t, port, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("structural migration never acquired the vault lease")
	}

	client := &http.Client{Timeout: time.Second}
	healthResp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/health", port))
	if err != nil {
		t.Fatalf("health during migration: %v", err)
	}
	healthResp.Body.Close()
	if healthResp.StatusCode != http.StatusOK {
		t.Fatalf("health during migration status = %d, want 200", healthResp.StatusCode)
	}

	vaultResp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/obsidian/status", port))
	if err != nil {
		t.Fatalf("vault request during migration: %v", err)
	}
	vaultResp.Body.Close()
	if vaultResp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("vault status during migration = %d, want 503", vaultResp.StatusCode)
	}
	if got := vaultResp.Header.Get("Retry-After"); got != "1" {
		t.Fatalf("Retry-After = %q, want 1", got)
	}

	close(release)
	select {
	case <-ds.startupDone:
	case <-time.After(5 * time.Second):
		t.Fatal("structural migration never completed")
	}
}

func TestDashboardHTTPShutdownDrainsBeforeStartReturns(t *testing.T) {
	port := freeTCPPort(t)
	ds := New(port, "dwyt-test-bin", t.TempDir(), "test")
	ds.startupTasksOverride = []startupTask{}
	serverErr := make(chan error, 1)
	go func() { serverErr <- ds.Start() }()
	if err := waitForDashboard(t, port, 10*time.Second); err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/api/shutdown", port), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("POST /api/shutdown: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("shutdown status = %d, want 202", resp.StatusCode)
	}
	select {
	case err := <-serverErr:
		if err != nil {
			t.Fatalf("Start after HTTP shutdown = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start returned before neither shutdown completion nor deadline")
	}
}
