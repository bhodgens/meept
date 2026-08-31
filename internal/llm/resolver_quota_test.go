package llm

import (
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

func TestResolverQuotaBlock(t *testing.T) {
	cfg := createTestConfig()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	resolver := NewResolver(cfg, logger)

	// Set quota config
	resolver.quotaCfg = &QuotaWaitConfig{
		Enabled:         true,
		MaxWait:         24 * time.Hour,
		DefaultEstimate: 1 * time.Hour,
	}

	// Verify no blocks initially
	blocks := resolver.ActiveQuotaBlocks()
	if len(blocks) != 0 {
		t.Fatalf("expected 0 blocks, got %d", len(blocks))
	}

	// Block one model from the existing alias
	blocks = resolver.ActiveQuotaBlocks()
	if len(blocks) != 0 {
		t.Fatalf("expected 0 blocks before blocking, got %d", len(blocks))
	}

	// Block via entry
	resolver.BlockQuotaEntry("coder", "zai", "glm-4.7", time.Now().Add(30*time.Minute))

	blocks = resolver.ActiveQuotaBlocks()
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].ProviderID != "zai" {
		t.Errorf("expected provider zai, got %s", blocks[0].ProviderID)
	}
	if blocks[0].ModelID != "glm-4.7" {
		t.Errorf("expected model glm-4.7, got %s", blocks[0].ModelID)
	}
}

func TestResolverQuotaCredentialBlock(t *testing.T) {
	cfg := createTestConfig()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	resolver := NewResolver(cfg, logger)

	resolver.quotaCfg = &QuotaWaitConfig{
		Enabled:         true,
		MaxWait:         24 * time.Hour,
		DefaultEstimate: 1 * time.Hour,
	}

	// Block by credential key
	resolver.BlockQuotaCredential("coder", "zai:key:abcd1234", time.Now().Add(1*time.Hour))

	unblock := resolver.QuotaBlockedUntil("zai:key:abcd1234")
	if unblock.IsZero() {
		t.Error("expected non-zero unblock time")
	}
	if unblock.Before(time.Now()) {
		t.Error("expected unblock time to be in the future")
	}
}

func TestResolverQuotaClearAfterReset(t *testing.T) {
	cfg := createTestConfig()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	resolver := NewResolver(cfg, logger)

	resolver.BlockQuotaEntry("coder", "zai", "glm-4.7", time.Now().Add(-1*time.Hour))

	// Block should have expired
	blocks := resolver.ActiveQuotaBlocks()
	if len(blocks) != 0 {
		t.Fatalf("expected 0 blocks after expiry, got %d", len(blocks))
	}
}

func TestResolverQuotaAllBlocked(t *testing.T) {
	cfg := createTestConfig()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	resolver := NewResolver(cfg, logger)

	resolver.quotaCfg = &QuotaWaitConfig{
		Enabled:         true,
		MaxWait:         24 * time.Hour,
		DefaultEstimate: 1 * time.Hour,
	}

	// Block both models in the coder alias
	resolver.BlockQuotaEntry("coder", "zai", "glm-4.7", time.Now().Add(1*time.Hour))
	resolver.BlockQuotaEntry("coder", "ollama", "llama3.2", time.Now().Add(1*time.Hour))

	if resolver.HasHealthyModels("coder") {
		t.Error("expected HasHealthyModels to be false when all models blocked")
	}
}

func TestResolverQuotaOneBlockStillHealthy(t *testing.T) {
	cfg := createTestConfig()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	resolver := NewResolver(cfg, logger)

	resolver.quotaCfg = &QuotaWaitConfig{
		Enabled:         true,
		MaxWait:         24 * time.Hour,
		DefaultEstimate: 1 * time.Hour,
	}

	// Block only one model
	resolver.BlockQuotaEntry("coder", "zai", "glm-4.7", time.Now().Add(1*time.Hour))

	// Should still have healthy models (ollama/llama3.2 is available)
	if !resolver.HasHealthyModels("coder") {
		t.Error("expected HasHealthyModels to be true when one model is available")
	}
}

// enabledQuotaCfg is the enabled-with-defaults QuotaWaitConfig used by the
// rotation tests below.
func enabledQuotaCfg() *QuotaWaitConfig {
	return &QuotaWaitConfig{
		Enabled:         true,
		MaxWait:         24 * time.Hour,
		DefaultEstimate: 1 * time.Hour,
	}
}

