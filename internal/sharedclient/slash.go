package sharedclient

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// SlashCommand represents a parsed slash command.
type SlashCommand struct {
	// Name is the command name without the leading slash.
	Name string
	// Args contains any arguments after the command name.
	Args []string
}

// CustomCommand represents a user-defined slash command loaded from a markdown file.
type CustomCommand struct {
	// Name is the command name (derived from filename or frontmatter).
	Name string
	// Description is a short description of what the command does.
	Description string
	// Arguments lists named arguments the command accepts.
	Arguments []string
	// Template is the body content used as the message template.
	Template string
}

// customCommandCache holds discovered custom commands, keyed by command name.
// Populated by DiscoverCustomCommands or SetCustomCommands (for testing).
var customCommandCache map[string]CustomCommand

// customCommandFrontmatter represents the YAML frontmatter of a command file.
type customCommandFrontmatter struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Arguments   []string `yaml:"arguments"`
}

// builtinCommands is the set of built-in commands that are handled locally.
var builtinCommands = map[string]struct{}{
	"help":      {},
	"new":       {},
	"clear":     {},
	"retry":     {},
	"undo":      {},
	"usage":     {},
	"stop":      {},
	"status":    {},
	"vim":       {},
	"session":   {},
	"task":      {},
	"cancel":    {},
	"amend":     {},
	"interrupt": {},
	"tasks":     {},
	"diff":      {},
	"model":     {},
	"compact":   {},
	"edit":      {},
	"plan":      {},
	"review":    {},
}

// CommandTasks is the "tasks" command name, exported for compatibility with tui/constants.go.
const CommandTasks = "tasks"

// ParseSlash parses a slash command from input text.
// Returns nil if the input is not a slash command.
//
// A valid slash command:
//   - Starts with "/"
//   - Has at least one character after the slash
//   - Command name is alphanumeric with hyphens/underscores allowed
//
// Examples:
//
//	"/help" -> SlashCommand{Name: "help", Args: nil}
//	"/usage" -> SlashCommand{Name: "usage", Args: nil}
//	"/session list" -> SlashCommand{Name: "session", Args: []string{"list"}}
//	"/my-skill arg1 arg2" -> SlashCommand{Name: "my-skill", Args: []string{"arg1", "arg2"}}
//	"hello" -> nil (not a command)
//	"/" -> nil (no command name)
func ParseSlash(input string) *SlashCommand {
	input = strings.TrimSpace(input)

	// Must start with /
	if !strings.HasPrefix(input, "/") {
		return nil
	}

	// Remove the leading slash
	rest := input[1:]
	if rest == "" {
		return nil
	}

	// Split into parts by whitespace
	parts := strings.Fields(rest)
	if len(parts) == 0 {
		return nil
	}

	// First part is the command name
	name := parts[0]

	// Validate command name: must be non-empty and contain only
	// alphanumeric characters, hyphens, or underscores
	if !isValidCommandName(name) {
		return nil
	}

	// Remaining parts are arguments
	var args []string
	if len(parts) > 1 {
		args = parts[1:]
	}

	return &SlashCommand{
		Name: name,
		Args: args,
	}
}

// isValidCommandName checks if a command name is valid.
// Valid names contain only alphanumeric characters, hyphens, and underscores.
func isValidCommandName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if !isCommandNameChar(r) {
			return false
		}
	}
	return true
}

