package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Test doubles (refusal-fallback leaf 03) ---

// refusalResolver implements ModelResolver with call recording so tests can
// assert resolution attempts and that RecordAliasFailure-class seams are
// never touched by the refusal path.
type refusalResolver struct {
	mu       sync.Mutex
	resolved map[string]string
	err      error
	lastRef  string
	calls    int
}

func (r *refusalResolver) ResolveEscalationRef(ref string) (string, error) {
	r.mu.Lock()
	r.calls++
	r.lastRef = ref
	resolved, ok := r.resolved[ref]
	r.mu.Unlock()
	if !ok {
		if r.err != nil {
			return "", r.err
		}
		return "", errors.New("unknown alias: " + ref)
	}
	return resolved, nil
}

func (r *refusalResolver) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

// recordingResolver pairs a ModelResolver with an alias-failure recorder.
// The refusalFailureRecorder seam (wired in the invariant tests below) is
// the assertion point; this type exists so future seam implementations can
// hang per-resolver failure state off the same fake.
type recordingResolver struct {
	*refusalResolver
}

// refusalChatter returns *llm.RefusalError for the first `refusals` calls,
// then delegates to the inner REAL chatter (a *llm.Client aimed at the
// capture server) so subsequent calls dial the wire and the pinned fallback
// model can be asserted from the received request body.
type refusalChatter struct {
	inner    llm.Chatter
	mu       sync.Mutex
	refusals int
	calls    int
}

func (c *refusalChatter) Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	c.mu.Lock()
	c.calls++
	remaining := c.refusals
	if remaining > 0 {
		c.refusals--
	}
	c.mu.Unlock()
	if remaining > 0 {
		return nil, &llm.RefusalError{
			ProviderID:   "local",
			ModelID:      "primary",
			Source:       "stop_reason",
			FinishReason: "refusal",
		}
	}
	return c.inner.Chat(ctx, messages, opts...)
}

func (c *refusalChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return c.Chat(ctx, messages, opts...)
}

func (c *refusalChatter) Config() *llm.ModelConfig {
	return c.inner.Config()
}

func (c *refusalChatter) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// --- Task 1: handleRefusal decision table ---

