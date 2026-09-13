package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/pkg/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Pins for the tool-call cycle guard, fresh-rig daemon11.log (2026-09-13).
//
// The observed failure: list_directory was called four times with empty
// arguments, the loop nudged ("No measurable progress on repeated calls,
// nudging"), and in the SAME iteration the byte-level cycle detector aborted
// the turn with args_hash=empty count=3 -- preempting the nudge that was
// supposed to correct the behaviour, reporting a constant hash for two
// different argument shapes ("" and "{}"), and surfacing only
// "agent execution failed: agent detected a cycle in tool calls" (no tool, no
// count, no reason).
//
// Thresholds after the fix (CycleThreshold: 3, NoProgressWarnAt: 3):
//   repeat 1, 2 -> ok, no guard action
//   repeat 3    -> no-progress ladder injects the corrective nudge; the cycle
//                  guard does NOT abort (it used to)
//   repeat 4    -> cycle abort (the model repeated the identical call AFTER
//                  the nudge was delivered)

func newCycleGuardTestDetector() *cycleDetector {
	return newCycleDetector(DetectionConfig{
		CycleThreshold:       3,
		ConvergenceThreshold: 3,
		HistorySize:          10,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// ---------------------------------------------------------------------------
// Problem 1: the argument hash must not collapse distinct arguments
// ---------------------------------------------------------------------------

func TestHashArgs_DistinguishesNoArgsFromEmptyObject(t *testing.T) {
	noArgs := hashArgs("")
	emptyObj := hashArgs("{}")

	assert.NotEqual(t, noArgs, emptyObj,
		`a call with no arguments is not a call with an empty object: "" and "{}" must hash differently`)
	assert.NotEqual(t, "empty", noArgs, `the old "empty" sentinel collapsed both shapes onto one key`)
	assert.NotEqual(t, "empty", emptyObj, `the old "empty" sentinel collapsed both shapes onto one key`)
	assert.Equal(t, 16, len(noArgs), "hashArgs must return the real digest, not a sentinel")
	assert.NotContains(t, noArgs+emptyObj, "empty", "no argument shape may be special-cased to a literal key")
}

func TestHashArgs_HashesExactArguments(t *testing.T) {
	// A changed argument is a different call, byte for byte. Near-duplicates
	// (key order / whitespace) are the normalized no-progress ladder's job
	// (HashToolCall), never the abort guard's.
	assert.NotEqual(t, hashArgs(`{"path":"/x"}`), hashArgs(`{"path": "    /x"}`),
		"argument text that differs must hash differently so a retry can never look identical")
	assert.NotEqual(t, hashArgs(`{}`), hashArgs(`{"path": "/x"}`))
	assert.Equal(t, hashArgs(`{"path":"/x"}`), hashArgs(`{"path":"/x"}`),
		"identical arguments must still hash identically")
}

// ---------------------------------------------------------------------------
// Problems 1 + 4 at the detector level
// ---------------------------------------------------------------------------

func TestCycleDetector_NoArgsAndEmptyObjectDoNotCollide(t *testing.T) {
	cd := newCycleGuardTestDetector()

	// "" "" {} {} -- under the old "empty" key this was four identical
	// signatures and aborted on the third.
	seq := []struct {
		args    string
		repeats int
	}{
		{``, 1},
		{``, 2},
		{`{}`, 1}, // run restarts: a different argument shape
		{`{}`, 2},
	}
	for i, step := range seq {
		abort, repeats := cd.recordCall("list_directory", step.args)
		assert.False(t, abort,
			"call %d (args=%q): distinct argument shapes must not abort", i+1, step.args)
		assert.Equal(t, step.repeats, repeats,
			"call %d (args=%q): run length", i+1, step.args)
	}
}

func TestCycleDetector_ThreeIdenticalCallsNudgeFourthAborts(t *testing.T) {
	cd := newCycleGuardTestDetector()

	for i := 1; i <= 3; i++ {
		abort, repeats := cd.recordCall("list_directory", `{}`)
		assert.False(t, abort,
			"call %d: the nudge threshold must not abort -- the no-progress ladder injects its corrective nudge at exactly this count", i)
		assert.Equal(t, i, repeats, "call %d: run length", i)
	}

	abort, repeats := cd.recordCall("list_directory", `{}`)
	assert.True(t, abort,
		"the 4th identical call (one repeat after the nudge was issued) must abort")
	assert.Equal(t, 4, repeats)
}

func TestCycleDetector_ArgumentChangeResetsRun(t *testing.T) {
	cd := newCycleGuardTestDetector()

	// (tool, {}), (tool, {}), (tool, {"path": "/x"}) must never abort.
	for i := 1; i <= 2; i++ {
		abort, repeats := cd.recordCall("list_directory", `{}`)
		require.False(t, abort, "call %d must not abort", i)
		require.Equal(t, i, repeats)
	}

	abort, repeats := cd.recordCall("list_directory", `{"path": "/x"}`)
	assert.False(t, abort, "a retry that changes its arguments must never trip the guard")
	assert.Equal(t, 1, repeats, "the changed call restarts the run at 1")

	// The new signature then needs its own full run (3 nudged + 1) to abort.
	for i := 2; i <= 3; i++ {
		abort, repeats = cd.recordCall("list_directory", `{"path": "/x"}`)
		assert.False(t, abort, "call %d of the new signature must not abort", i)
		assert.Equal(t, i, repeats)
	}
	abort, repeats = cd.recordCall("list_directory", `{"path": "/x"}`)
	assert.True(t, abort, "the new signature's own 4th call aborts")
	assert.Equal(t, 4, repeats)
}

// A degenerate/unset detection config must not panic (the old detectCycle
// indexed an empty window when CycleThreshold <= 0) and must never abort on a
// tool's first call.
func TestCycleDetector_DegenerateConfigDoesNotPanic(t *testing.T) {
	cd := newCycleDetector(DetectionConfig{HistorySize: 0},
		slog.New(slog.NewTextHandler(io.Discard, nil)))

	abort, repeats := cd.recordCall("list_directory", "")
	assert.False(t, abort, "a tool's first call must never abort")
	assert.Equal(t, 1, repeats)

	abort, repeats = cd.recordCall("list_directory", "")
	assert.True(t, abort, "with CycleThreshold unset the second identical call is the abort")
	assert.Equal(t, 2, repeats)
	assert.Equal(t, 1, len(cd.history), "the history window stays bounded when HistorySize is unset")
}

// ---------------------------------------------------------------------------
// Problem 3: the terminal error stays classifiable AND explains itself
// ---------------------------------------------------------------------------

func TestCycleAbortError_ClassifiableAndDescriptive(t *testing.T) {
	err := cycleAbortError("list_directory", 4)

	require.True(t, errors.Is(err, ErrCycleDetected),
		"errors.Is must still classify the abort as a cycle (callers distinguish it from a real tool failure)")
	assert.Contains(t, err.Error(), "list_directory", "the surfaced error must name the tool")
	assert.Contains(t, err.Error(), "4", "the surfaced error must carry the repeat count")
	assert.NotEqual(t, ErrCycleDetected.Error(), err.Error(),
		"the surfaced error must not be the bare sentinel string")

	// A real tool failure must NOT be classifiable as a cycle.
	toolFailure := fmt.Errorf("tool execution failed: %w", errors.New("no path specified"))
	assert.False(t, errors.Is(toolFailure, ErrCycleDetected),
		"a tool failure must never look like a cycle abort")
}

// ---------------------------------------------------------------------------
// Problem 2 integration: the nudge is delivered before the abort
// ---------------------------------------------------------------------------

// cycleScriptChatter serves a fixed response script and records, for each LLM
// call it serves, whether the conversation it received already contained the
// no-progress nudge. That is the ordering evidence: the abort must happen on a
// call that had already seen the nudge.
type cycleScriptChatter struct {
	responses []*llm.Response
	callCount int
	sawNudge  []bool
}

func newCycleScriptChatter(responses ...*llm.Response) *cycleScriptChatter {
	return &cycleScriptChatter{responses: responses}
}

func (m *cycleScriptChatter) Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	if m.callCount >= len(m.responses) {
		return nil, errors.New("no more mock responses")
	}
	nudged := false
	for _, msg := range messages {
		if strings.Contains(msg.Content, "no measurable progress") {
			nudged = true
			break
		}
	}
	m.sawNudge = append(m.sawNudge, nudged)
	resp := m.responses[m.callCount]
	m.callCount++
	return resp, nil
}

func (m *cycleScriptChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return m.Chat(ctx, messages, opts...)
}

func (m *cycleScriptChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "cycle-script-chatter"}
}

// repeatedCallTool returns a tool-call response for the same tool/args.
func repeatedCallTool(id int, tool, args string) *llm.Response {
	return &llm.Response{
		Content:      "working",
		FinishReason: "tool_calls",
		Usage:        llm.TokenUsage{TotalTokens: 10},
		ToolCalls: []llm.ToolCall{{
			ID:   fmt.Sprintf("tc-%d", id),
			Type: "function",
			Function: llm.ToolCallFunction{
				Name:      tool,
				Arguments: args,
			},
		}},
	}
}

func newCycleGuardLoop(t *testing.T, chatter *cycleScriptChatter, toolName string) *AgentLoop {
	t.Helper()
	registry := NewPlaceholderToolRegistry()
	registry.Register(&mockGuardTool{name: toolName})
	secChecker := security.NewPermissionChecker(security.Config{})
	loop := NewAgentLoop("test-session", "/tmp",
		WithLLMChatter(chatter),
		WithToolRegistry(registry),
		WithSecurityChecker(secChecker),
		WithMessageBus(bus.New(nil, slogDiscardLogger())),
		WithAgentConfig(AgentConfig{
			MaxIterations: 10,
			Guards:        DefaultGuardConfig(),
		}),
	)
	loop.executor = NewExecutor(registry, secChecker)
	return loop
}

// Four identical list_directory calls with empty arguments (the daemon11.log
// shape): the 3rd must nudge and continue, the 4th must abort with an error
// naming the tool and the count -- and the 4th LLM call must be the first one
// that had already seen the nudge.
func TestCycleGuardIntegration_NudgePrecedesAbortAndExplainsItself(t *testing.T) {
	chatter := newCycleScriptChatter(
		repeatedCallTool(1, "list_directory", `{}`),
		repeatedCallTool(2, "list_directory", `{}`),
		repeatedCallTool(3, "list_directory", `{}`),
		repeatedCallTool(4, "list_directory", `{}`),
		&llm.Response{Content: "should never be reached", Usage: llm.TokenUsage{TotalTokens: 1}},
	)
	loop := newCycleGuardLoop(t, chatter, "list_directory")

	response, err := loop.RunOnce(context.Background(), "list the project", "conv-cycle-nudge-first")

	require.Error(t, err, "four identical calls in a row must abort")
	require.True(t, errors.Is(err, ErrCycleDetected),
		"the abort must stay classifiable as a cycle, got %v", err)
	assert.Contains(t, err.Error(), "list_directory", "the surfaced error must name the tool")
	assert.Contains(t, err.Error(), "4", "the surfaced error must carry the repeat count")
	assert.NotEqual(t, ErrCycleDetected.Error(), err.Error(),
		"the surfaced error must not be the bare 'agent detected a cycle in tool calls'")

	// The user-facing reply is the loop's own explanation, not the generic
	// "I encountered an error during processing" substitution.
	assert.Contains(t, response, "list_directory", "the reply must say which action was repeating")
	assert.Contains(t, response, "4", "the reply must say how often it repeated")
	assert.NotContains(t, response, "I encountered an error during processing")

	// Ordering: the abort fired on the 4th LLM call, i.e. the model repeated
	// the identical call AFTER the nudge had been delivered (call index 2 =
	// iteration 3 must not have seen a nudge yet; index 3 = iteration 4 must).
	require.Equal(t, 4, chatter.callCount,
		"the abort must happen on the 4th repetition, not the 3rd (which is the nudge)")
	require.Len(t, chatter.sawNudge, 4)
	assert.False(t, chatter.sawNudge[0], "iteration 1: no nudge yet")
	assert.False(t, chatter.sawNudge[1], "iteration 2: no nudge yet")
	assert.False(t, chatter.sawNudge[2], "iteration 3 is where the nudge is issued, it cannot have seen one")
	assert.True(t, chatter.sawNudge[3],
		"iteration 4 must already have seen the corrective nudge -- the abort must never preempt it")

	// The nudge really landed in the conversation.
	conv := loop.conversations.Get("conv-cycle-nudge-first")
	require.NotNil(t, conv)
	nudges := 0
	for _, m := range conv.GetMessages() {
		if strings.Contains(m.Content, "no measurable progress") {
			nudges++
		}
	}
	assert.GreaterOrEqual(t, nudges, 1, "the corrective nudge must be present in the conversation")
}

// The ladder-absent branch: with no no-progress ladder wired there is nothing
// to yield to, so the byte-level guard must still abort (never a dead guard).
func TestCycleGuardIntegration_AbortsWithoutNoProgressLadder(t *testing.T) {
	chatter := newCycleScriptChatter(
		repeatedCallTool(1, "list_directory", `{}`),
		repeatedCallTool(2, "list_directory", `{}`),
		repeatedCallTool(3, "list_directory", `{}`),
		repeatedCallTool(4, "list_directory", `{}`),
		&llm.Response{Content: "should never be reached", Usage: llm.TokenUsage{TotalTokens: 1}},
	)
	loop := newCycleGuardLoop(t, chatter, "list_directory")
	loop.noProgress = nil

	_, err := loop.RunOnce(context.Background(), "list the project", "conv-cycle-no-ladder")

	require.Error(t, err, "the cycle guard must still fire when the ladder is not wired")
	assert.True(t, errors.Is(err, ErrCycleDetected), "got %v", err)
	assert.Equal(t, 4, chatter.callCount)
}

// A retry that changes its arguments must never trip the guard -- including the
// colliding pair that used to share the "empty" key.
func TestCycleGuardIntegration_ChangedArgumentsNeverAbort(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"empty object then a real path", []string{`{}`, `{}`, `{"path": "/x"}`}},
		{"no-args call is not an empty-object call", []string{``, ``, `{}`, `{}`}},
		{"path then a different path", []string{`{"path": "/x"}`, `{"path": "/x"}`, `{"path": "/y"}`}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			responses := make([]*llm.Response, 0, len(tc.args)+1)
			for i, args := range tc.args {
				responses = append(responses, repeatedCallTool(i+1, "list_directory", args))
			}
			responses = append(responses, &llm.Response{Content: "done", Usage: llm.TokenUsage{TotalTokens: 1}})

			chatter := newCycleScriptChatter(responses...)
			loop := newCycleGuardLoop(t, chatter, "list_directory")

			response, err := loop.RunOnce(context.Background(), "list the project", "conv-cycle-changed-args")

			require.NoError(t, err, "changed arguments must never abort the turn")
			assert.Equal(t, "done", response)
			assert.Equal(t, len(tc.args)+1, chatter.callCount,
				"every scripted call must run and the final text sample must be reached")
		})
	}
}
