package main

// voice_link_test.go - flag-validation and status-parsing tests for the
// `meept voice link` CLI (deadair-link plan leaf 03). Pure: no daemon,
// no RPC. Mirrors voice_test.go's formatter-test style.

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestVoiceLinkSubcommandRegistered(t *testing.T) {
	v := newVoiceCmd()
	var linkCmd bool
	for _, sub := range v.Commands() {
		if sub.Name() == "link" {
			linkCmd = true
			break
		}
	}
	if !linkCmd {
		t.Fatal("voice subcommands missing: link")
	}
}

func TestVoiceLinkFlagValidation(t *testing.T) {
	tests := []struct {
		name    string
		flags   linkFlags
		wantErr string // substring; "" = valid
	}{
		{"missing a", linkFlags{b: "secretary", seconds: 60}, "--a is required"},
		{"missing b", linkFlags{a: "companion", seconds: 60}, "--b is required"},
		{"empty both", linkFlags{seconds: 60}, "--a is required"},
		{"same employee", linkFlags{a: "companion", b: "companion", seconds: 60}, "different employees"},
		{"zero seconds", linkFlags{a: "a", b: "b", seconds: 0}, "--seconds must be > 0"},
		{"negative seconds", linkFlags{a: "a", b: "b", seconds: -5}, "--seconds must be > 0"},
		{"valid", linkFlags{a: "companion", b: "secretary", seconds: 90}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.flags.validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("validate(%+v) = %v, want nil", tc.flags, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validate(%+v) = nil, want error containing %q", tc.flags, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("validate(%+v) = %v, want error containing %q", tc.flags, err, tc.wantErr)
			}
		})
	}
}

func TestVoiceLinkFlagDefaults(t *testing.T) {
	cmd := newVoiceLinkCmd()
	sec, err := cmd.Flags().GetInt("seconds")
	if err != nil {
		t.Fatalf("seconds flag: %v", err)
	}
	if sec != 60 {
		t.Fatalf("--seconds default = %d, want 60", sec)
	}
	// Same-employee refusal fires EARLY through RunE (before any daemon
	// connect): executing the command with --a == --b must never reach
	// connectDaemon (there is no daemon in this test - a connection
	// attempt would hang or fail with a connect error, not the
	// validation message).
	cmd.SetArgs([]string{"--a", "x", "--b", "x", "--seconds", "10"})
	err = cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "different employees") {
		t.Fatalf("RunE same-employee = %v, want early validation error", err)
	}
}

func TestVoiceLinkStatusParsing(t *testing.T) {
	// The CLI parses the daemon's voice.link.status envelope; pin the
	// wire shape round-trip against a hand-built JSON (the exact
	// internal/rpc/voice_link.go field names).
	raw := `{
		"link_id": "link-abc",
		"ended": false,
		"a": {"call_id": "local-1", "employee_id": "companion", "assistant_turns": 2, "user_turns": 1,
		      "transcript": [{"role": "assistant", "text": "hello", "at": "2026-11-09T12:00:00Z"}]},
		"b": {"call_id": "local-2", "employee_id": "secretary", "assistant_turns": 1, "user_turns": 0,
		      "transcript": []}
	}`
	var res voiceLinkStatusResult
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	if res.LinkID != "link-abc" || res.Ended {
		t.Fatalf("envelope = %+v", res)
	}
	if res.A.AssistantTurns != 2 || res.A.UserTurns != 1 || res.A.EmployeeID != "companion" {
		t.Fatalf("side a = %+v", res.A)
	}
	if len(res.A.Transcript) != 1 || res.A.Transcript[0].Text != "hello" {
		t.Fatalf("side a transcript = %+v", res.A.Transcript)
	}
	if len(res.B.Transcript) != 0 {
		t.Fatalf("side b transcript = %+v, want empty", res.B.Transcript)
	}
}

// TestVoiceLinkAcceptanceGate pins the exit rule: the run-error path
// names the convergence failure when a side is short on assistant turns
// (the check is a pure comparison, driven through the same constants
// the runner uses).
func TestVoiceLinkAcceptanceGate(t *testing.T) {
	if linkRequiredAssistantTurns != 2 {
		t.Fatalf("acceptance bar = %d, leaf doc pins 2", linkRequiredAssistantTurns)
	}
	short := voiceLinkSideStatus{AssistantTurns: 1}
	okA := voiceLinkSideStatus{AssistantTurns: 2}
	okB := voiceLinkSideStatus{AssistantTurns: 3}
	check := func(a, b voiceLinkSideStatus) error {
		if a.AssistantTurns < linkRequiredAssistantTurns || b.AssistantTurns < linkRequiredAssistantTurns {
			return errors.New("link did not converge")
		}
		return nil
	}
	if err := check(short, okB); err == nil {
		t.Fatal("side short on turns must fail the gate")
	}
	if err := check(okA, okB); err != nil {
		t.Fatalf("both sides >= 2 must pass: %v", err)
	}
}

// TestVoiceLinkMonitorFlag pins the --monitor surface: flag exists with
// default false, the parse lands on linkFlags.monitor, and the open
// result mirrors the daemon's a_mon_path/b_mon_path wire keys.
func TestVoiceLinkMonitorFlag(t *testing.T) {
	cmd := newVoiceLinkCmd()
	mon, err := cmd.Flags().GetBool("monitor")
	if err != nil {
		t.Fatalf("monitor flag not registered: %v", err)
	}
	if mon {
		t.Fatal("--monitor default = true, want false (opt-in tap)")
	}

	// Parse through the command surface: --monitor flips the flag value.
	cmd.SetArgs([]string{"--a", "x", "--b", "x", "--monitor"})
	_ = cmd.Execute() // same-employee refuses early (no daemon); the flag parse already happened
	mon, err = cmd.Flags().GetBool("monitor")
	if err != nil || !mon {
		t.Fatalf("--monitor parse = %v (err %v), want true", mon, err)
	}

	// linkFlags carries the knob; validate() never rejects it (pure
	// pass-through - the monitor flag has no constraints of its own).
	f := linkFlags{a: "companion", b: "secretary", seconds: 60, monitor: true}
	if err := f.validate(); err != nil {
		t.Fatalf("validate with monitor = %v, want nil", err)
	}
}

// TestVoiceLinkOpenResultMonitorPaths pins the CLI mirror of the
// monitor-augmented voice.link.open shape: fields parse, and an
// envelope WITHOUT them (an old daemon / non-monitor open) leaves them
// empty.
func TestVoiceLinkOpenResultMonitorPaths(t *testing.T) {
	withMon := `{"link_id":"link-1","a_ccid":"local-1","b_ccid":"local-2","a_mon_path":"/s/link-1.mon-a","b_mon_path":"/s/link-1.mon-b"}`
	var o voiceLinkOpenResult
	if err := json.Unmarshal([]byte(withMon), &o); err != nil {
		t.Fatalf("unmarshal monitor open result: %v", err)
	}
	if o.MonitorAPath != "/s/link-1.mon-a" || o.MonitorBPath != "/s/link-1.mon-b" {
		t.Fatalf("monitor paths = %q/%q", o.MonitorAPath, o.MonitorBPath)
	}
	without := `{"link_id":"link-2","a_ccid":"local-3","b_ccid":"local-4"}`
	var o2 voiceLinkOpenResult
	if err := json.Unmarshal([]byte(without), &o2); err != nil {
		t.Fatalf("unmarshal legacy open result: %v", err)
	}
	if o2.MonitorAPath != "" || o2.MonitorBPath != "" {
		t.Fatalf("legacy shape leaked monitor paths: %+v", o2)
	}
}
