package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Tests for the tokenizer-based preprocessDurations (compact JSON5 + arrays).
// The pre-tokenizer implementation was line-based: it anchored on the FIRST
// colon of each line, so compact single-line objects, multiple members per
// line, and quoted values earlier on the same line all misbehaved.

func TestPreprocessDurations_CompactObject(t *testing.T) {
	in := `{retry:30s,warn:1h,enabled:true}`
	out := preprocessDurations(in)
	for _, want := range []string{"retry:30000000000", "warn:3600000000000"} {
		if !strings.Contains(out, want) {
			t.Errorf("compact object not rewritten, missing %q in:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "enabled:true") {
		t.Errorf("non-duration member disturbed:\n%s", out)
	}
}

func TestPreprocessDurations_QuotedValueBeforeBareOnSameLine(t *testing.T) {
	// Regression (aa7eecfe follow-up): the line-based pass anchored on the
	// FIRST colon and skipped the whole line when it saw a quoted value —
	// the bare 45s was never rewritten. The tokenizer must rewrite both.
	in := `{horizon: "24h", base_throttle: 45s}`
	out := preprocessDurations(in)
	if !strings.Contains(out, "horizon: 86400000000000") {
		t.Errorf("quoted duration before bare token not converted:\n%s", out)
	}
	if !strings.Contains(out, "base_throttle: 45000000000") {
		t.Errorf("bare duration after quoted value not converted:\n%s", out)
	}
}

func TestPreprocessDurations_MultipleBareDurationsOneLine(t *testing.T) {
	in := `{retry: 30s, warn: 1h, cool: 250ms}`
	out := preprocessDurations(in)
	for _, want := range []string{"retry: 30000000000", "warn: 3600000000000", "cool: 250000000"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestPreprocessDurations_DurationArrays(t *testing.T) {
	in := `{backoff: [30s, 1h, 90.5s], labels: ["5m", "10m"], mixed: [30s, "5m", 7d]}`
	out := preprocessDurations(in)
	// Bare array elements are quoted by the scanner (they become numbers
	// only after convertQuotedDurations, which anchors on ':' and therefore
	// leaves array elements as quoted strings — the value-level contract is
	// that bare tokens are made parseable, not that arrays become numbers).
	for _, want := range []string{`backoff: ["30s", "1h", "90.5s"]`, `labels: ["5m", "10m"]`, `mixed: ["30s", "5m", "7d"]`} {
		if !strings.Contains(out, want) {
			t.Errorf("array not normalized, missing %q in:\n%s", want, out)
		}
	}
}

func TestPreprocessDurations_CompactStringDurationKeyExempt(t *testing.T) {
	// The stringDurationKeys exemption must survive compact form: the
	// line-based quotedDurationToNanos read the key from the FIRST colon on
	// the line, so {queue:{interactive_window:"5m"},llm:{...}} exempted
	// "queue" instead of interactive_window and rewrote the string field.
	in := `{queue:{interactive_window:"5m"},llm:{horizon:"24h"}}`
	out := preprocessDurations(in)
	if !strings.Contains(out, `interactive_window:"5m"`) {
		t.Errorf("interactive_window rewritten in compact form:\n%s", out)
	}
	if !strings.Contains(out, "horizon:86400000000000") {
		t.Errorf("non-exempt compact duration not converted:\n%s", out)
	}
}

func TestPreprocessDurations_CommentsAndFakeKeys(t *testing.T) {
	in := `{
		// retry: 30s — a duration mention in a comment, must stay a comment
		/* horizon: "24h" block-comment mention */
		interval: 2h, // trailing comment with warn: 1h
		note: "the key timeout: 45s appears inside this string",
	}`
	out := preprocessDurations(in)
	if strings.Count(out, "30000000000") != 0 {
		t.Errorf("comment content converted to nanoseconds:\n%s", out)
	}
	if !strings.Contains(out, "// retry: 30s") {
		t.Errorf("line comment disturbed:\n%s", out)
	}
	if !strings.Contains(out, "interval: 7200000000000") {
		t.Errorf("real value not converted:\n%s", out)
	}
	if !strings.Contains(out, "the key timeout: 45s appears inside this string") {
		t.Errorf("string content mangled:\n%s", out)
	}
}

func TestPreprocessDurations_DurationLikeTextInStringsUntouched(t *testing.T) {
	// Prose containing duration text must never be rewritten. The `label`
	// case pins the aa7eecfe contract: a quoted PURE duration in value
	// position ("1h30m" as the whole value) IS converted to nanoseconds —
	// same as horizon: "24h" — while prose with embedded duration words is
	// not a duration token and passes through byte-identically.
	in := `{description: "runs about 1h and 30m total"}`
	out := preprocessDurations(in)
	if out != in {
		t.Errorf("string contents rewritten:\nin:  %s\nout: %s", in, out)
	}
	quoted := `{description: "runs about 1h", label: "1h30m"}`
	got := preprocessDurations(quoted)
	if !strings.Contains(got, `"runs about 1h"`) {
		t.Errorf("prose string rewritten:\n%s", got)
	}
	if !strings.Contains(got, `label: 5400000000000`) {
		t.Errorf("pure duration value not converted (aa7eecfe contract):\n%s", got)
	}
}

func TestPreprocessDurations_CompactExemptBareValue(t *testing.T) {
	// Bare duration under an exempt key: the old pass quoted it (making the
	// value parse) but left it a string. Preserve that.
	in := `{queue:{interactive_window:5m}}`
	out := preprocessDurations(in)
	if !strings.Contains(out, `interactive_window:"5m"`) {
		t.Errorf("exempt bare duration not quoted as string:\n%s", out)
	}
}

func TestPreprocessDurations_CompoundAndFractionalDurations(t *testing.T) {
	in := `{a: 1h30m, b: 90.5s, c: 500us, d: 1d}`
	out := preprocessDurations(in)
	for _, want := range []string{"a: 5400000000000", "b: 90500000000", "c: 500000", "d: 86400000000000"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// TestLoadJSON5_CompactDurations end-to-end: a fully compact single-line
// JSON5 config (the shape the line-based pass could not handle) must load
// through the daemon -c path. Keys are quoted: hujson.Standardize (the
// actual parser) requires quoted member names, so bare keys are only legal
// at the transform level (see the TestPreprocessDurations_* unit tests).
func TestLoadJSON5_CompactDurations(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.json5")
	content := `{"skills":{"evolver":{"enabled":true,"interval":90m}}}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var cfg durationProbeConfig
	if err := LoadJSON5(path, &cfg); err != nil {
		t.Fatalf("LoadJSON5 compact: %v", err)
	}
	if got := cfg.Skills.Evolver.Interval; got != 90*time.Minute {
		t.Fatalf("interval = %v, want 90m", got)
	}
}
