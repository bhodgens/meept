package main

// voice_test.go - unit tests for the voice CLI's pure formatters and
// arg gates (local-leg leaf 08, T4/T6). No daemon, no sockets.

import (
	"strings"
	"testing"
)

func TestVoiceStatusTableBothLegs(t *testing.T) {
	rows := []voiceCallStatusRow{
		// contact-display leaf 03 (O1): a phone row on the leaf 02 wire
		// shape carries the display pair, not the raw target.
		{CallID: "cc-phone", EmployeeID: "eve", TargetName: "Jane Doe", TargetDisplay: "Jane Doe (•••5567)", SinceRFC3339: "2026-11-07T10:00:00Z", Leg: "phone"},
		// The local leg's "local:<label>" passes through verbatim in the
		// display pair (leaf 02); the legacy Target fallback is pinned
		// separately in TestVoiceStatusTableLegacyTarget.
		{CallID: "local-abc", EmployeeID: "eve", TargetName: "local:desk", TargetDisplay: "local:desk", SinceRFC3339: "2026-11-07T10:05:00Z", Leg: "local"},
	}
	out := voiceStatusTable(rows)
	for _, want := range []string{"LEG", "phone", "local", "cc-phone", "local-abc", "Jane Doe (•••5567)", "local:desk", "2026-11-07T10:00:00Z"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status table missing %q:\n%s", want, out)
		}
	}
}

// TestVoiceStatusTableLegacyTarget pins the old-server fallback (leaf 03,
// O1): a row with ONLY the deprecated raw target renders it verbatim; a
// row with no target info at all renders "unknown".
func TestVoiceStatusTableLegacyTarget(t *testing.T) {
	out := voiceStatusTable([]voiceCallStatusRow{
		{CallID: "old", EmployeeID: "eve", Target: "local:desk"},
		{CallID: "bare", EmployeeID: "eve"},
	})
	if !strings.Contains(out, "local:desk") {
		t.Fatalf("legacy target fallback not rendered:\n%s", out)
	}
	if !strings.Contains(out, "unknown") {
		t.Fatalf("unknown fallback not rendered:\n%s", out)
	}
}

func TestVoiceStatusTableEmpty(t *testing.T) {
	out := voiceStatusTable(nil)
	if !strings.Contains(out, "no live voice calls") {
		t.Fatalf("empty table message wrong: %q", out)
	}
	if strings.Contains(out, "LEG") {
		t.Fatalf("empty table must not render a header: %q", out)
	}
}

func TestVoiceStatusTableDefaults(t *testing.T) {
	// Empty leg falls back to phone; empty since renders "-".
	out := voiceStatusTable([]voiceCallStatusRow{{CallID: "x", EmployeeID: "e", Target: "t"}})
	if !strings.Contains(out, "phone") || !strings.Contains(out, "-") {
		t.Fatalf("defaults not applied: %q", out)
	}
}

func TestVoiceLocalRequiresEmployee(t *testing.T) {
	cmd := newVoiceLocalCmd()
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--employee is required") {
		t.Fatalf("want --employee gate, got %v", err)
	}
}

func TestVoiceCmdRegistered(t *testing.T) {
	cmd := newVoiceCmd()
	if cmd.Name() != "voice" {
		t.Fatalf("command name: %q", cmd.Name())
	}
	foundLocal, foundStatus := false, false
	for _, sub := range cmd.Commands() {
		switch sub.Name() {
		case "local":
			foundLocal = true
		case "status":
			foundStatus = true
		}
	}
	if !foundLocal || !foundStatus {
		t.Fatalf("voice subcommands missing: local=%v status=%v", foundLocal, foundStatus)
	}
}

// Per-call timing override flags (voice local): validation + flag wiring.
// The *Raw fields carry the cobra sentinel (-1 = flag absent); validate()
// converts them to *int overrides (nil = no override, byte-identical
// behavior). Negative ms values and smart-turn mode outside 0..2 are
// refused with named errors.

func TestLocalFlagsTimingOverridesValid(t *testing.T) {
	f := localFlags{
		employee:              "eve",
		turnConfirmMsRaw:      900,
		smartTurnModeRaw:      2,
		deepgramEndpointMsRaw: 250,
	}
	if err := f.validate(); err != nil {
		t.Fatalf("valid overrides refused: %v", err)
	}
	tc, stm, dg := f.timingOverrides()
	if tc == nil || *tc != 900 || stm == nil || *stm != 2 || dg == nil || *dg != 250 {
		t.Fatalf("overrides not converted to pointers: %v %v %v", tc, stm, dg)
	}
}

func TestLocalFlagsTimingOverridesAbsent(t *testing.T) {
	// -1 sentinel = flag absent (cobra default), not an explicit 0.
	f := localFlags{employee: "eve", turnConfirmMsRaw: -1, smartTurnModeRaw: -1, deepgramEndpointMsRaw: -1}
	if err := f.validate(); err != nil {
		t.Fatalf("absent overrides must be valid: %v", err)
	}
	tc, stm, dg := f.timingOverrides()
	if tc != nil || stm != nil || dg != nil {
		t.Fatal("absent flags must leave overrides nil")
	}
}

func TestLocalFlagsTimingOverridesInvalid(t *testing.T) {
	neg, big, zero := -2, 3, 0
	tests := []struct {
		name string
		f    localFlags
		want string
	}{
		{"negative turn-confirm-ms", localFlags{employee: "e", turnConfirmMsRaw: neg, smartTurnModeRaw: -1, deepgramEndpointMsRaw: -1}, "turn_confirm_ms"},
		{"negative deepgram-endpoint-ms", localFlags{employee: "e", turnConfirmMsRaw: -1, smartTurnModeRaw: -1, deepgramEndpointMsRaw: neg}, "deepgram_endpoint_ms"},
		{"smart-turn-mode too high", localFlags{employee: "e", turnConfirmMsRaw: -1, smartTurnModeRaw: big, deepgramEndpointMsRaw: -1}, "smart_turn_mode"},
		{"smart-turn-mode negative", localFlags{employee: "e", turnConfirmMsRaw: -1, smartTurnModeRaw: neg, deepgramEndpointMsRaw: -1}, "smart_turn_mode"},
		{"explicit zero ok", localFlags{employee: "e", turnConfirmMsRaw: zero, smartTurnModeRaw: zero, deepgramEndpointMsRaw: zero}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.f.validate()
			if tt.want == "" {
				if err != nil {
					t.Fatalf("want valid, got %v", err)
				}
				tc, stm, dg := tt.f.timingOverrides()
				if tc == nil || *tc != 0 || stm == nil || *stm != 0 || dg == nil || *dg != 0 {
					t.Fatal("explicit zero must be a non-nil pointer to 0")
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("want error naming %q, got %v", tt.want, err)
			}
		})
	}
}

func TestVoiceLocalCmdHasTimingFlags(t *testing.T) {
	cmd := newVoiceLocalCmd()
	for _, name := range []string{"turn-confirm-ms", "smart-turn-mode", "deepgram-endpoint-ms"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("voice local missing --%s flag", name)
		}
	}
}
