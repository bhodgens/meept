package voice

// Employee phone binding (leaf 12): one meept session per (number,
// employee), the day brief injected once per call, per-call turn running,
// transcript persistence, and progress events. internal/voice stays
// dependency-free (it must never import internal/agent, internal/session,
// or internal/employee), so the binding works over three narrow interfaces
// the daemon wiring adapts (internal/daemon/voice_employee.go).
//
// SEAM CHOICES (documented per the leaf):
//   - Session lookup: lookup-by-NAME via the session store's List() query
//     using the stable name "voice:<employee>:<number>" (the leaf's
//     preferred option; no new session column). The daemon adapter sets
//     the name at create time via UpdateName and binds employee_id via the
//     existing Store.SetEmployeeID.
//   - Brief injection: per-call prompt PREFIX on the FIRST turn only,
//     through RunOnce's userMessage argument. The alternatives were
//     rejected against GetOrCreateWired semantics: hook-registry
//     injection does not work because ConfigSnapshot shares the template
//     hookRegistry across clones (and RunSessionStart transforms are
//     discarded by the loop's blank-identifier handling of transform
//     messages), and SetContextInjector
//     mutates the shared/cached loop. A first-turn prefix injects exactly
//     once per call, mutates NO shared loop state, survives concurrent
//     sessions by construction, and persists the brief into the session
//     message history (the on-call record of what the employee knew).
//   - Goal hold: NOT here. The binding is per-call mechanics; the
//     goal-round hold/release lives in internal/daemon/voice_employee.go,
//     which owns the employee-manager reference internal/voice may not
//     take.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Progress event kinds for voice.call.progress.
const (
	ProgressKindPrompt    = "prompt"
	ProgressKindSay       = "say"
	ProgressKindInterrupt = "interrupt"
)

// CallDirection values for the call store rows.
const (
	DirectionIncoming = "incoming"
	DirectionOutgoing = "outgoing"
)

// SessionName is the stable per-(employee, number) session NAME used for
// lookup-by-name. It is a session identity key, never an id: session_id
// and conversation_id remain the store's identifiers (the review
// checklist's resolve-BOTH-ids rule is honored by carrying both in
// SessionHandle).
func SessionName(employeeID, number string) string {
	return "voice:" + employeeID + ":" + number
}

// SessionHandle carries BOTH identity keys of the bound meept session
// (session_id and conversation_id are distinct; new code handles both).
type SessionHandle struct {
	ID             string
	ConversationID string
}

// SessionStore is the narrow session-store seam (lookup-by-name, create,
// rename, employee bind). The daemon wiring adapts session.Store onto it
// (internal/daemon/voice_employee.go sessionStoreAdapter); internal/voice
// never imports internal/session. The List projection is the lookup-by-
// name input: sessions carry BOTH identity keys.
type SessionStore interface {
	Create(name string) (SessionHandle, error)
	List() ([]NamedSession, error)
	UpdateName(sessionID, name string) error
	SetEmployeeID(sessionID, employeeID string) error
}

// NamedSession is the projection of a stored session the name lookup
// needs. It carries both identity keys.
type NamedSession struct {
	ID             string
	Name           string
	ConversationID string
	EmployeeID     string
}

// TurnRunner executes one employee turn on a resolved session. The daemon
// adapter wraps agent.Manager.GetOrCreateWired + loop.RunOnce with
// session persistence (the ChatHandler.persistExchange seam: RunOnce does
// NOT persist by itself). internal/voice never imports internal/agent.
type TurnRunner interface {
	Run(ctx context.Context, sess SessionHandle, prompt string) (reply string, err error)
}

// ProgressEvent is the typed payload of the voice.call.progress bus topic
// (one stable shape -> typed Topic[T], WS class "event", never
// chat_message).
type ProgressEvent struct {
	CallID     string    `json:"call_id"`
	EmployeeID string    `json:"employee_id"`
	Kind       string    `json:"kind"` // prompt | say | interrupt
	At         time.Time `json:"at"`
}

// ProgressPublisher is the bus seam; the daemon adapter publishes through
// bus.PublishT on the typed voice.call.progress topic.
type ProgressPublisher interface {
	PublishProgress(ev ProgressEvent)
}

