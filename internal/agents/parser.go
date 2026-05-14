package agents

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Parser errors.
var (
	ErrNoFrontmatter = errors.New("no YAML frontmatter found")
	ErrInvalidYAML   = errors.New("invalid YAML frontmatter")
	ErrNoID          = errors.New("agent has no id in frontmatter")
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

// ParseAgentFile parses an AGENT.md file and returns an AgentDefinition.
func ParseAgentFile(path string) (*AgentDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &ParseError{
			Path:    path,
			Message: "failed to read file",
			Cause:   err,
		}
	}

	def, err := ParseAgentText(string(data))
	if err != nil {
		return nil, &ParseError{
			Path:    path,
			Message: "failed to parse",
			Cause:   err,
		}
	}

	def.Path = path
	return def, nil
}

// ParseAgentText parses raw AGENT.md text and returns an AgentDefinition.
func ParseAgentText(text string) (*AgentDefinition, error) {
	frontmatter, body, err := splitFrontmatter(text)
	if err != nil {
		return nil, err
	}

	meta, err := parseMetadata(frontmatter)
	if err != nil {
		return nil, err
	}

	if meta.ID == "" {
		return nil, ErrNoID
	}

	def := &AgentDefinition{
		AgentMetadata: *meta,
		Body:          strings.TrimSpace(body),
	}

	return def, nil
}

// ParseMetadataOnly parses only the YAML frontmatter from an AGENT.md file.
// This is faster than ParseAgentFile as it skips the body.
func ParseMetadataOnly(path string) (*AgentMetadata, error) {
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

	if meta.ID == "" {
		return nil, &ParseError{
			Path:    path,
			Message: "agent has no id",
			Cause:   ErrNoID,
		}
	}

	return meta, nil
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

	// Check for --- at start of remaining content (empty frontmatter case)
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

// parseMetadata parses the YAML frontmatter into AgentMetadata.
func parseMetadata(frontmatter string) (*AgentMetadata, error) {
	var meta AgentMetadata

	if err := yaml.Unmarshal([]byte(frontmatter), &meta); err != nil {
		return nil, ErrInvalidYAML
	}

	// Handle alternative field names (with underscores instead of hyphens).
	// The frontmatter was already validated above, so this parse cannot fail.
	var altMeta struct {
		AdditionalTools       []string          `yaml:"additional_tools"`
		AvailableSkills       []string          `yaml:"available_skills"`
		SkillTriggers         map[string]string `yaml:"skill_triggers"`
		MaxIterations         int               `yaml:"max_iterations"`
		TimeoutSeconds        int               `yaml:"timeout_seconds"`
		MaxTokensPerTurn      int               `yaml:"max_tokens_per_turn"`
		MaxConversationTokens int               `yaml:"max_conversation_tokens"`
		MaxMemoryRefs         int               `yaml:"max_memory_refs"`
		TopP                  *float64          `yaml:"top_p"`
	}
	if err := yaml.Unmarshal([]byte(frontmatter), &altMeta); err != nil {
		return nil, fmt.Errorf("parse alt agent metadata: %w", err)
	}

	// Merge alternative field values if primary is empty/zero
	if len(meta.AdditionalTools) == 0 && len(altMeta.AdditionalTools) > 0 {
		meta.AdditionalTools = altMeta.AdditionalTools
	}
	if len(meta.AvailableSkills) == 0 && len(altMeta.AvailableSkills) > 0 {
		meta.AvailableSkills = altMeta.AvailableSkills
	}
	if meta.SkillTriggers == nil && altMeta.SkillTriggers != nil {
		meta.SkillTriggers = altMeta.SkillTriggers
	}
	if meta.MaxIterations == 0 && altMeta.MaxIterations != 0 {
		meta.MaxIterations = altMeta.MaxIterations
	}
	if meta.TimeoutSeconds == 0 && altMeta.TimeoutSeconds != 0 {
		meta.TimeoutSeconds = altMeta.TimeoutSeconds
	}
	if meta.MaxTokensPerTurn == 0 && altMeta.MaxTokensPerTurn != 0 {
		meta.MaxTokensPerTurn = altMeta.MaxTokensPerTurn
	}
	if meta.MaxConversationTokens == 0 && altMeta.MaxConversationTokens != 0 {
		meta.MaxConversationTokens = altMeta.MaxConversationTokens
	}
	if meta.MaxMemoryRefs == 0 && altMeta.MaxMemoryRefs != 0 {
		meta.MaxMemoryRefs = altMeta.MaxMemoryRefs
	}
	if meta.TopP == nil && altMeta.TopP != nil {
		meta.TopP = altMeta.TopP
	}

	return &meta, nil
}
