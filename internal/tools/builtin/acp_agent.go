package builtin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/acp"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

const (
	acpToolName         = "acp_agent"
	acpVerbLaunch       = "launch"
	acpVerbSend         = "send"
	acpVerbRead         = "read"
	acpVerbStop         = "stop"
	errACPDisabled      = "acp disabled"
	errACPAgentReq      = "agent is required"
	errACPVerb          = "verb must be launch, send, read, or stop"
	errACPMessageReq    = "message is required"
	errACPAgentNotFound = "agent not found: %s"
	errACPAgentDisabled = "agent disabled: %s"
	errACPMaxAgents     = "max agents: %s"
)

// acpSession is the session seam used by ACPAgentTool. *acp.Session
// implements it; tests inject a fake.
type acpSession interface {
	State() acp.SessionState
	Send(context.Context, string) (string, error)
	Events() <-chan acp.SessionEvent
}

// acpMgr is the manager seam used by ACPAgentTool. Production wraps
// *acp.Manager; tests inject a fake so they never need unexported startFn.
type acpMgr interface {
	Enabled() bool
	GetOrCreate(ctx context.Context, agentID, workdir string) (acpSession, error)
	Stop(agentID string) error
}

type acpManagerAdapter struct {
	m *acp.Manager
}

func (a acpManagerAdapter) Enabled() bool {
	return a.m != nil && a.m.Enabled()
}

func (a acpManagerAdapter) GetOrCreate(
	ctx context.Context,
	agentID string,
	workdir string,
) (acpSession, error) {
	if a.m == nil {
		return nil, acp.ErrDisabled
	}
	s, err := a.m.GetOrCreate(ctx, agentID, workdir)
	if err != nil {
		return nil, err
	}
	if s == nil {
		return nil, nil
	}
	return s, nil
}

func (a acpManagerAdapter) Stop(agentID string) error {
	if a.m == nil {
		return nil
	}
	return a.m.Stop(agentID)
}

func adaptACPManager(m *acp.Manager) acpMgr {
	if m == nil {
		return nil
	}
	return acpManagerAdapter{m: m}
}

// ACPAgentTool is the single meta-tool models see for ACP agent control.
// Verbs: launch, send, read, stop.
type ACPAgentTool struct {
	tools.ToolDefaults
	mgr     acpMgr
	enabled bool
}

// NewACPAgentTool builds the acp_agent tool. A nil manager is safe; Execute
// returns "acp disabled" until SetManager is given a live manager.
func NewACPAgentTool(m *acp.Manager) *ACPAgentTool {
	return &ACPAgentTool{
		mgr:     adaptACPManager(m),
		enabled: true,
	}
}

// SetManager injects a production *acp.Manager. Nil receiver and nil manager
// are no-ops (repo setter rule).
func (t *ACPAgentTool) SetManager(m *acp.Manager) {
	if t == nil || m == nil {
		return
	}
	t.mgr = adaptACPManager(m)
}

// SetEnabled is the local kill switch. Nil receiver is a no-op.
func (t *ACPAgentTool) SetEnabled(v bool) {
	if t == nil {
		return
	}
	t.enabled = v
}

func (t *ACPAgentTool) Name() string { return acpToolName }

func (t *ACPAgentTool) Category() string { return "agent" }

func (t *ACPAgentTool) Description() string {
	return "Control an ACP coding agent. verb=launch warms a session; " +
		"verb=send delivers a message and returns the reply; " +
		"verb=read drains recent event chunks; verb=stop tears the session down. " +
		"The session parameter is accepted for future multi-session use and ignored in v1."
}

func (t *ACPAgentTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"agent": {
				Type:        schemaTypeString,
				Description: "ACP agent id from the agents catalog.",
			},
			"verb": {
				Type:        schemaTypeString,
				Description: "Operation to perform.",
				Enum:        []string{acpVerbLaunch, acpVerbSend, acpVerbRead, acpVerbStop},
			},
			"message": {
				Type:        schemaTypeString,
				Description: "Prompt text. Required when verb is send.",
			},
			"session": {
				Type:        schemaTypeString,
				Description: "Optional session id. Unused in v1 (one session per agent).",
			},
		},
		Required: []string{"agent", "verb"},
	}
}

func (t *ACPAgentTool) IsReadOnly(input map[string]any) bool {
	verb, _ := input["verb"].(string)
	return strings.EqualFold(strings.TrimSpace(verb), acpVerbRead)
}

