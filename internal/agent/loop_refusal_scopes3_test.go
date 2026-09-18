package agent

// scopes-3 audit fixes:
//
//   - Finding A (MED): a streaming refusal-fallback retry reused the same
//     delta accumulator, so the fallback's full text appended after the
//     refused attempt's partial tokens in the client-visible text_so_far
//     preview. The fix bumps a streamAttemptEpoch when handleRefusal arms a
//     retry; the accumulator resets when the epoch moves.
//
//   - Finding B (LOW): the one-hop refusal budget reset across park/resume
//     generations (clearRefusalFreshTurnState cleared the pin on every
//     resume), so refuse → park → resume → refuse cycles each earned a
//     fresh fallback hop. The fix carries a persistent hop counter on the
//     loop that survives resume re-entry and resets only on a genuinely
//     fresh (non-resumed) turn.

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// streamingRefusalChatter emits partial deltas then refuses on its first
// call; subsequent calls stream fallback text successfully. It records every
// delta the caller saw, in order, so tests can assert the preview the client
// would have rendered.
type streamingRefusalChatter struct {
	mu          sync.Mutex
	streamed    []string // deltas the caller received, concatenated in order
	calls       int
	streamCalls int
	deltasEmu   []string
}

func (c *streamingRefusalChatter) record(deltas ...string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deltasEmu = append(c.deltasEmu, deltas...)
}

func (c *streamingRefusalChatter) Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	c.mu.Lock()
	c.calls++
	call := c.calls
	deltas := c.deltasEmu
	c.mu.Unlock()

	if call == 1 {
		// Refused attempt: emit partial tokens, then refuse.
		var got []string
		for _, d := range deltas {
			got = append(got, d)
		}
		c.mu.Lock()
		c.streamed = append(c.streamed, got...)
		c.mu.Unlock()
		return nil, &llm.RefusalError{
			ProviderID:   "local",
			ModelID:      "primary",
			Source:       "stop_reason",
			FinishReason: "refusal",
		}
	}
	// Fallback attempt: stream the full fallback text.
	c.mu.Lock()
	c.streamed = append(c.streamed, deltas...)
	c.mu.Unlock()
	content := ""
	for _, d := range deltas {
		content += d
	}
	return &llm.Response{Content: content, Model: "fb-model"}, nil
}

func (c *streamingRefusalChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return c.Chat(ctx, messages, opts...)
}

func (c *streamingRefusalChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ProviderID: "local", ModelID: "primary"}
}

func (c *streamingRefusalChatter) ChatWithDeltaCallback(ctx context.Context, messages []llm.ChatMessage, onDelta llm.DeltaCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	c.mu.Lock()
	c.streamCalls++
	streamCall := c.streamCalls
	calls := c.calls
	c.mu.Unlock()
	if streamCall == 1 {
		// Refused attempt: stream its partial tokens through the callback
		// FIRST — the caller's accumulator must consume them — then refuse.
		for _, d := range c.deltasEmu[:2] {
			if err := onDelta(d); err != nil {
				return nil, err
			}
		}
		return c.Chat(ctx, messages, opts...)
	}
	// Fallback attempt: stream ONLY this attempt's deltas (a real fallback
	// never replays the refused attempt's tokens), then serve the response.
	for _, d := range c.deltasEmu[2:] {
		if err := onDelta(d); err != nil {
			return nil, err
		}
	}
	c.mu.Lock()
	if calls == 0 {
		c.calls = 1 // keep the refusal accounting coherent if misused
	}
	c.mu.Unlock()
	return c.Chat(ctx, messages, opts...)
}

// streamingRefusalLoop wires a loop like the leaf-03 retry tests, with the
// streaming refusal chatter installed.
func streamingRefusalLoop(t *testing.T, chatter *streamingRefusalChatter) *AgentLoop {
	t.Helper()
	loop := NewAgentLoop("sess-refusal-stream", t.TempDir(),
		WithLoopLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
		WithLLMChatter(chatter),
	)
	loop.refusalResolver = &refusalResolver{resolved: map[string]string{"fb": "local/fb-model"}}
	loop.spec = &AgentSpec{RefusalModel: "fb"}
	loop.refusalEventPublisher = func(string, map[string]any) {}
	loop.refusalOverrideApplier = loop.SetPersistentModelOverride
	loop.refusalOverrideClear = loop.ClearModelOverride
	return loop
}

