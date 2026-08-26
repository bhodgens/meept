package llm

import "testing"

// Leaf 01 Task 1: an explicitly-set Enabled=false pointer must make the
// config non-zero so shouldSendReasoning does not silently skip it.
func TestReasoningConfig_EnabledFalseIsNotZero(t *testing.T) {
	rc := &ReasoningConfig{}
	b := false
	rc.Enabled = &b
	if rc.IsZero() {
		t.Fatal("explicit Enabled=false must not be zero-valued")
	}
}
