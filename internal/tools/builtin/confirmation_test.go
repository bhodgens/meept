package builtin

import (
	"testing"
)

func TestConfirmationResponse(t *testing.T) {
	details := map[string]any{"old_id": "abc", "new_id": "def"}
	got := ConfirmationResponse("mark_superseded", true, "supersede abc with def", details)

	if v, ok := got["requires_confirmation"].(bool); !ok || !v {
		t.Errorf("requires_confirmation must be bool true, got %v", got["requires_confirmation"])
	}
	if got["action"] != "mark_superseded" {
		t.Errorf("action = %v, want mark_superseded", got["action"])
	}
	if v, ok := got["reversible"].(bool); !ok || !v {
		t.Errorf("reversible must be bool true when reversibleFlag=true, got %v", got["reversible"])
	}
	if got["summary"] != "supersede abc with def" {
		t.Errorf("summary = %v, want 'supersede abc with def'", got["summary"])
	}
	if got["confirm_arg"] != "confirmed" {
		t.Errorf("confirm_arg = %v, want 'confirmed'", got["confirm_arg"])
	}
	detailsMap, ok := got["details"].(map[string]any)
	if !ok {
		t.Fatalf("details must be map[string]any, got %T", got["details"])
	}
	if detailsMap["old_id"] != "abc" {
		t.Errorf("details.old_id = %v, want abc", detailsMap["old_id"])
	}
	if detailsMap["new_id"] != "def" {
		t.Errorf("details.new_id = %v, want def", detailsMap["new_id"])
	}
}

func TestConfirmationResponse_NotReversible(t *testing.T) {
	got := ConfirmationResponse("purge_auto_claims", false, "purge 10 auto claims", nil)
	if v, ok := got["reversible"].(bool); !ok || v {
		t.Errorf("reversible must be bool false when reversibleFlag=false, got %v", got["reversible"])
	}
	if _, present := got["details"]; present {
		t.Errorf("details should be absent when nil, got %v", got["details"])
	}
}

func TestIsConfirmationRequest(t *testing.T) {
	cases := []struct {
		in   map[string]any
		want bool
	}{
		{map[string]any{"requires_confirmation": true}, true},
		{map[string]any{"requires_confirmation": false}, false},
		{map[string]any{"requires_confirmation": "true"}, false}, // not bool
		{map[string]any{}, false},
		{nil, false},
	}
	for i, c := range cases {
		got := IsConfirmationRequest(c.in)
		if got != c.want {
			t.Errorf("case %d: got %v, want %v", i, got, c.want)
		}
	}
}

func TestDeclineResponse(t *testing.T) {
	orig := ConfirmationResponse("mark_superseded", false, "summary", map[string]any{"k": "v"})
	got := DeclineResponse(orig)

	if v, ok := got["declined"].(bool); !ok || !v {
		t.Errorf("declined must be bool true, got %v", got["declined"])
	}
	if got["action"] != "mark_superseded" {
		t.Errorf("action = %v, want mark_superseded", got["action"])
	}
	if got["summary"] != "summary" {
		t.Errorf("summary = %v, want 'summary'", got["summary"])
	}
	if _, ok := got["user_note"]; !ok {
		t.Errorf("user_note key must be present (empty string allowed)")
	}
}
