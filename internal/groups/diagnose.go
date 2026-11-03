package groups

// diagnose.go — the report-only diagnosis engine (agent-groups plan tree,
// docs/plans/20261030-agent-groups/06-diagnosis.md, R4/R5).
//
// One engine answers the two diagnosis questions, shared by EVERY surface
// (RPC groups.check/explain-attribution, the meept groups CLI, doctor):
//
//	explain — "group X denies shell for agent Y: which binding did that?"
//	          per grant/deny entry, the binding + policy that produced it.
//	check   — enumerate every R5 config-defect class with names
//	          (scriptable: callers map DefectCount>0 to exit 1).
//
// THE SINGLE-RESOLUTION-PATH RULE (C2/R4, the drift trap): explain and
// check NEVER reimplement precedence. Every resolution goes through the
// same Resolve / ResolveDefaultManaged / Validate / ValidatePolicyTools
// core the daemon gates enforce. This file contains zero precedence
// logic; it only assembles inputs, attributes the outcome, and renders
// findings. Report-only: nothing here mutates state.

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// normalizeDiagnosisToolName applies the agent-grant vocabulary
// normalization at the diagnosis boundary. The canonical mapping lives in
// internal/agent (CanonicalToolName); this package must not import
// internal/agent (C6 import discipline), so the documented grant-side
// precedent is mirrored here — used ONLY to attribute entries and to
// validate names against an injected known set. Callers that hold the
// real normalizer inject it via DiagnosisOptions.NormalizeToolName
// (the daemon wiring); this fallback is for CLI/doctor paths that
// resolve from config alone.
func normalizeDiagnosisToolName(name string) string {
	switch name {
	case "shell_execute":
		return "shell"
	case "web_search_execute":
		return "web_search"
	case "file_read_text":
		return "file_read"
	case "file_write_text":
		return "file_write"
	case "file_edit_text":
		return "file_edit"
	default:
		return name
	}
}

// ---------------------------------------------------------------------------
// Defect codes (stable --json keys). Structural classes reuse the frozen
// groups.Defect vocabulary (DefectCyclicGroup, DefectDanglingPolicy, ...);
// the diagnosis-local classes use the codes below so the CLI/doctor can
// match on codes without colliding with the core's kinds.
// ---------------------------------------------------------------------------

const (
	// DefectUnknownGroup: an agent declares a group that no binding uses
	// as its subject (C4 — membership with no policy effect).
	DefectUnknownGroup = "unknown_group"
	// DefectDenyAllowConflict: one policy denies a tool it also allows
	// (legal: deny wins, D4 — reported so the author knows the allow is
	// dead config).
	DefectDenyAllowConflict = "deny_allow_conflict"
	// DefectManagedEmptyAllowlist: a policy yields Managed with an empty
	// allow union — agents under it are denied EVERYTHING (D8 surprise);
	// also reported when default_managed=true has no "default" policy.
	DefectManagedEmptyAllowlist = "managed_empty_allowlist"
	// DefectAgentDeniedEverything: an agent's effective record is Managed
	// with zero allowed tools despite a direct/group binding.
	DefectAgentDeniedEverything = "agent_denied_everything"
	// DefectAgentDefaultManaged: default_managed=true and the agent has
	// no membership and no binding — it runs under the fail-closed
	// `default` policy (reported, not fatal).
	DefectAgentDefaultManaged = "agent_default_managed"
	// DefectUnregisteredTool: a policy names a tool outside the injected
	// known set (C6; inert when no known set is available).
	DefectUnregisteredTool = "unregistered_tool"
	// DefectActiveOverride: an ACTIVE override grants a tool beyond group
	// policy (leaf 07's C8 surface, counted through the reader seam).
	DefectActiveOverride = "active_override"
	// DefectExpiredOverride: an override row is past expiry.
	DefectExpiredOverride = "expired_override"
	// DefectOverridesReader: the overrides reader itself errored.
	DefectOverridesReader = "overrides_reader"
)

// ---------------------------------------------------------------------------
// explain types (stable --json shapes, T4)
// ---------------------------------------------------------------------------

