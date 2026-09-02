package llm

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"
)

// endpointTestConfig builds fixtures exercising the D10 endpoint-identity
// rules: openai/gpt-a and openai/gpt-b share host AND credential (same
// endpoint, shared fate), xai and xai-oauth-style configs share a host but
// differ in credential, and gala-mlx/gala-llama mirror the real-world
// same-host-different-runtime practice that makes host-only keys wrong
// (audit R2).
func endpointTestConfig() *ProvidersConfig {
	return &ProvidersConfig{
		Providers: map[string]ProviderConfig{
			"openai": {
				API: "openai",
				Options: ProviderOptionsConfig{
					BaseURL: "https://api.openai.example/v1",
					APIKey:  "sk-openai-fixed",
				},
				Models: map[string]ModelDef{
					"gpt-a": {Name: "gpt-a", Capabilities: []string{"completion"}},
					"gpt-b": {Name: "gpt-b", Capabilities: []string{"reasoning"}},
				},
			},
			"xai": {
				API: "openai",
				Options: ProviderOptionsConfig{
					BaseURL: "https://api.x.ai/v1",
					APIKey:  "sk-xai-fixed",
				},
				Models: map[string]ModelDef{
					"grok": {Name: "grok", Capabilities: []string{"completion"}},
				},
			},
			"gala-mlx": {
				API: "openai",
				Options: ProviderOptionsConfig{
					BaseURL: "http://gala.local:8080/v1",
				},
				Models: map[string]ModelDef{
					"mlx-a": {Name: "mlx-a", Capabilities: []string{"completion"}},
				},
			},
			"gala-llama": {
				API: "openai",
				Options: ProviderOptionsConfig{
					BaseURL: "http://gala.local:8080/v1",
				},
				Models: map[string]ModelDef{
					"llama-a": {Name: "llama-a", Capabilities: []string{"completion"}},
				},
			},
		},
		ModelAliases: map[string]ModelAliasEntry{
			// timeout unset (0): endpoint-block tests must not be confounded
			// by the alias-level explicit-timeout feature.
			"medium":    {Models: []string{"openai/gpt-a"}},
			"thinkhard": {Models: []string{"openai/gpt-b"}},
			"mixed":     {Models: []string{"openai/gpt-a", "xai/grok"}},
			// explicit alias timeout opt-in (D10): arms on consistent
			// same-member failure after max_fails consecutive hits.
			"soft": {Models: []string{"openai/gpt-a"}, Timeout: 60, MaxFails: 2},
			// timeout 0 in CONFIG: alias-level block must NEVER arm.
			"broke": {Models: []string{"xai/grok"}, MaxFails: 2},
		},
	}
}

func newEndpointTestResolver(t *testing.T) *Resolver {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewResolver(endpointTestConfig(), logger)
}

// throttleErr is a bare 429 without any quota signal (D7): FailureThrottle.
func throttleErr() error {
	return &APIError{StatusCode: 429, Detail: "provider busy, try later"}
}