func TestAgentLoop_HandleRefusal_DecisionTable(t *testing.T) {
	refusal := &llm.RefusalError{ProviderID: "p1", ModelID: "m1", Source: "stop_reason", FinishReason: "refusal"}

	tests := []struct {
		name         string
		spec         *AgentSpec
		resolver     *refusalResolver
		wantRetry    bool
		wantRefusal  bool // true => the ORIGINAL refusal must surface
		wantEvent    bool
		wantOverride string // expected pinned override after the call ("" = none)
	}{
		{
			// Case 1: spec wins over the (empty) global slot.
			name:         "spec-set-global-empty",
			spec:         &AgentSpec{RefusalModel: "fallback-model"},
			resolver:     &refusalResolver{resolved: map[string]string{"fallback-model": "local/fb"}},
			wantRetry:    true,
			wantEvent:    true,
			wantOverride: "local/fb",
		},
		{
			// Case 2: empty spec inherits the global slot (mirrored onto the
			// spec by the loop's config accessor, exactly like the daemon
			// does at wiring time).
			name:         "spec-empty-global-set",
			spec:         &AgentSpec{RefusalModel: "global-fb"},
			resolver:     &refusalResolver{resolved: map[string]string{"global-fb": "openai/gpt-fb"}},
			wantRetry:    true,
			wantEvent:    true,
			wantOverride: "openai/gpt-fb",
		},
		{
			// Case 3: both empty => feature off, original error surfaces.
			name:        "both-empty-off",
			spec:        &AgentSpec{},
			resolver:    &refusalResolver{},
			wantRetry:   false,
			wantRefusal: true,
		},
		{
			// Case 4: configured but the resolver cannot resolve it =>
			// degrade to off, original error surfaces.
			name:        "configured-but-unresolvable",
			spec:        &AgentSpec{RefusalModel: "ghost-model"},
			resolver:    &refusalResolver{},
			wantRetry:   false,
			wantRefusal: true,
		},
		{
			// Case 5: configured, resolves, differs from the serving model
			// => retry with the fallback pinned, event published.
			name:         "resolves-and-differs",
			spec:         &AgentSpec{RefusalModel: "strong-fb"},
			resolver:     &refusalResolver{resolved: map[string]string{"strong-fb": "p-strong/m-fb"}},
			wantRetry:    true,
			wantEvent:    true,
			wantOverride: "p-strong/m-fb",
		},
		{
			// Case 6: the fallback IS the serving model => one-hop rule,
			// surface the original error instead of looping forever.
			name:        "already-serving-fallback",
			spec:        &AgentSpec{RefusalModel: "fallback-model"},
			resolver:    &refusalResolver{resolved: map[string]string{"fallback-model": "local/fb"}},
			wantRetry:   false,
			wantRefusal: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bus := newEventCaptureBus()
			rr := &recordingResolver{refusalResolver: tt.resolver}

			loop := NewAgentLoop("sess-refusal-"+tt.name, t.TempDir(),
				WithLoopLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
			)
			loop.spec = tt.spec
			loop.refusalResolver = rr
			loop.refusalEventPublisher = bus.publisher()
			loop.refusalOverrideApplier = loop.SetPersistentModelOverride
			loop.refusalOverrideClear = loop.ClearModelOverride
			// The serving model identity for one-hop comparison.
			loop.refusalServingRef = func() string {
				if tt.name == "already-serving-fallback" {
					return tt.resolver.resolved["fallback-model"]
				}
				return "local/primary"
			}

			retry, err := loop.handleRefusal(refusal)

			if tt.wantRefusal {
				require.Error(t, err, "give-up exits must surface the original refusal")
				var got *llm.RefusalError
				require.True(t, errors.As(err, &got), "surfaced error must BE the RefusalError")
				assert.False(t, retry)
				assert.Equal(t, 0, bus.count(), "no event on give-up paths")
				assert.Empty(t, loop.GetModelOverride(), "no override may be pinned on give-up")
				return
			}

			assert.NoError(t, err)
			assert.True(t, retry)
			assert.Equal(t, tt.wantOverride, loop.GetModelOverride(), "fallback must be pinned via the persistent override")
			if tt.wantEvent {
				require.Equal(t, 1, bus.count(), "exactly one event on the fallback path")
				payload := bus.payload(t, 0)
				assert.Equal(t, "refusal_fallback", payload["reason"])
				assert.Equal(t, 0, payload["fix_loops"])
				assert.Contains(t, payload, "agent_id")
				assert.Contains(t, payload, "from_model")
				assert.Equal(t, tt.wantOverride, payload["to_model"])
			}
			// Precedence: the resolver must have been asked for exactly one ref.
			assert.Equal(t, 1, tt.resolver.callCount())
		})
	}

	// Case 1/2 precedence guard: the spec value outranks a global default
	// when both are set (the loop mirrors the global onto the spec only when
	// the spec field is empty).
	t.Run("spec-outranks-global", func(t *testing.T) {
		rr := &refusalResolver{resolved: map[string]string{
			"spec-fb":   "local/spec-fb",
			"global-fb": "local/global-fb",
		}}
		bus := newEventCaptureBus()
		loop := NewAgentLoop("sess-refusal-precedence", t.TempDir(),
			WithLoopLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
		)
		loop.spec = &AgentSpec{RefusalModel: "spec-fb"}
		loop.refusalResolver = rr
		loop.refusalEventPublisher = bus.publisher()
		loop.refusalOverrideApplier = loop.SetPersistentModelOverride
		loop.refusalServingRef = func() string { return "local/primary" }

		retry, err := loop.handleRefusal(&llm.RefusalError{Source: "stop_reason", FinishReason: "refusal"})
		require.NoError(t, err)
		assert.True(t, retry)
		assert.Equal(t, "local/spec-fb", loop.GetModelOverride())
	})
}

func TestAgentLoop_RefusalFallbackRef_Precedence(t *testing.T) {
	tests := []struct {
		name   string
		spec   *AgentSpec
		global string
		want   string
	}{
		{name: "spec wins", spec: &AgentSpec{RefusalModel: "spec-m"}, global: "global-m", want: "spec-m"},
		{name: "global fills empty spec", spec: &AgentSpec{}, global: "global-m", want: "global-m"},
		{name: "both empty off", spec: &AgentSpec{}, global: "", want: ""},
		{name: "nil spec with global", spec: nil, global: "global-m", want: "global-m"},
		{name: "nil spec no global", spec: nil, global: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loop := NewAgentLoop("sess-ref-"+tt.name, t.TempDir())
			loop.spec = tt.spec
			loop.globalRefusalModel = tt.global
			assert.Equal(t, tt.want, loop.refusalFallbackRef())
		})
	}
}

