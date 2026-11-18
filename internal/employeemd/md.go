// Package employeemd parses an EMPLOYEE.md file (YAML frontmatter + markdown
// body) into a map[string]any that is byte-compatible with the
// agents.create / agents.update / agents.validate RPC payload. It is a pure
// parse-to-map surface: no daemon, no database, no registry. The
// constitution and goals are passed through verbatim for downstream
// validation (Manager.Hire and agents validate own that).
package employeemd

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Sentinel errors returned by Parse.
var (
	// ErrNoFrontmatter indicates the markdown body has no --- frontmatter.
	ErrNoFrontmatter = errors.New("employeemd: no frontmatter delimiters found")
	// ErrInvalidYAML indicates the frontmatter failed to decode as YAML.
	ErrInvalidYAML = errors.New("employeemd: invalid frontmatter YAML")
	// ErrMissingID indicates the frontmatter did not contain a non-empty id.
	ErrMissingID = errors.New("employeemd: frontmatter 'id' is required")
)

// Parse converts an EMPLOYEE.md file's YAML frontmatter plus markdown body
// into a map[string]any whose JSON encoding is byte-compatible with the
// agents.create / agents.update / agents.validate payload. The entire markdown
// body after the closing frontmatter delimiter becomes "prompt" (TrimSpace'd).
// Unknown top-level frontmatter keys are preserved in the output map for
// forward compatibility. The id frontmatter field is required and must be a
// non-empty string.
func Parse(data []byte) (map[string]any, error) {
	text := string(data)

	frontmatter, body, err := splitFrontmatter(text)
	if err != nil {
		return nil, err
	}

	// Decode the frontmatter into an untyped map, preserving YAML scalar
	// types (ints decode to int, floats to float64) so that json.Marshal
	// emits numeric values rather than strings.
	var fm map[string]any
	if err := yaml.Unmarshal([]byte(frontmatter), &fm); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidYAML, err)
	}

	id, ok := fm["id"].(string)
	if !ok || strings.TrimSpace(id) == "" {
		return nil, ErrMissingID
	}

	out := make(map[string]any, len(fm)+1)
	out["id"] = id

	// Scalar passthrough fields.
	for _, k := range []string{"name", "description", "model", "memory_scope", "executor_agent"} {
		if v, ok := fm[k]; ok {
			out[k] = v
		}
	}

	// Tools: merge additional_tools into tools.
	var tools []any
	if v, ok := fm["tools"]; ok {
		if l, lok := v.([]any); lok {
			tools = append(tools, l...)
		} else {
			tools = append(tools, v)
		}
	}
	if v, ok := fm["additional_tools"]; ok {
		if l, lok := v.([]any); lok {
			tools = append(tools, l...)
		} else {
			tools = append(tools, v)
		}
	}
	if len(tools) > 0 {
		out["tools"] = tools
	}

	// Constraints: canonical map with BotConstraints JSON keys, folded with
	// the agent-style top-level aliases when the canonical key is absent.
	constraints, err := buildConstraints(fm)
	if err != nil {
		return nil, err
	}
	out["constraints"] = constraints

	// Triggers: passthrough verbatim (type, schedule?, topic?,
	// prompt_template?, enabled).
	if v, ok := fm["triggers"]; ok {
		out["triggers"] = v
	}

	// Enabled bool passthrough (only when present).
	if v, ok := fm["enabled"]; ok {
		out["enabled"] = v
	}

	// Constitution and goals passed through verbatim (do NOT merge the
	// constitution's nested constraints with the top-level constraints).
	if v, ok := fm["constitution"]; ok {
		out["constitution"] = v
	}
	if v, ok := fm["goals"]; ok {
		out["goals"] = v
	}

	// Preserve unknown top-level frontmatter keys.
	for k, v := range fm {
		switch k {
		case "id", "name", "description", "model", "memory_scope",
			"tools", "additional_tools", "constraints", "triggers",
			"enabled", "executor_agent", "constitution", "goals",
			// Agent-style aliases are folded into constraints, not emitted
			// at top level.
			"max_iterations", "timeout_seconds":
			continue
		}
		out[k] = v
	}

	out["prompt"] = strings.TrimSpace(body)

	return out, nil
}

// constraintRule maps a canonical frontmatter constraint key to its output
// (BotConstraints JSON) key and applies any value conversion.
type constraintRule struct {
	outKey  string
	convert func(v any) (any, error)
}

// buildConstraints derives the "constraints" output map. Canonical frontmatter
// constraint keys (max_iterations, timeout_seconds, max_tokens_per_turn,
// daily_budget_cents, max_invocations_per_day) take precedence; agent-style
// top-level aliases (max_iterations, timeout_seconds) are folded in only when
// the canonical key is absent. timeout_seconds converts to an integer
// nanosecond value (time.Duration) for the "timeout" JSON key.
func buildConstraints(fm map[string]any) (map[string]any, error) {
	// Canonical map: the top-level frontmatter "constraints" sub-map.
	var canonical map[string]any
	if v, ok := fm["constraints"]; ok {
		canonical, _ = v.(map[string]any) // non-map handled as absent
	}

	rules := map[string]constraintRule{
		"max_iterations":          {outKey: "max_iterations", convert: passThrough},
		"timeout_seconds":         {outKey: "timeout", convert: secondsToDuration},
		"max_tokens_per_turn":     {outKey: "max_tokens_per_turn", convert: passThrough},
		"daily_budget_cents":      {outKey: "daily_budget_cents", convert: passThrough},
		"max_invocations_per_day": {outKey: "max_invocations_per_day", convert: passThrough},
	}

	out := make(map[string]any, len(rules))
	for canonKey, rule := range rules {
		var val any
		if v, ok := canonical[canonKey]; ok {
			val = v
		} else if v, ok := fm[canonKey]; ok {
			val = v
		}
		if val == nil {
			continue
		}
		converted, err := rule.convert(val)
		if err != nil {
			return nil, fmt.Errorf("employeemd: constraints.%s: %w", canonKey, err)
		}
		out[rule.outKey] = converted
	}
	return out, nil
}

func passThrough(v any) (any, error) { return v, nil }

// secondsToDuration converts a YAML integer (seconds) into an integer
// nanosecond value for a time.Duration JSON field (e.g. 300 -> 300000000000).
func secondsToDuration(v any) (any, error) {
	var secs int64
	switch n := v.(type) {
	case int:
		secs = int64(n)
	case int64:
		secs = n
	case uint:
		secs = int64(n)
	case uint64:
		secs = int64(n)
	default:
		return nil, fmt.Errorf("timeout_seconds must be an integer, got %T", v)
	}
	return time.Duration(secs) * time.Second, nil
}

// splitFrontmatter splits YAML frontmatter from the markdown body. The
// frontmatter must be delimited by --- markers. This mirrors the approach in
// internal/agents/parser.go (deliberately not imported: it belongs to the
// unrelated roster-discovery domain, and the duplication is small).
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
