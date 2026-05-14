package templates

import (
	"errors"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Parser errors.
var (
	ErrNoFrontmatter = errors.New("no YAML frontmatter found")
	ErrInvalidYAML   = errors.New("invalid YAML frontmatter")
	ErrNoName        = errors.New("template has no name in frontmatter")
)

// ParseError wraps a parsing error with file path context.
type ParseError struct {
	Path    string
	Message string
	Cause   error
}

func (e *ParseError) Error() string {
	if e.Cause != nil {
		return e.Path + ": " + e.Message + ": " + e.Cause.Error()
	}
	return e.Path + ": " + e.Message
}

func (e *ParseError) Unwrap() error {
	return e.Cause
}

// ParseTemplateFile parses a template .md file and returns a Template.
// Returns an error if the file cannot be read or has invalid format.
func ParseTemplateFile(path string) (*Template, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &ParseError{
			Path:    path,
			Message: "failed to read file",
			Cause:   err,
		}
	}

	tmpl, err := ParseTemplateText(string(data))
	if err != nil {
		return nil, &ParseError{
			Path:    path,
			Message: "failed to parse",
			Cause:   err,
		}
	}

	tmpl.Path = path
	return tmpl, nil
}

// ParseTemplateText parses raw template .md text and returns a Template.
func ParseTemplateText(text string) (*Template, error) {
	frontmatter, body, err := splitFrontmatter(text)
	if err != nil {
		return nil, err
	}

	meta, err := parseMetadata(frontmatter)
	if err != nil {
		return nil, err
	}

	if meta.Name == "" {
		return nil, ErrNoName
	}

	// Default scope to turn if not specified.
	scope := meta.Scope
	if scope == "" {
		scope = ScopeTurn
	}

	tmpl := &Template{
		Name:        meta.Name,
		Description: meta.Description,
		Scope:       scope,
		Body:        strings.TrimSpace(body),
	}

	return tmpl, nil
}

// ParseTemplateMetadataOnly parses only the YAML frontmatter from a template
// file. This is faster than ParseTemplateFile as it skips parsing the body.
func ParseTemplateMetadataOnly(path string) (*Template, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &ParseError{
			Path:    path,
			Message: "failed to read file",
			Cause:   err,
		}
	}

	frontmatter, _, err := splitFrontmatter(string(data))
	if err != nil {
		return nil, &ParseError{
			Path:    path,
			Message: "failed to split frontmatter",
			Cause:   err,
		}
	}

	meta, err := parseMetadata(frontmatter)
	if err != nil {
		return nil, &ParseError{
			Path:    path,
			Message: "failed to parse metadata",
			Cause:   err,
		}
	}

	if meta.Name == "" {
		return nil, &ParseError{
			Path:    path,
			Message: "template has no name",
			Cause:   ErrNoName,
		}
	}

	scope := meta.Scope
	if scope == "" {
		scope = ScopeTurn
	}

	return &Template{
		Name:        meta.Name,
		Description: meta.Description,
		Scope:       scope,
		Path:        path,
	}, nil
}

// splitFrontmatter splits YAML frontmatter from the markdown body.
// The frontmatter must be delimited by --- markers.
func splitFrontmatter(text string) (frontmatter, body string, err error) {
	trimmed := strings.TrimLeft(text, " \t\n\r")
	if !strings.HasPrefix(trimmed, "---") {
		return "", "", ErrNoFrontmatter
	}

	// Find the opening ---.
	_, after, ok := strings.Cut(trimmed, "---")
	if !ok {
		return "", "", ErrNoFrontmatter
	}

	// Skip past the opening marker and any trailing content on that line.
	rest := after
	newlinePos := strings.Index(rest, "\n")
	if newlinePos == -1 {
		return "", "", ErrNoFrontmatter
	}
	rest = rest[newlinePos+1:]

	// Check for --- at start of remaining content (empty frontmatter case).
	if strings.HasPrefix(rest, "---") {
		afterClose := rest[3:]
		_, after, ok := strings.Cut(afterClose, "\n")
		if ok {
			body = after
		} else {
			body = strings.TrimPrefix(afterClose, "\n")
		}
		return "", body, nil
	}

	// Find the closing --- on its own line.
	closePos := strings.Index(rest, "\n---")
	if closePos == -1 {
		// Try end-of-string ---.
		trimmedRest := strings.TrimRight(rest, " \t\n\r")
		if strings.HasSuffix(trimmedRest, "---") {
			closePos = strings.LastIndex(trimmedRest, "---")
			frontmatter = rest[:closePos]
			body = ""
			return frontmatter, body, nil
		}
		return "", "", ErrNoFrontmatter
	}

	frontmatter = rest[:closePos]
	// Skip the \n--- and any content after the closing marker line.
	afterClose := rest[closePos+4:]
	_, after0, ok0 := strings.Cut(afterClose, "\n")
	if ok0 {
		body = after0
	} else {
		body = ""
	}

	return frontmatter, body, nil
}

// parseMetadata parses the YAML frontmatter into TemplateMetadata.
func parseMetadata(frontmatter string) (*TemplateMetadata, error) {
	var meta TemplateMetadata

	if err := yaml.Unmarshal([]byte(frontmatter), &meta); err != nil {
		return nil, ErrInvalidYAML
	}

	return &meta, nil
}
