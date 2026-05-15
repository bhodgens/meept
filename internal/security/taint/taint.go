// Package taint implements information flow tracking for security.
//
// It provides a lattice-based taint propagation model that prevents tainted
// values from flowing into sensitive sinks without explicit declassification.
// This guards against prompt injection, data exfiltration, and other
// confused-deputy attacks.
//
// Inspired by OpenFang: https://github.com/adamtornhill/openfang
package taint

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
)

// TaintLabel represents a classification label applied to data flowing through the system.
type TaintLabel string //nolint:revive // stutter is intentional

const (
	// TaintNone indicates data is clean and trusted.
	TaintNone TaintLabel = ""
	// TaintUserInput indicates data that originated from direct user input.
	TaintUserInput TaintLabel = "user_input"
	// TaintSecret indicates secret material (API keys, tokens, passwords).
	TaintSecret TaintLabel = "secret"
	// TaintUntrusted indicates data produced by an untrusted / sandboxed agent.
	TaintUntrusted TaintLabel = "untrusted"
	// TaintExternal indicates data that originated from an external network request.
	TaintExternal TaintLabel = "external"
	// TaintShell indicates data that will be executed in a shell context.
	TaintShell TaintLabel = "shell"
)

// String returns the string representation of the taint label.
func (l TaintLabel) String() string {
	if l == TaintNone {
		return "none"
	}
	return string(l)
}

// TaintedValue is a value annotated with taint labels tracking its provenance.
type TaintedValue struct {
	// Value is the actual string payload.
	Value string
	// Taints is the set of taint labels currently attached.
	Taints []TaintLabel
	// Source is a human-readable description of where this value originated.
	Source string
}

// NewTaintedValue creates a new tainted value with the given labels.
func NewTaintedValue(value string, taints []TaintLabel, source string) *TaintedValue {
	return &TaintedValue{
		Value:  value,
		Taints: deduplicateLabels(taints),
		Source: source,
	}
}

// Clean creates a clean (untainted) value with no labels.
func Clean(value, source string) *TaintedValue {
	return &TaintedValue{
		Value:  value,
		Taints: []TaintLabel{},
		Source: source,
	}
}

// IsTainted returns true if this value carries any taint labels at all.
func (v *TaintedValue) IsTainted() bool {
	return len(v.Taints) > 0
}

// HasLabel returns true if the value has the specified taint label.
func (v *TaintedValue) HasLabel(label TaintLabel) bool {
	return slices.Contains(v.Taints, label)
}

// Merge combines the taint labels from another value into this one.
//
// This is used when two values are concatenated or otherwise combined;
// the result must carry the union of both label sets.
func (v *TaintedValue) Merge(other *TaintedValue) {
	merged := make(map[TaintLabel]struct{})

	for _, t := range v.Taints {
		merged[t] = struct{}{}
	}
	for _, t := range other.Taints {
		merged[t] = struct{}{}
	}

	v.Taints = make([]TaintLabel, 0, len(merged))
	for t := range merged {
		v.Taints = append(v.Taints, t)
	}
}

// Declassify removes a specific label from this value.
//
// This is an explicit security decision -- the caller is asserting that
// the value has been sanitised or that the label is no longer relevant.
func (v *TaintedValue) Declassify(label TaintLabel) {
	newTaints := make([]TaintLabel, 0, len(v.Taints))
	for _, t := range v.Taints {
		if t != label {
			newTaints = append(newTaints, t)
		}
	}
	v.Taints = newTaints
}

// deduplicateLabels removes duplicate taint labels.
func deduplicateLabels(labels []TaintLabel) []TaintLabel {
	seen := make(map[TaintLabel]struct{})
	result := make([]TaintLabel, 0, len(labels))

	for _, l := range labels {
		if _, exists := seen[l]; !exists {
			seen[l] = struct{}{}
			result = append(result, l)
		}
	}
	return result
}

