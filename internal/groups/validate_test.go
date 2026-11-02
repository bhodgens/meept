package groups

import (
	"strings"
	"testing"
)

// TestValidate_CyclicGroup pins T2: a cyclic group-membership chain
// produces a NAMED error (the group name appears in the message).
// Chains exist where a policy name doubles as another group's label
// (the only representable group-in-group nesting in the C3 surface);
// A -> B -> A would expand forever.
func TestValidate_CyclicGroup(t *testing.T) {
	bindings := []Binding{
		// group a's policy is named "b", and "b" is itself a group whose
		// policy is named "a": membership chain a -> b -> a.
		{Subject: "group:a", Policy: "b", Scope: "daemon"},
		{Subject: "group:b", Policy: "a", Scope: "daemon"},
	}
	policies := []Policy{{Name: "b"}, {Name: "a"}}

	err := Validate(policies, bindings)
	if err == nil {
		t.Fatal("Validate returned nil for a cyclic group chain, want named error")
	}
	if !strings.Contains(err.Error(), "cyclic_group") {
		t.Fatalf("cycle error does not carry the cyclic_group kind: %v", err)
	}
	if !strings.Contains(err.Error(), "a") || !strings.Contains(err.Error(), "b") {
		t.Fatalf("cycle error does not NAME the groups: %v", err)
	}
}

// TestValidate_CanonicalSelfNamedGroupPins pins that the canonical
// fixture shape (group:coders -> policy "coders") is NOT flagged as a
// cycle — group label and policy name sharing a string is legal.
func TestValidate_CanonicalSelfNamedGroupPins(t *testing.T) {
	policies := []Policy{{Name: "coders", ToolsAllowed: []string{"file_read"}}}
	bindings := []Binding{{Subject: "group:coders", Policy: "coders", Scope: "daemon"}}
	if err := Validate(policies, bindings); err != nil {
		t.Fatalf("canonical self-named group/policy pair rejected: %v", err)
	}
}

// TestValidate_DanglingPolicyRef pins T2: a binding to an unknown policy
// is a named error.
func TestValidate_DanglingPolicyRef(t *testing.T) {
	policies := []Policy{{Name: "real"}}
	bindings := []Binding{{Subject: "agent:a1", Policy: "missing_policy", Scope: "daemon"}}

	err := Validate(policies, bindings)
	if err == nil {
		t.Fatal("Validate returned nil for a dangling policy ref, want named error")
	}
	if !strings.Contains(err.Error(), "missing_policy") {
		t.Fatalf("dangling-ref error does not NAME the policy: %v", err)
	}
	if !strings.Contains(err.Error(), "dangling_policy") {
		t.Fatalf("dangling-ref error does not carry the kind: %v", err)
	}
}

