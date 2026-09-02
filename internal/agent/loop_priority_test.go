package agent

import (
	"context"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	pkgsecurity "github.com/caimlas/meept/pkg/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Stubs
// ---------------------------------------------------------------------------

// priorityChatter is a recording llm.Chatter stub: it captures the ChatOption
// slice of every Chat call (inspectable via llm.PriorityOf) and returns a
// canned final response, so one LLM call completes one turn.
type priorityChatter struct {
	mu        sync.Mutex
	opts      [][]llm.ChatOption // per-call captured option slices
	callCount int
}

func (m *priorityChatter) Chat(_ context.Context, _ []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	m.mu.Lock()
	m.opts = append(m.opts, opts)
	m.callCount++
	m.mu.Unlock()
	return &llm.Response{
		Content:      "priority test done",
		FinishReason: "stop",
		Usage:        llm.TokenUsage{TotalTokens: 5},
	}, nil
}

func (m *priorityChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return m.Chat(ctx, messages, opts...)
}

func (m *priorityChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "priority-test-model"}
}

// callOpts returns the captured ChatOption slice of the Nth LLM call (1-based).
func (m *priorityChatter) callOpts(n int) []llm.ChatOption {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n < 1 || n > len(m.opts) {
		return nil
	}
	return m.opts[n-1]
}

// newPriorityTestLoop builds the minimal RunOnce harness (mirrors
// guards_test.go) with the given extra loop options.
func newPriorityTestLoop(chatter llm.Chatter, extra ...LoopOption) *AgentLoop {
	secChecker := pkgsecurity.NewPermissionChecker(pkgsecurity.Config{})
	opts := []LoopOption{
		WithLLMChatter(chatter),
		WithToolRegistry(NewPlaceholderToolRegistry()),
		WithSecurityChecker(secChecker),
		WithMessageBus(bus.New(nil, slogDiscardLogger())),
		WithAgentConfig(AgentConfig{MaxIterations: 5}),
	}
	return NewAgentLoop("test-session", "/tmp", append(opts, extra...)...)
}

// ---------------------------------------------------------------------------
// D11 slot priority wiring (tree 04 leaf 03): interactive chat turns
// ---------------------------------------------------------------------------

// TestPriority_MainTurnInteractive drives the D11 wiring end to end: a loop
// built with WithInteractiveTurns(true) (the daemon-loop shape) must mark its
// main chat turn interactive at the llm.Chat seam, so the client's slot gate
// grants it ahead of waiting background callers.
func TestPriority_MainTurnInteractive(t *testing.T) {
	chatter := &priorityChatter{}
	loop := newPriorityTestLoop(chatter, WithInteractiveTurns(true))

	_, err := loop.RunOnce(context.Background(), "hello", "conv-priority-int")
	require.NoError(t, err)
	require.GreaterOrEqual(t, chatter.callCount, 1, "expected at least one LLM call")

	assert.True(t, llm.PriorityOf(chatter.callOpts(1)),
		"main chat turn opts must carry llm.WithPriority(true) on an interactive loop")
}

// TestPriority_DefaultBackground pins the no-regression rule: loops built
// without WithInteractiveTurns (verifier children, registry specialists,
// goal loops) must NOT mark chat turns interactive — priority-less callers
// keep the gate's background-lane ordering byte-identical to today.
func TestPriority_DefaultBackground(t *testing.T) {
	chatter := &priorityChatter{}
	loop := newPriorityTestLoop(chatter)

	_, err := loop.RunOnce(context.Background(), "hello", "conv-priority-bg")
	require.NoError(t, err)
	require.GreaterOrEqual(t, chatter.callCount, 1)

	assert.False(t, llm.PriorityOf(chatter.callOpts(1)),
		"chat turns of a priority-less loop must stay background (no WithPriority)")
}

// TestPriority_SessionCloneInherits covers ConfigSnapshot propagation: a
// per-session loop created from an interactive template (the
// Manager.GetOrCreateWired path for the daemon loop) stays interactive,
// while the un-optioned default remains background.
func TestPriority_SessionCloneInherits(t *testing.T) {
	interactive := newPriorityTestLoop(&priorityChatter{}, WithInteractiveTurns(true))
	bg := newPriorityTestLoop(&priorityChatter{})

	clone := NewAgentLoop("clone-session", "/tmp", interactive.ConfigSnapshot()...)
	require.NotNil(t, clone)
	assert.True(t, clone.interactiveTurns, "session clone of an interactive template must stay interactive")

	bgClone := NewAgentLoop("clone-session-bg", "/tmp", bg.ConfigSnapshot()...)
	require.NotNil(t, bgClone)
	assert.False(t, bgClone.interactiveTurns, "session clone of a background template must stay background")
}
