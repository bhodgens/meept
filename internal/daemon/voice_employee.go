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
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/ledger"
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
	bus    *bus.MessageBus
	logger *slog.Logger

	// Ledger brief seams (leaf 03-voice/01): nil ledgerStore = the
	// per-call brief no-ops (feature off by absence, no config knob in
	// v1); nil llm has the same effect. Set via VoiceEmployeeDeps.
	ledgerStore *ledger.Store
	llm         func(prompt string) (string, error)

	// dayBriefFn (leaf 03-voice/03): the wiring-built day-brief closure
	// (open-commitments seam included). Only set when the ledger store
	// is present; holdPhoneCall builds through it and passes an empty
	// DayBrief when nil (nil-store posture unchanged).
	dayBriefFn func(ctx context.Context, employeeID, number string) (voice.DayBrief, error)

	holdMu    sync.Mutex
	holds     map[string]*sync.Mutex // employee_id -> serialization mutex
	heldCalls map[string]holdEntry   // call_control_id -> hold routing entry

	// gates tracks the async goal-hold readiness per inbound call
	// (audit 2026-11-09 finding 1): the webhook spawn seam arms a gate
	// and runs holdPhoneCall on a goroutine; the talk loop's turn
	// runner awaits the gate before the employee's FIRST turn, so the
	// hold still serializes the goal round while the carrier webhook
	// returns promptly. Always non-nil on the WireVoiceEmployee path;
	// hand-built test wirings may leave it nil (degrades to the
	// synchronous-hold posture at the spawn seam).
	gates *holdTracker
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

	// Employees resolves the bound employee's definition (persona prompt +
	// constitution) for turn-time stamping. Nil degrades to the template
	// persona (logged at turn time) - same optional-seam posture as Hold.
	Employees EmployeeDefinitionResolver

	// CallStorePath is $MEEPT_HOME/voice/calls.db (config.MeeptPath).
	CallStorePath string

	// LedgerStore is the employee ledger store (leaf 03-voice/01, built
	// by buildLedger). Nil -> the per-call brief at release is off
	// (feature off by absence; no config knob in v1).
	LedgerStore *ledger.Store

	// BriefLLM is the prompt-in/text-out LLM seam the per-call brief
	// uses — production adapters are built over the daemon's existing
	// llm client by ledger_wiring.go (no new HTTP client). Nil with a
	// non-nil LedgerStore still no-ops the brief.
	BriefLLM func(prompt string) (string, error)
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
// the BOUND EMPLOYEE'S DEFINITION is stamped onto the loop before every
// turn (prompt + constitution -> persona/output constraints), RunOnce
// runs on the session's conversation id, and the exchange is persisted
// to the session store (persistExchange seam) so the phone conversation
// history survives across calls.
//
// Employee-definition seam (voice-persona fix): GetOrCreateWired clones
// the DAEMON TEMPLATE loop, whose config carries the default persona
// (SOUL.md personality + DefaultConstitution/Purpose). Without the stamp
// below, a voice turn ran the daemon default persona - the bound
// employee's spoken-word prompt (companion: no markdown, short spoken
// sentences) and constitution constraints never reached the model. The
// stamp mirrors what a chat turn for that employee would run:
//   - Persona: SystemPromptOverride = def.Prompt + SynthesizedPrompt
//     (constitution constraints/charter). The override lane
//     (buildSystemPromptWithOverride, internal/agent/loop.go:9064) wins
//     over the SOUL.md personality on every prompt build site, and
//     GlobalRules are still appended - platform rules stay in force.
//   - Enforcement: Constraints.Never/NeverRules flow into AgentConfig so
//     the executor's constitution gate (PreExecChecker) and the
//     post-turn audit pattern-match the employee's prohibitions.
//   - Output filters: the reply path already applies the frozen
//     post-step pipeline (applyReplyGuard + filter chain via
//     RunOnceWithParts); the persona override is what makes the employee's
//     no-markdown/short-sentence constraints reach the model so the
//     filter chain judges an in-persona draft.
//
// The resolver never nils: an unknown/missing employee definition is a
// logged Warn + empty override (the turn proceeds on the template persona)
// - a voice call is never dropped because a definition lookup failed
// (the voice never-quota-parked contract's same posture).
type loopTurnRunner struct {
	mgr        *agent.Manager
	template   *agent.AgentLoop
	workingDir string
	sessions   session.Store
	// employees resolves the bound employee's definition. Nil degrades to
	// the legacy template-persona behavior (documented posture, same as
	// the Hold seam).
	employees EmployeeDefinitionResolver
	logger    *slog.Logger
}

