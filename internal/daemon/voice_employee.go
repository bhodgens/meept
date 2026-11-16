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

	// storePath is the calls.db path (kept for the Stop-path pin test
	// and diagnostics; the store handle itself stays unexported).
	storePath string

	store  *voice.CallStore
	bus    *bus.MessageBus
	logger *slog.Logger

	// Ledger brief seams (leaf 03-voice/01): nil ledgerStore = the
	// per-call brief no-ops (feature off by absence, no config knob in
	// v1); nil llm has the same effect. Set via VoiceEmployeeDeps.
	ledgerStore *ledger.Store
	llm         func(prompt string) (string, error)

	// displayLookup is the contact-display resolver seam (leaf 02 §5):
	// notifyCallRelay resolves the stored caller number through it so the
	// relay HEADER carries the name/last-4 display form, never the raw
	// E.164. nil (contact store unavailable) renders the unknown-caller
	// form — nil-safe via resolveDisplayFor.
	displayLookup CallerDisplayLookup

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

	// DisplayLookup (contact-display leaf 02 §5): the resolver seam the
	// call-relay header keys the stored caller number through. Nil ->
	// the relay renders the unknown-caller + last-4 form (never raw).
	DisplayLookup CallerDisplayLookup

	// BriefLLM is the prompt-in/text-out LLM seam the per-call brief
	// uses — production adapters are built over the daemon's existing
	// llm client by ledger_wiring.go (no new HTTP client). Nil with a
	// non-nil LedgerStore still no-ops the brief.
	BriefLLM func(prompt string) (string, error)

	// MomentResolver builds the per-employee "# Moment" renderer for the
	// voice clone loop (plan 20261110-situational leaf 01-moment/02;
	// internal/daemon/awareness_wiring.go). nil (awareness disabled) =
	// no renderer is ever stamped and voice prompts stay byte-identical
	// (D1). Keyed by the bound employee id — the same id SetAgentID
	// stamps (caller-authz leaf 02 precedent).
	MomentResolver func(employeeID string) func() string

	// SchedulerTZ is the daemon's [scheduler] timezone (C-8, stamp-clock
	// fallback chain): used for the voice turn stamp when the bound
	// employee's definition carries no usable Timezone. Empty keeps
	// time.Local (the daemon clock).
	SchedulerTZ string

	// GuidedIntakePromptFor (uptractor-ports leaf 05): resolves the
	// guided-intake prompt extension for the bound employee (the
	// one-question-at-a-time procedure block). Empty result = no
	// extension (the employee_leg row did not opt in — guided intake is
	// the secretary's call behavior, secretary-scoped per the owner
	// decision). nil seam = the extension is never stamped
	// (off-by-absence, D1 posture same as MomentResolver).
	GuidedIntakePromptFor func(employeeID string) string
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
	// momentResolver builds the per-employee "# Moment" renderer stamped
	// onto session clones each turn (awareness leaf 01/02). nil = inert.
	momentResolver func(employeeID string) func() string
	// schedulerTZ is the daemon's [scheduler] timezone — the MIDDLE of
	// the C-8 stamp-clock fallback chain (employee def tz -> scheduler tz
	// -> time.Local). Empty keeps time.Local. Set by WireVoiceEmployee
	// from the daemon config; hand-built test runners may leave it "".
	schedulerTZ string
	// guidedIntakePromptFor resolves the guided-intake procedure block
	// for the bound employee (leaf 05). nil = never stamped.
	guidedIntakePromptFor func(employeeID string) string

	// Prior-reasoning retention (voice lane): the LAST captured
	// llm.Response.Reasoning from a voice turn on each session, bounded
	// and overwritten each turn (latest wins). It NEVER enters the
	// conversation history (the "Reasoning never persists in history"
	// invariant — AddAssistantMessage strips thinking before append), so
	// it cannot create the stale-reasoning stagnation the invariant
	// guards against. It is injected as OPTIONAL bounded scaffolding into
	// the NEXT turn's system prompt (voice-reasoning scaffold below) and
	// only ever reaches the model's context — never the reply text and
	// therefore never qwen3-TTS.
	reasonMu       sync.Mutex
	retainedReason map[string]string // sessionID -> bounded prior reasoning
	// reasonSeen tracks each slot's last-turn time (playMu-sibling:
	// reasonMu guards both) so the bounded map can evict the
	// oldest-turned session when maxRetainedReasoningSessions is hit.
	reasonSeen map[string]time.Time
}

