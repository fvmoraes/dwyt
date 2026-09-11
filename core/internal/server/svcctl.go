package server

import (
	"context"
	"sync"
	"time"

	"github.com/fvmoraes/dwyt/internal/health"
	"github.com/fvmoraes/dwyt/internal/log"
	"github.com/fvmoraes/dwyt/internal/procman"
	"github.com/fvmoraes/dwyt/internal/state"
)

// ServiceReconciler is the single watchdog for DWYT-managed services
// (Cross-Platform Startup Plan §10-§17, §43-§44, §59). procman stays the
// process executor; the reconciler owns the *decision* layer:
//
//	observe → adopt-or-recover → publish state
//
// It never restarts a process that is still running (that would make it a
// second lifecycle owner racing the API handlers); it only spawns when the
// process is gone, with a bounded backoff, after adopting an external
// healthy instance when one answers on the service port instead.

// Managed-service lifecycle states.
const (
	svcUnknown  = "unknown"
	svcStopped  = "stopped"
	svcStarting = "starting"
	svcHealthy  = "healthy"
	svcDegraded = "degraded"
	svcFailed   = "failed"
)

const (
	defaultReconcileInterval = 5 * time.Second
	defaultStartingGrace     = 15 * time.Second
	// Cross-Platform §43: a single failed probe must not flip a healthy
	// service; two consecutive failures degrade it.
	degradeAfterFailures = 2
)

// nextBackoff returns the recovery delay after the attempt-th failed start
// (Cross-Platform §44: bounded 1s → 3s → 10s, capped).
func nextBackoff(attempt int) time.Duration {
	switch {
	case attempt <= 1:
		return 1 * time.Second
	case attempt == 2:
		return 3 * time.Second
	default:
		return 10 * time.Second
	}
}

// applyObservation is the pure state-transition core (§16, §43, §17):
//
//	not running                → failed (bookkeeping reset; recovery is the
//	                             caller's decision, gated by backoff)
//	running + healthy          → healthy, failures reset
//	running + unhealthy, still → starting (grace: connection refused right
//	    within grace             after spawn is expected, not offline)
//	running + unhealthy, first → healthy with transient failure noted
//	    failure after healthy    (hysteresis)
//	running + unhealthy, more  → degraded
func applyObservation(state string, running, healthy bool, failCount int, withinGrace bool) (string, int) {
	switch {
	case !running:
		return svcFailed, 0
	case healthy:
		return svcHealthy, 0
	case withinGrace:
		return svcStarting, failCount
	case state == svcHealthy && failCount+1 < degradeAfterFailures:
		return svcHealthy, failCount + 1
	default:
		return svcDegraded, failCount + 1
	}
}

// serviceManager is the process-execution surface the reconciler needs.
// *procman.ProcessManager implements it; tests use a fake for determinism.
type serviceManager interface {
	Status(name string) *procman.ServiceStatus
	Start(name string) (*procman.ServiceStatus, error)
}

type contextServiceManager interface {
	StartContext(ctx context.Context, name string) (*procman.ServiceStatus, error)
}

type reconcilerOptions struct {
	// waitWarmDone, when set, gates the first reconcile pass on the
	// startup warmCodebase attempt finishing (no racing owners).
	waitWarmDone <-chan struct{}
	// healthURLs overrides per-service health URLs (tests).
	healthURLs map[string]string
	// backoff overrides the retry progression (tests).
	backoff []time.Duration
	// interval/grace override the loop cadence and starting grace (tests).
	interval time.Duration
	grace    time.Duration
}

type svcPolicy struct {
	name       string
	autoStart  bool
	healthURL  string
	state      string
	failCount  int
	attempt    int
	nextRetry  time.Time
	lastChange time.Time
	starts     int
}

type ServiceReconciler struct {
	pm       serviceManager
	rs       *state.RuntimeState
	opts     reconcilerOptions
	mu       sync.Mutex
	policies map[string]*svcPolicy
	inFlight map[string]bool
	stopCh   chan struct{}
	stopOnce sync.Once
	runOnce  sync.Once
	done     chan struct{}
}

