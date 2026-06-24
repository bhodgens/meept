package agent

import (
	"encoding/json"
	"testing"
)

// validJSON is a helper that asserts a string is valid JSON via encoding/json.
func validJSON(t *testing.T, s string) {
	t.Helper()
	if !json.Valid([]byte(s)) {
		t.Fatalf("expected valid JSON, got invalid: %q", s)
	}
}

func TestNewExtractJSON_DirectJSON(t *testing.T) {
	in := `{"a": 1}`
	got := ExtractJSON(in)
	if got != in {
		t.Errorf("ExtractJSON(%q) = %q; want %q", in, got, in)
	}
	validJSON(t, got)
}

func TestNewExtractJSON_MarkdownJSONFence(t *testing.T) {
	in := "```json\n{\"x\": true}\n```"
	want := `{"x": true}`
	got := ExtractJSON(in)
	if got != want {
		t.Errorf("ExtractJSON(%q) = %q; want %q", in, got, want)
	}
	validJSON(t, got)
}

func TestNewExtractJSON_MarkdownGenericFence(t *testing.T) {
	in := "```\n{\"steps\": [{\"id\": 1}]}\n```"
	want := `{"steps": [{"id": 1}]}`
	got := ExtractJSON(in)
	if got != want {
		t.Errorf("ExtractJSON(%q) = %q; want %q", in, got, want)
	}
	validJSON(t, got)
}

func TestNewExtractJSON_ProseBefore(t *testing.T) {
	in := `Here is the plan: {"x": 1}`
	want := `{"x": 1}`
	got := ExtractJSON(in)
	if got != want {
		t.Errorf("ExtractJSON(%q) = %q; want %q", in, got, want)
	}
	validJSON(t, got)
}

func TestNewExtractJSON_BraceInString(t *testing.T) {
	// A '}' inside a string literal must not close the object early.
	in := `{"a": "}"}`
	got := ExtractJSON(in)
	if got != in {
		t.Errorf("ExtractJSON(%q) = %q; want %q", in, got, in)
	}
	validJSON(t, got)
}

func TestNewExtractJSON_OpenBraceInString(t *testing.T) {
	// An opening '{' inside a string must not increment depth.
	in := `{"desc": "use { for init"}`
	got := ExtractJSON(in)
	if got != in {
		t.Errorf("ExtractJSON(%q) = %q; want %q", in, got, in)
	}
	validJSON(t, got)
}

func TestNewExtractJSON_EscapedQuotes(t *testing.T) {
	// Escaped quotes inside strings must not toggle in-string mode.
	in := `{"a": "say \"hi\""}`
	got := ExtractJSON(in)
	if got != in {
		t.Errorf("ExtractJSON(%q) = %q; want %q", in, got, in)
	}
	validJSON(t, got)
}

func TestNewExtractJSON_EscapedBackslash(t *testing.T) {
	// An escaped backslash followed by a quote: "\\" should not escape the quote.
	// Input: {"path": "C:\\"}  — after \\ the " is a real string terminator.
	in := `{"path": "C:\\"}`
	got := ExtractJSON(in)
	if got != in {
		t.Errorf("ExtractJSON(%q) = %q; want %q", in, got, in)
	}
	validJSON(t, got)
}

func TestNewExtractJSON_NestedObjects(t *testing.T) {
	in := `{"a": {"b": {"c": 1}}}`
	got := ExtractJSON(in)
	if got != in {
		t.Errorf("ExtractJSON(%q) = %q; want %q", in, got, in)
	}
	validJSON(t, got)
}

func TestNewExtractJSON_NestedArrays(t *testing.T) {
	in := `{"steps": [{"id": 1}, {"id": 2}]}`
	got := ExtractJSON(in)
	if got != in {
		t.Errorf("ExtractJSON(%q) = %q; want %q", in, got, in)
	}
	validJSON(t, got)
}

func TestNewExtractJSON_NoJSON(t *testing.T) {
	in := `plain text without json`
	got := ExtractJSON(in)
	if got != "" {
		t.Errorf("ExtractJSON(%q) = %q; want empty", in, got)
	}
}

func TestNewExtractJSON_InvalidThenValid(t *testing.T) {
	// First candidate is malformed (missing value); scanner should reject via
	// json.Valid and continue to the next object.
	in := `{"bad": } {"good": true}`
	want := `{"good": true}`
	got := ExtractJSON(in)
	if got != want {
		t.Errorf("ExtractJSON(%q) = %q; want %q", in, got, want)
	}
	validJSON(t, got)
}

func TestNewExtractJSON_StrayBraceInProse(t *testing.T) {
	in := `Prose with } brace {"real": "json"} here`
	want := `{"real": "json"}`
	got := ExtractJSON(in)
	if got != want {
		t.Errorf("ExtractJSON(%q) = %q; want %q", in, got, want)
	}
	validJSON(t, got)
}

func TestNewExtractJSON_EmptyInput(t *testing.T) {
	got := ExtractJSON("")
	if got != "" {
		t.Errorf("ExtractJSON(\"\") = %q; want empty", got)
	}
}

func TestNewExtractJSON_OnlyBraces(t *testing.T) {
	// '{}' is a valid empty JSON object.
	in := `{}`
	got := ExtractJSON(in)
	if got != in {
		t.Errorf("ExtractJSON(%q) = %q; want %q", in, got, in)
	}
	validJSON(t, got)
}