// GrantEntry is one attributed grant/deny line: the tool name plus the
// binding and policy that produced it (R4 verbatim anchor).
type GrantEntry struct {
	// Tool is the canonical (registered-form) tool name.
	Tool string `json:"tool"`
	// Effect is "allow" | "deny".
	Effect string `json:"effect"`
	// Policy is the policy whose allow/deny list carried this tool
	// ("default" on the default_managed route).
	Policy string `json:"policy"`
	// BindingSubject is the subject ("agent:<id>" | "group:<name>") of the
	// binding that brought the policy in.
	BindingSubject string `json:"binding_subject"`
	// BindingScope is the binding's scope ("daemon" | "project:<id>" |
	// "daemon" for the default_managed synthetic row).
	BindingScope string `json:"binding_scope"`
	// Reason names non-binding routes ("derived from policy layering").
	// Empty for ordinary binding-attributed entries.
	Reason string `json:"reason,omitempty"`
}

// ExplainSource is one binding that selected the subject.
type ExplainSource struct {
	Subject string `json:"subject"`
	Policy  string `json:"policy"`
	Scope   string `json:"scope"`
	// Note explains synthetic rows ("default_managed routed..."). Empty
	// for ordinary config bindings.
	Note string `json:"note,omitempty"`
}

// ExplainResult is the groups.explain answer (stable --json keys, T4).
type ExplainResult struct {
	// AgentID is the subject the resolution ran for.
	AgentID string `json:"agent_id"`
	// Groups are the agent's effective frontmatter memberships (sorted).
	Groups []string `json:"groups"`
	// Managed mirrors Resolved.Managed (group policy was ACTIVE).
	Managed bool `json:"managed"`
	// AllowedTools is the effective allow set (sorted; empty managed =
	// deny-all, D8).
	AllowedTools []string `json:"allowed_tools"`
	// DeniedTools is the deny set that bit this agent (sorted).
	DeniedTools []string `json:"denied_tools"`
	// Grants attributes EVERY entry (allow AND deny) to its binding +
	// policy — the R4 contract ("which binding did that?").
	Grants []GrantEntry `json:"grants"`
	// Bindings lists every binding that selected this subject (including
	// the default_managed synthetic row when it routed).
	Bindings []ExplainSource `json:"bindings"`
	// Resolved carries the shared resolution record verbatim.
	Resolved Resolved `json:"resolved"`
}

// boundPolicy pairs one selected policy with the binding that selected it.
type boundPolicy struct {
	policy  Policy
	binding Binding
}

// scopeOrDefault renders an empty binding scope as "daemon" (the effective
// scope for diagnosis surfaces).
func scopeOrDefault(scope string) string {
	if scope == "" {
		return "daemon"
	}
	return scope
}

