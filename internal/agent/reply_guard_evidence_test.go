package agent

import "testing"

// Pin for the 2026-09-18 scopes-2 gate finding: the async task-completion
// evidence envelope ({"evidence": ["job job-... completed by agent coder:
// ..."]}) shipped verbatim as the user-facing chat reply. The tool-result
// dump detector must treat "evidence" as an identity key.
func TestToolResultJSONKey_EvidenceEnvelope(t *testing.T) {
	reply := `{"evidence":["job job-20260918105646.742634000-0002 completed by agent coder: {\"status\": \"completed\"}"],"status":"completed"}`
	key, ok := toolResultJSONKey(reply)
	if !ok {
		t.Fatalf("evidence envelope not detected as machine-shaped dump (key=%q)", key)
	}
	if key != `"evidence"` {
		t.Fatalf("expected evidence key, got %q", key)
	}
}
