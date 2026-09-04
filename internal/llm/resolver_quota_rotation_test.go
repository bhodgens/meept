package llm

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"
)

// TestResolver_RotateToNextModel_AfterQuotaBlock is the resolver-side
// regression test for the agnes-429 failover bug: when the first alias
// candidate (agnes) 429s with a quota-shaped body, the agent loop blocks it
// via BlockQuotaEntry/BlockQuotaCredential and then calls RotateToNextModel.
// Rotation must land on the NEXT candidate — skipping the blocked one — and
// must NOT consume the failed 429 as alias ill-health (ConsecutiveFails
// stays 0: no RecordAliasFailure on the quota path).
func TestResolver_RotateToNextModel_AfterQuotaBlock(t *testing.T) {
	r := newTestResolver(enabledQuotaCfg())

	// Block the current head (agnes stand-in: zai/glm-4.7) via the exact two
	// calls the agent loop's quota branch makes.
	agnes := r.aliases["coder"].Models[0]
	if agnes.ProviderID != "zai" || agnes.ModelID != "glm-4.7" {
		t.Fatalf("test fixture drift: expected coder[0]=zai/glm-4.7, got %s/%s", agnes.ProviderID, agnes.ModelID)
	}
	quotaErr := &QuotaResetError{
		ProviderID: agnes.ProviderID,
		ModelID:    agnes.ModelID,
		Code:       "usage_limit_reached",
		StatusCode: http.StatusTooManyRequests,
		ResetAt:    time.Now().Add(12 * time.Hour),
		MaxWait:    24 * time.Hour,
	}
	r.BlockQuotaEntry("coder", quotaErr.ProviderID, quotaErr.ModelID, quotaErr.ResetAt)
	r.BlockQuotaCredential("coder", QuotaCredentialKey(quotaErr.ProviderID, agnes), quotaErr.ResetAt)

	// Alias health must be untouched by the quota marks.
	if _, fails, _, ok := r.GetAliasHealth("coder"); ok && fails != 0 {
		t.Fatalf("ConsecutiveFails = %d after quota marks, want 0 (quota is not alias ill-health)", fails)
	}

	// Rotate: the blocked agnes candidate must be skipped, landing on
	// ollama/llama3.2 (the local candidate whose endpoint may be down —
	// rotation attempts it regardless; that attempt surfaces as a plain
	// error and is the loop's concern, not the resolver's).
	next, err := r.RotateToNextModel("coder")
	if err != nil {
		t.Fatalf("RotateToNextModel after agnes quota block: %v", err)
	}
	if next.ProviderID != "ollama" || next.ModelID != "llama3.2" {
		t.Errorf("rotation after quota block = %s/%s, want ollama/llama3.2 (next candidate)", next.ProviderID, next.ModelID)
	}
	if idx, _, _, ok := r.GetAliasHealth("coder"); !ok || idx != 1 {
		t.Errorf("CurrentIndex = %d (ok=%v), want 1", idx, ok)
	}

	// A subsequent resolve must NOT serve the still-blocked agnes model.
	mc, err := r.ResolveForAlias("coder", "")
	if err != nil {
		t.Fatalf("ResolveForAlias after rotation: %v", err)
	}
	if mc.ProviderID == "zai" && mc.ModelID == "glm-4.7" {
		t.Errorf("resolve served the quota-blocked candidate agnes/zai/glm-4.7")
	}
}

// TestResolver_RotateToNextModel_AllQuotaBlockedAfter429 verifies the
// terminal case of the chain: when every alias candidate is quota-blocked,
// forced rotation fails with ErrAllModelsQuotaBlocked and leaves rotation
// state unchanged — the loop then surfaces the original quota error for
// caller-side parking instead of spinning.
func TestResolver_RotateToNextModel_AllQuotaBlockedAfter429(t *testing.T) {
	r := newTestResolver(enabledQuotaCfg())

	r.BlockQuotaEntry("coder", "zai", "glm-4.7", time.Now().Add(time.Hour))
	r.BlockQuotaEntry("coder", "ollama", "llama3.2", time.Now().Add(time.Hour))

	next, err := r.RotateToNextModel("coder")
	if !errors.Is(err, ErrAllModelsQuotaBlocked) {
		t.Fatalf("expected ErrAllModelsQuotaBlocked, got %v", err)
	}
	if next != nil {
		t.Errorf("expected nil model, got %s/%s", next.ProviderID, next.ModelID)
	}
	if idx, _, _, ok := r.GetAliasHealth("coder"); !ok || idx != 0 {
		t.Errorf("CurrentIndex = %d (ok=%v), want 0 (state must be unchanged on failed rotation)", idx, ok)
	}
}

