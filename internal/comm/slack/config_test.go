package slack

import "testing"

func TestSlackConfigValidateDefaultOff(t *testing.T) {
	// The zero config (no [comm.slack] block at all) must validate clean -
	// the default-off invariant (T1/T4).
	if err := (SlackConfig{}).Validate(); err != nil {
		t.Fatalf("zero config Validate() = %v, want nil", err)
	}
	enabledButEmpty := SlackConfig{Enabled: true}
	if err := enabledButEmpty.Validate(); err == nil {
		t.Fatal("enabled=true with empty token/secret/channel: want error, got nil")
	}
}

func TestSlackConfigValidateNamesMissingKeys(t *testing.T) {
	cfg := SlackConfig{Enabled: true, BotToken: "xoxb-test"}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("want error for missing signing_secret + channel_id")
	}
	for _, key := range []string{"signing_secret", "channel_id"} {
		if !contains(err.Error(), key) {
			t.Errorf("error %q does not name missing key %q", err.Error(), key)
		}
	}
}

func TestSlackConfigValidateClean(t *testing.T) {
	cfg := SlackConfig{
		Enabled:       true,
		BotToken:      "xoxb-test",
		SigningSecret: "secret",
		ChannelID:     "C123",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("full config Validate() = %v, want nil", err)
	}
}

func TestSlackConfigAPIURLDefaultAndOverride(t *testing.T) {
	zero := SlackConfig{}
	if got := zero.apiURL(); got != DefaultAPIBaseURL {
		t.Errorf("default apiURL = %q, want %q", got, DefaultAPIBaseURL)
	}
	over := SlackConfig{APIBaseURL: "http://127.0.0.1:1/"}
	if got := over.apiURL(); got != "http://127.0.0.1:1" {
		t.Errorf("override apiURL = %q (trailing slash must be trimmed)", got)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