// newTestResolver builds a resolver over createTestConfig with the given
// quota config (nil = unset).
func newTestResolver(quotaCfg *QuotaWaitConfig) *Resolver {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	r := NewResolver(createTestConfig(), logger)
	if quotaCfg != nil {
		r.SetQuotaConfig(quotaCfg)
	}
	return r
}

// TestResolver_SetQuotaConfig verifies the production setter semantics:
// copy-by-value, nil clears, nil receiver is a no-op.
func TestResolver_SetQuotaConfig(t *testing.T) {
	r := newTestResolver(nil)

	// Disabled (nil) by default
	if r.quotaCfg != nil {
		t.Fatal("expected quotaCfg to be nil by default")
	}

	// Set copies the value: mutating the caller's struct afterwards must
	// not affect the resolver.
	in := QuotaWaitConfig{Enabled: true, MaxWait: time.Hour}
	r.SetQuotaConfig(&in)
	in.Enabled = false
	if !r.quotaEnabled() {
		t.Error("expected resolver to keep its own copy (Enabled must stay true)")
	}

	// Re-set with zero value: zero-value QuotaWaitConfig means DISABLED.
	r.SetQuotaConfig(&QuotaWaitConfig{})
	if r.quotaEnabled() {
		t.Error("expected zero-value QuotaWaitConfig to mean disabled")
	}

	// SetQuotaConfig(nil) clears.
	r.SetQuotaConfig(enabledQuotaCfg())
	r.SetQuotaConfig(nil)
	if r.quotaCfg != nil {
		t.Error("expected SetQuotaConfig(nil) to clear quota config")
	}

	// Nil receiver is a no-op (typed-nil guard).
	var nr *Resolver
	nr.SetQuotaConfig(enabledQuotaCfg())
}

// TestResolver_ResolveForAlias_CredentialBlockedCurrentRotates is Bug A:
// a credential-blocked CURRENT model must rotate, not be returned as-is.
func TestResolver_ResolveForAlias_CredentialBlockedCurrentRotates(t *testing.T) {
	r := newTestResolver(enabledQuotaCfg())

	// Credential-block the current (index 0) model: zai/glm-4.7. The
	// credential key is derived from the model config (default key).
	resolver := r
	health := resolver.getOrCreateHealth("coder")
	zai := resolver.aliases["coder"].Models[0]
	resolver.BlockQuotaCredential("coder", QuotaCredentialKey(zai.ProviderID, zai), time.Now().Add(1*time.Hour))

	mc, err := resolver.ResolveForAlias("coder", "")
	if err != nil {
		t.Fatalf("ResolveForAlias: %v", err)
	}
	if mc.ProviderID != "ollama" || mc.ModelID != "llama3.2" {
		t.Errorf("expected rotation to ollama/llama3.2, got %s/%s", mc.ProviderID, mc.ModelID)
	}
	if health.CurrentIndex != 1 {
		t.Errorf("expected CurrentIndex 1, got %d", health.CurrentIndex)
	}
}

// TestResolver_ResolveForAlias_AllBlocked is Bug B: when every candidate is
// quota-blocked, ResolveForAlias must FAIL with ErrAllModelsQuotaBlocked
// instead of returning a blocked model.
func TestResolver_ResolveForAlias_AllBlocked(t *testing.T) {
	r := newTestResolver(enabledQuotaCfg())

	r.BlockQuotaEntry("coder", "zai", "glm-4.7", time.Now().Add(1*time.Hour))
	r.BlockQuotaEntry("coder", "ollama", "llama3.2", time.Now().Add(1*time.Hour))

	mc, err := r.ResolveForAlias("coder", "")
	if !errors.Is(err, ErrAllModelsQuotaBlocked) {
		t.Fatalf("expected ErrAllModelsQuotaBlocked, got %v", err)
	}
	if mc != nil {
		t.Errorf("expected nil model, got %s/%s", mc.ProviderID, mc.ModelID)
	}
	if !strings.Contains(err.Error(), "coder") {
		t.Errorf("expected alias name in error, got %q", err.Error())
	}
}

