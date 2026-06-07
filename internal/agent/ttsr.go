package agent

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// TTSRRule represents a stream rule that monitors LLM output for pattern
// violations and can inject corrective content when triggered.
type TTSRRule struct {
	Name      string   `yaml:"name" json:"name"`
	Scope     string   `yaml:"scope" json:"scope"`         // "text", "thinking", "tool_call", "any"
	Condition string   `yaml:"condition" json:"condition"` // regex pattern
	Interrupt bool     `yaml:"interrupt" json:"interrupt"` // true = abort+retry, false = in-band reminder
	Repeat    string   `yaml:"repeat" json:"repeat"`       // "once" or "after-gap:N"
	Globs     []string `yaml:"globs" json:"globs"`         // file globs for scoped rules
	Content   string   `yaml:"-" json:"-"`                 // rule body from markdown content

	compiled    *regexp.Regexp
	injectedAt  int  // turn number when last injected
	hasInjected bool // whether rule has been injected at all
}

// ttsrFrontmatter holds the YAML frontmatter parsed from a TT-SR rule file.
type ttsrFrontmatter struct {
	Name      string   `yaml:"name"`
	Scope     string   `yaml:"scope"`
	Condition string   `yaml:"condition"`
	Interrupt bool     `yaml:"interrupt"`
	Repeat    string   `yaml:"repeat"`
	Globs     []string `yaml:"globs"`
}

// TTSRManager manages stream rule monitoring and enforcement.
type TTSRManager struct {
	mu     sync.RWMutex
	rules  []*TTSRRule
	logger *slog.Logger
}

// NewTTSRManager creates a new TT-SR manager.
func NewTTSRManager(logger *slog.Logger) *TTSRManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &TTSRManager{
		logger: logger.With("component", "ttsr"),
	}
}

// LoadRules scans a directory for skill/rule files with a scope field in their
// YAML frontmatter (indicating a TT-SR rule). Each matching file is parsed and
// its compiled rules are added to the manager.
func (m *TTSRManager) LoadRules(skillsDir string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Reset rules on reload
	m.rules = m.rules[:0]

	m.scanDirLocked(skillsDir)

	m.logger.Info("ttsr: rules loaded", "count", len(m.rules))
	return nil
}

// LoadRulesFromDirs loads TT-SR rules from multiple directories, appending
// rules from each. Existing rules are reset before loading. Non-existent
// directories are silently skipped.
func (m *TTSRManager) LoadRulesFromDirs(dirs []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.rules = m.rules[:0]

	for _, dir := range dirs {
		m.scanDirLocked(dir)
	}

	m.logger.Info("ttsr: rules loaded", "count", len(m.rules), "dirs", dirs)
	return nil
}

// scanDirLocked scans a single directory for rule files. Caller must hold m.mu.
func (m *TTSRManager) scanDirLocked(skillsDir string) {
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return // no skills directory is fine
		}
		m.logger.Warn("ttsr: reading skills directory", "path", skillsDir, "error", err)
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			// Check for SKILL.md or RULE.md inside directory
			for _, name := range []string{"SKILL.md", "RULE.md"} {
				path := filepath.Join(skillsDir, entry.Name(), name)
				if err := m.loadRuleFile(path); err != nil {
					m.logger.Warn("ttsr: failed to load rule file",
						"path", path,
						"error", err,
					)
				}
			}
		} else if strings.HasSuffix(entry.Name(), ".md") {
			path := filepath.Join(skillsDir, entry.Name())
			if err := m.loadRuleFile(path); err != nil {
				m.logger.Warn("ttsr: failed to load rule file",
					"path", path,
					"error", err,
				)
			}
		}
	}
}

// loadRuleFile loads a single rule file. Caller must hold m.mu.
func (m *TTSRManager) loadRuleFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	frontmatter, body, err := splitTTSRFrontmatter(string(data))
	if err != nil || frontmatter == "" {
		return nil // not a TT-SR rule file, skip silently
	}

	var fm ttsrFrontmatter
	if err := yaml.Unmarshal([]byte(frontmatter), &fm); err != nil {
		return fmt.Errorf("parsing frontmatter: %w", err)
	}

	// Only treat as TT-SR rule if scope is set
	if fm.Scope == "" {
		return nil
	}

	// Validate scope
	switch fm.Scope {
	case "text", "thinking", "tool_call", "any":
		// valid
	default:
		return fmt.Errorf("invalid scope %q in rule %q", fm.Scope, fm.Name)
	}

	if fm.Condition == "" {
		return fmt.Errorf("rule %q has no condition", fm.Name)
	}

	compiled, err := regexp.Compile(fm.Condition)
	if err != nil {
		return fmt.Errorf("compiling regex for rule %q: %w", fm.Name, err)
	}

	repeat := fm.Repeat
	if repeat == "" {
		repeat = "once"
	}

	rule := &TTSRRule{
		Name:      fm.Name,
		Scope:     fm.Scope,
		Condition: fm.Condition,
		Interrupt: fm.Interrupt,
		Repeat:    repeat,
		Globs:     fm.Globs,
		Content:   strings.TrimSpace(body),
		compiled:  compiled,
	}

	m.rules = append(m.rules, rule)
	return nil
}

