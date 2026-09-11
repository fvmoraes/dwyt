package state

import "time"

// InvalidateProcessObservation clears process facts that cannot be trusted
// across daemon boots while retaining the operator's intent and port request.
func (s *RuntimeState) InvalidateProcessObservation(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	previous, ok := s.Processes[name]
	if !ok {
		return
	}
	s.Processes[name] = ProcessInfo{
		Name:          name,
		RequestedPort: previous.RequestedPort,
		State:         "unknown",
		DesiredState:  previous.DesiredState,
		Ownership:     "unknown",
	}
	delete(s.ToolErrors, name)
	s.maybeSave()
}

// SetProcessDesired persists operator/startup intent independently of the last
// observation. A stopped intent survives daemon restarts and prevents the
// reconciler from immediately undoing an explicit stop.
func (s *RuntimeState) SetProcessDesired(name, desired string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.Processes[name]
	p.Name = name
	p.DesiredState = desired
	s.Processes[name] = p
	s.maybeSave()
}

// SetProcessLifecycle replaces the lifecycle projection in one critical
// section, avoiding transient combinations such as healthy=true/state=failed.
// Existing timestamps and port hints are retained when the update omits them.
func (s *RuntimeState) SetProcessLifecycle(next ProcessInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()

	previous := s.Processes[next.Name]
	if next.StartedAt.IsZero() {
		switch {
		case next.PID > 0 && next.PID != previous.PID:
			next.StartedAt = time.Now()
		case !previous.StartedAt.IsZero():
			next.StartedAt = previous.StartedAt
		}
	}
	if next.RequestedPort == 0 {
		next.RequestedPort = previous.RequestedPort
	}
	if next.EffectivePort == 0 {
		next.EffectivePort = previous.EffectivePort
	}
	if next.Port == 0 {
		next.Port = next.EffectivePort
	}
	if next.DesiredState == "" {
		next.DesiredState = previous.DesiredState
	}
	if next.Ownership == "" {
		next.Ownership = previous.Ownership
	}
	if next.Identity == "" {
		next.Identity = previous.Identity
	}

	s.Processes[next.Name] = next
	if next.Healthy {
		delete(s.ToolErrors, next.Name)
	} else if next.LastError != "" {
		s.ToolErrors[next.Name] = next.LastError
	}
	s.maybeSave()
}
