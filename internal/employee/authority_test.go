package employee

import (
	"context"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// DetectEscalationCycles — cycle detection over the escalates_to graph.
// ---------------------------------------------------------------------------

func TestDetectEscalationCycles(t *testing.T) {
	tests := []struct {
		name        string
		graph       StaticEscalationGraph
		seeds       []string
		wantCycles  int
		wantErr     bool
		errContains string
	}{
		{
			name: "no cycles acyclic",
			graph: StaticEscalationGraph{
				"a": {EscalatesTo: []string{"b"}},
				"b": {EscalatesTo: []string{"user"}},
			},
			seeds:      []string{"a", "b"},
			wantCycles: 0,
			wantErr:    false,
		},
		{
			name: "direct self-escalation",
			graph: StaticEscalationGraph{
				"a": {EscalatesTo: []string{"a"}},
			},
			seeds:       []string{"a"},
			wantCycles:  1,
			wantErr:     true,
			errContains: "cycle",
		},
		{
			name: "two-hop transitive cycle",
			graph: StaticEscalationGraph{
				"a": {EscalatesTo: []string{"b"}},
				"b": {EscalatesTo: []string{"a"}},
			},
			seeds:       []string{"a"},
			wantCycles:  1,
			wantErr:     true,
			errContains: "cycle",
		},
		{
			name: "three-hop transitive cycle",
			graph: StaticEscalationGraph{
				"a": {EscalatesTo: []string{"b"}},
				"b": {EscalatesTo: []string{"c"}},
				"c": {EscalatesTo: []string{"a"}},
			},
			seeds:       []string{"a"},
			wantCycles:  1,
			wantErr:     true,
			errContains: "cycle",
		},
		{
			name: "cycle deduplicated across seeds",
			graph: StaticEscalationGraph{
				"a": {EscalatesTo: []string{"b"}},
				"b": {EscalatesTo: []string{"a"}},
			},
			seeds:      []string{"a", "b"},
			wantCycles: 1,
			wantErr:    true,
		},
		{
			name: "two independent cycles",
			graph: StaticEscalationGraph{
				"a": {EscalatesTo: []string{"b"}},
				"b": {EscalatesTo: []string{"a"}},
				"c": {EscalatesTo: []string{"d"}},
				"d": {EscalatesTo: []string{"c"}},
			},
			seeds:      []string{"a", "c"},
			wantCycles: 2,
			wantErr:    true,
		},
		{
			name: "unresolved agent ID reported",
			graph: StaticEscalationGraph{
				"a": {EscalatesTo: []string{"ghost"}},
			},
			seeds:       []string{"a"},
			wantCycles:  0,
			wantErr:     true,
			errContains: "unresolved",
		},
		{
			name: "empty seeds is no-op",
			graph: StaticEscalationGraph{
				"a": {EscalatesTo: []string{"a"}},
			},
			seeds:      nil,
			wantCycles: 0,
			wantErr:    false,
		},
		{
			name:        "nil graph is no-op",
			graph:       nil,
			seeds:       []string{"a"},
			wantCycles:  0,
			wantErr:     false,
		},
		{
			name: "diamond not a cycle",
			graph: StaticEscalationGraph{
				"a": {EscalatesTo: []string{"b", "c"}},
				"b": {EscalatesTo: []string{"d"}},
				"c": {EscalatesTo: []string{"d"}},
				"d": {EscalatesTo: []string{"user"}},
			},
			seeds:      []string{"a"},
			wantCycles: 0,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cycles, err := DetectEscalationCycles(tt.graph, tt.seeds)
			if len(cycles) != tt.wantCycles {
				t.Errorf("got %d cycles, want %d: %+v", len(cycles), tt.wantCycles, cycles)
			}
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
			if tt.wantErr && tt.errContains != "" && err != nil {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errContains)
				}
			}
		})
	}
}

