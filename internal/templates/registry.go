package templates

import (
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
)

// Registry holds loaded templates with lookup by name.
type Registry struct {
	mu        sync.RWMutex
	templates map[string]*Template // normalized name -> template
	sessions  *SessionStore
	logger    *slog.Logger
}

// RegistryOption is a functional option for configuring Registry.
type RegistryOption func(*Registry)

// WithRegistryLogger sets the logger for registry operations.
func WithRegistryLogger(logger *slog.Logger) RegistryOption {
	return func(r *Registry) {
		r.logger = logger
	}
}

// WithSessionStore sets the session store for the registry.
func WithSessionStore(store *SessionStore) RegistryOption {
	return func(r *Registry) {
		if store != nil {
			r.sessions = store
		}
	}
}

// NewRegistry creates a new template registry.
func NewRegistry(opts ...RegistryOption) *Registry {
	r := &Registry{
		templates: make(map[string]*Template),
		sessions:  NewSessionStore(),
		logger:    slog.Default(),
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// Register adds or replaces a template in the registry.
func (r *Registry) Register(tmpl *Template) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := normalizeName(tmpl.Name)

	if existing, ok := r.templates[key]; ok {
		r.logger.Warn("Replacing existing template registration",
			"name", tmpl.Name,
			"old_path", existing.Path,
			"new_path", tmpl.Path,
		)
	}

	r.templates[key] = tmpl
	r.logger.Info("Registered template", "name", tmpl.Name)
}

// RegisterAll registers multiple templates at once.
func (r *Registry) RegisterAll(templates []*Template) {
	for _, tmpl := range templates {
		r.Register(tmpl)
	}
}

// Unregister removes a template by name.
func (r *Registry) Unregister(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := normalizeName(name)
	if _, ok := r.templates[key]; ok {
		delete(r.templates, key)
		r.logger.Info("Unregistered template", "name", name)
		return true
	}
	return false
}

// Get looks up a template by name (case-insensitive).
func (r *Registry) Get(name string) *Template {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.templates[normalizeName(name)]
}

// List returns all registered templates.
func (r *Registry) List() []*Template {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*Template, 0, len(r.templates))
	for _, tmpl := range r.templates {
		result = append(result, tmpl)
	}

	// Sort by name for consistent ordering.
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}

// Names returns sorted list of all registered template names.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.templates))
	for _, tmpl := range r.templates {
		names = append(names, tmpl.Name)
	}
	sort.Strings(names)
	return names
}

// Count returns the number of registered templates.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.templates)
}

// Clear removes all templates from the registry.
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.templates = make(map[string]*Template)
	r.logger.Info("Cleared all templates from registry")
}

// Substitute looks up a template by name and applies argument substitution.
// Returns an error if the template is not found.
func (r *Registry) Substitute(name string, args []string) (string, error) {
	tmpl := r.Get(name)
	if tmpl == nil {
		return "", fmt.Errorf("template not found: %s", name)
	}

	return Substitute(tmpl.Body, args), nil
}

// SessionStore returns the underlying session store for direct access.
func (r *Registry) SessionStore() *SessionStore {
	return r.sessions
}

// ActivateSessionTemplate activates a session-scoped template for the given
// conversation. It looks up the template, substitutes the provided args, and
// adds it to the session store. Returns an error if the template is not found
// or if it would exceed session limits.
func (r *Registry) ActivateSessionTemplate(conversationID, templateName string, args []string) error {
	tmpl := r.Get(templateName)
	if tmpl == nil {
		return fmt.Errorf("template not found: %s", templateName)
	}

	substituted := Substitute(tmpl.Body, args)

	active := ActiveTemplate{
		Name:            tmpl.Name,
		SubstitutedBody: substituted,
		CharCount:       len(substituted),
	}

	return r.sessions.Activate(conversationID, active)
}

// DeactivateSessionTemplate removes a session-scoped template from the given
// conversation. Returns true if the template was found and removed.
func (r *Registry) DeactivateSessionTemplate(conversationID, templateName string) bool {
	return r.sessions.Deactivate(conversationID, templateName)
}

// ClearSessionTemplates removes all active session-scoped templates for the
// given conversation. Returns the names of deactivated templates.
func (r *Registry) ClearSessionTemplates(conversationID string) []string {
	return r.sessions.Clear(conversationID)
}

// GetActiveTemplates returns all active session-scoped templates for the
// given conversation.
func (r *Registry) GetActiveTemplates(conversationID string) []ActiveTemplate {
	return r.sessions.GetActive(conversationID)
}

// SessionTemplateContext assembles all active session-scoped templates for a
// conversation into a single fenced block for injection into the agent's
// system prompt. Returns an empty string if no templates are active.
func (r *Registry) SessionTemplateContext(conversationID string) string {
	return r.sessions.ContextString(conversationID)
}

// LoadFromDiscovery runs the given discovery and registers all found
// templates in this registry.
func (r *Registry) LoadFromDiscovery(d *Discovery) error {
	templates, err := d.Discover()
	if err != nil {
		return fmt.Errorf("template discovery failed: %w", err)
	}

	r.RegisterAll(templates)
	r.logger.Info("Loaded templates from discovery", "count", len(templates))
	return nil
}

// ErrTemplateNotFound is returned when a template is not found in the registry.
var ErrTemplateNotFound = errors.New("template not found")
