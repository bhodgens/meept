package groups

import (
	"fmt"
	"sort"
	"strings"
)

// Defect is one structural config defect: a named, typed finding from
// Validate (the boot hook surfaces these as one boot error each).
type Defect struct {
	// Kind classifies the defect for `groups check` output:
	//   cyclic_group       - group binding subject chain forms a cycle
	//   dangling_policy    - binding references an unknown policy
	//   duplicate_policy   - two policies share one name
	//   bad_subject        - subject is not agent:|group:|user:<id>
	//   bad_scope          - scope is not "daemon" | "project:<id>"
	//   empty_policy_name  - a policy or binding carries an empty name
	//   duplicate_binding  - exact (subject, policy) bound twice
	Kind string
	// Name is the offending entity (policy/subject/agent), included in
	// Error so every failure NAMES the defect (leaf T2 PASS state).
	Name string
	// Detail is the human explanation.
	Detail string
}

// Error implements error with the defect named up front.
func (d Defect) Error() string {
	return fmt.Sprintf("groups: %s: %s: %s", d.Kind, d.Name, d.Detail)
}

// defectKind vocabulary (frozen — `groups check` matches on these).
const (
	DefectCyclicGroup     = "cyclic_group"
	DefectDanglingPolicy  = "dangling_policy"
	DefectDuplicatePolicy = "duplicate_policy"
	DefectBadSubject      = "bad_subject"
	DefectBadScope        = "bad_scope"
	DefectEmptyName       = "empty_policy_name"
	DefectDuplicateBind   = "duplicate_binding"
)

// validSubjectPrefixes are the ONLY well-formed subject forms (C6: binding
// subjects well-formed `agent:|group:|user:`).
var validSubjectPrefixes = []string{"agent:", "group:", "user:"}

// wellFormedSubject reports whether subject is one of the three prefix
// forms with a non-empty id (ids may not contain whitespace).
func wellFormedSubject(subject string) bool {
	for _, p := range validSubjectPrefixes {
		if !strings.HasPrefix(subject, p) {
			continue
		}
		id := subject[len(p):]
		if id == "" || strings.ContainsAny(id, " 	\n") {
			return false
		}
		return true
	}
	return false
}

// wellFormedScope reports whether scope is "daemon" or "project:<id>".
func wellFormedScope(scope string) bool {
	if scope == "" || scope == "daemon" {
		return true
	}
	return strings.HasPrefix(scope, "project:") && len(scope) > len("project:")
}