// TaintSink represents a destination that restricts which taint labels may flow into it.
type TaintSink struct { //nolint:revive // stutter is intentional
	// Name is the human-readable name of the sink (e.g., "shell_exec").
	Name string
	// BlockedLabels are labels that are NOT allowed to reach this sink.
	BlockedLabels []TaintLabel
}

// ShellExecSink returns a sink for shell command execution.
// It blocks external network data, untrusted agent data, and user input
// to prevent injection.
func ShellExecSink() *TaintSink {
	return &TaintSink{
		Name: "shell_exec",
		BlockedLabels: []TaintLabel{
			TaintExternal,
			TaintUntrusted,
			TaintUserInput,
		},
	}
}

// NetFetchSink returns a sink for outbound network fetches.
// It blocks secrets to prevent data exfiltration.
func NetFetchSink() *TaintSink {
	return &TaintSink{
		Name: "net_fetch",
		BlockedLabels: []TaintLabel{
			TaintSecret,
		},
	}
}

// AgentMessageSink returns a sink for sending messages to another agent.
// It blocks secrets from being transmitted.
func AgentMessageSink() *TaintSink {
	return &TaintSink{
		Name: "agent_message",
		BlockedLabels: []TaintLabel{
			TaintSecret,
		},
	}
}

// TaintViolationError describes a taint policy violation: a labelled value
// tried to reach a sink that blocks that label.
type TaintViolationError struct { //nolint:revive // stutter with package name is intentional for API clarity
	// Label is the offending label.
	Label TaintLabel
	// SinkName is the sink that rejected the value.
	SinkName string
	// Source is the source of the tainted value.
	Source string
	// Value is a truncated version of the offending value.
	Value string
}

// Error returns the error message for the violation.
func (v TaintViolationError) Error() string {
	return fmt.Sprintf("taint violation: label '%s' from source '%s' is not allowed to reach sink '%s'",
		v.Label, v.Source, v.SinkName)
}

// Tracker tracks taint labels across operations.
type Tracker struct {
	mu        sync.RWMutex
	logger    *slog.Logger
	variables map[string]*TaintedValue
	contexts  []string // Stack for nested scopes
}

// NewTracker creates a new taint tracker.
func NewTracker(logger *slog.Logger) *Tracker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Tracker{
		logger:    logger,
		variables: make(map[string]*TaintedValue),
		contexts:  make([]string, 0, 10),
	}
}

// MarkUserInput marks a value as originating from user input.
func (t *Tracker) MarkUserInput(value, source string) *TaintedValue {
	t.mu.Lock()
	defer t.mu.Unlock()

	tv := NewTaintedValue(value, []TaintLabel{TaintUserInput}, source)
	t.logMark("user_input", source, value)
	return tv
}

// MarkSecret marks a value as containing secret material.
func (t *Tracker) MarkSecret(value, source string) *TaintedValue {
	t.mu.Lock()
	defer t.mu.Unlock()

	tv := NewTaintedValue(value, []TaintLabel{TaintSecret}, source)
	t.logMark("secret", source, value)
	return tv
}

// MarkUntrusted marks a value as originating from an untrusted source.
func (t *Tracker) MarkUntrusted(value, source string) *TaintedValue {
	t.mu.Lock()
	defer t.mu.Unlock()

	tv := NewTaintedValue(value, []TaintLabel{TaintUntrusted}, source)
	t.logMark("untrusted", source, value)
	return tv
}

// MarkExternal marks a value as originating from external network data.
func (t *Tracker) MarkExternal(value, source string) *TaintedValue {
	t.mu.Lock()
	defer t.mu.Unlock()

	tv := NewTaintedValue(value, []TaintLabel{TaintExternal}, source)
	t.logMark("external", source, value)
	return tv
}

// logMark logs a taint marking operation.
func (t *Tracker) logMark(label, source, value string) {
	truncated := value
	if len(truncated) > 80 {
		truncated = truncated[:80] + "..."
	}
	t.logger.Debug("taint marked",
		"label", label,
		"source", source,
		"value", truncated,
	)
}

