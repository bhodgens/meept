package models

import (
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/tui/types"
)

// TestMemoryListItemBridgeBadge: bridge-tagged rows render the [bridge]
// marker with the verified flag and evidence count (branch 09 B06);
// non-bridge rows keep the plain [type] description.
func TestMemoryListItemBridgeBadge(t *testing.T) {
	bridge := memoryListItem{item: types.MemoryItem{
		ID:             "bridge-1",
		Content:        "projectx is led by alice",
		Type:           "bridge",
		RelevanceScore: 0.9,
		Metadata: map[string]any{
			"unit_type":    "bridge",
			"verified":     "true",
			"evidence_ids": `["mem-1","mem-2","mem-3"]`,
		},
	}}
	desc := bridge.Description()
	if !strings.Contains(desc, "[bridge]") {
		t.Errorf("bridge row missing [bridge] marker: %q", desc)
	}
	if !strings.Contains(desc, "verified: yes") {
		t.Errorf("bridge row missing verified flag: %q", desc)
	}
	if !strings.Contains(desc, "evidence: 3") {
		t.Errorf("bridge row missing evidence count: %q", desc)
	}
	// Lowercase UI rule: no capital letters in the description.
	if strings.ToLower(desc) != desc {
		t.Errorf("bridge description not lowercase: %q", desc)
	}

	unverified := memoryListItem{item: types.MemoryItem{
		ID:             "bridge-2",
		Content:        "projectx uses postgres",
		RelevanceScore: 0.8,
		Metadata: map[string]any{
			"unit_type":    "bridge",
			"verified":     "false",
			"evidence_ids": `["mem-1"]`,
		},
	}}
	desc = unverified.Description()
	if !strings.Contains(desc, "verified: no") || !strings.Contains(desc, "evidence: 1") {
		t.Errorf("unverified bridge description wrong: %q", desc)
	}

	source := memoryListItem{item: types.MemoryItem{
		ID:             "mem-1",
		Content:        "alice leads the projectx migration",
		Type:           "episodic",
		RelevanceScore: 0.7,
	}}
	desc = source.Description()
	if strings.Contains(desc, "bridge") {
		t.Errorf("source row must not carry the bridge marker: %q", desc)
	}
	if desc != "[episodic] score: 0.70" {
		t.Errorf("source description changed: %q", desc)
	}
}

// TestBridgeEvidenceCountShapes: the helper tolerates the JSON-string form
// the BridgeStore writes and the pre-decoded array form.
func TestBridgeEvidenceCountShapes(t *testing.T) {
	if got := bridgeEvidenceCount(map[string]any{"evidence_ids": `["a","b"]`}); got != 2 {
		t.Errorf("json-string form: got %d, want 2", got)
	}
	if got := bridgeEvidenceCount(map[string]any{"evidence_ids": []any{"a", "b", "c"}}); got != 3 {
		t.Errorf("array form: got %d, want 3", got)
	}
	if got := bridgeEvidenceCount(map[string]any{}); got != 0 {
		t.Errorf("missing key: got %d, want 0", got)
	}
}

// TestBridgeVerifiedTextShapes covers the verified-flag rendering.
func TestBridgeVerifiedTextShapes(t *testing.T) {
	cases := map[string]string{
		"true":    "yes",
		"false":   "no",
		"":        "unknown",
		"garbage": "unknown",
	}
	for in, want := range cases {
		if got := bridgeVerifiedText(in); got != want {
			t.Errorf("bridgeVerifiedText(%q) = %q, want %q", in, got, want)
		}
	}
}