// sortedDiagStrings returns a sorted, de-duplicated, nil-when-empty copy.
func sortedDiagStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// Explain resolves one agent through Resolve (the SAME core the gates
// make — zero duplicated precedence) and attributes each allow/deny entry
// to the binding + policy that produced it.
//
// defaultManaged mirrors the daemon resolver's D7 routing: when the pure
// Resolve yields the unmanaged record and defaultManaged is true, the
// resolver would route through the fail-closed `default` policy — explain
// shows that record, not the unmanaged one.
func Explain(specTools []string, agentID string, frontmatterGroups []string, policies []Policy, bindings []Binding, opts *ResolveOptions, defaultManaged bool) *ExplainResult {
	// The resolution itself must run with the same normalizer the gates
	// use: a non-nil ResolveOptions carrying a nil NormalizeToolName
	// would skip normalization inside resolveLayered (its nil guard is
	// per-field), so the engine wraps the injected normalizer with the
	// diagnosis fallback here.
	resolveOpts := opts
	if opts == nil || opts.NormalizeToolName == nil {
		resolveOpts = &ResolveOptions{NormalizeToolName: normalizeDiagnosisToolName}
		if opts != nil {
			resolveOpts.ExtraAllows = opts.ExtraAllows
		}
	}
	norm := resolveOpts.NormalizeToolName

	res := Resolve(specTools, agentID, frontmatterGroups, policies, bindings, resolveOpts)
	if !res.Managed && defaultManaged {
		res = ResolveDefaultManaged(specTools, policies, resolveOpts)
	}

	// --- binding selection view ---
	subject := "agent:" + agentID
	member := map[string]struct{}{}
	for _, gm := range frontmatterGroups {
		member[gm] = struct{}{}
	}
	sources := []ExplainSource{}
	for _, b := range bindings {
		match := b.Subject == subject
		if !match && strings.HasPrefix(b.Subject, "group:") {
			_, match = member[strings.TrimPrefix(b.Subject, "group:")]
		}
		if match {
			sources = append(sources, ExplainSource{Subject: b.Subject, Policy: b.Policy, Scope: scopeOrDefault(b.Scope)})
		}
	}
	if res.Managed && len(sources) == 0 {
		sources = append(sources, ExplainSource{
			Subject: subject,
			Policy:  "default",
			Scope:   "daemon",
			Note:    "default_managed routed (no matching binding)",
		})
	}

	// --- per-entry attribution ---
	selected := selectedPolicies(agentID, frontmatterGroups, bindings, policies)
	grants := make([]GrantEntry, 0, len(res.AllowedTools)+len(res.DeniedTools))
	grants = append(grants, attributeEntries("allow", res.AllowedTools, selected, norm, res.Managed)...)
	grants = append(grants, attributeEntries("deny", res.DeniedTools, selected, norm, res.Managed)...)
	sort.Slice(grants, func(i, j int) bool {
		if grants[i].Effect != grants[j].Effect {
			return grants[i].Effect < grants[j].Effect
		}
		return grants[i].Tool < grants[j].Tool
	})

	return &ExplainResult{
		AgentID:      agentID,
		Groups:       sortedDiagStrings(frontmatterGroups),
		Managed:      res.Managed,
		AllowedTools: res.AllowedTools,
		DeniedTools:  res.DeniedTools,
		Grants:       grants,
		Bindings:     sources,
		Resolved:     res,
	}
}

// selectedPolicies reconstructs the selection Resolve made (agent-subject
// bindings first, then one-level group bindings; dangling refs skipped —
// the dangling case is Check's defect, not explain's). Bookkeeping for
// attribution labels only: any drift between it and Resolve surfaces as a
// "derived from policy layering" reason, which is itself the diagnostic.
func selectedPolicies(agentID string, frontmatterGroups []string, bindings []Binding, policies []Policy) []boundPolicy {
	byName := PoliciesByID(policies)
	subject := "agent:" + agentID
	member := map[string]struct{}{}
	for _, gm := range frontmatterGroups {
		member[gm] = struct{}{}
	}
	var out []boundPolicy
	seen := map[string]struct{}{}
	for _, b := range bindings {
		selected := b.Subject == subject
		if !selected && strings.HasPrefix(b.Subject, "group:") {
			_, selected = member[strings.TrimPrefix(b.Subject, "group:")]
		}
		if !selected {
			continue
		}
		if _, dup := seen[b.Policy]; dup {
			continue
		}
		p, ok := byName[b.Policy]
		if !ok {
			continue // dangling: Resolve skips it; Check reports it
		}
		seen[b.Policy] = struct{}{}
		out = append(out, boundPolicy{policy: p, binding: b})
	}
	return out
}

// attributeEntries builds attributed entries for one effect over the named
// tools. Attribution: the FIRST selected policy (binding order) whose
// allow/deny list carries the tool (after normalization) owns the entry;
// a tool no list mentions arrives via layering interactions and is
// reported with the derived reason. No selected policies (the default
// route) names the synthetic default row.
func attributeEntries(effect string, tools []string, selected []boundPolicy, norm func(string) string, managed bool) []GrantEntry {
	out := make([]GrantEntry, 0, len(tools))
	for _, raw := range tools {
		entry := GrantEntry{Tool: raw, Effect: effect}
		attributed := false
		for _, bp := range selected {
			var list []string
			if effect == "allow" {
				list = bp.policy.ToolsAllowed
			} else {
				list = bp.policy.ToolsDenied
			}
			for _, declared := range list {
				if norm(declared) == raw {
					entry.Policy = bp.policy.Name
					entry.BindingSubject = bp.binding.Subject
					entry.BindingScope = scopeOrDefault(bp.binding.Scope)
					attributed = true
					break
				}
			}
			if attributed {
				break
			}
		}
		if !attributed {
			entry.Reason = "derived from policy layering"
			if managed && len(selected) == 0 {
				entry.Policy = "default"
				entry.BindingScope = "daemon"
			}
		}
		out = append(out, entry)
	}
	return out
}

