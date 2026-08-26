// Package builtin — agent_message.go implements inter-agent messaging
// tools (loop-economics leaf 10): send_agent_message delivers a queued
// direct message with a receipt; inbox drains the caller's unread
// messages and marks them read.
//
// Delivery model: messages are persisted in the employee.MessageStore
// (table agent_messages) so a message to a busy/offline employee waits
// until the recipient's next turn start; draining transitions
// queued→delivered so each message injects exactly once. Cross-daemon
// messaging is out of scope.
//
// The store is abstracted behind agentMessageStore because importing
// internal/employee here creates an import cycle (builtin → employee →
// bot → … → tools/builtin). The daemon wires the concrete
// *employee.MessageStore in.
package builtin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/models"
)

// MaxAgentMessageBody mirrors employee.MaxMessageBodyBytes (32KB cap on
// message bodies, enforced at enqueue). Duplicated as a constant rather
// than imported to avoid the builtin → employee import cycle.
const MaxAgentMessageBody = 32 * 1024

// AgentMessage is the tool-layer view of one stored inter-agent message.
type AgentMessage struct {
	ID          string     `json:"id"`
	From        string     `json:"from"`
	To          string     `json:"to"`
	Body        string     `json:"body"`
	State       string     `json:"state"`
	CreatedAt   time.Time  `json:"created_at"`
	DeliveredAt *time.Time `json:"delivered_at,omitempty"`
}

// agentMessageStore abstracts the employee.MessageStore methods used by
// the messaging tools.
type agentMessageStore interface {
	Enqueue(msg *AgentMessage) error
	DrainInbox(to string, limit int) ([]*AgentMessage, error)
	MarkRead(ids []string) error
}

// AgentMessager is the exported store surface the daemon implements when
// adapting employee.MessageStore for the messaging tools.
type AgentMessager = agentMessageStore

// SendAgentMessageTool sends a direct agent-to-agent message. The
// recipient must exist in the roster; unknown recipients get an error
// listing valid targets.
type SendAgentMessageTool struct {
	tools.ToolDefaults
	store        agentMessageStore
	targetExists func(id string) bool
	listTargets  func() []string
	// sender resolves the sending agent's ID at execute time. When nil,
	// "orchestrator" is used.
	sender func(ctx context.Context) string
}

// NewSendAgentMessageTool creates the send_agent_message tool.
func NewSendAgentMessageTool(
	store agentMessageStore,
	targetExists func(id string) bool,
	listTargets func() []string,
	sender func(ctx context.Context) string,
) *SendAgentMessageTool {
	return &SendAgentMessageTool{
		store:        store,
		targetExists: targetExists,
		listTargets:  listTargets,
		sender:       sender,
	}
}

func (t *SendAgentMessageTool) Name() string { return "send_agent_message" }

func (t *SendAgentMessageTool) Category() string { return "platform" }

func (t *SendAgentMessageTool) Description() string {
	return "Send a direct message to another agent by ID. The message is queued and delivered at the recipient's next turn start. Use platform_agents first to discover available agents."
}

func (t *SendAgentMessageTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"to": {
				Type:        schemaTypeString,
				Description: "The ID of the recipient agent.",
			},
			schemaPropMessage: {
				Type:        schemaTypeString,
				Description: fmt.Sprintf("The message body (max %d bytes).", MaxAgentMessageBody),
			},
		},
		Required: []string{"to", "message"},
	}
}

// SendMessageResult is the receipt returned to the sender.
type SendMessageResult struct {
	ID    string `json:"id"`
	To    string `json:"to"`
	State string `json:"state"`
}

func (t *SendAgentMessageTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.store == nil {
		return tools.NewErrorResult("message store not available"), nil
	}
	to, _ := args["to"].(string)
	body, _ := args[schemaPropMessage].(string)

	if strings.TrimSpace(to) == "" {
		return tools.NewErrorResult("to is required"), nil
	}
	if strings.TrimSpace(body) == "" {
		return tools.NewErrorResult("message is required"), nil
	}

	if t.targetExists != nil && !t.targetExists(to) {
		valid := ""
		if t.listTargets != nil {
			valid = strings.Join(t.listTargets(), ", ")
		}
		if valid == "" {
			valid = "(no agents registered)"
		}
		return tools.NewErrorResult(fmt.Sprintf(
			"unknown recipient %q. valid targets: %s", to, valid)), nil
	}

	from := "orchestrator"
	if t.sender != nil {
		if s := t.sender(ctx); s != "" {
			from = s
		}
	}

	msg := &AgentMessage{From: from, To: to, Body: body}
	if err := t.store.Enqueue(msg); err != nil {
		return nil, fmt.Errorf("send_agent_message: %w", err)
	}
	return SendMessageResult{ID: msg.ID, To: msg.To, State: msg.State}, nil
}

// InboxTool drains the calling agent's unread messages and marks them
// read. The loop's turn-start injection is the primary delivery path;
// this tool lets an agent poll on demand.
type InboxTool struct {
	tools.ToolDefaults
	store      agentMessageStore
	owner      func(ctx context.Context) string
	bus        handoffBus
	source     string
	drainLimit int
}

// NewInboxTool creates the inbox tool. bus may be nil (no
// agent.message.delivered events published). source identifies the
// publisher on the bus.
func NewInboxTool(store agentMessageStore, owner func(ctx context.Context) string, bus handoffBus, source string) *InboxTool {
	return &InboxTool{store: store, owner: owner, bus: bus, source: source, drainLimit: 20}
}

func (t *InboxTool) Name() string { return "inbox" }

func (t *InboxTool) Category() string { return "platform" }

func (t *InboxTool) Description() string {
	return "Check your inbox: returns unread messages from other agents and marks them read."
}

func (t *InboxTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type:       schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{},
		Required:   []string{},
	}
}

// InboxResult is the drained inbox payload.
type InboxResult struct {
	Messages []*AgentMessage `json:"messages"`
	Count    int             `json:"count"`
}

func (t *InboxTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.store == nil {
		return tools.NewErrorResult("message store not available"), nil
	}
	me := ""
	if t.owner != nil {
		me = t.owner(ctx)
	}
	msgs, err := t.store.DrainInbox(me, t.drainLimit)
	if err != nil {
		return nil, fmt.Errorf("inbox: %w", err)
	}
	ids := make([]string, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID
		m.State = "read"
	}
	if err := t.store.MarkRead(ids); err != nil {
		return nil, fmt.Errorf("inbox: mark read: %w", err)
	}
	t.publishDelivered(ids)
	if msgs == nil {
		msgs = []*AgentMessage{}
	}
	return InboxResult{Messages: msgs, Count: len(msgs)}, nil
}

func (t *InboxTool) publishDelivered(ids []string) {
	if t.bus == nil || len(ids) == 0 {
		return
	}
	for _, mid := range ids {
		msg, err := models.NewBusMessage(models.MessageTypeEvent, t.source, map[string]string{
			"message_id": mid,
			"event":      "agent.message.delivered",
		})
		if err != nil {
			continue
		}
		t.bus.Publish("agent.message.delivered", msg)
	}
}
