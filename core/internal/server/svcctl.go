package server

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/fvmoraes/dwyt/internal/health"
	"github.com/fvmoraes/dwyt/internal/log"
	"github.com/fvmoraes/dwyt/internal/procman"
	"github.com/fvmoraes/dwyt/internal/state"
)

// ServiceReconciler is the single decision owner for DWYT-managed services.
// ProcessManager remains the executor; every start/stop/restart request and
// periodic observation is serialized through the policy for that service.
const (
	svcUnknown  = "unknown"
	svcStopped  = "stopped"
	svcStarting = "starting"
	svcHealthy  = "healthy"
	svcDegraded = "degraded"
	svcFailed   = "failed"
)

const (
	desiredRunning = "running"
	desiredStopped = "stopped"

	ownershipUnknown = "unknown"
	ownershipManaged = "managed"
	ownershipAdopted = "adopted"
)

const (
	defaultReconcileInterval = 5 * time.Second
	defaultStartingGrace     = 15 * time.Second
	degradeAfterFailures     = 2
	maxStartAttempts         = 3
	// degradeCooldown is how long a policy stays degraded after exhausting its
	// bounded start budget. When it elapses the reconciler reopens exactly one
	// fresh bounded budget, so an unrecoverable service retries periodically
	// without ever becoming a restart storm.
	degradeCooldown = 60 * time.Second
)

var (
	ErrUnknownService = errors.New("unknown managed service")
	ErrAdoptedProcess = errors.New("adopted process is not owned by DWYT")
)

// ManagedService is intentionally small: policy and dynamic endpoints belong
// here; process creation remains in procman. ValidateIdentity must fail closed:
// a plain HTTP 200 is not sufficient evidence that a port belongs to DWYT.
type ManagedService struct {
	Name                 string
	AutoStart            bool
	Required             bool
	RequestedPort        int
	HealthURL            func() string
	ValidateIdentity     func(context.Context, string) (bool, error)
	PublishEffectivePort func(int)
}

// nextBackoff returns the wait after the Nth failed start. The sequence is
// bounded; after maxStartAttempts the policy becomes degraded until a real
// observation recovers it or an explicit request resets the budget.
func nextBackoff(attempt int) time.Duration {
	switch {
	case attempt <= 1:
		return time.Second
	case attempt == 2:
		return 3 * time.Second
	default:
		return 10 * time.Second
	}
}

func applyObservation(current string, running, healthy bool, failCount int, withinGrace bool) (string, int) {
	switch {
	case !running:
		return svcFailed, 0
	case healthy:
		return svcHealthy, 0
	case withinGrace:
		return svcStarting, failCount
	case current == svcHealthy && failCount+1 < degradeAfterFailures:
		return svcHealthy, failCount + 1
	default:
		return svcDegraded, failCount + 1
	}
}

// serviceManager is deliberately the legacy-minimum executor surface. Context
// aware start/stop/restart capabilities are detected below so focused fakes and
// older library users remain source-compatible.
type serviceManager interface {
	Status(name string) *procman.ServiceStatus
	Start(name string) (*procman.ServiceStatus, error)
}

type contextServiceManager interface {
	StartContext(context.Context, string) (*procman.ServiceStatus, error)
}

type contextServiceStopper interface {
	StopContext(context.Context, string) (*procman.ServiceStatus, error)
}

type serviceStopper interface {
	Stop(string) (*procman.ServiceStatus, error)
}

type contextServiceRestarter interface {
	RestartContext(context.Context, string) (*procman.ServiceStatus, error)
}

type serviceRestarter interface {
	Restart(string) (*procman.ServiceStatus, error)
}

type reconcilerOptions struct {
	// waitWarmDone is retained while startup callers are migrated. New wiring
	// starts the reconciler directly and leaves it nil.
	waitWarmDone <-chan struct{}
	// healthURLs is the compatibility test hook. Supplying one explicitly opts
	// that service into health-only identity for the test; production services
	// use ManagedService.ValidateIdentity.
	healthURLs map[string]string
	services   []ManagedService
	backoff    []time.Duration
	interval   time.Duration
	grace      time.Duration
	// cooldown overrides degradeCooldown for deterministic tests. Zero means
	// use the default.
	cooldown time.Duration
	// now is an injectable clock. Production leaves it nil and falls back to
	// time.Now, so tests can exercise the degrade→cooldown→reopen path without
	// sleeping.
	now func() time.Time
}

