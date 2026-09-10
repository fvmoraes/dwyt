package server

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
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
		case <-time.After(25 * time.Millisecond):
		}
	}
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

func TestDashboardServesWhileStartupTasksRun(t *testing.T) {
	home := t.TempDir()
	port := freeTCPPort(t)
	ds := New(port, "dwyt-test-bin", home, "test")

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
}

func TestStartupTasksStopOnCancelledContext(t *testing.T) {
	home := t.TempDir()
	port := freeTCPPort(t)
	ds := New(port, "dwyt-test-bin", home, "test")

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