// CheckDelta checks a complete response (or streaming delta) against all rules.
// source is "text", "thinking", or "tool_call".
// Returns a slice of matched rules (empty if no match).
func (m *TTSRManager) CheckDelta(source string, delta string, turnNum int) []*TTSRRule {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var matched []*TTSRRule
	for _, rule := range m.rules {
		if !matchesScope(rule.Scope, source) {
			continue
		}
		if !canTrigger(rule, turnNum) {
			continue
		}
		if rule.compiled.MatchString(delta) {
			matched = append(matched, rule)
		}
	}
	return matched
}

// MarkInjected marks a rule as having been injected at the given turn.
func (m *TTSRManager) MarkInjected(ruleName string, turnNum int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, rule := range m.rules {
		if rule.Name == ruleName {
			rule.hasInjected = true
			rule.injectedAt = turnNum
			return
		}
	}
}

// RestoreInjected restores injection state from persistence.
func (m *TTSRManager) RestoreInjected(injected map[string]int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, rule := range m.rules {
		if turn, ok := injected[rule.Name]; ok {
			rule.hasInjected = true
			rule.injectedAt = turn
		}
	}
}

// InjectionState returns the current injection state for persistence.
func (m *TTSRManager) InjectionState() map[string]int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	state := make(map[string]int, len(m.rules))
	for _, rule := range m.rules {
		if rule.hasInjected {
			state[rule.Name] = rule.injectedAt
		}
	}
	return state
}

// HasRules returns true if the manager has any loaded rules.
func (m *TTSRManager) HasRules() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.rules) > 0
}

// Rules returns a copy of the current rules list (for inspection/testing).
func (m *TTSRManager) Rules() []*TTSRRule {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*TTSRRule, len(m.rules))
	copy(out, m.rules)
	return out
}

// canTrigger checks whether a rule is allowed to trigger based on its repeat
// policy and the current turn number.
func canTrigger(rule *TTSRRule, turnNum int) bool {
	switch {
	case rule.Repeat == "" || rule.Repeat == "once":
		return !rule.hasInjected
	case strings.HasPrefix(rule.Repeat, "after-gap:"):
		gapStr := strings.TrimPrefix(rule.Repeat, "after-gap:")
		gap, err := strconv.Atoi(gapStr)
		if err != nil {
			return true // malformed gap, allow trigger
		}
		if !rule.hasInjected {
			return true // never injected before
		}
		return turnNum-rule.injectedAt >= gap
	default:
		return true // unknown repeat policy, allow trigger
	}
}

// matchesScope returns true if the rule scope matches the stream source.
func matchesScope(scope, source string) bool {
	if scope == "any" {
		return true
	}
	return scope == source
}

// splitTTSRFrontmatter splits YAML frontmatter from a markdown body.
// Returns ("", "", nil) when no frontmatter is present.
func splitTTSRFrontmatter(text string) (frontmatter, body string, err error) {
	trimmed := strings.TrimLeft(text, " \t\n\r")
	if !strings.HasPrefix(trimmed, "---") {
		return "", "", nil
	}

	_, after, ok := strings.Cut(trimmed, "---")
	if !ok {
		return "", "", nil
	}

	rest := after
	newlinePos := strings.Index(rest, "\n")
	if newlinePos == -1 {
		return "", "", nil
	}
	rest = rest[newlinePos+1:]

	if strings.HasPrefix(rest, "---") {
		afterClose := rest[3:]
		if idx := strings.Index(afterClose, "\n"); idx >= 0 {
			body = afterClose[idx+1:]
		} else {
			body = ""
		}
		return "", body, nil
	}

	closePos := strings.Index(rest, "\n---")
	if closePos == -1 {
		return "", "", nil
	}

	frontmatter = rest[:closePos]
	afterClose := rest[closePos+4:]
	if idx := strings.Index(afterClose, "\n"); idx >= 0 {
		body = afterClose[idx+1:]
	} else {
		body = ""
	}

	return frontmatter, body, nil
}
