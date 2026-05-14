package templates

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Limits for session-scoped templates.
const (
	// MaxSessionScopedTemplates is the maximum number of concurrently active
	// session-scoped templates per conversation.
	MaxSessionScopedTemplates = 5

	// MaxSessionScopedCharsTotal is the maximum total character count from
	// all active session-scoped templates in a single conversation.
	MaxSessionScopedCharsTotal = 8000
)

// ActiveTemplate represents a template that is currently active for a
// conversation. The body has already been substituted with the arguments
// provided at activation time.
type ActiveTemplate struct {
	// Name is the template name.
	Name string `json:"name"`

	// SubstitutedBody is the template body with args already applied.
	SubstitutedBody string `json:"substituted_body"`

	// ActivatedAt is when the template was activated for the conversation.
	ActivatedAt time.Time `json:"activated_at"`

	// CharCount is the character count of the substituted body.
	CharCount int `json:"char_count"`
}

// SessionStore tracks per-conversation active session-scoped templates.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string][]ActiveTemplate // conversationID -> active templates
}

// NewSessionStore creates a new SessionStore.
func NewSessionStore() *SessionStore {
	return &SessionStore{
		sessions: make(map[string][]ActiveTemplate),
	}
}

// Activate adds a session-scoped template to the active list for the given
// conversation. Returns an error if adding the template would exceed
// MaxSessionScopedTemplates or MaxSessionScopedCharsTotal.
//
// If a template with the same name is already active, it is replaced.
func (s *SessionStore) Activate(conversationID string, active ActiveTemplate) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	templates := s.sessions[conversationID]

	// Check if already active (will be replaced, no count increase).
	replacing := false
	totalChars := 0
	for _, t := range templates {
		if t.Name == active.Name {
			replacing = true
		}
		totalChars += t.CharCount
	}

	// If not replacing an existing one, check the count limit.
	if !replacing {
		if len(templates) >= MaxSessionScopedTemplates {
			names := make([]string, len(templates))
			for i, t := range templates {
				names[i] = t.Name
			}
			return fmt.Errorf(
				"cannot activate template %q: maximum of %d session-scoped templates reached (active: %s)",
				active.Name, MaxSessionScopedTemplates, strings.Join(names, ", "),
			)
		}
	}

	// Check the total character limit.
	newTotalChars := totalChars + active.CharCount
	if replacing {
		// Subtract the old template's chars if replacing.
		for _, t := range templates {
			if t.Name == active.Name {
				newTotalChars -= t.CharCount
				break
			}
		}
	}
	if newTotalChars > MaxSessionScopedCharsTotal {
		return fmt.Errorf(
			"cannot activate template %q: would exceed maximum of %d total characters for session-scoped templates (current: %d, adding: %d)",
			active.Name, MaxSessionScopedCharsTotal, totalChars, active.CharCount,
		)
	}

	// Replace or append.
	if replacing {
		for i, t := range templates {
			if t.Name == active.Name {
				s.sessions[conversationID][i] = active
				return nil
			}
		}
	}

	s.sessions[conversationID] = append(templates, active)
	return nil
}

// Deactivate removes a specific session-scoped template from the given
// conversation. Returns true if the template was found and removed.
func (s *SessionStore) Deactivate(conversationID, templateName string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	templates, ok := s.sessions[conversationID]
	if !ok {
		return false
	}

	key := normalizeName(templateName)
	for i, t := range templates {
		if normalizeName(t.Name) == key {
			s.sessions[conversationID] = append(templates[:i], templates[i+1:]...)
			if len(s.sessions[conversationID]) == 0 {
				delete(s.sessions, conversationID)
			}
			return true
		}
	}

	return false
}

// Clear removes all active session-scoped templates for the given conversation.
// Returns the names of deactivated templates.
func (s *SessionStore) Clear(conversationID string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	templates, ok := s.sessions[conversationID]
	if !ok {
		return nil
	}

	names := make([]string, len(templates))
	for i, t := range templates {
		names[i] = t.Name
	}

	delete(s.sessions, conversationID)
	return names
}

// GetActive returns all active session-scoped templates for the given
// conversation. Returns nil if none are active.
func (s *SessionStore) GetActive(conversationID string) []ActiveTemplate {
	s.mu.RLock()
	defer s.mu.RUnlock()

	templates, ok := s.sessions[conversationID]
	if !ok {
		return nil
	}

	// Return a copy to avoid data races.
	result := make([]ActiveTemplate, len(templates))
	copy(result, templates)
	return result
}

// ContextString assembles all active session-scoped templates for a
// conversation into a single fenced block for injection into the agent's
// system prompt. Returns an empty string if no templates are active.
func (s *SessionStore) ContextString(conversationID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	templates, ok := s.sessions[conversationID]
	if !ok || len(templates) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("<template-context>\n")
	for _, t := range templates {
		fmt.Fprintf(&b, "<!-- template: %s (activated %s) -->\n", t.Name, t.ActivatedAt.Format(time.RFC3339))
		b.WriteString(t.SubstitutedBody)
		b.WriteString("\n")
	}
	b.WriteString("</template-context>")

	return b.String()
}
