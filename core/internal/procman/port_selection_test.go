package procman

import (
	"errors"
	"net"
	"strconv"
	"testing"

	"github.com/fvmoraes/dwyt/internal/health"
)

func TestProcessManagerPublishesRequestedAndEffectivePort(t *testing.T) {
	// Deterministically pin the topology FindFreePortE must resolve: the base
	// port is held occupied for the whole test, while base+1 is proven
	// bindable and then released so the selector can claim exactly it. This
	// removes the flake where an unrelated process could grab base+1 between
	// reservation and Start.
	base, baseHolder := reserveOccupiedBaseWithFreeNext(t)
	defer func() { _ = baseHolder.Close() }()

	pm := New(t.TempDir())
	bin, args := longRunningCmd()
	pm.Register("test", bin, "", base, args...)

	// Requirement (1): a Status taken right after Register — before Start —
	// reports the requested port with a zero effective port.
	if pre := pm.Status("test"); pre.RequestedPort != base || pre.EffectivePort != 0 || pre.Port != 0 {
		t.Fatalf("status after Register must report requested=%d effective=0, got %+v", base, pre)
	}

	status, err := pm.Start("test")
	if err != nil {
		t.Fatalf("Start error = %v", err)
	}
	t.Cleanup(func() { _, _ = pm.Stop("test") })

	if status.RequestedPort != base {
		t.Fatalf("requested port = %d, want %d", status.RequestedPort, base)
	}
	if status.EffectivePort != base+1 || status.Port != base+1 {
		t.Fatalf("effective port = %d (compat=%d), want %d", status.EffectivePort, status.Port, base+1)
	}
	if again := pm.Status("test"); again.RequestedPort != base || again.EffectivePort != base+1 {
		t.Fatalf("status lost port distinction: %+v", again)
	}
}

// reserveOccupiedBaseWithFreeNext returns a base port kept occupied by the
// returned listener, having proven base+1 is free (by binding and releasing
// it) so FindFreePortE(base) is forced to select base+1 deterministically.
func reserveOccupiedBaseWithFreeNext(t *testing.T) (int, net.Listener) {
	t.Helper()
	for attempt := 0; attempt < 200; attempt++ {
		seed, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		base := seed.Addr().(*net.TCPAddr).Port
		_ = seed.Close()
		if base+1 >= 65535 {
			continue
		}
		// Hold the base port occupied for the whole test.
		baseHolder, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(base)))
		if err != nil {
			continue
		}
		// Prove base+1 is currently bindable, then release it so the selector
		// can take it. Nothing in this package races for it after release.
		next, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(base+1)))
		if err != nil {
			_ = baseHolder.Close()
			continue
		}
		_ = next.Close()
		return base, baseHolder
	}
	t.Fatal("could not reserve an occupied base port with a free successor")
	return 0, nil
}

func TestProcessManagerAllCandidatePortsOccupiedReturnsConflict(t *testing.T) {
	listeners, base := reserveContiguousPorts(t, 5)
	defer closeListeners(listeners)

	pm := New(t.TempDir())
	bin, args := longRunningCmd()
	pm.Register("test", bin, "", base, args...)
	status, err := pm.Start("test")
	if !errors.Is(err, health.ErrPortConflict) {
		t.Fatalf("Start error = %v, want ErrPortConflict", err)
	}
	if status == nil || status.Running || status.RequestedPort != base || status.EffectivePort != 0 {
		t.Fatalf("conflict status = %+v", status)
	}
}

func reserveContiguousPorts(t *testing.T, count int) ([]net.Listener, int) {
	t.Helper()
	for attempt := 0; attempt < 100; attempt++ {
		seed, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		base := seed.Addr().(*net.TCPAddr).Port
		_ = seed.Close()
		if base+count >= 65535 {
			continue
		}
		listeners := make([]net.Listener, 0, count)
		for offset := 0; offset < count; offset++ {
			listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(base+offset)))
			if err != nil {
				closeListeners(listeners)
				listeners = nil
				break
			}
			listeners = append(listeners, listener)
		}
		if len(listeners) == count {
			return listeners, base
		}
	}
	t.Fatal("could not reserve a contiguous TCP port range")
	return nil, 0
}

func closeListeners(listeners []net.Listener) {
	for _, listener := range listeners {
		_ = listener.Close()
	}
}