// BindingDeps wires the binding. Stores/publisher may be nil only when
// the corresponding call path is unreachable; a nil transcript store is
// an error at NewBinding (every call must record).
type BindingDeps struct {
	Calls    CallStoreT
	Sessions SessionStore
	Turns    TurnRunner
	Progress ProgressPublisher
	Logger   *slog.Logger
}

// CallStoreT is the call-record seam (the concrete *CallStore satisfies
// it; tests may fake it). Named to avoid a self-referential type alias.
type CallStoreT interface {
	Start(ctx context.Context, rec CallRecord) error
	End(ctx context.Context, callID string, outcome string) error
	AppendTranscript(ctx context.Context, callID string, line TranscriptLine) error
	TodayTranscripts(ctx context.Context, number string) ([]CallTranscript, error)
	SessionIDFor(number, employeeID string) (string, bool)
}

// Binding runs one call: resolve-or-create the session, inject the day
// brief once, run turns, persist the transcript, publish progress. All
// mutable state is per-call; the Binding itself is lock-free shared.
type Binding struct {
	deps   BindingDeps
	logger *slog.Logger

	mu    sync.Mutex
	calls map[string]*callState // call_control_id -> per-call state
}

// callState is one live call's binding state.
type callState struct {
	callID     string
	number     string
	employeeID string
	session    SessionHandle
	brief      string // rendered PromptBlock; injected on the first turn only
	briefUsed  bool
}

// NewBinding constructs the binding.
func NewBinding(deps BindingDeps) (*Binding, error) {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if deps.Calls == nil || isNilTyped(deps.Calls) {
		return nil, errors.New("voice binding: call store is required")
	}
	if deps.Sessions == nil || isNilTyped(deps.Sessions) {
		return nil, errors.New("voice binding: session store is required")
	}
	if deps.Turns == nil || isNilTyped(deps.Turns) {
		return nil, errors.New("voice binding: turn runner is required")
	}
	return &Binding{
		deps:   deps,
		logger: deps.Logger,
		calls:  make(map[string]*callState),
	}, nil
}

// isNilTyped guards typed-nil pointers stored in interface fields (repo
// typed-nil rule).
func isNilTyped(v any) bool {
	switch t := v.(type) {
	case *CallStore:
		return t == nil
	case nil:
		return true
	default:
		return false
	}
}

// OnCallStart resolves or creates the ONE meept session per
// (number, employee), binds employee_id on it, records the call row, and
// registers per-call state. The brief itself is injected lazily on the
// first RunTurn (the caller supplies it once here).
func (b *Binding) OnCallStart(ctx context.Context, ccid, number, employeeID, direction string, brief DayBrief) (string, error) {
	if ccid == "" {
		return "", errors.New("voice binding: call control id is required")
	}
	sess, err := b.resolveSession(employeeID, number)
	if err != nil {
		return "", err
	}
	if err := b.deps.Calls.Start(ctx, CallRecord{
		CallID:     ccid,
		Number:     number,
		EmployeeID: employeeID,
		Direction:  direction,
		SessionID:  sess.ID,
	}); err != nil {
		return "", err
	}

	b.mu.Lock()
	b.calls[ccid] = &callState{
		callID:     ccid,
		number:     number,
		employeeID: employeeID,
		session:    sess,
		brief:      brief.PromptBlock(),
	}
	b.mu.Unlock()

	b.logger.Info("voice call bound to session",
		"ccid", ccid, "number", number, "employee_id", employeeID,
		"session_id", sess.ID, "conversation_id", sess.ConversationID)
	return sess.ID, nil
}