type svcPolicy struct {
	service ManagedService
	opMu    sync.Mutex

	desired       string
	ownership     string
	state         string
	failCount     int
	attempt       int
	nextRetry     time.Time
	lastChange    time.Time
	effectivePort int
	starts        int
	inFlight      bool
	// cooldownUntil is set when the bounded start budget is exhausted. The
	// reconciler refuses new starts until now >= cooldownUntil, then reopens a
	// single fresh budget.
	cooldownUntil time.Time
}

type ServiceReconciler struct {
	pm   serviceManager
	rs   *state.RuntimeState
	opts reconcilerOptions

	mu       sync.RWMutex
	policies map[string]*svcPolicy
	order    []string

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
	if opts.cooldown <= 0 {
		opts.cooldown = degradeCooldown
	}
	if opts.now == nil {
		opts.now = time.Now
	}
	services := opts.services
	if len(services) == 0 {
		services = []ManagedService{
			{Name: "codebase", AutoStart: true, RequestedPort: 9749, HealthURL: func() string { return "http://127.0.0.1:9749" + codebaseHealthPath }},
			{Name: "headroom", AutoStart: false, RequestedPort: 8787, HealthURL: func() string { return "http://127.0.0.1:8787/health" }},
		}
	}

	rc := &ServiceReconciler{
		pm: pm, rs: rs, opts: opts,
		policies: make(map[string]*svcPolicy, len(services)),
		stopCh:   make(chan struct{}), done: make(chan struct{}),
	}
	for _, configured := range services {
		service := configured
		if service.Name == "" {
			continue
		}
		if override, ok := opts.healthURLs[service.Name]; ok {
			overrideURL := override
			service.HealthURL = func() string { return overrideURL }
			service.ValidateIdentity = func(ctx context.Context, endpoint string) (bool, error) {
				return health.ProbeURLContext(ctx, endpoint), nil
			}
		}
		if service.RequestedPort == 0 {
			service.RequestedPort = portFromHealthURL(serviceURL(service))
		}

		// Persisted process facts (ownership, effective port, identity, PID,
		// health) describe a PREVIOUS daemon boot and must never be trusted as
		// if they were freshly validated. The only survivors are the operator's
		// durable intent (desired) and the durable port request. Every in-memory
		// policy therefore starts unknown/unowned/unhealthy with a zero effective
		// port; a real observation or adoption in the first reconcile pass is the
		// only thing that may promote it.
		desired := desiredStopped
		if service.AutoStart {
			desired = desiredRunning
		}
		if rs != nil {
			if persisted, ok := rs.GetProcess(service.Name); ok {
				if persisted.DesiredState == desiredRunning || persisted.DesiredState == desiredStopped {
					desired = persisted.DesiredState
				}
			}
			// Ensure the durable intent and port request exist, then immediately
			// invalidate the observation so a stale healthy/managed/adopted record
			// cannot leak into status before validation.
			rs.SetProcessDesired(service.Name, desired)
			if service.RequestedPort > 0 {
				rs.SetProcessLifecycle(state.ProcessInfo{
					Name: service.Name, RequestedPort: service.RequestedPort,
					DesiredState: desired,
				})
			}
			rs.InvalidateProcessObservation(service.Name)
		}
		rc.policies[service.Name] = &svcPolicy{
			service: service, desired: desired, ownership: ownershipUnknown,
			state: svcUnknown, effectivePort: 0,
		}
		rc.order = append(rc.order, service.Name)
	}
	return rc
}

func serviceURL(service ManagedService) string {
	if service.HealthURL == nil {
		return ""
	}
	return service.HealthURL()
}

func portFromHealthURL(raw string) int {
	parsed, err := url.Parse(raw)
	if err != nil {
		return 0
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		return 0
	}
	return port
}

func (rc *ServiceReconciler) Run() { rc.RunContext(context.Background()) }