// EmployeeDefinitionResolver resolves one employee's runtime definition:
// the persona prompt and the constitution that constrains output. The
// narrow shape internal/daemon needs; employee.Manager satisfies it
// structurally via GetEmployee.
type EmployeeDefinitionResolver interface {
	GetEmployee(ctx context.Context, id string) (*employee.Employee, error)
}

// employeeSystemPrompt renders the employee's system prompt for voice
// turns: the definition prompt (spoken-word persona) followed by the
// constitution synthesized constraints (purpose/role/charter/never
// rules). Empty definition prompt + empty constitution -> "" (no
// override; the template persona stands).
func employeeSystemPrompt(def *employee.Employee) string {
	if def == nil {
		return ""
	}
	prompt := strings.TrimSpace(def.Prompt)
	if def.HasConstitution() {
		c := def.Constitution
		synthesized := employee.SynthesizedPrompt(&c, "")
		if synthesized != "" {
			if prompt != "" {
				prompt = prompt + "\n\n" + synthesized
			} else {
				prompt = synthesized
			}
		}
	}
	return prompt
}

// applyEmployeeDefinition stamps the bound employee's definition onto the
// session loop before the turn: persona override + constitution
// constraints + model ref. It mutates ONLY the per-session cloned loop
// (GetOrCreateWired never returns the shared template), so concurrent
// sessions are isolated by construction. Returns the override actually
// applied ("" = template persona) for logging/tests.
func applyEmployeeDefinition(ctx context.Context, loop *agent.AgentLoop, employeeID string, resolver EmployeeDefinitionResolver, logger *slog.Logger) string {
	if resolver == nil || employeeID == "" {
		return ""
	}
	def, err := resolver.GetEmployee(ctx, employeeID)
	if err != nil || def == nil {
		logger.Warn("voice turn runner: employee definition unavailable (turn runs the template persona)",
			"employee_id", employeeID, "error", err)
		return ""
	}

	cfg := loop.GetConfig()
	override := employeeSystemPrompt(def)
	if override != "" {
		cfg.SystemPromptOveride = override
	} else {
		cfg.SystemPromptOveride = ""
	}
	if def.HasConstitution() {
		c := def.Constitution
		cfg.Purpose = c.Purpose
		cfg.Constitution = employee.SynthesizedPrompt(&c, cfg.Constitution)
		if len(c.Constraints.Never) > 0 {
			cfg.Restrictions = strings.Join(c.Constraints.Never, "; ")
		}
	}
	loop.SetConfig(cfg)

	// Per-employee model binding: the definition's model ref routes the
	// loop onto the employee's configured model. The PERSISTENT override
	// survives across turns (the one-shot lane auto-clears after one
	// reasoning cycle, wrong for a standing per-employee binding); empty
	// definition ref keeps the template's model unchanged.
	if ref := strings.TrimSpace(def.Model); ref != "" {
		loop.SetPersistentModelOverride(ref)
	}
	return override
}

// Run implements voice.TurnRunner.
func (r *loopTurnRunner) Run(ctx context.Context, sess voice.SessionHandle, prompt string) (string, error) {
	loop, err := r.mgr.GetOrCreateWired(sess.ID, r.workingDir, r.template)
	if err != nil {
		return "", fmt.Errorf("voice turn runner: wired loop for %s: %w", sess.ID, err)
	}
	employeeID := sess.EmployeeID
	if override := applyEmployeeDefinition(ctx, loop, employeeID, r.employees, r.logger); override != "" {
		r.logger.Debug("voice turn runner: employee persona applied",
			"session_id", sess.ID, "employee_id", employeeID, "prompt_bytes", len(override))
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
			employees:  deps.Employees,
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
		Binding:     binding,
		store:       store,
		bus:         deps.Bus,
		logger:      logger,
		ledgerStore: deps.LedgerStore,
		llm:         deps.BriefLLM,
		holds:       make(map[string]*sync.Mutex),
		heldCalls:   make(map[string]holdEntry),
		gates:       newHoldTracker(),
	}
	return w, nil
}

// progressPublisherFunc adapts a func onto voice.ProgressPublisher.
type progressPublisherFunc func(ev voice.ProgressEvent)

// PublishProgress implements voice.ProgressPublisher.
func (f progressPublisherFunc) PublishProgress(ev voice.ProgressEvent) { f(ev) }

// ---- async goal-hold gate (audit 2026-11-09 finding 1) ----

