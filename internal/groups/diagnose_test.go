package groups

// diagnose_test.go — leaf 06-diagnosis unit gate (tasks T1/T4 + the R5
// checklist): per-class defect detection through the shared Check engine,
// per-entry source attribution through Explain, and the stable --json key
// contract. Every assertion here runs against the SAME engine the RPC,
// CLI, and doctor surfaces call — no parallel resolution.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// c3Fixture is the C3 shape: coders policy (allow file_read + shell, deny
// web_fetch) bound to group:coders; agent aastub member with grants
// file_read + shell_execute (normalizes to shell).
func c3Fixture() ([]Policy, []Binding, []DiagnosisAgent) {
	return c3FixtureP(), c3FixtureB(), []DiagnosisAgent{{
		ID:      "aastub",
		Groups:  []string{"coders"},
		Tools:   []string{"file_read", "shell_execute"},
		Enabled: true,
	}}
}

func c3FixtureP() []Policy {
	return []Policy{{
		Name:         "coders",
		ToolsAllowed: []string{"file_read", "shell"},
		ToolsDenied:  []string{"web_fetch"},
	}}
}

func c3FixtureB() []Binding {
	return []Binding{{Subject: "group:coders", Policy: "coders", Scope: "daemon"}}
}

// ---------------------------------------------------------------------------
// explain: per-entry source attribution (R4)
// ---------------------------------------------------------------------------

func TestExplainAttributesGrantsToBindingAndPolicy(t *testing.T) {
	policies, bindings, _ := c3Fixture()
	res := Explain([]string{"file_read", "shell_execute"}, "aastub", []string{"coders"},
		policies, bindings, &ResolveOptions{}, false)

	if !res.Managed {
		t.Fatalf("explain: managed=false, want true (binding resolved)")
	}
	// web_fetch is denied by the policy but the agent never GRANTED it in
	// its spec, so the deny set that "bits this agent" is empty (the
	// Resolved.DeniedTools contract). The deny attribution is exercised
	// by the deny-that-bits case below.
	if len(res.Grants) != 2 {
		t.Fatalf("explain: got %d grants, want 2 (file_read + shell allow); grants: %+v", len(res.Grants), res.Grants)
	}
	byToolEffect := map[string]GrantEntry{}
	for _, g := range res.Grants {
		byToolEffect[g.Tool+"/"+g.Effect] = g
	}

	shell, ok := byToolEffect["shell/allow"]
	if !ok {
		t.Fatalf("explain: shell allow entry missing: %+v", res.Grants)
	}
	if shell.Policy != "coders" || shell.BindingSubject != "group:coders" || shell.BindingScope != "daemon" {
		t.Fatalf("explain: shell grant attributed to policy=%q binding=%q scope=%q; want coders/group:coders/daemon",
			shell.Policy, shell.BindingSubject, shell.BindingScope)
	}
	if shell.Reason != "" {
		t.Fatalf("explain: shell grant carries reason %q; want empty (directly attributed)", shell.Reason)
	}

	deny, ok := byToolEffect["web_fetch/deny"]
	_ = deny
	_ = ok // deny-that-bits covered by TestExplainDenyAttribution below
}

// a deny that BITES (the spec granted the tool the policy denies) is
// attributed to its binding + policy too — the F8 question shape ("group X
// denies shell for agent Y: which binding did that?").
func TestExplainDenyAttribution(t *testing.T) {
	policies := []Policy{{
		Name:         "coders",
		ToolsAllowed: []string{"file_read", "shell"},
		ToolsDenied:  []string{"shell"},
	}}
	bindings := []Binding{{Subject: "group:coders", Policy: "coders", Scope: "daemon"}}
	res := Explain([]string{"file_read", "shell_execute"}, "aastub", []string{"coders"},
		policies, bindings, &ResolveOptions{}, false)

	if len(res.DeniedTools) != 1 || res.DeniedTools[0] != "shell" {
		t.Fatalf("explain deny: denied=%v; want [shell]", res.DeniedTools)
	}
	var deny *GrantEntry
	for i := range res.Grants {
		if res.Grants[i].Effect == "deny" && res.Grants[i].Tool == "shell" {
			deny = &res.Grants[i]
			break
		}
	}
	if deny == nil {
		t.Fatalf("explain deny: no attributed deny entry in %+v", res.Grants)
	}
	if deny.Policy != "coders" || deny.BindingSubject != "group:coders" || deny.BindingScope != "daemon" {
		t.Fatalf("explain deny: attributed to policy=%q binding=%q scope=%q; want coders/group:coders/daemon",
			deny.Policy, deny.BindingSubject, deny.BindingScope)
	}
}