// ---------------------------------------------------------------------------
// check types (stable --json shapes, T4) + the R5 defect enumeration
// ---------------------------------------------------------------------------

// CheckFinding is one enumerated defect (stable --json keys).
type CheckFinding struct {
	// Code is one of the defect codes (or a frozen groups.Defect kind).
	Code string `json:"code"`
	// Name is the offending entity (group/policy/agent/tool/subject).
	Name string `json:"name"`
	// Detail is the human explanation.
	Detail string `json:"detail"`
}

// CheckResult is the groups.check answer (stable --json keys, T4).
// Callers exit 0 when DefectCount==0 and 1 otherwise (scriptable).
type CheckResult struct {
	// Ok is true when no defects were found.
	Ok bool `json:"ok"`
	// DefectCount is len(Defects) — the exit-code decision number.
	DefectCount int `json:"defect_count"`
	// Defects enumerates every finding (sorted: code, then name, then
	// detail — deterministic output for goldens and scripts).
	Defects []CheckFinding `json:"defects"`
	// AgentsChecked is the number of enabled agents whose effective
	// posture was evaluated.
	AgentsChecked int `json:"agents_checked"`
	// Overrides is the override summary from the reader seam (nil reader:
	// empty summary with a note).
	Overrides OverrideSummary `json:"overrides"`
}

// OverrideSummary reports the leaf-07 override surface through the seam.
type OverrideSummary struct {
	Active  int    `json:"active"`
	Expired int    `json:"expired"`
	Total   int    `json:"total"`
	Note    string `json:"note,omitempty"`
}

// OverridesReader is the leaf-07 seam: check counts ACTIVE and expired
// overrides THROUGH this interface, never by reading the overrides store
// itself (coordination boundary with leaf 07-overrides, which owns
// internal/groups/overrides.go). A nil reader reports zero overrides
// with a note; when leaf 07 lands its reader it satisfies this
// structurally — no change needed here.
type OverridesReader interface {
	// ActiveOverrides returns the currently-active override rows.
	ActiveOverrides(ctx context.Context) ([]OverrideRow, error)
	// ExpiredOverrides returns the expired rows.
	ExpiredOverrides(ctx context.Context) ([]OverrideRow, error)
}

// OverrideRow is the minimal row shape the seam reports (identity + the
// allowed tool — enough for named check findings).
type OverrideRow struct {
	// Subject is the override's subject ("agent:<id>" | "group:<name>").
	Subject string `json:"subject"`
	// Tool is the override-allowed tool name.
	Tool string `json:"tool"`
	// ExpiresAt is the expiry instant (RFC3339; empty = no expiry).
	ExpiresAt string `json:"expires_at,omitempty"`
}

// DiagnosisAgent is one discovered agent as the engine sees it (pure —
// no internal/agents import; the caller maps its roster into this).
type DiagnosisAgent struct {
	ID      string
	Groups  []string
	Tools   []string // spec grant set (raw spellings; normalized internally)
	Enabled bool
}

// CheckOptions carries the injected seams. Nil is valid.
type CheckOptions struct {
	// NormalizeToolName is the live registry-backed normalizer (daemon
	// wiring injects agent.CanonicalToolName). Nil = the diagnosis
	// fallback vocabulary.
	NormalizeToolName func(string) string
}

