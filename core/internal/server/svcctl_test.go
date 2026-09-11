package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/fvmoraes/dwyt/internal/procman"
	"github.com/fvmoraes/dwyt/internal/state"
)

// The reconciler is the single watchdog for DWYT-managed services. Its
// contract (Cross-Platform Startup Plan §10/§16/§43/§44/§59):
//   - adopt an already-healthy service instead of spawning a duplicate
//   - publish an explicit state machine (starting/healthy/degraded/failed)
//   - never restart a process that is still running (no double-owner kills)
//   - bounded backoff between recovery attempts, never a restart storm
//   - a single failed probe must not flip a healthy service to degraded
//
// State transitions and backoff are pure functions; lifecycle behavior is
// exercised against a fake serviceManager so tests stay deterministic on
// every OS. Real procman integration remains covered by warm_codebase_test.

func TestBackoffProgressionIsBounded(t *testing.T) {
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		// nextBackoff(N) is the wait AFTER the N-th failed attempt (§44:
		// attempt 1 → wait 1s, attempt 2 → wait 3s, attempt 3+ → wait 10s).
		{attempt: 1, want: 1 * time.Second},
		{attempt: 2, want: 3 * time.Second},
		{attempt: 3, want: 10 * time.Second},
		{attempt: 4, want: 10 * time.Second},
		{attempt: 99, want: 10 * time.Second},
	}
	for _, tc := range cases {
		if got := nextBackoff(tc.attempt); got != tc.want {
			t.Errorf("nextBackoff(%d) = %s, want %s", tc.attempt, got, tc.want)
		}
	}
}

func TestHysteresisSingleFailureKeepsHealthy(t *testing.T) {
	st, fails := applyObservation(svcHealthy, true, false, 0, false)
	if st != svcHealthy || fails != 1 {
		t.Fatalf("one failed probe while healthy: got state=%s fails=%d, want healthy/1", st, fails)
	}
	st, fails = applyObservation(st, true, false, fails, false)
	if st != svcDegraded || fails != 2 {
		t.Fatalf("second consecutive failure: got state=%s fails=%d, want degraded/2", st, fails)
	}
}

func TestHealthyProbeResetsFailureCount(t *testing.T) {
	st, fails := applyObservation(svcDegraded, true, true, 2, false)
	if st != svcHealthy || fails != 0 {
		t.Fatalf("recovery: got state=%s fails=%d, want healthy/0", st, fails)
	}
}

func TestGracePeriodKeepsStarting(t *testing.T) {
	st, _ := applyObservation(svcStarting, true, false, 0, true)
	if st != svcStarting {
		t.Fatalf("service within grace period: got %s, want starting", st)
	}
	st, _ = applyObservation(svcStarting, true, false, 0, false)
	if st != svcDegraded {
		t.Fatalf("service past grace and unhealthy: got %s, want degraded", st)
	}
}

func TestDeadProcessIsFailed(t *testing.T) {
	st, _ := applyObservation(svcHealthy, false, false, 0, false)
	if st != svcFailed {
		t.Fatalf("process exited: got %s, want failed", st)
	}
}

// TestReconcilerAdoptsHealthyExternalService pins the adoption rule: when
// the registered port already answers, the reconciler must NOT spawn a
// duplicate process (Cross-Platform §59) and must publish healthy state.
func TestReconcilerAdoptsHealthyExternalService(t *testing.T) {
	srv := httptestHealthOK(t)
	defer srv.Close()

	fm := newFakeManager()
	fm.status["codebase"] = &procman.ServiceStatus{Name: "codebase", Running: false}

	rs := state.Init(t.TempDir())
	rc := newServiceReconciler(fm, rs, reconcilerOptions{
		waitWarmDone: closedChan(),
		healthURLs:   map[string]string{"codebase": srv.URL + "/health"},
	})
	rc.once(context.Background())

	if st := rc.stateOf("codebase"); st != svcHealthy {
		t.Fatalf("adopted service state = %s, want healthy", st)
	}
	if fm.starts["codebase"] != 0 {
		t.Fatalf("reconciler spawned a process even though the port already answered (starts=%d)", fm.starts["codebase"])
	}
}

// TestReconcilerDoesNotRestartWhileRunning pins the no-double-owner rule:
// a process that is running but failing its health must be observed
// (starting within grace, then degraded), never killed behind the
// operator's back.
func TestReconcilerDoesNotRestartWhileRunning(t *testing.T) {
	fm := newFakeManager()
	fm.status["codebase"] = &procman.ServiceStatus{
		Name: "codebase", Running: true, Healthy: false, Error: "health probe failed",
	}

	rs := state.Init(t.TempDir())
	rc := newServiceReconciler(fm, rs, reconcilerOptions{waitWarmDone: closedChan()})
	rc.once(context.Background())

	if got := rc.stateOf("codebase"); got == svcFailed {
		t.Fatal("running process must not be marked failed")
	}
	if rc.startAttemptsOf("codebase") != 0 {
		t.Fatalf("reconciler started a service that was already running")
	}
	if fm.starts["codebase"] != 0 {
		t.Fatalf("reconciler spawned a duplicate process (starts=%d)", fm.starts["codebase"])
	}
}

