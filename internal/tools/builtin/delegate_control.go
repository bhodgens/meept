// Package builtin — delegate_control.go implements the model-facing
// child-control surface for the C1 async delegation seam (dsh-convergence
// leaf docs/plans/20261102-dsh-convergence/01-delegation/03-child-control.md,
// contract C2): subagent_send_message / subagent_interrupt /
// subagent_list_children, keyed by the C1 per-session child registry and
// routed to the existing message-queue/steer + ACP cancel primitives.
//
// These tools are DECORATION over primitives that already exist
// (DelegateManager registry + Cancel, Dispatcher.SteerActiveAgent,
// *acp.Session.Send/Cancel). They do not reimplement steering. The only
// security-sensitive behavior beyond the primitives is the interrupt
// authorization rule (a child may only be interrupted by its direct parent
// session), which fails closed when the caller session cannot be resolved.
package builtin

import (
	"context"
	"errors"
	"fmt"

	"github.com/caimlas/meept/internal/acp"
	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/id"
)

// DelegateController is the controller bound behind the three delegate
// control tools. It holds the live C1 registry/manager plus the dispatcher
// (for steering conversation-bound children) and the ACP manager (for
// acp-mode children). It is deliberately plain-data and immutable after
// construction except for the caller-session resolver seam.
type DelegateController struct {
	mgr    *agent.DelegateManager
	dsp    *agent.Dispatcher
	acpM   *acp.Manager
	caller func(ctx context.Context) string
	// completionBus is the bus background workflow runs publish their
	// settlement on (DSH-8 delivery). Nil = no publish; the engine's channel
	// seam still delivers the run.
	completionBus workflowBus
}

// NewDelegateController constructs the controller. A nil manager, dispatcher
// or ACP manager is tolerated at construction; the individual send/list
// methods fail closed (with an error envelope) when the primitive they need
// was not supplied.
func NewDelegateController(mgr *agent.DelegateManager, dsp *agent.Dispatcher, acpM *acp.Manager) *DelegateController {
	return &DelegateController{mgr: mgr, dsp: dsp, acpM: acpM}
}

// SetCallerSessionResolver injects the mapping from a calling turn's context
// to its owning session id. It is used as the default parent session for
// subagent_list_children and to authorize subagent_interrupt. A nil resolver
// leaves the controller unable to resolve a caller — list then requires an
// explicit parent_session_id and interrupt denies (fails closed).
func (c *DelegateController) SetCallerSessionResolver(f func(ctx context.Context) string) {
	if f == nil {
		return
	}
	c.caller = f
}

// SetCompletionBus wires the bus background workflow runs (DSH-8 delivery)
// publish their settlement on. A typed-nil / nil bus is ignored (nil-safe per
// the setters rule).
func (c *DelegateController) SetCompletionBus(b workflowBus) {
	if b == nil {
		return
	}
	c.completionBus = b
}

// resolveCallerSession returns the calling turn's session id, or "" when no
// resolver is bound or it returns empty.
func (c *DelegateController) resolveCallerSession(ctx context.Context) string {
	if c.caller == nil {
		return ""
	}
	return c.caller(ctx)
}

// childView is the sanitized projection subagent_list_children returns for a
// child — never the internal ChildRecord (no descriptor dump, no seed).
type childView struct {
	ChildID string `json:"child_id"`
	AgentID string `json:"agent_id"`
	Mode    string `json:"mode"`
	State   string `json:"state"`
	Model   string `json:"model"`
}