// Store stores a tainted value under a variable name.
func (t *Tracker) Store(name string, value *TaintedValue) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.variables[name] = value
	t.logger.Debug("taint stored", "variable", name, "taints", labelStrings(value.Taints))
}

// Retrieve retrieves a tainted value by variable name.
// Returns nil if the variable doesn't exist.
func (t *Tracker) Retrieve(name string) *TaintedValue {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.variables[name]
}

// Propagate merges taint labels from multiple values.
// The result carries the union of all input label sets.
func (t *Tracker) Propagate(values ...*TaintedValue) *TaintedValue {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(values) == 0 {
		return Clean("", "propagation")
	}

	mergedLabels := make(map[TaintLabel]struct{})
	var combinedValue strings.Builder
	var combinedSource strings.Builder

	first := true
	for _, v := range values {
		if v == nil {
			continue
		}

		for _, label := range v.Taints {
			mergedLabels[label] = struct{}{}
		}

		if !first {
			combinedValue.WriteString(" ")
			combinedSource.WriteString("+")
		}
		combinedValue.WriteString(v.Value)
		combinedSource.WriteString(v.Source)
		first = false
	}

	labels := make([]TaintLabel, 0, len(mergedLabels))
	for label := range mergedLabels {
		labels = append(labels, label)
	}

	return NewTaintedValue(combinedValue.String(), labels, combinedSource.String())
}

// CheckSink checks whether a value is safe to flow into the given sink.
// Returns nil if safe, or a TaintViolationError describing the conflict.
func (t *Tracker) CheckSink(value *TaintedValue, sink *TaintSink) *TaintViolationError {
	if value == nil {
		return nil
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	for _, label := range value.Taints {
		if slices.Contains(sink.BlockedLabels, label) {
			truncated := value.Value
			if len(truncated) > 100 {
				truncated = truncated[:100] + "..."
			}
			return &TaintViolationError{
				Label:    label,
				SinkName: sink.Name,
				Source:   value.Source,
				Value:    truncated,
			}
		}
	}
	return nil
}

// CheckShellCommand checks if a shell command should be blocked by taint tracking.
// Returns a violation if blocked, nil if allowed.
func (t *Tracker) CheckShellCommand(command string) *TaintViolationError {
	sink := ShellExecSink()

	// First check if the command has suspicious patterns
	for _, pattern := range SuspiciousPatterns {
		if strings.Contains(command, pattern) {
			// If it looks suspicious, assume it might be externally tainted
			tv := NewTaintedValue(command, []TaintLabel{TaintExternal}, "shell_command")
			return t.CheckSink(tv, sink)
		}
	}

	return nil
}

// CheckWebFetch checks if a URL should be blocked by taint tracking.
// Returns a violation if blocked, nil if allowed.
func (t *Tracker) CheckWebFetch(url string) *TaintViolationError {
	sink := NetFetchSink()

	// Check for exfiltration patterns
	lowerURL := strings.ToLower(url)
	exfilPatterns := []string{
		"api_key=",
		"apikey=",
		"token=",
		"secret=",
		"password=",
		"authorization:",
	}

	for _, pattern := range exfilPatterns {
		if strings.Contains(lowerURL, pattern) {
			tv := NewTaintedValue(url, []TaintLabel{TaintSecret}, "web_fetch")
			return t.CheckSink(tv, sink)
		}
	}

	return nil
}

// labelStrings converts a slice of TaintLabel to a slice of string.
func labelStrings(labels []TaintLabel) []string {
	result := make([]string, len(labels))
	for i, l := range labels {
		result[i] = l.String()
	}
	return result
}

// SuspiciousPatterns are patterns that may indicate injected external data
// or malicious shell commands.
var SuspiciousPatterns = []string{
	"curl ",
	"| sh",
	"| bash",
	"base64 -d",
	"$(curl",
	"`curl",
	patternEval,
	"wget ",
	"$(wget",
	"`wget",
}
