package procman

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestCoalescer builds a coalescer with an injected clock and probe so the
// tests are fully deterministic (no real HTTP, no wall-clock sleeps). The
// returned control lets a test gate when the leader's probe completes and read
// how many probes were actually issued.
func newTestCoalescer(ttl time.Duration) (*probeCoalescer, *coalescerControl) {
	ctrl := &coalescerControl{result: true}
	p := &probeCoalescer{
		ttl:   ttl,
		now:   ctrl.nowFunc(),
		doGet: ctrl.probeFunc(),
		cache: make(map[string]probeCacheEntry),
		calls: make(map[string]*probeCall),
	}
	return p, ctrl
}

type coalescerControl struct {
	mu       sync.Mutex
	nowValue time.Time
	release  chan struct{} // when non-nil, doGet blocks until it is closed
	entered  chan struct{} // closed by doGet once the leader is inside
	calls    atomic.Int64
	result   bool
}

func (c *coalescerControl) nowFunc() func() time.Time {
	if c.nowValue.IsZero() {
		c.nowValue = time.Unix(1_700_000_000, 0)
	}
	return func() time.Time {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.nowValue
	}
}

func (c *coalescerControl) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nowValue = c.nowValue.Add(d)
}

func (c *coalescerControl) probeFunc() func(context.Context, string) (bool, error) {
	return func(ctx context.Context, url string) (bool, error) {
		c.calls.Add(1)
		c.mu.Lock()
		entered := c.entered
		release := c.release
		c.mu.Unlock()
		if entered != nil {
			close(entered)
		}
		if release != nil {
			<-release
		}
		return c.result, nil
	}
}

// TestProbeCoalescerSingleGetForConcurrentCallers proves that N concurrent
// callers for the same URL trigger exactly one GET; the leader runs the probe
// while the followers wait, and all observe the same result.
func TestProbeCoalescerSingleGetForConcurrentCallers(t *testing.T) {
	p, ctrl := newTestCoalescer(time.Second)
	release := make(chan struct{})
	entered := make(chan struct{})
	ctrl.mu.Lock()
	ctrl.release = release
	ctrl.entered = entered
	ctrl.mu.Unlock()

	const followers = 8
	results := make(chan bool, followers)
	var wg sync.WaitGroup
	for i := 0; i < followers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- p.probe(context.Background(), "http://svc/health")
		}()
	}

	// The leader is now inside the (blocked) probe. Followers are parked on the
	// shared call. Releasing lets the single GET complete for everyone.
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("probe never entered")
	}
	close(release)
	wg.Wait()
	close(results)

	for r := range results {
		if !r {
			t.Fatal("all coalesced callers must observe the leader's healthy result")
		}
	}
	if got := ctrl.calls.Load(); got != 1 {
		t.Fatalf("issued %d GETs for %d concurrent callers, want exactly 1", got, followers)
	}
}

// TestProbeCoalescerCancelledFollowerReturnsViaOwnCtx proves a follower whose
// ctx is cancelled while the leader is still probing returns immediately (via
// its own ctx) without waiting for the leader, and does not issue a GET.
func TestProbeCoalescerCancelledFollowerReturnsViaOwnCtx(t *testing.T) {
	p, ctrl := newTestCoalescer(time.Second)
	release := make(chan struct{})
	entered := make(chan struct{})
	ctrl.mu.Lock()
	ctrl.release = release
	ctrl.entered = entered
	ctrl.mu.Unlock()

	// Leader parked inside the probe.
	leaderDone := make(chan bool, 1)
	go func() { leaderDone <- p.probe(context.Background(), "http://svc/health") }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("leader probe never entered")
	}

	// Follower joins, then is cancelled before the leader finishes.
	ctx, cancel := context.WithCancel(context.Background())
	followerDone := make(chan bool, 1)
	go func() { followerDone <- p.probe(ctx, "http://svc/health") }()
	cancel()

	select {
	case r := <-followerDone:
		if r {
			t.Fatal("cancelled follower must return false via its own ctx")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled follower did not return before the leader finished")
	}

	// Only one GET was ever issued (the leader's); the follower did not add one.
	if got := ctrl.calls.Load(); got != 1 {
		t.Fatalf("cancelled follower must not issue a GET, calls = %d", got)
	}
	close(release)
	<-leaderDone
}

