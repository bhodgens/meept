package daemon

// Voice wiring (leaf 14): constructs the whole-daemon voice stack when
// [voice].enabled=true and wires it onto Components. The voice block in
// components.go calls WireVoice once every dependency it captures exists
// (msgBus, session store, agent loop manager, template loop, employee
// manager, effects ledger, tool registry); pointer-capture order is
// checked at the call site (repo defect class).
//
// Structure:
//   - ResolveVoiceSecrets: Broker.Value("telnyx_api_key") -> telnyx client
//     (in-process only; allowlisted in scripts/audit-secrets-value-callers.py).
//   - Talk loops: per-call goroutine (internal/voice.TalkLoop) consuming
//     final prompts -> binding RunTurn -> pipelined supervisor -> say.
//   - Release-hook fan-out: registry Release + binding OnCallEnd +
//     childproc NoteCallEnd on EVERY terminal outcome.
//   - SpliceRunner closure for the HTTP media route (leaf 06 RunSplice
//     with the per-call AudioPair).
//   - voice_call tool registration via the toolprov provider seam.
//   - Shutdown: child stopped before stores closed (Stop order).
//
// internal/daemon imports both internal/voice and internal/comm/http, so
// this is the one file allowed to bridge the cycle the leaf-10 NOTE
// describes (voice -> activity -> employee -> bot -> comm/http).

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/activity"
	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/auditlog"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/callerid"
	commhttp "github.com/caimlas/meept/internal/comm/http"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/contact"
	"github.com/caimlas/meept/internal/ledger"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/secrets"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/tools/builtin"
	"github.com/caimlas/meept/internal/voice"
	"github.com/caimlas/meept/internal/voice/telnyx"
)

// VoiceWiring is the constructed voice stack carried on Components. Every
// field is nil when the voice block is disabled.
type VoiceWiring struct {
	// Manager drives the call lifecycle (C5).
	Manager *voice.Manager
	// Registry tracks live calls (C8).
	Registry voice.Registry
	// Dialer places outbound calls (C9).
	Dialer *voice.Dialer
	// Binding is the employee phone binding (leaf 12).
	Binding *voice.Binding
	// ChildProc manages the meept-voice child (leaf 13).
	ChildProc *voice.ChildProc
	// TextServer is the C1 socket listener.
	TextServer *voice.TextServer
	// Routes serves the two Telnyx HTTP endpoints (leaf 10).
	Routes *commhttp.VoiceRoutes
	// Employee holds the goal hold seam + call store (leaf 12 wiring).
	Employee *VoiceEmployeeWiring
	// Local is the local voice leg (leaf 07). Nil when
	// voice.local.enabled is false - leaf 08's RPC refuses opens on nil
	// with the disabled-leg error.
	Local *voice.LocalLeg
	// Messaging is the Telnyx SMS webhook routes (leaf 12). Nil when
	// voice.messaging.enabled is false (the route stays unserved).
	Messaging *commhttp.MessagingRoutes

	// Signal is the armed Signal voice leg + its sidecar supervisor
	// (contact-channels leaf 11a T6). Nil when [signal].enabled is
	// false (clean disabled boot) OR the sidecar failed to arm (the
	// degrade posture: Signal call paths stay refused, boot proceeds).
	Signal *WireSignalLegResult

	// Contact is the employee contact chain (leaf 02). Nil when the
	// chain could not assemble (store failure) - the contact tool stays
	// unregistered and the feature stays refused (opt-in posture).
	Contact *ContactChainWiring

	// outcomes collects terminal call outcomes for the contact voice
	// attempts (leaf 02 T2): fed from the release hook (section 8).
	outcomes *voice.OutcomeRegistry

	// auditStore is the canonical audit chain (leaf 02's chain sinks).
	auditStore *auditlog.Store

	// supervisor is the C6 gate shared by all talk loops.
	supervisor voice.Supervisor

	// tel is the resolved telnyx seam (shared by manager + dialer +
	// hangup paths). The narrow CallController interface (not the
	// concrete client) keeps the wiring testable with a fake seam.
	tel telnyx.CallController

	// telClient is the CONCRETE client behind tel (leaf 02): the reaper's
	// standalone CallStatusChecker is satisfied by *telnyx.Client, which
	// the narrow controller interface does not carry. nil in test wirings
	// (a fake controller has no status authority; RunReaper's nil-Status
	// guard then keeps the reaper a logged no-op — exactly the
	// TestReaperWithoutCheckerDoesNothing contract).
	telClient *telnyx.Client

	// loops maps call_control_id -> live talk loop so the C1 frame
	// handler routes frames and teardown cancels the right goroutine.
	// loopCancels (same lock) holds the per-call context cancel armed at
	// loop setup (audit H4): releaseCall cancels it so a released call's
	// talk loop exits promptly instead of self-reaping after silence
	// timers.
	loopsMu     sync.Mutex
	loops       map[string]*voice.TalkLoop
	loopCancels map[string]context.CancelFunc

	// linkTurns is the half-duplex coordinator registry shared with the
	// LinkManager (voice_link_rpc.go passes it into LinkDeps.Turns): the
	// link's two legs pre-register by registry target BEFORE their leg
	// opens, so routeFrame resolves each setup'd link call to its
	// coordinator and constructs a turn-locked talk loop. Plain
	// (phone/local) calls never match a target and keep the legacy
	// uncoordinated construction byte-for-byte.
	linkTurns *voice.LinkTurnRegistry

	// callEvents is the operator-telemetry ring shared by every talk loop
	// (routeFrame arms TalkDeps.OnEvent from it) and read back over
	// voice.local.events (voice_local_rpc.go adapts it onto the seam).
	// Best-effort observability: append-only under its own mutex, FIFO
	// evicted per call id, NEVER consulted on any call-path decision.
	callEvents *voice.TelemetryRing

	// bus publishes voice.call.ended (typed topic); nil drops events.
	bus *bus.MessageBus

	// broker resolves the cloud STT/TTS keys at emission time (leaf 03,
	// C4). In-process Broker.Value consumers only - never a child env,
	// command line, or bus payload. nil degrades keys emission to empty
	// keys with a loud WARN (keys are still sent: the child factory fails
	// that call, the child and other calls stay up).
	broker *secrets.Broker

	// stopCtx/stopCancel bound the text server lifetime (set at Start).
	stopCtx    context.Context
	stopCancel context.CancelFunc
	// textSocketPath is the C1 socket path (kept for the Start readiness
	// probe and Stop cleanup).
	textSocketPath string

	// displayLookup is the contact-display boundary resolver seam
	// (contact-display tree leaf 01): resolveDisplay keys caller numbers
	// through it. Wired at construction (leaf 02) from
	// WireVoiceDeps.ContactDisplayStore via the sqliteDisplayLookup
	// adapter; nil (no contact store, or store open failed) keeps the
	// unknown-caller path — the resolver is nil-safe and never panics.
	// The caller-authz tree (O5) will satisfy the seam structurally with
	// the store's own LookupByPhone and retire the adapter.
	displayLookup CallerDisplayLookup

	// displayStore is the contacts.db handle behind displayLookup (leaf
	// 02). Owned (opened at the components.go voice block, closed at
	// Stop). nil when unwired.
	displayStore *contact.SQLiteStore

	// reaperCancel stops the dead-air reaper goroutine at shutdown (leaf
	// 02, T4). nil when the reaper never started (phone leg disabled, or
	// a negative config knob = explicitly off).
	reaperCancel context.CancelFunc

	// cfg + logger are construction-time values.
	cfg    config.VoiceConfig
	logger *slog.Logger

	// caller is the inbound caller-authz resolver slot (caller-authz
	// leaf 02). Nil resolver reads fail-soft to Info{Number} at the
	// answer site; components.go arms it with the contact-store resolver
	// right after BuildContactChain succeeds (the store handle does not
	// exist at WireVoice time - the chain is built over the live wiring).
	caller *callerResolverSlot

	// plainOpts configures the optional plain-HTTP loopback listener's
	// middleware (rate limit + access log parity with the main listener;
	// zero values take the server.go defaults). Set by WireVoice from the
	// transport.http block.
	plainOpts commhttp.PlainListenerOptions

	// plainServe is the plain-listener serve seam (bughunt D9 test
	// hook): production leaves it nil (the real commhttp listener);
	// tests inject an immediately-returning Serve to pin the
	// immediate-exit-is-fatal contract.
	plainServe func(ctx context.Context, addr string) error
}

// WireVoiceDeps carries the components WireVoice captures. All pointer
// fields are typed-nil guarded by the caller (components.go checks each
// before calling; WireVoice re-checks the load-bearing ones). The narrow
// interface shapes match the leaf-12 adapters, so the concrete components
// satisfy them structurally.
type WireVoiceDeps struct {
	Config   *config.Config
	Bus      *bus.MessageBus
	Sessions session.Store
	Loops    *agent.Manager
	Template *agent.AgentLoop
	Audit    *auditlog.Store
	Effects  voice.EffectsLedger
	Broker   *secrets.Broker
	// Employees resolves the bound employee's definition for the voice
	// turn runner (persona prompt + constitution stamping). Nil degrades
	// to the daemon template persona (the legacy behavior).
	Employees EmployeeDefinitionResolver
	// SupervisorChatter is the resolved fast-model client for the C6
	// gate (voice.supervisor_model alias). nil degrades the gate to
	// disabled (every sentence allowed) with a logged warning.
	SupervisorChatter voice.SupervisorChatter
	// SupervisorChatterClient resolves a *llm.Client for the
	// voice.supervisor_model ref (alias or provider/model) at
	// construction time (audit M1). Nil (or a failed resolution) falls
	// back to the components-injected SupervisorChatter - the legacy
	// classifier-chatter behavior. components.go passes a closure over
	// createAuxiliaryLLMClientWithResolver, the SAME resolution path
	// classifier chatter uses.
	SupervisorChatterClient func(modelRef string) *llm.Client
	// Day-brief sources for the local leg (leaf 07): the activity
	// aggregator adapts onto voice.ActivitySource, and the memory/goal
	// narrow interfaces are injected by components.go (the employee
	// framework owns both stores). Nil sources omit their brief section
	// (leaf-04 rule); Transcripts always comes from the employee stack's
	// call store, never nil here.
	ActivityAggregator *activity.Aggregator
	MemorySource       voice.MemorySource
	GoalSource         voice.GoalSource
	Logger             *slog.Logger
	// LedgerStore + BriefLLM (leaf 03-voice/01): the employee ledger
	// store (buildLedger) and the prompt-in/text-out brief LLM seam over
	// the daemon's EXISTING llm client. Either nil -> the per-call brief
	// at release no-ops (feature off by absence, no config knob in v1).
	LedgerStore *ledger.Store
	BriefLLM    func(prompt string) (string, error)
	// DayBriefBuilder (leaf 03-voice/03): set by WireVoice to the shared
	// day-brief closure (with the ledger open-commitments seam) when a
	// ledger store is present, so the signal leg's bindCall populates its
	// OnCallStart brief through the SAME builder the local/phone legs use.
	// Nil -> the signal leg keeps the empty-brief posture (nil store).
	DayBriefBuilder func(ctx context.Context, employeeID, number string) (voice.DayBrief, error)
	// ContactDisplayStore (contact-display leaf 02): the contacts.db
	// handle the boundary resolver keys caller numbers through. Owned by
	// the caller (components.go opens it beside the voice block, closes
	// via the wiring's displayStore reference at Stop). Nil -> the
	// displayLookup seam stays unwired and every surface renders the
	// unknown-caller form.
	ContactDisplayStore *contact.SQLiteStore
	// MomentResolver (plan 20261110-situational leaf 01-moment/02): the
	// per-employee "# Moment" renderer resolver from buildAwareness
	// (awareness_wiring.go), threaded to the voice turn runner so each
	// session clone gets the bound employee's renderer before its turn.
	// Nil (awareness disabled — the default) = the inert D1 posture.
	MomentResolver func(employeeID string) func() string
}

