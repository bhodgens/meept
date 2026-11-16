package services

// groups_service_test.go — leaf 06 T1/T4 service-layer gate: the assembly
// (config + roster -> engine) and the RPC-mirrored method set.

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/groups"
)

// fixtureConfig is the C3 config shape.
func fixtureConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Security.Groups.Policies = []config.GroupsPolicyConfig{{
		Name:         "coders",
		ToolsAllowed: []string{"file_read", "shell"},
		ToolsDenied:  []string{"web_fetch"},
	}}
	cfg.Security.Groups.Bindings = []config.GroupsBindingConfig{{
		Subject: "group:coders",
		Policy:  "coders",
		Scope:   "daemon",
	}}
	return cfg
}

// fixtureRoster maps a discovered roster with one grouped agent.
func fixtureRoster() []groups.DiagnosisAgent {
	return []groups.DiagnosisAgent{{
		ID:      "aastub",
		Groups:  []string{"coders"},
		Tools:   []string{"file_read", "shell_execute"},
		Enabled: true,
	}}
}

// serviceWithRoster builds a GroupsService whose discovery is replaced by
// the fixture roster (no filesystem tier reads in tests).
func serviceWithRoster(t *testing.T, cfg *config.Config, roster []groups.DiagnosisAgent) *GroupsService {
	t.Helper()
	svc := NewGroupsService(cfg, "")
	orig := rosterDiscoveryHook
	rosterDiscoveryHook = func() []groups.DiagnosisAgent { return roster }
	t.Cleanup(func() { rosterDiscoveryHook = orig })
	return svc
}

func TestGroupsServiceExplainAttribution(t *testing.T) {
	svc := serviceWithRoster(t, fixtureConfig(), fixtureRoster())
	res, err := svc.Explain(context.Background(), "aastub")
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	if !res.Managed {
		t.Fatal("explain: managed=false")
	}
	var shell *groups.GrantEntry
	for i := range res.Grants {
		if res.Grants[i].Tool == "shell" && res.Grants[i].Effect == "allow" {
			shell = &res.Grants[i]
		}
	}
	if shell == nil {
		t.Fatalf("explain: no shell grant in %+v", res.Grants)
	}
	if shell.Policy != "coders" || shell.BindingSubject != "group:coders" || shell.BindingScope != "daemon" {
		t.Fatalf("explain: shell attributed to %q/%q/%q; want coders/group:coders/daemon",
			shell.Policy, shell.BindingSubject, shell.BindingScope)
	}
}

func TestGroupsServiceExplainUnknownAgent(t *testing.T) {
	svc := serviceWithRoster(t, fixtureConfig(), fixtureRoster())
	_, err := svc.Explain(context.Background(), "nobody")
	if err == nil || !strings.Contains(err.Error(), "not in the discovered roster") {
		t.Fatalf("explain unknown agent: err=%v; want roster-miss error", err)
	}
	if _, err := svc.Explain(context.Background(), "  "); err == nil {
		t.Fatal("explain empty id: want error")
	}
}

// fakeEmployeeExplainResolver is the secretary-shaped seam: it answers one
// employee id and reports every other id as not an employee.
type fakeEmployeeExplainResolver struct {
	view EmployeeExplain
}

func (f fakeEmployeeExplainResolver) ExplainEmployee(_ context.Context, id string) (EmployeeExplain, bool, error) {
	if id != f.view.ID {
		return EmployeeExplain{}, false, nil
	}
	return f.view, true, nil
}

