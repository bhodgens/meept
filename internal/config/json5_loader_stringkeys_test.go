package config

import (
	"strings"
	"testing"
)

// Regression (tree 04 leaf 01 follow-up): queue.interactive_window is a
// STRING field whose value looks like a Go duration. The loader's
// duration-to-nanos rewrite used to convert it to an integer, which the
// schema validator then rejected on reload (configui save/load roundtrip
// failed with "type mismatch: found number, expected string").
func TestPreprocessDurations_KeepsStringDurationKeys(t *testing.T) {
	in := `{
  queue: {
    interactive_window: "5m",
  },
  llm: {
    failure_policy: {
      horizon: "24h",
      base_throttle: "30s",
    },
  },
}`
	out := preprocessDurations(in)
	if !strings.Contains(out, `interactive_window: "5m"`) {
		t.Errorf("interactive_window was rewritten:\n%s", out)
	}
	if !strings.Contains(out, "horizon: 86400000000000") {
		t.Errorf("llm horizon was not rewritten to nanoseconds:\n%s", out)
	}
	if !strings.Contains(out, "base_throttle: 30000000000") {
		t.Errorf("base_throttle was not rewritten to nanoseconds:\n%s", out)
	}
}
