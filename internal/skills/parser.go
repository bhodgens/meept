package skills

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Parser errors.
var (
	ErrNoFrontmatter = errors.New("no YAML frontmatter found")
	ErrInvalidYAML   = errors.New("invalid YAML frontmatter")
	ErrNoName        = errors.New("skill has no name in frontmatter")
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

// ParseSkillMetadataOnly parses only the YAML frontmatter from a SKILL.md file.
// This is faster than ParseSkillFile as it skips parsing the body content.
// YAML frontmatter is optional: when absent the slug (directory name) is used as the name.
func ParseSkillMetadataOnly(path string) (*SkillIndexEntry, error) {
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
		if errors.Is(err, ErrNoFrontmatter) {
			// No frontmatter — derive name from directory.
			// Return valid entry alongside ErrNoFrontmatter so callers can warn.
			slug := slugFromPath(path)
			return &SkillIndexEntry{
				Name:      slug,
				Path:      path,
				RiskLevel: "medium",
			}, ErrNoFrontmatter
		}
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
			Message: "skill has no name",
			Cause:   ErrNoName,
		}
	}

	entry := &SkillIndexEntry{
		Name:         meta.Name,
		Description:  meta.Description,
		Requires:     meta.Requires,
		Tags:         meta.Tags,
		Path:         path,
		RiskLevel:    meta.RiskLevel,
		AllowedTools: meta.AllowedTools,
		Examples:     meta.Examples,
	}

	// Apply defaults
	if entry.RiskLevel == "" {
		entry.RiskLevel = "medium"
	}

	return entry, nil
}

// ParseSkillFile parses a SKILL.md file and returns a Skill.
// Returns an error if the file cannot be read or has invalid format.
func ParseSkillFile(path string) (*Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &ParseError{
			Path:    path,
			Message: "failed to read file",
			Cause:   err,
		}
	}

	skill, err := ParseSkillText(string(data))
	if err != nil {
		return nil, &ParseError{
			Path:    path,
			Message: "failed to parse",
			Cause:   err,
		}
	}

	skill.Path = path
	return skill, nil
}

// ParseSkillText parses raw SKILL.md text and returns a Skill.
// YAML frontmatter is optional: when absent the full text becomes the body.
func ParseSkillText(text string) (*Skill, error) {
	frontmatter, body, err := splitFrontmatter(text)
	if err != nil {
		if errors.Is(err, ErrNoFrontmatter) {
			// No frontmatter — use full text as body, no name set.
			// Return valid skill alongside ErrNoFrontmatter so callers can warn.
			return &Skill{
				Body:          strings.TrimSpace(text),
				RiskLevel:     "medium",
				MaxIterations: 10,
			}, ErrNoFrontmatter
		}
		return nil, err
	}

	meta, err := parseMetadata(frontmatter)
	if err != nil {
		return nil, err
	}

	if meta.Name == "" {
		return nil, ErrNoName
	}

	skill := &Skill{
		Name:          meta.Name,
		Description:   meta.Description,
		Requires:      meta.Requires,
		Tags:          meta.Tags,
		Examples:      meta.Examples,
		Body:          strings.TrimSpace(body),
		AllowedTools:  meta.AllowedTools,
		RiskLevel:     meta.RiskLevel,
		MaxIterations: meta.MaxIterations,
		Temperature:   meta.Temperature,
		MaxTokens:     meta.MaxTokens,
		MCPServers:    meta.MCPServers,
	}

	// Apply defaults
	if skill.RiskLevel == "" {
		skill.RiskLevel = "medium"
	}
	if skill.MaxIterations == 0 {
		skill.MaxIterations = 10
	}

	return skill, nil
}

// splitFrontmatter splits YAML frontmatter from the markdown body.
// The frontmatter must be delimited by --- markers.
func splitFrontmatter(text string) (frontmatter, body string, err error) {
	trimmed := strings.TrimLeft(text, " \t\n\r")
	if !strings.HasPrefix(trimmed, "---") {
		return "", "", ErrNoFrontmatter
	}

	// Find the opening ---
	_, after, ok := strings.Cut(trimmed, "---")
	if !ok {
		return "", "", ErrNoFrontmatter
	}

	// Skip past the opening marker and any trailing content on that line
	rest := after
	newlinePos := strings.Index(rest, "\n")
	if newlinePos == -1 {
		return "", "", ErrNoFrontmatter
	}
	rest = rest[newlinePos+1:]

	// Find the closing --- (can be at start of a line or immediately after opening)
	// Check for --- at start of remaining content (empty frontmatter case)
	if strings.HasPrefix(rest, "---") {
		// Empty frontmatter
		afterClose := rest[3:]
		_, after, ok := strings.Cut(afterClose, "\n")
		if ok {
			body = after
		} else {
			body = strings.TrimPrefix(afterClose, "\n")
		}
		return "", body, nil
	}

	// Find the closing --- on its own line
	closePos := strings.Index(rest, "\n---")
	if closePos == -1 {
		// Try end-of-string ---
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
	// Skip the \n--- and any content after the closing marker line
	afterClose := rest[closePos+4:]
	_, after0, ok0 := strings.Cut(afterClose, "\n")
	if ok0 {
		body = after0
	} else {
		body = ""
	}

	return frontmatter, body, nil
}

// parseMetadata parses the YAML frontmatter into SkillMetadata.
func parseMetadata(frontmatter string) (*SkillMetadata, error) {
	// Start with zero values to detect which fields were actually set
	var meta SkillMetadata

	if err := yaml.Unmarshal([]byte(frontmatter), &meta); err != nil {
		return nil, ErrInvalidYAML
	}

	// Handle alternative field names (with underscores instead of hyphens).
	// The frontmatter was already validated above, so this parse cannot fail.
	var altMeta struct {
		AllowedTools  []string `yaml:"allowed_tools"`
		RiskLevel     string   `yaml:"risk_level"`
		MaxIterations int      `yaml:"max_iterations"`
		MaxTokens     *int     `yaml:"max_tokens"`
	}
	if err := yaml.Unmarshal([]byte(frontmatter), &altMeta); err != nil {
		return nil, fmt.Errorf("parse alt skill metadata: %w", err)
	}

	// Merge alternative field values if primary is empty/zero
	if len(meta.AllowedTools) == 0 && len(altMeta.AllowedTools) > 0 {
		meta.AllowedTools = altMeta.AllowedTools
	}
	if meta.RiskLevel == "" && altMeta.RiskLevel != "" {
		meta.RiskLevel = altMeta.RiskLevel
	}
	if meta.MaxIterations == 0 && altMeta.MaxIterations != 0 {
		meta.MaxIterations = altMeta.MaxIterations
	}
	if meta.MaxTokens == nil && altMeta.MaxTokens != nil {
		meta.MaxTokens = altMeta.MaxTokens
	}

	return &meta, nil
}

// slugFromPath derives a skill slug from the file path by using the parent directory name.
func slugFromPath(path string) string {
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		return filepath.Base(dir)
	}
	return "unknown"
}