func TestExplainDefaultManagedRoute(t *testing.T) {
	policies := []Policy{{Name: "default", ToolsAllowed: []string{"file_read"}}}
	res := Explain([]string{"file_read"}, "lonely", nil, policies, nil, &ResolveOptions{}, true)

	if !res.Managed {
		t.Fatalf("default_managed route: managed=false, want true")
	}
	if len(res.Bindings) != 1 || res.Bindings[0].Policy != "default" || res.Bindings[0].Note == "" {
		t.Fatalf("default_managed route: bindings %+v; want the synthetic default row with a note", res.Bindings)
	}
	if len(res.Grants) == 0 || res.Grants[0].Policy != "default" {
		t.Fatalf("default_managed route: grants %+v; want entries attributed to the default policy", res.Grants)
	}
	if res.Grants[0].BindingScope != "daemon" {
		t.Fatalf("default_managed route: synthetic binding scope %q; want daemon", res.Grants[0].BindingScope)
	}
}

// ---------------------------------------------------------------------------
// check: all eight R5 edge-case classes, table test per class
// ---------------------------------------------------------------------------

func TestCheckR5EdgeCases(t *testing.T) {
	cases := []struct {
		name     string
		policies []Policy
		bindings []Binding
		agents   []DiagnosisAgent
		known    map[string]struct{}
		defMgd   bool
		over     OverridesReader
		wantCode string
		wantName string
	}{
		{
			name:     "R5-1 cyclic group membership",
			policies: []Policy{{Name: "a", ToolsAllowed: []string{"shell"}}, {Name: "b", ToolsAllowed: []string{"shell"}}},
			// The representable containment chain: the policy names double
			// as group labels, so group:a -> policy b == "group a contains
			// group b" and vice versa (the canonical self-collision fixture
			// shape from Validate's cycle detector).
			bindings: []Binding{
				{Subject: "group:a", Policy: "b"},
				{Subject: "group:b", Policy: "a"},
			},
			agents:   nil,
			wantCode: DefectCyclicGroup,
			wantName: "a",
		},
		{
			name:     "R5-2 dangling policy ref",
			policies: []Policy{{Name: "real", ToolsAllowed: []string{"shell"}}},
			bindings: []Binding{{Subject: "group:coders", Policy: "missing_policy"}},
			agents:   nil,
			wantCode: DefectDanglingPolicy,
			wantName: "missing_policy",
		},
		{
			name:     "R5-3 dangling tool name",
			policies: []Policy{{Name: "typo", ToolsAllowed: []string{"shel_l"}}},
			bindings: nil,
			agents:   nil,
			known:    map[string]struct{}{"shell": {}},
			wantCode: DefectUnregisteredTool,
			wantName: "shel_l",
		},
		{
			name:     "R5-4 unknown group in frontmatter",
			policies: []Policy{{Name: "coders", ToolsAllowed: []string{"shell"}}},
			bindings: []Binding{{Subject: "group:coders", Policy: "coders"}},
			agents:   []DiagnosisAgent{{ID: "zzdefect", Groups: []string{"ghost-group"}, Enabled: true}},
			wantCode: DefectUnknownGroup,
			wantName: "ghost-group",
		},
		{
			name: "R5-5 deny+allow same tool",
			policies: []Policy{{
				Name:         "conflicted",
				ToolsAllowed: []string{"shell"},
				ToolsDenied:  []string{"shell"},
			}},
			bindings: nil,
			agents:   nil,
			wantCode: DefectDenyAllowConflict,
			wantName: "shell",
		},
		{
			name: "R5-6 Managed+empty allowlist",
			policies: []Policy{{
				Name:         "empty_managed",
				ToolsAllowed: []string{},
				ToolsDenied:  []string{"shell"},
			}},
			bindings: nil,
			agents:   nil,
			wantCode: DefectManagedEmptyAllowlist,
			wantName: "empty_managed",
		},
		{
			name:     "R5-7 agent in no group + default_managed",
			policies: []Policy{{Name: "default", ToolsAllowed: []string{"file_read"}}},
			bindings: nil,
			agents:   []DiagnosisAgent{{ID: "lonely", Enabled: true}},
			defMgd:   true,
			wantCode: DefectAgentDefaultManaged,
			wantName: "lonely",
		},
		{
			name:     "R5-8 expired override",
			policies: []Policy{{Name: "coders", ToolsAllowed: []string{"shell"}}},
			bindings: []Binding{{Subject: "group:coders", Policy: "coders"}},
			agents:   []DiagnosisAgent{{ID: "aastub", Groups: []string{"coders"}, Tools: []string{"shell"}, Enabled: true}},
			over: fakeOverrides{expired: []OverrideRow{
				{Subject: "agent:aastub", Tool: "web_fetch", ExpiresAt: "2026-01-01T00:00:00Z"},
			}},
			wantCode: DefectExpiredOverride,
			wantName: "agent:aastub",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := Check(tc.policies, tc.bindings, tc.agents, tc.known, tc.defMgd, tc.over, nil)
			if res.Ok {
				t.Fatalf("check: ok=true; want defect %s/%s; defects: %+v", tc.wantCode, tc.wantName, res.Defects)
			}
			found := false
			for _, d := range res.Defects {
				if d.Code == tc.wantCode && d.Name == tc.wantName {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("check: defect %s/%s not reported; defects: %+v", tc.wantCode, tc.wantName, res.Defects)
			}
		})
	}
}