// TestEndpointKey_HostPlusCredential pins the audit R2 identity rules.
func TestEndpointKey_HostPlusCredential(t *testing.T) {
	sameHostSameCredA := &ModelConfig{BaseURL: "https://api.openai.example/v1", ProviderID: "openai", ModelID: "model-1", APIKey: "sk-1"}
	sameHostSameCredB := &ModelConfig{BaseURL: "https://api.openai.example/v1", ProviderID: "openai", ModelID: "model-2", APIKey: "sk-1"}

	// Same host AND same credential, different model ids -> same key (D10
	// shared fate).
	if EndpointKey(sameHostSameCredA) != EndpointKey(sameHostSameCredB) {
		t.Errorf("models sharing host+credential must share the endpoint key: %q vs %q",
			EndpointKey(sameHostSameCredA), EndpointKey(sameHostSameCredB))
	}

	distinctHost := &ModelConfig{BaseURL: "https://api.other.example/v1", ProviderID: "openai", ModelID: "model-1", APIKey: "sk-1"}
	if EndpointKey(sameHostSameCredA) == EndpointKey(distinctHost) {
		t.Error("distinct hosts must produce distinct endpoint keys")
	}

	// Same host (api.x.ai), different credential: API key vs OAuth
	// subscription must NOT share fate (audit R2 xai/xai-oauth fixture).
	apiKeyModel := &ModelConfig{BaseURL: "https://api.x.ai/v1", ProviderID: "xai", ModelID: "grok", APIKey: "sk-xai"}
	oauthModel := &ModelConfig{BaseURL: "https://api.x.ai/v1", ProviderID: "xai", ModelID: "grok", OAuthProvider: "xai-oauth"}
	if EndpointKey(apiKeyModel) == EndpointKey(oauthModel) {
		t.Error("same host with unrelated credentials must produce distinct keys")
	}

	// Same host (gala), different provider runtimes (audit R2 gala fixture).
	galaMlx := &ModelConfig{BaseURL: "http://gala:8080/v1", ProviderID: "gala-mlx", ModelID: "m"}
	galaLlama := &ModelConfig{BaseURL: "http://gala:8080/v1", ProviderID: "gala-llama", ModelID: "m"}
	if EndpointKey(galaMlx) == EndpointKey(galaLlama) {
		t.Error("same host across distinct providers/credentials must produce distinct keys")
	}

	// Empty and nil configs: stable, non-empty fallback keys.
	if EndpointKey(nil) == "" {
		t.Error("nil config must yield a stable non-empty fallback key")
	}
	if EndpointKey(nil) != EndpointKey(nil) {
		t.Error("nil config key must be stable")
	}
	empty := &ModelConfig{}
	if EndpointKey(empty) == "" || EndpointKey(empty) != EndpointKey(&ModelConfig{}) {
		t.Errorf("empty configs must share a stable fallback key, got %q", EndpointKey(empty))
	}
}

// TestEndpointBlock_SameEndpointModelSkipped is the primary D10 test: a
// timeout/throttle on model-1 makes the resolver skip model-2 when it lives
// on the same endpoint, and serve the model on the different endpoint.
func TestEndpointBlock_SameEndpointModelSkipped(t *testing.T) {
	r := newEndpointTestResolver(t)

	gptA := r.aliases["mixed"].Models[0]
	r.RecordAliasFailure("mixed", throttleErr(), gptA)

	// Whitelist: the default alias cooldown from RecordAliasFailure would
	// advance rotation on its own; clear it so ONLY the endpoint block is
	// under test.
	health := r.health["mixed"]
	health.CooldownUntil = time.Time{}
	health.ConsecutiveFails = 0

	mc, err := r.ResolveForAlias("mixed", "")
	if err != nil {
		t.Fatalf("ResolveForAlias: %v", err)
	}
	if mc.ProviderID != "xai" || mc.ModelID != "grok" {
		t.Errorf("expected rotation to xai/grok (different endpoint), got %s/%s", mc.ProviderID, mc.ModelID)
	}
	if health.CurrentIndex != 1 {
		t.Errorf("expected CurrentIndex 1, got %d", health.CurrentIndex)
	}

	// The failed model must be marked on the Resolver-level map, keyed by
	// host+credential.
	key := EndpointKey(gptA)
	if until, ok := r.endpointBlocks[key]; !ok || !until.After(time.Now()) {
		t.Errorf("expected live endpoint block for %q, got %v (ok=%v)", key, until, ok)
	}
}