// TestResolver_RotateToNextModel_SkipsBlockedAndFailsWhenAllBlocked is
// Bug C: forced rotation skips quota-blocked models and fails distinctly
// when all are blocked.
func TestResolver_RotateToNextModel_SkipsBlockedAndFailsWhenAllBlocked(t *testing.T) {
	// (a) blocked candidate is skipped
	r := newTestResolver(enabledQuotaCfg())
	r.BlockQuotaEntry("coder", "ollama", "llama3.2", time.Now().Add(1*time.Hour))
	mc, err := r.RotateToNextModel("coder")
	if err != nil {
		t.Fatalf("RotateToNextModel: %v", err)
	}
	if mc.ProviderID != "zai" || mc.ModelID != "glm-4.7" {
		t.Errorf("expected skip-blocked rotation to wrap to zai/glm-4.7, got %s/%s", mc.ProviderID, mc.ModelID)
	}

	// (b) all blocked -> ErrAllModelsQuotaBlocked, rotation state unchanged
	r2 := newTestResolver(enabledQuotaCfg())
	r2.BlockQuotaEntry("coder", "zai", "glm-4.7", time.Now().Add(1*time.Hour))
	r2.BlockQuotaEntry("coder", "ollama", "llama3.2", time.Now().Add(1*time.Hour))
	mc, err = r2.RotateToNextModel("coder")
	if !errors.Is(err, ErrAllModelsQuotaBlocked) {
		t.Fatalf("expected ErrAllModelsQuotaBlocked, got %v", err)
	}
	if mc != nil {
		t.Errorf("expected nil model, got %s/%s", mc.ProviderID, mc.ModelID)
	}
	if idx, _, _, ok := r2.GetAliasHealth("coder"); !ok || idx != 0 {
		t.Errorf("expected CurrentIndex to stay 0, got %d (ok=%v)", idx, ok)
	}
}

// TestResolver_SetQuotaConfig_GatesSkipLogic verifies disabled quota config
// means NO skip logic: blocked entries do not influence resolution.
func TestResolver_SetQuotaConfig_GatesSkipLogic(t *testing.T) {
	// quotaCfg unset (nil) -> disabled -> blocked model still returned.
	r := newTestResolver(nil)
	r.BlockQuotaEntry("coder", "zai", "glm-4.7", time.Now().Add(1*time.Hour))
	mc, err := r.ResolveForAlias("coder", "")
	if err != nil {
		t.Fatalf("ResolveForAlias with disabled quota config: %v", err)
	}
	if mc.ProviderID != "zai" {
		t.Errorf("expected current (blocked) model zai when quota disabled, got %s", mc.ProviderID)
	}

	// Zero-value QuotaWaitConfig (Enabled=false) is also disabled.
	r2 := newTestResolver(&QuotaWaitConfig{})
	r2.BlockQuotaEntry("coder", "zai", "glm-4.7", time.Now().Add(1*time.Hour))
	mc, err = r2.ResolveForAlias("coder", "")
	if err != nil {
		t.Fatalf("ResolveForAlias with zero-value quota config: %v", err)
	}
	if mc.ProviderID != "zai" {
		t.Errorf("expected current (blocked) model zai when quota disabled, got %s", mc.ProviderID)
	}
}

// TestResolver_LazyClearOnSuccess verifies RecordAliasSuccess deletes only
// EXPIRED block entries (both maps) and leaves unexpired ones alone.
func TestResolver_LazyClearOnSuccess(t *testing.T) {
	r := newTestResolver(enabledQuotaCfg())

	// One expired + one unexpired entry block, one expired + one unexpired
	// credential block.
	r.BlockQuotaEntry("coder", "zai", "glm-4.7", time.Now().Add(-time.Minute))  // expired
	r.BlockQuotaEntry("coder", "ollama", "llama3.2", time.Now().Add(time.Hour)) // unexpired
	r.BlockQuotaCredential("coder", "zai:key:expired", time.Now().Add(-time.Minute))
	r.BlockQuotaCredential("coder", "zai:key:live", time.Now().Add(time.Hour))

	r.RecordAliasSuccess("coder")

	// Expired entry must be gone: ActiveQuotaBlocks should report exactly
	// the two unexpired blocks.
	blocks := r.ActiveQuotaBlocks()
	if len(blocks) != 2 {
		t.Fatalf("expected 2 remaining blocks, got %d: %+v", len(blocks), blocks)
	}
	seen := map[string]bool{}
	for _, b := range blocks {
		seen[b.CredentialKey] = true
		if b.AliasName != "coder" {
			t.Errorf("expected AliasName coder, got %q", b.AliasName)
		}
	}
	if seen["ollama|llama3.2"] {
		t.Error("expected unexpired entry block for ollama/llama3.2 to survive")
	}
	if !seen["zai:key:live"] {
		t.Error("expected unexpired credential block zai:key:live to survive")
	}
	if seen["zai:key:expired"] {
		t.Error("expected expired credential block to be lazily deleted")
	}
}

