package main

// voice_test.go - unit tests for the voice CLI's pure formatters and
// arg gates (local-leg leaf 08, T4/T6). No daemon, no sockets.

import (
	"strings"
	"testing"
)

func TestVoiceStatusTableBothLegs(t *testing.T) {
	rows := []voiceCallStatusRow{
		{CallID: "cc-phone", EmployeeID: "eve", Target: "+15551234", SinceRFC3339: "2026-11-07T10:00:00Z", Leg: "phone"},
		{CallID: "local-abc", EmployeeID: "eve", Target: "local:desk", SinceRFC3339: "2026-11-07T10:05:00Z", Leg: "local"},
	}
	out := voiceStatusTable(rows)
	for _, want := range []string{"LEG", "phone", "local", "cc-phone", "local-abc", "+15551234", "local:desk", "2026-11-07T10:00:00Z"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status table missing %q:\n%s", want, out)
		}
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
