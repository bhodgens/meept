package sharedclient

// SessionHistory provides per-session input history management.
// Wraps multiple History instances keyed by session ID.
type SessionHistory struct {
	histories map[string]*History
	maxSize   int
}

// NewSessionHistory creates a new per-session history manager.
func NewSessionHistory(maxSize int) *SessionHistory {
	return &SessionHistory{
		histories: make(map[string]*History),
		maxSize:   maxSize,
	}
}

// GetHistory returns the history for a specific session, creating it if needed.
func (s *SessionHistory) GetHistory(sessionID string) *History {
	if h, ok := s.histories[sessionID]; ok {
		return h
	}
	h := NewHistory(s.maxSize)
	s.histories[sessionID] = h
	return h
}

// Add adds an entry to the history for the given session.
func (s *SessionHistory) Add(sessionID, entry string) {
	s.GetHistory(sessionID).Add(entry)
}

// Up returns the previous entry for the given session.
// Returns the entry and true if available, or empty string and false otherwise.
func (s *SessionHistory) Up(sessionID, temporary string) (string, bool) {
	return s.GetHistory(sessionID).Up(temporary)
}

// Down returns the next entry for the given session.
// Returns the entry and true if available, or empty string and false otherwise.
func (s *SessionHistory) Down(sessionID, temporary string) (string, bool) {
	return s.GetHistory(sessionID).Down(temporary)
}

// ClearSession clears the history for a specific session.
func (s *SessionHistory) ClearSession(sessionID string) {
	if h, ok := s.histories[sessionID]; ok {
		h.Clear()
	}
}

// ClearAll clears all session histories.
func (s *SessionHistory) ClearAll() {
	s.histories = make(map[string]*History)
}

// GetEntries returns all entries for a session.
func (s *SessionHistory) GetEntries(sessionID string) []string {
	return s.GetHistory(sessionID).Entries()
}

// Reset resets the navigation state for a session.
func (s *SessionHistory) Reset(sessionID string) {
	s.GetHistory(sessionID).Reset()
}

// HasPrevious returns true if there's a previous entry for the session.
func (s *SessionHistory) HasPrevious(sessionID string) bool {
	return s.GetHistory(sessionID).HasPrevious()
}

// HasNext returns true if there's a next entry for the session.
func (s *SessionHistory) HasNext(sessionID string) bool {
	return s.GetHistory(sessionID).HasNext()
}