// maxPriorReasoningBytes bounds a single retained reasoning block before
// it is stashed or injected. Kept deliberately small: the block is one
// optional reminder, not a replay of the whole chain of thought.
const maxPriorReasoningBytes = 4000

// maxRetainedReasoningSessions bounds the retainedReason map (defect
// 2026-11-14: the map grew one entry per session id, unbounded for a
// long-lived daemon serving many calls). Small by the same posture as
// maxPriorReasoningBytes: a handful of concurrent voice sessions is the
// real working set; eviction drops the OLDEST-TURNED session's slot
// (last-N by insert/update order — a "recently used" posture without a
// separate list structure).
const maxRetainedReasoningSessions = 8

// boundReasoning truncates reasoning text to the retention cap. A whole
// leading slice is kept; the tail is dropped with a marker so the model
// is not handed a mid-thought fragment. The cut is RUNE-SAFE: a raw
// byte slice at the cap could split a multi-byte UTF-8 rune and hand the
// model a corrupted tail, so the boundary backs up to the last complete
// rune inside the cap (ASCII reasoning is unaffected — every byte is a
// rune boundary).
func boundReasoning(r string) string {
	if len(r) <= maxPriorReasoningBytes {
		return r
	}
	cut := maxPriorReasoningBytes
	// Back the cut up over any trailing partial rune: while the last
	// byte at the boundary is a UTF-8 continuation byte (10xxxxxx), the
	// rune it belongs to started earlier and would be split.
	for cut > 0 && isUTF8Continuation(r[cut]) {
		cut--
	}
	return r[:cut] + "\n[…retained reasoning truncated]"
}

// isUTF8Continuation reports whether b is a UTF-8 continuation byte
// (0b10xxxxxx): a byte that can never START a rune.
func isUTF8Continuation(b byte) bool { return b&0xC0 == 0x80 }

