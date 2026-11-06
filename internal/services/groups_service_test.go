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
