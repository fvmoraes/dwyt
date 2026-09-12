package state

// ToolErrorsSnapshot returns a stable copy for additive API status payloads.
func (s *RuntimeState) ToolErrorsSnapshot() map[string]string {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.ToolErrors))
	for tool, message := range s.ToolErrors {
		out[tool] = message
	}
	return out
}

// ClientsSnapshot returns the persisted setup selection without exposing the
// mutable backing slice to status readers.
func (s *RuntimeState) ClientsSnapshot() []string {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.Clients...)
}
