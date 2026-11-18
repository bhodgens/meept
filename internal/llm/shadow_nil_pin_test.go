package llm

import (
	"context"
	"log/slog"
	"testing"
)

// ResolveShadow with no alias on a resolver whose default model is unset
// must return an empty preview, not panic on a nil *ModelConfig (C6:
// never error/panic on a legitimately-unroutable preview).
func TestResolveShadow_NoDefaultModelDoesNotPanic(t *testing.T) {
	cfg := createTestConfig()
	cfg.Model = "" // ResolveModelRef("") returns nil
	r := NewResolver(cfg, slog.New(slog.DiscardHandler))

	dec, err := r.ResolveShadow(context.Background(), ShadowRequest{})
	if err != nil {
		t.Fatalf("ResolveShadow(no alias, no default): %v", err)
	}
	if dec == nil {
		t.Fatal("ResolveShadow returned nil decision")
	}
	if len(dec.Candidates) != 0 {
		t.Fatalf("candidates = %d, want 0", len(dec.Candidates))
	}
}
