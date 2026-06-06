package agent

import (
	"log/slog"
	"os"
	"testing"
)

func TestPairProgrammingDriver_Name(t *testing.T) {
	d := NewPairProgrammingDriver(PairProgrammingDriverDeps{Logger: slog.New(slog.NewTextHandler(os.Stderr, nil))})
	if d.Name() != "pair_programming" {
		t.Errorf("Name() = %q, want pair_programming", d.Name())
	}
}

func TestPairProgrammingDriver_CanInitiate(t *testing.T) {
	d := NewPairProgrammingDriver(PairProgrammingDriverDeps{Logger: slog.New(slog.NewTextHandler(os.Stderr, nil))})
	if !d.CanInitiate("coder", "test reason") {
		t.Error("CanInitiate should return true")
	}
}

func TestPairProgrammingDriver_getOtherParticipant(t *testing.T) {
	d := NewPairProgrammingDriver(PairProgrammingDriverDeps{Logger: slog.New(slog.NewTextHandler(os.Stderr, nil))})
	tests := []struct {
		parts    []string
		current  string
		expected string
	}{
		{[]string{"a", "b"}, "a", "b"},
		{[]string{"a", "b"}, "b", "a"},
		{[]string{"a", "b", "c"}, "a", "b"},
		{[]string{"a"}, "a", ""},
	}
	for _, tc := range tests {
		got := d.getOtherParticipant(tc.parts, tc.current)
		if got != tc.expected {
			t.Errorf("getOtherParticipant(%v, %q) = %q, want %q", tc.parts, tc.current, got, tc.expected)
		}
	}
}

func TestPairProgrammingDriver_parseObserverResponse(t *testing.T) {
	d := NewPairProgrammingDriver(PairProgrammingDriverDeps{Logger: slog.New(slog.NewTextHandler(os.Stderr, nil))})
	tests := []struct {
		name       string
		input      string
		wantAction string
	}{
		// Structured JSON responses
		{"structured approve", `{"action": "approve", "feedback": "code is clean"}`, "approve"},
		{"structured request_changes", `{"action": "request_changes", "feedback": "fix nil check"}`, "request_changes"},
		{"structured request_token", `{"action": "request_token", "feedback": "let me drive"}`, "request_token"},
		{"structured in text", "Review complete. {\"action\": \"approve\", \"feedback\": \"LGTM\"}", "approve"},

		// Fallback keyword matching
		{"keyword approve", "This looks good to me. Approve.", "approve"},
		{"keyword lgtm", "LGTM", "approve"},
		{"keyword request_token", "I want to take over as driver. request_token", "request_token"},
		{"keyword default", "There's a bug in line 42. Fix the off-by-one.", "request_changes"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			action, _ := d.parseObserverResponse(tc.input)
			if action != tc.wantAction {
				t.Errorf("parse(%q) action = %q, want %q", tc.input, action, tc.wantAction)
			}
		})
	}
}

func TestNewPairProgrammingDriver_Defaults(t *testing.T) {
	d := NewPairProgrammingDriver(PairProgrammingDriverDeps{})
	if d.logger == nil {
		t.Error("logger should not be nil")
	}
	if d.conversations == nil {
		t.Error("conversations map should be initialized")
	}
}