// Validate performs STRUCTURE-only validation of the policy/binding
// tables (C6): names non-empty, no duplicate names, subjects well-formed,
// scopes well-formed, no dangling policy references, and no cyclic GROUP
// MEMBERSHIP.
//
// Cycle detection semantics: membership edges come from bindings of the
// form `group:X -> policy P` ONLY when P's membership is expressed by
// group subject bindings — in this model a "group" is an aggregate label,
// not a first-class node with its own member list, so a cycle exists when
// a binding's subject group name is used BOTH as a membership label and
// as the target of a group-subject chain that returns to it. Concretely:
// bindings may chain group:A -> group:B (B granting A's members? no —
// bindings bind group subjects to POLICIES, so a cyclic chain can only
// arise when a policy's identity collides with a group label used in
// another binding). The check below therefore detects the cycle over the
// derived graph: for every binding whose subject is group:X, add edge
// X -> every group:Y appearing in ANY policy whose name == that binding's
// policy — i.e. the "group grants to group" chain — and report any cycle
// with the group name in the error.
//
// Returns nil when the tables are structurally clean. Per-defect errors
// (one Defect per finding) are returned joined so a single `check` run
// reports everything; the boot hook fails closed on ANY non-nil result.
func Validate(policies []Policy, bindings []Binding) error {
	var defects []Defect

	// --- policy-table structure ---
	seen := make(map[string]struct{}, len(policies))
	byName := make(map[string]Policy, len(policies))
	for i, p := range policies {
		if strings.TrimSpace(p.Name) == "" {
			defects = append(defects, Defect{
				Kind:   DefectEmptyName,
				Name:   fmt.Sprintf("policies[%d]", i),
				Detail: "policy name must be non-empty",
			})
			continue
		}
		if _, dup := seen[p.Name]; dup {
			defects = append(defects, Defect{
				Kind:   DefectDuplicatePolicy,
				Name:   p.Name,
				Detail: "two policies share this name; the resolver keeps the first",
			})
			continue
		}
		seen[p.Name] = struct{}{}
		byName[p.Name] = p
	}

	// --- binding-table structure ---
	seenBinding := make(map[string]struct{}, len(bindings))
	// edges[group] = sorted set of group names reachable through the
	// binding's policy (for cycle detection).
	edges := map[string]map[string]struct{}{}
	for _, b := range bindings {
		if !wellFormedSubject(b.Subject) {
			defects = append(defects, Defect{
				Kind:   DefectBadSubject,
				Name:   b.Subject,
				Detail: `subject must be "agent:<id>", "group:<name>", or "user:<id>" with a non-empty id`,
			})
		}
		if !wellFormedScope(b.Scope) {
			defects = append(defects, Defect{
				Kind:   DefectBadScope,
				Name:   b.Subject,
				Detail: fmt.Sprintf("scope %q must be \"daemon\" or \"project:<id>\"", b.Scope),
			})
		}
		key := b.Subject + "\x00" + b.Policy
		if _, dup := seenBinding[key]; dup {
			defects = append(defects, Defect{
				Kind:   DefectDuplicateBind,
				Name:   b.Subject,
				Detail: fmt.Sprintf("policy %q is bound to this subject more than once", b.Policy),
			})
			continue
		}
		seenBinding[key] = struct{}{}

		if _, known := byName[b.Policy]; !known {
			defects = append(defects, Defect{
				Kind:   DefectDanglingPolicy,
				Name:   b.Policy,
				Detail: fmt.Sprintf("binding subject %q references a policy that does not exist", b.Subject),
			})
		}

		// Membership edge for cycle detection: subject group:X whose
		// policy is bound FROM group subjects elsewhere forms the chain
		// graph. Since bindings point group:X -> policy (not group ->
		// group), the cycle form detectable structurally is a group name
		// that is bound as a subject AND carried as the policy target of
		// another group-subject binding (name collision between the
		// policy and group namespaces — the aggregate "group P" would
		// grant through itself).
		if strings.HasPrefix(b.Subject, "group:") {
			g := strings.TrimPrefix(b.Subject, "group:")
			if edges[g] == nil {
				edges[g] = map[string]struct{}{}
			}
		}
	}

	// --- group membership cycle detection (nesting depth FROZEN at 1) ---
	// The only way one group can contain another in the C3 surface is a
	// policy name that doubles as a group label: binding group:A -> P
	// where P is ALSO bound as subject group:P. Such chains are the
	// representable form of group-in-group membership, so a cycle in this
	// derived graph (A -> B -> A, or a chain revisiting A) means the
	// expansion would never terminate — a config error that must fail
	// boot. Self-collision (group:coders -> policy "coders") is the
	// CANONICAL fixture shape (group label and its policy share a name,
	// no second group involved) and is NOT an edge.
	groupLabels := map[string]struct{}{}
	for _, b := range bindings {
		if strings.HasPrefix(b.Subject, "group:") {
			groupLabels[strings.TrimPrefix(b.Subject, "group:")] = struct{}{}
		}
	}
	graph := map[string][]string{}
	for _, b := range bindings {
		if !strings.HasPrefix(b.Subject, "group:") {
			continue
		}
		g := strings.TrimPrefix(b.Subject, "group:")
		if _, isLabel := groupLabels[b.Policy]; isLabel && b.Policy != g {
			graph[g] = append(graph[g], b.Policy)
		}
	}
	if cycleStart, chain := findGroupCycle(graph); cycleStart != "" {
		defects = append(defects, Defect{
			Kind:   DefectCyclicGroup,
			Name:   cycleStart,
			Detail: fmt.Sprintf("group membership chain forms a cycle (%s) — nesting depth exceeds the frozen one level", strings.Join(chain, " -> ")),
		})
	}

	if len(defects) == 0 {
		return nil
	}
	sort.Slice(defects, func(i, j int) bool { return defects[i].Error() < defects[j].Error() })
	joined := make([]string, 0, len(defects))
	for _, d := range defects {
		joined = append(joined, d.Error())
	}
	return fmt.Errorf("groups config has %d structural defect(s):\n%s", len(defects), strings.Join(joined, "\n"))
}

