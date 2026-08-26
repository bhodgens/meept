// Package security: declarative shell command permission tables.
//
// The permission table provides prefix-keyed allow/ask/deny policy that is
// evaluated BEFORE tirith pattern scanning. Tirith detects dangerous
// PATTERNS; the table expresses operator POLICY ("git push always asks;
// rm -rf always denies"). A table match short-circuits; an "allow" still
// runs tirith as defense-in-depth.
package security

import (
	"fmt"
	"sort"
	"strings"
)

// Shell rule actions.
const (
	ShellActionAllow = "allow"
	ShellActionAsk   = "ask"
	ShellActionDeny  = "deny"
)

// Preset names for [security.shell_permissions].
const (
	PresetWorkspace = "workspace" // default
	PresetReadonly  = "readonly"
	PresetDanger    = "danger"
)

// ShellRule is a single declarative shell permission entry.
type ShellRule struct {
	Action string // "allow" | "ask" | "deny"
}

// tableRule is a compiled prefix rule. Matching is purely token-based —
// no regex. A rule's prefix may contain "|" separating segments that must
// appear in order (e.g. "curl | sh"), and its final token may end with "="
// marking a value-carrying prefix (e.g. "dd if=" matches "dd if=/dev/zero").
type tableRule struct {
	segs     [][]string // lowercased token segments, in order
	raw      string     // original lowercased prefix (returned as matchedPrefix)
	action   string
	catchAll bool
}

// PermissionTable evaluates shell commands against sorted prefix rules.
// It is immutable after construction and safe for concurrent use; Evaluate
// performs no I/O and holds no locks (mutexio-friendly).
type PermissionTable struct {
	rules []tableRule // most specific first; catch-all last
}

// validShellAction reports whether s is one of the three supported actions.
func validShellAction(s string) bool {
	switch s {
	case ShellActionAllow, ShellActionAsk, ShellActionDeny:
		return true
	}
	return false
}

// normalizeTokens splits into whitespace-delimited lowercase tokens,
// collapsing runs of whitespace. No regex, no quote evaluation — mirrors
// tokenizeCommand-style splitting in fence.go.
func normalizeTokens(s string) []string {
	fields := strings.Fields(strings.ToLower(s))
	return fields
}

// compilePrefix builds a tableRule from a prefix string. ok=false for empty
// prefixes or invalid actions.
func compilePrefix(prefix, action string) (tableRule, bool) {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	if prefix == "" || !validShellAction(action) {
		return tableRule{}, false
	}
	r := tableRule{
		raw:      prefix,
		action:   action,
		catchAll: prefix == "*",
	}
	if r.catchAll {
		r.segs = [][]string{{"*"}}
		return r, true
	}
	for _, seg := range strings.Split(prefix, "|") {
		tokens := strings.Fields(seg)
		if len(tokens) == 0 {
			continue // tolerate stray pipes like "a || b"
		}
		r.segs = append(r.segs, tokens)
	}
	if len(r.segs) == 0 {
		return tableRule{}, false
	}
	return r, true
}

// NewPermissionTable builds a table from prefix -> rule. Entries with invalid
// or empty actions are silently dropped (use BuildPermissionTable for strict
// validation). Rules are ordered most-specific-first so longer prefixes win;
// the "*" catch-all always evaluates last.
func NewPermissionTable(rules map[string]ShellRule) *PermissionTable {
	table := &PermissionTable{}
	for prefix, rule := range rules {
		if tr, ok := compilePrefix(prefix, rule.Action); ok {
			table.rules = append(table.rules, tr)
		}
	}
	sort.SliceStable(table.rules, func(i, j int) bool {
		si := table.ruleSpecificity(table.rules[i])
		sj := table.ruleSpecificity(table.rules[j])
		if si != sj {
			return si > sj
		}
		return table.rules[i].raw < table.rules[j].raw
	})
	return table
}

// ruleSpecificity ranks rules: catch-all is least specific; otherwise more
// tokens = more specific.
func (p *PermissionTable) ruleSpecificity(r tableRule) int {
	if r.catchAll {
		return -1
	}
	n := 0
	for _, seg := range r.segs {
		n += len(seg)
	}
	return n
}

// segMatchesTokens reports whether seg matches tokens at offset i, returning
// the next offset. The final segment token may carry a trailing '=' meaning
// it matches any command token beginning with that stem.
func segMatchesAt(seg, tokens []string, i int) (int, bool) {
	if i+len(seg) > len(tokens) {
		return i, false
	}
	for j, pt := range seg {
		ct := tokens[i+j]
		last := j == len(seg)-1 && strings.HasSuffix(pt, "=")
		if last {
			if !strings.HasPrefix(ct, pt) { // "if=" matches "if=/dev/zero"
				return i, false
			}
		} else if ct != pt {
			return i, false
		}
	}
	return i + len(seg), true
}