// TestEndpointBlock_CrossAliasSharedFate is the audit B3 test: medium and
// thinkhard are DIFFERENT aliases over models sharing one endpoint. State on
// AliasHealth (per-alias) could never deliver this; the Resolver-level map
// must.
func TestEndpointBlock_CrossAliasSharedFate(t *testing.T) {
	r := newEndpointTestResolver(t)

	mediumModel := r.aliases["medium"].Models[0]
	r.RecordAliasFailure("medium", throttleErr(), mediumModel)

	// The failing alias itself: its only model is endpoint-blocked.
	if _, err := r.ResolveForAlias("medium", ""); !errors.Is(err, ErrAllEndpointsBlocked) {
		t.Fatalf("expected ErrAllEndpointsBlocked for the failing alias, got %v", err)
	}

	// The OTHER alias over the same endpoint: must see the block too.
	if _, err := r.ResolveForAlias("thinkhard", ""); !errors.Is(err, ErrAllEndpointsBlocked) {
		t.Fatalf("expected ErrAllEndpointsBlocked for the sibling alias (cross-alias shared fate), got %v", err)
	}

	// ...and the error must be distinct from the quota error.
	if _, err := r.ResolveForAlias("medium", ""); errors.Is(err, ErrAllModelsQuotaBlocked) {
		t.Fatal("endpoint all-blocked must never surface as ErrAllModelsQuotaBlocked")
	}
}

// TestEndpointBlock_TransportTimeoutOnly is the audit M4 test: a transport
// failure (context deadline, no HTTP response) reaches RecordAliasFailure
// without status/body and must still mark the endpoint.
func TestEndpointBlock_TransportTimeoutOnly(t *testing.T) {
	r := newEndpointTestResolver(t)

	gptA := r.aliases["mixed"].Models[0]
	// No APIError: a bare wrapped transport timeout.
	r.RecordAliasFailure("mixed", context.DeadlineExceeded, gptA)

	health := r.health["mixed"]
	health.CooldownUntil = time.Time{}
	health.ConsecutiveFails = 0

	mc, err := r.ResolveForAlias("mixed", "")
	if err != nil {
		t.Fatalf("ResolveForAlias after transport timeout: %v", err)
	}
	if mc.ProviderID != "xai" {
		t.Errorf("expected transport timeout to block endpoint and rotate to xai/grok, got %s/%s", mc.ProviderID, mc.ModelID)
	}
}

// TestVerdictForFailure_TransportSeam pins the classification seam used by
// RecordAliasFailure: status-bearing errors go through Classify (leaf 01);
// transport timeouts map to FailureThrottle; ordinary errors stay None.
func TestVerdictForFailure_TransportSeam(t *testing.T) {
	// Status-bearing: bare 429 -> throttle via Classify (D7).
	v := VerdictForFailure(&APIError{StatusCode: 429, Detail: "busy"})
	if v.Class != FailureThrottle {
		t.Errorf("expected FailureThrottle for bare 429, got %v", v.Class)
	}

	// Transport timeout without any HTTP response -> FailureThrottle.
	v = VerdictForFailure(context.DeadlineExceeded)
	if v.Class != FailureThrottle || v.Reason != "transport_timeout" {
		t.Errorf("expected transport_timeout throttle, got class=%v reason=%q", v.Class, v.Reason)
	}

	var netTimeout net.Error = &fakeNetTimeout{}
	v = VerdictForFailure(fmtWrap("dial tcp: i/o timeout", netTimeout))
	if v.Class != FailureThrottle {
		t.Errorf("expected net.Error timeout to map to FailureThrottle, got %v", v.Class)
	}

	// Ordinary failure with no verdict: alias cooldown still applies, but
	// no endpoint block.
	if v := VerdictForFailure(nil); v.Class != FailureNone {
		t.Errorf("expected FailureNone for nil error, got %v", v.Class)
	}
	if v := VerdictForFailure(errors.New("something else")); v.Class != FailureNone {
		t.Errorf("expected FailureNone for non-transport non-HTTP error, got %v", v.Class)
	}

	// Quota-shaped 429 must NOT block endpoints (quota has its own path).
	v = VerdictForFailure(&APIError{StatusCode: 429, Detail: "quota exceeded"})
	if v.Class != FailureQuota {
		t.Errorf("expected FailureQuota for quota-shaped 429, got %v", v.Class)
	}
}