// WireVoice constructs the voice stack from cfg. It returns (nil, nil)
// when voice.enabled is false (the caller treats that as the disabled
// boot). A configuration error returns a named error: voice is opt-in, so
// a HALF-configured voice block must fail the boot loudly, never degrade
// to half-wired.
// Config returns the resolved [voice] config block (read-only; the
// guided-intake arming seam reads the employee_leg rows).
func (w *VoiceWiring) Config() config.VoiceConfig { return w.cfg }

func WireVoice(ctx context.Context, deps WireVoiceDeps) (*VoiceWiring, error) {
	cfg := deps.Config.Voice
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	vlog := logger.With("component", "voice")

	// -- 1. Telnyx client (PHONE leg). Broker.Value resolves the API token
	// in-process ONLY (audit-secrets-value-callers allowlist row); it reaches
	// the client constructor and nothing else - never a log line, env entry,
	// or child process. A missing broker/secret DEGRADES the PHONE leg to
	// disabled (nil carrier client; every phone call path fails closed at
	// dial/answer time) - scratch boots and broker-less installs still boot,
	// mirroring the wireSlackChannel optional-channel shape. It does NOT
	// disable the LOCAL leg: the local leg never imports telnyx or touches
	// the broker key (internal/voice/AGENTS.md invariant), so it arms
	// independently of the phone secret. Only when BOTH the phone token is
	// unresolvable AND voice.local.enabled=false is the whole block disabled
	// (the fully-disabled boot, unchanged posture).
	var tel *telnyx.Client
	if deps.Broker != nil {
		token, err := deps.Broker.Value("telnyx_api_key")
		if err != nil {
			vlog.Warn("voice wiring: telnyx_api_key unresolvable (secrets broker error); phone leg disabled, local leg unaffected",
				"err", err)
		} else {
			tel = telnyx.NewClient(telnyxBaseURL(), token)
		}
	} else {
		vlog.Warn("voice wiring: no secrets broker configured; telnyx_api_key unresolvable " +
			"(phone leg disabled, local leg unaffected; declare it under [secrets.sources] for phone calls)")
	}
	phoneEnabled := tel != nil
	if !phoneEnabled && !cfg.Local.Enabled {
		vlog.Warn("voice wiring degraded to disabled: no telnyx_api_key and voice.local.enabled=false; no voice leg can arm")
		return nil, nil //nolint:nilnil // deliberate: no voice leg can arm is a valid disabled deployment; callers (daemon.go boot) nil-check the wiring
	}

	// -- 2. Signature verifiers (leaf 02): mandatory for the PHONE routes;
	// an undecodable public key fails the boot. Only the phone leg needs
	// them (the local leg rides C2 audio pairs over SocketDir, never Telnyx
	// HTTP), so a local-only install skips this - it must not REQUIRE a
	// voice.telnyx_public_key.
	var verify commhttp.VerifyFuncs
	if phoneEnabled {
		pubKey, err := decodeTelnyxPublicKey(cfg.TelnyxPublicKey)
		if err != nil {
			return nil, err
		}
		verify = commhttp.VerifyFuncs{
			Webhook: func(body []byte, headers http.Header) error {
				return telnyx.VerifyWebhookSignature(body, headers, pubKey)
			},
			Media: func(headers http.Header, streamURL string) error {
				return telnyx.VerifyMediaSocket(headers, streamURL, pubKey)
			},
		}
	}

	// -- 3. C1 text server (shared by every call). Started at Start;
	// closed at Stop AFTER the child stops. The socket path derives from
	// SocketDir (C1 lives beside the per-call audio pairs).
	textSocketPath := filepath.Join(voice.SocketDir(), "text.sock")
	textServer := voice.NewTextServer(textSocketPath, vlog.With("sub", "text-socket"))

	// -- 4. Registry (C8) + call manager (C5). The manager's SessionSpawner
	// ensures the child is up before the call is answered; a spawn failure
	// refuses the call (ErrChildUnavailable), never hangs it.
	registry := voice.NewRegistry(cfg.MaxConcurrentCalls)
	child := voice.NewChildProc(voice.ChildProcConfig{
		BinaryPath:     cfg.ChildBinaryPath,
		TextSocketPath: textSocketPath,
		AudioDir:       voice.SocketDir(),
		OnnxLib:        cfg.OnnxRuntimeLib,
		IdleStop:       time.Duration(cfg.IdleStopSeconds) * time.Second,
		StartTimeout:   30 * time.Second,
		Handshake:      voice.HandshakeProbe(textServer),
		TokenAuth:      textServer,
		Live:           func() int { return len(registry.Live()) },
	}, vlog.With("sub", "child"), nil)
	// phoneHold is filled once the employee stack exists (below). The
	// spawn closure reads it at call time, never during WireVoice, so
	// the later assignment is visible. A nil read degrades to no
	// ended-state rather than panicking.
	var phoneHold *VoiceEmployeeWiring
	// Caller-authz slot (leaf 02): captured by the spawn closure below
	// (the inbound answer site) and handed to VoiceWiring at construction.
	// components.go arms the contact-store resolver into it once the
	// contact chain builds; a call that lands before arming (or with the
	// contact feature off) resolves fail-soft to Info{Number} — the call
	// still answers (O1).
	callerSlot := &callerResolverSlot{}
	spawn := func(callCtx context.Context, ev telnyx.CallEvent) (context.CancelFunc, error) {
		if err := child.EnsureRunning(callCtx); err != nil {
			return nil, err
		}
		// Acquire the C8 registry slot for THIS inbound call: the C1 setup
		// router resolves the talk loop through Registry.Live(), so a call
		// that skips Acquire can never arm its loop (leaf-15 e2e finding).
		// The employee is the configured default phone employee; outbound
		// dials Acquire through the dialer with the requesting employee.
		if cfg.DefaultEmployee == "" {
			return nil, fmt.Errorf("voice wiring: voice.default_employee is empty; " +
				"inbound calls have no owning employee (set the phone employee id)")
		}
		if _, err := registry.Acquire(ev.CallControlID, cfg.DefaultEmployee, ev.From); err != nil {
			return nil, fmt.Errorf("voice wiring: registry acquire: %w", err)
		}
		// Caller-authz (leaf 02): resolve the inbound caller ONCE here,
		// BEFORE holdPhoneCall arms the call row. The 2s budget bounds the
		// sqlite read; every failure mode (resolver not armed, store error,
		// expired budget) degrades to fail-soft Info{Number} with a WARN
		// and the call PROCEEDS — answering is never gated (O1). The
		// identity rides the registry row (SetCaller) so the setup router
		// hands the SAME instance (Caller populated) to the talk loop ->
		// bindingTurnRunner stamps it on every turn ctx. Registry fakes
		// without the SetCaller lane keep the zero Info (the legal
		// "unresolved caller" posture; TestBindingTurnRunnerStampsZeroCaller
		// pins the stamp's behavior there).
		//
		// Master Q2: the TURN-STAMP site (bindingTurnRunner) is shared
		// across legs, but this RESOLVE site is the phone leg's answer.
		// The signal leg's Instance.Caller stays zero — signal targets are
		// uuids, and contact resolution over signal_uuid is the flagged
		// fast-follow; enforcement (step 1c) treats an unresolved caller as
		// unapproved (O2 posture), never as an open gate.
		{
			resolveCtx, resolveCancel := context.WithTimeout(context.Background(), callerResolveBudget)
			caller := voice.ResolveCallerOrFailSoft(resolveCtx, callerSlot.get(), ev.From, vlog.With("ccid", ev.CallControlID))
			resolveCancel()
			registry.SetCaller(ev.CallControlID, caller)
		}
		// Goal-hold + call row (ended-state gap): same acquire the local
		// leg's Hold seam does. Release rides ReleaseHook exactly once;
		// do not keep AcquireCallHold's release func (a second release
		// would be a double-unlock without the once-guard, and the
		// contract is hook-only, matching the local wiring).
		//
		// audit 2026-11-09 finding 1: holdPhoneCall (AcquireCallHold ->
		// mu.Lock) can block a FULL prior goal round. It must never run
		// on this goroutine — HandleCallEvent rides the carrier webhook
		// handler, and a stall there buys a Telnyx redelivery storm on a
		// 500 after the leg was already answered. The spawn arms a
		// per-call hold gate and runs the hold + OnCallStart on its own
		// goroutine; the webhook returns immediately. Serialization of
		// the GOAL ROUND is unchanged: the talk loop's turn runner
		// awaits this same gate before the employee's FIRST turn
		// (holdReady), so the hold is still taken before any employee
		// state the call could race is touched. fact-free filler/
		// greeting lines may play while the gate pends (they read no
		// employee state; the supervisor-gate contract keeps `say`
		// gated on the supervisor, not on the goal hold).
		fresh := voice.FreshSessionFor(cfg, cfg.DefaultEmployee)
		if phoneHold != nil && phoneHold.gates != nil {
			gate := phoneHold.gates.arm(ev.CallControlID)
			go func() {
				defer gate.markReady()
				holdPhoneCall(phoneHold, ev.CallControlID, cfg.DefaultEmployee, ev.From,
					voice.DirectionIncoming, fresh, gate)
			}()
		} else {
			// Degraded posture (hand-built wirings without the tracker):
			// the pre-leaf synchronous hold — correct, just unbounded.
			holdPhoneCall(phoneHold, ev.CallControlID, cfg.DefaultEmployee, ev.From,
				voice.DirectionIncoming, fresh)
		}
		return func() {}, nil
	}
	manager := voice.NewManager(voice.ManagerConfig{
		StreamURL: cfg.PublicStreamURL,
		Codec:     telnyx.DefaultCodec,
	}, tel, spawn)

	// -- 5. Employee stack (leaf 12): call store + binding + goal hold.
	// The voice dir must exist before the sqlite store opens calls.db.
	if err := os.MkdirAll(config.MeeptPath("voice"), 0o755); err != nil {
		return nil, fmt.Errorf("voice wiring: voice dir: %w", err)
	}
	// Contact-display resolver seam (leaf 02): the components-injected
	// contacts.db handle adapts through sqliteDisplayLookup (O5 shim)
	// onto VoiceWiring.displayLookup AND the employee relay's seam, so
	// every egress surface (ended event, status rows, relay header)
	// resolves through the SAME boundary. Nil handle -> unwired (unknown-
	// caller path everywhere, nil-safe).
	displayLookup := newDisplayLookupSeam(deps.ContactDisplayStore)
	employee, err := WireVoiceEmployee(VoiceEmployeeDeps{
		Sessions:       deps.Sessions,
		LoopManager:    deps.Loops,
		TemplateLoop:   deps.Template,
		WorkingDir:     config.MeeptPath("voice"),
		Bus:            deps.Bus,
		Employees:      deps.Employees,
		CallStorePath:  filepath.Join(config.MeeptPath("voice"), "calls.db"),
		LedgerStore:    deps.LedgerStore,
		DisplayLookup:  displayLookup,
		BriefLLM:       deps.BriefLLM,
		MomentResolver: deps.MomentResolver,
		// C-8 (bughunt 2026-11-11): the voice clock stamp falls back to
		// the scheduler timezone (the same config the awareness lane's
		// moment block resolves via aw2SchedulerLoc) instead of the raw
		// daemon local zone.
		SchedulerTZ: deps.Config.Scheduler.Timezone,
		// Guided intake prompt seam (leaf 05, secretary-scoped): resolved
		// per bound employee id from the [voice.employee_leg] rows; an
		// employee whose row lacks guided_intake gets an empty extension.
		GuidedIntakePromptFor: func(employeeID string) string {
			return guidedIntakePromptExtension(cfg, employeeID)
		},
		Logger: vlog.With("sub", "employee"),
	})
	if err != nil {
		return nil, fmt.Errorf("voice wiring: employee stack: %w", err)
	}
	phoneHold = employee

	// -- 5b. Shared day-brief builder (leaf 03-voice/03): ONE builder
	// closure over the leaf-04 sources - the SAME sources the local leg
	// has always used - now extended with the ledger open-commitments
	// seam. Reused by: the local leg's DayBriefBuilder, the phone leg's
	// holdPhoneCall (via VoiceEmployeeWiring.dayBriefFn), and the signal
	// leg (via WireVoiceDeps.DayBriefBuilder). Phone/signal only build a
	// brief when the ledger store is present (nil store -> those leg
	// sites keep passing today's empty DayBrief — posture unchanged);
	// the local leg keeps its pre-existing always-build posture.
	buildCallBrief := func(ctx context.Context, employeeID, number string) (voice.DayBrief, error) {
		return voice.BuildDayBrief(ctx, voice.Deps{
			Activity:    voice.AggregatorSource{Agg: deps.ActivityAggregator},
			Transcripts: employee.store,
			Memory:      deps.MemorySource,
			Goals:       deps.GoalSource,
			Commitments: ledgerCommitmentsSource(deps.LedgerStore),
		}, employeeID, number, time.Now(), time.Local)
	}
	if deps.LedgerStore != nil {
		employee.dayBriefFn = buildCallBrief
		deps.DayBriefBuilder = buildCallBrief
	}

	// -- 6. Supervisor (C6). The fast-model seam honors
	// voice.supervisor_model (audit M1): when set, a dedicated client is
	// resolved for that alias/model through the same resolution path
	// components.go uses for classifier chatter. Empty model, no
	// factory, or failed resolution falls back to the components-built
	// classifier chatter (legacy behavior); nil chatter with
	// Enabled=true degrades the gate to allow-all with a logged warning.
	var supervisor voice.Supervisor
	chatter := deps.SupervisorChatter
	if cfg.SupervisorModel != "" {
		if deps.SupervisorChatterClient != nil {
			if dedicated := deps.SupervisorChatterClient(cfg.SupervisorModel); dedicated != nil {
				chatter = &chatterAdapter{client: dedicated}
				vlog.Info("voice wiring: supervisor gate resolved dedicated model",
					"supervisor_model", cfg.SupervisorModel)
			} else {
				vlog.Warn("voice wiring: voice.supervisor_model did not resolve to a model; "+
					"supervisor gate falls back to the classifier chatter chain",
					"supervisor_model", cfg.SupervisorModel)
			}
		} else {
			vlog.Warn("voice wiring: no client factory available for voice.supervisor_model; "+
				"supervisor gate falls back to the classifier chatter chain",
				"supervisor_model", cfg.SupervisorModel)
		}
	}
	if chatter != nil {
		supervisor = voice.NewSupervisor(voice.SupervisorConfig{
			Enabled:      cfg.SupervisorEnabled,
			Timeout:      time.Duration(cfg.SupervisorTimeoutMs) * time.Millisecond,
			FallbackLine: "I cannot confirm that yet.",
		}, chatter, vlog.With("sub", "supervisor"))
	} else {
		supervisor = voice.NewSupervisor(voice.SupervisorConfig{Enabled: false}, nil, vlog.With("sub", "supervisor"))
	}

	// -- 7. Dialer (C9) with the audit adapter. Phone-only: nil without a
	// telnyx key, so the voice_call tool refuses outbound dials (fail
	// closed) and the phone side stays visibly disabled while the local
	// leg lives.
	var dialer *voice.Dialer
	if phoneEnabled {
		var audit voice.AuditWriter
		if deps.Audit != nil {
			audit = auditWriterAdapter{store: deps.Audit}
		}
		dialer = voice.NewDialer(voice.DialConfig{
			Enabled:         true,
			AllowedNumbers:  cfg.AllowedNumbers,
			PublicStreamURL: cfg.PublicStreamURL,
			ConnectionID:    cfg.ConnectionID,
			FromNumber:      cfg.FromNumber,
			TimeoutSecs:     cfg.TimeoutSecs,
			// Outbound twin of LocalDeps.Hold. Nil is not used here:
			// the phone leg is armed, so the hold is wired. A nil
			// employee cannot happen (WireVoiceEmployee already
			// returned). Release is the manager hook, not this closure.
			Hold: func(ccid, emp string) {
				holdPhoneCall(employee, ccid, emp, livePhoneTarget(registry, ccid), voice.DirectionOutgoing,
					voice.FreshSessionFor(cfg, emp))
			},
		}, tel, manager, registry, deps.Effects, audit, vlog.With("sub", "dialer"))
	}

	w := &VoiceWiring{
		Manager:        manager,
		Registry:       registry,
		Dialer:         dialer,
		Binding:        employee.Binding,
		ChildProc:      child,
		TextServer:     textServer,
		Employee:       employee,
		supervisor:     supervisor,
		loops:          make(map[string]*voice.TalkLoop),
		loopCancels:    make(map[string]context.CancelFunc),
		linkTurns:      voice.NewLinkTurnRegistry(vlog.With("sub", "link-turns")),
		callEvents:     voice.NewTelemetryRing(voice.DefaultCallEvents, voice.DefaultCallEventRings),
		bus:            deps.Bus,
		textSocketPath: textSocketPath,
		broker:         deps.Broker,
		displayLookup:  displayLookup,
		displayStore:   deps.ContactDisplayStore,
		cfg:            cfg,
		logger:         vlog,
		caller:         callerSlot,
		plainOpts: commhttp.PlainListenerOptions{
			RateLimitPerMinute: deps.Config.Transport.HTTP.RateLimitRPM,
			RateLimitBurst:     deps.Config.Transport.HTTP.RateLimitBurst,
			TrustXForwardedFor: deps.Config.Transport.HTTP.TrustXForwardedFor,
			Logger:             vlog.With("sub", "voice-plain"),
		},
	}
	// The phone seam is an interface field; assigning a nil *Client here
	// would leave a non-nil interface wrapping a nil pointer (typed-nil
	// trap). Set it only when the phone leg actually armed so the
	// nil-carrier manager + nil-tel guards read the truth (phone leg
	// disabled) in the local-only boot.
	w.auditStore = deps.Audit
	if phoneEnabled {
		w.tel = tel
		w.telClient = tel
	}

	// -- 7a-b. Call recording (leaf 06, T3): consent gate evaluated at
	// STARTUP, never lazily at first call. record_calls=true with an
	// empty record_consent_pin is a refusal: Error log + recording forced
	// off (fail-closed — a silent record-without-consent is the one
	// outcome this gate exists to make impossible; all-party-consent
	// states). Recording rides the PHONE leg only: with the carrier
	// client unresolvable there is nothing to record, so the wiring
	// degrades to unwired (same nil-tel posture as the reaper).
	{
		policy := voice.RecordingPolicy{
			Enabled:          cfg.RecordCalls,
			ConsentAnnounced: cfg.RecordConsentPin != "",
		}
		switch {
		case policy.ConsentRefused():
			vlog.Error("voice wiring: record_calls enabled but record_consent_pin is empty; "+
				"recording FORCED OFF — set voice.record_consent_pin (the call must announce "+
				"recording and AI before any record_start)",
				"record_calls", true)
		case policy.Enabled && phoneEnabled:
			store := employee.store
			manager.ArmRecording(policy, tel, func(ctx context.Context, ccid, url string) error {
				return store.SetRecordingURL(ctx, ccid, url)
			})
			vlog.Info("voice wiring: call recording armed (consent pin set)",
				"format", "mp3", "channels", "dual")
		}
	}

	// -- 7b. LocalLeg (local-leg plan CL5/leaf 07): constructed when
	// voice.local.enabled; nil otherwise (leaf 08's RPC checks nil before
	// refusing with the disabled-leg error). The brief builder adapts the
	// leaf-04 BuildDayBrief over the day-brief sources - the SAME sources
	// the phone path would use, injected as a closure so internal/voice
	// stays dependency-free. The cancel-draft hook routes the splice's
	// barge-in seam to the wiring's talk loop (same HandleBargeIn the
	// media-side interrupt uses). --
	if cfg.Local.Enabled {
		local, lerr := voice.NewLocalLeg(voice.LocalLegConfig{
			Enabled:    true,
			Label:      cfg.Local.Name,
			BargeInRms: cfg.Local.BargeInRms,
		}, voice.LocalDeps{
			Registry:        registry,
			ChildProc:       child,
			TextServer:      textServer,
			Manager:         manager,
			Binding:         employee.Binding,
			DayBriefBuilder: buildCallBrief,
			Profile: func(employeeID string) voice.Profile {
				return voice.ResolveProfile(cfg, employeeID, "", voice.LegLocal)
			},
			// Fresh-session flag (fresh-session plan leaf 02): resolve
			// global voice.fresh_session_per_call + the employee_leg
			// override once per call, daemon-side, so the binding just
			// receives the computed bool (mirrors the Profile closure).
			FreshSession: func(employeeID string) bool {
				return voice.FreshSessionFor(cfg, employeeID)
			},
			// Goal-hold seam (AcquireCallHold gap closure): every opened
			// local call acquires the per-employee hold so the release
			// fan-out's ReleaseHook sees it in heldCalls and runs
			// Binding.OnCallEnd to mark the calls.db row 'ended'. The
			// release itself rides ReleaseHook -> ReleaseForCall at teardown;
			// this only acquires. Phone inbound/outbound use holdPhoneCall;
			// do not fold them into this closure (a second OnCallStart
			// on the local leg would duplicate the call row).
			Hold: func(ccid, emp string) {
				employee.AcquireCallHold(ccid, emp)
			},
			// Drain-driven floor (turn-taking leaf 02): the local leg
			// resolves each opened call's registry target against the
			// SAME coordinator registry routeFrame uses for talk loops
			// (link targets are pre-registered by LinkManager.Open
			// before Leg.Open). A link leg's splice gets the
			// AudioDrained dep -> the floor releases on the paced
			// tail's actual drain, not on turn_final alone. Plain
			// local calls never match -> nil dep -> byte-identical.
			LinkTurns: w.linkTurns,
			Logger:    vlog.With("sub", "local-leg"),
		})
		if lerr != nil {
			return nil, fmt.Errorf("voice wiring: local leg: %w", lerr)
		}
		local.SetCancelDraftHook(func(ccid string) {
			w.loopsMu.Lock()
			loop := w.loops[ccid]
			w.loopsMu.Unlock()
			if loop != nil {
				// Splice-side barge-in (local leg): same drop contract
				// as a child interrupt frame, minus the clear echo (the
				// splice itself sent clear toward the child).
				loop.HandleBargeIn(ccid)
			}
		})
		w.Local = local
		vlog.Info("voice wiring: local leg armed", "label", cfg.Local.Name)
	}

	// -- 7c. Signal leg (contact-channels leaf 11a T6): constructed when
	// [signal].enabled; nil otherwise (the disabled boot is clean - no
	// Leg, no supervisor, no warnings beyond the wiring's info line).
	// The leg rides the SAME registry/manager/text-server/binding as the
	// phone+local legs (one child, one registry, one call store), and
	// carries its own supervised signal-cli sidecar. Degrade posture
	// mirrors the phone leg's secrets gate: an unarmed sidecar logs a
	// loud WARN and leaves Signal call paths refused; the boot proceeds.
	if deps.Config.Signal.Enabled {
		sigRes, serr := WireSignalLeg(ctx, deps.Config.Signal, deps, registry, manager, textServer, employee.Binding, vlog.With("sub", "signal"))
		if serr != nil {
			return nil, fmt.Errorf("voice wiring: signal leg: %w", serr)
		}
		if sigRes != nil {
			w.Signal = sigRes
		}
	}

	// -- 8. Release-hook fan-out (the leaf's once-per-outcome contract).
	// Sinks: talk-loop teardown, registry Release, binding OnCallEnd (via
	// the leaf-12 ReleaseHook), childproc NoteCallEnd. Manager fires the
	// hook exactly once per ccid; each sink is idempotent under it.
	// Contact outcome collector (leaf 02): the voice attempts wait on
	// this map; the release hook is the single terminal-outcome source.
	w.outcomes = voice.NewOutcomeRegistry()
	manager.SetReleaseHook(func(ccid string, out voice.Outcome) {
		w.outcomes.Record(ccid, out)
		w.releaseCall(ccid, out)
	})

	// -- 8b. Structured call brief + commitments (leaf 04, C4/C7):
	// ADDITIVE observers on the answered + terminal lanes. The stub is
	// written at answer; the finalize (analyzer + idempotent store write
	// + C7 commitment filing) runs on a post-release goroutine, off the
	// hot path. The frozen releaseOnce funnel above is untouched. Fail-
	// soft throughout: nil analyzer or nil ledger store = stub stays /
	// filing skipped, never a release failure.
	wireBriefObservers(w, employee, manager, vlog.With("sub", "callbrief"))

	// -- 9. C1 frame router: frames from the child land on the right
	// call's talk loop; a setup frame registers a fresh loop.
	textServer.SetHandler(w.routeFrame)

	// -- 10. HTTP routes (leaf 10) with the splice runner closure.
	// Phone-only: without a telnyx key the Telnyx webhook/media endpoints
	// stay unserved, so no inbound phone call can ever reach the manager
	// (the phone leg stays fully disabled); the local leg needs no HTTP
	// route (it rides C2 audio pairs over SocketDir).
	if phoneEnabled {
		runner := w.spliceRunner()
		routes, err := commhttp.NewVoiceRoutes(manager, verify, commhttp.VoiceRoutesConfig{
			Enabled:      true,
			SpliceRunner: runner,
		}, vlog.With("sub", "routes"))
		if err != nil {
			if closeErr := employee.Close(); closeErr != nil {
				w.logger.Debug("voice wiring: employee close", "err", closeErr)
			}
			return nil, fmt.Errorf("voice wiring: routes: %w", err)
		}
		w.Routes = routes
	}

	// -- 10b. Messaging webhook routes (leaf 12): the SAME Ed25519
	// verifier as the voice routes (injected; no second crypto impl).
	// OnFinalized feeds the contact chain's SMS attempt DLR wait; the
	// wiring arms it after the chain exists (BuildContactChain sets
	// Messaging.OnFinalized through the exported seam). Disabled block
	// leaves the route unserved.
	if deps.Config.Voice.Messaging.Enabled {
		msgDeps := commhttp.MessagingWebhookDeps{
			Verify: verify.Webhook,
		}
		mr, err := commhttp.NewMessagingRoutes(msgDeps, vlog.With("sub", "messaging"))
		if err == nil {
			w.Messaging = mr
		} else {
			vlog.Warn("voice wiring: messaging routes degraded (route unserved)", "error", err)
		}
	}

	return w, nil
}

