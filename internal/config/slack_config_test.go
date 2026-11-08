package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSlackConfig_DisabledByDefault pins the C2/T4 default-off contract:
// the [comm.slack] block ships OFF; enabling the approval channel is an
// explicit user decision.
func TestSlackConfig_DisabledByDefault(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Slack.Enabled {
		t.Error("comm.slack should be disabled by default")
	}
	if cfg.Slack.BotToken != "" || cfg.Slack.SigningSecret != "" || cfg.Slack.ChannelID != "" {
		t.Errorf("secret-bearing fields must default empty, got %+v", cfg.Slack)
	}
	if len(cfg.Slack.AllowedUserIDs) != 0 {
		t.Errorf("allowed_user_ids should default empty (fail closed), got %v", cfg.Slack.AllowedUserIDs)
	}
	if cfg.Slack.APIBaseURL != "https://slack.com/api" {
		t.Errorf("api_base_url = %q, want https://slack.com/api", cfg.Slack.APIBaseURL)
	}
}

// TestSlackConfig_JSON5RoundTrip proves the block loads from the JSON5
// file format via LoadJSON5 (the loader the template test exercises),
// including defaults-fill semantics on a partial section.
func TestSlackConfig_JSON5RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.json5")
	content := `{
  // slack approval channel (JSON5: comments + trailing commas)
  "slack": {
    "enabled": true,
    "bot_token": "xoxb-test-token",
    "signing_secret": "sigshhh",
    "channel_id": "C0ABC123",
    "allowed_user_ids": ["U1AAA", "U2BBB"],
  },
}
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	if err := LoadJSON5(path, cfg); err != nil {
		t.Fatalf("load json5: %v", err)
	}
	s := cfg.Slack
	if !s.Enabled {
		t.Error("enabled not parsed")
	}
	if s.BotToken != "xoxb-test-token" {
		t.Errorf("bot_token = %q", s.BotToken)
	}
	if s.SigningSecret != "sigshhh" {
		t.Errorf("signing_secret = %q", s.SigningSecret)
	}
	if s.ChannelID != "C0ABC123" {
		t.Errorf("channel_id = %q", s.ChannelID)
	}
	if len(s.AllowedUserIDs) != 2 || s.AllowedUserIDs[0] != "U1AAA" || s.AllowedUserIDs[1] != "U2BBB" {
		t.Errorf("allowed_user_ids = %v", s.AllowedUserIDs)
	}
	// Partial section: the omitted api_base_url keeps the DefaultConfig value.
	if s.APIBaseURL != "https://slack.com/api" {
		t.Errorf("api_base_url default-fill = %q, want https://slack.com/api", s.APIBaseURL)
	}
}

// TestSlackConfig_ValidateEnabled pins the load-time contract: enabled
// with any required key missing fails and names EVERY missing key; the
// disabled block never fails.
func TestSlackConfig_ValidateEnabled(t *testing.T) {
	if err := (SlackConfig{}).ValidateEnabled(); err != nil {
		t.Errorf("disabled zero config must pass, got %v", err)
	}
	err := SlackConfig{Enabled: true, BotToken: "  ", ChannelID: ""}.ValidateEnabled()
	if err == nil {
		t.Fatal("enabled with missing keys must fail")
	}
	for _, want := range []string{"bot_token", "signing_secret", "channel_id"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should name %s: %v", want, err)
		}
	}
	// Whitespace-only counts as empty (TrimSpace).
	if err := (SlackConfig{Enabled: true, BotToken: "x", SigningSecret: "y", ChannelID: "z"}).ValidateEnabled(); err != nil {
		t.Errorf("complete enabled config must pass, got %v", err)
	}
}
