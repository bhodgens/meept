// Package skills provides skill discovery, parsing, and execution for meept.
//
// This file implements Hermes-Agent skill compatibility, enabling Meept to
// auto-discover, parse, and execute skills from ~/.hermes/skills/ using the
// agentskills.io open standard.
package skills

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

// HermesPrerequisites describes runtime requirements for a Hermes skill.
type HermesPrerequisites struct {
	// EnvVars lists required environment variable names.
	EnvVars []string `yaml:"env_vars" json:"env_vars,omitempty"`
	// Commands lists required CLI commands (checked via exec.LookPath).
	Commands []string `yaml:"commands" json:"commands,omitempty"`
	// PythonPackages lists required Python packages (checked via pip show).
	PythonPackages []string `yaml:"python_packages" json:"python_packages,omitempty"`
}

// ConfigVar describes a Hermes-style configuration variable declaration.
type ConfigVar struct {
	Key         string `yaml:"key"         json:"key"`
	Description string `yaml:"description" json:"description,omitempty"`
	Default     string `yaml:"default"     json:"default,omitempty"`
	Prompt      string `yaml:"prompt"      json:"prompt,omitempty"`
}

// HermesExtended holds Hermes-specific metadata fields from the
// metadata.hermes section of a SKILL.md frontmatter.
type HermesExtended struct {
	// Config declares config variables for Config.yaml integration.
	Config []ConfigVar `yaml:"config" json:"config,omitempty"`
	// Triggers are keyword triggers (mapped to Meept tags).
	Triggers []string `yaml:"triggers" json:"triggers,omitempty"`
	// Toolsets are named tool groupings (informational only).
	Toolsets []string `yaml:"toolsets" json:"toolsets,omitempty"`
}

// HermesMetadataExtended wraps the nested metadata.hermes structure.
type HermesMetadataExtended struct {
	Hermes *HermesExtended `yaml:"hermes" json:"hermes,omitempty"`
}

// HermesSkillMetadata represents the full frontmatter of a Hermes SKILL.md.
// It embeds the base SkillMetadata for fields shared with Meept and adds
// Hermes-specific fields.
type HermesSkillMetadata struct {
	SkillMetadata

	// Version is the skill version (informational; Meept does not track versions).
	Version string `yaml:"version" json:"version,omitempty"`
	// License is the skill license (informational only).
	License string `yaml:"license" json:"license,omitempty"`
	// Platforms lists supported OS platforms (mapped to Meept tags).
	Platforms []string `yaml:"platforms" json:"platforms,omitempty"`
	// Prerequisites describes runtime requirements validated before execution.
	Prerequisites HermesPrerequisites `yaml:"prerequisites" json:"prerequisites,omitempty"`
	// Metadata holds nested Hermes-specific metadata.
	Metadata *HermesMetadataExtended `yaml:"metadata" json:"metadata,omitempty"`
}

// PrerequisiteChecker validates Hermes skill prerequisites before execution.
type PrerequisiteChecker interface {
	// CheckEnvVars verifies that all required environment variables are set.
	CheckEnvVars(vars []string) error
	// CheckCommands verifies that all required commands are available on PATH.
	CheckCommands(cmds []string) error
	// CheckPythonPackages verifies that all required Python packages are installed.
	CheckPythonPackages(pkgs []string) error
}

// DefaultPrerequisiteChecker implements PrerequisiteChecker using standard
// os/exec and os.Getenv lookups.
type DefaultPrerequisiteChecker struct {
	logger *slog.Logger
}

// NewDefaultPrerequisiteChecker creates a DefaultPrerequisiteChecker.
// Nil logger is replaced with slog.Default.
func NewDefaultPrerequisiteChecker(logger *slog.Logger) *DefaultPrerequisiteChecker {
	if logger == nil {
		logger = slog.Default()
	}
	return &DefaultPrerequisiteChecker{logger: logger}
}

// CheckEnvVars verifies that all required environment variables are set and non-empty.
func (c *DefaultPrerequisiteChecker) CheckEnvVars(vars []string) error {
	for _, v := range vars {
		if err := c.checkEnvVar(v); err != nil {
			return err
		}
	}
	return nil
}

// checkEnvVar checks a single environment variable.
func (c *DefaultPrerequisiteChecker) checkEnvVar(name string) error {
	v, ok := os.LookupEnv(name)
	if !ok || v == "" {
		return fmt.Errorf("missing required env var %s", name)
	}
	return nil
}

// CheckCommands verifies that all required commands are available on PATH.
func (c *DefaultPrerequisiteChecker) CheckCommands(cmds []string) error {
	for _, cmd := range cmds {
		if _, err := exec.LookPath(cmd); err != nil {
			return fmt.Errorf("missing required command: %s", cmd)
		}
	}
	return nil
}

