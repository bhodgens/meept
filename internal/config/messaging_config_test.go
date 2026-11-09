package config

// Tests for the [voice.messaging] block (contact-channels plan leaf 12
// T5): DefaultConfig defaults present, and a config load that omits the
// block inherits them (unmarshal runs ONTO DefaultConfig).

import (
	"testing"
)

// TestDefaultConfigMessagingDefaults pins the DefaultConfig values:
// disabled by default, DLR wait 30.
func TestDefaultConfigMessagingDefaults(t *testing.T) {
	cfg := DefaultConfig()
	m := cfg.Voice.Messaging
	if m.Enabled {
		t.Fatal("voice.messaging.enabled default = true, want false (opt-in)")
	}
	if m.DLRWaitSeconds != 30 {
		t.Fatalf("voice.messaging.dlr_wait_seconds default = %d, want 30", m.DLRWaitSeconds)
	}
	if m.FromNumber != "" || m.ProfileID != "" {
		t.Fatalf("from_number/profile_id defaults = %q/%q, want empty", m.FromNumber, m.ProfileID)
	}
}

// TestMessagingConfigRoundTrip pins the wire keys: a JSON5 object with
// the documented keys unmarshals onto the struct (keys byte-exact).
func TestMessagingConfigRoundTrip(t *testing.T) {
	cfg := DefaultConfig()
	doc := `{"voice": {"messaging": {"enabled": true, "from_number": "+15550001111", "profile_id": "prof-1", "dlr_wait_seconds": 45}}}`
	if err := UnmarshalJSON5([]byte(doc), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	m := cfg.Voice.Messaging
	if !m.Enabled || m.FromNumber != "+15550001111" || m.ProfileID != "prof-1" || m.DLRWaitSeconds != 45 {
		t.Fatalf("messaging after load = %+v, want enabled from +15550001111 prof-1 wait 45", m)
	}
}

// TestMessagingConfigOmittedInheritsDefaults pins the inheritance rule:
// an omitted messaging block keeps the DefaultConfig values (the load
// unmarshal runs onto DefaultConfig, never a zero struct).
func TestMessagingConfigOmittedInheritsDefaults(t *testing.T) {
	cfg := DefaultConfig()
	doc := `{"voice": {"enabled": true}}`
	if err := UnmarshalJSON5([]byte(doc), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.Voice.Messaging.DLRWaitSeconds != 30 {
		t.Fatalf("omitted messaging block lost its default wait: %+v", cfg.Voice.Messaging)
	}
}
