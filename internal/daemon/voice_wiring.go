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
	"sync"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/auditlog"
	"github.com/caimlas/meept/internal/bus"
	commhttp "github.com/caimlas/meept/internal/comm/http"
	"github.com/caimlas/meept/internal/config"
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

	// supervisor is the C6 gate shared by all talk loops.
	supervisor voice.Supervisor

	// tel is the resolved telnyx seam (shared by manager + dialer +
	// hangup paths). The narrow CallController interface (not the
	// concrete client) keeps the wiring testable with a fake seam.
	tel telnyx.CallController

	// loops maps call_control_id -> live talk loop so the C1 frame
	// handler routes frames and teardown cancels the right goroutine.
	// loopCancels (same lock) holds the per-call context cancel armed at
	// loop setup (audit H4): releaseCall cancels it so a released call's
	// talk loop exits promptly instead of self-reaping after silence
	// timers.
	loopsMu     sync.Mutex
	loops       map[string]*voice.TalkLoop
	loopCancels map[string]context.CancelFunc

	// bus publishes voice.call.ended (typed topic); nil drops events.
	bus *bus.MessageBus

	// stopCtx/stopCancel bound the text server lifetime (set at Start).
	stopCtx    context.Context
	stopCancel context.CancelFunc
	// textSocketPath is the C1 socket path (kept for the Start readiness
	// probe and Stop cleanup).
	textSocketPath string

	// cfg + logger are construction-time values.
	cfg    config.VoiceConfig
	logger *slog.Logger
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
	Logger                  *slog.Logger
}