// TestValidate_BadSubjectForms pins the C6 subject well-formedness check.
func TestValidate_BadSubjectForms(t *testing.T) {
	cases := []struct {
		subject string
		want    string // defect kind, "" = clean
	}{
		{"agent:coder", ""},
		{"group:coders", ""},
		{"user:ben", ""},
		{"", DefectBadSubject},
		{"coder", DefectBadSubject},       // no prefix
		{"agent:", DefectBadSubject},      // empty id
		{"team:coders", DefectBadSubject}, // unknown prefix
		{"group:coders extra", DefectBadSubject},
	}
	for _, tc := range cases {
		bindings := []Binding{{Subject: tc.subject, Policy: "p", Scope: "daemon"}}
		policies := []Policy{{Name: "p"}}
		err := Validate(policies, bindings)
		if tc.want == "" {
			if err != nil {
				t.Errorf("subject %q: unexpected defect: %v", tc.subject, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("subject %q: want defect kind %q, got: %v", tc.subject, tc.want, err)
		}
	}
}

// TestValidate_BadScopes pins the scope well-formedness check.
func TestValidate_BadScopes(t *testing.T) {
	cases := []struct {
		scope string
		bad   bool
	}{
		{"daemon", false},
		{"", false},
		{"project:proj-1", false},
		{"project:", true},
		{"cluster:xyz", true},
	}
	for _, tc := range cases {
		policies := []Policy{{Name: "p"}}
		bindings := []Binding{{Subject: "agent:a", Policy: "p", Scope: tc.scope}}
		err := Validate(policies, bindings)
		if tc.bad && (err == nil || !strings.Contains(err.Error(), DefectBadScope)) {
			t.Errorf("scope %q: want %s defect, got %v", tc.scope, DefectBadScope, err)
		}
		if !tc.bad && err != nil {
			t.Errorf("scope %q: unexpected defect: %v", tc.scope, err)
		}
	}
}

// TestValidate_CleanConfig passes for the documented example config.
func TestValidate_CleanConfig(t *testing.T) {
	policies := []Policy{
		{Name: "coders", ToolsAllowed: []string{"file_read", "shell"}},
		{Name: "default", ToolsAllowed: []string{"file_read"}},
	}
	bindings := []Binding{
		{Subject: "group:coders", Policy: "coders", Scope: "daemon"},
		{Subject: "agent:web-bot", Policy: "default", Scope: "project:proj-1"},
		{Subject: "user:ben", Policy: "coders"},
	}
	if err := Validate(policies, bindings); err != nil {
		t.Fatalf("clean config rejected: %v", err)
	}
}

// TestValidate_CollectsAllDefects pins that one call reports every finding
// (the boot hook surfaces one error; `groups check` needs the full list).
func TestValidate_CollectsAllDefects(t *testing.T) {
	policies := []Policy{{Name: "dup"}, {Name: "dup"}}
	bindings := []Binding{
		{Subject: "junk", Policy: "ghost", Scope: "bogus"},
	}
	err := Validate(policies, bindings)
	if err == nil {
		t.Fatal("want defects, got nil")
	}
	for _, want := range []string{"duplicate_policy", "dangling_policy", "bad_subject", "bad_scope", "ghost", "dup"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("aggregate error missing %q:\n%v", want, err)
		}
	}
}

// TestValidate_DuplicateBinding pins the exact-duplicate binding check.
func TestValidate_DuplicateBinding(t *testing.T) {
	policies := []Policy{{Name: "p"}}
	bindings := []Binding{
		{Subject: "agent:a", Policy: "p", Scope: "daemon"},
		{Subject: "agent:a", Policy: "p", Scope: "daemon"},
	}
	err := Validate(policies, bindings)
	if err == nil || !strings.Contains(err.Error(), DefectDuplicateBind) {
		t.Fatalf("want duplicate_binding defect, got %v", err)
	}
}

// TestValidatePolicyTools pins C6's named-offender contract with the
// injected normalizer (no internal/tools import; the registry set is a
// plain map here).
func TestValidatePolicyTools(t *testing.T) {
	known := map[string]struct{}{"file_read": {}, "shell": {}}
	normalize := func(n string) string {
		if n == "shell_execute" {
			return "shell"
		}
		return n
	}

	t.Run("normalized match passes", func(t *testing.T) {
		policies := []Policy{{Name: "p", ToolsAllowed: []string{"shell_execute"}}}
		if err := ValidatePolicyTools(policies, known, normalize); err != nil {
			t.Fatalf("normalized grant rejected: %v", err)
		}
	})
	t.Run("unknown name rejected WITH the name", func(t *testing.T) {
		policies := []Policy{{Name: "p", ToolsAllowed: []string{"file_read", "definitely_not_a_tool"}}}
		err := ValidatePolicyTools(policies, known, normalize)
		if err == nil {
			t.Fatal("unknown tool name accepted, want named rejection")
		}
		if !strings.Contains(err.Error(), "definitely_not_a_tool") {
			t.Fatalf("rejection does not NAME the offender: %v", err)
		}
		if !strings.Contains(err.Error(), "p") {
			t.Fatalf("rejection does not NAME the policy: %v", err)
		}
	})
	t.Run("denied names validated too", func(t *testing.T) {
		policies := []Policy{{Name: "p", ToolsDenied: []string{"nope"}}}
		err := ValidatePolicyTools(policies, known, normalize)
		if err == nil || !strings.Contains(err.Error(), "nope") {
			t.Fatalf("deny-side offender not named: %v", err)
		}
	})
}