// fakeQuotaSource is a stand-in for config.QuotaRetryConfig implementing the
// ConfigFromSchema parameter interface (keeps internal/llm free of an
// internal/config import).
type fakeQuotaSource struct {
	enabled            bool
	maxWait            time.Duration
	defaultEstimate    time.Duration
	deferCheckInterval time.Duration
}

func (f *fakeQuotaSource) GetEnabled() bool                     { return f.enabled }
func (f *fakeQuotaSource) GetMaxWait() time.Duration            { return f.maxWait }
func (f *fakeQuotaSource) GetDefaultEstimate() time.Duration    { return f.defaultEstimate }
func (f *fakeQuotaSource) GetDeferCheckInterval() time.Duration { return f.deferCheckInterval }

// TestConfigFromSchema_Roundtrip verifies the getter-source contract used by
// daemon wiring: ConfigFromSchema copies all four fields from any source
// implementing the parameter interface.
func TestConfigFromSchema_Roundtrip(t *testing.T) {
	src := &fakeQuotaSource{
		enabled:            true,
		maxWait:            90 * time.Minute,
		defaultEstimate:    5 * time.Minute,
		deferCheckInterval: 30 * time.Second,
	}

	got := ConfigFromSchema(src)
	want := QuotaWaitConfig{
		Enabled:            true,
		MaxWait:            90 * time.Minute,
		DefaultEstimate:    5 * time.Minute,
		DeferCheckInterval: 30 * time.Second,
	}
	if got != want {
		t.Errorf("ConfigFromSchema() = %+v, want %+v", got, want)
	}

	// And the produced config enables the resolver's skip logic.
	r := newTestResolver(nil)
	r.SetQuotaConfig(ptrTo(got))
	if !r.quotaEnabled() {
		t.Error("expected ConfigFromSchema-produced config to enable quota logic")
	}
}

func ptrTo(q QuotaWaitConfig) *QuotaWaitConfig { return &q }

// TestResolverQuota_StickyPinSkipsBlockedModel verifies that a sticky-pinned
// caller is not served a quota-blocked model: the pin is released and the
// caller re-pins to the unblocked candidate (same skip semantics as the
// non-sticky rotation path).
func TestResolverQuota_StickyPinSkipsBlockedModel(t *testing.T) {
	r := newTestResolver(&QuotaWaitConfig{
		Enabled:         true,
		MaxWait:         24 * time.Hour,
		DefaultEstimate: 1 * time.Hour,
	})

	// Enable sticky balancing on the coder alias so ResolveForAlias takes
	// the resolveStickyCaller path.
	r.mu.Lock()
	r.aliases["coder"].BalancedStickyRequests = true
	r.mu.Unlock()

	// First resolve pins the caller to the rotation head (zai/glm-4.7).
	first, err := r.ResolveForAlias("coder", "caller-1")
	if err != nil {
		t.Fatalf("initial resolve: %v", err)
	}

	// Quota-block the pinned model, then resolve again: the sticky path
	// must NOT return the blocked model.
	r.BlockQuotaEntry("coder", first.ProviderID, first.ModelID, time.Now().Add(30*time.Minute))

	second, err := r.ResolveForAlias("coder", "caller-1")
	if err != nil {
		t.Fatalf("resolve after block: %v", err)
	}
	if second.ProviderID == first.ProviderID && second.ModelID == first.ModelID {
		t.Errorf("sticky caller re-served quota-blocked model %s/%s", first.ProviderID, first.ModelID)
	}

	// With every candidate blocked, the resolve still serves a model (the
	// sticky path cannot error) so the caller's request surfaces the
	// provider's quota error.
	r.BlockQuotaEntry("coder", second.ProviderID, second.ModelID, time.Now().Add(30*time.Minute))
	third, err := r.ResolveForAlias("coder", "caller-1")
	if err != nil {
		t.Fatalf("resolve with all blocked: %v", err)
	}
	if third == nil {
		t.Fatal("expected a model even when all candidates are blocked")
	}
}