func TestDetectEscalationCycles_CycleString(t *testing.T) {
	graph := StaticEscalationGraph{
		"a": {EscalatesTo: []string{"b"}},
		"b": {EscalatesTo: []string{"c"}},
		"c": {EscalatesTo: []string{"a"}},
	}
	cycles, err := DetectEscalationCycles(graph, []string{"a"})
	if err == nil {
		t.Fatal("expected cycle error")
	}
	if len(cycles) != 1 {
		t.Fatalf("got %d cycles, want 1", len(cycles))
	}
	s := cycles[0].String()
	// Should contain all three IDs and arrows between them.
	if !strings.Contains(s, "a") || !strings.Contains(s, "b") || !strings.Contains(s, "c") {
		t.Errorf("cycle string %q missing members", s)
	}
	if !strings.Contains(s, "->") {
		t.Errorf("cycle string %q missing arrow", s)
	}
}

func TestDetectEscalationCycles_DeterministicOrder(t *testing.T) {
	// Two cycles of different lengths: shorter should be reported first.
	graph := StaticEscalationGraph{
		"a": {EscalatesTo: []string{"a"}}, // length 1
		"b": {EscalatesTo: []string{"c"}},
		"c": {EscalatesTo: []string{"d"}},
		"d": {EscalatesTo: []string{"b"}}, // length 3
	}
	cycles, _ := DetectEscalationCycles(graph, []string{"a", "b"})
	if len(cycles) != 2 {
		t.Fatalf("got %d cycles, want 2", len(cycles))
	}
	if len(cycles[0].AgentIDs) > len(cycles[1].AgentIDs) {
		t.Errorf("cycles not sorted shortest-first: %v before %v",
			cycles[0].AgentIDs, cycles[1].AgentIDs)
	}
}

func TestDetectEscalationCycles_RoleSentinels(t *testing.T) {
	t.Run("role-prefixed user is terminal leaf", func(t *testing.T) {
		graph := StaticEscalationGraph{
			"a": {EscalatesTo: []string{"role:user"}},
		}
		cycles, err := DetectEscalationCycles(graph, []string{"a"})
		if err != nil {
			t.Errorf("role:user should be terminal: got err %v", err)
		}
		if len(cycles) != 0 {
			t.Errorf("role:user should not produce cycles: got %d", len(cycles))
		}
	})
	t.Run("role-prefixed oncall is terminal leaf", func(t *testing.T) {
		graph := StaticEscalationGraph{
			"a": {EscalatesTo: []string{"role:oncall"}},
		}
		cycles, err := DetectEscalationCycles(graph, []string{"a"})
		if err != nil {
			t.Errorf("role:oncall should be terminal: got err %v", err)
		}
		if len(cycles) != 0 {
			t.Errorf("role:oncall should not produce cycles: got %d", len(cycles))
		}
	})
	t.Run("legacy user still works as terminal leaf", func(t *testing.T) {
		graph := StaticEscalationGraph{
			"a": {EscalatesTo: []string{"user"}},
		}
		cycles, err := DetectEscalationCycles(graph, []string{"a"})
		if err != nil {
			t.Errorf("legacy 'user' should be terminal: got err %v", err)
		}
		if len(cycles) != 0 {
			t.Errorf("legacy 'user' should not produce cycles: got %d", len(cycles))
		}
	})
	t.Run("role-prefixed custom sink is terminal", func(t *testing.T) {
		graph := StaticEscalationGraph{
			"a": {EscalatesTo: []string{"role:custom-team"}},
		}
		cycles, err := DetectEscalationCycles(graph, []string{"a"})
		if err != nil {
			t.Errorf("role:custom-team should be terminal: got err %v", err)
		}
		if len(cycles) != 0 {
			t.Errorf("role:custom-team should not produce cycles: got %d", len(cycles))
		}
	})
}

// ---------------------------------------------------------------------------
// EscalationRouter tests.
// ---------------------------------------------------------------------------

