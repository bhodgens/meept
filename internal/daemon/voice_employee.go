package daemon

// Voice employee wiring (leaf 12, daemon side): glues the session store,
// the agent loop manager, the bus, and the goal-loop hold onto the
// dependency-free internal/voice binding. ExecuteBot is deliberately NOT
// used (it mints fresh bot-exec ids and drops context); turns run through
// AgentLoop.RunOnce on the resolved session's conversation id with the
// ChatHandler.persistExchange persistence seam.
//
// Goal hold (T5): the call IS the employee's goal round. Round entry
// acquires a per-employee serialization mutex (the same pattern as
// employee.Manager.triggerViaGoalLoop's acquireInvokeMutex - that helper
// is unexported, so the pattern is mirrored here), held for the call's
// life and released exactly once by the Manager release hook from leaf 05
// on EVERY terminal outcome (hangup, no_answer, failed, answered+hangup).
// Scheduled assesses hitting the held employee block on the same mutex,
// so the goal loop's DecideSkip path (LastDecideSkip) stays the skip
// mechanism, not a queue; the existing paused-mid-call reflect skip in
// goal_loop.go (strings.EqualFold(statusFn, "paused")) is unchanged.

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/voice"
)

// TopicVoiceCallProgress is the typed voice.call.progress declaration.
// Publishers and subscribers reference the same var so the compiler
// enforces the payload; the payload carries a WSClass marker so the relay
// classifies it as a generic "event", never chat_message.
var TopicVoiceCallProgress = bus.NewTopic[voice.ProgressEvent]("voice.call.progress")

// WS classification: the voice.call.progress topic name is not in the
// relay's chat_message set, so its events classify as generic "event"
// (the wsclass default) - never chat_message (blank-bubble invariant). If
// the payload later needs an explicit marker, wrap ProgressEvent and
// implement wsclass.WSClassified on the wrapper.
// VoiceEmployeeWiring is the daemon-side construction returned by
// WireVoiceEmployee: the voice binding plus the goal-round hold seam the
// dial/spawn paths call.
type VoiceEmployeeWiring struct {
	Binding *voice.Binding

	store  *voice.CallStore
	logger *slog.Logger

	holdMu    sync.Mutex
	holds     map[string]*sync.Mutex // employee_id -> serialization mutex
	heldCalls map[string]holdEntry   // call_control_id -> hold routing entry

}

// VoiceEmployeeDeps carries the daemon components the wiring needs. All
// pointer fields are typed-nil guarded at construction.
type VoiceEmployeeDeps struct {
	Sessions     session.Store    // the meept session store (binding + naming)
	LoopManager  *agent.Manager   // per-session loop source (GetOrCreateWired)
	TemplateLoop *agent.AgentLoop // the daemon template loop
	WorkingDir   string           // per-session working dir for cloned loops
	Bus          *bus.MessageBus
	Logger       *slog.Logger

	// CallStorePath is $MEEPT_HOME/voice/calls.db (config.MeeptPath).
	CallStorePath string
}

// sessionStoreAdapter adapts session.Store onto voice.SessionStore via
// lookup-by-NAME over List (documented seam choice; no new session
// column). The identity duality rule: the projection carries BOTH
// session_id and conversation_id.
type sessionStoreAdapter struct {
	store session.Store
}

// Create implements voice.SessionStore.
func (a sessionStoreAdapter) Create(name string) (voice.SessionHandle, error) {
	sess, err := a.store.Create(name)
	if err != nil {
		return voice.SessionHandle{}, err
	}
	if sess == nil {
		return voice.SessionHandle{}, fmt.Errorf("voice session adapter: create %q returned nil session", name)
	}
	return voice.SessionHandle{ID: sess.ID, ConversationID: sess.ConversationID}, nil
}

// List implements voice.SessionStore.
func (a sessionStoreAdapter) List() ([]voice.NamedSession, error) {
	sessions, err := a.store.List()
	if err != nil {
		return nil, err
	}
	out := make([]voice.NamedSession, 0, len(sessions))
	for _, s := range sessions {
		if s == nil {
			continue
		}
		out = append(out, voice.NamedSession{
			ID:             s.ID,
			Name:           s.Name,
			ConversationID: s.ConversationID,
			EmployeeID:     s.EmployeeID,
		})
	}
	return out, nil
}

