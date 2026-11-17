package config

// ============================================================================
// 00T-send gate (§R4) — [voice.messaging] texting-leg keys (plan
// 20261110-telnyx-texting, master.md §8, FROZEN):
//
//	webhook_base_url string   // per-message webhook base; empty = omit
//	                          // webhook_url on sends (contact channel
//	                          // derives {base}/api/v1/voice/messaging/webhook)
//	stop_reply       string   // STOP/START confirmation text; zero default
//	help_reply       string   // HELP reply text; zero default
//	inbound_enabled  bool     // D3 kill switch for the SMS leg; DEFAULT FALSE
//
// Defaults are ZERO-VALUES ONLY (01-send2 task 5: "no defaults beyond
// zero-values" — the reply WORDING defaults live in the sms pkg per the
// master serialization note, not in schema.go). D3: disabled daemon
// behavior must stay byte-identical, so inbound_enabled can never
// default true and the strings must not gain surprise wording here.
//
// Both load boundaries are pinned, matching the existing messaging_texting
// posture: JSON5 via UnmarshalJSON5 onto DefaultConfig (messaging_config_
// test.go pattern) and TOML via toml.Unmarshal (multiuser_config_test.go
// pattern, pelletier/go-toml/v2) — Load() tries JSON5 first, falls back to
// TOML for legacy meept.toml installs, so the keys must exist in BOTH tag
// sets (json:"..." + toml:"...", byte-exact snake_case).
// ============================================================================

