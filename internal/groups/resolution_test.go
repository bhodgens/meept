package groups

import (
	"strings"
	"testing"
)

// TestResolver_ValidatePolicies_NamesOffender pins the C6 boot surface:
// a policy naming an unregistered tool fails validation WITH the offender
// named; a normalized match passes.
func TestResolver_ValidatePolicies_NamesOffender(t *testing.T) {
	opts := ResolverOptions{NormalizeToolName: func(n string) string {
		if n == "shell_execute" {
			return "shell"
		}
		return n
	}}.WithKnownTools(map[string]struct{}{"file_read": {}, "shell": {}})

	t.Run("unknown name rejected with the name", func(t *testing.T) {
		r := NewResolver(
			[]Policy{{Name: "bad", ToolsAllowed: []string{"file_read", "ghost_tool"}}},
			[]Binding{{Subject: "group:g", Policy: "bad"}},
			false, &opts)
		err := r.ValidatePolicies()
		if err == nil || !strings.Contains(err.Error(), "ghost_tool") {
			t.Fatalf("want named rejection, got %v", err)
		}
	})

	t.Run("normalized grant passes", func(t *testing.T) {
		r := NewResolver(
			[]Policy{{Name: "ok", ToolsAllowed: []string{"shell_execute"}}},
			[]Binding{{Subject: "group:g", Policy: "ok"}},
			false, &opts)
		if err := r.ValidatePolicies(); err != nil {
			t.Fatalf("normalized grant rejected: %v", err)
		}
	})

	t.Run("nil resolver and no known tools are inert", func(t *testing.T) {
		var nilr *Resolver
		if err := nilr.ValidatePolicies(); err != nil {
			t.Fatalf("nil resolver must be inert: %v", err)
		}
		r := NewResolver([]Policy{{Name: "ghosty", ToolsAllowed: []string{"ghost"}}}, nil, false, nil)
		if err := r.ValidatePolicies(); err != nil {
			t.Fatalf("no known-tools set: C6 leg inert, got %v", err)
		}
	})
}

// TestResolver_ResolveForAgent pins the D7 routing: a matching binding
// resolves managed; without one, default_managed=false yields the unmanaged
// record and true routes through the `default` policy fail-closed.
func TestResolver_ResolveForAgent(t *testing.T) {
	policies := []Policy{
		{Name: "coders", ToolsAllowed: []string{"file_read"}, ToolsDenied: []string{"shell"}},
		{Name: "default", ToolsAllowed: []string{"file_read"}},
	}
	bindings := []Binding{{Subject: "group:coders", Policy: "coders"}}

	t.Run("grouped: deny beats allow, managed", func(t *testing.T) {
		r := NewResolver(policies, bindings, false, nil)
		res := r.ResolveForAgent("a1", []string{"coders"}, []string{"file_read", "shell"})
		if !res.Managed {
			t.Fatal("grouped agent must resolve managed")
		}
		if len(res.AllowedTools) != 1 || res.AllowedTools[0] != "file_read" {
			t.Fatalf("allowed=%v, want [file_read]", res.AllowedTools)
		}
		if len(res.DeniedTools) != 1 || res.DeniedTools[0] != "shell" {
			t.Fatalf("denied=%v, want [shell]", res.DeniedTools)
		}
	})

	t.Run("ungrouped unmanaged: legacy record (D7)", func(t *testing.T) {
		r := NewResolver(policies, bindings, false, nil)
		res := r.ResolveForAgent("free", nil, []string{"file_read", "shell"})
		if res.Managed {
			t.Fatal("ungrouped with default_managed=false must be unmanaged")
		}
		if len(res.AllowedTools) != 0 {
			t.Fatalf("unmanaged record carries no gates, got %v", res.AllowedTools)
		}
	})

	t.Run("ungrouped default_managed: routed through default policy", func(t *testing.T) {
		r := NewResolver(policies, bindings, true, nil)
		res := r.ResolveForAgent("free", nil, []string{"file_read", "shell"})
		if !res.Managed {
			t.Fatal("default_managed=true must route ungrouped through the default policy")
		}
		if len(res.AllowedTools) != 1 || res.AllowedTools[0] != "file_read" {
			t.Fatalf("allowed=%v, want [file_read]", res.AllowedTools)
		}
	})

	t.Run("nil resolver: unmanaged", func(t *testing.T) {
		var r *Resolver
		if res := r.ResolveForAgent("a", nil, nil); res.Managed {
			t.Fatal("nil resolver must resolve unmanaged")
		}
	})
}