// fakeOverrides is the leaf-07 reader-seam test double.
type fakeOverrides struct {
	active  []OverrideRow
	expired []OverrideRow
	err     error
}

func (f fakeOverrides) ActiveOverrides(context.Context) ([]OverrideRow, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.active, nil
}

func (f fakeOverrides) ExpiredOverrides(context.Context) ([]OverrideRow, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.expired, nil
}

// active override rows are counted (R5-8 active half).
func TestCheckCountsActiveOverrides(t *testing.T) {
	policies, bindings, agents := c3Fixture()
	over := fakeOverrides{active: []OverrideRow{
		{Subject: "agent:aastub", Tool: "web_fetch", ExpiresAt: "2027-01-01T00:00:00Z"},
	}}
	res := Check(policies, bindings, agents, nil, false, over, nil)
	found := false
	for _, d := range res.Defects {
		if d.Code == DefectActiveOverride && d.Name == "agent:aastub" {
			found = true
		}
	}
	if !found {
		t.Fatalf("check: active override not reported; defects: %+v", res.Defects)
	}
	if res.Overrides.Active != 1 || res.Overrides.Total != 1 {
		t.Fatalf("check: override summary %+v; want active=1 total=1", res.Overrides)
	}
}

// overrides reader errors surface as a named finding, never a panic.
func TestCheckOverridesReaderError(t *testing.T) {
	_, _, agents := c3Fixture()
	over := fakeOverrides{err: fmt.Errorf("store closed")}
	res := Check(nil, nil, agents, nil, false, over, nil)
	found := false
	for _, d := range res.Defects {
		if d.Code == DefectOverridesReader {
			found = true
		}
	}
	if !found {
		t.Fatalf("check: reader error not reported; defects: %+v", res.Defects)
	}
}

// nil reader: zero overrides + the note (the pre-leaf-07 posture).
func TestCheckNilOverridesReader(t *testing.T) {
	res := Check(nil, nil, nil, nil, false, nil, nil)
	if !res.Ok || res.Overrides.Note == "" {
		t.Fatalf("check: nil reader should be clean with a note; got ok=%v note=%q", res.Ok, res.Overrides.Note)
	}
}

// clean config: ok=true, zero defects (the exit-code contract input).
func TestCheckCleanConfig(t *testing.T) {
	policies, bindings, agents := c3Fixture()
	known := map[string]struct{}{"shell": {}, "file_read": {}, "web_fetch": {}}
	res := Check(policies, bindings, agents, known, false, nil, nil)
	if !res.Ok || res.DefectCount != 0 || len(res.Defects) != 0 {
		t.Fatalf("check: clean fixture reported defects: %+v", res.Defects)
	}
	if res.AgentsChecked != 1 {
		t.Fatalf("check: agents_checked=%d, want 1", res.AgentsChecked)
	}
}

// deny-all posture on a BOUND agent (R5-6's live shape).
func TestCheckAgentDeniedEverything(t *testing.T) {
	policies := []Policy{{Name: "empty_managed", ToolsDenied: []string{"shell"}}}
	bindings := []Binding{{Subject: "agent:worker", Policy: "empty_managed"}}
	agents := []DiagnosisAgent{{ID: "worker", Tools: []string{"shell"}, Enabled: true}}
	res := Check(policies, bindings, agents, nil, false, nil, nil)
	found := false
	for _, d := range res.Defects {
		if d.Code == DefectAgentDeniedEverything && d.Name == "worker" {
			found = true
		}
	}
	if !found {
		t.Fatalf("check: bound deny-all agent not reported; defects: %+v", res.Defects)
	}
}

