package daemon

// voice_timing_override.go - the per-call timing override store for
// `meept voice local --turn-confirm-ms/--smart-turn-mode/
// --deepgram-endpoint-ms`.
//
// The store is keyed by EMPLOYEE ID, not call id: the registry enforces one
// live call per employee across both legs, so the employee id is a stable
// identity for a live call that the daemon knows BEFORE leg.Open runs. The
// child answers the C1 open frame with setup, and setup triggers keys
// emission — registering an override after leg.Open returned would race
// that setup frame. sendKeysFrame already resolves the employee id from the
// registry by ccid, so it looks the override up by the same key.
//
// Nil entry = no override = byte-identical config behavior. Entries are
// deleted on open refusal and on release so nothing leaks into a later
// call. All access is mutex-guarded; no I/O under the mutex.

import "sync"

// callTimingOverrides holds the three per-call timing selectors. A nil
// field means "no override for this selector"; an explicit pointer to 0 is
// a real override (restore legacy per-final dispatch / timer-only
// endpointing / smart-turn off).
type callTimingOverrides struct {
	TurnConfirmMs      *int
	SmartTurnMode      *int
	DeepgramEndpointMs *int
}

// timingOverridesByEmployee is the mutex-guarded store embedded on
// VoiceWiring. A nil map is a valid empty store (lazy init on first set).
type timingOverridesByEmployee struct {
	mu   sync.Mutex
	byID map[string]*callTimingOverrides
}

func (s *timingOverridesByEmployee) get(employeeID string) *callTimingOverrides {
	if employeeID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.byID[employeeID]
}

func (s *timingOverridesByEmployee) set(employeeID string, o *callTimingOverrides) {
	if employeeID == "" || o == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byID == nil {
		s.byID = make(map[string]*callTimingOverrides)
	}
	s.byID[employeeID] = o
}

func (s *timingOverridesByEmployee) delete(employeeID string) {
	if employeeID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byID, employeeID)
}

// deleteIf removes the entry only when it is still the given pointer.
// Used on the open-refusal path so a concurrent refused open for the same
// employee cannot delete a different, live call's override.
func (s *timingOverridesByEmployee) deleteIf(employeeID string, want *callTimingOverrides) {
	if employeeID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if cur := s.byID[employeeID]; cur == want {
		delete(s.byID, employeeID)
	}
}

// VoiceWiring accessors. Kept as methods (not raw field access) so the
// mutex discipline has one home and the tests can pin get/set/delete.

func (w *VoiceWiring) callTimingOverride(employeeID string) *callTimingOverrides {
	return w.callTimingOverrides.get(employeeID)
}

func (w *VoiceWiring) setCallTimingOverride(employeeID string, o *callTimingOverrides) {
	w.callTimingOverrides.set(employeeID, o)
}

func (w *VoiceWiring) deleteCallTimingOverride(employeeID string) {
	w.callTimingOverrides.delete(employeeID)
}

func (w *VoiceWiring) deleteCallTimingOverrideIf(employeeID string, o *callTimingOverrides) {
	if o == nil {
		return // nothing was registered by this caller
	}
	w.callTimingOverrides.deleteIf(employeeID, o)
}
