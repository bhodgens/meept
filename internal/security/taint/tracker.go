// Package taint implements information flow tracking for security.
package taint

import (
	"log/slog"
	"slices"
)

// TaintLogger is an interface for logging taint violations.
type TaintLogger interface { //nolint:revive // stutter is intentional
	LogViolation(violation *TaintViolationError)
}

// DefaultTaintLogger is the default implementation of TaintLogger.
type DefaultTaintLogger struct {
	logger *slog.Logger
}

// NewDefaultTaintLogger creates a new default taint logger.
func NewDefaultTaintLogger(logger *slog.Logger) *DefaultTaintLogger {
	if logger == nil {
		logger = slog.Default()
	}
	return &DefaultTaintLogger{logger: logger}
}

// LogViolation logs a taint violation.
func (l *DefaultTaintLogger) LogViolation(violation *TaintViolationError) {
	l.logger.Warn("taint violation",
		"label", violation.Label.String(),
		"sink", violation.SinkName,
		"source", violation.Source,
		"value", violation.Value,
	)
}

// ExtendedTracker extends Tracker with context management and enhanced logging.
type ExtendedTracker struct {
	*Tracker
	logger      TaintLogger
	contexts    []*Context
	maxContexts int
}

// Context represents a taint tracking scope.
type Context struct {
	Name      string
	Variables map[string]*TaintedValue
	Parent    *Context
}

// NewExtendedTracker creates a new extended taint tracker.
func NewExtendedTracker(logger *slog.Logger) *ExtendedTracker {
	var tlog TaintLogger
	if logger != nil {
		tlog = NewDefaultTaintLogger(logger)
	} else {
		tlog = NewDefaultTaintLogger(slog.Default())
	}

	return &ExtendedTracker{
		Tracker:     NewTracker(logger),
		logger:      tlog,
		contexts:    make([]*Context, 0, 10),
		maxContexts: 100,
	}
}

// PushContext creates a new nested context scope.
// All subsequent stores will be scoped to this context until PopContext is called.
func (t *ExtendedTracker) PushContext(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var parent *Context
	if len(t.contexts) > 0 {
		parent = t.contexts[len(t.contexts)-1]
	}

	ctx := &Context{
		Name:      name,
		Variables: make(map[string]*TaintedValue),
		Parent:    parent,
	}

	t.contexts = append(t.contexts, ctx)
	t.Tracker.logger.Debug("taint context pushed", "name", name, "depth", len(t.contexts))
}

// PopContext removes the current context scope.
// Returns true if a context was removed, false if there were no contexts to pop.
func (t *ExtendedTracker) PopContext() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(t.contexts) == 0 {
		return false
	}

	ctx := t.contexts[len(t.contexts)-1]
	t.contexts = t.contexts[:len(t.contexts)-1]

	t.Tracker.logger.Debug("taint context popped", "name", ctx.Name, "remaining", len(t.contexts))
	return true
}

// CurrentContext returns the current context, or nil if no contexts are active.
func (t *ExtendedTracker) CurrentContext() *Context {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if len(t.contexts) == 0 {
		return nil
	}
	return t.contexts[len(t.contexts)-1]
}

// StoreWithContext stores a tainted value in the current context scope.
func (t *ExtendedTracker) StoreWithContext(name string, value *TaintedValue) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Inline context lookup — CurrentContext() acquires its own RLock,
	// but we already hold the write lock here, so a recursive lock would
	// deadlock. Access the contexts slice directly instead.
	var ctx *Context
	if len(t.contexts) > 0 {
		ctx = t.contexts[len(t.contexts)-1]
	}

	if ctx != nil {
		ctx.Variables[name] = value
		t.Tracker.logger.Debug("taint stored in context",
			"context", ctx.Name,
			"variable", name,
			"taints", labelStrings(value.Taints),
		)
	} else {
		// Fall back to global storage
		t.variables[name] = value
		t.Tracker.logger.Debug("taint stored globally",
			"variable", name,
			"taints", labelStrings(value.Taints),
		)
	}
}

// RetrieveFromContext retrieves a tainted value from the current context,
// falling back to parent contexts and then global storage.
func (t *ExtendedTracker) RetrieveFromContext(name string) *TaintedValue {
	t.mu.RLock()
	defer t.mu.RUnlock()

	// Search contexts from innermost to outermost
	for _, v := range slices.Backward(t.contexts) {
		ctx := v
		if value, ok := ctx.Variables[name]; ok {
			return value
		}
	}

	// Fall back to global storage
	return t.variables[name]
}

// Clear removes all stored variables and contexts.
func (t *ExtendedTracker) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.variables = make(map[string]*TaintedValue)
	t.contexts = make([]*Context, 0, t.maxContexts)
	t.Tracker.logger.Debug("taint tracker cleared")
}

// ClearContexts removes all contexts but preserves global variables.
func (t *ExtendedTracker) ClearContexts() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.contexts = make([]*Context, 0, t.maxContexts)
	t.Tracker.logger.Debug("taint contexts cleared")
}

// CheckSinkWithLogging checks a value against a sink and logs any violations.
// Returns true if safe, false if a violation was found and logged.
func (t *ExtendedTracker) CheckSinkWithLogging(value *TaintedValue, sink *TaintSink) bool {
	violation := t.CheckSink(value, sink)
	if violation != nil {
		t.logger.LogViolation(violation)
		return false
	}
	return true
}

// DeepCopy creates a deep copy of a tainted value.
func DeepCopy(value *TaintedValue) *TaintedValue {
	if value == nil {
		return nil
	}

	taints := make([]TaintLabel, len(value.Taints))
	copy(taints, value.Taints)

	return &TaintedValue{
		Value:  value.Value,
		Taints: taints,
		Source: value.Source,
	}
}

// FilterValues removes any values from the slice that have the specified taint label.
func FilterValues(values []*TaintedValue, label TaintLabel) []*TaintedValue {
	filtered := make([]*TaintedValue, 0, len(values))
	for _, v := range values {
		if v != nil && !v.HasLabel(label) {
			filtered = append(filtered, v)
		}
	}
	return filtered
}

// SanitizableTracker extends the tracker with sanitization capabilities.
type SanitizableTracker struct {
	*ExtendedTracker
	sanitizerFunc func(string) string
}

// NewSanitizableTracker creates a tracker with a custom sanitization function.
func NewSanitizableTracker(logger *slog.Logger, sanitizer func(string) string) *SanitizableTracker {
	return &SanitizableTracker{
		ExtendedTracker: NewExtendedTracker(logger),
		sanitizerFunc:   sanitizer,
	}
}

// Sanitize applies the sanitization function and creates a clean value.
// The original taint labels are removed, indicating the data has been sanitized.
func (t *SanitizableTracker) Sanitize(value *TaintedValue) *TaintedValue {
	if value == nil {
		return nil
	}

	cleaned := value.Value
	if t.sanitizerFunc != nil {
		cleaned = t.sanitizerFunc(cleaned)
	}

	return Clean(cleaned, value.Source+":sanitized")
}

// DeclassifyWithReason removes a specific label with an auditable reason.
func (t *SanitizableTracker) DeclassifyWithReason(value *TaintedValue, label TaintLabel, reason string) *TaintedValue {
	if value == nil {
		return nil
	}

	deepCopy := DeepCopy(value)
	deepCopy.Declassify(label)

	t.Tracker.logger.Info("taint declassified",
		"label", label.String(),
		"source", value.Source,
		"reason", reason,
	)

	return deepCopy
}