func TestNewExtractJSON_EmptyThenJunk(t *testing.T) {
	// '{}' returns the empty object; trailing 'not valid {' is ignored.
	in := `{} not valid {`
	want := `{}`
	got := ExtractJSON(in)
	if got != want {
		t.Errorf("ExtractJSON(%q) = %q; want %q", in, got, want)
	}
	validJSON(t, got)
}

func TestNewExtractJSON_ArrayNotObject(t *testing.T) {
	// The scanner only matches '{'-delimited objects, so a bare JSON array
	// returns empty string. This is intentional and matches legacy behavior.
	in := `[1, 2, 3]`
	got := ExtractJSON(in)
	if got != "" {
		t.Errorf("ExtractJSON(%q) = %q; want empty (arrays are not matched)", in, got)
	}
}

func TestNewExtractJSON_TableDriven(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "simple object",
			in:   `{"a": 1}`,
			want: `{"a": 1}`,
		},
		{
			name: "object in fence with language",
			in:   "```json\n{\"a\": 1}\n```",
			want: `{"a": 1}`,
		},
		{
			name: "object in generic fence",
			in:   "```\n{\"a\": 1}\n```",
			want: `{"a": 1}`,
		},
		{
			name: "prose before object",
			in:   "Here is the plan:\n{\"x\": 1}",
			want: `{"x": 1}`,
		},
		{
			name: "brace in string literal",
			in:   `{"a": "}"}`,
			want: `{"a": "}"}`,
		},
		{
			name: "escaped quotes in string",
			in:   `{"a": "say \"hi\""}`,
			want: `{"a": "say \"hi\""}`,
		},
		{
			name: "nested objects three deep",
			in:   `{"a": {"b": {"c": 1}}}`,
			want: `{"a": {"b": {"c": 1}}}`,
		},
		{
			name: "no json at all",
			in:   "not json at all",
			want: "",
		},
		{
			name: "first invalid then valid",
			in:   `{"bad": } {"good": true}`,
			want: `{"good": true}`,
		},
		{
			name: "stray closing brace in prose before real object",
			in:   `Prose with } brace {"real": "json"} here`,
			want: `{"real": "json"}`,
		},
		{
			name: "empty input",
			in:   "",
			want: "",
		},
		{
			name: "whitespace only",
			in:   "   \n\t  ",
			want: "",
		},
		{
			name: "array only is not matched",
			in:   `[1, 2, 3]`,
			want: "",
		},
		{
			name: "fence with prose around",
			in:   "Sure!\n```json\n{\"k\": \"v\"}\n```\nLet me know.",
			want: `{"k": "v"}`,
		},
		{
			name: "object with array of strings containing braces",
			in:   `{"items": ["a", "}", "{"]}`,
			want: `{"items": ["a", "}", "{"]}`,
		},
		{
			name: "multiple valid objects returns first",
			in:   `{"first": 1} {"second": 2}`,
			want: `{"first": 1}`,
		},
		{
			name: "deeply nested with strings containing braces",
			in:   `{"a": {"b": "}{", "c": {"d": "{"}}}`,
			want: `{"a": {"b": "}{", "c": {"d": "{"}}}`,
		},
		{
			name: "escaped backslash before closing quote",
			in:   `{"p": "C:\\"}`,
			want: `{"p": "C:\\"}`,
		},
		{
			name: "escaped backslash then escaped quote",
			in:   `{"p": "C:\\\""}`,
			want: `{"p": "C:\\\""}`,
		},
		{
			name: "unbalanced extra closing brace after object",
			in:   `{"a": 1}}`,
			want: `{"a": 1}`,
		},
		{
			name: "fence without closing backticks falls through",
			in:   "```json\n{\"a\": 1}",
			want: `{"a": 1}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractJSON(tc.in)
			if got != tc.want {
				t.Errorf("ExtractJSON(%q) = %q; want %q", tc.in, got, tc.want)
			}
			// Every non-empty result must be valid JSON.
			if tc.want != "" {
				validJSON(t, got)
			}
		})
	}
}

// TestNewExtractJSON_RealisticPlan exercises a realistic strategic-plan payload
// similar to what the LLM emits, wrapped in a markdown fence with surrounding
// prose.
func TestNewExtractJSON_RealisticPlan(t *testing.T) {
	in := `I'll break this into two steps.

` + "```json" + `
{
  "steps": [
    {
      "id": 1,
      "description": "Read the config file",
      "tool_hint": "code",
      "depends_on": []
    },
    {
      "id": 2,
      "description": "Replace the value",
      "tool_hint": "code",
      "depends_on": [1]
    }
  ]
}
` + "```" + `

Let me know if you'd like adjustments.`

	got := ExtractJSON(in)
	if got == "" {
		t.Fatalf("ExtractJSON returned empty for realistic plan input")
	}
	validJSON(t, got)

	// Sanity check: the result should decode into a map with a "steps" key.
	var m map[string]any
	if err := json.Unmarshal([]byte(got), &m); err != nil {
		t.Fatalf("result did not unmarshal: %v", err)
	}
	steps, ok := m["steps"]
	if !ok {
		t.Fatalf("result missing 'steps' key; keys=%v", mapKeys(m))
	}
	arr, ok := steps.([]any)
	if !ok {
		t.Fatalf("expected 'steps' to be an array; got %T", steps)
	}
	if len(arr) != 2 {
		t.Errorf("expected 2 steps; got %d", len(arr))
	}
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
