package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/acp"
	"github.com/caimlas/meept/pkg/models"
)

// ExecutorACP is the AgentSpec.Executor value that routes turns through
// the external ACP harness instead of the local LLM reasoning loop
// (docs/workflows/acp-employees.md, dim 3A).
const ExecutorACP = "acp"

// ErrACPExecutorNotWired is the turn error for an executor: acp agent
// whose loop never received an ACPExecutor ([acp] enabled kill switch off
// or wiring skipped). The branch fails loudly with this sentinel; it never
// falls back to the local LLM loop.
var ErrACPExecutorNotWired = errors.New("acp executor not wired")

// ErrNoACPSpecBinding is returned by ACPExecutor.Run when the spec carries
// no acp: block (executor: acp without a harness binding).
var ErrNoACPSpecBinding = errors.New("acp executor: agent spec carries no acp binding")

// ErrNoACPSession is returned when the manager resolved no live session
// (its documented nil-session/nil-error "not started" signal).
var ErrNoACPSession = errors.New("acp executor: manager started no session")

// ACPSession is the session surface ACPExecutor drives. *acp.Session
// satisfies it; tests inject a fake.
type ACPSession interface {
	Send(ctx context.Context, text string) (string, error)
	Events() <-chan acp.SessionEvent
}

// ACPSessionProvider is the narrow acp-manager surface ACPExecutor needs.
// *acp.Manager satisfies it through acpManagerProvider (the concrete
// manager returns *acp.Session, so a thin adapter bridges the return
// type); tests inject a fake provider and never spawn a subprocess.
type ACPSessionProvider interface {
	GetOrCreatePersona(ctx context.Context, agentID, persona, workdir string, opts acp.PersonaOpts) (ACPSession, error)
}

// acpManagerProvider adapts *acp.Manager to ACPSessionProvider.
type acpManagerProvider struct {
	m *acp.Manager
}

func (p acpManagerProvider) GetOrCreatePersona(
	ctx context.Context,
	agentID, persona, workdir string,
	opts acp.PersonaOpts,
) (ACPSession, error) {
	if p.m == nil {
		return nil, acp.ErrDisabled
	}
	s, err := p.m.GetOrCreatePersona(ctx, agentID, persona, workdir, opts)
	if err != nil {
		return nil, err
	}
	if s == nil {
		// Documented manager contract: a nil session with a nil error
		// means the start waiter resolved without a session.
		return nil, ErrNoACPSession
	}
	return s, nil
}

// compile-time proof that the real session type satisfies the seam.
var _ ACPSession = (*acp.Session)(nil)

// ACPExecutor drives one agent turn through an external ACP harness
// (executor: acp agent definitions, dim 3A). It shares the daemon's
// *acp.Manager with the acp_agent tool, so a persona session is reused
// across both paths.
type ACPExecutor struct {
	provider ACPSessionProvider
}

// NewACPExecutor builds the executor on the daemon's acp manager. A nil
// manager ([acp] disabled) returns nil, and the injection setters treat
// nil as a no-op: the loop branch then fails loudly for any agent that
// still declares executor: acp.
func NewACPExecutor(m *acp.Manager) *ACPExecutor {
	if m == nil {
		return nil
	}
	return &ACPExecutor{provider: acpManagerProvider{m: m}}
}

// newACPExecutorFromProvider builds an executor on an injected provider
// (tests). A nil provider returns nil.
func newACPExecutorFromProvider(p ACPSessionProvider) *ACPExecutor {
	if p == nil {
		return nil
	}
	return &ACPExecutor{provider: p}
}

// Run executes one turn on the external harness: GetOrCreatePersona owns
// the session/new lane (once per persona session; live sessions are
// reused), Send carries the prompt text through session/prompt, streamed
// session/update notifications flow to emit, and the final agent message
// text is returned to the caller's shared post-processing pipeline.
func (e *ACPExecutor) Run(
	ctx context.Context,
	spec *AgentSpec,
	workdir string,
	promptText string,
	emit func(updateText string),
) (string, error) {
	if e == nil || e.provider == nil {
		return "", ErrACPExecutorNotWired
	}
	if spec == nil || spec.ACP == nil {
		return "", ErrNoACPSpecBinding
	}
	sess, err := e.provider.GetOrCreatePersona(ctx, spec.ACP.Agent, spec.ACP.Persona, workdir, acp.PersonaOpts{
		CommandArgs:       spec.ACP.CommandArgs,
		PermissionMode:    spec.ACP.PermissionMode,
		DecisionThreshold: spec.ACP.DecisionThreshold,
	})
	if err != nil {
		return "", fmt.Errorf("acp executor: %w", err)
	}
	if sess == nil {
		return "", ErrNoACPSession
	}

	// Stream session/update notifications while the prompt runs. The
	// drain goroutine stops when Send returns; the events channel stays
	// open for the session's lifetime (shared with the acp_agent tool's
	// read verb), so it is never closed here and trailing events after
	// the final text remain buffered for later consumers.
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		events := sess.Events()
		for {
			select {
			case <-done:
				// send finished: drain whatever is already buffered
				// so late updates are not dropped, then exit.
				for {
					select {
					case ev, ok := <-events:
						if !ok {
							return
						}
						emitACPUpdate(emit, ev)
					default:
						return
					}
				}
			case ev, ok := <-events:
				if !ok {
					return
				}
				emitACPUpdate(emit, ev)
			}
		}
	}()
	finalText, err := sess.Send(ctx, promptText)
	close(done)
	wg.Wait()
	if err != nil {
		return "", fmt.Errorf("acp executor: %w", err)
	}
	return finalText, nil
}

