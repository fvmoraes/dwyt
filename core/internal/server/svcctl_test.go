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

// startCount is a lock-safe accessor so tests never race the reconcile
// goroutine's writes to the starts map (important under -race).
func (f *fakeServiceManager) startCount(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.starts[name]
}

func TestReconcilerStopBeforeRunDoesNotBlock(t *testing.T) {
	fm := newFakeManager()
	// A dead auto-start service would normally be started on the first pass.
	fm.status["codebase"] = &procman.ServiceStatus{Name: "codebase", Running: false}
	rc := newServiceReconciler(fm, state.Init(t.TempDir()), reconcilerOptions{
		services: []ManagedService{{Name: "codebase", AutoStart: true, RequestedPort: 9749}},
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := rc.StopContext(ctx); err != nil {
		t.Fatalf("StopContext before Run() error = %v", err)
	}
	// The compatibility Stop method is idempotent after the bounded stop.
	rc.Stop()

	// A stop requested before Run must win outright: the first reconciliation
	// never runs, so no service may ever be started. Run() is now a no-op
	// because the stop signal is already latched; give the (immediately
	// returning) loop goroutine a moment and assert zero starts.
	rc.Run()
	select {
	case <-rc.done:
	case <-time.After(time.Second):
		t.Fatal("Run() goroutine did not observe the pre-Run stop signal")
	}
	if got := fm.startCount("codebase"); got != 0 {
		t.Fatalf("reconciler started a service after a pre-Run stop: starts=%d, want 0", got)
	}
	if got := rc.startAttemptsOf("codebase"); got != 0 {
		t.Fatalf("reconciler recorded start attempts after a pre-Run stop: attempts=%d, want 0", got)
	}
}

// TestReconcilerDegradedReopensBudgetAfterCooldown pins the anti-storm
// recovery contract (§44): after the bounded start budget is exhausted the
// policy stays degraded for a cooldown window during which NO further starts
// happen, and only once the cooldown elapses does the reconciler reopen a
// single fresh bounded budget. An injectable clock makes this deterministic
// with no sleeping.
func TestReconcilerDegradedReopensBudgetAfterCooldown(t *testing.T) {
	fm := newLifecycleFakeManager()
	close(fm.startRelease)
	fm.startErr = context.DeadlineExceeded // every start fails

	now := time.Unix(0, 0)
	clock := func() time.Time { return now }

	rs := state.Init(t.TempDir())
	rc := newServiceReconciler(fm, rs, reconcilerOptions{
		services: []ManagedService{{Name: "codebase", AutoStart: true, RequestedPort: 9749}},
		backoff:  []time.Duration{0, 0, 0}, // no backoff so the budget is spent in-window
		cooldown: time.Minute,
		now:      clock,
	})

	// Spend the entire bounded budget. Even with extra passes, starts are
	// capped at maxStartAttempts and the policy lands in degraded.
	for range 8 {
		rc.once(context.Background())
	}
	if got := fm.startCount("codebase"); got != maxStartAttempts {
		t.Fatalf("start attempts before cooldown = %d, want bounded %d", got, maxStartAttempts)
	}
	if got := rc.stateOf("codebase"); got != svcDegraded {
		t.Fatalf("state after exhausting budget = %q, want %q", got, svcDegraded)
	}

	// Advance time but stay inside the cooldown: still no new starts.
	now = now.Add(59 * time.Second)
	for range 5 {
		rc.once(context.Background())
	}
	if got := fm.startCount("codebase"); got != maxStartAttempts {
		t.Fatalf("reconciler started during cooldown: starts=%d, want still %d", got, maxStartAttempts)
	}
	if got := rc.stateOf("codebase"); got != svcDegraded {
		t.Fatalf("state during cooldown = %q, want %q", got, svcDegraded)
	}

	// Cross the cooldown boundary: exactly one fresh budget reopens, so the
	// 4th start happens (bounded — not a storm).
	now = now.Add(2 * time.Second)
	rc.once(context.Background())
	if got := fm.startCount("codebase"); got != maxStartAttempts+1 {
		t.Fatalf("post-cooldown start count = %d, want %d (one reopened attempt)", got, maxStartAttempts+1)
	}
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

// F3 lifecycle-owner contract tests. These exercise the public decision
// surface that every startup path and HTTP handler must use; procman remains
// an executor behind this boundary.
func TestReconcilerConcurrentStartRequestsCoalesce(t *testing.T) {
	fm := newLifecycleFakeManager()
	fm.startPort = 9750
	rs := state.Init(t.TempDir())
	rc := newServiceReconciler(fm, rs, reconcilerOptions{
		services: []ManagedService{{
			Name:      "codebase",
			AutoStart: true,
			HealthURL: func() string { return "" },
		}},
	})

	const callers = 3
	errCh := make(chan error, callers)
	for range callers {
		go func() {
			_, err := rc.StartService(context.Background(), "codebase")
			errCh <- err
		}()
	}

	select {
	case <-fm.startEntered:
	case <-time.After(time.Second):
		t.Fatal("no start request reached the process executor")
	}
	close(fm.startRelease)
	for range callers {
		if err := <-errCh; err != nil {
			t.Fatalf("StartService error = %v", err)
		}
	}
	if got := fm.startCount("codebase"); got != 1 {
		t.Fatalf("concurrent lifecycle requests spawned %d processes, want 1", got)
	}
}

func TestReconcilerStopPersistsDesiredStopped(t *testing.T) {
	fm := newLifecycleFakeManager()
	fm.status["codebase"] = &procman.ServiceStatus{
		Name: "codebase", Running: true, Healthy: true, PID: 4242, Port: 9749,
	}
	close(fm.startRelease)
	rs := state.Init(t.TempDir())
	rc := newServiceReconciler(fm, rs, reconcilerOptions{
		services: []ManagedService{{Name: "codebase", AutoStart: true}},
	})

	if _, err := rc.StopService(context.Background(), "codebase"); err != nil {
		t.Fatalf("StopService error = %v", err)
	}
	persisted, ok := rs.GetProcess("codebase")
	if !ok {
		t.Fatal("desired state was not persisted")
	}
	if persisted.DesiredState != desiredStopped {
		t.Fatalf("desired state = %q, want %q", persisted.DesiredState, desiredStopped)
	}
	if got := fm.stopCount("codebase"); got != 1 {
		t.Fatalf("underlying stop calls = %d, want 1", got)
	}

	rc.once(context.Background())
	if got := fm.startCount("codebase"); got != 0 {
		t.Fatalf("reconcile restarted an explicitly stopped service: starts=%d", got)
	}
}

func TestReconcilerRejectsHealthyPortWithWrongIdentity(t *testing.T) {
	srv := httptestHealthOK(t)
	defer srv.Close()

	fm := newLifecycleFakeManager()
	close(fm.startRelease)
	fm.startErr = context.DeadlineExceeded
	rs := state.Init(t.TempDir())
	rc := newServiceReconciler(fm, rs, reconcilerOptions{
		services: []ManagedService{{
			Name:      "codebase",
			AutoStart: true,
			HealthURL: func() string { return srv.URL + "/health" },
			ValidateIdentity: func(context.Context, string) (bool, error) {
				return false, nil
			},
		}},
		backoff: []time.Duration{time.Hour},
	})

	rc.once(context.Background())
	if got := fm.startCount("codebase"); got != 1 {
		t.Fatalf("wrong-identity endpoint was adopted instead of starting managed service: starts=%d", got)
	}
	if proc, ok := rs.GetProcess("codebase"); ok && proc.Ownership == ownershipAdopted {
		t.Fatal("wrong-identity endpoint was published as adopted")
	}
}

func TestReconcilerAdoptsExistingHealthyServiceWithIdentity(t *testing.T) {
	srv := httptestHealthOK(t)
	defer srv.Close()

	fm := newLifecycleFakeManager()
	close(fm.startRelease)
	rs := state.Init(t.TempDir())
	publishedPort := 0
	rc := newServiceReconciler(fm, rs, reconcilerOptions{
		services: []ManagedService{{
			Name:          "codebase",
			AutoStart:     true,
			RequestedPort: 9749,
			HealthURL:     func() string { return srv.URL + "/health" },
			ValidateIdentity: func(context.Context, string) (bool, error) {
				return true, nil
			},
			PublishEffectivePort: func(port int) { publishedPort = port },
		}},
	})

	rc.once(context.Background())
	if got := fm.startCount("codebase"); got != 0 {
		t.Fatalf("validated service should be adopted, starts=%d", got)
	}
	proc, ok := rs.GetProcess("codebase")
	if !ok {
		t.Fatal("adopted service was not published")
	}
	if proc.Ownership != ownershipAdopted {
		t.Fatalf("ownership = %q, want %q", proc.Ownership, ownershipAdopted)
	}
	if proc.EffectivePort == 0 || publishedPort != proc.EffectivePort {
		t.Fatalf("effective port was not propagated: state=%d callback=%d", proc.EffectivePort, publishedPort)
	}
}

func TestReconcilerBoundedRetries(t *testing.T) {
	fm := newLifecycleFakeManager()
	close(fm.startRelease)
	fm.startErr = context.DeadlineExceeded
	rs := state.Init(t.TempDir())
	rc := newServiceReconciler(fm, rs, reconcilerOptions{
		services: []ManagedService{{Name: "codebase", AutoStart: true}},
		backoff:  []time.Duration{0, 0, 0},
	})

	for range 8 {
		rc.once(context.Background())
	}
	if got := fm.startCount("codebase"); got != maxStartAttempts {
		t.Fatalf("start attempts = %d, want bounded %d", got, maxStartAttempts)
	}
	if got := rc.stateOf("codebase"); got != svcDegraded {
		t.Fatalf("state after retry budget = %q, want %q", got, svcDegraded)
	}
}

func TestReconcilerPublishesEffectivePort(t *testing.T) {
	fm := newLifecycleFakeManager()
	fm.startPort = 9750
	close(fm.startRelease)
	rs := state.Init(t.TempDir())
	publishedPort := 0
	rc := newServiceReconciler(fm, rs, reconcilerOptions{
		services: []ManagedService{{
			Name:                 "codebase",
			AutoStart:            true,
			RequestedPort:        9749,
			PublishEffectivePort: func(port int) { publishedPort = port },
		}},
	})

	status, err := rc.StartService(context.Background(), "codebase")
	if err != nil {
		t.Fatalf("StartService error = %v", err)
	}
	if status.Port != 9750 || publishedPort != 9750 {
		t.Fatalf("effective port status=%d callback=%d, want 9750", status.Port, publishedPort)
	}
	proc, ok := rs.GetProcess("codebase")
	if !ok || proc.RequestedPort != 9749 || proc.EffectivePort != 9750 || proc.Port != 9750 {
		t.Fatalf("runtime port state = %+v, present=%v", proc, ok)
	}
}

// lifecycleHealthMode provides state-driven, OS-independent process health
// transitions for every lifecycle matrix. No fake mode relies on wall-clock
// sleeps: tests advance it by observing Status or releasing an explicit gate.
type lifecycleHealthMode uint8

const (
	lifecycleHealthHealthy lifecycleHealthMode = iota
	lifecycleHealthDelayed
	lifecycleHealthNever
	lifecycleHealthFlap
)

// lifecycleFakeManager is the shared deterministic failure-injection executor
// for Core, Status, and duplicate-runtime coverage. It models successful and
// failed spawns, delayed/never/flapping health, early process exit, effective
// port changes, cancellation races, and a deliberately delayed stop.
type lifecycleFakeManager struct {
	mu                     sync.Mutex
	status                 map[string]*procman.ServiceStatus
	starts                 map[string]int
	stops                  map[string]int
	restarts               map[string]int
	statusChecks           map[string]int
	startPort              int
	startErr               error
	healthMode             lifecycleHealthMode
	healthDelayChecks      int
	exitAfterStatusChecks  int
	portAfterStatusChecks  int
	changedPort            int
	startPublishesOnCancel bool
	blockStop              bool

	startEntered   chan struct{}
	startCompleted chan struct{}
	startCanceled  chan struct{}
	startRelease   chan struct{}
	stopEntered    chan struct{}
	stopRelease    chan struct{}
	restartEntered chan struct{}
	enteredOnce    sync.Once
	completedOnce  sync.Once
	canceledOnce   sync.Once
	stopOnce       sync.Once
	restartOnce    sync.Once
}

func newLifecycleFakeManager() *lifecycleFakeManager {
	return &lifecycleFakeManager{
		status:         map[string]*procman.ServiceStatus{},
		starts:         map[string]int{},
		stops:          map[string]int{},
		restarts:       map[string]int{},
		statusChecks:   map[string]int{},
		startPort:      9749,
		startEntered:   make(chan struct{}),
		startCompleted: make(chan struct{}),
		startCanceled:  make(chan struct{}),
		startRelease:   make(chan struct{}),
		stopEntered:    make(chan struct{}),
		stopRelease:    make(chan struct{}),
		restartEntered: make(chan struct{}),
	}
}

func (f *lifecycleFakeManager) Status(name string) *procman.ServiceStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.observedStatusLocked(name)
}

func (f *lifecycleFakeManager) observedStatusLocked(name string) *procman.ServiceStatus {
	st := f.status[name]
	if st == nil {
		return &procman.ServiceStatus{Name: name}
	}
	cp := *st
	if !cp.Running {
		return &cp
	}

	f.statusChecks[name]++
	checks := f.statusChecks[name]
	if f.exitAfterStatusChecks > 0 && checks >= f.exitAfterStatusChecks {
		cp.Running = false
		cp.Healthy = false
		f.status[name] = &cp
		return &cp
	}
	if f.portAfterStatusChecks > 0 && checks >= f.portAfterStatusChecks && f.changedPort > 0 {
		cp.Port = f.changedPort
		f.status[name] = &cp
	}
	switch f.healthMode {
	case lifecycleHealthDelayed:
		cp.Healthy = checks > f.healthDelayChecks
	case lifecycleHealthNever:
		cp.Healthy = false
	case lifecycleHealthFlap:
		cp.Healthy = checks%2 == 0
	}
	return &cp
}

func (f *lifecycleFakeManager) Start(name string) (*procman.ServiceStatus, error) {
	return f.StartContext(context.Background(), name)
}

func (f *lifecycleFakeManager) StartContext(ctx context.Context, name string) (*procman.ServiceStatus, error) {
	f.mu.Lock()
	f.starts[name]++
	f.mu.Unlock()
	f.enteredOnce.Do(func() { close(f.startEntered) })
	defer f.completedOnce.Do(func() { close(f.startCompleted) })

	select {
	case <-f.startRelease:
	case <-ctx.Done():
		if f.startPublishesOnCancel {
			f.mu.Lock()
			st := f.startedStatusLocked(name)
			f.status[name] = st
			cp := *st
			f.mu.Unlock()
			f.canceledOnce.Do(func() { close(f.startCanceled) })
			return &cp, ctx.Err()
		}
		return &procman.ServiceStatus{Name: name, Error: ctx.Err().Error()}, ctx.Err()
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.startErr != nil {
		return &procman.ServiceStatus{Name: name, Error: f.startErr.Error(), Port: f.startPort}, f.startErr
	}
	st := f.startedStatusLocked(name)
	f.status[name] = st
	cp := *st
	return &cp, nil
}

func (f *lifecycleFakeManager) startedStatusLocked(name string) *procman.ServiceStatus {
	return &procman.ServiceStatus{
		Name: name, Running: true, Healthy: f.healthMode == lifecycleHealthHealthy,
		PID: 4242, Port: f.startPort,
	}
}

func (f *lifecycleFakeManager) StopContext(ctx context.Context, name string) (*procman.ServiceStatus, error) {
	f.mu.Lock()
	f.stops[name]++
	f.mu.Unlock()
	f.stopOnce.Do(func() { close(f.stopEntered) })
	if f.blockStop {
		select {
		case <-f.stopRelease:
		case <-ctx.Done():
			return &procman.ServiceStatus{Name: name, Error: ctx.Err().Error()}, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	st := &procman.ServiceStatus{Name: name, Running: false, Healthy: false, Port: f.startPort}
	f.status[name] = st
	cp := *st
	return &cp, nil
}

func (f *lifecycleFakeManager) RestartContext(ctx context.Context, name string) (*procman.ServiceStatus, error) {
	f.restartOnce.Do(func() { close(f.restartEntered) })
	f.mu.Lock()
	f.restarts[name]++
	f.mu.Unlock()
	if _, err := f.StopContext(ctx, name); err != nil {
		return nil, err
	}
	return f.StartContext(ctx, name)
}

func (f *lifecycleFakeManager) startCount(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.starts[name]
}

func (f *lifecycleFakeManager) stopCount(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stops[name]
}

func (f *lifecycleFakeManager) restartCount(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.restarts[name]
}

func (f *lifecycleFakeManager) statusSnapshot(name string) *procman.ServiceStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	st := f.status[name]
	if st == nil {
		return &procman.ServiceStatus{Name: name}
	}
	cp := *st
	return &cp
}

func TestLifecycleFakeFailureInjectionModes(t *testing.T) {
	start := func(t *testing.T, fake *lifecycleFakeManager) {
		t.Helper()
		close(fake.startRelease)
		if _, err := fake.StartContext(context.Background(), "codebase"); err != nil {
			t.Fatalf("StartContext() error = %v", err)
		}
	}

	t.Run("delayed never and flapping health", func(t *testing.T) {
		delayed := newLifecycleFakeManager()
		delayed.healthMode = lifecycleHealthDelayed
		delayed.healthDelayChecks = 1
		start(t, delayed)
		if delayed.Status("codebase").Healthy {
			t.Fatal("delayed health became ready before its explicit observation budget")
		}
		if !delayed.Status("codebase").Healthy {
			t.Fatal("delayed health did not become ready after its observation budget")
		}

		never := newLifecycleFakeManager()
		never.healthMode = lifecycleHealthNever
		start(t, never)
		if never.Status("codebase").Healthy || never.Status("codebase").Healthy {
			t.Fatal("never-healthy service reported healthy")
		}

		flap := newLifecycleFakeManager()
		flap.healthMode = lifecycleHealthFlap
		start(t, flap)
		first, second := flap.Status("codebase").Healthy, flap.Status("codebase").Healthy
		if first == second {
			t.Fatal("flapping health did not alternate across observations")
		}
	})

	t.Run("process exit and mutable port", func(t *testing.T) {
		exited := newLifecycleFakeManager()
		exited.exitAfterStatusChecks = 1
		start(t, exited)
		if got := exited.Status("codebase"); got.Running {
			t.Fatalf("process exit injection still reported running: %+v", got)
		}

		moved := newLifecycleFakeManager()
		moved.portAfterStatusChecks = 1
		moved.changedPort = 9750
		start(t, moved)
		if got := moved.Status("codebase").Port; got != 9750 {
			t.Fatalf("mutable port = %d, want 9750", got)
		}
	})

	t.Run("delayed stop", func(t *testing.T) {
		fake := newLifecycleFakeManager()
		fake.blockStop = true
		start(t, fake)
		done := make(chan error, 1)
		go func() {
			_, err := fake.StopContext(context.Background(), "codebase")
			done <- err
		}()
		select {
		case <-fake.stopEntered:
		case <-time.After(time.Second):
			t.Fatal("delayed stop did not enter")
		}
		select {
		case err := <-done:
			t.Fatalf("delayed stop completed before release: %v", err)
		default:
		}
		close(fake.stopRelease)
		if err := <-done; err != nil {
			t.Fatalf("delayed stop error = %v", err)
		}
		if got := fake.statusSnapshot("codebase"); got.Running {
			t.Fatalf("stopped fake still reported running: %+v", got)
		}
	})
}

func TestDuplicateRuntimeMatrix(t *testing.T) {
	t.Run("IDE first DWYT adopts without spawn", func(t *testing.T) {
		srv := httptestHealthOK(t)
		defer srv.Close()

		fake := newLifecycleFakeManager()
		close(fake.startRelease)
		rs := state.Init(t.TempDir())
		rc := newServiceReconciler(fake, rs, reconcilerOptions{services: []ManagedService{{
			Name: "codebase", AutoStart: true, RequestedPort: 9749,
			HealthURL:        func() string { return srv.URL + "/health" },
			ValidateIdentity: func(context.Context, string) (bool, error) { return true, nil },
		}}})
		if _, err := rc.StartService(context.Background(), "codebase"); err != nil {
			t.Fatalf("StartService() error = %v", err)
		}
		if got := fake.startCount("codebase"); got != 0 {
			t.Fatalf("IDE-first service was spawned %d times, want 0", got)
		}
		if proc, ok := rs.GetProcess("codebase"); !ok || proc.Ownership != ownershipAdopted {
			t.Fatalf("IDE-first ownership = %+v, want adopted", proc)
		}
	})

	t.Run("DWYT first stays single managed runtime", func(t *testing.T) {
		fake := newLifecycleFakeManager()
		close(fake.startRelease)
		rs := state.Init(t.TempDir())
		rc := newServiceReconciler(fake, rs, reconcilerOptions{services: []ManagedService{{Name: "codebase", AutoStart: true}}})
		if _, err := rc.StartService(context.Background(), "codebase"); err != nil {
			t.Fatalf("StartService() error = %v", err)
		}
		rc.once(context.Background())
		if got := fake.startCount("codebase"); got != 1 {
			t.Fatalf("DWYT-first runtime spawned %d times, want 1", got)
		}
		if proc, ok := rs.GetProcess("codebase"); !ok || proc.Ownership != ownershipManaged {
			t.Fatalf("DWYT-first ownership = %+v, want managed", proc)
		}
	})

	t.Run("concurrent starts coalesce", func(t *testing.T) {
		fake := newLifecycleFakeManager()
		rs := state.Init(t.TempDir())
		rc := newServiceReconciler(fake, rs, reconcilerOptions{services: []ManagedService{{Name: "codebase", AutoStart: true}}})
		results := make(chan error, 2)
		for range 2 {
			go func() {
				_, err := rc.StartService(context.Background(), "codebase")
				results <- err
			}()
		}
		select {
		case <-fake.startEntered:
		case <-time.After(time.Second):
			t.Fatal("concurrent starts did not reach the fake")
		}
		close(fake.startRelease)
		for range 2 {
			if err := <-results; err != nil {
				t.Fatalf("StartService() error = %v", err)
			}
		}
		if got := fake.startCount("codebase"); got != 1 {
			t.Fatalf("concurrent starts spawned %d processes, want 1", got)
		}
	})

	t.Run("restart waits for a blocked start", func(t *testing.T) {
		fake := newLifecycleFakeManager()
		rs := state.Init(t.TempDir())
		rc := newServiceReconciler(fake, rs, reconcilerOptions{services: []ManagedService{{Name: "codebase", AutoStart: true}}})
		startDone := make(chan error, 1)
		go func() {
			_, err := rc.StartService(context.Background(), "codebase")
			startDone <- err
		}()
		select {
		case <-fake.startEntered:
		case <-time.After(time.Second):
			t.Fatal("initial start did not reach the fake")
		}

		restartRequested := make(chan struct{})
		restartDone := make(chan error, 1)
		go func() {
			close(restartRequested)
			_, err := rc.RestartService(context.Background(), "codebase")
			restartDone <- err
		}()
		<-restartRequested
		if got := fake.restartCount("codebase"); got != 0 {
			t.Fatalf("restart crossed the lifecycle lock before start completed: restarts=%d", got)
		}

		close(fake.startRelease)
		if err := <-startDone; err != nil {
			t.Fatalf("initial StartService() error = %v", err)
		}
		if err := <-restartDone; err != nil {
			t.Fatalf("RestartService() error = %v", err)
		}
		if got := fake.startCount("codebase"); got != 2 {
			t.Fatalf("start count after serialized restart = %d, want 2", got)
		}
		if got := fake.restartCount("codebase"); got != 1 {
			t.Fatalf("restart count = %d, want 1", got)
		}
		if got := fake.stopCount("codebase"); got != 1 {
			t.Fatalf("stop count during restart = %d, want 1", got)
		}
	})

	t.Run("shutdown drains start that publishes during cancellation", func(t *testing.T) {
		fake := newLifecycleFakeManager()
		fake.startPublishesOnCancel = true
		ds := New(freeTCPPort(t), t.TempDir(), t.TempDir(), "test")
		ds.startupTasksOverride = []startupTask{}
		ds.SvcCtl = newServiceReconciler(fake, ds.RuntimeState, reconcilerOptions{
			services: []ManagedService{{Name: "codebase", AutoStart: true}},
		})
		serverDone := make(chan error, 1)
		go func() { serverDone <- ds.Start() }()
		if err := waitForDashboard(t, ds.Port, 5*time.Second); err != nil {
			t.Fatal(err)
		}
		select {
		case <-fake.startEntered:
		case <-time.After(time.Second):
			t.Fatal("dashboard reconciler did not start the managed service")
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := ds.Shutdown(ctx); err != nil {
			t.Fatalf("Shutdown() error = %v", err)
		}
		select {
		case <-fake.startCanceled:
		case <-time.After(time.Second):
			t.Fatal("blocked start did not observe dashboard cancellation")
		}
		select {
		case err := <-serverDone:
			if err != nil {
				t.Fatalf("Start() after shutdown error = %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("dashboard Start did not return after shutdown")
		}
		if got := fake.stopCount("codebase"); got != 1 {
			t.Fatalf("shutdown left a start-race child undrained: stops=%d, want 1", got)
		}
		if got := fake.statusSnapshot("codebase"); got.Running {
			t.Fatalf("shutdown left an orphaned child running: %+v", got)
		}
	})
}

// TestStopManagedChildrenStopsOnlyOwnedProcesses pins the shutdown-drain
// contract: only ownership=managed processes are stopped; an adopted process
// (running, but not spawned by DWYT) is never touched; and stopping a managed
// child is a drain, not an intent change — DesiredState=running survives so the
// service is restored on the next boot.
func TestStopManagedChildrenStopsOnlyOwnedProcesses(t *testing.T) {
	adoptedSrv := httptestHealthOK(t)
	defer adoptedSrv.Close()

	fm := newLifecycleFakeManager()
	close(fm.startRelease)
	// The managed service reports running+healthy so observing it marks it
	// managed (DWYT owns the executor entry).
	fm.status["managed"] = &procman.ServiceStatus{Name: "managed", Running: true, Healthy: true, PID: 4242, Port: 9749}
	// The adopted service is NOT reported running by the executor, but its port
	// answers with a valid identity — so the reconciler adopts it (ownership
	// adopted, PID owned by another process) without ever spawning it.
	fm.status["adopted"] = &procman.ServiceStatus{Name: "adopted", Running: false}

	rs := state.Init(t.TempDir())
	rc := newServiceReconciler(fm, rs, reconcilerOptions{
		services: []ManagedService{
			{Name: "managed", AutoStart: true, RequestedPort: 9749},
			{
				Name: "adopted", AutoStart: true, RequestedPort: 8787,
				HealthURL:        func() string { return adoptedSrv.URL + "/health" },
				ValidateIdentity: func(context.Context, string) (bool, error) { return true, nil },
			},
		},
	})

	// First reconcile: "managed" is observed running (ownership→managed);
	// "adopted" is validated and adopted (ownership→adopted).
	rc.once(context.Background())

	if proc, ok := rs.GetProcess("managed"); !ok || proc.Ownership != ownershipManaged {
		t.Fatalf("managed service ownership = %+v, want %q", proc, ownershipManaged)
	}
	if proc, ok := rs.GetProcess("adopted"); !ok || proc.Ownership != ownershipAdopted {
		t.Fatalf("adopted service ownership = %+v, want %q", proc, ownershipAdopted)
	}

	if err := rc.StopManagedChildren(context.Background()); err != nil {
		t.Fatalf("StopManagedChildren error = %v", err)
	}

	if got := fm.stopCount("managed"); got != 1 {
		t.Fatalf("managed running child stops = %d, want exactly 1", got)
	}
	if got := fm.stopCount("adopted"); got != 0 {
		t.Fatalf("adopted running process stops = %d, want 0", got)
	}

	// The managed service's operator intent must survive the drain so it is
	// restored on the next boot rather than latched to stopped.
	proc, ok := rs.GetProcess("managed")
	if !ok {
		t.Fatal("managed process disappeared after drain")
	}
	if proc.DesiredState != desiredRunning {
		t.Fatalf("managed DesiredState = %q, want preserved %q", proc.DesiredState, desiredRunning)
	}
}

// TestShutdownDrainsManagedChildrenViaLazyController proves DashboardServer
// Shutdown stops managed children even when the controller was created lazily
// (assigned to ds.SvcCtl by a handler, never Run by the lifecycle) and that the
// drain is not a double stop: one managed child → exactly one stop.
func TestShutdownDrainsManagedChildrenViaLazyController(t *testing.T) {
	fm := newLifecycleFakeManager()
	close(fm.startRelease)
	fm.status["codebase"] = &procman.ServiceStatus{Name: "codebase", Running: true, Healthy: true, PID: 4242, Port: 9749}

	rs := state.Init(t.TempDir())
	controller := newServiceReconciler(fm, rs, reconcilerOptions{
		services: []ManagedService{{Name: "codebase", AutoStart: true, RequestedPort: 9749}},
	})
	// Observe once so the running executor entry is owned as managed.
	controller.once(context.Background())
	if proc, ok := rs.GetProcess("codebase"); !ok || proc.Ownership != ownershipManaged {
		t.Fatalf("precondition: codebase not managed: %+v", proc)
	}

	ds := &DashboardServer{RuntimeState: rs}
	// Simulate a lazily-created controller that the lifecycle never Run: the
	// field is set but svcCtlStarted stays false. lifecycleStarted must be true
	// so Shutdown performs the full drain instead of the pre-bind latch.
	ds.SvcCtl = controller
	ds.lifecycleStarted = true
	ds.svcCtlStarted = false

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := ds.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown error = %v", err)
	}
	if got := fm.stopCount("codebase"); got != 1 {
		t.Fatalf("managed child stops during shutdown = %d, want exactly 1", got)
	}

	// Idempotent: a second Shutdown must not stop the child again.
	if err := ds.Shutdown(ctx); err != nil {
		t.Fatalf("second Shutdown error = %v", err)
	}
	if got := fm.stopCount("codebase"); got != 1 {
		t.Fatalf("second Shutdown re-stopped the child: stops=%d, want still 1", got)
	}
}

func TestReconcilerPersistedObservationStartsUnknown(t *testing.T) {
	home := t.TempDir()
	persisted := state.Init(home)
	persisted.SetProcessLifecycle(state.ProcessInfo{
		Name: "codebase", PID: 999999, Port: 9750,
		RequestedPort: 9749, EffectivePort: 9750,
		Healthy: true, State: svcHealthy, DesiredState: desiredStopped,
		Ownership: ownershipManaged, Identity: "stale-identity",
	})
	reloaded := state.Init(home)

	manager := newLifecycleFakeManager()
	close(manager.startRelease)
	_ = newServiceReconciler(manager, reloaded, reconcilerOptions{
		services: []ManagedService{{Name: "codebase", AutoStart: true, RequestedPort: 9749}},
	})

	process, ok := reloaded.GetProcess("codebase")
	if !ok {
		t.Fatal("persisted desired state disappeared")
	}
	if process.Healthy || process.State != svcUnknown || process.PID != 0 {
		t.Fatalf("persisted observation was trusted before validation: %+v", process)
	}
	if process.DesiredState != desiredStopped {
		t.Fatalf("desired state = %q, want preserved %q", process.DesiredState, desiredStopped)
	}
}