// RunTurn runs the caller's utterance as one employee turn on the bound
// session: the first turn carries the day brief as a prompt prefix
// (injected exactly once per call), the reply and both sides are appended
// to the call transcript, the exchange is persisted to the session, and
// progress events publish per final prompt (kind "prompt"). Allowed-say /
// interrupt progress events are published by the talk loop via
// PublishSay / PublishInterrupt.
func (b *Binding) RunTurn(ctx context.Context, ccid, text string) (string, error) {
	b.mu.Lock()
	st, ok := b.calls[ccid]
	if !ok {
		b.mu.Unlock()
		return "", fmt.Errorf("voice binding: unknown call %q", ccid)
	}
	prompt := text
	if !st.briefUsed && st.brief != "" {
		prompt = st.brief + "\n\n---\n\n" + text
		st.briefUsed = true
	}
	b.mu.Unlock()

	ev := ProgressEvent{
		CallID:     ccid,
		EmployeeID: st.employeeID,
		Kind:       ProgressKindPrompt,
		At:         time.Now().UTC(),
	}
	b.deps.Progress.PublishProgress(ev)

	reply, err := b.deps.Turns.Run(ctx, st.session, prompt)
	if err != nil {
		return "", fmt.Errorf("voice binding: turn for call %s: %w", ccid, err)
	}

	now := time.Now().UTC()
	appendErr := b.deps.Calls.AppendTranscript(ctx, ccid, TranscriptLine{Role: "user", Text: text, At: now})
	if reply != "" {
		if aErr := b.deps.Calls.AppendTranscript(ctx, ccid, TranscriptLine{Role: "assistant", Text: reply, At: now}); aErr != nil && appendErr == nil {
			appendErr = aErr
		}
	}
	if appendErr != nil {
		// The turn succeeded; transcript loss is logged, never fatal.
		b.logger.Error("voice binding: transcript append", "ccid", ccid, "error", appendErr)
	}
	return reply, nil
}

// PublishSay publishes one allowed-say progress event (talk-loop path).
// Unknown call ids are dropped with a log line.
func (b *Binding) PublishSay(ccid string) {
	b.publishKind(ccid, ProgressKindSay)
}

// PublishInterrupt publishes one caller-interrupt progress event.
func (b *Binding) PublishInterrupt(ccid string) {
	b.publishKind(ccid, ProgressKindInterrupt)
}

func (b *Binding) publishKind(ccid, kind string) {
	st := b.state(ccid)
	if st == nil {
		b.logger.Warn("voice binding: progress for unknown call", "ccid", ccid, "kind", kind)
		return
	}
	b.deps.Progress.PublishProgress(ProgressEvent{
		CallID:     ccid,
		EmployeeID: st.employeeID,
		Kind:       kind,
		At:         time.Now().UTC(),
	})
}

// OnCallEnd flushes the terminal outcome and drops the per-call state.
// Unknown call ids are a no-op (idempotent, matching the manager).
func (b *Binding) OnCallEnd(ctx context.Context, ccid, outcome string) error {
	b.mu.Lock()
	_, live := b.calls[ccid]
	delete(b.calls, ccid)
	b.mu.Unlock()
	if !live {
		return nil
	}
	return b.deps.Calls.End(ctx, ccid, outcome)
}

// state returns the per-call state snapshot under the lock.
func (b *Binding) state(ccid string) *callState {
	b.mu.Lock()
	defer b.mu.Unlock()
	st, ok := b.calls[ccid]
	if !ok {
		return nil
	}
	cp := *st // copy: briefUsed is mutated only here, under this lock
	return &cp
}

// resolveSession creates or reuses the session for (number, employee):
// lookup by stable name first, then create + rename + employee-bind.
func (b *Binding) resolveSession(employeeID, number string) (SessionHandle, error) {
	name := SessionName(employeeID, number)
	sessions, err := b.deps.Sessions.List()
	if err != nil {
		return SessionHandle{}, fmt.Errorf("voice binding: list sessions: %w", err)
	}
	for _, s := range sessions {
		if s.Name == name {
			return SessionHandle{ID: s.ID, ConversationID: s.ConversationID}, nil
		}
	}

	sess, err := b.deps.Sessions.Create("voice call")
	if err != nil {
		return SessionHandle{}, fmt.Errorf("voice binding: create session: %w", err)
	}
	if err := b.deps.Sessions.UpdateName(sess.ID, name); err != nil {
		return SessionHandle{}, fmt.Errorf("voice binding: name session %s: %w", sess.ID, err)
	}
	if err := b.deps.Sessions.SetEmployeeID(sess.ID, employeeID); err != nil {
		return SessionHandle{}, fmt.Errorf("voice binding: bind employee on %s: %w", sess.ID, err)
	}
	return sess, nil
}

// BuildDayBrief assembles a call's DayBrief via leaf-04 Build with the
// given sources; the store's TodayTranscripts supplies prior calls. Used
// by the daemon wiring per call, before OnCallStart.
func BuildDayBrief(ctx context.Context, deps Deps, employeeID, number string, now time.Time, loc *time.Location) (DayBrief, error) {
	return Build(ctx, deps, employeeID, number, now, loc)
}
