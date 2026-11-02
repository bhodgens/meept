package groups

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// T1: Resolve — the frozen C3 precedence matrix
// ---------------------------------------------------------------------------

// table-driven matrix covering every cell the leaf requires:
// deny-beats-allow; intersection never widens spec; empty effective
// allowlist => AllowedTools empty + Managed=true (D8); scope/class
// narrowing; one-level nesting; subject forms.
func TestResolve_Matrix(t *testing.T) {
	type testcase struct {
		name        string
		specTools   []string
		agentID     string
		groups      []string
		policies    []Policy
		bindings    []Binding
		opts        *ResolveOptions
		wantAllowed []string
		wantDenied  []string
		wantMemory  string
		wantFSRoots []string
		wantNetwork string
		wantModels  []string
		wantManaged bool
	}

	cases := []testcase{
		{
			name:      "deny beats allow across policies",
			specTools: []string{"file_read", "shell", "web_fetch"},
			agentID:   "a1",
			policies: []Policy{
				{Name: "p_allow", ToolsAllowed: []string{"file_read", "shell", "web_fetch"}},
				{Name: "p_deny", ToolsDenied: []string{"shell"}},
			},
			bindings: []Binding{
				{Subject: "agent:a1", Policy: "p_allow"},
				{Subject: "agent:a1", Policy: "p_deny"},
			},
			wantAllowed: []string{"file_read", "web_fetch"},
			wantDenied:  []string{"shell"},
			wantManaged: true,
		},
		{
			name:      "intersection never widens the spec",
			specTools: []string{"file_read"},
			agentID:   "a2",
			policies:  []Policy{{Name: "wide", ToolsAllowed: []string{"file_read", "shell", "git_push"}}},
			bindings:  []Binding{{Subject: "agent:a2", Policy: "wide"}},
			// even with overrides: spec is the ceiling
			opts:        &ResolveOptions{ExtraAllows: []string{"git_push"}},
			wantAllowed: []string{"file_read"},
			wantManaged: true,
		},
		{
			name:        "empty effective allowlist => Managed=true + no tools (D8)",
			specTools:   []string{"file_read", "shell"},
			agentID:     "a3",
			policies:    []Policy{{Name: "empty", ToolsAllowed: []string{}}},
			bindings:    []Binding{{Subject: "agent:a3", Policy: "empty"}},
			wantAllowed: nil,
			wantManaged: true,
		},
		{
			name:      "deny-only policy narrows the spec (no allow list = deny gate only)",
			specTools: []string{"file_read", "shell"},
			agentID:   "a4",
			policies:  []Policy{{Name: "noshell", ToolsDenied: []string{"shell"}}},
			bindings:  []Binding{{Subject: "agent:a4", Policy: "noshell"}},
			// An active policy with NO allows: effective allowlist is
			// empty => D8 deny-everything for a managed subject.
			wantAllowed: nil,
			wantDenied:  []string{"shell"},
			wantManaged: true,
		},
		{
			name:      "memory scope narrowing: narrowest wins",
			specTools: []string{"file_read"},
			agentID:   "a5",
			policies: []Policy{
				{Name: "g1", MemoryMaxScope: "global"},
				{Name: "g2", MemoryMaxScope: "project"},
			},
			bindings: []Binding{
				{Subject: "agent:a5", Policy: "g1"},
				{Subject: "agent:a5", Policy: "g2"},
			},
			wantAllowed: nil,
			wantMemory:  "project",
			wantManaged: true,
		},
		{
			name:      "network class most-restrictive-wins",
			specTools: []string{"file_read"},
			agentID:   "a6",
			policies: []Policy{
				{Name: "open", NetworkClass: "unrestricted"},
				{Name: "lock", NetworkClass: "none"},
			},
			bindings: []Binding{
				{Subject: "agent:a6", Policy: "open"},
				{Subject: "agent:a6", Policy: "lock"},
			},
			wantNetwork: "none",
			wantManaged: true,
		},
		{
			name:      "fs_roots union across policies",
			specTools: []string{"file_read"},
			agentID:   "a7",
			policies: []Policy{
				{Name: "r1", FSRoots: []string{"~/a", "~/b"}},
				{Name: "r2", FSRoots: []string{"~/b", "~/c"}},
			},
			bindings: []Binding{
				{Subject: "agent:a7", Policy: "r1"},
				{Subject: "agent:a7", Policy: "r2"},
			},
			wantFSRoots: []string{"~/a", "~/b", "~/c"},
			wantManaged: true,
		},
		{
			name:      "allowed_models intersect",
			specTools: []string{"file_read"},
			agentID:   "a8",
			policies: []Policy{
				{Name: "m1", AllowedModels: []string{"coder", "planner"}},
				{Name: "m2", AllowedModels: []string{"coder", "analyst"}},
			},
			bindings: []Binding{
				{Subject: "agent:a8", Policy: "m1"},
				{Subject: "agent:a8", Policy: "m2"},
			},
			wantModels:  []string{"coder"},
			wantManaged: true,
		},
		{
			name:        "group binding selects the policy (one level of nesting)",
			specTools:   []string{"file_read", "shell"},
			agentID:     "a9",
			groups:      []string{"coders"},
			policies:    []Policy{{Name: "pol", ToolsAllowed: []string{"file_read"}}},
			bindings:    []Binding{{Subject: "group:coders", Policy: "pol"}},
			wantAllowed: []string{"file_read"},
			wantManaged: true,
		},
		{
			name:      "group-of-group does NOT nest (depth exactly 1)",
			specTools: []string{"file_read", "shell"},
			agentID:   "a10",
			groups:    []string{"outer"},
			policies: []Policy{
				{Name: "outer-pol", ToolsAllowed: []string{"file_read"}},
				{Name: "inner-pol", ToolsAllowed: []string{"shell"}},
			},
			bindings: []Binding{
				// outer members are NOT granted inner's policy through a
				// group:inner subject — memberships are flat labels; the
				// agent resolved ONLY through its own label's binding.
				{Subject: "group:outer", Policy: "outer-pol"},
				{Subject: "group:inner", Policy: "inner-pol"},
			},
			wantAllowed: []string{"file_read"},
			wantManaged: true,
		},
		{
			name:        "user-subject bindings never match agent resolution",
			specTools:   []string{"file_read"},
			agentID:     "a11",
			policies:    []Policy{{Name: "human", ToolsAllowed: []string{"shell"}}},
			bindings:    []Binding{{Subject: "user:ben", Policy: "human"}},
			wantManaged: false,
		},
		{
			name:      "normalizer seam: shell_execute resolves to shell",
			specTools: []string{"shell_execute"},
			agentID:   "a12",
			policies:  []Policy{{Name: "sh", ToolsDenied: []string{"shell_execute"}}},
			bindings:  []Binding{{Subject: "agent:a12", Policy: "sh"}},
			opts: &ResolveOptions{NormalizeToolName: func(n string) string {
				if n == "shell_execute" {
					return "shell"
				}
				return n
			}},
			wantDenied:  []string{"shell"},
			wantManaged: true,
		},
		{
			name:      "override allow cannot rescue a DENIED tool",
			specTools: []string{"shell"},
			agentID:   "a13",
			policies: []Policy{
				{Name: "allowshell", ToolsAllowed: []string{"shell"}},
				{Name: "denyshell", ToolsDenied: []string{"shell"}},
			},
			bindings: []Binding{
				{Subject: "agent:a13", Policy: "allowshell"},
				{Subject: "agent:a13", Policy: "denyshell"},
			},
			opts:        &ResolveOptions{ExtraAllows: []string{"shell"}},
			wantAllowed: nil,
			wantDenied:  []string{"shell"},
			wantManaged: true,
		},
		{
			name:        "ungrouped unmanaged: Managed=false, no gates (D7)",
			specTools:   []string{"file_read", "shell"},
			agentID:     "free",
			wantManaged: false,
		},
		{
			name:        "dangling binding ref degrades to unmanaged (Validate names it)",
			specTools:   []string{"file_read"},
			agentID:     "a14",
			bindings:    []Binding{{Subject: "agent:a14", Policy: "ghost"}},
			wantManaged: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Resolve(tc.specTools, tc.agentID, tc.groups, tc.policies, tc.bindings, tc.opts)
			if got.Managed != tc.wantManaged {
				t.Fatalf("Managed = %v, want %v (resolved: %s)", got.Managed, tc.wantManaged, got)
			}
			if strings.Join(got.AllowedTools, ",") != strings.Join(tc.wantAllowed, ",") {
				t.Errorf("AllowedTools = %v, want %v", got.AllowedTools, tc.wantAllowed)
			}
			if strings.Join(got.DeniedTools, ",") != strings.Join(tc.wantDenied, ",") {
				t.Errorf("DeniedTools = %v, want %v", got.DeniedTools, tc.wantDenied)
			}
			if got.MemoryMaxScope != tc.wantMemory {
				t.Errorf("MemoryMaxScope = %q, want %q", got.MemoryMaxScope, tc.wantMemory)
			}
			if strings.Join(got.FSRoots, ",") != strings.Join(tc.wantFSRoots, ",") {
				t.Errorf("FSRoots = %v, want %v", got.FSRoots, tc.wantFSRoots)
			}
			if got.NetworkClass != tc.wantNetwork {
				t.Errorf("NetworkClass = %q, want %q", got.NetworkClass, tc.wantNetwork)
			}
			if strings.Join(got.AllowedModels, ",") != strings.Join(tc.wantModels, ",") {
				t.Errorf("AllowedModels = %v, want %v", got.AllowedModels, tc.wantModels)
			}
		})
	}
}