// TestGroupsServiceExplainEmployeeNotRoster pins that an employee id such
// as secretary is explained from the employee row, not rejected as a
// missing roster agent and not rendered as a discovered AGENT.md subject.
func TestGroupsServiceExplainEmployeeNotRoster(t *testing.T) {
	svc := serviceWithRoster(t, fixtureConfig(), fixtureRoster())
	svc.SetEmployeeExplainResolver(fakeEmployeeExplainResolver{
		view: EmployeeExplain{
			ID:                  "secretary",
			ConstitutionPresent: true,
			ToolsAllowed:        []string{"web_fetch", "file_read"},
			ToolsForbidden:      []string{"shell_execute", "git_push"},
		},
	})

	res, err := svc.Explain(context.Background(), "secretary")
	if err != nil {
		t.Fatalf("explain secretary: %v", err)
	}
	if res == nil {
		t.Fatal("explain secretary: nil result")
	}
	if res.Managed || len(res.Groups) != 0 {
		t.Fatalf("explain secretary pretended to be a roster agent: managed=%v groups=%v", res.Managed, res.Groups)
	}
	subject, constitution := "", ""
	for _, b := range res.Bindings {
		if strings.HasPrefix(b.Subject, "employee:") {
			subject = b.Subject
			constitution = b.Note
			break
		}
	}
	if subject != "employee:secretary" {
		t.Fatalf("explain secretary: subject %q; want employee:secretary (not a roster agent: subject)", subject)
	}
	if constitution != "constitution present" {
		t.Fatalf("explain secretary: constitution note %q; want constitution present", constitution)
	}
	allowed, forbidden := map[string]bool{}, map[string]bool{}
	for _, g := range res.Grants {
		if g.BindingSubject != "employee:secretary" {
			t.Fatalf("explain secretary: grant %+v is not an employee-row grant", g)
		}
		switch g.Reason {
		case "tools_allowed":
			allowed[g.Tool] = true
		case "tools_forbidden":
			forbidden[g.Tool] = true
		default:
			t.Fatalf("explain secretary: grant reason %q; want tools_allowed or tools_forbidden", g.Reason)
		}
	}
	for _, tool := range []string{"web_fetch", "file_read"} {
		if !allowed[tool] {
			t.Fatalf("explain secretary: tools_allowed missing %q (got %v)", tool, allowed)
		}
	}
	for _, tool := range []string{"shell_execute", "git_push"} {
		if !forbidden[tool] {
			t.Fatalf("explain secretary: tools_forbidden missing %q (got %v)", tool, forbidden)
		}
	}
	if len(res.AllowedTools) != 0 || len(res.DeniedTools) != 0 {
		t.Fatalf("explain secretary: roster allow/deny sets populated (%v / %v); employee tools must not ride the roster resolution", res.AllowedTools, res.DeniedTools)
	}

	// A wired resolver that does not know the id must still fail, same as
	// a roster miss. The existing roster explain stays on the other test.
	if _, err := svc.Explain(context.Background(), "nobody"); err == nil || !strings.Contains(err.Error(), "not in the discovered roster") {
		t.Fatalf("explain missing employee and roster agent: err=%v; want roster-miss error", err)
	}
}

func TestGroupsServiceCheckDefects(t *testing.T) {
	cfg := fixtureConfig()
	// Inject: unknown group in the roster + a dangling policy binding.
	cfg.Security.Groups.Bindings = append(cfg.Security.Groups.Bindings,
		config.GroupsBindingConfig{Subject: "group:coders", Policy: "missing_policy"})
	roster := fixtureRoster()
	roster[0].Groups = []string{"coders", "ghost-group"}

	svc := serviceWithRoster(t, cfg, roster)
	res, err := svc.Check(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if res.Ok || res.DefectCount < 2 {
		t.Fatalf("check: ok=%v count=%d; want >=2 defects", res.Ok, res.DefectCount)
	}
	sawDangling, sawGhost := false, false
	for _, d := range res.Defects {
		if d.Code == groups.DefectDanglingPolicy && d.Name == "missing_policy" {
			sawDangling = true
		}
		if d.Code == groups.DefectUnknownGroup && d.Name == "ghost-group" {
			sawGhost = true
		}
	}
	if !sawDangling || !sawGhost {
		t.Fatalf("check: dangling=%v ghost=%v; defects: %+v", sawDangling, sawGhost, res.Defects)
	}
}

func TestGroupsServiceCheckClean(t *testing.T) {
	svc := serviceWithRoster(t, fixtureConfig(), fixtureRoster())
	known := map[string]struct{}{"shell": {}, "file_read": {}, "web_fetch": {}}
	res, err := svc.Check(context.Background(), known, nil)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !res.Ok || res.DefectCount != 0 {
		t.Fatalf("check: clean fixture reported %+v", res.Defects)
	}
}

// The RPC DiagnosisService interface is satisfied structurally by
// *GroupsService (the daemon wiring compiles only when the shapes match —
// this assertion makes drift a compile error HERE too).
func TestGroupsServiceSatisfiesRPCDiagnosisInterface(t *testing.T) {
	var _ interface {
		Explain(ctx context.Context, agentID string) (*groups.ExplainResult, error)
		Check(ctx context.Context, knownTools map[string]struct{}, overrides groups.OverridesReader) (*groups.CheckResult, error)
	} = NewGroupsService(fixtureConfig(), "")
}
