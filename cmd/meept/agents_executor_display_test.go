package main

import "testing"

// TestRenderExecutorSection verifies the `executor:` section of
// `agents show` (acp-employees dim 3A): lowercase values per the repo UI
// convention, "-" for an empty persona, "global" for an empty permission
// mode, joined command_args.
func TestRenderExecutorSection(t *testing.T) {
	tests := []struct {
		name string
		in   map[string]any
		want string
	}{
		{
			name: "full acp binding",
			in: map[string]any{
				"executor": "acp",
				"acp": map[string]any{
					"agent":           "hermes",
					"persona":         "bob",
					"permission_mode": "human",
					"command_args":    []any{"-p", "borg"},
				},
			},
			want: "\nexecutor: acp\n" +
				"  acp agent: hermes\n" +
				"  persona: bob\n" +
				"  permission mode: human\n" +
				"  command_args: -p borg\n",
		},
		{
			name: "defaults for empty persona and permission mode",
			in: map[string]any{
				"executor": "acp",
				"acp": map[string]any{
					"agent": "hermes",
				},
			},
			want: "\nexecutor: acp\n" +
				"  acp agent: hermes\n" +
				"  persona: -\n" +
				"  permission mode: global\n" +
				"  command_args: -\n",
		},
		{
			name: "missing acp block",
			in: map[string]any{
				"executor": "acp",
			},
			want: "\nexecutor: acp\n" +
				"  acp agent: -\n" +
				"  persona: -\n" +
				"  permission mode: global\n" +
				"  command_args: -\n",
		},
		{
			name: "empty executor renders nothing",
			in:   map[string]any{},
			want: "",
		},
		{
			name: "empty executor string renders nothing",
			in:   map[string]any{"executor": ""},
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := renderExecutorSection(tc.in)
			if got != tc.want {
				t.Errorf("renderExecutorSection() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestRenderExecutorSection_Lowercase pins the repo UI convention: the
// rendered section contains no uppercase characters.
func TestRenderExecutorSection_Lowercase(t *testing.T) {
	got := renderExecutorSection(map[string]any{
		"executor": "acp",
		"acp": map[string]any{
			"agent":           "Hermes",
			"persona":         "Bob",
			"permission_mode": "Human",
			"command_args":    []any{"-p", "borg"},
		},
	})
	for _, r := range got {
		if r >= 'A' && r <= 'Z' {
			t.Fatalf("renderExecutorSection contains uppercase %q: %q", r, got)
		}
	}
}

// TestEmployeeExecutorLine covers the employee detail-view executor line
// (acp-employees phase 3 display parity): lowercase id with the (acp)
// suffix when executor_agent is set, empty line when absent or blank.
func TestEmployeeExecutorLine(t *testing.T) {
	tests := []struct {
		name string
		in   map[string]any
		want string
	}{
		{"set renders lowercased id", map[string]any{"executor_agent": "Hermes"}, "executor: hermes (acp)"},
		{"surrounding spaces trimmed", map[string]any{"executor_agent": "  bob  "}, "executor: bob (acp)"},
		{"absent renders nothing", map[string]any{}, ""},
		{"empty renders nothing", map[string]any{"executor_agent": ""}, ""},
		{"non-string renders nothing", map[string]any{"executor_agent": 42}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := employeeExecutorLine(tc.in); got != tc.want {
				t.Errorf("employeeExecutorLine(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
