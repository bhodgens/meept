package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadJSON5_GroupsSectionRoundTrip pins the C3 config surface: the
// [security.groups] section loads policies/bindings/default_managed through
// the JSON5 loader with the exact field names the e2e gate overlays use
// ("groups" TOP-LEVEL is the e2e overlay's dotted path; the schema home is
// security.groups — both are verified here via the JSON5 loader and the
// default).
func TestLoadJSON5_GroupsSectionRoundTrip(t *testing.T) {
	content := `{
  "security": {
    "groups": {
      "default_managed": true,
      "decision_log_level": "all",
      "admin_agents": ["agent-update", "ops-bot"],
      "admin_users": ["ben"],
      "policies": [
        {
          "name": "coders",
          "tools_allowed": ["file_read", "shell_execute"],
          "tools_denied": ["web_fetch"],
          "memory_max_scope": "project",
          "fs_roots": ["~/work"],
          "network_class": "allowlist",
          "allowed_models": ["coder"]
        }
      ],
      "bindings": [
        { "subject": "group:coders", "policy": "coders", "scope": "daemon" }
      ],
    },
  },
}`
	path := filepath.Join(t.TempDir(), "meept.json5")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	g := cfg.Security.Groups
	if !g.DefaultManaged {
		t.Error("default_managed did not round-trip (want true)")
	}
	if g.DecisionLogLevel != "all" {
		t.Errorf("decision_log_level = %q, want \"all\"", g.DecisionLogLevel)
	}
	if len(g.AdminAgents) != 2 || g.AdminAgents[0] != "agent-update" {
		t.Errorf("admin_agents = %v, want [agent-update ops-bot]", g.AdminAgents)
	}
	if len(g.Policies) != 1 {
		t.Fatalf("policies did not load: got %d entries", len(g.Policies))
	}
	p := g.Policies[0]
	if p.Name != "coders" || len(p.ToolsAllowed) != 2 || p.ToolsAllowed[1] != "shell_execute" {
		t.Errorf("policy[0] = %+v, want name=coders with shell_execute allowed", p)
	}
	if len(p.ToolsDenied) != 1 || p.ToolsDenied[0] != "web_fetch" {
		t.Errorf("policy tools_denied = %v, want [web_fetch]", p.ToolsDenied)
	}
	if p.MemoryMaxScope != "project" || p.NetworkClass != "allowlist" {
		t.Errorf("ceilings = (%q, %q), want (project, allowlist)", p.MemoryMaxScope, p.NetworkClass)
	}
	if len(g.Bindings) != 1 || g.Bindings[0].Subject != "group:coders" || g.Bindings[0].Policy != "coders" {
		t.Errorf("bindings did not round-trip: %+v", g.Bindings)
	}
}

// TestDefaultConfig_GroupsSection pins the DefaultConfig() entries the leaf
// REQUIRES (zero-value defaults pitfall): default_managed=false and
// decision_log_level="deny-warn", plus the C5 admin default.
func TestDefaultConfig_GroupsSection(t *testing.T) {
	cfg := DefaultConfig()
	g := cfg.Security.Groups
	if g.DefaultManaged {
		t.Error("default_managed must default false (D7: ungrouped agents keep legacy behavior)")
	}
	if g.DecisionLogLevel != "deny-warn" {
		t.Errorf("default decision_log_level = %q, want \"deny-warn\"", g.DecisionLogLevel)
	}
	if len(g.AdminAgents) != 1 || g.AdminAgents[0] != "agent-update" {
		t.Errorf("default admin_agents = %v, want [agent-update]", g.AdminAgents)
	}
	if len(g.Policies) != 0 || len(g.Bindings) != 0 {
		t.Errorf("default policy/binding tables must be empty, got %d/%d",
			len(g.Policies), len(g.Bindings))
	}
}
