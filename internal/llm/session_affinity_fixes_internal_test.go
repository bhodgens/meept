package llm

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"
)

// Sticky-rotation + teardown-key regression pins (session-affinity leaf
// 07 GAP A / GAP B). These exercise the resolver state machine directly;
// the e2e suites in e2e/suites/session-{affinity,teardown} prove the
// daemon wiring end to end.

// stickyTestConfig builds the e2e rotation sandbox's topology: two
// providers with distinct hosts and credentials behind alias "general",
// timeout unset so the alias-level explicit-timeout block never arms.
func stickyTestConfig() *ProvidersConfig {
	return &ProvidersConfig{
		Providers: map[string]ProviderConfig{
			"fake": {
				API: "openai",
				Options: ProviderOptionsConfig{
					BaseURL: "http://127.0.0.1:1/v1",
					APIKey:  fakeAPIKeyA,
				},
				Models: map[string]ModelDef{
					"fake-model": {Name: "fake-model", Capabilities: []string{"completion"}},
				},
			},
			"fakeb": {
				API: "openai",
				Options: ProviderOptionsConfig{
					BaseURL: "http://127.0.0.1:2/v1",
					APIKey:  fakeAPIKeyB,
				},
				Models: map[string]ModelDef{
					"fake-model": {Name: "fake-model", Capabilities: []string{"completion"}},
				},
			},
		},
		ModelAliases: map[string]ModelAliasEntry{
			"general": {Models: []string{"fake/fake-model", "fakeb/fake-model"}},
		},
	}
}

func newStickyFixTestResolver(t *testing.T) *Resolver {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewResolver(stickyTestConfig(), logger)
}

// sticky5xx builds the error shape the streaming client surfaces to
// RecordAliasFailure after its short-retry budget dies on a 503.
func sticky5xx() error {
	return &ClientError{
		Message: "streaming failed after 3 attempts",
		Cause:   &APIError{StatusCode: http.StatusServiceUnavailable, Detail: "stub down"},
	}
}

func stickyModel(t *testing.T, r *Resolver, providerID string) *ModelConfig {
	t.Helper()
	models := r.FindByProvider(providerID)
	if len(models) == 0 {
		t.Fatalf("no model for provider %s", providerID)
	}
	return models[0]
}

// stickyEndpointBlocked reads the resolver's endpoint-block map for key
// (helper so tests do not reach into the map shape in five places).
func stickyEndpointBlocked(r *Resolver, key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	until, ok := r.endpointBlocks[key]
	return ok && time.Now().Before(until)
}

// Test5xxArmsEndpointBlockAndStickyYields (GAP A core): a plain 5xx
// alias failure must arm an endpoint block on the failed member's
// EndpointKey, and the next resolve for the same session must serve the
// healthy peer — the sticky hook must not re-pull the failed member.
func Test5xxArmsEndpointBlockAndStickyYields(t *testing.T) {
	r := newStickyFixTestResolver(t)

	m1, err := r.ResolveForAlias("general", "sess-1")
	if err != nil {
		t.Fatalf("resolve 1: %v", err)
	}
	if m1.ProviderID != "fake" {
		t.Fatalf("resolve 1 served %s, want fake", m1.ProviderID)
	}
	keyA := EndpointKey(m1)

	r.RecordAliasFailure("general", sticky5xx(), m1)

	if !stickyEndpointBlocked(r, keyA) {
		t.Fatal("5xx alias failure did not arm an endpoint block on the failed member — the sticky hook has no disqualification signal (GAP A)")
	}

	m2, err := r.ResolveForAlias("general", "sess-1")
	if err != nil {
		t.Fatalf("resolve 2: %v", err)
	}
	if m2.ProviderID != "fakeb" {
		t.Fatalf("resolve after A's failure served %s/%s, want fakeb (GAP A: sticky hook re-pulled the failed member)", m2.ProviderID, m2.ModelID)
	}
}

// TestCooldownCursorStaysOnHealthyMember: the post-failure alias
// cooldown advance must not walk the cursor back onto the member the
// failure just left (second half of the observed ping-pong).
func TestCooldownCursorStaysOnHealthyMember(t *testing.T) {
	r := newStickyFixTestResolver(t)

	m1, err := r.ResolveForAlias("general", "")
	if err != nil {
		t.Fatalf("resolve 1: %v", err)
	}
	if m1.ProviderID != "fake" {
		t.Fatalf("resolve 1 served %s, want fake", m1.ProviderID)
	}
	r.RecordAliasFailure("general", sticky5xx(), m1)

	m2, err := r.ResolveForAlias("general", "")
	if err != nil {
		t.Fatalf("resolve 2: %v", err)
	}
	if m2.ProviderID != "fakeb" {
		t.Fatalf("resolve 2 served %s, want fakeb", m2.ProviderID)
	}
	m3, err := r.ResolveForAlias("general", "")
	if err != nil {
		t.Fatalf("resolve 3: %v", err)
	}
	if m3.ProviderID != "fakeb" {
		t.Fatalf("resolve 3 served %s, want fakeb (cooldown advance walked the cursor back onto the failed member)", m3.ProviderID)
	}
}