// TestResolver_RotateToNextModel_SkipsDownEndpointQuotaChain reproduces the
// full agnes → local → ollama chain at the resolver seam: agnes blocked by
// quota, rotation skips it to the (down) local candidate; that attempt
// fails with a non-quota error, alias health records it, and rotation then
// proceeds to the third candidate (ollama) — the mid-chain hop the agent
// loop's non-rate-limit branch performs.
func TestResolver_RotateToNextModel_SkipsDownEndpointQuotaChain(t *testing.T) {
	// Three-candidate alias: agnes (quota'd) → local (down) → ollama (ok).
	cfg := createTestConfig()
	cfg.ModelAliases["chain"] = ModelAliasEntry{
		Models: []string{"zai/glm-4.7", "ollama/llama3.2", "agnes/m"},
	}
	cfg.Providers["agnes"] = ProviderConfig{
		API:     "openai",
		Options: ProviderOptionsConfig{BaseURL: "https://agnes.invalid"},
		Models: map[string]ModelDef{
			"m": {Name: "m", Capabilities: []string{"completion"}},
		},
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	r := NewResolver(cfg, logger)
	r.SetQuotaConfig(enabledQuotaCfg())

	// agnes 429'd: the loop quota-blocks it. Rotation head is zai/glm-4.7
	// (agnes stand-in position 0) — block and rotate, mirroring the loop.
	r.BlockQuotaEntry("chain", "zai", "glm-4.7", time.Now().Add(time.Hour))

	// Hop 1: rotation lands on the next candidate (ollama/llama3.2).
	first, err := r.RotateToNextModel("chain")
	if err != nil {
		t.Fatalf("rotation 1: %v", err)
	}
	if first.ProviderID != "ollama" {
		t.Errorf("hop 1 = %s/%s, want ollama/*", first.ProviderID, first.ModelID)
	}

	// Hop 2: the local endpoint is down — the loop records a plain failure
	// (RecordAliasFailure) and rotates again. Rotation must proceed to the
	// third candidate even though it wraps past the quota-blocked head.
	r.RecordAliasFailure("chain", errors.New("connection refused"), first)
	second, err := r.RotateToNextModel("chain")
	if err != nil {
		t.Fatalf("rotation 2 after down-endpoint failure: %v", err)
	}
	if second.ProviderID != "agnes" {
		t.Errorf("hop 2 = %s/%s, want agnes/m (third candidate)", second.ProviderID, second.ModelID)
	}

	// Hop 3: if the wrapped candidate 429s again, the loop re-blocks it and
	// rotates once more — cycling back to the only quota-unblocked member.
	// (Plain endpoint failures do NOT quota-block: only QuotaResetError
	// marks do.) The chain keeps attempting members rather than failing the
	// turn while one unblocked candidate remains.
	r.BlockQuotaEntry("chain", "agnes", "m", time.Now().Add(time.Hour))
	third, err := r.RotateToNextModel("chain")
	if err != nil {
		t.Fatalf("rotation 3 after agnes re-block: %v", err)
	}
	if third.ProviderID != "ollama" {
		t.Errorf("hop 3 = %s/%s, want ollama/* (only unblocked candidate)", third.ProviderID, third.ModelID)
	}
	// Terminal case is covered by TestResolver_RotateToNextModel_AllQuotaBlockedAfter429:
	// with every candidate quota-blocked, rotation refuses with
	// ErrAllModelsQuotaBlocked and the loop surfaces the quota error for parking.
}

// TestClassifyQuotaVsRateLimit_Agnes429 pins the classification contract the
// failover depends on: an agnes 429 with a quota-shaped body must classify
// as QuotaResetError (rotate-and-block), while a bare 429 without any quota
// signal stays RateLimitError (existing backoff-rotation branch).
func TestClassifyQuotaVsRateLimit_Agnes429(t *testing.T) {
	quotaBody := []byte(`{"error":{"type":"usage_limit_reached","message":"monthly usage cap reached","resets_at":1893456000}}`)

	// Quota-shaped body + 429 → ParseQuotaResponse returns a QuotaResetError.
	header := http.Header{}
	qe := ParseQuotaResponse(http.StatusTooManyRequests, header, quotaBody, QuotaContext{
		ProviderID: "agnes",
		ModelID:    "agnes-2.5-flash",
		MaxWait:    24 * time.Hour,
	})
	if qe == nil {
		t.Fatal("ParseQuotaResponse(agnes 429, quota body) = nil, want QuotaResetError")
	}
	if qe.ProviderID != "agnes" {
		t.Errorf("ProviderID = %q, want agnes", qe.ProviderID)
	}

	// And classifyQuotaDecision must route it to the quota path (not
	// RateLimitError) so the loop's quota branch engages.
	providerDetail := ParseRateLimitBody(quotaBody)
	if !classifyQuotaDecision(http.StatusTooManyRequests, quotaBody, providerDetail) {
		t.Error("classifyQuotaDecision(agnes 429, quota body) = false, want true (quota classification)")
	}
}