// --- Task 2: loop hook site + override pinning ---

// TestAgentLoop_RefusalError_RetriesFallbackThenSucceeds drives the REAL
// error path (chatWithFailoverRaw): call 1 refuses, the hook pins the
// fallback via the request-scoped override, call 2 is served by the fallback
// model and succeeds. The wire models assert the pin actually re-targeted
// the request (WithModelOverride outranks the alias channel).
func TestAgentLoop_RefusalError_RetriesFallbackThenSucceeds(t *testing.T) {
	cap := &modelCapture{}
	server := newModelCaptureServer(cap)
	defer server.Close()

	resolver := resolvedAliasResolver(t, server.URL, "lfm-8b-q4", "alias-served-model")

	inner := llm.NewClient(&llm.ModelConfig{
		BaseURL:    server.URL,
		ProviderID: "local",
		ModelID:    "wire-model",
	})
	chatter := &refusalChatter{inner: inner, refusals: 1}
	loop := NewAgentLoop("sess-refusal-retry", t.TempDir(),
		WithLoopLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
		WithResolver(resolver),
		WithModelRef(testClassifierAlias),
		WithLLMChatter(chatter),
	)
	// Fallback: the alias "fb" resolves to local/fb-model on the SAME
	// endpoint so the wire capture sees it.
	fbResolver := llm.NewResolver(&llm.ProvidersConfig{
		ModelAliases: map[string]llm.ModelAliasEntry{
			"fb": {Models: []string{"local/fb-model"}},
		},
		Providers: map[string]llm.ProviderConfig{
			"local": {API: "openai", Options: llm.ProviderOptionsConfig{BaseURL: server.URL}, Models: map[string]llm.ModelDef{
				"fb-model": {Name: "fb-model"},
			}},
		},
	}, nil)
	loop.refusalResolver = fbResolver
	loop.spec = &AgentSpec{RefusalModel: "fb"}
	loop.refusalEventPublisher = func(string, map[string]any) {} // no bus wired
	loop.refusalOverrideApplier = loop.SetPersistentModelOverride
	loop.refusalOverrideClear = loop.ClearModelOverride

	resp, err := loop.chatWithFailoverRaw(context.Background(),
		[]llm.ChatMessage{{Role: llm.RoleUser, Content: "hello"}}, nil)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, 2, chatter.callCount(), "exactly 2 model calls: refusal + fallback retry")
	// Call 1 (the refusal) short-circuits at the stub and never dials; the
	// ONE wire request is the fallback retry, and it must carry the pinned
	// fallback model (WithModelOverride outranks the alias channel).
	assert.Equal(t, 1, cap.count())
	assert.Equal(t, "fb-model", cap.models[0])
	// The fallback pin is persistent for the remainder of the turn window
	// (mirroring the escalation override); it must be clearable via the
	// fresh-turn restore seam — no sticky fallback across turns.
	loop.ClearRefusalFallback()
	assert.Empty(t, loop.GetModelOverride(), "fallback override must clear on fresh-turn restore")
}

// --- Task 3: invariant pins ---