// TestRefusalFallback_StreamRetryResetsAccumulator (scopes-3 finding A): the
// first attempt streams partial tokens and refuses; the fallback retry's
// text must NOT append after the refused partial in the accumulator that
// feeds the client-visible text_so_far preview — the epoch-gated reset in
// RunOnceWithParts' streamOnDelta must yield the fallback text only. Driven
// through the REAL chatWithFailoverRaw retry path: the refused attempt's
// deltas reach the caller's callback (the loop closure's own accumulator is
// what resets), so this test mirrors the RunOnceWithParts closure shape via
// newStreamAccumulator against the same epoch the retry armed.
func TestRefusalFallback_StreamRetryResetsAccumulator(t *testing.T) {
	chatter := &streamingRefusalChatter{}
	chatter.record("partial ans", "wer that w")
	chatter.record("fallback", " full", " text")
	loop := streamingRefusalLoop(t, chatter)

	// The exact accumulator shape RunOnceWithParts builds: epoch-gated,
	// reset on attempt change, feeding text_so_far.
	acc := newStreamAccumulator(loop)
	var published []string
	onDelta := func(delta string) error {
		published = append(published, acc.observe(delta))
		return nil
	}

	// Drive the same retry path chatWithFailoverRaw drives: refuse →
	// handleRefusal arms the pin (epoch bump) → retry streams via
	// ChatWithDeltaCallback exactly like the loop's wrappedOnDelta path.
	resp, err := loop.chatWithFailoverRaw(context.Background(),
		[]llm.ChatMessage{{Role: llm.RoleUser, Content: "hello"}}, onDelta)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, 2, chatter.calls, "refused attempt + fallback retry")
	require.NotEmpty(t, published, "the refused attempt's partial stream must reach the preview accumulator")

	// The client-visible preview: refused partials + fallback text would be
	// the bug. The epoch reset must leave ONLY the fallback text.
	assert.Equal(t, "fallback full text", published[len(published)-1],
		"stream accumulator must reset on the refusal retry: refused-partial + fallback concatenation is the finding-A bug")
}

// TestRefusalFallback_StreamEpochResetsAccumulator_Unit is a tighter unit
// pin on the epoch mechanism itself: the accumulator built by
// newStreamAccumulator discards the refused attempt's partial text when the
// epoch bumps (handleRefusal arming a retry).
func TestRefusalFallback_StreamEpochResetsAccumulator_Unit(t *testing.T) {
	loop := NewAgentLoop("sess-epoch", t.TempDir(),
		WithLoopLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))

	require.Equal(t, int64(0), loop.streamAttemptEpochSnapshot())

	// The RunOnceWithParts accumulator: refusal attempt streams first.
	acc := newStreamAccumulator(loop)
	require.Equal(t, "refused partial", acc.observe("refused partial"))

	// handleRefusal arms the retry → epoch bumps → the next delta resets.
	loop.mu.Lock()
	loop.refusalFallbackHops++
	loop.streamAttemptEpoch.Add(1)
	loop.mu.Unlock()

	assert.Equal(t, "fallback text", acc.observe("fallback text"),
		"the epoch bump must discard the refused attempt's partial stream")
}