import (
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// TestMessagingTextingKeysDefaults pins the DefaultConfig zero-values for
// the four new keys: empty strings, inbound disabled (D3 opt-in).
func TestMessagingTextingKeysDefaults(t *testing.T) {
	m := DefaultConfig().Voice.Messaging
	if m.WebhookBaseURL != "" {
		t.Errorf("webhook_base_url default = %q, want empty (no webhook derived when unset)", m.WebhookBaseURL)
	}
	if m.StopReply != "" {
		t.Errorf("stop_reply default = %q, want empty (wording defaults live in sms pkg)", m.StopReply)
	}
	if m.HelpReply != "" {
		t.Errorf("help_reply default = %q, want empty (wording defaults live in sms pkg)", m.HelpReply)
	}
	if m.InboundEnabled {
		t.Error("inbound_enabled default = true, want false (D3: SMS leg ships opt-in)")
	}
}

// TestMessagingTextingKeysJSON5RoundTrip pins the wire keys on the JSON5
// boundary: a doc with all four byte-exact snake_case keys unmarshals
// onto the struct (and the legacy keys keep working alongside).
func TestMessagingTextingKeysJSON5RoundTrip(t *testing.T) {
	cfg := DefaultConfig()
	doc := `{"voice": {"messaging": {
		"enabled": true,
		"from_number": "+15555510098",
		"profile_id": "prof-1",
		"dlr_wait_seconds": 45,
		"webhook_base_url": "https://meept.example.com",
		"stop_reply": "You have been unsubscribed",
		"help_reply": "Reply STOP to unsubscribe",
		"inbound_enabled": true
	}}}`
	if err := UnmarshalJSON5([]byte(doc), &cfg); err != nil {
		t.Fatalf("unmarshal JSON5: %v", err)
	}
	m := cfg.Voice.Messaging
	if m.WebhookBaseURL != "https://meept.example.com" {
		t.Errorf("webhook_base_url = %q after JSON5 load", m.WebhookBaseURL)
	}
	if m.StopReply != "You have been unsubscribed" {
		t.Errorf("stop_reply = %q after JSON5 load", m.StopReply)
	}
	if m.HelpReply != "Reply STOP to unsubscribe" {
		t.Errorf("help_reply = %q after JSON5 load", m.HelpReply)
	}
	if !m.InboundEnabled {
		t.Error("inbound_enabled = false after JSON5 load with true in doc")
	}
	// Legacy keys unaffected by the new field set (same block, byte-exact).
	if !m.Enabled || m.FromNumber != "+15555510098" || m.ProfileID != "prof-1" || m.DLRWaitSeconds != 45 {
		t.Errorf("legacy messaging fields regressed: %+v", m)
	}
}

// TestMessagingTextingKeysTOMLRoundTrip pins the same keys on the legacy
// TOML boundary (Load()'s fallback path): toml.Unmarshal honours the
// toml tags, so meept.toml installs get the identical surface.
func TestMessagingTextingKeysTOMLRoundTrip(t *testing.T) {
	cfg := DefaultConfig()
	doc := "[voice.messaging]\n" +
		"enabled = true\n" +
		"webhook_base_url = \"https://meept.example.com\"\n" +
		"stop_reply = \"You have been unsubscribed\"\n" +
		"help_reply = \"Reply STOP to unsubscribe\"\n" +
		"inbound_enabled = true\n"
	if err := toml.Unmarshal([]byte(doc), cfg); err != nil {
		t.Fatalf("toml.Unmarshal: %v", err)
	}
	m := cfg.Voice.Messaging
	if m.WebhookBaseURL != "https://meept.example.com" {
		t.Errorf("webhook_base_url = %q after TOML load", m.WebhookBaseURL)
	}
	if m.StopReply != "You have been unsubscribed" {
		t.Errorf("stop_reply = %q after TOML load", m.StopReply)
	}
	if m.HelpReply != "Reply STOP to unsubscribe" {
		t.Errorf("help_reply = %q after TOML load", m.HelpReply)
	}
	if !m.InboundEnabled {
		t.Error("inbound_enabled = false after TOML load with true in doc")
	}
}

// TestMessagingTextingKeysOmittedInheritsDefaults pins the inheritance
// rule at both boundaries: an omitted/absent messaging block keeps every
// DefaultConfig value — new keys zero, legacy wait 30 (unmarshal runs
// ONTO DefaultConfig, never a zero struct).
func TestMessagingTextingKeysOmittedInheritsDefaults(t *testing.T) {
	// JSON5 boundary: [voice] present, messaging omitted entirely.
	cfg := DefaultConfig()
	if err := UnmarshalJSON5([]byte(`{"voice": {"enabled": true}}`), &cfg); err != nil {
		t.Fatalf("unmarshal JSON5: %v", err)
	}
	m := cfg.Voice.Messaging
	if m.WebhookBaseURL != "" || m.StopReply != "" || m.HelpReply != "" || m.InboundEnabled {
		t.Errorf("omitted messaging block gained non-default texting keys: %+v", m)
	}
	if m.DLRWaitSeconds != 30 {
		t.Errorf("omitted messaging block lost wait default: %d, want 30", m.DLRWaitSeconds)
	}

	// TOML boundary: unrelated key set; messaging block absent.
	tcfg := DefaultConfig()
	if err := toml.Unmarshal([]byte("[voice]\nenabled = true\n"), tcfg); err != nil {
		t.Fatalf("toml.Unmarshal: %v", err)
	}
	tm := tcfg.Voice.Messaging
	if tm.WebhookBaseURL != "" || tm.StopReply != "" || tm.HelpReply != "" || tm.InboundEnabled {
		t.Errorf("TOML: omitted messaging block gained non-default texting keys: %+v", tm)
	}
}

// TestMessagingTextingKeysExplicitFalseHonored pins D3's tri-state
// posture on inbound_enabled: an explicit false in the doc is honored
// (never force-defaulted true), matching the cluster-security normalize
// rule the repo already follows for opt-in booleans.
func TestMessagingTextingKeysExplicitFalseHonored(t *testing.T) {
	cfg := DefaultConfig()
	doc := `{"voice": {"messaging": {"inbound_enabled": false}}}`
	if err := UnmarshalJSON5([]byte(doc), &cfg); err != nil {
		t.Fatalf("unmarshal JSON5: %v", err)
	}
	if cfg.Voice.Messaging.InboundEnabled {
		t.Error("explicit inbound_enabled=false was flipped true by the load boundary")
	}
}