func newServiceReconciler(pm serviceManager, rs *state.RuntimeState, opts reconcilerOptions) *ServiceReconciler {
	if opts.interval <= 0 {
		opts.interval = defaultReconcileInterval
	}
	if opts.grace <= 0 {
		opts.grace = defaultStartingGrace
	}
	rc := &ServiceReconciler{
		pm:       pm,
		rs:       rs,
		opts:     opts,
		policies: map[string]*svcPolicy{},
		inFlight: map[string]bool{},
		stopCh:   make(chan struct{}),
		done:     make(chan struct{}),
	}
	rc.policies["codebase"] = &svcPolicy{
		name:      "codebase",
		autoStart: true,
		healthURL: "http://127.0.0.1:9749/health",
		state:     svcUnknown,
	}
	rc.policies["headroom"] = &svcPolicy{
		name:      "headroom",
		autoStart: false, // startHeadroomIfNeeded owns headroom startup; the reconciler observes and publishes only
		healthURL: "http://127.0.0.1:8787/health",
		state:     svcUnknown,
	}
	for name, url := range opts.healthURLs {
		if p, ok := rc.policies[name]; ok {
			p.healthURL = url
		}
	}
	return rc
}

// Run launches the reconcile loop with a background context. It is kept for
// callers that do not own a lifecycle; DashboardServer uses RunContext.
func (rc *ServiceReconciler) Run() {
	rc.RunContext(context.Background())
}

// RunContext launches the reconcile loop once and propagates cancellation to
// an in-progress warmup gate. Repeated calls are harmless.
func (rc *ServiceReconciler) RunContext(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	rc.runOnce.Do(func() {
		go func() {
			defer close(rc.done)
			// time.NewTimer + Reset, not time.After in a loop (timer churn).
			timer := time.NewTimer(rc.opts.interval)
			defer timer.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-rc.stopCh:
					return
				case <-timer.C:
					rc.once(ctx)
					if ctx.Err() != nil {
						return
					}
					timer.Reset(rc.opts.interval)
				}
			}
		}()
	})
}

// Stop terminates the loop. Safe before Run and safe to call multiple times.
func (rc *ServiceReconciler) Stop() {
	_ = rc.StopContext(context.Background())
}

