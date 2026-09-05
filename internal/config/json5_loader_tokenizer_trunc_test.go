package config

import "testing"

// TestPreprocessDurations_TruncatedBackslashString pins the panic fix: a
// truncated config whose last string ends in a bare backslash used to slice
// past the end of the input in BOTH tokenizer passes (slice bounds out of
// range) — a panic before hujson could produce a real parse error. The
// tokenizer must tolerate the input; hujson owns the rejection.
func TestPreprocessDurations_TruncatedBackslashString(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("preprocessDurations panicked on truncated escape: %v", r)
		}
	}()
	for _, in := range []string{
		`key: "value\`,      // unterminated string, trailing backslash
		`{a: "x\", b: 30s}`, // escaped quote then truncation
		`interval: "1h\`,    // duration-shaped string, truncated escape
		`key: "value`,       // unterminated without backslash (control)
	} {
		_ = preprocessDurations(in)
	}
}