// WireVoice constructs the voice stack from cfg. It returns (nil, nil)
// when voice.enabled is false (the caller treats that as the disabled
// boot). A configuration error returns a named error: voice is opt-in, so
// a HALF-configured voice block must fail the boot loudly, never degrade
// to half-wired.
func WireVoice(ctx context.Context, deps WireVoiceDeps) (*VoiceWiring, error) {
	cfg := deps.Config.Voice
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	vlog := logger.With("component", "voice")

	// -- 1. Telnyx client: Broker.Value resolves the API token in-process
	// ONLY (audit-secrets-value-callers allowlist row); it reaches the
	// client constructor and nothing else - never a log line, env entry,
	// or child process. A missing broker/secret DEGRADES to voice-disabled
	// with a loud WARN (a scratch boot or a broker-less install must still
	// boot; the dial-time gates and the route-level fail-closed posture
	// keep every call path refused without credentials), never a daemon
	// boot failure - mirroring the wireSlackChannel optional-channel shape.
	telnyxToken := ""
	if deps.Broker != nil {
		token, err := deps.Broker.Value("telnyx_api_key")
		if err != nil {
			vlog.Warn("voice wiring degraded to disabled: secrets broker error", "err", err)
			return nil, nil
		}
		telnyxToken = token
	} else {
		vlog.Warn("voice wiring degraded to disabled: no secrets broker configured; " +
			"telnyx_api_key unresolvable (declare it under [secrets.sources])")
		return nil, nil
	}
	tel := telnyx.NewClient(telnyxBaseURL(), telnyxToken)

	// -- 2. Signature verifiers (leaf 02): mandatory; an undecodable
	// public key fails the boot.
	pubKey, err := decodeTelnyxPublicKey(cfg.TelnyxPublicKey)
	if err != nil {
		return nil, err
	}
	verify := commhttp.VerifyFuncs{
		Webhook: func(body []byte, headers http.Header) error {
			return telnyx.VerifyWebhookSignature(body, headers, pubKey)
		},
		Media: func(headers http.Header, streamURL string) error {
			return telnyx.VerifyMediaSocket(headers, streamURL, pubKey)
		},
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
		Live:           func() int { return len(registry.Live()) },
	}, vlog.With("sub", "child"), nil)
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
	employee, err := WireVoiceEmployee(VoiceEmployeeDeps{
		Sessions:      deps.Sessions,
		LoopManager:   deps.Loops,
		TemplateLoop:  deps.Template,
		WorkingDir:    config.MeeptPath("voice"),
		Bus:           deps.Bus,
		CallStorePath: filepath.Join(config.MeeptPath("voice"), "calls.db"),
		Logger:        vlog.With("sub", "employee"),
	})
	if err != nil {
		return nil, fmt.Errorf("voice wiring: employee stack: %w", err)
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

	// -- 7. Dialer (C9) with the audit adapter.
	var audit voice.AuditWriter
	if deps.Audit != nil {
		audit = auditWriterAdapter{store: deps.Audit}
	}
	dialer := voice.NewDialer(voice.DialConfig{
		Enabled:         true,
		AllowedNumbers:  cfg.AllowedNumbers,
		PublicStreamURL: cfg.PublicStreamURL,
		ConnectionID:    cfg.ConnectionID,
		FromNumber:      cfg.FromNumber,
		TimeoutSecs:     cfg.TimeoutSecs,
	}, tel, manager, registry, deps.Effects, audit, vlog.With("sub", "dialer"))

	w := &VoiceWiring{
		Manager:        manager,
		Registry:       registry,
		Dialer:         dialer,
		Binding:        employee.Binding,
		ChildProc:      child,
		TextServer:     textServer,
		Employee:       employee,
		supervisor:     supervisor,
		tel:            tel,
		loops:          make(map[string]*voice.TalkLoop),
		loopCancels:    make(map[string]context.CancelFunc),
		bus:            deps.Bus,
		textSocketPath: textSocketPath,
		cfg:            cfg,
		logger:         vlog,
	}

	// -- 8. Release-hook fan-out (the leaf's once-per-outcome contract).
	// Sinks: talk-loop teardown, registry Release, binding OnCallEnd (via
	// the leaf-12 ReleaseHook), childproc NoteCallEnd. Manager fires the
	// hook exactly once per ccid; each sink is idempotent under it.
	manager.SetReleaseHook(w.releaseCall)

	// -- 9. C1 frame router: frames from the child land on the right
	// call's talk loop; a setup frame registers a fresh loop.
	textServer.SetHandler(w.routeFrame)

	// -- 10. HTTP routes (leaf 10) with the splice runner closure.
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
func (a auditWriterAdapter) WriteAudit(ctx context.Context, row map[string]any) error {
	rec := auditlog.Record{
		Type:    "voice.dial",
		Payload: row,
	}
	if emp, ok := row["employee_id"].(string); ok {
		rec.EmployeeID = emp
	}
	_, err := a.store.Append(ctx, rec)
	return err
}

// routeFrame is the C1 frame router (TextServer handler). Setup frames
// create a talk loop per call; prompt/interrupt/end frames route to the
// loop registered for that call_control_id.
func (w *VoiceWiring) routeFrame(f voice.Frame) {
	switch f.Event {
	case voice.EventSetup:
		// bughunt 2026-11-07 voice F-6: idempotent setup, mirroring the
		// child's handleSetup ("setup for live call ignored"). Without the
		// guard a duplicate setup (child reconnect/replay) overwrote the
		// loop + loopCancel map entries: the replaced loop kept running
		// unreached (router looks up by map), received no frames, and its
		// silence timer eventually fired t.end -> managerHangup on the
		// CURRENT live call. The lost cancel func also leaked the ctx.
		w.loopsMu.Lock()
		if _, exists := w.loops[f.CallID]; exists {
			w.loopsMu.Unlock()
			w.logger.Debug("voice wiring: setup for live call ignored", "ccid", f.CallID)
			return
		}
		w.loopsMu.Unlock()
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
		loop := voice.NewTalkLoop(voice.TalkDeps{
			Supervisor: w.supervisor,
			Turns:      turnRunnerFor(w.Employee.Binding, inst),
			SessionFor: func(call voice.Instance) voice.SessionHandle {
				return w.sessionFor(call)
			},
			Binding: w.Employee.Binding,
			Send:    textFrameSender{srv: w.TextServer}.Send,
			OnEnd:   func(ccid, reason string) { w.managerHangup(ccid, reason) },
			Logger:  w.logger.With("sub", "talk", "ccid", f.CallID),
		}, voice.TalkLoopConfig{
			FillerLines:          w.cfg.Filler,
			GreetingLine:         greetingFromFiller(w.cfg.Filler),
			FallbackLine:         "I cannot confirm that yet.",
			MaxCallMinutes:       w.cfg.MaxCallMinutes,
			SilencePromptSeconds: w.cfg.SilencePromptSeconds,
		})
		w.loopsMu.Lock()
		w.loops[f.CallID] = loop
		// H4: arm the per-call context. releaseCall cancels it so a
		// released call's talk loop exits promptly instead of
		// self-reaping after silence timers. loopContext owns the
		// daemon-lifecycle parent.
		loopCtx, loopCancel := context.WithCancel(w.loopContext(f.CallID))
		w.loopCancels[f.CallID] = loopCancel
		w.loopsMu.Unlock()
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
			w.loopsMu.Lock()
			delete(w.loops, f.CallID)
			delete(w.loopCancels, f.CallID)
			w.loopsMu.Unlock()
		}()
		w.logger.Info("voice wiring: talk loop armed", "ccid", f.CallID, "employee_id", inst.EmployeeID)
	default:
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
	if err := w.tel.HangupCall(context.Background(), ccid); err != nil {
		// Best-effort carrier hangup; the manager already owns teardown
		// state. A failed POST leaves the leg to Telnyx's own timeout.
		w.logger.Debug("voice wiring: carrier hangup", "ccid", ccid, "err", err)
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
	if err := w.tel.HangupCall(context.Background(), ccid); err != nil {
		w.logger.Debug("voice wiring: splice carrier hangup", "ccid", ccid, "err", err)
	}
	w.Manager.FailOutcome(ccid, voice.OutcomeFailed, reason)
}

// releaseCall is the Manager release hook fan-out. Order matters for
// observability, not correctness (each sink is once-guarded):
//  1. childproc NoteCallEnd (idle timer accounting)
//  2. talk-loop cancel (per-call ctx, audit H4 — the loop's Run select
//     sees ctx.Done and exits promptly, cancelling its in-flight turn
//     pipeline)
//  3. registry Release (frees the C8 slot + target)
//  4. binding OnCallEnd (leaf-12 ReleaseHook: call row + goal hold)
func (w *VoiceWiring) releaseCall(ccid string, outcome voice.Outcome) {
	w.ChildProc.NoteCallEnd(ccid)

	w.loopsMu.Lock()
	loop := w.loops[ccid]
	cancel := w.loopCancels[ccid]
	delete(w.loops, ccid)
	delete(w.loopCancels, ccid)
	w.loopsMu.Unlock()
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

// publishCallEnded publishes voice.call.ended (typed topic, WS class
// event, payload {call_id, employee_id, number, direction, outcome,
// duration_ms}). The registry's live snapshot supplies the identity
// fields; a released ccid is gone from it, so the payload degrades to
// call_id + outcome (never an error). Best-effort: a nil bus drops it.
func (w *VoiceWiring) publishCallEnded(ccid string, outcome voice.Outcome) {
	if w.Employee == nil {
		return
	}
	ev := CallEndedEvent{CallID: ccid, Outcome: string(outcome)}
	for _, inst := range w.Registry.Live() {
		if inst.CallControlID == ccid {
			ev.EmployeeID = inst.EmployeeID
			ev.Number = inst.Target
			break
		}
	}
	if w.bus != nil {
		bus.PublishT(w.bus, TopicVoiceCallEnded, "voice", ev)
	}
	w.logger.Info("voice call ended", "call_id", ccid, "outcome", ev.Outcome,
		"employee_id", ev.EmployeeID, "number", ev.Number)
}

// CallEndedEvent is the typed payload of voice.call.ended.
type CallEndedEvent struct {
	CallID     string `json:"call_id"`
	EmployeeID string `json:"employee_id,omitempty"`
	Number     string `json:"number,omitempty"`
	Direction  string `json:"direction,omitempty"`
	Outcome    string `json:"outcome"`
	DurationMs int64  `json:"duration_ms,omitempty"`
}

// TopicVoiceCallEnded is the typed voice.call.ended declaration (the
// started event fires from the binding's OnCallStart path via the progress
// topic; ended carries the terminal outcome here).
var TopicVoiceCallEnded = bus.NewTopic[CallEndedEvent]("voice.call.ended")

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
	if registerTool != nil {
		registerTool(w.dialForTool(), w.agentIDForTool())
	}
	return nil
}

// dialForTool adapts the dialer onto the voice_call tool seam.
func (w *VoiceWiring) dialForTool() builtin.DialerFunc {
	return func(ctx context.Context, employeeID, target, requestID string) (string, error) {
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

// Stop tears the stack down in dependency order: child FIRST (it holds the
// text socket connection), then the text server listener, then the call
// store. Loop goroutines exit with the daemon lifecycle context.
func (w *VoiceWiring) Stop(ctx context.Context) {
	if w == nil {
		return
	}
	stopCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := w.ChildProc.Stop(stopCtx); err != nil {
		w.logger.Warn("voice wiring: child stop", "error", err)
	}
	if w.stopCancel != nil {
		w.stopCancel() // unblocks the TextServer.Start lifetime goroutine
	}
	w.TextServer.Close()
	if err := w.Employee.Close(); err != nil {
		w.logger.Warn("voice wiring: call store close", "error", err)
	}
	w.logger.Info("voice wiring: stopped")
}

// voiceSupervisorChatter resolves the C6 gate's fast-model seam for the
// components.go call site (audit M1). voice.supervisor_model is honored in
// WireVoice itself (via deps.SupervisorChatterClient, the
// createAuxiliaryLLMClientWithResolver resolution path shared with the
// classifier chain); when the model ref is empty, unresolvable, or no
// factory was injected, this returns the classifier client — else the main
// LLM client — as the legacy fallback. nil when neither exists (the gate
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
func turnRunnerFor(b *voice.Binding, inst voice.Instance) voice.TurnRunner {
	return bindingTurnRunner{b: b, inst: inst}
}

type bindingTurnRunner struct {
	b    *voice.Binding
	inst voice.Instance
}

// Run implements voice.TurnRunner.
func (r bindingTurnRunner) Run(ctx context.Context, _ voice.SessionHandle, prompt string) (string, error) {
	turnCtx := context.WithValue(ctx, employeeIDContextKey{}, r.inst.EmployeeID)
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

// greetingFromFiller derives the greeting line from the filler list (the
// greeting is filler-class; an empty list disables the greeting rather than
// hardcoding one).
func greetingFromFiller(filler []string) string {
	if len(filler) > 0 {
		return filler[0]
	}
	return ""
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