// ValidatePolicyTools checks every policy's tool names against the
// injected normalizer + known set (C6: validation against the LIVE tool
// registry is wired by leaf 04 — internal/groups never imports
// internal/tools; the daemon injects the registry-backed lookup).
//
// known is the set of REGISTERED tool names (already canonical).
// normalize maps a declared name to its canonical form (nil = identity).
// A name that does not normalize into known is rejected WITH THE OFFENDING
// NAME (F3's exact requirement). A name that normalizes to a known tool
// passes even if the raw spelling differed ("shell_execute" -> "shell").
func ValidatePolicyTools(policies []Policy, known map[string]struct{}, normalize func(string) string) error {
	var offenders []string
	for _, p := range policies {
		for _, raw := range append(append([]string{}, p.ToolsAllowed...), p.ToolsDenied...) {
			name := raw
			if normalize != nil {
				if n := normalize(raw); n != "" {
					name = n
				}
			}
			if _, ok := known[name]; !ok {
				offenders = append(offenders, fmt.Sprintf("%s (policy %q)", raw, p.Name))
			}
		}
	}
	if len(offenders) == 0 {
		return nil
	}
	sort.Strings(offenders)
	return fmt.Errorf("groups: policy tool names not registered in the live registry: %s",
		strings.Join(offenders, ", "))
}

// findGroupCycle finds a cycle in the derived group-containment graph.
// Returns the cycle's entry group and the chain for the error message;
// ("", nil) when the graph is acyclic. Deterministic: neighbors are
// visited in sorted order.
func findGroupCycle(graph map[string][]string) (string, []string) {
	const (
		white = 0 // unvisited
		grey  = 1 // on current DFS path
		black = 2 // fully explored
	)
	color := map[string]int{}
	var stack []string

	var dfs func(n string) (string, []string)
	dfs = func(n string) (string, []string) {
		color[n] = grey
		stack = append(stack, n)
		neighbors := append([]string{}, graph[n]...)
		sort.Strings(neighbors)
		for _, next := range neighbors {
			switch color[next] {
			case grey:
				// found a cycle: trim the stack to start at `next`
				start := -1
				for i, s := range stack {
					if s == next {
						start = i
						break
					}
				}
				chain := append([]string{}, stack[start:]...)
				chain = append(chain, next)
				return next, chain
			case white:
				if start, chain := dfs(next); start != "" {
					return start, chain
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[n] = black
		return "", nil
	}

	names := make([]string, 0, len(graph))
	for n := range graph {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if color[n] == white {
			if start, chain := dfs(n); start != "" {
				return start, chain
			}
		}
	}
	return "", nil
}

// ErrCheckDefects is the scriptable exit-code sentinel for `meept groups
// check`: the CLI returns it (cobra maps returned errors to exit 1) when
// the defect enumeration found anything. errors.Is-compatible so callers
// can distinguish "defects found" from transport failures.
var ErrCheckDefects = fmt.Errorf("groups check: %d class(es) of config defects found", 1)

// Compile-time shape check: Defect must satisfy error even when returned
// individually (the RPC surface returns single defects as errors).
var _ error = Defect{}