// isCommandNameChar returns true if the rune is valid in a command name.
func isCommandNameChar(r rune) bool {
	return (r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') ||
		r == '-' ||
		r == '_'
}

// BuiltinCommands returns a list of built-in command names for autocomplete.
func BuiltinCommands() []string {
	commands := make([]string, 0, len(builtinCommands))
	for name := range builtinCommands {
		commands = append(commands, name)
	}
	// Sort for consistent ordering
	sortStrings(commands)
	return commands
}

// IsBuiltin returns true if the command is a built-in (not a skill or custom).
func IsBuiltin(name string) bool {
	_, ok := builtinCommands[name]
	return ok
}

// IsCustomCommand returns true if the command is a user-defined custom command.
func IsCustomCommand(name string) bool {
	if customCommandCache == nil {
		return false
	}
	_, ok := customCommandCache[name]
	return ok
}

// GetCustomCommand returns the custom command definition for the given name,
// or the zero value and false if not found.
func GetCustomCommand(name string) (CustomCommand, bool) {
	if customCommandCache == nil {
		return CustomCommand{}, false
	}
	cmd, ok := customCommandCache[name]
	return cmd, ok
}

// CustomCommandNames returns a sorted list of all discovered custom command names.
func CustomCommandNames() []string {
	if customCommandCache == nil {
		return nil
	}
	names := make([]string, 0, len(customCommandCache))
	for name := range customCommandCache {
		names = append(names, name)
	}
	sortStrings(names)
	return names
}

// SetCustomCommands replaces the custom command cache. Used primarily for testing.
func SetCustomCommands(cmds map[string]CustomCommand) {
	customCommandCache = cmds
}

// DiscoverCustomCommands scans discovery paths for markdown command files and
// returns a map of command name to CustomCommand. Results are cached in the
// package-level customCommandCache for subsequent lookups.
//
// Discovery order (project-local overrides user-global on name collision):
//  1. .meept/commands/*.md  (project-local, if .meept/ exists in cwd)
//  2. ~/.meept/commands/*.md (user-global)
func DiscoverCustomCommands() map[string]CustomCommand {
	cmds := make(map[string]CustomCommand)

	// User-global first (lower priority)
	homeDir, err := os.UserHomeDir()
	if err == nil {
		userPath := filepath.Join(homeDir, ".meept", "commands")
		loadCommandsFromDir(cmds, userPath)
	}

	// Project-local second (higher priority, overwrites user-global on collision)
	cwd, err := os.Getwd()
	if err == nil {
		projectPath := filepath.Join(cwd, ".meept", "commands")
		if info, statErr := os.Stat(projectPath); statErr == nil && info.IsDir() {
			loadCommandsFromDir(cmds, projectPath)
		}
	}

	customCommandCache = cmds
	return cmds
}

// loadCommandsFromDir reads all *.md files in dir and adds them to cmds.
// If a command name already exists in cmds, it is overwritten (caller controls
// priority by invocation order).
func loadCommandsFromDir(cmds map[string]CustomCommand, dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		cmd, err := parseCommandFile(filepath.Join(dir, entry.Name()))
		if err != nil || cmd.Name == "" {
			continue
		}
		cmds[cmd.Name] = cmd
	}
}

// frontmatterDelimiter matches "---" on its own line at the start of a file,
// then everything up to the closing "---". The body after the closing delimiter
// is optional (the file may end immediately after "---").
var frontmatterRe = regexp.MustCompile(`(?s)^---\n(.*?)\n---\n?(.*)`)

// parseCommandFile reads a markdown file and extracts frontmatter + body.
func parseCommandFile(path string) (CustomCommand, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return CustomCommand{}, err
	}

	content := strings.TrimSpace(string(data))
	var fm customCommandFrontmatter
	var body string

	matches := frontmatterRe.FindStringSubmatch(content)
	if len(matches) == 3 {
		// Has frontmatter
		if err := yaml.Unmarshal([]byte(matches[1]), &fm); err != nil {
			return CustomCommand{}, fmt.Errorf("parse frontmatter in %s: %w", path, err)
		}
		body = strings.TrimSpace(matches[2])
	} else {
		// No frontmatter: use entire content as template, derive name from filename
		body = content
	}

	// Derive name: frontmatter takes precedence, then filename (without extension).
	name := fm.Name
	if name == "" {
		base := filepath.Base(path)
		name = strings.TrimSuffix(base, ".md")
	}

	// Validate derived name
	if !isValidCommandName(name) {
		return CustomCommand{}, fmt.Errorf("invalid command name %q derived from %s", name, path)
	}

	return CustomCommand{
		Name:        name,
		Description: fm.Description,
		Arguments:   fm.Arguments,
		Template:    body,
	}, nil
}

// RenderTemplate applies argument substitution to a custom command template.
//
// Substitutions:
//   - "$ARGUMENTS" is replaced with all arguments joined by a single space.
//   - "$1", "$2", ... "$N" are replaced by the corresponding positional argument.
func RenderTemplate(tmpl string, args []string) string {
	// Positional substitutions ($1, $2, ...)
	for i, arg := range args {
		placeholder := fmt.Sprintf("$%d", i+1)
		tmpl = strings.ReplaceAll(tmpl, placeholder, arg)
	}

	// Whole-arguments substitution
	allArgs := strings.Join(args, " ")
	tmpl = strings.ReplaceAll(tmpl, "$ARGUMENTS", allArgs)

	return tmpl
}

// SortStrings sorts a slice of strings in place using simple insertion sort.
// This avoids importing the sort package for a small slice.
func SortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// sortStrings is an alias for SortStrings, used internally.
var sortStrings = SortStrings

// IsSlashCommand checks if input starts with a slash command.
func IsSlashCommand(input string) bool {
	input = strings.TrimSpace(input)
	return strings.HasPrefix(input, "/") && len(input) > 1
}