func TestEscalationRouter_RouteEscalation(t *testing.T) {
	router := NewEscalationRouter(nil) // falls back to slog.Default()

	t.Run("valid escalation logs and returns nil", func(t *testing.T) {
		err := router.RouteEscalation(context.TODO(), "emp-a", "shell_execute",
			"high-risk action", []string{"user"})
		if err != nil {
			t.Errorf("expected nil error, got: %v", err)
		}
	})

	t.Run("empty employeeID rejected", func(t *testing.T) {
		err := router.RouteEscalation(context.TODO(), "", "shell_execute",
			"reason", []string{"user"})
		if err == nil {
			t.Error("expected error for empty employeeID")
		}
	})

	t.Run("empty approvers rejected", func(t *testing.T) {
		err := router.RouteEscalation(context.TODO(), "emp-a", "shell_execute",
			"reason", nil)
		if err == nil {
			t.Error("expected error for empty approvers")
		}
	})
}

func TestEscalationRouter_SetLogger_NilGuard(t *testing.T) {
	router := NewEscalationRouter(nil)
	// Must not panic on nil.
	router.SetLogger(nil)
	if router.logger == nil {
		t.Error("SetLogger(nil) should not blank the logger")
	}
}

// ---------------------------------------------------------------------------
// Role sentinel + escalation normalization helpers.
// ---------------------------------------------------------------------------

func TestIsRoleSentinel(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		// Role-prefixed (canonical form)
		{"role:user", true},
		{"role:oncall", true},
		{"role:admin", true},
		{"role:anything-here", true},
		// Legacy bare sentinels
		{"user", true},
		{"system", true},
		{"operator", true},
		{"oncall", true},
		{"admin", true},
		// Agent IDs — not sentinels
		{"emp-a", false},
		{"ci-monitor", false},
		{"", false},
		{"role", false}, // bare "role" without colon is an agent ID
		{"roleuser", false},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			if got := IsRoleSentinel(tt.id); got != tt.want {
				t.Errorf("IsRoleSentinel(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}

func TestNormalizeEscalatesTo(t *testing.T) {
	t.Run("normalizes legacy bare sentinels", func(t *testing.T) {
		input := []string{"user", "system", "operator", "oncall", "admin"}
		got := NormalizeEscalatesTo(input)
		want := []string{"role:user", "role:system", "role:operator", "role:oncall", "role:admin"}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("[%d] got %q, want %q", i, got[i], want[i])
			}
		}
	})
	t.Run("passes through role-prefixed unchanged", func(t *testing.T) {
		input := []string{"role:user", "role:custom-role"}
		got := NormalizeEscalatesTo(input)
		if got[0] != "role:user" || got[1] != "role:custom-role" {
			t.Errorf("role-prefixed IDs should pass through: got %v", got)
		}
	})
	t.Run("passes through agent IDs unchanged", func(t *testing.T) {
		input := []string{"emp-a", "ci-monitor", "debugger"}
		got := NormalizeEscalatesTo(input)
		if got[0] != "emp-a" || got[1] != "ci-monitor" || got[2] != "debugger" {
			t.Errorf("agent IDs should pass through: got %v", got)
		}
	})
	t.Run("mixed legacy and canonical and agent IDs", func(t *testing.T) {
		input := []string{"user", "role:oncall", "emp-a", "system"}
		got := NormalizeEscalatesTo(input)
		want := []string{"role:user", "role:oncall", "emp-a", "role:system"}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("[%d] got %q, want %q", i, got[i], want[i])
			}
		}
	})
	t.Run("empty input returns empty", func(t *testing.T) {
		if got := NormalizeEscalatesTo(nil); got != nil {
			t.Errorf("nil input should return nil, got %v", got)
		}
		if got := NormalizeEscalatesTo([]string{}); len(got) != 0 {
			t.Errorf("empty input should return empty, got %v", got)
		}
	})
}