// SendMessage delivers a message to a delegated child. A running child is
// steered (local/agent children via the dispatcher's queue; acp children via
// the recorded ACP session). A non-running child cannot be continued until
// the B6 resume seam lands — SendMessage surfaces a clear error instead of
// silently dropping. It returns a DELIVERY ACK (message_id), never the
// child's answer, per dsh semantics.
func (c *DelegateController) SendMessage(ctx context.Context, childID, message string) (any, error) {
	if c.mgr == nil {
		return tools.NewErrorResult("subagent_send_message: delegate manager not available"), nil
	}
	if message == "" {
		return tools.NewErrorResult("subagent_send_message: message is required"), nil
	}
	rec, ok := c.mgr.Lookup(childID)
	if !ok {
		return tools.NewErrorResult("subagent_send_message: child not found: " + childID), nil
	}

	switch rec.State {
	case agent.ChildStateRunning:
		if rec.Mode == "acp" {
			if c.acpM == nil {
				return tools.NewErrorResult("subagent_send_message: acp manager not available"), nil
			}
			sess, ok := c.mgr.Session(childID)
			if !ok || sess == nil {
				return tools.NewErrorResult("subagent_send_message: acp child has no live session: " + childID), nil
			}
			if _, err := sess.Send(ctx, message); err != nil {
				return tools.NewErrorResult(fmt.Sprintf("subagent_send_message: acp send failed: %v", err)), nil
			}
		} else {
			if c.dsp == nil {
				return tools.NewErrorResult("subagent_send_message: dispatcher not available"), nil
			}
			// MessageQueue.Steer is single-slot-replace by contract: a second
			// steer to the same child while it is still processing replaces the
			// pending slot, it never appends. That is intended.
			if err := c.dsp.SteerActiveAgent(ctx, rec.ConversationID, message, "subagent_send_message"); err != nil {
				if errors.Is(err, agent.ErrQueueNotFound) {
					return tools.NewErrorResult(
						fmt.Sprintf("subagent_send_message: child %s has no active turn to steer right now", childID)), nil
				}
				return tools.NewErrorResult(fmt.Sprintf("subagent_send_message: steer failed: %v", err)), nil
			}
		}
	default:
		// idle / pending / interrupted / completed / failed. The B6 resume
		// seam (leaf 06) is not present yet; continue via a fresh spawn rather
		// than silently dropping the message.
		return tools.NewErrorResult(fmt.Sprintf(
			"subagent_send_message: child %s is %s (not active); continue it with a fresh delegate_task spawn", childID, rec.State)), nil
	}

	return map[string]any{"message_id": id.Generate("delegate-msg-")}, nil
}

// Interrupt cancels a descendant child of the caller, fire-and-return: it
// marks the child interrupted (via the manager's Cancel) and returns
// accepted:true without waiting for the child to settle. Authorization is
// enforced: only a child's direct parent session may interrupt it; an
// unresolvable caller is denied (fail closed). An already-settled child is
// left untouched.
func (c *DelegateController) Interrupt(ctx context.Context, childID string) (any, error) {
	if c.mgr == nil {
		return tools.NewErrorResult("subagent_interrupt: delegate manager not available"), nil
	}
	rec, ok := c.mgr.Lookup(childID)
	if !ok {
		return tools.NewErrorResult("subagent_interrupt: child not found: " + childID), nil
	}

	caller := c.resolveCallerSession(ctx)
	if caller == "" {
		return tools.NewErrorResult("subagent_interrupt: caller session could not be resolved; interrupt denied"), nil
	}
	if rec.ParentSessionID != caller {
		return tools.NewErrorResult(
			"subagent_interrupt: unauthorized: only the child's parent session may interrupt it"), nil
	}

	if rec.State != agent.ChildStateRunning && rec.State != agent.ChildStatePending {
		return tools.NewErrorResult(fmt.Sprintf(
			"subagent_interrupt: child %s is already %s", childID, rec.State)), nil
	}

	// Cancel marks the record Interrupted, closes settlement, cancels the
	// per-child run context, the live ACP session and the external cancel hook
	// — all best-effort. Fire-and-return: surface accepted regardless of the
	// external hook's result.
	_ = c.mgr.Cancel(childID)
	return map[string]any{"accepted": true}, nil
}

// ListChildren enumerates a parent's delegated children from the C1 child
// registry, returning a sanitized projection (child_id, agent_id, mode,
// state, model) — never an internal record or secret material. The parent
// defaults to the caller's session when parentSessionID is empty.
func (c *DelegateController) ListChildren(ctx context.Context, parentSessionID string) (any, error) {
	if c.mgr == nil {
		return tools.NewErrorResult("subagent_list_children: delegate manager not available"), nil
	}
	parent := parentSessionID
	if parent == "" {
		parent = c.resolveCallerSession(ctx)
	}
	if parent == "" {
		return tools.NewErrorResult(
			"subagent_list_children: no parent session; pass parent_session_id or run within a session context"), nil
	}

	views := []childView{}
	for _, rec := range c.mgr.Children(parent) {
		views = append(views, childView{
			ChildID: rec.ChildID,
			AgentID: rec.AgentID,
			Mode:    rec.Mode,
			State:   string(rec.State),
			Model:   rec.Model,
		})
	}
	return map[string]any{"children": views}, nil
}