// telnyxBaseURL returns the API origin for telnyx.NewClient. Empty uses
// the telnyx default. The MEEPT_TELNYX_TEST_BASE_URL override exists for
// hermetic scratch-daemon gates (leaf 14 integration step 4); it is read
// from the process environment at boot, never from config, so a test
// override cannot leak into a production deployment. A plain env override,
// not a secret.
func telnyxBaseURL() string {
	return os.Getenv("MEEPT_TELNYX_TEST_BASE_URL")
}

// decodeTelnyxPublicKey decodes the base64 Ed25519 public key.
func decodeTelnyxPublicKey(b64 string) (ed25519.PublicKey, error) {
	if b64 == "" {
		return nil, fmt.Errorf("voice wiring: voice.telnyx_public_key is empty; routes would fail closed (configure the key from Telnyx Mission Control)")
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("voice wiring: voice.telnyx_public_key: %w", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("voice wiring: voice.telnyx_public_key: got %d bytes, want %d", len(raw), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(raw), nil
}

// auditWriterAdapter adapts *auditlog.Store onto voice.AuditWriter (C9).
type auditWriterAdapter struct {
	store *auditlog.Store
}

// WriteAudit implements voice.AuditWriter. One canonical row per dial
// attempt, written BEFORE the POST by the dialer's own order.
//
// bughunt D10 (disclosure): the dialer hands this adapter the FULL
// normalized E.164 target, and the naive append put it verbatim into the
// hash chain — a permanent egress surface the tree's disclosure rule
// (contact-display master O1/O4, pinned here by publishCallEnded's
// CallerDisplay contract) restricts to contact name + last-4. The
// payload is sanitized BEFORE the append: `target` becomes the masked
// last-4 form ("•••NNNN"), `target_e164_len` records the digit count so
// the row stays attributable without carrying the number. The chain hash
// covers the sanitized payload, so the full number is unrecoverable from
// the stored row by construction.
func (a auditWriterAdapter) WriteAudit(ctx context.Context, row map[string]any) error {
	sanitized := make(map[string]any, len(row)+1)
	for k, v := range row {
		sanitized[k] = v
	}
	if target, ok := row["target"].(string); ok && target != "" {
		digits := dialAuditDigits(target)
		sanitized["target"] = dialAuditMaskedTarget(target)
		sanitized["target_e164_len"] = len(digits)
	}
	rec := auditlog.Record{
		Type:    "voice.dial",
		Payload: sanitized,
	}
	if emp, ok := row["employee_id"].(string); ok {
		rec.EmployeeID = emp
	}
	_, err := a.store.Append(ctx, rec)
	return err
}

// dialAuditDigits returns the digit-only content of a target number.
func dialAuditDigits(target string) string {
	digits := make([]rune, 0, len(target))
	for _, r := range target {
		if r >= '0' && r <= '9' {
			digits = append(digits, r)
		}
	}
	return string(digits)
}

// dialAuditMaskedTarget renders the operator-safe last-4 form of a
// target number ("•••5567"); fewer than five digits renders the whole
// digit tail (a non-sensitive degenerate case the display resolver's
// short-number rule shares).
func dialAuditMaskedTarget(target string) string {
	digits := dialAuditDigits(target)
	if len(digits) <= 4 {
		return digits
	}
	return "•••" + digits[len(digits)-4:]
}

// routeFrame is the C1 frame router (TextServer handler). Setup frames
// create a talk loop per call; prompt/interrupt/end frames route to the
// loop registered for that call_control_id.
func (w *VoiceWiring) routeFrame(f voice.Frame) {
	switch f.Event {
	case voice.EventPlayed:
		// Child -> daemon end-of-turn AUDIO marker (turn-final drain fix):
		// the final byte of a last:true say stream left the child's .out
		// socket. Arm the live local splice's drain watch from this edge
		// (played-armed mode: nothing more can arrive for the turn, so the
		// fire is pending==0 + the SHORT final quiet window). The frame is
		// CONSUMED here — it never reaches the talk loop's frame queue.
		// No live splice (phone leg, plain local without the seam, torn
		// down) or a splice without the AudioDrained dep: silent no-op;
		// plain calls never gate a floor on this edge.
		if sp := voice.LocalSpliceFor(f.CallID); !sp.MarkFinalPlayed() {
			w.logger.Debug("voice wiring: played edge with no live drain watch",
				"ccid", f.CallID)
		}
	case voice.EventSetup:
		// bughunt 2026-11-07 voice F-6: idempotent setup, mirroring the
		// child's handleSetup ("setup for live call ignored"). Without the
		// guard a duplicate setup (child reconnect/replay) overwrote the
		// loop + loopCancel map entries: the replaced loop kept running
		// unreached (router looks up by map), received no frames, and its
		// silence timer eventually fired t.end -> managerHangup on the
		// CURRENT live call. The lost cancel func also leaked the ctx.
		//
		// bughunt A10: the ORIGINAL shape checked-then-inserted in TWO
		// separate critical sections. A hangup (releaseCall drops the map
		// entry + registry row) between them found no loop entry and the
		// setup armed a ZOMBIE talk loop on a dead call — the loop's
		// silence timers then fired managerHangup on the released id and
		// its registry row was already gone. The fix: build the loop and
		// resolve the instance OUTSIDE the lock, then make
		// check + insert + liveness-recheck ONE critical section. The
		// re-check consults the registry (releaseCall's Release runs
		// under the registry's own mutex, so a released call is absent
		// from Live()); a zombie arming is refused and the loop is never
		// registered, started, or sent keys.
		//
		// Setup carries the call identity the talk loop needs. The
		// registry instance was acquired at answer/dial time; find it.
		var inst voice.Instance
		for _, live := range w.Registry.Live() {
			if live.CallControlID == f.CallID {
				inst = live
				break
			}
		}
		if inst.CallControlID == "" {
			w.logger.Warn("voice wiring: setup for unknown call", "ccid", f.CallID)
			return
		}
		// Link half-duplex (turn-taking leaf): a link leg's registry
		// target resolves to its coordinator (pre-registered by
		// LinkManager.Open BEFORE the leg opened). Bound loops carry the
		// HoldPrompt/TurnState seams and side B suppresses its greeting
		// (single-greeting contract). A nil lookup = phone/plain-local =
		// the legacy uncoordinated construction, seams nil.
		coord, turnSide := w.linkTurns.Lookup(inst.Target)
		var holdPrompt func(voice.Instance, func()) bool
		var turnState func(ccid string, seq uint64, kind string)
		suppressGreeting := false
		if coord != nil {
			holdPrompt = coord.HoldPromptFor(turnSide)
			turnState = coord.TurnStateFor(turnSide)
			suppressGreeting = coord.SuppressesGreeting(turnSide)
		}
		loop := voice.NewTalkLoop(voice.TalkDeps{
			Supervisor: w.supervisor,
			Turns:      turnRunnerFor(w.Employee.Binding, w.Employee.gates, inst),
			SessionFor: func(call voice.Instance) voice.SessionHandle {
				return w.sessionFor(call)
			},
			Binding:    w.Employee.Binding,
			Send:       textFrameSender{srv: w.TextServer}.Send,
			OnEnd:      func(ccid, reason string) { w.managerHangup(ccid, reason) },
			Logger:     w.logger.With("sub", "talk", "ccid", f.CallID),
			HoldPrompt: holdPrompt,
			TurnState:  turnState,
			// Operator telemetry (voice-events): append the loop's
			// dispatch/final/barge-in/ended events to the shared per-call
			// ring, polled via voice.local.events. Pure bookkeeping;
			// never gates the loop (the seam is nil-safe in voice).
			OnEvent: w.appendCallEvent,
		}, voice.TalkLoopConfig{
			FillerLines:               w.cfg.Filler,
			GreetingLine:              w.cfg.GreetingLine,
			FallbackLine:              "I cannot confirm that yet.",
			MaxCallMinutes:            w.cfg.MaxCallMinutes,
			SilencePromptSeconds:      w.cfg.SilencePromptSeconds,
			SilencePromptLine:         w.cfg.SilencePrompt,
			SilenceHangupAfterSeconds: w.cfg.SilenceHangupAfterSeconds,
			TurnTimeoutSeconds:        w.cfg.TurnTimeoutSeconds,
			FillerGrace:               time.Duration(w.cfg.FillerGraceMs) * time.Millisecond,
			SuppressGreeting:          suppressGreeting,
		})
		// H4 + A10: arm the per-call context and install the loop in ONE
		// critical section. releaseCall cancels the ctx so a released
		// call's talk loop exits promptly instead of self-reaping after
		// silence timers; loopContext owns the daemon-lifecycle parent.
		// The registry re-check inside the SAME section closes the A10
		// window: a releaseCall that ran (registry row dropped) before we
		// took loopsMu refuses the arming outright — no zombie loop, no
		// keys frame, no goroutine.
		w.loopsMu.Lock()
		if _, exists := w.loops[f.CallID]; exists {
			w.loopsMu.Unlock()
			w.logger.Debug("voice wiring: setup for live call ignored", "ccid", f.CallID)
			return
		}
		stillLive := false
		for _, live := range w.Registry.Live() {
			if live.CallControlID == f.CallID {
				stillLive = true
				break
			}
		}
		if !stillLive {
			w.loopsMu.Unlock()
			w.logger.Warn("voice wiring: setup raced a hangup; loop not armed",
				"ccid", f.CallID)
			return
		}
		w.loops[f.CallID] = loop
		loopCtx, loopCancel := context.WithCancel(w.loopContext(f.CallID))
		w.loopCancels[f.CallID] = loopCancel
		w.loopsMu.Unlock()
		if coord != nil {
			coord.Bind(turnSide, loop)
		}
		// Leaf 03 (C4): the extended keys frame goes out right after the
		// loop registers, once per call, on the setup goroutine. The
		// idempotency guard above returned on duplicate setups before
		// reaching this line, so a replayed setup never re-sends keys;
		// the unknown-ccid and raced-hangup returns also precede it
		// (refused setups send nothing). Send failure degrades inside
		// sendKeysFrame (warn, no teardown).
		w.sendKeysFrame(f.CallID)
		go func() {
			// Talk loop lives for the call; Run returns at end (end
			// frame, timers) or when releaseCall cancels loopCtx. Run's
			// own error paths log; the goroutine cannot surface further.
			if runErr := loop.Run(loopCtx, inst); runErr != nil {
				w.logger.Debug("voice wiring: talk loop ended", "ccid", f.CallID, "err", runErr)
			}
			// Own the cancel either way: a self-ended loop releases its
			// context slot; a released loop was already cancelled.
			loopCancel()
			if coord != nil {
				// Half-duplex: the side is gone - release the peer's
				// parked prompt (it can no longer be answered by this
				// side going idle) and drop the dead loop reference.
				coord.Unbind(turnSide)
			}
			w.loopsMu.Lock()
			delete(w.loops, f.CallID)
			delete(w.loopCancels, f.CallID)
			w.loopsMu.Unlock()
		}()
		w.logger.Info("voice wiring: talk loop armed", "ccid", f.CallID, "employee_id", inst.EmployeeID)
	default:
		// turn-2 leaf 01: the local leg's AUDIO-plane barge-in trip.
		// The child's interrupt frame historically only reached the talk
		// loop (control plane); the live local splice was never latched,
		// so up to 64 already-read child chunks kept pacing the old
		// answer's tail toward the client. Latch + drain the splice's
		// outbound pump HERE, BEFORE the frame enters the loop queue
		// (the queue wait can be seconds; the resume on the next say
		// would otherwise pace the stale backlog out first). Phone legs
		// have no local splice (nil receiver -> no-op); a second trip
		// while latched is a no-op. The frame is still forwarded below:
		// the talk loop keeps sole ownership of the control plane
		// (draft cancel, clear frame to the child, binding publish,
		// telemetry) — the latch here emits none of those.
		if f.Event == voice.EventInterrupt {
			voice.LocalSpliceFor(f.CallID).LatchBargeIn()
		}
		w.loopsMu.Lock()
		loop := w.loops[f.CallID]
		w.loopsMu.Unlock()
		if loop == nil {
			// Frames for a call without a loop (setup lost, or teardown
			// already ran): prompt/interrupt are dropped with a log; end
			// is a no-op (the manager owns teardown idempotency).
			w.logger.Debug("voice wiring: frame for call without talk loop", "ccid", f.CallID, "event", f.Event)
			return
		}
		// Operator telemetry (voice-events): a FINAL prompt frame landing
		// here is the mic-speech evidence - the transcript reached the
		// daemon BEFORE the talk loop's dispatch of the same text. Partials
		// (last != true) never record: they are preview traffic, and the
		// ring's "heard" line is the operator's "speech detected" signal.
		// The append rides the SUCCESSFUL enqueue (C-13): a frame the
		// full-queue default branch DROPS never reached the loop, so it
		// must never be recorded as heard - the telemetry would lie about
		// delivery. The enqueue itself stays non-gating (never blocks).
		if f.Event == voice.EventPrompt && f.Last != nil && *f.Last {
			// Dead-air reaper refresh (leaf 02): a caller-side turn event
			// resets that call's dead-air clock. The manager fans this to
			// the additive activity observers (one nil-check when no
			// reaper runs); the notify itself is pure bookkeeping.
			w.Manager.NotifyCallActivity(f.CallID)
			select {
			case loop.Frames() <- f:
				w.callEvents.Append(f.CallID, voice.EventInput{
					Kind:  voice.EventKindHeard,
					Text:  voice.PreviewEventText(f.From),
					Words: len(strings.Fields(f.From)),
				})
			default:
				w.logger.Warn("voice wiring: talk loop frame queue full", "ccid", f.CallID, "event", f.Event)
			}
			return
		}
		// Leaf 03 (T3 raw-capture): a FINAL prompt's From field carries
		// the RAW (un-normalized) transcript. The talk loop must dispatch
		// the NORMALIZED Text (that is what the LLM turn runs on), so the
		// frame is rewritten here with raw text in From, normalized in
		// Text — the binding's RunTurn then appends the RAW text to the
		// call transcript (AppendTranscript keeps RAW) while the turn
		// prompt stays normalized. Partials keep Text as-is (preview
		// traffic) and carry no From payload.
		if f.Event == voice.EventPrompt && f.Last != nil && *f.Last && f.From != "" && f.From != f.Text {
			f.Text, f.From = f.From, ""
		}
		select {
		case loop.Frames() <- f:
		default:
			w.logger.Warn("voice wiring: talk loop frame queue full", "ccid", f.CallID, "event", f.Event)
		}
	}
}

// sessionFor resolves the bound meept session for a call. The binding owns
// per-call state; the session handle comes from its resolve path via the
// call store's session id. A miss returns an empty handle (the turn runner
// errors with a nameable message, the call never panics).
func (w *VoiceWiring) sessionFor(call voice.Instance) voice.SessionHandle {
	if sid, ok := w.Employee.store.SessionIDFor(call.Target, call.EmployeeID); ok && sid != "" {
		return voice.SessionHandle{ID: sid, ConversationID: sid}
	}
	return voice.SessionHandle{}
}

// loopContext returns the parent context for one call's talk loop: the
// daemon lifecycle context cancels every loop at shutdown. The per-call
// cancellation lives in loopCancels (armed in routeFrame, fired by
// releaseCall — audit H4).
func (w *VoiceWiring) loopContext(string) context.Context {
	if w.stopCancel != nil && w.stopCtx != nil {
		return w.stopCtx
	}
	return context.Background()
}

// managerHangup is the talk loop's OnEnd seam: the loop decided the call is
// over (max duration, silence, child end). The manager owns idempotent
// teardown; a plain hangup on a tracked id releases everything.
func (w *VoiceWiring) managerHangup(ccid, reason string) {
	// The carrier POST is best-effort and phone-led only: a nil seam (the
	// local-only boot with no telnyx key) skips it entirely - the manager
	// already owns teardown state. Without the guard, a self-reaped LOCAL
	// call (silence/max-call) whose talk loop fires managerHangup would
	// nil-deref the phone seam.
	if w.tel != nil {
		if err := w.tel.HangupCall(context.Background(), ccid); err != nil {
			// Best-effort carrier hangup; the manager already owns teardown
			// state. A failed POST leaves the leg to Telnyx's own timeout.
			w.logger.Debug("voice wiring: carrier hangup", "ccid", ccid, "err", err)
		}
	}
	w.Manager.FailOutcome(ccid, voice.OutcomeHangup, reason)
}

// spliceHangup is the splice's HangupFn seam (backpressure grace exceeded).
// It mirrors managerHangup (audit M4): the carrier POST kills the live
// phone leg, then FailOutcome runs the once-per-outcome release fan-out.
// The POST is best-effort — a failed call leaves the leg to Telnyx's own
// timeout while the daemon-side teardown still completes.
func (w *VoiceWiring) spliceHangup(ccid, reason string) {
	w.logger.Info("voice wiring: splice hangup", "ccid", ccid, "reason", reason)
	// The POST is best-effort and phone-led only; a nil seam (local-only
	// boot, no telnyx key) skips the carrier POST while the daemon-side
	// teardown still completes.
	if w.tel != nil {
		if err := w.tel.HangupCall(context.Background(), ccid); err != nil {
			w.logger.Debug("voice wiring: splice carrier hangup", "ccid", ccid, "err", err)
		}
	}
	w.Manager.FailOutcome(ccid, voice.OutcomeFailed, reason)
}

// holdPhoneCall registers the goal hold and starts the calls.db row for a
// live phone leg (inbound answer and outbound dial). Release is NOT taken
// here: Manager release hook -> ReleaseHook -> ReleaseForCall unlocks the
// per-employee mutex exactly once. Keeping AcquireCallHold's returned
// release func and also running the hook would be a second release attempt;
// the map-delete once-guard makes that a no-op, but the contract is
// hook-only, matching the local leg's Hold seam.
//
// Nil employee degrades to no ended-state (the pinned posture). OnCallStart
// failure is logged and does not fail the call (local-leg posture): without
// a live binding entry, OnCallEnd is a no-op and the row cannot reach ended,
// so the start has to land here — the phone leg had no other OnCallStart
// call site.
//
// fresh (fresh-session plan leaf 02): the caller resolves the per-call
// fresh-session flag from the voice config (voice.FreshSessionFor) and
// passes it through to the binding; false is byte-identical to the
// pre-leaf-02 shared-session behavior.
//
// gate (audit 2026-11-09 finding 1, optional variadic): set only by the
// ASYNC inbound path — the webhook spawn goroutine passes the call's hold
// gate. AcquireCallHold may block here for a FULL prior goal round; that
// is by design on this goroutine (not the webhook's). After the hold
// registers, the gate's released flag is re-checked: if the call
// terminal-fired while the hold pended, releaseCall's gate drop already
// ran BEFORE the ReleaseHook peek, so the hook saw an unregistered hold
// and skipped it — this side rolls the registration back instead
// (ReleaseForCall + OnCallEnd). Both sides are once-guarded (map delete +
// binding live check), so an interleaving where the hook DID see the
// registration makes the rollback a clean no-op. No gate = today's
// synchronous posture with no rollback check.
func holdPhoneCall(emp *VoiceEmployeeWiring, ccid, employeeID, number, direction string, fresh bool, gate ...*holdGate) {
	if emp == nil || ccid == "" || employeeID == "" {
		return
	}
	// NB: mu.Lock() below is the ONE place a long unbounded block is
	// legitimate on this stack — callers guarantee it is either the
	// webhook's decoupled goroutine (gate variant) or the dialer's own
	// goroutine (outbound). never hold any per-wiring lock across it
	// (mutexio).
	emp.AcquireCallHold(ccid, employeeID)
	if len(gate) > 0 && gate[0] != nil && emp.Binding != nil {
		if released, outcome := gate[0].releaseState(); released {
			// The call died while this hold pended and releaseCall's
			// hook-side teardown already skipped the (then-unregistered)
			// hold: no OnCallStart ran, so only the registration needs
			// rolling back. OnCallEnd is no-op-safe without a live
			// binding entry; run it anyway for the pathological
			// ordering where the start landed first.
			if emp.logger != nil {
				emp.logger.Info("voice wiring: phone hold rolled back — call released while the hold pended",
					"ccid", ccid, "outcome", outcome)
			}
			rollbackCtx, rbCancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = emp.Binding.OnCallEnd(rollbackCtx, ccid, outcome)
			rbCancel()
			emp.ReleaseForCall(ccid)
			return
		}
	}
	if emp.Binding == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Day brief (leaf 03-voice/03): built through the wiring's shared
	// builder ONLY when the ledger store armed it (nil dayBriefFn = no
	// store = today's empty-brief posture, byte-identical). A build
	// failure is fail-soft: the call proceeds with an empty brief —
	// teardown/call paths never depend on ledger health.
	brief := voice.DayBrief{}
	if emp.dayBriefFn != nil {
		if b, err := emp.dayBriefFn(ctx, employeeID, number); err != nil {
			if emp.logger != nil {
				emp.logger.Warn("voice wiring: phone day brief build failed (continuing with an empty brief)",
					"ccid", ccid, "error", err)
			}
		} else {
			brief = b
		}
	}
	_, err := emp.Binding.OnCallStart(ctx, ccid, number, employeeID, direction, brief, fresh)
	if err != nil && emp.logger != nil {
		emp.logger.Warn("voice wiring: phone OnCallStart failed (call continues unbound)",
			"ccid", ccid, "direction", direction, "error", err)
	}
	// Second gate re-check AFTER the start (closes the narrow window where
	// releaseCall's drop + hook-side OnCallEnd landed between the pre-check
	// and b.calls registration — the hook would have skipped the then-
	// unbound row, and without this check the row strands 'live'). Both
	// rollback sinks are idempotent (heldCalls map-delete once-guard,
	// binding live check), so racing the hook again is a clean no-op.
	if len(gate) > 0 && gate[0] != nil && emp.Binding != nil {
		if released, outcome := gate[0].releaseState(); released {
			if emp.logger != nil {
				emp.logger.Info("voice wiring: phone call row rolled back — released during hold start",
					"ccid", ccid, "outcome", outcome)
			}
			endCtx, endCancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = emp.Binding.OnCallEnd(endCtx, ccid, outcome)
			endCancel()
			emp.ReleaseForCall(ccid)
		}
	}
}

// livePhoneTarget returns the registry target for ccid, or "" when the
// slot is already gone. Outbound dial Acquires before Hold, so the
// normalized target is present for OnCallStart.
func livePhoneTarget(reg voice.Registry, ccid string) string {
	if reg == nil || ccid == "" {
		return ""
	}
	for _, inst := range reg.Live() {
		if inst.CallControlID == ccid {
			return inst.Target
		}
	}
	return ""
}

// appendCallEvent is the talk loop's OnEvent seam (voice-events operator
// telemetry): the shared per-call ring append. Nil-safe (a nil ring drops
// the event); pure bookkeeping, never I/O (mutexio-safe).
func (w *VoiceWiring) appendCallEvent(ccid string, in voice.EventInput) uint64 {
	return w.callEvents.Append(ccid, in)
}

// releaseCall is the Manager release hook fan-out. Order matters for
// observability, not correctness (each sink is once-guarded):
//  1. hold-gate drop (audit 2026-11-09 finding 1): mark the pending-hold
//     gate released and remove it BEFORE the ReleaseHook peek, so exactly
//     one side (the hook, or the hold goroutine's rollback) tears the
//     registration down when a hold was still pending on the mutex
//  2. C1 end frame (audit 2026-11-09 finding 10): the child learns the
//     call ended through the frame protocol, best-effort BEFORE the talk
//     loop is dropped and the transport closes
//  3. childproc NoteCallEnd (idle timer accounting)
//  4. talk-loop cancel (per-call ctx, audit H4 — the loop's Run select
//     sees ctx.Done and exits promptly, cancelling its in-flight turn
//     pipeline)
//  5. registry Release (frees the C8 slot + target)
//  6. binding OnCallEnd (leaf-12 ReleaseHook: call row + goal hold)
func (w *VoiceWiring) releaseCall(ccid string, outcome voice.Outcome) {
	if w.Employee != nil && w.Employee.gates != nil {
		w.Employee.gates.drop(ccid, string(outcome))
	}
	// Guided intake (leaf 05): the call's confirm state dies with the
	// call — a replayed outcome or a new call on a recycled ccid can
	// never inherit a confirmation.
	clearIntakeSession(ccid)
	w.sendEndFrame(ccid, outcome)
	w.ChildProc.NoteCallEnd(ccid)

	w.loopsMu.Lock()
	loop := w.loops[ccid]
	cancel := w.loopCancels[ccid]
	delete(w.loops, ccid)
	delete(w.loopCancels, ccid)
	w.loopsMu.Unlock()
	// Operator telemetry (voice-events): the call_ended line normally
	// comes from the talk loop's own end() (rich reason, endOnce-guarded).
	// A call that never armed a loop AND never recorded anything (a dial
	// failure before setup, a no-answer reaper) gets its terminal here
	// instead, so every ring-visible call has a named end. The probe is
	// HasRing (C-20): "no ring" must mean never-created, not FIFO-EVICTED
	// - a Since/!found probe resurrects an evicted call's ring with a
	// synthetic call_ended, inflating the live-ring map with dead calls.
	if loop == nil && !w.callEvents.HasRing(ccid) {
		w.callEvents.Append(ccid, voice.EventInput{
			Kind:   voice.EventKindCallEnded,
			Reason: string(outcome),
		})
	}
	if loop == nil {
		// The loop's frames channel is not closed here (close would race
		// a concurrent routeFrame send); the drop from the map stops
		// delivery, and the loop's own end path reaps it. Prompt frames
		// after release are dropped by the router's unknown-call branch.
	} else {
		w.logger.Debug("voice wiring: talk loop dropped from map on release", "ccid", ccid)
	}
	// H4: cancel the per-call context AFTER the map drop. The loop's
	// deferred cleanup re-runs cancel (idempotent) when Run returns; a
	// cancel racing loop setup is impossible — releaseCall only sees a
	// ccid after the manager tracks the call, and the loop arms its ctx
	// at the setup frame, which strictly precedes any terminal event.
	if cancel != nil {
		cancel()
	}

	w.Registry.Release(ccid)
	w.Employee.ReleaseHook(ccid, outcome)
	w.publishCallEnded(ccid, outcome)
}

// sendEndFrame ships the C1 end frame (finding 10: teardown parity — the
// child used to learn release only through socket EOF; internal/voicechild
// handleEnd tears the pipeline down on this frame). Frame contract per
// internal/voice/AGENTS.md: byte-exact JSON line, call_id carried, `last`
// OMITTED for end (nil pointer, not explicit false). Reason carries the
// terminal outcome so child logs name why the call ended.
//
// Best-effort by contract: TextServer.Send fails fast when no child is
// connected (it never queues, unlike SendRaw's child-readiness wait), and
// a write error means the transport is already dead — either way the
// failure is logged and the release fan-out continues. It must NEVER
// block or fail teardown: socket EOF remains the fallback signal.
func (w *VoiceWiring) sendEndFrame(ccid string, outcome voice.Outcome) {
	if w.TextServer == nil || ccid == "" {
		return
	}
	if err := w.TextServer.Send(voice.Frame{
		CallID: ccid,
		Event:  voice.EventEnd,
		Reason: string(outcome),
	}); err != nil {
		w.logger.Debug("voice wiring: end frame not delivered (child gone; socket EOF is the fallback)",
			"ccid", ccid, "outcome", string(outcome), "err", err)
	}
}

// publishCallEnded publishes voice.call.ended (typed topic, WS class
// event, payload {call_id, employee_id, caller_name, caller_display,
// direction, outcome, duration_ms}). The registry's live snapshot supplies
// the identity fields; a released ccid is gone from it, so the payload
// degrades to call_id + outcome (never an error). Best-effort: a nil bus
// drops it. DISCLOSURE (contact-display O1/O4): the raw caller number is
// NEVER published on this egress surface — the boundary resolver renders
// the contact name (CallerName) plus the operator-safe last-4 display form
// (CallerDisplay); an unknown or unresolved number still carries at most
// its last four digits.
func (w *VoiceWiring) publishCallEnded(ccid string, outcome voice.Outcome) {
	if w.Employee == nil {
		return
	}
	ev := CallEndedEvent{CallID: ccid, Outcome: string(outcome)}
	for _, inst := range w.Registry.Live() {
		if inst.CallControlID == ccid {
			ev.EmployeeID = inst.EmployeeID
			info := w.resolveDisplay(context.Background(), inst.Target)
			ev.CallerName = info.Name
			ev.CallerDisplay = info.Display
			break
		}
	}
	if w.bus != nil {
		bus.PublishT(w.bus, TopicVoiceCallEnded, "voice", ev)
	}
	w.logger.Info("voice call ended", "call_id", ccid, "outcome", ev.Outcome,
		"employee_id", ev.EmployeeID, "caller_display", ev.CallerDisplay)
}

// CallEndedEvent is the typed payload of voice.call.ended.
//
// DISCLOSURE RULE (contact-display master O1/O4, FROZEN): this payload is
// an EGRESS surface — it identifies the caller by resolved contact name
// (CallerName) and the operator-safe last-4 form (CallerDisplay). The raw
// E.164 number field was REMOVED: no bus/WS/relay consumer routed on it
// (verified by survey at leaf 02). Nothing beyond a 4-digit tail may ever
// be added to this shape.
type CallEndedEvent struct {
	CallID        string `json:"call_id"`
	EmployeeID    string `json:"employee_id,omitempty"`
	CallerName    string `json:"caller_name"`
	CallerDisplay string `json:"caller_display"`
	Direction     string `json:"direction,omitempty"`
	Outcome       string `json:"outcome"`
	DurationMs    int64  `json:"duration_ms,omitempty"`
}

// TopicVoiceCallEnded is the typed voice.call.ended declaration (the
// started event fires from the binding's OnCallStart path via the progress
// topic; ended carries the terminal outcome here).
var TopicVoiceCallEnded = bus.NewTopic[CallEndedEvent]("voice.call.ended")

// sendOpenFrame sends the C1 open frame (local-leg plan CL1) for ccid.
// The leg comes from the registry instance (leaf 06); provider selection
// follows CL4 precedence (local-leg config falls back to global, empty
// stays empty). The child-native codec (L16@16k, the C2 chunk format) is
// always the open frame codec - the NEGOTIATED wire codec belongs to the
// splice, not to the child.
func (w *VoiceWiring) sendOpenFrame(ccid string) error {
	f := voice.Frame{
		CallID:     ccid,
		Event:      voice.EventOpen,
		Codec:      telnyx.DefaultCodec.BidirectionalCodec,
		SampleRate: telnyx.DefaultCodec.SamplingRate,
		Direction:  "inbound",
		From:       w.cfg.FromNumber,
	}
	if w.Manager != nil && w.Manager.IsOutbound(ccid) {
		f.Direction = "outbound"
	}
	for _, inst := range w.Registry.Live() {
		if inst.CallControlID != ccid {
			continue
		}
		f.To = inst.Target
		// CL4 resolver: per-call employee defaults (voice.employee_leg)
		// over per-leg config over provider default. The registry leg is
		// authoritative for THIS call (acquire pinned it; ResolveLeg is
		// for acquire-time, not send-time).
		p := voice.ResolveProfile(w.cfg, inst.EmployeeID, "", inst.Leg)
		f.SttVendor = p.SttVendor
		f.SttModel = p.SttModel
		f.TtsVendor = p.TtsVendor
		f.TtsModel = p.TtsModel
		f.TtsVoice = p.TtsVoice
		if inst.Leg == voice.LegLocal {
			f.Local = true
			f.BargeInRms = w.cfg.Local.BargeInRms
		}
		break
	}
	return w.TextServer.Send(f)
}

// spliceRunner builds the leaf-10 SpliceRunner closure: RunSplice for this
// call with a per-call AudioPair under the manager's per-call context.
func (w *VoiceWiring) spliceRunner() func(context.Context, string, commhttp.WebSocketConn, commhttp.StartFrame) error {
	return func(ctx context.Context, ccid string, conn commhttp.WebSocketConn, start commhttp.StartFrame) error {
		w.ChildProc.NoteCallStart(ccid)
		audio, err := voice.NewAudioPair(voice.SocketDir(), ccid)
		if err != nil {
			w.logger.Error("voice wiring: audio pair", "ccid", ccid, "error", err)
			return err
		}
		defer audio.Close()

		// bughunt voice-sidecar local leg (CL1): open the call on the child
		// BEFORE splicing. The child creates its pipeline, dials the C2 pair
		// this runner splices, and answers with the setup frame routeFrame
		// arms the talk loop on. Without this frame the child never builds a
		// pipeline, so no setup ever arrives, so no loop arms - the omission
		// that kept every production call on stub audio. Failure is warn-and-
		// continue (degrade, not fail - mirrors the supervisor philosophy): a
		// call must not die because an optional arm frame failed to send.
		if err := w.sendOpenFrame(ccid); err != nil {
			w.logger.Warn("voice wiring: open frame send failed", "ccid", ccid, "error", err)
		}

		// Codec is the child-native config (legacy wiring field, default
		// L16@16k). CodecWire (audit H2) is the NEGOTIATED wire codec
		// from the media start frame: the splice transcodes wire <-> child
		// through it (PCMU negotiated => mulaw both directions) while the
		// child's C2 chunks stay L16 s16le@16k. Empty (no encoding in the
		// start frame) falls back to Codec.
		var cfg voice.SpliceConfig
		cfg.Codec.BidirectionalCodec = telnyx.DefaultCodec.BidirectionalCodec
		cfg.Codec.SamplingRate = telnyx.DefaultCodec.SamplingRate
		if start.Encoding != "" {
			cfg.CodecWire.BidirectionalCodec = start.Encoding
			cfg.CodecWire.SamplingRate = start.SampleRate
			// bughunt 2026-11-07 voice F-3: the echoed sample rate must
			// MATCH what the transcode path assumes, or the splice
			// silently mis-resamples. The path is codec-selected ONLY:
			// L16 passes bytes through and everything downstream
			// (640-byte "20 ms" chunks, silence fill, pacing) assumes
			// 16 kHz; PCMU is 8 kHz with explicit up/downsample. An
			// L16@8000 negotiation would play at double speed with
			// half-length chunks; a PCMU@16000 would double-decode.
			// Refuse the mismatched media leg instead of shipping
			// corrupted audio (the SpliceRunner error ends the call
			// path; the caller hears the carrier's failure, not noise).
			switch start.Encoding {
			case "L16":
				if start.SampleRate != 0 && start.SampleRate != 16000 {
					err := fmt.Errorf("voice wiring: negotiated wire codec %s@%d is unsupported (L16 must be 16000)", start.Encoding, start.SampleRate)
					w.logger.Error("voice wiring: wire rate mismatch", "ccid", ccid, "error", err)
					return err // deferred audio.Close() tears the pair down
				}
			case "PCMU":
				if start.SampleRate != 0 && start.SampleRate != 8000 {
					err := fmt.Errorf("voice wiring: negotiated wire codec %s@%d is unsupported (PCMU must be 8000)", start.Encoding, start.SampleRate)
					w.logger.Error("voice wiring: wire rate mismatch", "ccid", ccid, "error", err)
					return err // deferred audio.Close() tears the pair down
				}
			}
		}

		return voice.RunSplice(ctx, ccid, cfg, voice.SpliceDeps{
			TelnyxWS: conn,
			Audio:    audio,
			Text:     rawFrameSender{srv: w.TextServer},
			HangupFn: func(hangupCcid, reason string) {
				w.spliceHangup(hangupCcid, reason)
			},
			CancelDraftFn: func(cancelCcid string) {
				w.loopsMu.Lock()
				loop := w.loops[cancelCcid]
				w.loopsMu.Unlock()
				if loop != nil {
					// Media-side barge-in (Telnyx interrupt): same drop
					// contract as a child interrupt frame.
					loop.HandleBargeIn(cancelCcid)
				}
			},
			Logger: w.logger.With("sub", "splice", "ccid", ccid),
		})
	}
}

// Start brings the text server up and registers the voice_call tool via the
// toolprov provider seam. Called from Components.Start AFTER the loop
// machinery runs (the tool registration needs the dialer, which needs the
// employee stack).
//
// TextServer.Start is a lifetime-blocking method (it waits on ctx.Done), so
// it runs on its own goroutine bound to the wiring's stop context; this
// function waits only for the listener to accept (the socket file appears)
// before returning, so Components.Start stays synchronous and a bind
// failure still fails the boot.
func (w *VoiceWiring) Start(ctx context.Context, registerTool func(dial builtin.DialerFunc, agentID builtin.AgentIDFunc)) error {
	w.stopCtx, w.stopCancel = context.WithCancel(ctx)
	// Dead-air call reaper (leaf 02, T4): phone-led only — the reaper
	// probes Telnyx, so it is meaningless without the carrier client.
	// A NEGATIVE knob (interval or max minutes) explicitly disables it;
	// 0 takes the frozen defaults (interval 30s / dead air 60s / max 30m).
	// Runs on its own goroutine bound to the daemon lifecycle ctx, stopped
	// by reaperCancel at Stop.
	if w.tel != nil && (w.cfg.ReaperIntervalS >= 0 || w.cfg.ReaperMaxCallMinutes >= 0) {
		reaperCtx, reaperCancel := context.WithCancel(ctx)
		w.reaperCancel = reaperCancel
		// Status rides the CONCRETE *telnyx.Client (the standalone
		// CallStatusChecker is satisfied by *Client, not by the narrow
		// CallController interface the wiring carries — the manager's
		// client field is the same instance).
		var status voice.CallStatusChecker
		if w.telClient != nil {
			status = w.telClient
		}
		go w.Manager.RunReaper(reaperCtx, voice.ReaperConfig{
			Status:      status,
			Errors:      w.Employee.store,
			Interval:    time.Duration(w.cfg.ReaperIntervalS) * time.Second,
			DeadAir:     time.Duration(w.cfg.ReaperDeadAirS) * time.Second,
			MaxDuration: time.Duration(w.cfg.ReaperMaxCallMinutes) * time.Minute,
		})
		w.logger.Info("voice reaper started",
			"interval_s", w.cfg.ReaperIntervalS,
			"dead_air_s", w.cfg.ReaperDeadAirS,
			"max_call_minutes", w.cfg.ReaperMaxCallMinutes)
	}
	srvErr := make(chan error, 1)
	go func() { srvErr <- w.TextServer.Start(w.stopCtx) }()
	// Wait for the socket file (listener bound) or a bind failure.
	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case err := <-srvErr:
			return fmt.Errorf("voice wiring: text server: %w", err)
		default:
		}
		if _, err := os.Stat(w.textSocketPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			w.stopCancel()
			return fmt.Errorf("voice wiring: text server socket %s never appeared within 5s", w.textSocketPath)
		}
		time.Sleep(5 * time.Millisecond)
	}
	w.logger.Info("voice wiring: text server armed")
	// Plain-HTTP loopback listener (voice.loopback_plain_addr, leaf 10 T6;
	// audit HIGH: previously a dead knob with no production caller). Rides
	// w.stopCtx so VoiceWiring.Stop's cancel shuts it down with the rest of
	// the subsystem. A refused address fails the boot loudly.
	if err := w.startPlainListener(w.stopCtx); err != nil {
		return err
	}
	if registerTool != nil {
		registerTool(w.dialForTool(), w.agentIDForTool())
	}
	return nil
}

// dialForTool adapts the dialer onto the voice_call tool seam.
func (w *VoiceWiring) dialForTool() builtin.DialerFunc {
	return func(ctx context.Context, employeeID, target, requestID string) (string, error) {
		// Phone leg disabled (local-only boot, no telnyx key): the dialer
		// is nil, so an outbound phone dial refuses cleanly instead of a
		// nil-deref panic.
		if w.Dialer == nil {
			return "", fmt.Errorf("voice: phone leg disabled (no telnyx_api_key); outbound dial refused")
		}
		res, err := w.Dialer.Dial(ctx, voice.DialRequest{
			EmployeeID: employeeID,
			Target:     target,
			RequestID:  requestID,
		})
		if err != nil {
			return "", err
		}
		return res.CallControlID, nil
	}
}

// agentIDForTool resolves the calling employee from the executor context.
func (w *VoiceWiring) agentIDForTool() builtin.AgentIDFunc {
	return func(ctx context.Context) string { return agentIDFromContext(ctx) }
}

// openContactDisplayStore opens the daemon-owned contacts.db handle for
// the caller-display resolver (components.go voice block, contact-display
// leaf 02). It reads the SAME database file the contact chain uses
// (contact_wiring.go's path convention); the store's WAL + busy_timeout
// pragmas make the concurrent handles safe. Fail-soft by contract: any
// error logs a WARN and returns nil — the seam stays unwired and every
// egress surface renders the unknown-caller form (a display nicety must
// never fail the boot).
func openContactDisplayStore(logger *slog.Logger) *contact.SQLiteStore {
	if logger == nil {
		logger = slog.Default()
	}
	baseDir := config.MeeptPath("contact")
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		logger.Warn("caller display: contact store dir unavailable; display resolver unwired", "error", err)
		return nil
	}
	store, err := contact.NewStore(filepath.Join(baseDir, "contacts.db"))
	if err != nil {
		logger.Warn("caller display: contact store open failed; display resolver unwired", "error", err)
		return nil
	}
	return store
}