// fakeNetTimeout is a minimal net.Error carrying Timeout()=true.
type fakeNetTimeout struct{}

func (fakeNetTimeout) Error() string   { return "i/o timeout" }
func (fakeNetTimeout) Timeout() bool   { return true }
func (fakeNetTimeout) Temporary() bool { return true }

// fmtWrap wraps an error with a message prefix, like fmt.Errorf("%s: %w").
func fmtWrap(msg string, err error) error {
	return &wrappedErr{msg: msg, err: err}
}

type wrappedErr struct {
	msg string
	err error
}

func (w *wrappedErr) Error() string { return w.msg + ": " + w.err.Error() }
func (w *wrappedErr) Unwrap() error { return w.err }

// TestEndpointBlock_ExpiryLazyClear verifies expiry consults the injected
// clock lazily and RecordAliasSuccess sweeps the expired entry (same
// pattern as the quota-block lazy clear).
func TestEndpointBlock_ExpiryLazyClear(t *testing.T) {
	r := newEndpointTestResolver(t)
	base := time.Now()
	current := base
	r.now = func() time.Time { return current }

	gptA := r.aliases["mixed"].Models[0]
	r.RecordAliasFailure("mixed", throttleErr(), gptA)
	key := EndpointKey(gptA)

	if _, ok := r.endpointBlocks[key]; !ok {
		t.Fatal("expected endpoint block to be recorded")
	}

	// Advance past the block window (30s default base): resolution must
	// lazily treat the endpoint as unblocked.
	current = base.Add(45 * time.Second)
	health := r.health["mixed"]
	health.CooldownUntil = time.Time{}
	health.ConsecutiveFails = 0
	if _, err := r.ResolveForAlias("mixed", ""); err != nil {
		t.Fatalf("expected expired block to resolve again, got %v", err)
	}

	// A success sweeps the expired entry from the map (lazy clear parity
	// with quota blocks).
	r.RecordAliasSuccess("mixed")
	if _, ok := r.endpointBlocks[key]; ok {
		t.Error("expected RecordAliasSuccess to sweep the expired endpoint block")
	}
}

// TestAliasTimeout_ExplicitOptInAndDoubling covers leaf Task 3: explicit
// timeout: arms an alias-level block on consistent same-member failure,
// with incremental doubling capped at 4x base.
func TestAliasTimeout_ExplicitOptInAndDoubling(t *testing.T) {
	r := newEndpointTestResolver(t)
	base := time.Now()
	current := base
	r.now = func() time.Time { return current }

	// "soft": timeout 60, max_fails 2. First failure: streak 1 < 2, no
	// alias block.
	gptA := r.aliases["soft"].Models[0]
	r.RecordAliasFailure("soft", errors.New("boom"), gptA)
	if !r.health["soft"].TimeoutBlockUntil.IsZero() {
		t.Fatal("alias block must not arm before max_fails consistent failures")
	}

	// Second consistent failure of the SAME member: block for the 60s base.
	r.RecordAliasFailure("soft", errors.New("boom"), gptA)
	block1 := r.health["soft"].TimeoutBlockUntil
	if block1.IsZero() {
		t.Fatal("expected alias block armed after max_fails consistent failures")
	}
	if got := block1.Sub(current); got != 60*time.Second {
		t.Errorf("expected 60s alias block, got %v", got)
	}
	if _, err := r.ResolveForAlias("soft", ""); !errors.Is(err, ErrAllEndpointsBlocked) {
		t.Fatalf("expected ErrAllEndpointsBlocked while alias blocked, got %v", err)
	}

	// Next consistent failure DOUBLES: 120s.
	current = block1.Add(-5 * time.Second)
	r.RecordAliasFailure("soft", errors.New("boom"), gptA)
	block2 := r.health["soft"].TimeoutBlockUntil
	if got := block2.Sub(current); got != 120*time.Second {
		t.Errorf("expected doubling to 120s, got %v", got)
	}

	// Cap at 4x base: further consistent failures stay at 240s.
	current = block2.Add(-5 * time.Second)
	r.RecordAliasFailure("soft", errors.New("boom"), gptA)
	block3 := r.health["soft"].TimeoutBlockUntil
	if got := block3.Sub(current); got != 240*time.Second {
		t.Errorf("expected cap step 240s, got %v", got)
	}
	current = block3.Add(-5 * time.Second)
	r.RecordAliasFailure("soft", errors.New("boom"), gptA)
	lastBlock := r.health["soft"].TimeoutBlockUntil
	if got := lastBlock.Sub(current); got != 240*time.Second {
		t.Errorf("expected 4x-base cap to hold at 240s, got %v", got)
	}

	// Expiry releases the alias (lazy time check). The final failure
	// re-armed the capped 240s block, so advance past ITS deadline.
	current = lastBlock.Add(time.Second)
	if _, err := r.ResolveForAlias("soft", ""); err != nil {
		t.Fatalf("expected expired alias block to release, got %v", err)
	}

	// Success resets streak and block.
	r.RecordAliasSuccess("soft")
	if r.health["soft"].TimeoutStreak != 0 || !r.health["soft"].TimeoutBlockUntil.IsZero() {
		t.Error("expected success to reset the alias timeout streak and block")
	}
}