// matchSegments walks tokens left-to-right requiring each segment to occur
// consecutively, in order (segments need not be adjacent — "curl | sh"
// matches "curl http://x | sh").
func matchSegments(segs [][]string, tokens []string) bool {
	pos := 0
	for _, seg := range segs {
		matched := false
		for start := pos; start <= len(tokens)-len(seg); start++ {
			if next, ok := segMatchesAt(seg, tokens, start); ok {
				pos = next
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// Evaluate checks command against the table.
//
// Returns the decision ("allow"|"ask"|"deny"), the matched prefix (raw form),
// and ok=false when no rule matches (caller falls through to existing path).
//
// Matching is token-based and case-insensitive:
//   - longest prefix wins (catch-all "*" evaluated last);
//   - word boundaries are enforced ("rm -rf" does not match "rm -rfx");
//   - a single-token base command also matches same-family extensions
//     ("mkfs" matches "mkfs.ext4") but NOT arbitrary continuations;
//   - a final token ending with '=' is a value-carrying prefix
//     ("dd if=" matches "dd if=/dev/zero");
//   - '|' in a prefix separates segments that must appear in order
//     ("curl | sh" matches "curl http://x | sh").
//
// No regex, no I/O.
func (p *PermissionTable) Evaluate(command string) (decision, matchedPrefix string, ok bool) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", "", false
	}

	tokens := normalizeTokens(command)
	for _, rule := range p.rules {
		if rule.catchAll {
			return rule.action, "*", true
		}
		// Base-command extension match: single-token single-segment rules
		// match "cmd.something" (e.g. "mkfs" -> "mkfs.ext4 ...").
		if len(rule.segs) == 1 && len(rule.segs[0]) == 1 &&
			len(tokens) >= 1 && strings.HasPrefix(tokens[0], rule.segs[0][0]+".") {
			return rule.action, rule.raw, true
		}
		if matchSegments(rule.segs, tokens) {
			return rule.action, rule.raw, true
		}
	}
	return "", "", false
}

// workspacePresetDeny/Ask and readonlyPresetDeny/Ask are the documented
// preset contents.
var (
	workspacePresetDeny = []string{"rm -rf", "rm -fr", "mkfs", "dd if="}
	workspacePresetAsk  = []string{"sudo", "git push", "docker system prune", "chmod 777", "curl | sh", "bash -c", "sh -c"}
	readonlyPresetDeny  = []string{"rm -rf", "rm -fr", "mkfs", "dd if=", "git commit", "npm publish"}
	readonlyPresetAsk   = []string{"*"} // everything else asks
)

// BuildPermissionTable constructs a PermissionTable from a named preset plus
// optional user rules that override/extend the preset. Unknown presets and
// malformed actions return errors (fail-closed at config load time).
//
// Presets:
//   - workspace (default): denies destructive prefixes; asks for sudo,
//     git push, docker system prune, chmod 777, curl|sh, bash -c, sh -c.
//   - readonly: ask-by-default (catch-all) except the workspace deny list
//     plus git commit and npm publish which deny.
//   - danger: empty — every command falls through to existing evaluation.
func BuildPermissionTable(preset string, rules map[string]ShellRule) (*PermissionTable, error) {
	merged := map[string]ShellRule{}

	switch preset {
	case PresetWorkspace:
		for _, p := range workspacePresetDeny {
			merged[p] = ShellRule{Action: ShellActionDeny}
		}
		for _, p := range workspacePresetAsk {
			merged[p] = ShellRule{Action: ShellActionAsk}
		}
	case PresetReadonly:
		for _, p := range readonlyPresetDeny {
			merged[p] = ShellRule{Action: ShellActionDeny}
		}
		for _, p := range readonlyPresetAsk {
			merged[p] = ShellRule{Action: ShellActionAsk}
		}
	case PresetDanger:
		// empty — all commands fall through
	default:
		return nil, fmt.Errorf("shell_permissions: unknown preset %q (want %q, %q, or %q)",
			preset, PresetWorkspace, PresetReadonly, PresetDanger)
	}

	// User rules override/extend the preset.
	for prefix, rule := range rules {
		if !validShellAction(rule.Action) {
			return nil, fmt.Errorf("shell_permissions: prefix %q has invalid action %q (want allow, ask, or deny)", prefix, rule.Action)
		}
		merged[strings.TrimSpace(prefix)] = ShellRule{Action: strings.ToLower(rule.Action)}
	}

	return NewPermissionTable(merged), nil
}
