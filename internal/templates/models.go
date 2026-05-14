// Package templates provides lightweight prompt template discovery, parsing,
// and substitution for meept.
//
// Templates are .md files with YAML frontmatter (name, description, scope)
// and a body containing positional argument slots ($1, $2, $@, ${@:N},
// ${@:N:L}). They fill the gap between typing a raw prompt and invoking a
// full skill.
package templates

// Priority levels for template discovery (lower is higher priority).
const (
	PriorityProject = 0 // .meept/templates/ (project-local)
	PriorityUser    = 1 // ~/.meept/templates/ (user-global)
	PrioritySystem  = 2 // ~/.config/meept/templates/ (system-wide)
)

// TemplateScope controls how long an injected template persists in an
// agent's context.
type TemplateScope string

const (
	// ScopeTurn means the template is used for the current turn only (default).
	ScopeTurn TemplateScope = "turn"
	// ScopeSession means the template persists for the entire conversation
	// until explicitly cleared.
	ScopeSession TemplateScope = "session"
)

// Template represents a parsed prompt template from a .md file.
type Template struct {
	// Name is the unique identifier for the template (e.g., "summarize").
	Name string `json:"name"`

	// Description is a human-readable description of what the template does.
	Description string `json:"description"`

	// Scope controls how long the injected template persists.
	Scope TemplateScope `json:"scope"`

	// Body contains the template text with argument slots.
	Body string `json:"body"`

	// Path is the filesystem path the template was loaded from.
	Path string `json:"path"`

	// Priority indicates the discovery tier (0=project, 1=user, 2=system).
	Priority int `json:"priority"`
}

// TemplateMetadata holds the parsed YAML frontmatter from a template .md file.
type TemplateMetadata struct {
	Name        string        `yaml:"name"`
	Description string        `yaml:"description"`
	Scope       TemplateScope `yaml:"scope"`
}