// TestResolve_DefaultManaged pins D7's fail-closed routing: unbound agent
// under default_managed=true resolves through the "default" policy; a
// missing default policy is deny-everything (Managed, no tools).
func TestResolve_DefaultManaged(t *testing.T) {
	t.Run("default policy grants read only", func(t *testing.T) {
		got := ResolveDefaultManaged(
			[]string{"file_read", "shell"},
			[]Policy{{Name: "default", ToolsAllowed: []string{"file_read"}}},
			nil,
		)
		if !got.Managed {
			t.Fatal("Managed = false, want true")
		}
		if strings.Join(got.AllowedTools, ",") != "file_read" {
			t.Errorf("AllowedTools = %v, want [file_read]", got.AllowedTools)
		}
	})
	t.Run("missing default policy denies everything", func(t *testing.T) {
		got := ResolveDefaultManaged([]string{"file_read"}, nil, nil)
		if !got.Managed || len(got.AllowedTools) != 0 {
			t.Fatalf("want Managed=true with empty allowlist, got %s", got)
		}
	})
}

// TestResolve_NoIOShape pins the purity contract: Resolve is deterministic
// across calls (same inputs => identical output ordering) and carries no
// hidden state.
func TestResolve_Deterministic(t *testing.T) {
	policies := []Policy{{Name: "p", ToolsAllowed: []string{"b", "a", "c"}}}
	bindings := []Binding{{Subject: "agent:x", Policy: "p"}}
	first := Resolve([]string{"c", "a"}, "x", nil, policies, bindings, nil)
	second := Resolve([]string{"c", "a"}, "x", nil, policies, bindings, nil)
	if strings.Join(first.AllowedTools, ",") != "a,c" ||
		strings.Join(second.AllowedTools, ",") != "a,c" {
		t.Fatalf("resolution not deterministic/sorted: %v vs %v", first.AllowedTools, second.AllowedTools)
	}
}
