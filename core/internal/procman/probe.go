package procman

import (
	"context"
	"sync"
	"time"
)

// probeCoalescer deduplicates concurrent health probes for the same URL and
// caches the last result (positive OR negative) for a short TTL. It is a small
// internal singleflight — no external dependency — so that N concurrent Status
// calls for one service produce a single GET, and a burst of Status calls
// right after that reuses the cached answer instead of re-hitting the service.
//
// Design goals it satisfies:
//   - one in-flight GET per URL; concurrent callers become followers
//   - a follower returns as soon as EITHER the leader's probe finishes OR the
//     follower's own ctx is done, whichever comes first
//   - the leader's probe runs with no state mutex held (Status snapshots under
//     mp.mu and calls probe() afterwards)
//   - once the leader finishes, its result is cached until now+ttl; the next
//     probe after the TTL issues a fresh GET
type probeCoalescer struct {
	ttl   time.Duration
	now   func() time.Time                                          // injectable clock for deterministic tests
	doGet func(ctx context.Context, url string) (bool, error)      // injectable probe for tests
	mu    sync.Mutex
	cache map[string]probeCacheEntry
	calls map[string]*probeCall
}

type probeCacheEntry struct {
	ok      bool
	expires time.Time
}

// probeCall is the shared state for one in-flight probe. done is closed by the
// leader when the result is ready; followers select on it against their ctx.
type probeCall struct {
	done chan struct{}
	ok   bool
}

func newProbeCoalescer(ttl time.Duration) *probeCoalescer {
	return &probeCoalescer{
		ttl:   ttl,
		now:   time.Now,
		doGet: probeHealthURLContext2s,
		cache: make(map[string]probeCacheEntry),
		calls: make(map[string]*probeCall),
	}
}

func probeHealthURLContext2s(ctx context.Context, url string) (bool, error) {
	return probeHealthURLContext(ctx, url, 2*time.Second)
}

// probe returns the health of url, coalescing concurrent callers and reusing a
// cached result within the TTL. A follower whose ctx is cancelled before the
// leader finishes returns false via its own ctx without waiting further.
func (p *probeCoalescer) probe(ctx context.Context, url string) bool {
	if ctx == nil {
		ctx = context.Background()
	}

	p.mu.Lock()
	// Fresh cached result?
	if entry, ok := p.cache[url]; ok && p.now().Before(entry.expires) {
		p.mu.Unlock()
		return entry.ok
	}
	// Join an in-flight probe as a follower.
	if call, ok := p.calls[url]; ok {
		p.mu.Unlock()
		select {
		case <-call.done:
			return call.ok
		case <-ctx.Done():
			return false
		}
	}
	// Become the leader for this URL.
	call := &probeCall{done: make(chan struct{})}
	p.calls[url] = call
	p.mu.Unlock()

	// Probe with NO lock held. Use a detached context so a follower cancelling
	// its own ctx cannot abort the shared request; the leader owns the GET and
	// its lifetime is bounded by the client's own 2s timeout.
	ok, _ := p.doGet(context.Background(), url)

	p.mu.Lock()
	call.ok = ok
	p.cache[url] = probeCacheEntry{ok: ok, expires: p.now().Add(p.ttl)}
	delete(p.calls, url)
	p.mu.Unlock()
	close(call.done)

	// The leader is not a follower: it returns its own fresh result and is not
	// subject to ctx cancellation racing against a completed probe. If the
	// leader's ctx was cancelled meanwhile, honour it.
	if err := ctx.Err(); err != nil {
		return false
	}
	return ok
}