// TestAliasTimeout_ConsistentMemberRule verifies a DIFFERENT member failing
// resets the consecutive counter (identity check, mirroring issue #30).
func TestAliasTimeout_ConsistentMemberRule(t *testing.T) {
	// Alias over two members on DIFFERENT endpoints so the endpoint-block
	// path stays out of the way; use plain errors (no throttle verdict).
	cfg := endpointTestConfig()
	cfg.ModelAliases["twin"] = ModelAliasEntry{
		Models:   []string{"xai/grok", "gala-mlx/mlx-a"},
		Timeout:  60,
		MaxFails: 2,
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	r := NewResolver(cfg, logger)

	grok := r.aliases["twin"].Models[0]
	mlx := r.aliases["twin"].Models[1]

	// Alternate members: no consistent streak, so the alias never blocks.
	r.RecordAliasFailure("twin", errors.New("boom"), grok)
	r.RecordAliasFailure("twin", errors.New("boom"), mlx)
	r.RecordAliasFailure("twin", errors.New("boom"), grok)
	r.RecordAliasFailure("twin", errors.New("boom"), mlx)

	if !r.health["twin"].TimeoutBlockUntil.IsZero() {
		t.Error("alternating members must never arm the alias timeout block")
	}
	if r.health["twin"].TimeoutStreak > 1 {
		t.Errorf("streak must reset on member change, got %d", r.health["twin"].TimeoutStreak)
	}
}

// TestAliasTimeout_NotArmedWithoutExplicitConfig verifies the opt-in rule:
// an alias whose config does NOT declare timeout: never alias-blocks, no
// matter how consistently one member fails. (Plain errors keep endpoint
// blocks out of the picture.)
func TestAliasTimeout_NotArmedWithoutExplicitConfig(t *testing.T) {
	r := newEndpointTestResolver(t)

	grok := r.aliases["broke"].Models[0] // timeout unset in config
	for i := 0; i < 5; i++ {
		r.RecordAliasFailure("broke", errors.New("boom"), grok)
	}
	if !r.health["broke"].TimeoutBlockUntil.IsZero() {
		t.Error("alias without explicit timeout: must never alias-block")
	}

	health := r.health["broke"]
	health.CooldownUntil = time.Time{}
	health.ConsecutiveFails = 0
	if _, err := r.ResolveForAlias("broke", ""); err != nil {
		t.Fatalf("expected unconfigured alias to keep serving, got %v", err)
	}
}