// Stop tears the stack down in dependency order: live calls first (each
// release fires the release-hook fan-out — end frame, talk-loop cancel,
// registry release, binding OnCallEnd, per-call brief — so terminal call
// rows land while the call store is still open), then the signal sidecar,
// the child (it holds the text socket connection), the text server
// listener, and finally the stores.
//
// bughunt D2 (HIGH): Stop previously went straight to w.Employee.Close()
// with live calls untouched — their release hooks then wrote terminal call
// rows into a CLOSED store (rows stranded 'live' + sql.ErrConnDone). The
// pre-close drain below drives every live call through the release funnel
// BEFORE any store closes.
//
// The Signal sidecar stops after the drain (its client holds a signal-cli
// socket conn, not the C1 socket — but the order keeps the leg's call
// paths quiesced before the shared transport closes).
func (w *VoiceWiring) Stop(ctx context.Context) {
	if w == nil {
		return
	}
	stopCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// Stop the reaper FIRST (leaf 02): its sweep must not release calls
	// while the drain below is running the funnel itself.
	if w.reaperCancel != nil {
		w.reaperCancel()
	}
	w.drainLiveCalls(stopCtx)
	StopSignalLeg(stopCtx, w.Signal)
	if w.ChildProc != nil {
		if err := w.ChildProc.Stop(stopCtx); err != nil {
			w.logger.Warn("voice wiring: child stop", "error", err)
		}
	}
	if w.stopCancel != nil {
		w.stopCancel() // unblocks the TextServer.Start lifetime goroutine
	}
	if w.TextServer != nil {
		w.TextServer.Close()
	}
	if w.Employee != nil {
		if err := w.Employee.Close(); err != nil {
			w.logger.Warn("voice wiring: call store close", "error", err)
		}
	}
	if w.displayStore != nil {
		// Contact-display store (leaf 02): owned via
		// WireVoiceDeps.ContactDisplayStore (components.go opened it).
		if err := w.displayStore.Close(); err != nil {
			w.logger.Warn("voice wiring: display contacts store close", "error", err)
		}
	}
	w.logger.Info("voice wiring: stopped")
}