// TestRefusalFallback_HopBudgetSurvivesResume (scopes-3 finding B): a
// resumed turn re-enters RunOnceWithParts-like fresh-turn state clearing but
// must NOT reset the persistent hop budget; after maxRefusalFallbackHops
// pins the guard surfaces the refusal instead of re-arming.
func TestRefusalFallback_HopBudgetSurvivesResume(t *testing.T) {
	loop := NewAgentLoop("sess-hop-budget", t.TempDir(),
		WithLoopLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	loop.spec = &AgentSpec{RefusalModel: "fb"}
	loop.refusalResolver = &refusalResolver{resolved: map[string]string{"fb": "local/fb-model"}}
	loop.refusalEventPublisher = func(string, map[string]any) {}
	loop.refusalOverrideApplier = loop.SetPersistentModelOverride
	loop.refusalOverrideClear = loop.ClearModelOverride

	refusal := &llm.RefusalError{ProviderID: "local", ModelID: "primary", Source: "stop_reason", FinishReason: "refusal"}

	// Generation 1: fresh turn, refusal falls back (hop 1 armed).
	retry, err := loop.handleRefusal(refusal)
	require.NoError(t, err)
	assert.True(t, retry, "first refusal on a fresh turn arms the fallback")
	assert.Equal(t, 1, func() int { loop.mu.RLock(); defer loop.mu.RUnlock(); return loop.refusalFallbackHops }())
	assert.False(t, loop.refusalHopsExhausted())

	// Turn parked mid-fallback: the resume re-runs clearRefusalFreshTurnState
	// (pin clears — correct) but the budget must survive.
	loop.clearRefusalFreshTurnState()
	assert.Empty(t, loop.GetModelOverride(), "resume clears the pin (correct, unchanged behavior)")
	assert.False(t, loop.refusalHopsExhausted(), "budget survives the resume — it is not turn-scoped")

	// Generation 2 (resumed): refusal again — the second hop arms.
	retry, err = loop.handleRefusal(refusal)
	require.NoError(t, err)
	assert.True(t, retry, "second refusal arms the second (final) hop")
	assert.True(t, loop.refusalHopsExhausted())

	// Generation 3 (another resume): the guard must surface the refusal —
	// no third fallback arm.
	retry, err = loop.handleRefusal(refusal)
	require.Error(t, err, "budget exhausted: the original refusal must surface")
	assert.False(t, retry, "no third fallback arm across park/resume generations")
	var surfaced *llm.RefusalError
	require.True(t, errors.As(err, &surfaced), "the RefusalError itself must surface")
}

// TestRefusalFallback_HopBudgetResetsOnGenuinelyFreshTurn (scopes-3 finding
// B complement): the budget is not a session-life sentence — a genuinely
// fresh (non-resumed) turn consumes the cleared marker and resets the count,
// so a later unrelated turn can still use its fallback.
func TestRefusalFallback_HopBudgetResetsOnGenuinelyFreshTurn(t *testing.T) {
	loop := NewAgentLoop("sess-hop-reset", t.TempDir(),
		WithLoopLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	loop.spec = &AgentSpec{RefusalModel: "fb"}
	loop.refusalResolver = &refusalResolver{resolved: map[string]string{"fb": "local/fb-model"}}
	loop.refusalEventPublisher = func(string, map[string]any) {}
	loop.refusalOverrideApplier = loop.SetPersistentModelOverride
	loop.refusalOverrideClear = loop.ClearModelOverride

	refusal := &llm.RefusalError{ProviderID: "local", ModelID: "primary", Source: "stop_reason", FinishReason: "refusal"}

	// Spend the budget on an earlier turn: refusal → fallback armed and
	// consumed (fresh-turn clear mirrors the turn boundary) → refusal again
	// → second hop armed.
	retry, err := loop.handleRefusal(refusal)
	require.NoError(t, err)
	require.True(t, retry)
	loop.clearRefusalFreshTurnState()
	retry, err = loop.handleRefusal(refusal)
	require.NoError(t, err)
	require.True(t, retry)
	require.True(t, loop.refusalHopsExhausted())

	// A later genuinely-fresh turn (no WithResumedTurn): the turn-start
	// sequence clears the fresh state and resets the budget.
	loop.clearRefusalFreshTurnState()
	loop.resetRefusalHopBudgetOnFreshTurn()
	assert.False(t, loop.refusalHopsExhausted(), "a genuinely fresh turn resets the cross-generation budget")

	// And a resumed turn must NOT reset it: spend the fresh budget with a
	// full two-hop cycle (refuse → clear/consume → refuse), then simulate
	// the resume path order — clearRefusalFreshTurnState only.
	retry, err = loop.handleRefusal(refusal)
	require.NoError(t, err)
	require.True(t, retry)
	loop.clearRefusalFreshTurnState()
	retry, err = loop.handleRefusal(refusal)
	require.NoError(t, err)
	require.True(t, retry)
	require.True(t, loop.refusalHopsExhausted())
	loop.clearRefusalFreshTurnState()
	assert.True(t, loop.refusalHopsExhausted(),
		"resume re-entry (clear without reset) must keep the budget spent")
}
