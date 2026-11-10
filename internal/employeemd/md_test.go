package employeemd

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bot"
)

// companionFixture mirrors e2e/suites/employee-markdown/fixtures/companion.md.
const companionFixture = `---
id: companion
name: Companion
description: Conversational companion, read-only
memory_scope: private
tools: [memory_search, platform_status]
additional_tools: [web_search]
constraints:
  max_iterations: 4
  timeout_seconds: 300
  daily_budget_cents: 150
triggers:
  - type: webhook
    enabled: true
enabled: true
constitution:
  purpose: Keep the operator company and report on recent work
  role: Companion
  autonomy_tier: tier_2_propose
  escalates_to: [user]
  constraints:
    risk_ceiling: low
    max_tokens_per_turn: 800
---
You are Companion. Speak for the ear, short sentences, plain words.
`

func TestParseCompanion(t *testing.T) {
	m, err := Parse([]byte(companionFixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if got := m["id"]; got != "companion" {
		t.Errorf("id = %v, want companion", got)
	}
	if got := m["name"]; got != "Companion" {
		t.Errorf("name = %v, want Companion", got)
	}
	if got := m["memory_scope"]; got != "private" {
		t.Errorf("memory_scope = %v, want private", got)
	}
	if got := m["prompt"]; got != "You are Companion. Speak for the ear, short sentences, plain words." {
		t.Errorf("prompt = %q", got)
	}

	// Tools must merge additional_tools into tools.
	tools, ok := m["tools"].([]any)
	if !ok {
		t.Fatalf("tools is %T, want []any", m["tools"])
	}
	wantTools := []string{"memory_search", "platform_status", "web_search"}
	if len(tools) != len(wantTools) {
		t.Fatalf("tools length = %d, want %d", len(tools), len(wantTools))
	}
	for i, want := range wantTools {
		if tools[i] != want {
			t.Errorf("tools[%d] = %v, want %v", i, tools[i], want)
		}
	}

	// Constraints: timeout_seconds -> timeout as nanosecond int.
	c, ok := m["constraints"].(map[string]any)
	if !ok {
		t.Fatalf("constraints is %T, want map[string]any", m["constraints"])
	}
	if got := c["max_iterations"]; got != 4 {
		t.Errorf("constraints.max_iterations = %v, want 4", got)
	}
	if got := c["daily_budget_cents"]; got != 150 {
		t.Errorf("constraints.daily_budget_cents = %v, want 150", got)
	}

	// Gocha check: the "constraints" sub-object must JSON-decode into
	// bot.BotConstraints with Timeout held as an integer-nanosecond value.
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("json.Marshal(constraints): %v", err)
	}
	var bc bot.BotConstraints
	if err := json.Unmarshal(raw, &bc); err != nil {
		t.Fatalf("json.Unmarshal into BotConstraints: %v", err)
	}
	if bc.Timeout != 300*time.Second {
		t.Errorf("Timeout = %v, want %v (300s)", bc.Timeout, 300*time.Second)
	}
	if bc.MaxIterations != 4 {
		t.Errorf("MaxIterations = %d, want 4", bc.MaxIterations)
	}
	if bc.DailyBudgetCents != 150 {
		t.Errorf("DailyBudgetCents = %d, want 150", bc.DailyBudgetCents)
	}

	// Constitution passed through verbatim, its nested constraints untouched.
	con, ok := m["constitution"].(map[string]any)
	if !ok {
		t.Fatalf("constitution is %T, want map[string]any", m["constitution"])
	}
	inner, ok := con["constraints"].(map[string]any)
	if !ok {
		t.Fatalf("constitution.constraints is %T, want map[string]any", con["constraints"])
	}
	if got := inner["risk_ceiling"]; got != "low" {
		t.Errorf("constitution.constraints.risk_ceiling = %v, want low", got)
	}
	if got := inner["max_tokens_per_turn"]; got != 800 {
		t.Errorf("constitution.constraints.max_tokens_per_turn = %v, want 800", got)
	}
	// Constitution constraints must NOT leak into top-level constraints.
	if _, leak := c["max_tokens_per_turn"]; leak {
		t.Errorf("constitution max_tokens_per_turn leaked into top-level constraints")
	}

	// Triggers passthrough.
	trig, ok := m["triggers"].([]any)
	if !ok || len(trig) != 1 {
		t.Fatalf("triggers = %v, want one entry", m["triggers"])
	}
	if tg, ok := trig[0].(map[string]any); ok {
		if tg["type"] != "webhook" || tg["enabled"] != true {
			t.Errorf("trigger = %v, want {type:webhook, enabled:true}", tg)
		}
	}
}

func TestParseTopLevelAliases(t *testing.T) {
	// Constraint aliases set at top level fold into constraints only when
	// the canonical constraints map does not set that key.
	input := `---
id: aliasbot
max_iterations: 7
timeout_seconds: 60
constraints:
  max_iterations: 9
---
Do the thing.
`
	m, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse aliases: %v", err)
	}
	c, ok := m["constraints"].(map[string]any)
	if !ok {
		t.Fatalf("constraints is %T", m["constraints"])
	}
	// Canonical wins for max_iterations.
	if got := c["max_iterations"]; got != 9 {
		t.Errorf("max_iterations = %v, want 9 (canonical wins)", got)
	}
	// timeout only in alias -> folded in.
	if got := c["timeout"]; got != time.Duration(60)*time.Second {
		t.Errorf("timeout = %v, want %v", got, time.Duration(60)*time.Second)
	}
	// Top-level aliases must not appear at top level.
	if _, dup := m["max_iterations"]; dup {
		t.Errorf("top-level max_iterations leaked into output")
	}
	if _, dup := m["timeout_seconds"]; dup {
		t.Errorf("top-level timeout_seconds leaked into output")
	}
}

func TestParseErrors(t *testing.T) {
	if _, err := Parse([]byte("no frontmatter here.\n")); err == nil {
		t.Fatal("expected error for missing frontmatter")
	}
	if _, err := Parse([]byte("---\nname: no id\n---\nbody\n")); err == nil {
		t.Fatal("expected error for missing id")
	}
	if _, err := Parse([]byte("---\nid: [not, a, string]\n---\nbody\n")); err == nil {
		t.Fatal("expected error for non-string id")
	}
	if _, err := Parse([]byte("---\nid: x\ntools: [\n---\nbody\n")); err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestParseUnknownKeysPreserved(t *testing.T) {
	input := `---
id: futura
custom_scope: something
status_color: "#ff00ff"
---
body
`
	m, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := m["custom_scope"]; got != "something" {
		t.Errorf("custom_scope = %v, want something", got)
	}
	if got := m["status_color"]; got != "#ff00ff" {
		t.Errorf("status_color = %v, want #ff00ff", got)
	}
}