// holdGate is one inbound call's goal-hold readiness signal. The webhook
// spawn seam arms it BEFORE launching holdPhoneCall on a goroutine; the
// goroutine marks it ready (defer, so even a failed hold releases turn
// waiters) once AcquireCallHold + OnCallStart have completed. The talk
// loop's turn runner awaits it before the employee's FIRST turn, so the
// per-employee serialization the synchronous hold provided is preserved
// while the carrier webhook returns promptly.
//
// released is set by releaseCall (through the tracker) when the call's
// terminal outcome fires while the hold is still pending: the hold
// goroutine sees it after the mutex is finally acquired and rolls the
// registration back (ReleaseForCall + OnCallEnd) so a hung-up-during-
// pending call cannot leak the employee mutex or a stuck 'live' row. The
// rollback is once-guarded by the same map-delete/live checks the normal
// fan-out uses, so the two paths racing is safe.
type holdGate struct {
	ready    chan struct{}
	once     sync.Once
	mu       sync.Mutex
	released bool
	reason   string
}

// markReady closes the gate exactly once.
func (g *holdGate) markReady() { g.once.Do(func() { close(g.ready) }) }

// markReleased records that the call terminal-fired while the hold pends
// (with the outcome reason for the rollback row). Idempotent.
func (g *holdGate) markReleased(outcome string) {
	g.mu.Lock()
	if !g.released {
		g.released = true
		g.reason = outcome
	}
	g.mu.Unlock()
}

// releaseState reports the recorded terminal state for a rollback.
func (g *holdGate) releaseState() (bool, string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.released, g.reason
}

// holdTracker is the ccid -> holdGate index shared by the spawn seam,
// the turn runner, and releaseCall. All methods are nil-receiver safe
// except arm: a nil tracker means "no async gating" and the caller
// degrades to the synchronous hold.
type holdTracker struct {
	mu    sync.Mutex
	gates map[string]*holdGate
}

func newHoldTracker() *holdTracker {
	return &holdTracker{gates: make(map[string]*holdGate)}
}

// arm registers a fresh gate for ccid (a replayed call.initiated replaces
// the prior gate; the prior hold goroutine keeps its own pointer, and the
// replacement only re-points the lookups) and returns it.
func (t *holdTracker) arm(ccid string) *holdGate {
	g := &holdGate{ready: make(chan struct{})}
	t.mu.Lock()
	if t.gates == nil {
		t.gates = make(map[string]*holdGate)
	}
	t.gates[ccid] = g
	t.mu.Unlock()
	return g
}

// peek returns the current gate for ccid (nil when absent or tracker nil).
func (t *holdTracker) peek(ccid string) *holdGate {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.gates[ccid]
}

// drop removes ccid's gate and marks it released (outcome carried for the
// hold goroutine's rollback). Called from releaseCall BEFORE the ReleaseHook
// fan-out: any hold still pending on the mutex sees released=true the
// moment it registers, and rolls back; the ReleaseHook's heldCalls peek
// happens after this, so the two once-guarded paths cannot BOTH skip the
// teardown. A nil tracker / unknown ccid is a no-op (synchronous-posture
// calls and outbound legs never arm).
func (t *holdTracker) drop(ccid, outcome string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	g := t.gates[ccid]
	delete(t.gates, ccid)
	t.mu.Unlock()
	if g != nil {
		g.markReleased(outcome)
	}
}

// await gates a call's first employee turn: blocks until ccid's hold gate
// is marked ready (hold acquired + OnCallStart applied), the gate is
// absent/never armed (nothing to wait for), or ctx is cancelled (the call
// was released mid-pend). Returns true when the caller may proceed after
// the hold (or no hold is pending); false on ctx cancellation.
func (t *holdTracker) await(ctx context.Context, ccid string) bool {
	g := t.peek(ccid)
	if g == nil {
		return true
	}
	select {
	case <-g.ready:
		return true
	case <-ctx.Done():
		return false
	}
}

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
// delete so a replayed outcome cannot double-unlock). AFTER teardown it
// publishes the call-end employee.notify relay (notifyCallRelay) for
// EVERY terminal outcome - held or not, phone or local leg.
func (w *VoiceEmployeeWiring) ReleaseHook(ccid string, outcome voice.Outcome) {
	// Peek under holdMu (the ONLY guard of heldCalls; the map delete in
	// ReleaseForCall is the once-guard against replayed outcomes).
	w.holdMu.Lock()
	_, seen := w.heldCalls[ccid]
	w.holdMu.Unlock()
	if seen {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := w.Binding.OnCallEnd(ctx, ccid, string(outcome)); err != nil {
			w.logger.Error("voice employee wiring: end call", "ccid", ccid, "error", err)
		}
		w.ReleaseForCall(ccid)
		w.logger.Info("voice call released", "ccid", ccid, "outcome", string(outcome))
		// Best-effort per-call ledger brief (leaf 03-voice/01): runs
		// AFTER the release + log line, inside the same once-guard, and
		// NEVER fails or delays the teardown — every failure is WARN/INFO
		// only, mirroring the notifyCallRelay contract. nil store/llm =
		// complete no-op (feature off by absence).
		w.runCallBrief(ctx, ccid)
	}
	// Best-effort and last: the relay must never delay or fail the
	// teardown above (the notification contract, pinned by the unit test).
	w.notifyCallRelay(ccid, outcome)
}