// UpdateName implements voice.SessionStore.
func (a sessionStoreAdapter) UpdateName(sessionID, name string) error {
	return a.store.UpdateName(sessionID, name)
}

// SetEmployeeID implements voice.SessionStore.
func (a sessionStoreAdapter) SetEmployeeID(sessionID, employeeID string) error {
	return a.store.SetEmployeeID(sessionID, employeeID)
}

// loopTurnRunner adapts agent.Manager onto voice.TurnRunner: the loop is
// cloned per session via GetOrCreateWired (never the shared template),
// RunOnce runs on the session's conversation id, and the exchange is
// persisted to the session store (persistExchange seam) so the phone
// conversation history survives across calls.
type loopTurnRunner struct {
	mgr        *agent.Manager
	template   *agent.AgentLoop
	workingDir string
	sessions   session.Store
	logger     *slog.Logger
}

// Run implements voice.TurnRunner.
func (r *loopTurnRunner) Run(ctx context.Context, sess voice.SessionHandle, prompt string) (string, error) {
	loop, err := r.mgr.GetOrCreateWired(sess.ID, r.workingDir, r.template)
	if err != nil {
		return "", fmt.Errorf("voice turn runner: wired loop for %s: %w", sess.ID, err)
	}
	reply, err := loop.RunOnce(ctx, prompt, sess.ConversationID)
	if err != nil {
		return "", err
	}
	r.persistExchange(sess.ID, prompt, reply)
	return reply, nil
}

// persistExchange mirrors agent.ChatHandler.persistExchange: user message
// + assistant reply into session_messages, best-effort (logged, never
// fatal). Called AFTER RunOnce returns.
func (r *loopTurnRunner) persistExchange(sessID, userMsg, reply string) {
	if r.sessions == nil || sessID == "" {
		return
	}
	now := time.Now().UTC()
	msgs := []session.Message{{
		SessionID: sessID,
		Role:      "user",
		Content:   userMsg,
		Timestamp: now,
		EntryType: "message",
		BranchID:  "main",
	}}
	if reply != "" {
		msgs = append(msgs, session.Message{
			SessionID: sessID,
			Role:      "assistant",
			Content:   reply,
			Timestamp: now,
			EntryType: "message",
			BranchID:  "main",
		})
	}
	if err := r.sessions.SaveMessages(sessID, msgs); err != nil {
		r.logger.Warn("voice turn runner: persist exchange",
			"session_id", sessID, "error", err)
	}
}

// busProgressPublisher publishes through the typed topic. The wire
// payload is the plain ProgressEvent (JSON-stable); the class marker is
// delivered via the voiceProgressEvent wrapper only where the relay needs
// it. Publishing with a nil bus is a no-op.
func busProgressPublisher(b *bus.MessageBus, logger *slog.Logger) func(voice.ProgressEvent) {
	return func(ev voice.ProgressEvent) {
		if b == nil {
			return
		}
		bus.PublishT(b, TopicVoiceCallProgress, "voice", ev)
		logger.Debug("voice call progress", "call_id", ev.CallID, "kind", ev.Kind)
	}
}

// WireVoiceEmployee constructs the daemon-side voice employee stack: the
// SQLite call store, the binding, the typed progress publisher, and the
// goal-round hold. Returns typed-nil-safe wiring: a nil Bus is allowed
// (progress events drop), a nil LoopManager fails construction.
func WireVoiceEmployee(deps VoiceEmployeeDeps) (*VoiceEmployeeWiring, error) {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if deps.LoopManager == nil || deps.TemplateLoop == nil {
		return nil, fmt.Errorf("voice employee wiring: loop manager and template loop are required")
	}
	if deps.Sessions == nil {
		return nil, fmt.Errorf("voice employee wiring: session store is required")
	}
	if deps.CallStorePath == "" {
		return nil, fmt.Errorf("voice employee wiring: call store path is required")
	}

	store, err := voice.NewCallStore(deps.CallStorePath, logger.With("component", "voice-callstore"))
	if err != nil {
		return nil, err
	}

	progressFn := busProgressPublisher(deps.Bus, logger.With("component", "voice-progress"))
	binding, err := voice.NewBinding(voice.BindingDeps{
		Calls:    store,
		Sessions: sessionStoreAdapter{store: deps.Sessions},
		Turns: &loopTurnRunner{
			mgr:        deps.LoopManager,
			template:   deps.TemplateLoop,
			workingDir: deps.WorkingDir,
			sessions:   deps.Sessions,
			logger:     logger.With("component", "voice-turns"),
		},
		Progress: progressPublisherFunc(progressFn),
		Logger:   logger.With("component", "voice-binding"),
	})
	if err != nil {
		store.Close()
		return nil, err
	}

	w := &VoiceEmployeeWiring{
		Binding:   binding,
		store:     store,
		logger:    logger,
		holds:     make(map[string]*sync.Mutex),
		heldCalls: make(map[string]holdEntry),
	}
	return w, nil
}