// drainLiveCalls terminates every live call before the stores close. Each
// registry row is driven through the manager's once-per-ccid release
// funnel (FailOutcome -> releaseOnce -> fireRelease -> the release hook),
// which runs SYNCHRONOUSLY: end frame to the child, talk-loop ctx cancel,
// registry release, binding OnCallEnd (the terminal call-row write), and
// the per-call ledger brief all complete before FailOutcome returns — so
// every terminal row lands while the call store is still open. Unknown
// ids (loop self-reaped between the snapshot and the drain) are idempotent
// no-ops per the manager contract. ctx is honored only by the downstream
// hooks' own bounded contexts; the drain itself never blocks on it
// (releaseCall never waits on this caller).
func (w *VoiceWiring) drainLiveCalls(ctx context.Context) {
	if w.Manager == nil || w.Registry == nil {
		return
	}
	live := w.Registry.Live()
	if len(live) == 0 {
		return
	}
	ccids := make([]string, 0, len(live))
	for _, inst := range live {
		ccids = append(ccids, inst.CallControlID)
	}
	sort.Strings(ccids)
	w.logger.Info("voice wiring: draining live calls before store close",
		"count", len(ccids))
	for _, ccid := range ccids {
		w.Manager.FailOutcome(ccid, voice.OutcomeHangup, "daemon shutdown")
	}
}

