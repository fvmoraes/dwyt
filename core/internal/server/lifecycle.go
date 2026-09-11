package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/fvmoraes/dwyt/internal/kiropow"
	"github.com/fvmoraes/dwyt/internal/log"
)

// acceptReadyListener closes ready immediately before the HTTP server's first
// Accept call. Waiting on ready is stronger than merely creating the socket:
// optional services cannot begin until Serve has entered its accept loop.
type acceptReadyListener struct {
	net.Listener
	ready chan struct{}
	once  sync.Once
}

func (l *acceptReadyListener) Accept() (net.Conn, error) {
	l.once.Do(func() { close(l.ready) })
	return l.Listener.Accept()
}

// serveDashboard owns the Core listener and root lifecycle. It binds and puts
// http.Server into its accept loop before starting any optional reconciliation,
// warmup, migration or client-integration work.
func (ds *DashboardServer) serveDashboard(handler http.Handler) error {
	ds.lifecycleMu.Lock()
	if ds.shutdownRequested {
		ds.lifecycleMu.Unlock()
		return nil
	}
	if ds.lifecycleStarted {
		ds.lifecycleMu.Unlock()
		return fmt.Errorf("dashboard server already started")
	}

	addr := fmt.Sprintf("127.0.0.1:%d", ds.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		ds.lifecycleMu.Unlock()
		return fmt.Errorf("bind dashboard %s: %w", addr, err)
	}
	if tcpAddr, ok := listener.Addr().(*net.TCPAddr); ok {
		ds.Port = tcpAddr.Port
	}

	rootCtx, cancel := context.WithCancel(context.Background())
	readyListener := &acceptReadyListener{Listener: listener, ready: make(chan struct{})}
	httpServer := &http.Server{
		Handler: handler,
		BaseContext: func(net.Listener) context.Context {
			return rootCtx
		},
	}
	httpServer.RegisterOnShutdown(cancel)

	ds.lifecycleCtx = rootCtx
	ds.lifecycleCancel = cancel
	ds.httpServer = httpServer
	ds.lifecycleStarted = true

	serveErr := make(chan error, 1)
	go func() { serveErr <- httpServer.Serve(readyListener) }()

	select {
	case <-readyListener.ready:
		// Serve is now waiting in Accept. It is safe to launch optional work.
	case err := <-serveErr:
		cancel()
		ds.lifecycleStarted = false
		ds.lifecycleMu.Unlock()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve dashboard: %w", err)
	}

	startedAt := ds.startupStarted
	if startedAt.IsZero() {
		startedAt = time.Now()
		ds.startupStarted = startedAt
	}
	coreDuration := time.Since(startedAt)
	log.Info("core startup bound", log.Fields{
		"event": "core_startup_duration", "duration_ms": coreDuration.Milliseconds(), "port": ds.Port,
	})
	fmt.Printf("   Dashboard → http://localhost:%d\n", ds.Port)

	// Every Add happens while lifecycleMu is held and before Shutdown can
	// observe lifecycleStarted, preventing Add/Wait races.
	ds.startBackgroundReconciliation(rootCtx)
	ds.goLifecycleLocked(func() { ds.broadcastLoop(rootCtx) })
	ds.goLifecycleLocked(func() { ds.runCodebaseLifecycle(rootCtx) })
	ds.goLifecycleLocked(func() { ds.runHeadroomLifecycle(rootCtx) })
	if ds.hasSetupConfig && (contains(ds.setupConfig.Ias, "kiro") || contains(ds.setupConfig.Clients, "kiro")) {
		ds.goLifecycleLocked(func() { ds.reconcileKiroPower(rootCtx) })
	}
	lifecycleDone := make(chan struct{})
	ds.lifecycleDone = lifecycleDone
	go func() {
		ds.lifecycleWG.Wait()
		close(lifecycleDone)
	}()
	ds.lifecycleMu.Unlock()

	err = <-serveErr
	if errors.Is(err, http.ErrServerClosed) {
		return ds.completedShutdownResult()
	}

	// An unexpected Serve failure must not leave optional loops running.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	shutdownErr := ds.Shutdown(shutdownCtx)
	return errors.Join(fmt.Errorf("serve dashboard: %w", err), shutdownErr)
}

// goLifecycleLocked starts a root-context child that Shutdown must await.
// Caller must hold lifecycleMu so all WaitGroup additions precede Wait.
func (ds *DashboardServer) goLifecycleLocked(run func()) {
	ds.lifecycleWG.Add(1)
	go func() {
		defer ds.lifecycleWG.Done()
		run()
	}()
}

// runCodebaseLifecycle performs the legacy warmup after bind, then hands
// ownership to the reconciler. The lock closes the cancellation/start race:
// Shutdown either observes a started reconciler or cancellation wins first.
func (ds *DashboardServer) runCodebaseLifecycle(ctx context.Context) {
	warmCodebaseAttempt(ctx, ds.ProcMan, "http://127.0.0.1:9749/health")
	if ctx.Err() != nil || ds.SvcCtl == nil {
		return
	}

	ds.lifecycleMu.Lock()
	defer ds.lifecycleMu.Unlock()
	if ctx.Err() != nil || ds.lifecycleStopping {
		return
	}
	ds.SvcCtl.RunContext(ctx)
	ds.svcCtlStarted = true
	log.Info("service ready", log.Fields{
		"event": "service_ready_duration", "service": "codebase",
		"duration_ms": time.Since(ds.startupStarted).Milliseconds(),
	})
}