// ---------------------------------------------------------------------------
// subagent_send_message
// ---------------------------------------------------------------------------

// SubagentSendMessageTool delivers a steering message to a delegated child.
type SubagentSendMessageTool struct {
	tools.ToolDefaults
	ctrl *DelegateController
}

// NewSubagentSendMessageTool constructs the subagent_send_message tool bound
// to the controller.
func NewSubagentSendMessageTool(c *DelegateController) *SubagentSendMessageTool {
	return &SubagentSendMessageTool{ctrl: c}
}

func (t *SubagentSendMessageTool) Name() string     { return "subagent_send_message" }
func (t *SubagentSendMessageTool) Category() string { return "delegation" }

func (t *SubagentSendMessageTool) Description() string {
	return "Deliver a message to a delegated child subagent (from subagent_list_children or delegate_task). " +
		"A running child receives it at its next step. Returns a delivery acknowledgment (message_id), not the child's answer."
}

func (t *SubagentSendMessageTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"child_id": {
				Type:        schemaTypeString,
				Description: "The id of the delegated child to message.",
			},
			"message": {
				Type:        schemaTypeString,
				Description: "The steering message to deliver; a running child consumes it at its next step.",
			},
		},
		Required: []string{"child_id", "message"},
	}
}

func (t *SubagentSendMessageTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.ctrl == nil {
		return tools.NewErrorResult("subagent_send_message: controller not bound"), nil
	}
	return t.ctrl.SendMessage(ctx, asString(args["child_id"]), asString(args["message"]))
}

// ---------------------------------------------------------------------------
// subagent_interrupt
// ---------------------------------------------------------------------------

// SubagentInterruptTool cancels a delegated child, fire-and-return.
type SubagentInterruptTool struct {
	tools.ToolDefaults
	ctrl *DelegateController
}

// NewSubagentInterruptTool constructs the subagent_interrupt tool bound to
// the controller.
func NewSubagentInterruptTool(c *DelegateController) *SubagentInterruptTool {
	return &SubagentInterruptTool{ctrl: c}
}

func (t *SubagentInterruptTool) Name() string     { return "subagent_interrupt" }
func (t *SubagentInterruptTool) Category() string { return "delegation" }

func (t *SubagentInterruptTool) Description() string {
	return "Interrupt (cancel) a delegated child subagent spawned from this same session. " +
		"Returns accepted:true immediately; the child transitions to interrupted."
}

func (t *SubagentInterruptTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"child_id": {
				Type:        schemaTypeString,
				Description: "The id of the delegated child to interrupt.",
			},
		},
		Required: []string{"child_id"},
	}
}

func (t *SubagentInterruptTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.ctrl == nil {
		return tools.NewErrorResult("subagent_interrupt: controller not bound"), nil
	}
	return t.ctrl.Interrupt(ctx, asString(args["child_id"]))
}

// ---------------------------------------------------------------------------
// subagent_list_children
// ---------------------------------------------------------------------------

// SubagentListChildrenTool enumerates a parent's delegated children.
type SubagentListChildrenTool struct {
	tools.ToolDefaults
	ctrl *DelegateController
}

// NewSubagentListChildrenTool constructs the subagent_list_children tool
// bound to the controller.
func NewSubagentListChildrenTool(c *DelegateController) *SubagentListChildrenTool {
	return &SubagentListChildrenTool{ctrl: c}
}

func (t *SubagentListChildrenTool) Name() string     { return "subagent_list_children" }
func (t *SubagentListChildrenTool) Category() string { return "delegation" }

func (t *SubagentListChildrenTool) Description() string {
	return "List the delegated child subagents of the current session (child_id, agent_id, mode, state, model). " +
		"Scope to another parent with parent_session_id."
}

func (t *SubagentListChildrenTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"parent_session_id": {
				Type:        schemaTypeString,
				Description: "Optional session id whose delegated children to list; defaults to the caller's session.",
			},
		},
		Required: []string{},
	}
}

func (t *SubagentListChildrenTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.ctrl == nil {
		return tools.NewErrorResult("subagent_list_children: controller not bound"), nil
	}
	return t.ctrl.ListChildren(ctx, asString(args["parent_session_id"]))
}

// Compile-time interface assertions.
var (
	_ tools.Tool = (*SubagentSendMessageTool)(nil)
	_ tools.Tool = (*SubagentInterruptTool)(nil)
	_ tools.Tool = (*SubagentListChildrenTool)(nil)
)