// emitACPUpdate forwards one session event to the emit callback. "done"
// duplicates the final text Send returns; empty-text events (e.g. the
// "closed" marker) carry nothing to render. Both are skipped.
func emitACPUpdate(emit func(string), ev acp.SessionEvent) {
	if emit == nil || ev.Kind == "done" || ev.Text == "" {
		return
	}
	emit(ev.Text)
}

// WithACPExecutor injects the external ACP turn producer at loop
// construction. Nil is a no-op.
func WithACPExecutor(e *ACPExecutor) LoopOption {
	return func(l *AgentLoop) {
		if e != nil {
			l.acpExecutor = e
		}
	}
}

// SetACPExecutor injects the external ACP turn producer after
// construction (daemon wiring). Nil receiver and nil executor are no-ops
// (repo setter rule).
func (l *AgentLoop) SetACPExecutor(e *ACPExecutor) {
	if l == nil || e == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.acpExecutor = e
}

// ACPExecutor returns the injected external turn producer (nil = none).
func (l *AgentLoop) ACPExecutor() *ACPExecutor {
	if l == nil {
		return nil
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.acpExecutor
}

// acpTurnDecision resolves the executor branch (dim 3A) for one turn. The
// loop's own spec wins; registry-built loops are constructed without a
// spec option, so the shared AgentRegistry resolves it by agent id.
// isACPTurn reports whether the turn must route through the external
// harness (spec.Executor == "acp"); acpExec is the injected executor (nil
// = not wired).
func (l *AgentLoop) acpTurnDecision() (spec *AgentSpec, acpExec *ACPExecutor, isACPTurn bool) {
	l.mu.RLock()
	spec = l.spec
	acpExec = l.acpExecutor
	l.mu.RUnlock()
	if spec == nil && l.agentRegistry != nil {
		if s, ok := l.agentRegistry.GetSpec(l.agentID); ok {
			spec = s
		}
	}
	if spec == nil || spec.Executor != ExecutorACP {
		return spec, acpExec, false
	}
	return spec, acpExec, true
}

// runTurnProducer is the single turn-entry seam RunOnceWithParts and its
// reply-guard rewrite retry call. It resolves the executor branch (dim 3A):
// an executor: acp spec routes through the injected ACPExecutor; every
// other spec keeps the local LLM reasoning cycle. promptText is only used
// by the acp lane (the LLM lane reads the conversation).
func (l *AgentLoop) runTurnProducer(
	ctx context.Context,
	conv *Conversation,
	conversationID string,
	promptText string,
) (string, error) {
	spec, acpExec, isACPTurn := l.acpTurnDecision()
	if !isACPTurn {
		return l.reasoningCycle(ctx, conv, conversationID)
	}
	if acpExec == nil {
		// fail loudly: an executor: acp agent never silently falls back
		// to the local LLM loop. the message ships as the turn result
		// text (RunOnceWithParts' error path surfaces a non-empty
		// response verbatim) alongside the wrapped sentinel error.
		msg := fmt.Sprintf("acp executor not wired: agent %s declares executor: acp but no executor was injected ([acp] enabled kill switch off or wiring missing)", l.agentID)
		return msg, fmt.Errorf("%s: %w", msg, ErrACPExecutorNotWired)
	}
	return l.runACPTurn(ctx, spec, acpExec, conversationID, promptText)
}

// runACPTurn drives one turn through the external ACP harness. The prompt
// text goes through session/prompt (GetOrCreatePersona owns session/new),
// streamed updates publish onto the agent.progress topic tagged with the
// agent id + persona, and the final text returns to RunOnceWithParts'
// shared post-processing (reply guard, security scan, history append) --
// the same pipeline the LLM reply gets; no second reply path.
func (l *AgentLoop) runACPTurn(
	ctx context.Context,
	spec *AgentSpec,
	acpExec *ACPExecutor,
	conversationID string,
	promptText string,
) (string, error) {
	if acpExec == nil {
		return "", ErrACPExecutorNotWired
	}
	workdir := l.GetWorkingDir()
	emit := func(updateText string) {
		l.publishACPUpdate(conversationID, spec, updateText)
	}
	return acpExec.Run(ctx, spec, workdir, promptText, emit)
}

// publishACPUpdate forwards one streamed harness update to the bus as an
// agent.progress event tagged with the agent id and persona (design step
// 3: session/update notifications ride the existing progress topics).
func (l *AgentLoop) publishACPUpdate(conversationID string, spec *AgentSpec, updateText string) {
	if !l.progressEnabled || l.bus == nil {
		return
	}
	payload := map[string]any{
		"conversation_id": conversationID,
		"stage":           "acp",
		"detail":          updateText,
		"agent_id":        l.agentID,
		"timestamp":       time.Now().UTC().Format(time.RFC3339),
	}
	if spec != nil && spec.ACP != nil {
		payload["acp_agent"] = spec.ACP.Agent
		payload["persona"] = spec.ACP.Persona
	}
	msg, err := models.NewBusMessage(models.MessageTypeEvent, "agent", payload)
	if err != nil {
		l.logger.Warn("failed to create acp progress bus message", "error", err)
		return
	}
	l.bus.Publish("agent.progress", msg)
}
