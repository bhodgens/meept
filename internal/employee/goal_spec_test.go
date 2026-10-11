package employee

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// TestGoalSpecValidate is the table-driven validation test for GoalSpec
// (leaf 01 Task 1 / SHARED-CONVENTIONS C1).
func TestGoalSpecValidate(t *testing.T) {
	t.Parallel()

	valid := GoalSpec{
		EmployeeID: "emp1",
		Title:      "keep main shippable",
		Mandate:    "ensure every commit on main passes CI",
	}

	cases := []struct {
		name    string
		mutate  func(s *GoalSpec)
		wantErr string // empty means must pass
	}{
		{
			name:   "valid minimal",
			mutate: func(s *GoalSpec) {},
		},
		{
			name: "valid with source user",
			mutate: func(s *GoalSpec) {
				s.Source = "user"
			},
		},
		{
			name: "valid with source trigger",
			mutate: func(s *GoalSpec) {
				s.Source = "trigger"
				s.TriggerRef = "cron:nightly"
			},
		},
		{
			name: "valid with gate",
			mutate: func(s *GoalSpec) {
				s.Gate = &GoalGateSpec{Command: "go test ./...", TimeoutSeconds: 120}
			},
		},
		{
			name: "missing employee_id",
			mutate: func(s *GoalSpec) {
				s.EmployeeID = ""
			},
			wantErr: "employee_id",
		},
		{
			name: "missing title",
			mutate: func(s *GoalSpec) {
				s.Title = ""
			},
			wantErr: "title",
		},
		{
			name: "missing mandate",
			mutate: func(s *GoalSpec) {
				s.Mandate = ""
			},
			wantErr: "mandate",
		},
		{
			name: "invalid source",
			mutate: func(s *GoalSpec) {
				s.Source = "self_proposed"
			},
			wantErr: "source",
		},
		{
			name: "empty source string is valid (defaults to user)",
			mutate: func(s *GoalSpec) {
				s.Source = ""
			},
		},
		{
			name: "gate with empty command is still structurally valid at spec level",
			mutate: func(s *GoalSpec) {
				s.Gate = &GoalGateSpec{}
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := valid
			tc.mutate(&spec)
			err := spec.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

// TestGoalSpecToGoal verifies the GoalSpec → Goal mapping (leaf 01 Task 1).
func TestGoalSpecToGoal(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)

	t.Run("defaults", func(t *testing.T) {
		spec := GoalSpec{EmployeeID: "emp1", Title: "t", Mandate: "m"}
		g := spec.toGoal(now)
		if g == nil {
			t.Fatal("toGoal returned nil")
		}
		if g.EmployeeID != "emp1" || g.Title != "t" || g.Mandate != "m" {
			t.Errorf("basic fields not mapped: %+v", g)
		}
		if g.Source != SourceUser {
			t.Errorf("Source = %q, want %q (default)", g.Source, SourceUser)
		}
		if g.State != GoalActive {
			t.Errorf("State = %v, want GoalActive", g.State)
		}
		if g.Health != GoalUnknown {
			t.Errorf("Health = %v, want GoalUnknown", g.Health)
		}
		if !g.CreatedAt.Equal(now) {
			t.Errorf("CreatedAt = %v, want %v", g.CreatedAt, now)
		}
		if g.Gate != nil {
			t.Errorf("Gate = %+v, want nil", g.Gate)
		}
		if g.TriggerRef != "" {
			t.Errorf("TriggerRef = %q, want empty", g.TriggerRef)
		}
	})

	t.Run("source trigger and gate mapped", func(t *testing.T) {
		spec := GoalSpec{
			EmployeeID: "emp1",
			Title:      "t",
			Mandate:    "m",
			Source:     "trigger",
			TriggerRef: "webhook:ci",
			Gate: &GoalGateSpec{
				Command:           "npm test",
				TimeoutSeconds:    60,
				SkipWhenUnchanged: true,
			},
		}
		g := spec.toGoal(now)
		if g.Source != SourceTrigger {
			t.Errorf("Source = %q, want %q", g.Source, SourceTrigger)
		}
		if g.TriggerRef != "webhook:ci" {
			t.Errorf("TriggerRef = %q, want webhook:ci", g.TriggerRef)
		}
		if g.Gate == nil {
			t.Fatal("Gate = nil, want mapped GateConfig")
		}
		if g.Gate.Command != "npm test" || g.Gate.TimeoutSeconds != 60 || !g.Gate.SkipWhenUnchanged {
			t.Errorf("Gate = %+v, want command/timeout/skip mapped", g.Gate)
		}
	})

	t.Run("empty gate command maps to nil gate", func(t *testing.T) {
		spec := GoalSpec{
			EmployeeID: "emp1",
			Title:      "t",
			Mandate:    "m",
			Gate:       &GoalGateSpec{TimeoutSeconds: 30},
		}
		g := spec.toGoal(now)
		if g.Gate != nil {
			t.Errorf("Gate = %+v, want nil for empty command", g.Gate)
		}
	})
}

// TestGoalSpecValidateErrorMentionsField is a light guard that the error is
// descriptive (wrapped with %w per repo conventions once surfaced).
func TestGoalSpecValidateErrorMentionsField(t *testing.T) {
	t.Parallel()
	spec := GoalSpec{Title: "t", Mandate: "m", Source: "bogus"}
	err := spec.Validate()
	if err == nil {
		t.Fatal("expected error for invalid source")
	}
	if !errors.Is(err, ErrInvalidGoalSpec) {
		t.Fatalf("errors.Is(err, ErrInvalidGoalSpec) = false, got %v", err)
	}
}