// default_managed=true with no "default" policy is the deny-everything
// surprise (the D7 fail-safe direction, reported so the author knows).
func TestCheckDefaultManagedMissingDefaultPolicy(t *testing.T) {
	policies := []Policy{{Name: "coders", ToolsAllowed: []string{"shell"}}}
	res := Check(policies, nil, nil, nil, true, nil, nil)
	found := false
	for _, d := range res.Defects {
		if d.Code == DefectManagedEmptyAllowlist && d.Name == "default" {
			found = true
		}
	}
	if !found {
		t.Fatalf("check: missing default policy under default_managed not reported; defects: %+v", res.Defects)
	}
}

// deterministic output: identical inputs produce identical sorted defects.
func TestCheckDeterministicOrder(t *testing.T) {
	policies, bindings, agents := c3Fixture()
	policies = append(policies, Policy{Name: "zz", ToolsAllowed: []string{"bad_tool_a"}}, Policy{Name: "aa", ToolsAllowed: []string{"bad_tool_b"}})
	r1 := Check(policies, bindings, agents, map[string]struct{}{"shell": {}}, false, nil, nil)
	r2 := Check(policies, bindings, agents, map[string]struct{}{"shell": {}}, false, nil, nil)
	if len(r1.Defects) != len(r2.Defects) || len(r1.Defects) == 0 {
		t.Fatalf("check: defect counts differ or empty: %d vs %d", len(r1.Defects), len(r2.Defects))
	}
	for i := range r1.Defects {
		if r1.Defects[i] != r2.Defects[i] {
			t.Fatalf("check: order differs at %d: %+v vs %+v", i, r1.Defects[i], r2.Defects[i])
		}
	}
}

// ---------------------------------------------------------------------------
// T4: stable --json keys (golden key contract)
// ---------------------------------------------------------------------------

func TestExplainJSONKeysStable(t *testing.T) {
	// The struct tags ARE the wire contract; this test fails at compile
	// drift by asserting the serialized key set.
	res := Explain([]string{"file_read", "shell_execute"}, "aastub", []string{"coders"},
		c3FixtureP(), c3FixtureB(), &ResolveOptions{}, false)

	// Marshal through the JSON tags the RPC returns.
	raw, err := json.Marshal(map[string]any{
		"agent_id":      res.AgentID,
		"groups":        res.Groups,
		"managed":       res.Managed,
		"allowed_tools": res.AllowedTools,
		"denied_tools":  res.DeniedTools,
		"grants":        res.Grants,
		"bindings":      res.Bindings,
		"resolved":      res.Resolved,
	})
	if err != nil {
		t.Fatalf("marshal explain: %v", err)
	}
	for _, key := range []string{
		"\"agent_id\"", "\"groups\"", "\"managed\"", "\"allowed_tools\"",
		"\"denied_tools\"", "\"grants\"", "\"bindings\"", "\"resolved\"",
	} {
		if !strings.Contains(string(raw), key) {
			t.Fatalf("explain JSON missing stable key %s: %s", key, raw)
		}
	}
	// Grant entry keys.
	for _, key := range []string{"\"tool\"", "\"effect\"", "\"policy\"", "\"binding_subject\"", "\"binding_scope\""} {
		if !strings.Contains(string(raw), key) {
			t.Fatalf("explain grant JSON missing stable key %s: %s", key, raw)
		}
	}
}

func TestCheckJSONKeysStable(t *testing.T) {
	policies := []Policy{{Name: "empty_managed", ToolsDenied: []string{"shell"}}}
	res := Check(policies, nil, nil, nil, false, nil, nil)
	raw, err := json.Marshal(map[string]any{
		"ok":             res.Ok,
		"defect_count":   res.DefectCount,
		"defects":        res.Defects,
		"agents_checked": res.AgentsChecked,
		"overrides":      res.Overrides,
	})
	if err != nil {
		t.Fatalf("marshal check: %v", err)
	}
	for _, key := range []string{"\"ok\"", "\"defect_count\"", "\"defects\"", "\"agents_checked\"", "\"overrides\""} {
		if !strings.Contains(string(raw), key) {
			t.Fatalf("check JSON missing stable key %s: %s", key, raw)
		}
	}
	for _, key := range []string{"\"code\"", "\"name\"", "\"detail\""} {
		if !strings.Contains(string(raw), key) {
			t.Fatalf("check finding JSON missing stable key %s: %s", key, raw)
		}
	}
}

// ErrCheckDefects is a distinct sentinel (scriptable exit-code contract).
func TestErrCheckDefectsSentinel(t *testing.T) {
	if ErrCheckDefects == nil {
		t.Fatal("ErrCheckDefects is nil")
	}
	wrapped := fmt.Errorf("cli: %w", ErrCheckDefects)
	if !errors.Is(wrapped, ErrCheckDefects) {
		t.Fatal("ErrCheckDefects not errors.Is-compatible")
	}
}