// CheckPythonPackages verifies that all required Python packages are installed.
// It checks using "pip show <package>".
func (c *DefaultPrerequisiteChecker) CheckPythonPackages(pkgs []string) error {
	for _, pkg := range pkgs {
		if err := exec.Command("pip", "show", pkg).Run(); err != nil {
			return fmt.Errorf("missing required python package: %s", pkg)
		}
	}
	return nil
}

// HermesToolMapper translates Hermes tool names to Meept equivalents.
// Unmapped tools are passed through as-is. Tools with no Meept equivalent
// produce a warning log.
type HermesToolMapper struct {
	mapping map[string]string
	logger  *slog.Logger
}

// NewHermesToolMapper creates a HermesToolMapper with the standard tool mapping.
// Nil logger is replaced with slog.Default.
func NewHermesToolMapper(logger *slog.Logger) *HermesToolMapper {
	if logger == nil {
		logger = slog.Default()
	}
	return &HermesToolMapper{
		logger: logger,
		mapping: map[string]string{
			"schedule":     "schedule_create",
			"skill_view":   "skills.get",
			"skills_list":  "skills.list",
			"team_create":  "delegate_task",
			"team_list":    "platform_agents",
			"team_message": "request_handoff",
			"image_gen":    "", // no Meept equivalent yet — see https://github.com/bhodgens/meept/issues/13
		},
	}
}

// Translate maps a single Hermes tool name to its Meept equivalent.
// If no mapping exists, the original name is returned unchanged.
// If the mapping points to an empty string (no equivalent), the original
// name is returned and a warning is logged.
func (m *HermesToolMapper) Translate(toolName string) string {
	mapped, ok := m.mapping[toolName]
	if !ok {
		return toolName
	}
	if mapped == "" {
		m.logger.Warn("hermes tool has no meept equivalent",
			"tool", toolName,
		)
		return toolName
	}
	m.logger.Debug("hermes tool mapped",
		"hermes", toolName,
		"meept", mapped,
	)
	return mapped
}

// TranslateToolReferences rewrites Hermes tool name references in the skill
// body text. It handles common patterns:
//
//   - tool_name( style calls (where tool_name is immediately followed by open paren)
//   - "tool_name" or `tool_name` quoted references
//   - "- tool_name" list items at the start of a line
//
// Unmapped tools are left unchanged.
func (m *HermesToolMapper) TranslateToolReferences(body string) string {
	result := body

	// Sort keys by length descending so longer matches are replaced first.
	keys := make([]string, 0, len(m.mapping))
	for k, v := range m.mapping {
		if v != "" {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		return len(keys[i]) > len(keys[j])
	})

	// Replace tool references in common patterns.
	for _, hermesTool := range keys {
		meeptTool := m.mapping[hermesTool]

		// Pattern: "tool_name" or `tool_name` as quoted reference (do first)
		result = strings.ReplaceAll(result, `"`+hermesTool+`"`, `"`+meeptTool+`"`)
		result = strings.ReplaceAll(result, "`"+hermesTool+"`", "`"+meeptTool+"`")

		// Pattern: tool_name( - function call style (word boundary before, ( after)
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(hermesTool) + `\(`)
		result = re.ReplaceAllString(result, meeptTool+"(")

		// Pattern: - tool_name (list items at start of line)
		re = regexp.MustCompile(`(?m)^(\s*[-*]\s+)` + regexp.QuoteMeta(hermesTool) + `(\s|$)`)
		result = re.ReplaceAllString(result, "${1}"+meeptTool+"${2}")

		// Pattern: plain word-boundary replacement for natural language references.
		// Only matches when not already inside quotes/backticks (which were
		// handled above). Uses word boundaries to avoid partial matches.
		re = regexp.MustCompile(`\b` + regexp.QuoteMeta(hermesTool) + `\b`)
		result = re.ReplaceAllString(result, meeptTool)
	}

	return result
}

// CheckPrerequisites is a convenience function that runs all prerequisite
// checks for the given HermesPrerequisites. It returns the first error
// encountered, or nil if all checks pass.
func CheckPrerequisites(checker PrerequisiteChecker, prereqs *HermesPrerequisites) error {
	if checker == nil || prereqs == nil {
		return nil
	}

	if err := checker.CheckEnvVars(prereqs.EnvVars); err != nil {
		return err
	}
	if err := checker.CheckCommands(prereqs.Commands); err != nil {
		return err
	}
	if err := checker.CheckPythonPackages(prereqs.PythonPackages); err != nil {
		return err
	}

	return nil
}
