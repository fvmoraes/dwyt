package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fvmoraes/dwyt/internal/procman"
	"github.com/fvmoraes/dwyt/internal/state"
)

func assertPublishedTransition(t *testing.T, rs *state.RuntimeState, wantState string, wantAt time.Time) {
	t.Helper()
	info, ok := rs.GetProcess("codebase")
	if !ok {
		t.Fatal("codebase lifecycle was not published")
	}
	if info.State != wantState || !info.LastTransitionAt.Equal(wantAt) {
		t.Fatalf("published lifecycle = %+v, want state=%q transition=%s", info, wantState, wantAt)
	}
}

func TestReconcilerPublishesStoppedFailedDegradedAndRecoveryTimestamps(t *testing.T) {
	fm := newLifecycleFakeManager()
	close(fm.startRelease)
	fm.status["codebase"] = &procman.ServiceStatus{Name: "codebase", Running: true, Healthy: true, PID: 4242, Port: 9749}

	now := time.Date(2026, time.September, 11, 10, 0, 0, 0, time.UTC)
	rs := state.Init(t.TempDir())
	rc := newServiceReconciler(fm, rs, reconcilerOptions{
		services: []ManagedService{{Name: "codebase", AutoStart: true, RequestedPort: 9749}},
		backoff:  []time.Duration{0, 0, 0},
		cooldown: time.Minute,
		now:      func() time.Time { return now },
	})

	if _, err := rc.StopService(context.Background(), "codebase"); err != nil {
		t.Fatalf("StopService error = %v", err)
	}
	assertPublishedTransition(t, rs, svcStopped, now)

	now = now.Add(time.Minute)
	fm.startErr = errors.New("launch failed")
	if _, err := rc.StartService(context.Background(), "codebase"); err == nil {
		t.Fatal("failing start unexpectedly succeeded")
	}
	assertPublishedTransition(t, rs, svcFailed, now)

	now = now.Add(time.Minute)
	rc.once(context.Background())
	assertPublishedTransition(t, rs, svcFailed, now)
	now = now.Add(time.Minute)
	rc.once(context.Background())
	assertPublishedTransition(t, rs, svcDegraded, now)

	// One pass arms the cooldown while preserving the degraded transition.
	rc.once(context.Background())
	assertPublishedTransition(t, rs, svcDegraded, now)

	now = now.Add(time.Minute + time.Second)
	fm.startErr = nil
	rc.once(context.Background())
	assertPublishedTransition(t, rs, svcHealthy, now)
}

func TestFinishExplicitOperationUpdatesTransitionTimestampOnce(t *testing.T) {
	fm := newLifecycleFakeManager()
	close(fm.startRelease)
	now := time.Date(2026, time.September, 11, 11, 0, 0, 0, time.UTC)
	rs := state.Init(t.TempDir())
	rc := newServiceReconciler(fm, rs, reconcilerOptions{
		services: []ManagedService{{Name: "codebase", AutoStart: true, RequestedPort: 9749}},
		now:      func() time.Time { return now },
	})
	policy, err := rc.policy("codebase")
	if err != nil {
		t.Fatal(err)
	}

	rc.mu.Lock()
	rc.transitionLocked(policy, svcStarting, now)
	rc.mu.Unlock()
	rc.publish(policy, nil)

	now = now.Add(time.Minute)
	rc.finishExplicitOperation(policy, &procman.ServiceStatus{Name: "codebase", Running: true, Healthy: true, Port: 9749}, nil)
	assertPublishedTransition(t, rs, svcHealthy, now)

	// A repeated healthy result is not a transition and therefore must not
	// refresh the published transition time.
	unchanged := now
	now = now.Add(time.Minute)
	rc.finishExplicitOperation(policy, &procman.ServiceStatus{Name: "codebase", Running: true, Healthy: true, Port: 9749}, nil)
	assertPublishedTransition(t, rs, svcHealthy, unchanged)

	now = now.Add(time.Minute)
	rc.finishExplicitOperation(policy, nil, errors.New("restart failed"))
	assertPublishedTransition(t, rs, svcFailed, now)
}