func (ds *DashboardServer) runHeadroomLifecycle(ctx context.Context) {
	if err := ds.startHeadroomIfNeeded(ctx); err != nil {
		if ctx.Err() != nil {
			return
		}
		ds.RuntimeState.SetToolError("startup_headroom_start", err.Error())
		log.Warn("headroom startup failed", log.Fields{"error": err.Error()})
		return
	}
	ds.RuntimeState.SetToolError("startup_headroom_start", "")
}

func (ds *DashboardServer) reconcileKiroPower(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	if _, err := kiropow.EnsurePowerContext(ctx, ds.DwytHome, ds.DwytBin, ds.DefaultProject); err != nil {
		ds.RuntimeState.SetToolError("startup_kiro_power", err.Error())
		log.Warn("kiro power ensure failed", log.Fields{"error": err.Error()})
		return
	}
	ds.RuntimeState.SetToolError("startup_kiro_power", "")
}

// Shutdown is idempotent and bounded by ctx. It latches a pre-bind request,
// broadcasts cancellation once, then drains HTTP, startup tasks, the
// reconciler, Housekeeper and tracked lifecycle loops. Concurrent callers wait
// on one permanent result rather than creating independent waiter goroutines.
func (ds *DashboardServer) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	ds.lifecycleMu.Lock()
	if ds.shutdownDone == nil {
		ds.shutdownDone = make(chan struct{})
	}
	if ds.shutdownStarted {
		done := ds.shutdownDone
		ds.lifecycleMu.Unlock()
		return ds.waitShutdownResult(ctx, done)
	}
	ds.shutdownStarted = true
	ds.shutdownRequested = true
	ds.lifecycleStopping = true
	if !ds.lifecycleStarted {
		close(ds.shutdownDone)
		ds.lifecycleMu.Unlock()
		return nil
	}

	cancel := ds.lifecycleCancel
	startupCancel := ds.startupCancel
	httpServer := ds.httpServer
	startupDone := ds.startupDone
	lifecycleDone := ds.lifecycleDone
	svcCtl := ds.SvcCtl
	svcCtlStarted := ds.svcCtlStarted
	ds.lifecycleMu.Unlock()

	if cancel != nil {
		cancel()
	}
	if startupCancel != nil {
		startupCancel()
	}

	var shutdownErrs []error
	if httpServer != nil {
		if err := httpServer.Shutdown(ctx); err != nil {
			shutdownErrs = append(shutdownErrs, fmt.Errorf("shutdown dashboard HTTP server: %w", err))
			if closeErr := httpServer.Close(); closeErr != nil {
				shutdownErrs = append(shutdownErrs, fmt.Errorf("close dashboard HTTP server: %w", closeErr))
			}
		}
	}
	if err := waitDone(ctx, startupDone, "startup tasks"); err != nil {
		shutdownErrs = append(shutdownErrs, err)
	}
	if svcCtlStarted && svcCtl != nil {
		if err := svcCtl.StopContext(ctx); err != nil {
			shutdownErrs = append(shutdownErrs, fmt.Errorf("stop service reconciler: %w", err))
		}
	}
	if ds.Housekeeper != nil {
		if err := ds.Housekeeper.StopContext(ctx); err != nil {
			shutdownErrs = append(shutdownErrs, fmt.Errorf("stop housekeeper: %w", err))
		}
	}
	if err := waitDone(ctx, lifecycleDone, "lifecycle tasks"); err != nil {
		shutdownErrs = append(shutdownErrs, err)
	}

	shutdownErr := errors.Join(shutdownErrs...)
	ds.lifecycleMu.Lock()
	ds.shutdownErr = shutdownErr
	close(ds.shutdownDone)
	ds.lifecycleMu.Unlock()
	return shutdownErr
}

func (ds *DashboardServer) waitShutdownResult(ctx context.Context, done <-chan struct{}) error {
	if err := waitDone(ctx, done, "dashboard shutdown"); err != nil {
		return err
	}
	ds.lifecycleMu.Lock()
	defer ds.lifecycleMu.Unlock()
	return ds.shutdownErr
}

// completedShutdownResult is used by Start after Serve reports ErrServerClosed.
// It keeps the daemon process alive until the initiating Shutdown has completed
// its full drain, which is essential for the HTTP shutdown path on Windows.
func (ds *DashboardServer) completedShutdownResult() error {
	ds.lifecycleMu.Lock()
	stopping := ds.shutdownStarted
	done := ds.shutdownDone
	ds.lifecycleMu.Unlock()
	if !stopping || done == nil {
		return nil
	}
	<-done
	ds.lifecycleMu.Lock()
	defer ds.lifecycleMu.Unlock()
	return ds.shutdownErr
}

func waitDone(ctx context.Context, done <-chan struct{}, name string) error {
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for %s: %w", name, ctx.Err())
	}
}