// Check enumerates ALL eight R5 defect classes with names. Every
// resolution goes through Resolve / ResolveDefaultManaged / Validate /
// ValidatePolicyTools — never a parallel implementation.
//
// R5 coverage map (frozen checklist):
//
//  1. cyclic group membership        -> groups.Validate (DefectCyclicGroup)
//  2. dangling policy ref            -> groups.Validate (DefectDanglingPolicy)
//  3. dangling tool name             -> ValidatePolicyTools vs knownTools (C6)
//  4. unknown group in frontmatter   -> DefectUnknownGroup
//  5. deny+allow same tool           -> DefectDenyAllowConflict (deny wins)
//  6. Managed+empty allowlist        -> DefectManagedEmptyAllowlist (+ no
//     "default" policy under default_managed)
//  7. agent in no group + default_managed -> DefectAgentDefaultManaged
//     (plus DefectAgentDeniedEverything for the bound-but-empty shape)
//  8. active/expired overrides       -> reader seam (DefectActiveOverride /
//     DefectExpiredOverride)
func Check(policies []Policy, bindings []Binding, agentsIn []DiagnosisAgent, knownTools map[string]struct{}, defaultManaged bool, overrides OverridesReader, opts *CheckOptions) *CheckResult {
	resolveOpts := &ResolveOptions{}
	if opts != nil && opts.NormalizeToolName != nil {
		resolveOpts.NormalizeToolName = opts.NormalizeToolName
	} else {
		resolveOpts.NormalizeToolName = normalizeDiagnosisToolName
	}
	norm := resolveOpts.NormalizeToolName

	var findings []CheckFinding
	add := func(code, name, detail string) {
		findings = append(findings, CheckFinding{Code: code, Name: name, Detail: detail})
	}

	// --- structural classes through the shared groups.Validate ---
	findings = append(findings, enumerateStructural(policies, bindings)...)

	// --- R5: dangling tool name (C6, injected knowledge) ---
	if len(knownTools) > 0 {
		for _, p := range policies {
			for _, raw := range append(append([]string{}, p.ToolsAllowed...), p.ToolsDenied...) {
				if _, ok := knownTools[norm(raw)]; !ok {
					add(DefectUnregisteredTool, raw,
						fmt.Sprintf("policy %q names tool %q that is not registered in the live tool registry", p.Name, raw))
				}
			}
		}
	}

	// --- R5: unknown group in frontmatter (C4) ---
	groupLabels := map[string]struct{}{}
	for _, b := range bindings {
		if strings.HasPrefix(b.Subject, "group:") {
			groupLabels[strings.TrimPrefix(b.Subject, "group:")] = struct{}{}
		}
	}
	for _, a := range agentsIn {
		for _, gm := range a.Groups {
			if _, ok := groupLabels[gm]; !ok {
				add(DefectUnknownGroup, gm,
					fmt.Sprintf("agent %q declares group %q in frontmatter but no binding uses group:%s as a subject (membership has no policy effect)", a.ID, gm, gm))
			}
		}
	}

	// --- R5: deny+allow same tool in one policy (deny wins — report) ---
	for _, p := range policies {
		allowSet := map[string]struct{}{}
		for _, a := range p.ToolsAllowed {
			allowSet[norm(a)] = struct{}{}
		}
		seen := map[string]struct{}{}
		for _, d := range p.ToolsDenied {
			n := norm(d)
			if _, both := allowSet[n]; both {
				if _, dup := seen[n]; dup {
					continue
				}
				seen[n] = struct{}{}
				add(DefectDenyAllowConflict, n,
					fmt.Sprintf("policy %q both allows and denies tool %q; deny wins (D4) — the allow entry is dead config", p.Name, n))
			}
		}
	}

	// --- R5: Managed + empty allowlist (policies that deny everything) ---
	for _, p := range policies {
		if len(p.ToolsAllowed) == 0 && len(p.ToolsDenied) > 0 {
			add(DefectManagedEmptyAllowlist, p.Name,
				fmt.Sprintf("policy %q allows nothing and denies %d tool(s): every agent under it will be denied EVERYTHING (deny-all posture)", p.Name, len(p.ToolsDenied)))
		}
	}

	// --- default policy present when default_managed=true ---
	if defaultManaged {
		found := false
		for _, p := range policies {
			if p.Name == "default" {
				found = true
				break
			}
		}
		if !found {
			add(DefectManagedEmptyAllowlist, "default",
				`default_managed=true but no policy named "default" exists: ungrouped agents resolve to the deny-everything record`)
		}
	}

	// --- per-agent effective posture (via the shared Resolve) ---
	agentsChecked := 0
	for _, a := range agentsIn {
		if !a.Enabled {
			continue
		}
		agentsChecked++
		res := Resolve(a.Tools, a.ID, a.Groups, policies, bindings, resolveOpts)
		if !res.Managed && defaultManaged {
			res = ResolveDefaultManaged(a.Tools, policies, resolveOpts)
		}
		if res.Managed && len(res.AllowedTools) == 0 {
			directOrGroup := false
			subject := "agent:" + a.ID
			for _, b := range bindings {
				if b.Subject == subject {
					directOrGroup = true
					break
				}
				if strings.HasPrefix(b.Subject, "group:") {
					for _, gm := range a.Groups {
						if strings.TrimPrefix(b.Subject, "group:") == gm {
							directOrGroup = true
						}
					}
				}
			}
			if directOrGroup {
				add(DefectAgentDeniedEverything, a.ID,
					"agent resolves MANAGED with an empty effective allowlist: it will be denied EVERYTHING (check its bound policies' allow lists)")
			} else {
				add(DefectAgentDefaultManaged, a.ID,
					`agent has no group membership and no direct binding under default_managed=true: it will run under the fail-closed "default" policy`)
			}
		}
	}

	// --- R5: active/expired overrides (C8, through the reader seam) ---
	ov := OverrideSummary{}
	if overrides != nil {
		activeRows, aerr := overrides.ActiveOverrides(context.Background())
		expiredRows, eerr := overrides.ExpiredOverrides(context.Background())
		if aerr != nil || eerr != nil {
			add(DefectOverridesReader, "overrides",
				fmt.Sprintf("overrides reader errored (active: %v, expired: %v); override state not counted", aerr, eerr))
		} else {
			ov.Active = len(activeRows)
			ov.Expired = len(expiredRows)
			ov.Total = ov.Active + ov.Expired
			for _, row := range expiredRows {
				add(DefectExpiredOverride, row.Subject,
					fmt.Sprintf("override for tool %q is expired (expires_at=%s): remove it from group_overrides.json5", row.Tool, row.ExpiresAt))
			}
			for _, row := range activeRows {
				add(DefectActiveOverride, row.Subject,
					fmt.Sprintf("active override grants tool %q beyond group policy (expires_at=%s)", row.Tool, row.ExpiresAt))
			}
		}
	} else {
		ov.Note = "overrides reader not wired (leaf 07-overrides owns group_overrides.json5); zero overrides assumed"
	}

	sort.Slice(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Detail < b.Detail
	})

	return &CheckResult{
		Ok:            len(findings) == 0,
		DefectCount:   len(findings),
		Defects:       findings,
		AgentsChecked: agentsChecked,
		Overrides:     ov,
	}
}