// TestProbeCoalescerCachesUntilTTLThenRefetches proves the result is cached
// (positive and negative) for the TTL, then a fresh GET is issued once the TTL
// elapses. The clock is injected, so no real sleeps are needed.
func TestProbeCoalescerCachesUntilTTLThenRefetches(t *testing.T) {
	p, ctrl := newTestCoalescer(time.Second)

	// First probe issues a GET and caches the positive result.
	if !p.probe(context.Background(), "http://svc/health") {
		t.Fatal("first probe should be healthy")
	}
	if got := ctrl.calls.Load(); got != 1 {
		t.Fatalf("first probe calls = %d, want 1", got)
	}

	// Within the TTL the cache answers with no new GET.
	ctrl.advance(999 * time.Millisecond)
	if !p.probe(context.Background(), "http://svc/health") {
		t.Fatal("cached probe should still be healthy")
	}
	if got := ctrl.calls.Load(); got != 1 {
		t.Fatalf("cache hit issued a GET, calls = %d, want 1", got)
	}

	// A negative result is cached the same way.
	ctrl.result = false
	ctrl.advance(2 * time.Millisecond) // now past the 1s TTL
	if p.probe(context.Background(), "http://svc/health") {
		t.Fatal("probe after TTL should reflect the new unhealthy result")
	}
	if got := ctrl.calls.Load(); got != 2 {
		t.Fatalf("probe after TTL should issue a fresh GET, calls = %d, want 2", got)
	}
	// Negative result is cached too: no third GET within its TTL.
	ctrl.advance(500 * time.Millisecond)
	if p.probe(context.Background(), "http://svc/health") {
		t.Fatal("negative cache should still report unhealthy")
	}
	if got := ctrl.calls.Load(); got != 2 {
		t.Fatalf("negative result must be cached, calls = %d, want 2", got)
	}
}

// TestStatusDoesNotHoldStateMutexDuringProbe proves that mp.mu can be acquired
// while the HTTP probe backing a Status call is blocked in the server handler.
// If Status held mp.mu across the probe, the lock acquisition below would
// block until the server responded.
func TestStatusDoesNotHoldStateMutexDuringProbe(t *testing.T) {
	probeEntered := make(chan struct{})
	releaseProbe := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(probeEntered)
		<-releaseProbe
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	pm := New(t.TempDir())
	mp := &ManagedProcess{
		Name:      "svc",
		Bin:       os.Args[0],
		PID:       4242,
		StartedAt: time.Now(),
		cmd:       &exec.Cmd{},
		done:      make(chan struct{}),
	}
	pm.processes[mp.Name] = mp

	// Exercise the exact "snapshot under mp.mu, then probe lock-free" path:
	// take the snapshot under the lock, release it, and run the probe against
	// the blocking test server through the coalescer.
	statusDone := make(chan *ServiceStatus, 1)
	go func() {
		snap := pm.snapshot(mp) // acquires and releases mp.mu
		healthy := pm.prober.probe(context.Background(), server.URL)
		statusDone <- &ServiceStatus{Name: snap.name, Healthy: healthy}
	}()

	// Wait until the probe is confirmed in-flight (blocked in the handler).
	select {
	case <-probeEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("probe never reached the server")
	}

	// The state mutex must be immediately acquirable even though the probe is
	// still blocked in the HTTP handler.
	acquired := make(chan struct{})
	go func() {
		mp.mu.Lock()
		mp.mu.Unlock()
		close(acquired)
	}()
	select {
	case <-acquired:
	case <-time.After(2 * time.Second):
		t.Fatal("mp.mu is held during the HTTP probe: Status must not lock across the probe")
	}

	close(releaseProbe)
	select {
	case s := <-statusDone:
		if !s.Healthy {
			t.Fatalf("probe should report healthy, got %+v", s)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Status did not complete after the probe was released")
	}
}