// voiceSupervisorChatter resolves the C6 gate's fast-model seam for the
// components.go call site (audit M1). voice.supervisor_model is honored in
// WireVoice itself (via deps.SupervisorChatterClient, the
// createAuxiliaryLLMClientWithResolver resolution path shared with the
// classifier chain); when the model ref is empty, unresolvable, or no
// factory was injected, this returns the classifier client - else the main
// LLM client - as the legacy fallback. nil when neither exists (the gate
// then runs disabled - NewSupervisor logs it once).
func voiceSupervisorChatter(c *Components) voice.SupervisorChatter {
	if c == nil {
		return nil
	}
	if c.ClassifierClient != nil {
		return &chatterAdapter{client: c.ClassifierClient}
	}
	if c.LLMClient != nil {
		return &chatterAdapter{client: c.LLMClient}
	}
	return nil
}

// chatterAdapter adapts *llm.Client onto voice.SupervisorChatter (one
// fixed-shot ask).
type chatterAdapter struct {
	client *llm.Client
}

// Ask implements voice.SupervisorChatter.
func (a *chatterAdapter) Ask(ctx context.Context, prompt string) (string, error) {
	resp, err := a.client.Chat(ctx, []llm.ChatMessage{
		{Role: llm.RoleUser, Content: prompt},
	})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

// ---- wiring adapters ----

// turnRunnerFor adapts the binding onto voice.TurnRunner for ONE call:
// Binding.RunTurn(ctx, ccid, text) keys per-call state by call_control_id
// (leaf-12 contract), and the returned runner also stamps the calling
// employee's id onto the turn context so a voice_call executed DURING the
// call is attributable to this employee (the turn ctx reaches tool
// execution unbroken through RunOnce -> executeToolCalls -> Execute).
//
// gates (audit 2026-11-09 finding 1): for ASYNC-held inbound calls the
// runner awaits the call's goal-hold gate before the FIRST employee turn,
// so the per-employee serialization the synchronous hold provided is
// preserved even though the hold now runs off the webhook goroutine. A
// nil tracker or a never-armed ccid (outbound leg, local leg, sync-
// degraded wirings) is an immediate no-op.
func turnRunnerFor(b *voice.Binding, gates *holdTracker, inst voice.Instance) voice.TurnRunner {
	return bindingTurnRunner{b: b, gates: gates, inst: inst}
}

type bindingTurnRunner struct {
	b     *voice.Binding
	gates *holdTracker
	inst  voice.Instance
}

// Run implements voice.TurnRunner.
func (r bindingTurnRunner) Run(ctx context.Context, _ voice.SessionHandle, prompt string) (string, error) {
	if r.gates != nil && !r.gates.await(ctx, r.inst.CallControlID) {
		// The call released while the hold was still pending (or the
		// turn ctx was otherwise cancelled): never start an employee
		// turn for a dead call.
		return "", ctx.Err()
	}
	turnCtx := context.WithValue(ctx, employeeIDContextKey{}, r.inst.EmployeeID)
	// Caller-authz (leaf 02): the caller identity rides EVERY turn ctx
	// alongside the employee stamp. The stamp is unconditional — a zero
	// Info is a legal "voice turn, caller unresolved" mark (enforcement
	// distinguishes stamped-zero from absent; never-stamped is only
	// possible off the voice path).
	turnCtx = callerid.With(turnCtx, r.inst.Caller)
	// Guided intake (leaf 05): the ccid rides the turn ctx (the
	// intake_save tool's callID seam keys the confirm state by it), and
	// the caller-turn boundary advances the confirm state machine — a
	// save demanded last turn becomes confirmable THIS turn (the
	// uptractor turn-count rule: one full caller exchange between the
	// read-back and the commit).
	turnCtx = WithIntakeCCID(turnCtx, r.inst.CallControlID)
	advanceIntakeTurn(r.inst.CallControlID)
	return r.b.RunTurn(turnCtx, r.inst.CallControlID, prompt)
}

// employeeIDContextKey scopes the calling-employee stamp on turn contexts
// (dedicated struct key, repo SA1029 pattern).
type employeeIDContextKey struct{}

// textFrameSender adapts *TextServer (Frame-typed Send) onto the talk
// loop's Send func seam.
type textFrameSender struct {
	srv *voice.TextServer
}

func (s textFrameSender) Send(f voice.Frame) error { return s.srv.Send(f) }

// rawFrameSender adapts *TextServer onto voice.TextSender (the splice's
// raw-byte seam): frames are encoded to C1 JSON lines before the send.
type rawFrameSender struct {
	srv *voice.TextServer
}

// Send implements voice.TextSender.
func (s rawFrameSender) Send(raw []byte) error {
	f, err := voice.DecodeFrame(raw)
	if err != nil {
		return fmt.Errorf("voice wiring: decode frame for child: %w", err)
	}
	return s.srv.Send(f)
}

// agentIDFromContext resolves the calling employee's id from the tool
// execution context. The stamp is written by turnRunnerFor (the voice
// turn path); an absent scope returns empty (the tool then refuses with a
// nameable error - a dial is attributable only to an employee).
func agentIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(employeeIDContextKey{}).(string); ok {
		return v
	}
	return ""
}

