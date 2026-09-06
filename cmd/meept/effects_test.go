package main

import (
	"bytes"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// TestEffectsCommandStructure verifies the CLI command hierarchy: the
// effects command exists with list/reconcile subcommands and the expected
// flags (same fixture approach as changes_test.go — the cobra tree is
// exercised directly; connectDaemon is a package function, not a var, so
// RPC-level coverage comes from internal/rpc/effects_test.go and the CLI
// layer stays thin transport).
func TestEffectsCommandStructure(t *testing.T) {
	cmd := newEffectsCmd()

	if cmd == nil {
		t.Fatal("expected non-nil effects command")
	}
	if cmd.Use != "effects" {
		t.Errorf("effects command use: got %q, want %q", cmd.Use, "effects")
	}
	if cmd.Short == "" {
		t.Error("effects command should have short description")
	}

	subs := map[string]bool{}
	for _, sub := range cmd.Commands() {
		subs[sub.Name()] = true
	}
	if !subs["list"] {
		t.Error("effects command should have a 'list' subcommand")
	}
	if !subs["reconcile"] {
		t.Error("effects command should have a 'reconcile' subcommand")
	}

	listCmd := findSubcommand(t, cmd, "list")
	for _, flag := range []string{"state", "json"} {
		if listCmd.Flags().Lookup(flag) == nil {
			t.Errorf("effects list missing --%s flag", flag)
		}
	}

	reconcileCmd := findSubcommand(t, cmd, "reconcile")
	for _, flag := range []string{"complete", "abandon", "receipt", "reason"} {
		if reconcileCmd.Flags().Lookup(flag) == nil {
			t.Errorf("effects reconcile missing --%s flag", flag)
		}
	}
}

func TestEffectsCommandRegisteredOnRoot(t *testing.T) {
	root := &cobra.Command{Use: "meept"}
	root.AddCommand(newEffectsCmd())
	found := false
	for _, sub := range root.Commands() {
		if sub.Name() == "effects" {
			found = true
		}
	}
	if !found {
		t.Fatal("effects command not attached to root command tree")
	}
}

func TestHumanizeAge(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name string
		at   time.Time
		want string
	}{
		{"zero", time.Time{}, "unknown"},
		{"future", now.Add(5 * time.Minute), "unknown"},
		{"just now", now.Add(-30 * time.Second), "just now"},
		{"minutes", now.Add(-5 * time.Minute), "5m"},
		{"hours+minutes", now.Add(-(3*time.Hour + 12*time.Minute)), "3h12m"},
		{"days+hours", now.Add(-(2*24*time.Hour + 3*time.Hour)), "2d3h"},
	}
	for _, tc := range tests {
		if got := humanizeAge(tc.at); got != tc.want {
			t.Errorf("humanizeAge(%s) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestRunEffectsReconcileValidation(t *testing.T) {
	// Flag validation happens before any daemon connection: neither flag
	// and abandon-without-reason must fail fast without touching RPC.
	if err := runEffectsReconcile("some-key", false, false, "", ""); err == nil {
		t.Error("reconcile with neither flag: expected error")
	}
	if err := runEffectsReconcile("some-key", false, true, "", ""); err == nil {
		t.Error("abandon without reason: expected error")
	}
	if err := runEffectsReconcile("some-key", true, true, "", ""); err == nil {
		t.Error("both flags: expected error")
	}
}

func TestRenderEffectsTable(t *testing.T) {
	var buf bytes.Buffer
	records := []map[string]any{
		{
			"key":        "abc123",
			"state":      "claimed",
			"tool":       "push.notify",
			"claimed_at": "2026-09-05T12:00:00Z",
		},
	}
	renderEffectsTable(&buf, records)
	out := buf.String()
	for _, want := range []string{"KEY", "STATE", "TOOL", "CLAIMED", "AGE", "abc123", "claimed", "push.notify"} {
		if !bytes.Contains(buf.Bytes(), []byte(want)) {
			t.Errorf("table output missing %q:\n%s", want, out)
		}
	}
	// Effect keys are identities: never truncated.
	if !bytes.Contains(buf.Bytes(), []byte("abc123")) {
		t.Error("key truncated in table output")
	}

	buf.Reset()
	renderEffectsTable(&buf, nil)
	if !bytes.Contains(buf.Bytes(), []byte("no effects found")) {
		t.Errorf("empty table output = %q, want a lowercase no-effects hint", buf.String())
	}
}

func TestRenderEffectRecord(t *testing.T) {
	var buf bytes.Buffer
	rec := map[string]any{
		"key":                 "abc123",
		"state":               "claimed",
		"tool":                "push.notify",
		"provider_idempotent": false,
		"claimed_at":          "2026-09-05T12:00:00Z",
	}
	renderEffectRecord(&buf, rec)
	out := buf.String()
	for _, want := range []string{"key:", "state:", "tool:", "abc123", "claimed", "push.notify"} {
		if !bytes.Contains(buf.Bytes(), []byte(want)) {
			t.Errorf("record output missing %q:\n%s", want, out)
		}
	}
}