// enumerateStructural re-runs the shared Validate per defect candidate so
// each structural finding is individually named (the core owns every
// rule; this only splits its joined error back into named rows).
func enumerateStructural(policies []Policy, bindings []Binding) []CheckFinding {
	var out []CheckFinding

	// Policy-shaped defects: each policy probed alone.
	policyIndex := map[string]int{}
	for i, p := range policies {
		policyIndex[p.Name] = i
		if Validate([]Policy{p}, nil) == nil {
			continue
		}
		if strings.TrimSpace(p.Name) == "" {
			out = append(out, CheckFinding{
				Code:   DefectEmptyName,
				Name:   fmt.Sprintf("policies[%d]", i),
				Detail: "policy name must be non-empty",
			})
			continue
		}
		// A single valid-named policy failing alone means a duplicate
		// name (two policies share it).
		out = append(out, CheckFinding{
			Code:   DefectDuplicatePolicy,
			Name:   p.Name,
			Detail: "two policies share this name; the resolver keeps the first",
		})
	}
	_ = policyIndex

	// Binding-shaped defects: each binding probed against a clean base.
	for _, b := range bindings {
		// The probe base carries THIS binding's policy when it exists, so
		// the dangling-policy class cannot fire on the probe itself; a
		// failing probe then isolates subject/scope/duplicate shape.
		probeBase := []Policy{{Name: "__diag_probe__"}}
		known := false
		for _, p := range policies {
			if p.Name == b.Policy {
				probeBase = append(probeBase, p)
				known = true
				break
			}
		}
		if Validate(probeBase, []Binding{b}) == nil {
			continue
		}
		switch {
		case !wellFormedSubject(b.Subject):
			out = append(out, CheckFinding{
				Code:   DefectBadSubject,
				Name:   b.Subject,
				Detail: `subject must be "agent:<id>", "group:<name>", or "user:<id>" with a non-empty id`,
			})
		case !wellFormedScope(b.Scope):
			out = append(out, CheckFinding{
				Code:   DefectBadScope,
				Name:   b.Subject,
				Detail: fmt.Sprintf("scope %q must be \"daemon\" or \"project:<id>\"", b.Scope),
			})
		case strings.TrimSpace(b.Policy) == "":
			out = append(out, CheckFinding{
				Code:   DefectEmptyName,
				Name:   b.Subject,
				Detail: "binding policy name must be non-empty",
			})
		case !known:
			// Well-formed binding whose policy is absent from the table:
			// the dangling policy reference (R5 class 2).
			out = append(out, CheckFinding{
				Code:   DefectDanglingPolicy,
				Name:   b.Policy,
				Detail: fmt.Sprintf("binding subject %q references a policy that does not exist", b.Subject),
			})
		default:
			// Known policy, well-formed binding, still failing: the
			// duplicate-binding class (same subject+policy twice).
			out = append(out, CheckFinding{
				Code:   DefectDuplicateBind,
				Name:   b.Subject,
				Detail: fmt.Sprintf("policy %q is bound to this subject more than once", b.Policy),
			})
		}
	}

	// Cycle detection needs the WHOLE table: when the full-table Validate
	// still fails after every single-entity defect is excluded, the
	// residual is the cross-binding cycle (R5 class 1).
	if Validate(policies, bindings) == nil {
		return out
	}
	allSingleClean := true
	for _, b := range bindings {
		// Mirror the probe shape used above: base carries the binding's
		// own policy, so a clean single binding probes clean here too.
		probeBase := []Policy{}
		for _, p := range policies {
			if p.Name == b.Policy {
				probeBase = append(probeBase, p)
				break
			}
		}
		if Validate(probeBase, []Binding{b}) != nil {
			allSingleClean = false
			break
		}
	}
	if allSingleClean {
		for _, p := range policies {
			if strings.TrimSpace(p.Name) == "" {
				allSingleClean = false
				break
			}
			if Validate([]Policy{p}, nil) != nil {
				allSingleClean = false
				break
			}
		}
	}
	if allSingleClean {
		if err := Validate(policies, bindings); err != nil {
			// Name the cycle entry from the joined message: the core's
			// Defect.Error renders "groups: cyclic_group: <name>: ...".
			name := "group-chain"
			detail := err.Error()
			for _, d := range splitDefectLines(detail) {
				if strings.Contains(d, DefectCyclicGroup) {
					name = cyclicNameFromLine(d, name)
					detail = d
					break
				}
			}
			out = append(out, CheckFinding{
				Code:   DefectCyclicGroup,
				Name:   name,
				Detail: detail,
			})
		}
	}
	return out
}

// splitDefectLines splits a joined Validate error into its per-defect
// lines (each line after the first is one "groups: <kind>: ..." entry).
func splitDefectLines(joined string) []string {
	var lines []string
	for _, line := range strings.Split(joined, "\n") {
		if strings.HasPrefix(line, "groups: ") {
			lines = append(lines, line)
		}
	}
	return lines
}

// cyclicNameFromLine extracts the cycle entry group from one defect line
// ("groups: cyclic_group: a: group membership chain ...").
func cyclicNameFromLine(line, fallback string) string {
	parts := strings.Split(line, ":")
	// ["groups", " cyclic_group", " a", " group membership chain ..."]
	if len(parts) >= 3 {
		if n := strings.TrimSpace(parts[2]); n != "" {
			return n
		}
	}
	return fallback
}