// StopContext bounds the wait for the reconcile loop. Starting the loop after
// closing stopCh makes Stop-before-Run safe: it exits immediately and closes
// done instead of waiting forever for a Run call that may never come.
func (rc *ServiceReconciler) StopContext(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	rc.stopOnce.Do(func() { close(rc.stopCh) })
	rc.Run()
	select {
	case <-rc.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// stateOf reports the current lifecycle state of a service (test and
// diagnostics accessor).
func (rc *ServiceReconciler) stateOf(name string) string {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if p, ok := rc.policies[name]; ok {
		return p.state
	}
	return svcUnknown
}

func (rc *ServiceReconciler) startAttemptsOf(name string) int {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return rc.policies[name].starts
}

// once runs a single reconcile pass over every managed service, in a
// stable order, sequentially — no per-service goroutines to own.
func (rc *ServiceReconciler) once(ctx context.Context) {
	if rc.opts.waitWarmDone != nil {
		select {
		case <-rc.opts.waitWarmDone:
		case <-ctx.Done():
			return
		case <-rc.stopCh:
			return
		}
	}
	for _, name := range []string{"codebase", "headroom"} {
		if ctx.Err() != nil {
			return
		}
		rc.reconcileService(ctx, name)
	}
}

func (rc *ServiceReconciler) reconcileService(ctx context.Context, name string) {
	rc.mu.Lock()
	p, ok := rc.policies[name]
	if !ok {
		rc.mu.Unlock()
		return
	}
	now := time.Now()

	st := rc.pm.Status(name)
	if st != nil && st.Running {
		withinGrace := p.state == svcStarting && now.Sub(p.lastChange) < rc.opts.grace
		newState, newFails := applyObservation(p.state, true, st.Healthy, p.failCount, withinGrace)
		if newState == svcHealthy {
			p.attempt = 0
			p.nextRetry = time.Time{}
		}
		if newState != p.state {
			p.lastChange = now
		}
		p.state, p.failCount = newState, newFails
		rc.publishLocked(p, st)
		rc.mu.Unlock()
		return
	}

	// Not running: adopt a healthy external instance instead of spawning a
	// duplicate (§59). The probe is the service-identity check §32 asks for.
	if health.ProbeURL(p.healthURL) {
		p.state, p.failCount, p.attempt = svcHealthy, 0, 0
		p.nextRetry = time.Time{}
		p.lastChange = now
		rc.publishLocked(p, &procman.ServiceStatus{Name: name, Running: true, Healthy: true})
		rc.mu.Unlock()
		return
	}

	if !p.autoStart {
		if p.state != svcStopped {
			p.state, p.lastChange = svcStopped, now
			rc.publishLocked(p, st)
		}
		rc.mu.Unlock()
		return
	}

	// Dead auto-start service: bounded recovery, gated by backoff and the
	// singleflight slot. The start runs synchronously on the reconcile loop:
	// procman.Start is idempotent, and a slow (budget-burning) attempt
	// delays the next pass instead of racing it.
	if now.Before(p.nextRetry) || rc.inFlight[name] {
		rc.mu.Unlock()
		return
	}
	rc.inFlight[name] = true
	p.starts++
	p.attempt++
	p.state = svcStarting
	p.lastChange = now
	rc.publishLocked(p, st)
	rc.mu.Unlock()

	started := time.Now()
	var st2 *procman.ServiceStatus
	var err error
	if manager, ok := rc.pm.(contextServiceManager); ok {
		st2, err = manager.StartContext(ctx, name)
	} else {
		st2, err = rc.pm.Start(name)
	}
	durationMs := time.Since(started).Milliseconds()

	rc.mu.Lock()
	rc.inFlight[name] = false
	p.nextRetry = time.Now().Add(rc.backoffFor(p.attempt))
	switch {
	case err != nil && st2 != nil && st2.Running:
		p.state, p.failCount = svcStarting, 0 // grace begins
	case err != nil:
		p.state, p.failCount = svcFailed, 0
	case st2 != nil && st2.Healthy:
		p.state, p.failCount, p.attempt = svcHealthy, 0, 0
		p.nextRetry = time.Time{}
	default:
		p.state, p.failCount = svcStarting, 0
	}
	rc.publishLocked(p, st2)
	rc.mu.Unlock()

	log.Info("reconciler start attempt finished", log.Fields{
		"service": name, "duration_ms": durationMs, "state": p.state, "attempt": p.attempt,
	})
}

func (rc *ServiceReconciler) backoffFor(attempt int) time.Duration {
	if len(rc.opts.backoff) == 0 {
		return nextBackoff(attempt)
	}
	idx := attempt - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(rc.opts.backoff) {
		idx = len(rc.opts.backoff) - 1
	}
	return rc.opts.backoff[idx]
}

// publishLocked writes the reconciled state to the runtime state: the new
// lifecycle state plus the legacy healthy/error fields existing consumers
// already read (compat). Caller must hold rc.mu.
func (rc *ServiceReconciler) publishLocked(p *svcPolicy, st *procman.ServiceStatus) {
	errMsg := ""
	if st != nil {
		errMsg = st.Error
	}
	if p.state != svcHealthy && errMsg == "" {
		errMsg = "service state: " + p.state
	}
	rc.rs.SetProcessState(p.name, p.state, errMsg)
	rc.rs.SetProcessHealthy(p.name, p.state == svcHealthy, errMsg)
	if st != nil && st.Running && st.PID > 0 {
		rc.rs.RegisterProcess(p.name, st.PID, st.Port)
	}
}
