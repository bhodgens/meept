package config

import (
	"encoding/json"
	"testing"
	"time"
)

// TestStringDurationKeys_StayStrings pins the stringDurationKeys exemption
// for every config field declared `string` that holds duration-looking
// values (oauth.refresh_interval "30m", oauth.refresh_margin, queue
// .interactive_window "5m", agent.worker_pool.idle_timeout). Without the
// exemption the tokenizer rewrote the quoted value to a nanosecond integer
// and json.Unmarshal failed "cannot unmarshal number ... of type string" —
// a real user config shape that bricked config load.
func TestStringDurationKeys_StayStrings(t *testing.T) {
	src := `{
		"refresh_interval": "30m",
		"refresh_margin": "5m",
		"interactive_window": "5m",
		"idle_timeout": "5m",
		"real_duration_field": 30s
	}`
	out := preprocessDurations(src)

	var parsed struct {
		RefreshInterval   string `json:"refresh_interval"`
		RefreshMargin     string `json:"refresh_margin"`
		InteractiveWindow string `json:"interactive_window"`
		IdleTimeout       string `json:"idle_timeout"`
		RealDuration      int64  `json:"real_duration_field"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("unmarshal preprocessed config: %v\noutput: %s", err, out)
	}
	for name, got := range map[string]string{
		"refresh_interval":   parsed.RefreshInterval,
		"refresh_margin":     parsed.RefreshMargin,
		"interactive_window": parsed.InteractiveWindow,
		"idle_timeout":       parsed.IdleTimeout,
	} {
		if got == "" {
			t.Errorf("%s: empty or rewritten — exempt key must survive as its string value (output: %s)", name, out)
		}
	}
	if parsed.RefreshInterval != "30m" {
		t.Errorf("refresh_interval = %q, want %q (exempt key rewritten)", parsed.RefreshInterval, "30m")
	}
	if parsed.IdleTimeout != "5m" {
		t.Errorf("idle_timeout = %q, want %q", parsed.IdleTimeout, "5m")
	}
	// Genuine duration-typed fields still convert through both passes.
	if parsed.RealDuration != int64(30*time.Second) {
		t.Errorf("real_duration_field = %d, want %d (duration conversion broken)",
			parsed.RealDuration, int64(30*time.Second))
	}
}