func (rc *ServiceReconciler) RunContext(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	rc.runOnce.Do(func() {
		go func() {
			defer close(rc.done)
			// A stop requested before (or racing) Run must win: never run the
			// first reconciliation — and therefore never start a service — once
			// the reconciler has been asked to stop or the context is done.
			if rc.stopped() || ctx.Err() != nil {
				return
			}
			// Reconcile immediately; startup no longer needs an independent owner.
			rc.once(ctx)
			timer := time.NewTimer(rc.opts.interval)
			defer timer.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-rc.stopCh:
					return
				case <-timer.C:
					// Re-check the stop signal before each periodic pass so a stop
					// that arrives while the timer is pending cannot slip in a start.
					if rc.stopped() || ctx.Err() != nil {
						return
					}
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

func (rc *ServiceReconciler) Stop() { _ = rc.StopContext(context.Background()) }

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

// stopped reports whether StopContext has already signalled the reconciler to
// wind down. The periodic loop and once() both consult it so a stop request can
// never be overtaken by a fresh start.
func (rc *ServiceReconciler) stopped() bool {
	select {
	case <-rc.stopCh:
		return true
	default:
		return false
	}
}

func (rc *ServiceReconciler) policy(name string) (*svcPolicy, error) {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	policy := rc.policies[name]
	if policy == nil {
		return nil, fmt.Errorf("%w: %s", ErrUnknownService, name)
	}
	return policy, nil
}

func (rc *ServiceReconciler) stateOf(name string) string {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	if policy := rc.policies[name]; policy != nil {
		return policy.state
	}
	return svcUnknown
}

func (rc *ServiceReconciler) startAttemptsOf(name string) int {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	if policy := rc.policies[name]; policy != nil {
		return policy.starts
	}
	return 0
}

// StartService records durable intent before reconciling immediately. Multiple
// callers converge on policy.opMu and observe the first caller's result.
func (rc *ServiceReconciler) StartService(ctx context.Context, name string) (*procman.ServiceStatus, error) {
	return rc.requestAndReconcile(ctx, name, desiredRunning)
}

// StopService persists desired=stopped before touching the process. Adopted
// processes are deliberately left running because DWYT does not own them.
func (rc *ServiceReconciler) StopService(ctx context.Context, name string) (*procman.ServiceStatus, error) {
	return rc.requestAndReconcile(ctx, name, desiredStopped)
}

func (rc *ServiceReconciler) requestAndReconcile(ctx context.Context, name, desired string) (*procman.ServiceStatus, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	policy, err := rc.policy(name)
	if err != nil {
		return nil, err
	}
	policy.opMu.Lock()
	defer policy.opMu.Unlock()
	rc.setDesired(policy, desired)
	// An explicit operator/install request starts a fresh bounded retry budget.
	// Periodic reconciliation never takes this path, so a failing service still
	// cannot create an automatic restart storm.
	if desired == desiredRunning {
		rc.mu.Lock()
		policy.attempt = 0
		policy.nextRetry = time.Time{}
		policy.cooldownUntil = time.Time{}
		rc.mu.Unlock()
	}
	return rc.reconcilePolicy(ctx, policy)
}

// RestartService is serialized with periodic reconciliation and explicit
// start/stop requests. It refuses to terminate an adopted process.
func (rc *ServiceReconciler) RestartService(ctx context.Context, name string) (*procman.ServiceStatus, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	policy, err := rc.policy(name)
	if err != nil {
		return nil, err
	}
	policy.opMu.Lock()
	defer policy.opMu.Unlock()
	rc.setDesired(policy, desiredRunning)

	rc.mu.RLock()
	ownership := policy.ownership
	rc.mu.RUnlock()
	if ownership == ownershipAdopted {
		return rc.pm.Status(name), fmt.Errorf("restart %s: %w", name, ErrAdoptedProcess)
	}

	rc.mu.Lock()
	rc.transitionLocked(policy, svcStarting, rc.clock())
	policy.ownership = ownershipManaged
	rc.mu.Unlock()
	rc.publish(policy, rc.pm.Status(name))

	status, err := rc.restartProcess(ctx, name)
	rc.finishExplicitOperation(policy, status, err)
	return status, err
}

// StopManagedChildren terminates only the processes DWYT actually owns
// (ownership=managed). Adopted processes are left running because DWYT did not
// spawn them, and unknown-ownership policies are never force-killed. This is a
// shutdown-time drain, not an operator intent change: DesiredState is preserved
// so a service the operator wanted running is restored on the next boot instead
// of being silently latched to stopped. Per-service failures are aggregated
// with errors.Join so one stubborn child cannot mask the others.
func (rc *ServiceReconciler) StopManagedChildren(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	rc.mu.RLock()
	names := append([]string(nil), rc.order...)
	rc.mu.RUnlock()

	var errs []error
	for _, name := range names {
		policy, err := rc.policy(name)
		if err != nil {
			continue
		}
		policy.opMu.Lock()
		rc.mu.RLock()
		ownership := policy.ownership
		rc.mu.RUnlock()
		if ownership != ownershipManaged {
			policy.opMu.Unlock()
			continue
		}
		observed := rc.pm.Status(name)
		if observed == nil || !observed.Running {
			policy.opMu.Unlock()
			continue
		}
		_, err = rc.stopProcess(ctx, name)
		if err != nil {
			errs = append(errs, fmt.Errorf("stop managed child %s: %w", name, err))
		}
		policy.opMu.Unlock()
	}
	return errors.Join(errs...)
}

func (rc *ServiceReconciler) setDesired(policy *svcPolicy, desired string) {
	rc.mu.Lock()
	changed := policy.desired != desired
	policy.desired = desired
	if changed && desired == desiredRunning {
		policy.attempt = 0
		policy.nextRetry = time.Time{}
		policy.cooldownUntil = time.Time{}
	}
	rc.mu.Unlock()
	if rc.rs != nil {
		rc.rs.SetProcessDesired(policy.service.Name, desired)
	}
}

func (rc *ServiceReconciler) once(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	if rc.stopped() || ctx.Err() != nil {
		return
	}
	if rc.opts.waitWarmDone != nil {
		select {
		case <-rc.opts.waitWarmDone:
		case <-ctx.Done():
			return
		case <-rc.stopCh:
			return
		}
	}
	for _, name := range rc.order {
		if rc.stopped() || ctx.Err() != nil {
			return
		}
		rc.reconcileService(ctx, name)
	}
}

func (rc *ServiceReconciler) reconcileService(ctx context.Context, name string) {
	policy, err := rc.policy(name)
	if err != nil {
		return
	}
	policy.opMu.Lock()
	defer policy.opMu.Unlock()
	_, _ = rc.reconcilePolicy(ctx, policy)
}

func (rc *ServiceReconciler) reconcilePolicy(ctx context.Context, policy *svcPolicy) (*procman.ServiceStatus, error) {
	name := policy.service.Name
	observed := rc.pm.Status(name) // I/O is deliberately outside rc.mu.

	rc.mu.RLock()
	desired := policy.desired
	ownership := policy.ownership
	currentState := policy.state
	failCount := policy.failCount
	lastChange := policy.lastChange
	rc.mu.RUnlock()

	if desired == desiredStopped {
		if observed != nil && observed.Running && ownership != ownershipAdopted {
			if ownership == ownershipUnknown {
				ownership = ownershipManaged
			}
			stopped, err := rc.stopProcess(ctx, name)
			if err != nil {
				rc.mu.Lock()
				rc.transitionLocked(policy, svcFailed, rc.clock())
				policy.ownership = ownership
				rc.mu.Unlock()
				rc.publish(policy, stopped)
				return stopped, err
			}
			observed = stopped
		}
		rc.mu.Lock()
		rc.transitionLocked(policy, svcStopped, rc.clock())
		policy.failCount = 0
		policy.attempt = 0
		policy.nextRetry = time.Time{}
		policy.cooldownUntil = time.Time{}
		if ownership != ownershipUnknown {
			policy.ownership = ownership
		}
		rc.mu.Unlock()
		rc.publish(policy, observed)
		return observed, nil
	}

	if observed != nil && observed.Running {
		now := rc.clock()
		withinGrace := currentState == svcStarting && now.Sub(lastChange) < rc.opts.grace
		newState, newFailures := applyObservation(currentState, true, observed.Healthy, failCount, withinGrace)
		rc.mu.Lock()
		if policy.ownership == ownershipUnknown {
			policy.ownership = ownershipManaged
		}
		if observed.Port > 0 {
			policy.effectivePort = observed.Port
		}
		if newState == svcHealthy {
			policy.attempt = 0
			policy.nextRetry = time.Time{}
			policy.cooldownUntil = time.Time{}
		}
		rc.transitionLocked(policy, newState, now)
		policy.failCount = newFailures
		rc.mu.Unlock()
		rc.publish(policy, observed)
		return observed, nil
	}

	if adopted, status, err := rc.tryAdopt(ctx, policy); adopted || err != nil {
		return status, err
	}

	now := rc.clock()
	rc.mu.Lock()
	if policy.attempt >= maxStartAttempts {
		// The bounded start budget is exhausted. Stay degraded until the cooldown
		// elapses, then reopen exactly one fresh budget so recovery is periodic,
		// never a storm. cooldownUntil is armed once, on the transition.
		if policy.cooldownUntil.IsZero() {
			policy.cooldownUntil = now.Add(rc.cooldown())
		}
		if now.Before(policy.cooldownUntil) {
			rc.transitionLocked(policy, svcDegraded, now)
			policy.inFlight = false
			rc.mu.Unlock()
			rc.publish(policy, observed)
			return observed, nil
		}
		// Cooldown elapsed: reopen a single bounded budget.
		policy.attempt = 0
		policy.failCount = 0
		policy.nextRetry = time.Time{}
		policy.cooldownUntil = time.Time{}
	}
	if now.Before(policy.nextRetry) || policy.inFlight {
		rc.mu.Unlock()
		return observed, nil
	}
	policy.inFlight = true
	policy.starts++
	policy.attempt++
	rc.transitionLocked(policy, svcStarting, now)
	attempt := policy.attempt
	rc.mu.Unlock()
	rc.publish(policy, observed)

	startedAt := time.Now()
	status, err := rc.startProcess(ctx, name)
	duration := time.Since(startedAt)

	finishedAt := rc.clock()
	rc.mu.Lock()
	policy.inFlight = false
	if status != nil && status.Port > 0 {
		policy.effectivePort = status.Port
	}
	if status != nil && (status.Running || status.PID > 0) {
		policy.ownership = ownershipManaged
	}
	switch {
	case err != nil && status != nil && status.Running:
		rc.transitionLocked(policy, svcStarting, finishedAt)
		policy.failCount = 0
		policy.nextRetry = finishedAt.Add(rc.backoffFor(attempt))
	case err != nil:
		next := svcFailed
		if attempt >= maxStartAttempts {
			next = svcDegraded
		}
		rc.transitionLocked(policy, next, finishedAt)
		policy.failCount = 0
		policy.nextRetry = finishedAt.Add(rc.backoffFor(attempt))
	case status != nil && status.Healthy:
		rc.transitionLocked(policy, svcHealthy, finishedAt)
		policy.failCount = 0
		policy.attempt = 0
		policy.ownership = ownershipManaged
		policy.nextRetry = time.Time{}
		policy.cooldownUntil = time.Time{}
	default:
		rc.transitionLocked(policy, svcStarting, finishedAt)
		policy.failCount = 0
		policy.ownership = ownershipManaged
		policy.nextRetry = finishedAt.Add(rc.backoffFor(attempt))
	}
	finalState, finalAttempt := policy.state, policy.attempt
	rc.mu.Unlock()

	rc.publish(policy, status)
	log.Info("reconciler start attempt finished", log.Fields{
		"service": name, "duration_ms": duration.Milliseconds(),
		"state": finalState, "attempt": finalAttempt,
	})
	return status, err
}

func (rc *ServiceReconciler) tryAdopt(ctx context.Context, policy *svcPolicy) (bool, *procman.ServiceStatus, error) {
	service := policy.service
	endpoint := serviceURL(service)
	if endpoint == "" || service.ValidateIdentity == nil {
		return false, nil, nil
	}
	valid, err := service.ValidateIdentity(ctx, endpoint)
	if err != nil {
		return false, nil, err
	}
	if !valid {
		return false, nil, nil
	}
	port := portFromHealthURL(endpoint)
	if port == 0 {
		port = service.RequestedPort
	}
	status := &procman.ServiceStatus{
		Name: service.Name, Status: "online", State: svcHealthy,
		Running: true, Healthy: true, Port: port,
	}
	rc.mu.Lock()
	rc.transitionLocked(policy, svcHealthy, rc.clock())
	policy.ownership = ownershipAdopted
	policy.effectivePort = port
	policy.failCount = 0
	policy.attempt = 0
	policy.nextRetry = time.Time{}
	rc.mu.Unlock()
	rc.publish(policy, status)
	return true, status, nil
}

func (rc *ServiceReconciler) finishExplicitOperation(policy *svcPolicy, status *procman.ServiceStatus, err error) {
	now := rc.clock()
	rc.mu.Lock()
	if status != nil && status.Port > 0 {
		policy.effectivePort = status.Port
	}
	if err != nil {
		rc.transitionLocked(policy, svcFailed, now)
	} else if status != nil && status.Healthy {
		rc.transitionLocked(policy, svcHealthy, now)
		policy.ownership = ownershipManaged
		policy.attempt = 0
		policy.nextRetry = time.Time{}
	} else {
		rc.transitionLocked(policy, svcStarting, now)
		policy.ownership = ownershipManaged
	}
	rc.mu.Unlock()
	rc.publish(policy, status)
}

func (rc *ServiceReconciler) startProcess(ctx context.Context, name string) (*procman.ServiceStatus, error) {
	if manager, ok := rc.pm.(contextServiceManager); ok {
		return manager.StartContext(ctx, name)
	}
	return rc.pm.Start(name)
}

func (rc *ServiceReconciler) stopProcess(ctx context.Context, name string) (*procman.ServiceStatus, error) {
	if manager, ok := rc.pm.(contextServiceStopper); ok {
		return manager.StopContext(ctx, name)
	}
	if manager, ok := rc.pm.(serviceStopper); ok {
		return manager.Stop(name)
	}
	return rc.pm.Status(name), fmt.Errorf("stop %s: process executor does not support stop", name)
}

func (rc *ServiceReconciler) restartProcess(ctx context.Context, name string) (*procman.ServiceStatus, error) {
	if manager, ok := rc.pm.(contextServiceRestarter); ok {
		return manager.RestartContext(ctx, name)
	}
	if manager, ok := rc.pm.(serviceRestarter); ok {
		return manager.Restart(name)
	}
	if _, err := rc.stopProcess(ctx, name); err != nil {
		return nil, err
	}
	return rc.startProcess(ctx, name)
}

// clock returns the current time through the injectable hook so degrade,
// cooldown and backoff windows are deterministic in tests.
func (rc *ServiceReconciler) clock() time.Time {
	if rc.opts.now != nil {
		return rc.opts.now()
	}
	return time.Now()
}

// transitionLocked records a lifecycle transition exactly once. Callers must
// hold rc.mu and pass rc.clock() so LastTransitionAt is deterministic in tests
// and never advances while a service remains in the same state.
func (rc *ServiceReconciler) transitionLocked(policy *svcPolicy, next string, at time.Time) {
	if policy.state == next {
		return
	}
	policy.state = next
	policy.lastChange = at
}

func (rc *ServiceReconciler) cooldown() time.Duration {
	if rc.opts.cooldown > 0 {
		return rc.opts.cooldown
	}
	return degradeCooldown
}

func (rc *ServiceReconciler) backoffFor(attempt int) time.Duration {
	if len(rc.opts.backoff) == 0 {
		return nextBackoff(attempt)
	}
	index := attempt - 1
	if index < 0 {
		index = 0
	}
	if index >= len(rc.opts.backoff) {
		index = len(rc.opts.backoff) - 1
	}
	return rc.opts.backoff[index]
}

func (rc *ServiceReconciler) publish(policy *svcPolicy, status *procman.ServiceStatus) {
	rc.mu.Lock()
	// An effective port only exists once the process was actually observed (or
	// adopted) answering on it. The configured RequestedPort is a durable
	// request, never an observation: it must never be promoted into the
	// effective port here.
	if status != nil && status.Port > 0 {
		policy.effectivePort = status.Port
	}
	effectivePort := policy.effectivePort
	stateValue := policy.state
	desired := policy.desired
	ownership := policy.ownership
	requestedPort := policy.service.RequestedPort
	attempt := policy.attempt
	lastTransitionAt := policy.lastChange
	publisher := policy.service.PublishEffectivePort
	rc.mu.Unlock()

	// The effective-port callback is a real-observation signal. Fire it only
	// when a port was truly observed; a requested-but-not-yet-running service
	// must not publish its request as if it were serving traffic.
	if publisher != nil && effectivePort > 0 {
		publisher(effectivePort)
	}
	if rc.rs == nil {
		return
	}

	pid := 0
	errMessage := ""
	observedAt := time.Time{}
	if status != nil {
		pid = status.PID
		errMessage = status.Error
		observedAt = rc.clock()
	}
	if stateValue != svcHealthy && errMessage == "" {
		errMessage = "service state: " + stateValue
	}
	// Port/EffectivePort stay 0 until a real observation/adoption; only
	// RequestedPort carries the durable configuration. Health timestamps are
	// set only when ProcessManager supplied an observation, never merely because
	// a desired state was declared.
	next := state.ProcessInfo{
		Name:             policy.service.Name,
		PID:              pid,
		Port:             effectivePort,
		RequestedPort:    requestedPort,
		EffectivePort:    effectivePort,
		Healthy:          stateValue == svcHealthy,
		State:            stateValue,
		DesiredState:     desired,
		Ownership:        ownership,
		LastError:        errMessage,
		Attempt:          attempt,
		LastTransitionAt: lastTransitionAt,
	}
	if !observedAt.IsZero() {
		next.LastHealthAt = observedAt
		if status.Healthy {
			next.LastHealthyAt = observedAt
		}
	}
	rc.rs.SetProcessLifecycle(next)
}