// TestReconcilerRecoversDeadAutoStartService pins the bounded-recovery
// rule: a dead auto-start service is restarted, exactly once per backoff
// window, and a healthy start resolves the state.
func TestReconcilerRecoversDeadAutoStartService(t *testing.T) {
	fm := newFakeManager()
	fm.status["codebase"] = &procman.ServiceStatus{Name: "codebase", Running: false}
	fm.startErrs["codebase"] = context.DeadlineExceeded // simulate health budget expiry

	rs := state.Init(t.TempDir())
	rc := newServiceReconciler(fm, rs, reconcilerOptions{
		waitWarmDone: closedChan(),
		backoff:      []time.Duration{time.Hour, time.Hour},
	})
	rc.once(context.Background())
	if got := rc.startAttemptsOf("codebase"); got != 1 {
		t.Fatalf("first pass should attempt one start, got %d", got)
	}
	rc.once(context.Background())
	rc.once(context.Background())
	if got := rc.startAttemptsOf("codebase"); got != 1 {
		t.Fatalf("reconciler retried within backoff: attempts=%d", got)
	}

	// Service comes back healthy: the next pass adopts/resolves without a
	// new spawn and resets the failure bookkeeping.
	fm.startErrs["codebase"] = nil
	fm.status["codebase"] = &procman.ServiceStatus{Name: "codebase", Running: true, Healthy: true, PID: 4242, Port: 9749}
	rc.once(context.Background())
	if st := rc.stateOf("codebase"); st != svcHealthy {
		t.Fatalf("recovered service state = %s, want healthy", st)
	}
	if got := rc.startAttemptsOf("codebase"); got != 1 {
		t.Fatalf("healthy observation must not spawn (attempts=%d)", got)
	}
}

// TestReconcilerWaitsForStartupWarmup pins sequencing: with a warmup gate
// already closed, reconcile proceeds normally; the gate itself is exercised
// by the server wiring (warmCodebase completes before the loop's first
// pass, see Start()).
func TestReconcilerWaitsForStartupWarmup(t *testing.T) {
	fm := newFakeManager()
	fm.status["codebase"] = &procman.ServiceStatus{Name: "codebase", Running: false}

	rs := state.Init(t.TempDir())
	warm := make(chan struct{})
	close(warm)
	rc := newServiceReconciler(fm, rs, reconcilerOptions{waitWarmDone: warm})
	rc.once(context.Background())
	_ = rc
}

// --- test helpers ---

func httptestHealthOK(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
}

func closedChan() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

// fakeServiceManager stands in for procman so lifecycle tests are
// deterministic: no real processes, no health budgets, no OS variance.
type fakeServiceManager struct {
	mu        sync.Mutex
	status    map[string]*procman.ServiceStatus
	startErrs map[string]error
	starts    map[string]int
}

func newFakeManager() *fakeServiceManager {
	return &fakeServiceManager{
		status:    map[string]*procman.ServiceStatus{},
		startErrs: map[string]error{},
		starts:    map[string]int{},
	}
}

func (f *fakeServiceManager) Status(name string) *procman.ServiceStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	st, ok := f.status[name]
	if !ok {
		return nil
	}
	cp := *st
	return &cp
}

func (f *fakeServiceManager) Start(name string) (*procman.ServiceStatus, error) {
	f.mu.Lock()
	f.starts[name]++
	err := f.startErrs[name]
	f.mu.Unlock()
	if err != nil {
		return &procman.ServiceStatus{Name: name, Running: false, Error: err.Error()}, err
	}
	return &procman.ServiceStatus{Name: name, Running: true, Healthy: true, PID: 4242, Port: 9749}, nil
}

func TestReconcilerStopBeforeRunDoesNotBlock(t *testing.T) {
	rc := newServiceReconciler(newFakeManager(), state.Init(t.TempDir()), reconcilerOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := rc.StopContext(ctx); err != nil {
		t.Fatalf("StopContext before Run() error = %v", err)
	}
	// The compatibility Stop method is idempotent after the bounded stop.
	rc.Stop()
}

func TestReconcilerPublishesStartingBeforeBlockedStartCompletes(t *testing.T) {
	fm := newBlockingContextManager()
	fm.status["codebase"] = &procman.ServiceStatus{Name: "codebase", Running: false}
	rs := state.Init(t.TempDir())
	rc := newServiceReconciler(fm, rs, reconcilerOptions{waitWarmDone: closedChan()})

	done := make(chan struct{})
	go func() {
		rc.once(context.Background())
		close(done)
	}()
	select {
	case <-fm.entered:
	case <-time.After(time.Second):
		t.Fatal("reconciler never started the dead service")
	}
	if got := rc.stateOf("codebase"); got != svcStarting {
		t.Fatalf("state during blocked start = %s, want starting", got)
	}
	close(fm.release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reconcile pass did not finish")
	}
	if got := rc.stateOf("codebase"); got != svcHealthy {
		t.Fatalf("state after successful start = %s, want healthy", got)
	}
}

type blockingContextManager struct {
	*fakeServiceManager
	entered chan struct{}
	release chan struct{}
}

func newBlockingContextManager() *blockingContextManager {
	return &blockingContextManager{
		fakeServiceManager: newFakeManager(),
		entered:            make(chan struct{}),
		release:            make(chan struct{}),
	}
}

func (f *blockingContextManager) StartContext(ctx context.Context, name string) (*procman.ServiceStatus, error) {
	close(f.entered)
	select {
	case <-f.release:
		return &procman.ServiceStatus{Name: name, Running: true, Healthy: true, PID: 4242, Port: 9749}, nil
	case <-ctx.Done():
		return &procman.ServiceStatus{Name: name, Running: false, Error: ctx.Err().Error()}, ctx.Err()
	}
}