// ---- inbound caller resolution (caller-authz leaf 02) ----

// callerResolveBudget bounds the answer-time identity lookup. Resolution
// rides the INBOUND ANSWER path but must never delay it: the budget is
// generous for a local sqlite read and tiny against ringing timeouts, and
// an expiry degrades to the same fail-soft Info{Number} as any error.
const callerResolveBudget = 2 * time.Second

// callerResolverSlot is the late-bound resolver seam. The spawn closure
// (the inbound answer site) is constructed inside WireVoice, BEFORE the
// contact chain exists (BuildContactChain runs over the live wiring, so
// the contacts.db handle arrives after WireVoice returns). The slot lets
// the answer site read the resolver at CALL time — components.go arms it
// via VoiceWiring.SetCallerResolver — without a pointer-capture-order
// hazard. A zero slot (never armed) reads fail-soft.
type callerResolverSlot struct {
	mu       sync.Mutex
	resolver voice.CallerResolver
}

func (s *callerResolverSlot) set(r voice.CallerResolver) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resolver = r
}

func (s *callerResolverSlot) get() voice.CallerResolver {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resolver
}

// SetCallerResolver arms the inbound caller-authz resolver (components.go
// calls it after the contact chain builds). Nil-safe on a nil wiring/slot;
// re-setting replaces the resolver (the daemon arms exactly once at boot).
func (w *VoiceWiring) SetCallerResolver(r voice.CallerResolver) {
	if w == nil {
		return
	}
	w.caller.set(r)
}