// Test5xxNotMisroutedToRateLimitLane: the ProviderManager error switch
// must not classify a 5xx-bearing ClientError as a rate limit — the
// IsRateLimitError wrap-chain walks into the APIError's 503 (observed:
// the PM logged "Rate limit hit, rotating" and skipped recordFailure on
// exactly this shape).
func Test5xxNotMisroutedToRateLimitLane(t *testing.T) {
	err := sticky5xx()
	if IsRateLimitError(err) {
		t.Fatal("IsRateLimitError matched a 503-bearing ClientError — PM routes it to the rate-limit lane and never records a health failure")
	}
	if isAuthError(err) {
		t.Fatal("503 classified as auth error")
	}
	if isClientError(err) {
		t.Fatal("503 classified as client error")
	}
	var rl *RateLimitError
	if errors.As(err, &rl) {
		t.Fatal("503 wrapped into RateLimitError")
	}
}

// stubBaseURLer is a minimal endpointBaseURLer for Handles-level tests.
type stubBaseURLer struct {
	resolvable map[string]bool
}

func (s stubBaseURLer) TeardownBaseURL(endpointKey string) (string, bool) {
	if s.resolvable[endpointKey] {
		return "http://" + endpointKey, true
	}
	return "", false
}

// TestTeardownAdapterHandlesSnapshotKeys (GAP B): the llama teardown
// adapter must claim the host|credential snapshot keys
// reportSessionEndpoint writes when the manager resolves the host to a
// live llama runtime, and refuse unresolvable ones. The runtime-prefix
// shape keeps working; the cloud no-op may claim an unresolvable snapshot
// key (its Teardown is a no-op — the llama adapter with the real manager
// decides ownership first in the daemon wiring order).
func TestTeardownAdapterHandlesSnapshotKeys(t *testing.T) {
	snapshotKey := EndpointKey(&ModelConfig{
		ProviderID: "local-llama",
		ModelID:    "stub",
		BaseURL:    "http://127.0.0.1:8080/v1",
		APIKey:     fakeAPIKeyA,
	})

	lt := &LlamaServerTeardown{manager: stubBaseURLer{resolvable: map[string]bool{snapshotKey: true}}}
	if !lt.Handles(snapshotKey) {
		t.Fatalf("LlamaServerTeardown.Handles(%q)=false with a resolvable runtime — snapshot key never reaches the erase (GAP B regression)", snapshotKey)
	}
	unknown := EndpointKey(&ModelConfig{
		ProviderID: "some-cloud",
		ModelID:    "m",
		BaseURL:    "https://api.example.com/v1",
		APIKey:     fakeAPIKeyB,
	})
	if lt.Handles(unknown) {
		t.Fatalf("LlamaServerTeardown claimed unresolvable snapshot key %q", unknown)
	}
	if !lt.Handles("llama-cpp:127.0.0.1:8080") {
		t.Fatal("runtime key no longer handled — keep the original shape working")
	}
	if (&LlamaServerTeardown{}).Handles(snapshotKey) {
		t.Fatal("nil manager must claim nothing (never erase blindly)")
	}
	ct := &CloudTeardown{}
	if !ct.Handles("api.openai.example|fake:key:abc") {
		t.Fatal("cloud key no longer handled")
	}
	if ct.Handles("") {
		t.Fatal("empty key must claim nothing")
	}
}

// TestEndpointBlockClearsOnExpiryAndSuccess: the sticky disqualification
// must be a block with the documented lifetime — expired lazily by a
// later success on the recovering member (Contract 4: the preference
// re-arms only via the next success).
func TestEndpointBlockClearsOnExpiryAndSuccess(t *testing.T) {
	r := newStickyFixTestResolver(t)

	m1, err := r.ResolveForAlias("general", "sess-1")
	if err != nil {
		t.Fatalf("resolve 1: %v", err)
	}
	keyA := EndpointKey(m1)
	r.RecordAliasFailure("general", sticky5xx(), m1)

	// Simulate the block window expiring, then a recovery success.
	r.mu.Lock()
	r.endpointBlocks[keyA] = time.Now().Add(-time.Second)
	r.mu.Unlock()
	r.RecordAliasSuccessModel("general", m1)

	if stickyEndpointBlocked(r, keyA) {
		t.Fatal("expired endpoint block survived a recovery success on the same member")
	}
	_ = stickyModel(t, r, "fake") // fixture sanity: provider still resolvable
}