// TestAgentLoop_RefusalError_NeverRecordAliasFailure: a RefusalError on the
// error path must NEVER reach RecordAliasFailure / BlockQuota /
// RotateToNextModel. The hook and handleRefusal report into the recorder.
func TestAgentLoop_RefusalError_NeverRecordAliasFailure(t *testing.T) {
	cap := &modelCapture{}
	server := newModelCaptureServer(cap)
	defer server.Close()

	resolver := resolvedAliasResolver(t, server.URL, "lfm-8b-q4", "alias-served-model")

	inner := llm.NewClient(&llm.ModelConfig{
		BaseURL:    server.URL,
		ProviderID: "local",
		ModelID:    "wire-model",
	})
	chatter := &refusalChatter{inner: inner, refusals: 1}
	loop := NewAgentLoop("sess-refusal-invariant", t.TempDir(),
		WithLoopLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
		WithResolver(resolver),
		WithModelRef(testClassifierAlias),
		WithLLMChatter(chatter),
	)
	failures := 0
	loop.refusalResolver = &recordingResolver{
		refusalResolver: &refusalResolver{resolved: map[string]string{"fb": "local/fb-model"}},
	}
	loop.spec = &AgentSpec{RefusalModel: "fb"}
	loop.refusalEventPublisher = func(string, map[string]any) {}
	loop.refusalOverrideApplier = loop.SetPersistentModelOverride
	loop.refusalOverrideClear = loop.ClearModelOverride
	loop.refusalFailureRecorder = func(where string) { failures++ }

	_, err := loop.chatWithFailoverRaw(context.Background(),
		[]llm.ChatMessage{{Role: llm.RoleUser, Content: "hello"}}, nil)
	require.NoError(t, err)

	assert.Equal(t, 0, failures,
		"a RefusalError reached an alias-failure-class seam — the model is healthy, it declined")
}

// TestAgentLoop_RefusalError_FallbackAlsoRefuses_TwoCallsExactly: the
// fallback refusing too must surface the RefusalError after EXACTLY 2 model
// calls — no third attempt, no rotation.
func TestAgentLoop_RefusalError_FallbackAlsoRefuses_TwoCallsExactly(t *testing.T) {
	cap := &modelCapture{}
	server := newModelCaptureServer(cap)
	defer server.Close()

	resolver := resolvedAliasResolver(t, server.URL, "lfm-8b-q4", "alias-served-model")

	inner := llm.NewClient(&llm.ModelConfig{
		BaseURL:    server.URL,
		ProviderID: "local",
		ModelID:    "wire-model",
	})
	chatter := &refusalChatter{inner: inner, refusals: 2}
	loop := NewAgentLoop("sess-refusal-two-hops", t.TempDir(),
		WithLoopLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
		WithResolver(resolver),
		WithModelRef(testClassifierAlias),
		WithLLMChatter(chatter),
	)
	loop.refusalResolver = llm.NewResolver(&llm.ProvidersConfig{
		ModelAliases: map[string]llm.ModelAliasEntry{
			"fb": {Models: []string{"local/fb-model"}},
		},
		Providers: map[string]llm.ProviderConfig{
			"local": {API: "openai", Options: llm.ProviderOptionsConfig{BaseURL: server.URL}, Models: map[string]llm.ModelDef{
				"fb-model": {Name: "fb-model"},
			}},
		},
	}, nil)
	loop.spec = &AgentSpec{RefusalModel: "fb"}
	loop.refusalEventPublisher = func(string, map[string]any) {}
	loop.refusalOverrideApplier = loop.SetPersistentModelOverride
	loop.refusalOverrideClear = loop.ClearModelOverride

	_, err := loop.chatWithFailoverRaw(context.Background(),
		[]llm.ChatMessage{{Role: llm.RoleUser, Content: "hello"}}, nil)
	require.Error(t, err, "double refusal must surface, not loop")
	var refusal *llm.RefusalError
	require.True(t, errors.As(err, &refusal), "the RefusalError itself must surface")

	assert.Equal(t, 2, chatter.callCount(), "exactly 2 model calls — no third attempt")
}

// --- test helper: event-capturing bus stand-in ---

// eventCaptureBus records payloads published through the EventPublisher seam.
type eventCaptureBus struct {
	mu       sync.Mutex
	payloads []map[string]any
}

func newEventCaptureBus() *eventCaptureBus { return &eventCaptureBus{} }

func (b *eventCaptureBus) publisher() EventPublisher {
	return func(topic string, payload map[string]any) {
		b.mu.Lock()
		defer b.mu.Unlock()
		cloned := make(map[string]any, len(payload))
		for k, v := range payload {
			cloned[k] = v
		}
		b.payloads = append(b.payloads, cloned)
	}
}

func (b *eventCaptureBus) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.payloads)
}

func (b *eventCaptureBus) payload(t *testing.T, i int) map[string]any {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	require.Greater(t, len(b.payloads), i)
	return b.payloads[i]
}

// silence unused-import guards for helpers used only in some builds.
var (
	_ = json.Marshal
	_ = io.Discard
)