// daemonCallerResolver builds the voice.CallerResolver over the contact
// store (contacts.db is the system of record, decision O2). A nil store is
// tolerated: the resolver degrades to the fail-soft Number-only Info with a
// WARN per call (never-wired posture, pinned by the gate).
func daemonCallerResolver(store *contact.SQLiteStore, logger *slog.Logger) voice.CallerResolver {
	if logger == nil {
		logger = slog.Default()
	}
	return &contactCallerResolver{store: store, logger: logger.With("sub", "caller-resolver")}
}

type contactCallerResolver struct {
	store  *contact.SQLiteStore
	logger *slog.Logger
}

// ResolveCaller implements voice.CallerResolver. Fail-soft by contract:
// every lookup failure (nil store, db error, expired budget) returns
// callerid.Info{Number} with a WARN and a NIL error - the answer path has
// no failure branch to take (decision O1: gate actions, never answering).
// An unknown number is NOT an error: Known=false, Name="", Approved=false
// (decision O2), the Number-only Info.
func (r *contactCallerResolver) ResolveCaller(ctx context.Context, e164 string) (callerid.Info, error) {
	number := voice.NormalizeE164(e164)
	info := callerid.Info{Number: number}
	if r.store == nil {
		r.logger.Warn("daemon: caller resolver has no contact store; fail-soft unapproved caller")
		return info, nil
	}
	rec, found, err := r.store.LookupByPhone(ctx, number)
	if err != nil {
		// Includes the expired/cancelled answer budget: same fail-soft.
		r.logger.Warn("daemon: caller lookup failed; fail-soft unapproved caller", "err", err)
		return info, nil
	}
	if !found {
		return info, nil
	}
	info.Name = rec.DisplayName
	info.Approved = rec.VoiceApproved
	info.Known = true
	return info, nil
}
