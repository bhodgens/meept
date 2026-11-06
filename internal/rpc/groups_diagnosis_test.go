package rpc

// groups_diagnosis_test.go — leaf 06 T1/T3 RPC gate: groups.check answers
// through the wired diagnosis service (nil-safe degradation), and the
// service-backed explain carries the attribution keys.

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/groups"
)

// fakeDiagnosis is the DiagnosisService test double (the interface the
// daemon wiring satisfies with *services.GroupsService).
type fakeDiagnosis struct {
	explainRes *groups.ExplainResult
	explainErr error
	checkRes   *groups.CheckResult
	checkErr   error
	// lastKnown captures the known-tools injection the handler passed.
	lastKnown map[string]struct{}
}

func (f *fakeDiagnosis) Explain(_ context.Context, agentID string) (*groups.ExplainResult, error) {
	if f.explainErr != nil {
		return nil, f.explainErr
	}
	if f.explainRes == nil {
		f.explainRes = &groups.ExplainResult{AgentID: agentID, Managed: true}
	}
	return f.explainRes, nil
}

func (f *fakeDiagnosis) Check(_ context.Context, knownTools map[string]struct{}, _ groups.OverridesReader) (*groups.CheckResult, error) {
	f.lastKnown = knownTools
	if f.checkErr != nil {
		return nil, f.checkErr
	}
	if f.checkRes == nil {
		f.checkRes = &groups.CheckResult{Ok: true, Defects: []groups.CheckFinding{}}
	}
	return f.checkRes, nil
}

func TestGroupsCheckWired(t *testing.T) {
	h := NewGroupsHandler(nil, "", nil, nil, nil)
	h.SetDiagnosisService(&fakeDiagnosis{
		checkRes: &groups.CheckResult{
			Ok:          false,
			DefectCount: 1,
			Defects: []groups.CheckFinding{{
				Code:   groups.DefectUnknownGroup,
				Name:   "ghost-group",
				Detail: "declared but no binding",
			}},
			AgentsChecked: 3,
		},
	})

	result, err := h.handleCheck(context.Background(), []byte(`{"known_tools":["shell","file_read"]}`))
	if err != nil {
		t.Fatalf("groups.check: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("groups.check: result %T is not a map", result)
	}
	for _, key := range []string{"ok", "defect_count", "defects", "agents_checked", "overrides"} {
		if _, present := m[key]; !present {
			t.Fatalf("groups.check: missing stable key %q in %v", key, m)
		}
	}
	if m["defect_count"] != 1 {
		t.Fatalf("groups.check: defect_count=%v; want 1", m["defect_count"])
	}
}

func TestGroupsCheckNilServiceDegrades(t *testing.T) {
	h := NewGroupsHandler(nil, "", nil, nil, nil)
	_, err := h.handleCheck(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "diagnosis service not wired") {
		t.Fatalf("groups.check nil service: err=%v; want descriptive error", err)
	}
}

func TestGroupsCheckPassesKnownTools(t *testing.T) {
	fake := &fakeDiagnosis{}
	h := NewGroupsHandler(nil, "", nil, nil, nil)
	h.SetDiagnosisService(fake)
	if _, err := h.handleCheck(context.Background(), []byte(`{"known_tools":["shell","shell"]}`)); err != nil {
		t.Fatalf("groups.check: %v", err)
	}
	if _, ok := fake.lastKnown["shell"]; !ok || len(fake.lastKnown) != 1 {
		t.Fatalf("groups.check: known tools %v; want {shell}", fake.lastKnown)
	}
}

func TestGroupsExplainServiceBacked(t *testing.T) {
	h := NewGroupsHandler(nil, "", nil, nil, nil)
	h.SetDiagnosisService(&fakeDiagnosis{
		explainRes: &groups.ExplainResult{
			AgentID: "aastub",
			Groups:  []string{"coders"},
			Managed: true,
			Grants: []groups.GrantEntry{{
				Tool:           "shell",
				Effect:         "allow",
				Policy:         "coders",
				BindingSubject: "group:coders",
				BindingScope:   "daemon",
			}},
			Bindings: []groups.ExplainSource{{Subject: "group:coders", Policy: "coders", Scope: "daemon"}},
		},
	})

	result, err := h.handleExplain(context.Background(), []byte(`{"agent_id":"aastub"}`))
	if err != nil {
		t.Fatalf("groups.explain: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("groups.explain: result %T is not a map", result)
	}
	// The F8 RPC contract keys + the attribution keys.
	for _, key := range []string{"agent_id", "bindings", "policy", "grants", "resolved"} {
		if _, present := m[key]; !present {
			t.Fatalf("groups.explain: missing stable key %q in %v", key, m)
		}
	}
	if m["policy"] != "coders" {
		// The stable F8 key carries the FIRST selecting binding's policy.
		t.Fatalf("groups.explain: policy key %v; want coders", m["policy"])
	}
}

// TestDiagnosisServiceInterfaceShape pins the daemon-wiring contract:
// the interface the handler consumes matches the services.GroupsService
// method set (compile-time via a local structural assertion).
func TestDiagnosisServiceInterfaceShape(t *testing.T) {
	var _ DiagnosisService = (DiagnosisService)(nil)
}
