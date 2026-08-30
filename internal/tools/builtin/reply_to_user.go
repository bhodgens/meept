package builtin

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// ReplyToUserTool lets an agent speak to the user mid-turn through the
// harness speak router (leaf 11). The model always ends a turn the same way;
// the harness decides bubble vs notify vs parent report. The tool's
// structured result is a fire-and-forget ack — it does NOT terminate the
// turn, and the loop continues so the model can act on the ack.
//
// The agent loop injects the reply carrier at turn start via SetReplyFunc
// (agent.ReplyFuncSetter; ask.go SetResponseFunc pattern): the closure
// classifies the current run (session-attached / detached / isolated child)
// and routes through the loop's SpeakRouter. Isolated children get an error
// ("isolated child cannot speak to user") per C4.
//
// The loop detects a tool-notify this turn by tool-call name, so the
// post-turn final-text notify de-duplicates against this delivery.
type ReplyToUserTool struct {
	tools.ToolDefaults
	mu       sync.Mutex
	replyFn  func(text string) error
	notified bool
}

// NewReplyToUserTool creates the tool. The reply carrier may be provided
// here or injected later via SetReplyFunc; nil is safe but Execute returns
// an error result until wired.
func NewReplyToUserTool(replyFunc func(text string) error) *ReplyToUserTool {
	t := &ReplyToUserTool{}
	if replyFunc != nil {
		t.replyFn = replyFunc
	}
	return t
}

// SetReplyFunc injects the reply carrier. Typed-nil guard: a nil function is
// never stored (ask.go SetResponseFunc pattern). Called by the agent loop at
// turn start; the loop supplies a closure bound to its speak router and
// session/conversation context.
func (t *ReplyToUserTool) SetReplyFunc(fn func(text string) error) {
	if fn == nil {
		return
	}
	t.mu.Lock()
	t.replyFn = fn
	t.mu.Unlock()
}

// Notified reports whether the model already delivered a reply this turn.
// Informational: the loop's dedup uses the tool-call name scan so it stays
// correct even when the delivery was wired through a different route.
func (t *ReplyToUserTool) Notified() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.notified
}

// ResetNotified clears the per-turn notify flag.
func (t *ReplyToUserTool) ResetNotified() {
	t.mu.Lock()
	t.notified = false
	t.mu.Unlock()
}

func (t *ReplyToUserTool) Name() string { return "reply_to_user" }

func (t *ReplyToUserTool) Category() string { return "agent" }

func (t *ReplyToUserTool) Description() string {
	return "Send a message to the user while you continue working. Use this to surface intermediate findings or progress on long tasks without stopping. The harness decides how the message is delivered (chat, notification, or report). Fire-and-forget: returns an ack immediately."
}

func (t *ReplyToUserTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"text": {
				Type:        schemaTypeString,
				Description: "The message to deliver to the user. Be specific and concise.",
			},
		},
		Required: []string{"text"},
	}
}

// ReplyResult is the structured ack returned by the tool.
type ReplyResult struct {
	Ack     bool   `json:"ack"`
	Message string `json:"message,omitempty"`
}

func (t *ReplyToUserTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	text, _ := args["text"].(string)
	if strings.TrimSpace(text) == "" {
		return tools.NewErrorResult("text is required and must be a non-empty string"), nil
	}

	t.mu.Lock()
	replyFn := t.replyFn
	t.mu.Unlock()
	if replyFn == nil {
		return tools.NewErrorResult("reply_to_user tool is not available: no reply carrier configured"), nil
	}

	if err := replyFn(text); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("reply_to_user failed: %w", err)
	}
	t.mu.Lock()
	t.notified = true
	t.mu.Unlock()

	return tools.NewSuccessResult(ReplyResult{
		Ack:     true,
		Message: "message delivered to user",
	}), nil
}

// Ensure ReplyToUserTool implements the Tool interface.
var _ tools.Tool = (*ReplyToUserTool)(nil)

// Ensure the loop's turn-start wiring discovers this tool: the agent loop
// injects its speak carrier into every registry tool implementing
// agent.ReplyFuncSetter (satisfied structurally — same method signature).
var _ agent.ReplyFuncSetter = (*ReplyToUserTool)(nil)