func (t *ACPAgentTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	agent := strings.TrimSpace(acpArgString(args, "agent"))
	if agent == "" {
		return nil, errors.New(errACPAgentReq)
	}
	verb := strings.ToLower(strings.TrimSpace(acpArgString(args, "verb")))
	switch verb {
	case acpVerbLaunch, acpVerbSend, acpVerbRead, acpVerbStop:
	default:
		return nil, errors.New(errACPVerb)
	}
	if verb == acpVerbSend {
		if strings.TrimSpace(acpArgString(args, "message")) == "" {
			return nil, errors.New(errACPMessageReq)
		}
	}
	if t == nil || t.mgr == nil || !t.enabled || !t.mgr.Enabled() {
		return nil, errors.New(errACPDisabled)
	}

	start := time.Now()
	workdir := tools.WorkingDirFromContext(ctx)

	var (
		payload map[string]any
		err     error
	)
	switch verb {
	case acpVerbLaunch:
		payload, err = t.launch(ctx, agent, workdir)
	case acpVerbSend:
		payload, err = t.send(ctx, agent, workdir, acpArgString(args, "message"))
	case acpVerbRead:
		payload, err = t.read(ctx, agent, workdir)
	case acpVerbStop:
		payload, err = t.stop(agent)
	}
	if err != nil {
		return nil, translateACPError(err, agent)
	}
	payload["elapsed_ms"] = time.Since(start).Milliseconds()
	return tools.NewSuccessResult(payload), nil
}

func (t *ACPAgentTool) launch(
	ctx context.Context,
	agent string,
	workdir string,
) (map[string]any, error) {
	sess, err := t.mgr.GetOrCreate(ctx, agent, workdir)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"agent": agent,
		"state": sessionStateName(sess.State()),
	}, nil
}

func (t *ACPAgentTool) send(
	ctx context.Context,
	agent string,
	workdir string,
	message string,
) (map[string]any, error) {
	sess, err := t.mgr.GetOrCreate(ctx, agent, workdir)
	if err != nil {
		return nil, err
	}
	reply, err := sess.Send(ctx, message)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"agent": agent,
		"state": sessionStateName(sess.State()),
		"reply": reply,
	}, nil
}

func (t *ACPAgentTool) read(
	ctx context.Context,
	agent string,
	workdir string,
) (map[string]any, error) {
	sess, err := t.mgr.GetOrCreate(ctx, agent, workdir)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"agent":  agent,
		"state":  sessionStateName(sess.State()),
		"events": drainSessionEvents(sess.Events()),
	}, nil
}

func (t *ACPAgentTool) stop(agent string) (map[string]any, error) {
	if err := t.mgr.Stop(agent); err != nil {
		return nil, err
	}
	return map[string]any{
		"agent": agent,
		"state": "closed",
	}, nil
}

func acpArgString(args map[string]any, key string) string {
	s, _ := args[key].(string)
	return s
}

func sessionStateName(st acp.SessionState) string {
	switch st {
	case acp.StateStarting:
		return "starting"
	case acp.StateReady:
		return "ready"
	case acp.StateBusy:
		return "busy"
	case acp.StateClosed:
		return "closed"
	default:
		return "unknown"
	}
}

func drainSessionEvents(ch <-chan acp.SessionEvent) []map[string]string {
	out := []map[string]string{}
	if ch == nil {
		return out
	}
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, map[string]string{
				"kind": ev.Kind,
				"text": ev.Text,
			})
		default:
			return out
		}
	}
}

func translateACPError(err error, agentID string) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, acp.ErrDisabled):
		return errors.New(errACPDisabled)
	case errors.Is(err, acp.ErrAgentNotFound):
		return fmt.Errorf(errACPAgentNotFound, agentID)
	case errors.Is(err, acp.ErrAgentDisabled):
		return fmt.Errorf(errACPAgentDisabled, agentID)
	case errors.Is(err, acp.ErrMaxAgents):
		return fmt.Errorf(errACPMaxAgents, agentID)
	default:
		return fmt.Errorf("acp_agent: %w", err)
	}
}

var (
	_ tools.Tool = (*ACPAgentTool)(nil)
	_ acpSession = (*acp.Session)(nil)
	_ acpMgr     = acpManagerAdapter{}
)