// relayTranscriptBudget caps the rendered transcript section of the call
// relay (a few hundred characters keeps the notification scannable).
const relayTranscriptBudget = 400

// notifyCallRelay is the call-end notification relay (secretary leaf): on
// every terminal outcome it reads the call row from calls.db and publishes
// a compact relay of the call - employee id, leg (phone/local), direction,
// caller/target - plus the transcript rendered readably ("caller: ..." /
// "reply: ...", capped) on agent.SpeakTopicNotify ("employee.notify"). The
// speak push bridge (speak_push_bridge.go) decodes agent.NotifyPayload and
// pushes it to TUI/GUI/taskbar surfaces; the WS bridge independently
// forwards the same topic to browser clients. The call's bound session id
// fills BOTH payload id fields (session_id and conversation_id), the same
// duality sessionFor pins.
//
// Contract:
//   - Best-effort: every failure logs a Warn and NEVER fails the release
//     (a nil bus or store drops the relay silently).
//   - Calls with an empty transcript (no conversation happened) notify
//     nothing, as do ccids with no call row at all.
//   - Always-on by design: no per-employee or global opt-out knob exists
//     in [voice] today, and relaying the message is the secretary
//     employee's whole purpose. If an opt-out is ever wanted, add a
//     [voice] config key and gate HERE - this is the single publish site.
func (w *VoiceEmployeeWiring) notifyCallRelay(ccid string, outcome voice.Outcome) {
	if w.bus == nil || w.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rec, lines, ok, err := w.store.EndedCall(ctx, ccid)
	if err != nil {
		w.logger.Warn("voice employee wiring: call relay read", "ccid", ccid, "error", err)
		return
	}
	if !ok {
		return
	}
	text := renderCallRelay(rec, lines, string(outcome))
	if text == "" {
		return
	}
	pub := agent.BusSpeakPublisher(w.bus, "voice-call-relay")
	if err := pub(agent.SpeakNotify, text, rec.SessionID, rec.SessionID); err != nil {
		w.logger.Warn("voice employee wiring: call relay publish", "ccid", ccid, "error", err)
		return
	}
	w.logger.Debug("voice call relay published", "ccid", ccid, "session_id", rec.SessionID)
}

// renderCallRelay builds the notification text: a one-line header
// (outcome, employee, leg, direction, target) followed by the transcript
// as "caller: ..." / "reply: ..." lines. Returns "" (notify nothing) when
// the transcript carries no rendered content. The leg derives from the
// stored target ("local:<label>" is the local leg; everything else is the
// phone leg) because the registry instance is already released when the
// relay runs.
func renderCallRelay(rec voice.CallRecord, lines []voice.TranscriptLine, outcome string) string {
	leg := "phone"
	if strings.HasPrefix(rec.Number, "local:") {
		leg = "local"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "call ended (%s): employee %s | %s leg | %s | %s",
		outcome, rec.EmployeeID, leg, rec.Direction, rec.Number)
	used := 0
	for _, l := range lines {
		t := strings.TrimSpace(l.Text)
		if t == "" {
			continue
		}
		role := "caller"
		if l.Role == "assistant" {
			role = "reply"
		}
		line := role + ": " + t
		if used > 0 {
			line = "\n" + line
		}
		if used+len(line) > relayTranscriptBudget {
			b.WriteString("\n...")
			break
		}
		b.WriteString(line)
		used += len(line)
	}
	if used == 0 {
		return "" // empty transcript: no conversation happened, notify nothing
	}
	return b.String()
}

// Close closes the call store. Safe to call once at daemon shutdown.
func (w *VoiceEmployeeWiring) Close() error {
	if w == nil || w.store == nil {
		return nil
	}
	return w.store.Close()
}
