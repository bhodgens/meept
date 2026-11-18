package llm

import (
	"log/slog"
	"strings"
	"testing"
)

// stickyScanner flags any prompt containing "SECRET_MARKER".
type stickyScanner struct{}

func (stickyScanner) Scan(text string) []string {
	if strings.Contains(text, "SECRET_MARKER") {
		return []string{"aws_key"}
	}
	return nil
}

// A balanced-sticky alias must NOT bypass the C4 posture hard boundary:
// a sensitive prompt pinned to a public_only model must be re-pinned to the
// private model, and an all-public_only alias must refuse the turn.
func TestResolveForAlias_StickyHonorsPosture(t *testing.T) {
	r := NewResolver(createTestConfig(), slog.New(slog.DiscardHandler))
	r.SetPostureScanner(stickyScanner{})
	r.SetResolvePostureContext(func(string) string { return "SECRET_MARKER in prompt" })

	publicModel := &ModelConfig{ProviderID: "zai", ModelID: "public", DataPosture: PosturePublicOnly}
	privateModel := &ModelConfig{ProviderID: "zai", ModelID: "private", DataPosture: PosturePrivate}
	r.aliases["sticky-posture"] = &AliasEntry{
		Models:                 []*ModelConfig{publicModel, privateModel},
		BalancedStickyRequests: true,
	}

	// Pin the session to index 0 (public_only), as a prior non-sensitive
	// turn would have. A sensitive resolve must move to the private model.
	health := r.getOrCreateHealth("sticky-posture")
	if health.StickyPins == nil {
		health.StickyPins = make(map[string]StickyPin)
	}
	health.StickyPins["sess-sensitive"] = StickyPin{Index: 0}

	got, err := r.ResolveForAlias("sticky-posture", "sess-sensitive")
	if err != nil {
		t.Fatalf("ResolveForAlias(sticky, sensitive): %v", err)
	}
	if got != privateModel {
		t.Fatalf("sticky sensitive resolve = %q, want private model", got.ModelID)
	}
	pin := health.StickyPins["sess-sensitive"]
	if pin.Index != 1 {
		t.Fatalf("sticky pin index = %d, want 1 (re-pinned to cleared model)", pin.Index)
	}

	// All-public_only alias: a sensitive turn has no legal destination.
	r.aliases["sticky-all-public"] = &AliasEntry{
		Models:                 []*ModelConfig{publicModel},
		BalancedStickyRequests: true,
	}
	allPublicHealth := r.getOrCreateHealth("sticky-all-public")
	if allPublicHealth.StickyPins == nil {
		allPublicHealth.StickyPins = make(map[string]StickyPin)
	}
	allPublicHealth.StickyPins["sess2"] = StickyPin{Index: 0}
	if _, err := r.ResolveForAlias("sticky-all-public", "sess2"); err == nil {
		t.Fatal("sticky sensitive resolve on all-public_only alias must refuse the turn")
	}
}
