package agent

import (
	"testing"

	"github.com/caimlas/meept/internal/groups"
	"github.com/caimlas/meept/pkg/security"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// groupsResolverWith builds a resolver over one policy bound to one group.
func groupsResolverWith(policy groups.Policy, defaultManaged bool) *groups.Resolver {
	return groups.NewResolver(
		[]groups.Policy{policy},
		[]groups.Binding{{Subject: "group:coders", Policy: policy.Name}},
		defaultManaged,
		nil,
	)
}

// registryWithResolver is a minimal AgentRegistry carrying a tool registry
// and a groups resolver (the daemon wires both through RegistryConfig; the
// tests construct the struct directly like tool_scope_test.go).
func registryWithResolver(reg *PlaceholderToolRegistry, resolver *groups.Resolver) *AgentRegistry {
	return &AgentRegistry{tools: reg, groupsResolver: resolver}
}

// groupedCoderSpec is a coder-like spec carrying the `groups:` membership
// (frontmatter Groups field, leaf 03-membership/01).
func groupedCoderSpec() *AgentSpec {
	spec := coderLikeSpec()
	spec.ID = "aastub"
	spec.Groups = []string{"coders"}
	return spec
}

// ---------------------------------------------------------------------------
// T1: loop resolution record wired at loop creation
// ---------------------------------------------------------------------------

// TestGroupResolution_RegistryLoopCarriesRecord pins T1 site 1 (registry
// loops): a grouped agent's loop carries a MANAGED record; an ungrouped
// agent under default_managed=false carries the unmanaged record (compat).
func TestGroupResolution_RegistryLoopCarriesRecord(t *testing.T) {
	resolver := groupsResolverWith(groups.Policy{
		Name:         "coders",
		ToolsAllowed: []string{"file_read"},
		ToolsDenied:  []string{"shell"},
	}, false)

	reg := &AgentRegistry{
		tools:          registryWithGrants(groupedCoderSpec()),
		groupsResolver: resolver,
		logger:         discardLogger(),
	}
	reg.specs = map[string]*AgentSpec{"aastub": groupedCoderSpec()}

	loop := reg.createLoop(groupedCoderSpec())
	res := loop.GroupResolution()
	if res == nil {
		t.Fatal("grouped loop carries NO resolution record")
	}
	if !res.Managed {
		t.Fatal("grouped loop record must be Managed=true")
	}
	if len(res.AllowedTools) != 1 || res.AllowedTools[0] != "file_read" {
		t.Fatalf("record allowed=%v, want [file_read]", res.AllowedTools)
	}
	if len(res.DeniedTools) != 1 || res.DeniedTools[0] != "shell" {
		t.Fatalf("record denied=%v, want [shell]", res.DeniedTools)
	}
	// The executor consults the SAME record (call-time gate input).
	if eres := loop.executor.GroupResolution(); eres == nil || !eres.Managed {
		t.Fatal("executor did not inherit the loop's resolution record")
	}
}

// TestGroupResolution_UngroupedUnmanagedNilRecord pins the D7 compat leg:
// no groups config (nil resolver) => no record, byte-identical legacy.
func TestGroupResolution_UngroupedUnmanagedNilRecord(t *testing.T) {
	spec := coderLikeSpec()
	reg := &AgentRegistry{
		tools:  registryWithGrants(spec),
		logger: discardLogger(),
	}
	reg.specs = map[string]*AgentSpec{spec.ID: spec}

	loop := reg.createLoop(spec)
	if res := loop.GroupResolution(); res != nil {
		t.Fatalf("ungrouped unmanaged loop must carry no record, got %+v", res)
	}
}

// TestGroupResolution_DefaultManagedRoutesUngrouped pins D7's managed side:
// default_managed=true routes an UNGROUPED agent through the default policy
// (record present, Managed=true).
func TestGroupResolution_DefaultManagedRoutesUngrouped(t *testing.T) {
	resolver := groups.NewResolver(
		[]groups.Policy{{Name: "default", ToolsAllowed: []string{"file_read"}}},
		nil,
		true,
		nil,
	)
	spec := coderLikeSpec()
	reg := &AgentRegistry{
		tools:          registryWithGrants(spec),
		groupsResolver: resolver,
		logger:         discardLogger(),
	}
	reg.specs = map[string]*AgentSpec{spec.ID: spec}

	loop := reg.createLoop(spec)
	res := loop.GroupResolution()
	if res == nil || !res.Managed {
		t.Fatal("default_managed ungrouped loop must carry a managed record")
	}
}

// ---------------------------------------------------------------------------
// T2: dispatch intersection in filterTools
// ---------------------------------------------------------------------------

// TestDispatchIntersection_DenyRemovesFromDefsAndGet pins F1's dispatch
// leg: the denied tool is absent from GetDefinitions AND Get()=nil, while
// the allowed tool keeps working.
func TestDispatchIntersection_DenyRemovesFromDefsAndGet(t *testing.T) {
	resolver := groupsResolverWith(groups.Policy{
		Name:         "coders",
		ToolsAllowed: []string{"file_read"},
		ToolsDenied:  []string{"shell"},
	}, false)

	spec := groupedCoderSpec()
	reg := registryWithResolver(registryWithGrants(spec), resolver)

	filtered := reg.filterTools(spec)
	if filtered == nil {
		t.Fatal("filterTools returned nil")
	}
	if filtered.Get("shell") != nil {
		t.Fatal("denied tool must be uncallable through Get after intersection")
	}
	for _, def := range filtered.GetDefinitions() {
		if def.Function.Name == "shell" {
			t.Fatal("denied tool must be absent from GetDefinitions after intersection")
		}
	}
	if filtered.Get("file_read") == nil {
		t.Fatal("policy-allowed tool must survive the intersection")
	}
}

// TestDispatchIntersection_DenyAllExplicitEmpty pins D8: a managed record
// with an EMPTY effective allowlist denies EVERYTHING — the legacy
// FilteredToolRegistry empty=all semantics must never fire for a managed
// subject.
func TestDispatchIntersection_DenyAllExplicitEmpty(t *testing.T) {
	resolver := groupsResolverWith(groups.Policy{
		Name:         "locked",
		ToolsAllowed: nil,
	}, false)

	spec := groupedCoderSpec()
	reg := registryWithResolver(registryWithGrants(spec), resolver)

	filtered := reg.filterTools(spec)
	fr, ok := filtered.(*FilteredToolRegistry)
	if !ok {
		t.Fatalf("want *FilteredToolRegistry, got %T", filtered)
	}
	if !fr.denyAll {
		t.Fatal("managed empty allowlist must produce the explicit deny-all marker")
	}
	if filtered.Get("file_read") != nil {
		t.Fatal("deny-all: Get must return nil for every tool")
	}
	if len(filtered.GetDefinitions()) != 0 {
		t.Fatal("deny-all: GetDefinitions must be empty")
	}
}

// TestDispatchIntersection_UngroupedUnaffected pins D7 at the dispatch
// gate: with no resolver (or an unmanaged record), filterTools is
// byte-identical to the legacy behavior (full grant set offered).
func TestDispatchIntersection_UngroupedUnaffected(t *testing.T) {
	spec := coderLikeSpec()
	reg := &AgentRegistry{tools: registryWithGrants(spec)} // no resolver

	filtered := reg.filterTools(spec)
	if len(filtered.GetDefinitions()) == 0 {
		t.Fatal("ungrouped agent lost its tools with no groups resolver wired")
	}
	if filtered.Get("shell") == nil {
		t.Fatal("ungrouped agent lost its granted shell tool")
	}
}

// ---------------------------------------------------------------------------
// T3: call-time gate
// ---------------------------------------------------------------------------

// TestCallTimeGroupGate_DenyShortCircuits pins the call-time deny: a
// managed record with shell denied turns a base-allow verdict into a
// group_deny deny.
func TestCallTimeGroupGate_DenyShortCircuits(t *testing.T) {
	e := &Executor{security: nil}
	e.groupResolution = &groups.Resolved{
		Managed:      true,
		AllowedTools: []string{"file_read"},
		DeniedTools:  []string{"shell"},
	}
	// Base pipeline allowed the call; the group gate must deny it.
	base := allowResult()
	got := e.applyGroupGate(base, "shell_execute", "shell")
	if got.Allowed {
		t.Fatal("group deny must short-circuit the call")
	}
	if got.RuleSource != "group_deny" {
		t.Fatalf("rule_source=%q, want group_deny", got.RuleSource)
	}
}

// TestCallTimeGroupGate_NotInAllowlistDenied pins the allowlist-ceiling
// leg (C3): a managed agent calling a tool OUTSIDE the record's allowlist
// is denied even when the base pipeline allows it.
func TestCallTimeGroupGate_NotInAllowlistDenied(t *testing.T) {
	e := &Executor{}
	e.groupResolution = &groups.Resolved{
		Managed:      true,
		AllowedTools: []string{"file_read"},
	}
	got := e.applyGroupGate(allowResult(), "shell_execute", "shell")
	if got.Allowed || got.RuleSource != "group_deny" {
		t.Fatalf("outside-allowlist call must deny with group_deny, got %+v", got)
	}
	// Grant-vocabulary normalization: a call for the RAW grant token
	// resolves to the same verdict (shell_execute -> shell).
	got2 := e.applyGroupGate(allowResult(), "shell_execute", "shell_execute")
	if got2.Allowed || got2.RuleSource != "group_deny" {
		t.Fatalf("grant-vocabulary call must deny identically, got %+v", got2)
	}
}

// TestCallTimeGroupGate_AllowStamped pins the group_allow stamp: an allowed
// call inside the ceiling carries rule_source=group_allow.
func TestCallTimeGroupGate_AllowStamped(t *testing.T) {
	e := &Executor{}
	e.groupResolution = &groups.Resolved{
		Managed:      true,
		AllowedTools: []string{"file_read"},
	}
	got := e.applyGroupGate(allowResult(), "file_read", "file_read")
	if !got.Allowed {
		t.Fatalf("in-ceiling call must stay allowed, got %+v", got)
	}
	if got.RuleSource != "group_allow" {
		t.Fatalf("rule_source=%q, want group_allow", got.RuleSource)
	}
}

// TestCallTimeGroupGate_BaseDenyKeepsSource pins the precedence detail: a
// deny from the base pipeline (fence, financial, ...) keeps ITS rule
// source — the group gate only ever narrows.
func TestCallTimeGroupGate_BaseDenyKeepsSource(t *testing.T) {
	e := &Executor{}
	e.groupResolution = &groups.Resolved{
		Managed:      true,
		AllowedTools: []string{"file_read"},
	}
	base := allowResult()
	base.Allowed = false
	base.RuleSource = "fence"
	got := e.applyGroupGate(base, "file_write", "file_write")
	if got.RuleSource != "fence" {
		t.Fatalf("base-pipeline deny must keep its rule source, got %q", got.RuleSource)
	}
}

// TestCallTimeGroupGate_UnmanagedSkips pins D7 at the call-time gate: a
// nil (or unmanaged) record leaves every verdict untouched.
func TestCallTimeGroupGate_UnmanagedSkips(t *testing.T) {
	e := &Executor{} // nil record
	base := allowResult()
	if got := e.applyGroupGate(base, "shell", "shell"); got.RuleSource != base.RuleSource {
		t.Fatal("nil record must skip the gate entirely")
	}
	e.groupResolution = &groups.Resolved{Managed: false}
	if got := e.applyGroupGate(base, "shell", "shell"); got.RuleSource != base.RuleSource {
		t.Fatal("unmanaged record must skip the gate entirely")
	}
}

// allowResult is a base-pipeline allow verdict.
func allowResult() security.CheckResult {
	return security.CheckResult{
		Allowed:       true,
		Reason:        "Permitted",
		RuleSource:    "base_rule",
		EffectiveRisk: security.RiskLow,
	}
}

// discardLogger is defined in turn_watchdog_test.go.
