package liteclient

import "slices"

// History manages input history (previous commands/messages).
type History struct {
	entries   []string
	maxSize   int
	current   int // current position in history (for navigation)
	temporary string // temporary storage for current input when navigating
}

// NewHistory creates a new history manager.
func NewHistory(maxSize int) *History {
	return &History{
		entries: make([]string, 0, maxSize),
		maxSize: maxSize,
		current: -1,
	}
}

// Add adds a new entry to history.
func (h *History) Add(entry string) {
	if entry == "" {
		return
	}

	// Don't add duplicates consecutively
	if len(h.entries) > 0 && h.entries[len(h.entries)-1] == entry {
		return
	}

	h.entries = append(h.entries, entry)

	// Trim if over max size
	if len(h.entries) > h.maxSize {
		h.entries = h.entries[1:]
	}

	h.current = -1 // Reset to end of history
}

// Up returns the previous entry in history and true if available.
// When first called, stores the temporary (current input) for later restoration.
func (h *History) Up(temporary string) (string, bool) {
	if len(h.entries) == 0 {
		return "", false
	}

	// Store temporary on first navigation
	if h.current == -1 {
		h.temporary = temporary
	}

	// Move up in history
	if h.current < len(h.entries)-1 {
		h.current++
		return h.entries[len(h.entries)-1-h.current], true
	}

	// Already at oldest entry
	return h.entries[0], false
}

// Down returns the next entry in history and true if available.
// Returns the temporary (original input) when reaching the end.
func (h *History) Down(temporary string) (string, bool) {
	if len(h.entries) == 0 || h.current == -1 {
		return "", false
	}

	h.current--

	if h.current < 0 {
		// Back to the start - return original input
		return h.temporary, true
	}

	return h.entries[len(h.entries)-1-h.current], true
}

// Reset resets the history navigation state.
func (h *History) Reset() {
	h.current = -1
	h.temporary = ""
}

// HasPrevious returns true if there's a previous entry.
func (h *History) HasPrevious() bool {
	return len(h.entries) > 0 && h.current < len(h.entries)-1
}

// HasNext returns true if there's a next entry.
func (h *History) HasNext() bool {
	return h.current > 0
}

// Len returns the number of entries in history.
func (h *History) Len() int {
	return len(h.entries)
}

// Entries returns a copy of all history entries.
func (h *History) Entries() []string {
	return slices.Clone(h.entries)
}

// Clear clears all history entries.
func (h *History) Clear() {
	h.entries = h.entries[:0]
	h.current = -1
	h.temporary = ""
}