// progressPublisherFunc adapts a func onto voice.ProgressPublisher.
type progressPublisherFunc func(ev voice.ProgressEvent)

// PublishProgress implements voice.ProgressPublisher.
func (f progressPublisherFunc) PublishProgress(ev voice.ProgressEvent) { f(ev) }

// AcquireCallHold is round entry: it takes the per-employee serialization
// mutex (the triggerViaGoalLoop pattern), registers the call as the
// holder, and returns the release func. It blocks while any prior call
// (or a scheduled assess round) is live for this employee. ALL releases
// - direct or through the leaf-05 hook - funnel through ReleaseForCall,
// whose map delete is the once-guard, so a hold is unlocked exactly once
// no matter which terminal path fires first.
func (w *VoiceEmployeeWiring) AcquireCallHold(ccid, employeeID string) func() {
	w.holdMu.Lock()
	mu, ok := w.holds[employeeID]
	if !ok {
		mu = &sync.Mutex{}
		w.holds[employeeID] = mu
	}
	w.holdMu.Unlock()

	mu.Lock() // blocks while a prior call or assess round is live

	w.holdMu.Lock()
	w.heldCalls[ccid] = holdEntry{employeeID: employeeID, mu: mu}
	w.holdMu.Unlock()

	return func() { w.ReleaseForCall(ccid) }
}

// holdEntry is one registered call's release routing.
type holdEntry struct {
	employeeID string
	mu         *sync.Mutex
}

// ReleaseForCall drops the hold for a call exactly once (the map delete
// IS the once-guard: the ccid is removed before the unlock, so a replayed
// outcome cannot double-unlock). Unknown ccids are a no-op (idempotent
// teardown contract, matching the manager). Returns true when a hold was
// released.
func (w *VoiceEmployeeWiring) ReleaseForCall(ccid string) bool {
	w.holdMu.Lock()
	entry, ok := w.heldCalls[ccid]
	if ok {
		delete(w.heldCalls, ccid)
	}
	w.holdMu.Unlock()
	if !ok || entry.mu == nil {
		return false
	}
	entry.mu.Unlock()
	return true
}

// ReleaseHook is the leaf-05 release-hook adapter: install with
// Manager.SetReleaseHook(w.ReleaseHook). It ends the call row, publishes
// terminal cleanup, and drops the goal hold exactly once per ccid (the
// manager fires the hook once; this side also guards with its own map
// delete so a replayed outcome cannot double-unlock).
func (w *VoiceEmployeeWiring) ReleaseHook(ccid string, outcome voice.Outcome) {
	// Peek under holdMu (the ONLY guard of heldCalls; the map delete in
	// ReleaseForCall is the once-guard against replayed outcomes).
	w.holdMu.Lock()
	_, seen := w.heldCalls[ccid]
	w.holdMu.Unlock()
	if !seen {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := w.Binding.OnCallEnd(ctx, ccid, string(outcome)); err != nil {
		w.logger.Error("voice employee wiring: end call", "ccid", ccid, "error", err)
	}
	w.ReleaseForCall(ccid)
	w.logger.Info("voice call released", "ccid", ccid, "outcome", string(outcome))
}

// Close closes the call store. Safe to call once at daemon shutdown.
func (w *VoiceEmployeeWiring) Close() error {
	if w == nil || w.store == nil {
		return nil
	}
	return w.store.Close()
}