// priorReasoningBlock renders the model-facing scaffolding header for an
// injected prior-reasoning retention. The header explicitly marks the
// block as internal context the model may use to understand the
// conversation but must NOT read aloud or repeat — the block is a system
// prompt, so it can never reach TTS (only the reply text is spoken).
func priorReasoningBlock(reasoning string) string {
	return "\n\n# Prior reasoning (internal context)\n" +
		"The following is the model's prior chain-of-thought from the previous turn. " +
		"Use it to understand the ongoing conversation, but do NOT read it aloud or repeat it to the user.\n\n" +
		reasoning
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
func applyEmployeeDefinition(ctx context.Context, loop *agent.AgentLoop, employeeID string, resolver EmployeeDefinitionResolver, parent agent.ToolRegistry, logger *slog.Logger) string {
	if resolver == nil || employeeID == "" {
		return ""
	}
	def, err := resolver.GetEmployee(ctx, employeeID)
	if err != nil || def == nil {
		logger.Warn("voice turn runner: employee definition unavailable (turn runs the template persona)",
			"employee_id", employeeID, "error", err)
		// A turn with an unresolvable employee runs deny-all, never the
		// full template list: without the definition there is no
		// constitution to bind, so no tool is provably permitted. The
		// clone's persona stays the template's (the warning above); only
		// the registry is constrained.
		if parent != nil {
			loop.SetToolRegistry(agent.NewFilteredToolRegistry(parent, nil, agent.WithDenyAll()))
		}
		return ""
	}

	cfg := loop.GetConfig()
	override := employeeSystemPrompt(def)
	if override != "" {
		cfg.SystemPromptOveride = override
	} else {
		cfg.SystemPromptOveride = ""
	}
	// Evidence-format template slim (defect A, 2026-11-10): the Validation
	// & Escalation anchor carries a JSON claims/evidence TEMPLATE. An
	// employee with NO tools in its definition (secretary: tools: []) can
	// never produce tool evidence, so the model had nothing to summarize —
	// it echoed the template itself into the SPOKEN reply, which TTS read
	// to the caller (link-rec-1794333239 transcript b). Drop the template
	// block for zero-tool definitions; the prose validation rules stay.
	// Chat-lane loops never route through here, so their anchor ships
	// byte-identical (flag default false).
	cfg.OmitEvidenceFormatTemplate = len(def.Tools) == 0
	if def.HasConstitution() {
		c := def.Constitution
		cfg.Purpose = c.Purpose
		cfg.Constitution = employee.SynthesizedPrompt(&c, cfg.Constitution)
		if len(c.Constraints.Never) > 0 {
			cfg.Restrictions = strings.Join(c.Constraints.Never, "; ")
		}
		// The constitution's tool policy decides what the model may
		// see: a declared tools_allowed (including an explicit "*" or a
		// declared-empty list) or a non-empty tools_forbidden filters
		// the clone's registry via employeeVisibleTools. A constitution
		// that declares no tool lists at all keeps the template
		// registry (inherit-everything).
		if parent != nil {
			visible := employeeVisibleTools(parent, &c)
			loop.SetToolRegistry(visible)
			if visible != parent {
				cfg.OmitEvidenceFormatTemplate = len(visible.GetDefinitions()) == 0
			}
		}
	} else if parent != nil {
		loop.SetToolRegistry(parent)
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

// templateRegistry is the full tool list the shared voice template holds.
// The per-session clone must filter from this list, not from its own
// registry: a second turn would otherwise filter an already-filtered set.
func templateRegistry(loop *agent.AgentLoop) agent.ToolRegistry {
	if loop == nil {
		return nil
	}
	return loop.ToolRegistry()
}

// employeeTurnClone builds a throwaway per-turn clone of a SHARED
// template loop with the employee's constitution tool policy applied to
// its registry — the same rule applyEmployeeDefinition stamps on the
// voice clone (employeeVisibleTools over the TEMPLATE's full registry,
// never the clone's own). The shared template loop is never mutated:
// SetToolRegistry/SetAgentID run on the clone only (mirrors the voice
// lane's GetOrCreateWired pattern; the goal-loop lane has no session
// cache, so the clone lives for exactly one turn). Returns nil when the
// template is nil so callers fail loudly instead of panicking.
func employeeTurnClone(template *agent.AgentLoop, conversationID, employeeID string, c *employee.Constitution) *agent.AgentLoop {
	if template == nil {
		return nil
	}
	clone := agent.NewAgentLoop(conversationID, template.GetWorkingDir(), template.ConfigSnapshot()...)
	if clone == nil {
		return nil
	}
	if employeeID != "" {
		// Same authz stamp as the voice lane: the executor injects
		// details["agent_id"] from the loop id, and the PreExecChecker
		// (employee constitution gate) routes on it.
		clone.SetAgentID(employeeID)
	}
	if c != nil {
		if parent := templateRegistry(template); parent != nil {
			clone.SetToolRegistry(employeeVisibleTools(parent, c))
		}
	}
	return clone
}

// employeeVisibleTools returns the tool registry an employee turn may
// show the model. The rule:
//   - No tool policy at all (tools_allowed nil AND tools_forbidden
//     empty) returns parent unchanged (inherit-everything; this is also
//     the no-Constraints shape).
//   - tools_allowed with an explicit "*" inherits the parent list, then
//     tools_forbidden is subtracted.
//   - A DECLARED-empty tools_allowed (the constitution names zero
//     tools) shows NO tools - fail closed, never inherit.
//   - A non-empty tools_allowed is intersected with the parent, minus
//     tools_forbidden.
//
// tools_forbidden "*" returns a deny-all registry. Names match
// case-insensitively; the registry keeps the parent's spelling so Get
// stays exact.
func employeeVisibleTools(parent agent.ToolRegistry, c *employee.Constitution) agent.ToolRegistry {
	if parent == nil || c == nil {
		return parent
	}
	allowed := c.Constraints.ToolsAllowed
	forbidden := c.Constraints.ToolsForbidden
	// inherit = no allowlist declared (nil), or an explicit "*" entry.
	// A declared-empty (non-nil, len 0) allowlist is a deny-all policy,
	// NOT the undeclared case.
	star := false
	for _, name := range allowed {
		if strings.ToLower(strings.TrimSpace(name)) == "*" {
			star = true
			break
		}
	}
	if allowed != nil && len(allowed) == 0 && !star {
		return agent.NewFilteredToolRegistry(parent, nil, agent.WithDenyAll())
	}
	inherit := allowed == nil || star
	if inherit && len(forbidden) == 0 {
		return parent
	}
	for _, name := range forbidden {
		if strings.TrimSpace(name) == "*" {
			return agent.NewFilteredToolRegistry(parent, nil, agent.WithDenyAll())
		}
	}
	deny := make(map[string]struct{}, len(forbidden))
	for _, name := range forbidden {
		key := strings.ToLower(strings.TrimSpace(name))
		if key != "" {
			deny[key] = struct{}{}
		}
	}
	want := make(map[string]struct{}, len(allowed))
	if !star {
		for _, name := range allowed {
			key := strings.ToLower(strings.TrimSpace(name))
			if key != "" {
				want[key] = struct{}{}
			}
		}
	}
	var names []string
	for _, tool := range parent.List() {
		if tool == nil {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(tool.Name()))
		if _, no := deny[key]; no {
			continue
		}
		if !star && len(want) > 0 {
			if _, yes := want[key]; !yes {
				continue
			}
		}
		names = append(names, tool.Name())
	}
	if len(names) == 0 {
		return agent.NewFilteredToolRegistry(parent, nil, agent.WithDenyAll())
	}
	return agent.NewFilteredToolRegistry(parent, names)
}

// voiceTurnTimeLayout is the clock layout for the voice-turn stamp:
// weekday, date, wall time, and an explicit numeric UTC offset so the
// model can do timezone arithmetic. The zone ABBREVIATION is deliberately
// NOT part of the layout (the numeric offset carries the actionable
// information): Go's time.Parse only accepts "MST" elements matching
// three-letter or "UTC+-hh"-style zone text, so a numeric zone name
// (FixedZone("+0530", ...)) or a long IANA name (time.LoadLocation's
// "America/Denver") once failed the round-trip the pin test pins (C-7,
// 2026-11-11 bughunt). With the abbreviation gone, the SAME layout
// formats and parses: every zone Go renders round-trips (pinned by
// TestVoiceTurnContextStampRoundTripsTimeParse, FixedZone("+0530") and a
// long-name zone included). The exact prefix the stamp prepends is
// mirrored by agent.stripVoiceContextStamp (loop.go) — the pin tests in
// both packages keep the shapes in lockstep.
const voiceTurnTimeLayout = "Mon 2006-01-02 15:04 (UTC-07:00)"

// voiceTurnContextStamp renders the bracketed scaffolding line prepended to
// a voice turn's spoken prompt (see Run for why). The stamp is model-facing
// scaffolding: persistExchange still stores the raw spoken prompt, and the
// reply's echo is backstopped by agent.SpokenReplyViolation on the way out.
//
// C-4 (2026-11-11 bughunt) — model history vs. store divergence, PINNED
// as-is: the STAMPED prompt is what RunOnce puts in the model-visible
// Conversation (and thus what a conversation restore replays), while
// persistExchange below stores the RAW spoken prompt in the session
// store. The divergence is deliberate: each turn re-stamps a FRESH clock
// at the seam, so the model always carries a current date/time even when
// later turns replay earlier stamped history; the store keeps the user's
// actual words so transcripts/relays never read scaffolding aloud. Do NOT
// "fix" one side to match the other without re-deriving both consumers —
// changing the store content changes transcripts and relays, and removing
// the stamp from the model lane re-opens the live 2026-11-11 defect
// ("what time is it?" -> "I can't check the current time").
func voiceTurnContextStamp(now time.Time) string {
	return "[Context: current date/time is " + now.Format(voiceTurnTimeLayout) + "] "
}

// resolveVoiceTurnLocation resolves the CLOCK location for a voice turn's
// stamp (C-8, 2026-11-11 bughunt): the stamp should show the EMPLOYEE's
// wall clock, not the daemon's, so the resolution mirrors the awareness
// lane's zone chain (internal/daemon/awareness_wiring.go aw2SchedulerLoc /
// moment.EmployeeDef.Timezone): the bound employee's BotDefinition.Timezone
// (the same field the "# Moment" block renders) wins; empty/unparseable
// falls to the daemon scheduler timezone; that too empty/unparseable falls
// to time.Local (the pre-fix behavior — daemon clock). Never fails a turn:
// every fallback is silent here (the def-level bad-zone WARN belongs to
// the moment render path, and a clock stamp is best-effort scaffolding).
func (r *loopTurnRunner) resolveVoiceTurnLocation(ctx context.Context, employeeID string) *time.Location {
	if r.employees != nil && employeeID != "" {
		ctx2, cancel := context.WithTimeout(ctx, 2*time.Second)
		def, err := r.employees.GetEmployee(ctx2, employeeID)
		cancel()
		if err == nil && def != nil && def.Timezone != "" {
			if loc, tzErr := time.LoadLocation(def.Timezone); tzErr == nil {
				return loc
			}
		}
	}
	if r.schedulerTZ != "" {
		if loc, err := time.LoadLocation(r.schedulerTZ); err == nil {
			return loc
		}
	}
	return time.Local
}

// Run implements voice.TurnRunner.
func (r *loopTurnRunner) Run(ctx context.Context, sess voice.SessionHandle, prompt string) (string, error) {
	loop, err := r.mgr.GetOrCreateWired(sess.ID, r.workingDir, r.template)
	if err != nil {
		return "", fmt.Errorf("voice turn runner: wired loop for %s: %w", sess.ID, err)
	}
	employeeID := sess.EmployeeID
	// AgentID fix (caller-authz leaf 02, O5): voice clones execute with
	// the executor agent id = the bound employee id. The template loop
	// carries "default" (components.go WithAgentID("default")), clones
	// inherit it via ConfigSnapshot, and the executor injects
	// details["agent_id"] from its own id — the PreExecChecker (employee
	// constitution gate) routes on it, so a "default" voice loop silently
	// skipped every per-employee tool gate. Stamped BEFORE RunOnce on the
	// PER-SESSION clone only (GetOrCreateWired never returns the shared
	// template); empty employee id keeps the inherited id (byte-identical
	// pre-leaf behavior for non-employee sessions). The Manager cache key
	// derives from the TEMPLATE's agentID, so restamping never rehashes
	// the cache.
	if employeeID != "" {
		loop.SetAgentID(employeeID)
	}
	// Moment renderer stamp (plan 20261110-situational leaf 01-moment/02):
	// the voice override lane appends the employee's "# Moment" block
	// (buildSystemPromptWithOverride, gate §4). Resolved per bound
	// employee id — the same id the authz stamp above carries — and set
	// BEFORE RunOnce so the first turn's prompt already has the block. A
	// nil resolver (awareness off) explicitly sets nil: cached clones may
	// carry an id rebound to another employee (or none), and nil = the
	// pinned D1 inert shape.
	if r.momentResolver != nil {
		loop.SetMomentRenderer(r.momentResolver(employeeID))
	} else {
		loop.SetMomentRenderer(nil)
	}
	if override := applyEmployeeDefinition(ctx, loop, employeeID, r.employees, templateRegistry(r.template), r.logger); override != "" {
		r.logger.Debug("voice turn runner: employee persona applied",
			"session_id", sess.ID, "employee_id", employeeID, "prompt_bytes", len(override))
	}
	// Guided intake (leaf 05, secretary-scoped): when the employee's
	// employee_leg row opts in, append the one-question-at-a-time
	// procedure block to the persona override lane. Runs after
	// applyEmployeeDefinition so the extension rides the SAME
	// SystemPromptOveride override (buildSystemPromptWithOverride wins
	// over SOUL.md on every prompt build site). An empty extension (row
	// off) leaves the prompt byte-identical to pre-leaf behavior.
	if r.guidedIntakePromptFor != nil {
		if ext := r.guidedIntakePromptFor(employeeID); ext != "" {
			cfg := loop.GetConfig()
			if cfg.SystemPromptOveride != "" {
				cfg.SystemPromptOveride = cfg.SystemPromptOveride + "\n\n" + ext
			} else {
				cfg.SystemPromptOveride = ext
			}
			loop.SetConfig(cfg)
			r.logger.Debug("voice turn runner: guided intake prompt applied",
				"session_id", sess.ID, "employee_id", employeeID)
		}
	}
	// Prior-reasoning retention (voice lane): the last captured
	// llm.Response.Reasoning for this session rides the NEXT turn's
	// system prompt as OPTIONAL bounded scaffolding so the model can use
	// the prior chain-of-thought to understand the conversation. Appended
	// here — after the employee persona and the guided-intake extension —
	// so the block composes with the same override lane the voice loop
	// short-circuits through (buildSystemPromptWithOverride). It NEVER
	// touches conversation history (the reply text appended by
	// RunOnceWithParts is already thinking-stripped) and NEVER the reply
	// text, so it can never reach qwen3-TTS: only `reply` is spoken. A
	// session with no captured reasoning injects nothing (byte-identical
	// to the pre-retention turn).
	r.reasonMu.Lock()
	prior := r.retainedReason[sess.ID]
	r.reasonMu.Unlock()
	if prior != "" {
		cfg := loop.GetConfig()
		if cfg.SystemPromptOveride != "" {
			cfg.SystemPromptOveride += priorReasoningBlock(prior)
		} else {
			cfg.SystemPromptOveride = priorReasoningBlock(prior)
		}
		loop.SetConfig(cfg)
	}

	// Clock stamp (live defect 2026-11-11: asked "what time is it?" the
	// companion answered "I can't check the current time" — the voice turn
	// carried no clock, and the model honestly reported what its prompt
	// knew). Mirror the chat lane's injection idiom (strategic.go prepends
	// its session-context scaffolding to the task text): a short
	// date/time line is prepended to the USER prompt at turn start, on the
	// voice seam only. The clock is resolved in the BOUND EMPLOYEE's
	// timezone (C-8 fix: the "# Moment" awareness block renders the
	// employee's zone; the stamp previously used the daemon's clock —
	// resolveVoiceTurnLocation mirrors the awareness fallback chain).
	// Persisted history keeps the raw spoken prompt, and the spoken-reply
	// guard (agent.SpokenReplyViolation, applied to the reply below) stays
	// the echo backstop — the stamp is scaffolding for the model, never
	// something TTS should read. RunOnceWithParts strips the stamp from
	// every DERIVED prompt field (agent.stripVoiceContextStamp, C-3) while
	// keeping it on the model-visible message.
	//
	// C-6 (2026-11-11 bughunt): the stamp is declared on the turn context
	// via agent.WithVoiceClockStamp, so the loop wraps ONLY the raw spoken
	// prompt in the <<<USER_INPUT>>> boundary and prepends the stamp
	// OUTSIDE it as trusted scaffolding — inside the boundary the stamp
	// was formally "DATA only" (the system prompt's own words), and a
	// caller-supplied second bracketed context line was indistinguishable
	// from the authoritative one. The prompt carries the RAW spoken text
	// (the stamp rides the context alone; the loop renders the final
	// model-visible message), and persisted history / derived fields
	// (C-3) see the same raw prompt as before.
	now := time.Now().In(r.resolveVoiceTurnLocation(ctx, employeeID))
	stamp := voiceTurnContextStamp(now)
	// Opt-in reasoning capture (voice lane): arm a call-scoped sink on the
	// turn context so reasoningCycle captures llm.Response.Reasoning into
	// it; read it back below and retain it for the NEXT turn. The sink
	// travels the same turnCtx into the spoken-guard rewrite retry, so a
	// rewrite's reasoning overwrites the pre-retry reasoning (the final
	// spoken text's reasoning is what gets retained).
	sink := &agent.ReasoningSink{}
	turnCtx := agent.WithReasoningSink(agent.WithVoiceClockStamp(ctx, stamp), sink)
	reply, err := loop.RunOnce(turnCtx, prompt, sess.ConversationID)
	if err != nil {
		return "", err
	}
	// Machine-shaped output guard for the SPOKEN lane (defect A,
	// 2026-11-10): the chat lane's reply guard at RunOnceWithParts
	// (internal/agent/loop.go response assembly) DOES run for voice turns
	// — voice reaches it through RunOnce — but its detectors deliberately
	// TOLERATE the loop's honesty label ("[unverified: no tools were
	// executed this turn]", pinned by TestChainStability_NudgeBudgetRecovery
	// as the text-lane delivery) and evidence-prose shapes that are fine to
	// READ but garbage to HEAR: the recorded secretary reply carried the
	// label, the JSON-template vocabulary (tool_call_id / success=true /
	// memory_id), and TTS spoke it all to the caller. The voice lane gets
	// the spoken SUPERSET (agent.SpokenReplyViolation — shared shapes plus
	// spoken-only markers) with the SAME bounded posture as the chat
	// guard: ONE rewrite nudge re-entering the loop's turn producer, and a
	// second trip (or an errored/parked rewrite) ships the canned speakable
	// line. SEAM CHOICE: this adapter, NOT RunOnceWithParts — the chat
	// lane shares that seam and its pinned label-delivery behavior must not
	// change; internal/voice may not import internal/agent (import pin),
	// so the daemon adapter between the loop reply and the say frames is
	// the only legal shared choke point every voice reply passes through
	// (phone, local, and link legs all route Turns.Run here). The check is
	// pure/idempotent, and the rewrite budget is consumed at most once per
	// turn, so the guard effectively runs exactly once per reply.
	if violation, trips := agent.SpokenReplyViolation(reply); trips {
		r.logger.Warn("voice spoken guard replaced a machine-shaped reply; retrying once with a rewrite nudge",
			"session_id", sess.ID,
			"conversation_id", sess.ConversationID,
			"matched", violation,
		)
		retry, retryErr := loop.RunVoiceSpokenRewriteRetry(turnCtx, sess.ConversationID)
		if retryErr != nil {
			r.logger.Warn("voice spoken-guard rewrite retry failed; shipping the canned spoken line",
				"session_id", sess.ID, "error", retryErr)
			reply = agent.VoiceSpokenFallbackLine
		} else {
			reply = retry
		}
	}
	// Retain the captured reasoning for this session (latest wins),
	// bounded. Runs AFTER the spoken-guard rewrite so the sink holds the
	// FINAL reply's reasoning (RunVoiceSpokenRewriteRetry re-enters the
	// producer with the same turnCtx, overwriting the sink); stored so the
	// next Run on this session injects it as prior-reasoning scaffolding.
	if captured := sink.Get(); captured != "" {
		r.reasonMu.Lock()
		if r.retainedReason == nil {
			r.retainedReason = make(map[string]string)
		}
		// Bounded last-N by update order (defect 2026-11-14): the map
		// previously grew one entry per session id for the daemon's
		// whole life. When at capacity, evict the OLDEST slot (insert/
		// update order) before writing; the re-armed session simply
		// re-fills its slot on its next turn.
		if _, exists := r.retainedReason[sess.ID]; !exists &&
			len(r.retainedReason) >= maxRetainedReasoningSessions {
			oldest := ""
			oldestAt := time.Time{}
			for id := range r.retainedReason {
				if at := r.reasonSeen[id]; oldest == "" || at.Before(oldestAt) {
					oldest, oldestAt = id, at
				}
			}
			if oldest != "" {
				delete(r.retainedReason, oldest)
				delete(r.reasonSeen, oldest)
			}
		}
		r.retainedReason[sess.ID] = boundReasoning(captured)
		if r.reasonSeen == nil {
			r.reasonSeen = make(map[string]time.Time)
		}
		r.reasonSeen[sess.ID] = time.Now()
		r.reasonMu.Unlock()
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
			// Moment renderer seam (awareness leaf 01/02): resolved per bound
			// employee id at turn time on the session clone.
			momentResolver: deps.MomentResolver,
			schedulerTZ:    deps.SchedulerTZ,
			// Guided intake prompt seam (leaf 05): resolved per bound
			// employee id; empty = no extension.
			guidedIntakePromptFor: deps.GuidedIntakePromptFor,
		},
		Progress: progressPublisherFunc(progressFn),
		Logger:   logger.With("component", "voice-binding"),
	})
	if err != nil {
		store.Close()
		return nil, err
	}

	w := &VoiceEmployeeWiring{
		Binding:       binding,
		store:         store,
		storePath:     deps.CallStorePath,
		bus:           deps.Bus,
		logger:        logger,
		ledgerStore:   deps.LedgerStore,
		displayLookup: deps.DisplayLookup,
		llm:           deps.BriefLLM,
		holds:         make(map[string]*sync.Mutex),
		heldCalls:     make(map[string]holdEntry),
		gates:         newHoldTracker(),
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
	text := renderCallRelayWithLookup(rec, lines, string(outcome), w.displayLookup, w.logger)
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
// (outcome, employee, leg, direction, caller display) followed by the
// transcript as "caller: ..." / "reply: ..." lines. Returns "" (notify
// nothing) when the transcript carries no rendered content. The leg
// derives from the stored target ("local:<label>" is the local leg;
// everything else is the phone leg) because the registry instance is
// already released when the relay runs.
//
// DISCLOSURE RULE (contact-display master O1/O4, leaf 02 §5): the header
// is an EGRESS surface. It NEVER embeds the raw stored number — the phone
// leg shows the boundary-resolved display form (name or "Unknown caller"
// plus the masked last-4 tail; the local leg keeps its digit-free
// "local:<label>" target verbatim). The transcript BODY is untouched:
// spoken digits inside conversation lines stay verbatim.
func renderCallRelay(rec voice.CallRecord, lines []voice.TranscriptLine, outcome string) string {
	return renderCallRelayWithLookup(rec, lines, outcome, nil, nil)
}

// renderCallRelayWithLookup is renderCallRelay with the contact-display
// resolver seam: a non-nil lookup resolves rec.Number to the Display form
// for the header; nil (or an unresolvable number) renders the
// unknown-caller + last-4 form, never the raw number.
func renderCallRelayWithLookup(rec voice.CallRecord, lines []voice.TranscriptLine, outcome string, lookup CallerDisplayLookup, logger *slog.Logger) string {
	leg := "phone"
	target := rec.Number
	if strings.HasPrefix(rec.Number, "local:") {
		leg = "local"
	} else {
		// Egress header: resolve through the boundary resolver (nil-safe
		// unknown path). The local-leg label above has no number in it.
		target = resolveDisplayFor(context.Background(), lookup, logger, rec.Number).Display
	}
	var b strings.Builder
	fmt.Fprintf(&b, "call ended (%s): employee %s | %s leg | %s | %s",
		outcome, rec.EmployeeID, leg, rec.Direction, target)
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
