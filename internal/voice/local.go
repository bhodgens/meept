package voice

// LocalLeg lifecycle (local-leg plan CL5, docs/plans/20261106-voice-
// local-leg/07-local-leg.md): ONE callable unit that turns (employee,
// label) into a live local voice call - registry slot, audio pair, child
// pipeline via the C1 open frame, talk loop via the setup frame the
// child answers, day brief + binding exactly like a phone call, and a
// release fan-out identical to the phone path MINUS the carrier.
//
// Mirror of dial.go DialFlow steps 3-8 MINUS steps 1-2 (the enabled
// gate keeps; the audit row and the effects claim are DROPPED: no paid
// side effect, no external party - the CL5 deliberate difference, pinned
// structurally by the nil-carrier Manager in every fixture) and MINUS
// any telnyx call anywhere in this file (build-pinned: local.go imports
// no telnyx package; the LocalLeg carries no carrier seam at all).
//
// Order (Open):
//
//  1. gates: leg enabled; non-empty employee (named refusals)
//  2. ccid = "local-" + pkg/id.Generate(); target = "local:<label>"
//  3. registry AcquireLeg(..., leg=local) - ErrBusy / ErrDuplicateTarget
//     surface unchanged (rules shared across legs per CL6)
//  4. Manager.TrackOutbound(ccid) so FailOutcome / WaitCall / the
//     existing release fan-out work unchanged (no carrier involved)
//  5. AudioPair in SocketDir (the pair PATHS go to the caller: leaf 08's
//     clients dial them)
//  6. C1 open frame (direction "local", local:true, barge_in_rms,
//     CL4-resolved provider profile) - the child builds its pipeline,
//     dials the C2 pair, and answers with setup; routeFrame's existing
//     setup handler arms the talk loop from Registry.Live()
//  7. LocalSplice in a goroutine (client join grace armed)
//  8. DayBriefBuilder + Binding.OnCallStart with direction "local" and
//     number "local:<label>" - the SAME call-site shape as the phone
//     path (day brief built BEFORE OnCallStart)
//
// Teardown (End, and any splice terminal error): Manager.FailOutcome
// runs the once-per-outcome release fan-out (childproc note, loop
// cancel, registry release, binding end) exactly as a phone hangup
// does; End additionally cancels the splice context and closes the
// audio pair. Double End is idempotent.
//
// Interrupt: nothing extra here - the C1 interrupt frame routes by ccid
// through the wiring's talk loop, whose handleInterrupt resolves
// LocalSpliceFor(ccid).HandleInterrupt (leaf 07 entry point; the splice's
// latched cancel then fires the cancelDraftHook -> loop.HandleBargeIn for
// the RMS-seam parity drop; see bughunt 2026-11-11 C-1). A foreign ccid
// is a no-op on both paths.
//
// Reaping: no silence-based auto-hangup override - the talk loop's
// normal silence / max-call timers apply (same config as a phone call),
// so a forgotten local call self-reaps through managerHangup ->
// FailOutcome -> the release fan-out.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/caimlas/meept/pkg/id"
)

// ErrNoEmployee is the named refusal for an Open without an employee id.
var ErrNoEmployee = errors.New("voice: local leg open: employee_id is required")

// ErrLocalDisabled is the named refusal when the leg is not enabled
// (voice.local.enabled=false or the constructed config disabled it).
var ErrLocalDisabled = errors.New("voice: local leg is disabled")

// ErrUnknownLocalCall is returned by End for a ccid that was never
// opened by this leg. A double End (a ccid already reaped here) stays
// the idempotent nil the contract pins.
var ErrUnknownLocalCall = errors.New("voice: local leg: unknown call")

// LocalLegConfig carries the leg-level knobs. Enabled mirrors the
// voice.enabled && voice.local.enabled gate (the daemon glue folds both
// into this one bool); empty Label falls back to os.Hostname() at Open.
type LocalLegConfig struct {
	Enabled    bool
	Label      string // session label; empty = hostname (leaf 01)
	BargeInRms int    // s16 RMS threshold for the local VAD (CL1)
}

// ProfileResolver resolves one call's CL4 provider profile (voice
// selection). The daemon glue adapts voice.ResolveProfile over the live
// config onto it so internal/voice needs no config import here.
type ProfileResolver func(employeeID string) Profile

// LocalDeps carries the injected edges (CL5 verbatim field set). The
// concrete *Binding rides the dep because the daemon glue owns it; nil
// is tolerated (the call then runs unbound - production wiring always
// supplies it). DayBriefBuilder is injected BY the daemon glue so
// internal/voice stays dependency-free (no internal/activity,
// internal/agent, or internal/employee imports).
type LocalDeps struct {
	Registry        Registry
	ChildProc       childNoteSeam // the concrete *ChildProc; nil-safe call sites
	TextServer      *TextServer
	Manager         *Manager
	Binding         *Binding
	DayBriefBuilder func(ctx context.Context, employeeID, number string) (DayBrief, error)
	// Profile resolves the CL4 provider selection for the open frame.
	// Nil falls back to the empty profile (vendor defaults apply
	// child-side).
	Profile ProfileResolver
	// FreshSession resolves the per-call fresh-session flag for the
	// binding (fresh-session plan leaf 02): true makes
	// Binding.OnCallStart create a NEW per-call session instead of
	// reusing the shared (employee,target) one. The daemon wires it to
	// voice.FreshSessionFor over the live config (same closure shape as
	// Profile, so the global + per-employee override resolves once per
	// call outside the binding). Nil degrades to false (shared session,
	// byte-identical to the pre-leaf-02 behavior).
	FreshSession func(employeeID string) bool
	// Hold is the daemon-glue goal-hold seam (AcquireCallHold). The leg
	// invokes it once per successfully-opened call so the release
	// fan-out's ReleaseHook (internal/daemon/voice_employee.go) sees the
	// ccid in heldCalls and runs Binding.OnCallEnd to mark the calls.db
	// row ended; without it the row stays 'live' forever. Nil degrades
	// to no hold (the row then never reaches 'ended'), mirroring the
	// "do not claim ended-state behavior" posture.
	Hold func(ccid, employeeID string)
	// LinkTurns is the half-duplex coordinator registry (turn-taking
	// leaf 02, drain-driven floor). LocalLeg.Open resolves the call's
	// registry target against it BEFORE spawning the splice: a LINK
	// leg's target was pre-registered by LinkManager.Open (which runs
	// before Leg.Open by contract), so the splice's AudioDrained dep
	// lands on that call's coordinator and nothing else. Plain
	// (non-link) local calls never match a target -> dep nil -> the
	// splice never arms a drain watch - byte-identical to before.
	// Nil (tests / degraded wiring) = the same no-seam posture.
	LinkTurns *LinkTurnRegistry
	Logger    *slog.Logger
}

// holdCall acquires the goal hold for a live call when the seam is
// wired. Nil seam is a no-op (degraded stack). Called once per
// successfully-opened call, before any teardown path can run so the
// release fan-out always observes the hold.
func (l *LocalLeg) holdCall(ccid, employeeID string) {
	if l.deps.Hold == nil {
		return
	}
	l.deps.Hold(ccid, employeeID)
}

// LocalCall is one live local call's handle (CL5 verbatim): the ccid,
// the resolved meept session id, and the C2 pair PATHS the client dials
// (leaf 08's clients write mic bytes to InPath and read speaker bytes
// from OutPath). EvPath is the daemon-owned CONTROL listener for the
// barge-in clear lane (bughunt 2026-11-11 defect 1): JSON-lines, never
// PCM. Empty = the lane failed to come up (degraded: the client plays
// the old way, exactly the pre-fix behavior, never a call failure).
type LocalCall struct {
	CallID    string
	SessionID string
	InPath    string
	OutPath   string
	EvPath    string
}

// localCallState is one live call's leg-owned state.
type localCallState struct {
	ccid      string
	employee  string
	target    string
	pair      *AudioPair         // CHILD pair: the child dials <ccid>.in/.out
	cliPair   *AudioPair         // CLIENT pair: the client dials <ccid>.cli.in/.out
	ev        *ClientEventServer // daemon->client control lane (<ccid>.c.ev); nil = degraded
	cancel    context.CancelFunc // splice ctx cancel (End tears it down)
	sessionID string
	// ttsVoiceOverride is the tool's per-call `voice` arg (leaf 09
	// LocalFunc's 4th parameter): highest CL4 precedence, applied AFTER
	// the profile resolver fills the frame.
	ttsVoiceOverride string
}

// childNoteSeam is the ChildProc note seam the leg calls through. The
// concrete *ChildProc satisfies it; tests inject a recorder. Nil-safe
// call sites: a nil seam drops the note (production wiring always
// supplies the real child).
type childNoteSeam interface {
	NoteCallStart(ccid string)
	NoteCallEnd(ccid string)
}

// childSpawnSeam is the ensure-child seam the local leg calls through.
// The concrete *ChildProc satisfies it (EnsureRunning spawns the sidecar
// on first use); tests inject a stub or a recorder. Separate from
// childNoteSeam so the phone-only spawn seam stays the ONLY EnsureRunning
// caller at the wiring layer and the leg's own guarantee stays local.
type childSpawnSeam interface {
	EnsureRunning(ctx context.Context) error
}

// LocalLeg opens and tears down local voice calls. All mutable state is
// mutex-guarded; the deps are immutable after construction (except the
// cancelDraft hook, which is nil-guarded and set once at wiring).
type LocalLeg struct {
	cfg  LocalLegConfig
	deps LocalDeps
	log  *slog.Logger

	mu    sync.Mutex
	calls map[string]*localCallState

	// cancelDraftHook is the daemon glue's draft-cancel seam (the
	// wiring's loop.HandleBargeIn for this ccid). Set at wiring time.
	cancelDraftHook func(ccid string)

	// joinObsMu guards joinObs: the ADDITIVE client-join observer lane
	// (tree 20261110-voice-callleg leaf 02). The edge is the splice's
	// first client mic byte (the join gate that satisfies JoinTimeout)
	// — the local leg's last-mile observable "the call is live"
	// moment. The child-answers-setup edge itself is daemon-side
	// (voice_wiring routeFrame EventSetup, off-limits to this leaf),
	// so the adapter maps the join edge to Connected. Pure observation:
	// observers never gate the splice.
	joinObsMu sync.Mutex
	joinObs   []func(ccid string)
}

// AddClientJoinObserver registers a best-effort observer fired once per
// call when the client produces its first mic byte (leaf 02; the local
// CallLeg adapter maps it to Connected). Additive: any number may
// register; the splice's join gate is unchanged.
func (l *LocalLeg) AddClientJoinObserver(fn func(ccid string)) {
	if fn == nil {
		return
	}
	l.joinObsMu.Lock()
	l.joinObs = append(l.joinObs, fn)
	l.joinObsMu.Unlock()
}

// notifyClientJoin invokes every join observer outside l.mu and the
// observer mutex (snapshot then call — the Manager.notifyAnswered
// pattern). Called from the splice's pump goroutine; best-effort.
func (l *LocalLeg) notifyClientJoin(ccid string) {
	l.joinObsMu.Lock()
	if len(l.joinObs) == 0 {
		l.joinObsMu.Unlock()
		return
	}
	obs := make([]func(string), len(l.joinObs))
	copy(obs, l.joinObs)
	l.joinObsMu.Unlock()
	for _, fn := range obs {
		fn(ccid)
	}
}

// NewLocalLeg constructs the leg. Typed-nil guards on the load-bearing
// deps fields (repo rule): a nil Registry, Manager, or TextServer fails
// construction - each carries a load-bearing half of the lifecycle
// (capacity, release fan-out, C1 transport). The Manager may be the
// nil-carrier manager (telnyx.CallController nil): the local leg never
// issues a carrier call; it only rides TrackOutbound/FailOutcome.
func NewLocalLeg(cfg LocalLegConfig, deps LocalDeps) (*LocalLeg, error) {
	if deps.Registry == nil {
		return nil, errors.New("voice: local leg: registry is required")
	}
	if deps.TextServer == nil {
		return nil, errors.New("voice: local leg: text server is required")
	}
	if deps.Manager == nil {
		return nil, errors.New("voice: local leg: manager is required (it carries the release fan-out)")
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	return &LocalLeg{
		cfg:   cfg,
		deps:  deps,
		log:   deps.Logger,
		calls: make(map[string]*localCallState),
	}, nil
}

// SetCancelDraftHook wires the daemon glue's draft-cancel seam (the
// wiring's loop.HandleBargeIn). Nil fn is ignored (every Set* nil-guarded
// per repo rule). Without the hook, barge-in still flushes the child's
// TTS queue (the clear frame) - only the daemon-side draft cancellation
// is absent, the same shape as a phone call whose loop already died.
func (l *LocalLeg) SetCancelDraftHook(fn func(ccid string)) {
	if fn == nil {
		return
	}
	l.mu.Lock()
	l.cancelDraftHook = fn
	l.mu.Unlock()
}

// localLabel resolves the session label: the explicit arg wins, then the
// configured Label, then the hostname (leaf 01: "empty = hostname
// (resolved by caller)").
func (l *LocalLeg) localLabel(label string) (string, error) {
	if label != "" {
		return label, nil
	}
	if l.cfg.Label != "" {
		return l.cfg.Label, nil
	}
	host, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("voice: local leg: resolve label: %w", err)
	}
	return host, nil
}

// Open turns (employee, label) into a live local voice call (CL5). The
// registry errors surface unchanged; every other refusal is a named
// error BEFORE any side effect.
func (l *LocalLeg) Open(ctx context.Context, employeeID, label string, voiceOverride ...string) (LocalCall, error) {
	// -- 1. gates (named refusals, zero side effects) --
	if !l.cfg.Enabled {
		return LocalCall{}, ErrLocalDisabled
	}
	if employeeID == "" {
		return LocalCall{}, ErrNoEmployee
	}
	target, err := l.localLabel(label)
	if err != nil {
		return LocalCall{}, err
	}
	target = "local:" + target

	// -- 2. identity: ccid = "local-" + pkg/id.Generate() --
	ccid := id.Generate("local-")

	// -- 3. registry slot. ErrBusy / ErrDuplicateTarget surface
	// unchanged; cap + one-live-per-employee are shared across legs
	// (CL6). The target stays VERBATIM (NormalizeE164 must never mangle
	// "local:..."). --
	if _, err := l.deps.Registry.AcquireLeg(ccid, employeeID, target, LegLocal); err != nil {
		return LocalCall{}, err
	}

	// -- 4. TrackOutbound BEFORE the pair (phone-path order: the manager
	// tracks first, so any later failure can FailOutcome cleanly). --
	if err := l.deps.Manager.TrackOutbound(ccid); err != nil {
		l.deps.Registry.Release(ccid)
		return LocalCall{}, fmt.Errorf("voice: local leg: track: %w", err)
	}

	// -- 5b. child availability (local-leg CL5 closure, BEFORE the pair):
	// the phone spawn seam is the only other EnsureRunning caller, and a
	// local-only install (no telnyx token -> no phone leg) never crosses
	// it. The leg therefore guarantees the sidecar itself. Ordering
	// matters: ChildProc.spawn sweeps AudioDir/*.in|*.out as stale, so a
	// spawn AFTER NewAudioPair would delete the live pair files. Spawning
	// first also keeps the failure refusal clean: a failure refuses the
	// call before any pair/hold/note side effect beyond the registry
	// slot - which Release reaps directly. --
	if l.deps.ChildProc != nil {
		if s, ok := l.deps.ChildProc.(childSpawnSeam); ok {
			if err := s.EnsureRunning(ctx); err != nil {
				l.deps.Manager.FailOutcome(ccid, OutcomeFailed, fmt.Sprintf("child spawn: %v", err))
				l.deps.Registry.Release(ccid)
				return LocalCall{}, fmt.Errorf("voice: local leg: child spawn: %w", err)
			}
		}
	}

	// -- 6. audio pair (paths under SocketDir, sun_path-safe like every
	// phone call; NewAudioPair removes stale files itself). AFTER the
	// spawn: ChildProc.spawn sweeps AudioDir/*.in|*.out as stale and
	// would delete the live pair files. --
	pair, err := NewAudioPair(SocketDir(), ccid)
	if err != nil {
		l.deps.Manager.FailOutcome(ccid, OutcomeFailed, fmt.Sprintf("audio pair: %v", err))
		l.deps.Registry.Release(ccid)
		return LocalCall{}, fmt.Errorf("voice: local leg: audio pair: %w", err)
	}

	st := &localCallState{
		ccid:     ccid,
		employee: employeeID,
		target:   target,
		pair:     pair,
	}
	// Client pair (local-leg client transport): the CHILD owns the main
	// pair's accepts (it dials <ccid>.in/.out from its --audio-dir), so
	// the CLIENT needs its own two listeners. The CLI/RPC open result
	// returns the CLIENT pair paths; the child pair paths never leave
	// the daemon. Suffix ".c" (not ".cli"): the macOS sun_path limit is
	// 103 bytes and e2e sandboxes run close to it. The spawn's stale
	// sweep runs BEFORE either pair is created here, and it skips the
	// client-pair namespace entirely (bughunt 2026-11-10 A9): a spawn
	// concurrent with another live local call must not delete that
	// call's <ccid>.c.in/.c.out transport files.
	cliPair, err := NewAudioPair(SocketDir(), ccid+".c")
	if err != nil {
		l.deps.Manager.FailOutcome(ccid, OutcomeFailed, fmt.Sprintf("client pair: %v", err))
		if cerr := pair.Close(); cerr != nil {
			l.log.Debug("voice: local leg: child pair close on refusal", "ccid", ccid, "err", cerr)
		}
		l.deps.Registry.Release(ccid)
		return LocalCall{}, fmt.Errorf("voice: local leg: client pair: %w", err)
	}
	st.cliPair = cliPair
	// Client CONTROL lane (<ccid>.c.ev, bughunt 2026-11-11 defect 1):
	// the daemon->client JSON-lines listener that carries the barge-in
	// clear (the pair sockets stay raw-PCM-only by contract). Same 5
	// suffix bytes as ".c.in"/".c.out", so if the client pair listened,
	// the ev path fits the sun_path limit too (the 100-byte cap inside
	// mirrors the monitor tap's). A listen failure DEGRADES (nil server,
	// empty EvPath): the call runs exactly like before this fix - the
	// CLI keeps its ~750 ms ghost tail - but never fails. Stale .c.ev
	// files from a crashed daemon cannot wedge Listen: newMonitorListener
	// removes the path first (and the ChildProc stale sweep deliberately
	// never touches the client-pair namespace - bughunt 2026-11-10 A9).
	evPath := filepath.Join(SocketDir(), ccid+".c.ev")
	evSrv, evErr := NewClientEventServer(evPath, l.log.With("sub", "clientev", "ccid", ccid))
	if evErr != nil {
		l.log.Warn("voice: local leg: client event lane failed to open (barge-in client silence degraded)",
			"ccid", ccid, "err", evErr)
		evSrv, evPath = nil, ""
	}
	st.ev = evSrv
	// Tool-supplied CL4 override (voice_call's `voice` arg): highest
	// precedence - beats the per-call profile resolver (voice_wiring).
	if len(voiceOverride) > 0 {
		st.ttsVoiceOverride = voiceOverride[0]
	}
	spliceCtx, spliceCancel := context.WithCancel(context.Background())
	st.cancel = spliceCancel

	l.mu.Lock()
	l.calls[ccid] = st
	l.mu.Unlock()
	registerClientEvents(ccid, evSrv)

	// Acquire the goal hold ONCE the call is live (gap closure): the
	// release fan-out's ReleaseHook gates Binding.OnCallEnd on this
	// heldCalls entry, so the acquire must land before any teardown
	// path can run (splice goroutine below, End, or a later error). The
	// per-employee serialization mutex is free here - the registry's
	// one-live-call-per-employee rule rejected a concurrent open for
	// this employee before this point.
	l.holdCall(ccid, employeeID)

	// ChildProc note (releaseCall's sink 1 mirror): the child's idle
	// timer counts this call from now; NoteCallEnd fires at teardown.
	if l.deps.ChildProc != nil {
		l.deps.ChildProc.NoteCallStart(ccid)
	}

	// -- 6. open frame (CL1 local shape): identity + leg marker + RMS
	// threshold + the CL4-resolved provider profile. Send failure
	// degrades (warn, call continues on stub audio) - the same posture
	// the wiring's spliceRunner pins for phone calls. --
	if err := l.sendOpenFrame(st); err != nil {
		l.log.Warn("voice: local leg: open frame send failed (continuing on stub audio)", "ccid", ccid, "err", err)
	}

	// -- 7. splice in a goroutine: client mic chunked to the child, child
	// PCM paced to the client, the leaf-05 barge-in latch live. A
	// terminal splice error (client vanished, socket broke) funnels
	// through the same teardown as End; the pre-dial window (nobody has
	// dialed the pair yet - the client joins seconds later) is NOT
	// terminal: ReadFromClient surfaces "not connected" while the accept
	// is pending and the splice's own JoinTimeout bounds that grace
	// (ErrNoClient), so a never-dialed leg still self-reaps. --
	//
	// Drain seam (turn-taking leaf 02): resolve the coordinator for
	// THIS call's target at Open time (LinkManager.Open pre-registers
	// both link targets before Leg.Open runs, so a link leg resolves
	// here; a plain local target never matches). A resolved coordinator
	// gets the splice's AudioDrained edge -> floor release after the
	// paced tail actually leaves the socket.
	go func() {
		drainFn := wiredAudioDrain(l.deps.LinkTurns, target)
		err := RunLocalSplice(spliceCtx, ccid, LocalSpliceConfig{}, LocalSpliceDeps{
			Audio:        pair,
			Client:       localClientIO{cli: cliPair},
			Text:         rawTextSender{srv: l.deps.TextServer},
			CancelDraft:  func(cancelCcid string) { l.cancelDraft(cancelCcid) },
			AudioDrained: drainFn,
			OnClientJoin: l.notifyClientJoin,
			Logger:       l.log.With("sub", "localsplice", "ccid", ccid),
		})
		if err != nil && !errors.Is(err, context.Canceled) && spliceCtx.Err() == nil && !isNotConnected(err) && !errors.Is(err, ErrMicSpeechEnded) {
			l.log.Error("voice: local leg: splice ended", "ccid", ccid, "err", err)
			_ = l.End(ccid, fmt.Sprintf("splice: %v", err))
		}
	}()

	// -- 8. day brief + binding: the SAME call-site shape as the phone
	// path (build the brief, then Binding.OnCallStart with the call's
	// direction and number). Direction "local", number "local:<label>".
	// A builder failure degrades to an empty brief (never blocks the
	// call); OnCallEnd rides the existing release fan-out, so the call
	// row the binding records ends exactly once. --
	var brief DayBrief
	if l.deps.DayBriefBuilder != nil {
		b, berr := l.deps.DayBriefBuilder(ctx, employeeID, target)
		if berr != nil {
			l.log.Warn("voice: local leg: day brief build failed (continuing with an empty brief)", "ccid", ccid, "err", berr)
		} else {
			brief = b
		}
	}
	if l.deps.Binding != nil {
		// Fresh-session flag (leaf 02): resolved once per call via the
		// daemon-wired closure (nil seam = false = today's behavior).
		var fresh bool
		if l.deps.FreshSession != nil {
			fresh = l.deps.FreshSession(employeeID)
		}
		sid, serr := l.deps.Binding.OnCallStart(ctx, ccid, target, employeeID, "local", brief, fresh)
		if serr != nil {
			l.log.Warn("voice: local leg: binding OnCallStart failed (call continues unbound)", "ccid", ccid, "err", serr)
		} else {
			st.sessionID = sid
		}
	}

	inPath := pairInPath(cliPair)
	outPath := pairOutPath(cliPair)
	l.log.Info("voice: local call open", "ccid", ccid, "employee_id", employeeID,
		"target", target, "in", inPath, "out", outPath, "ev", evPath)
	return LocalCall{
		CallID:    ccid,
		SessionID: st.sessionID,
		InPath:    inPath,
		OutPath:   outPath,
		EvPath:    evPath,
	}, nil
}

// sendOpenFrame builds and sends the CL1 open frame for st. Identity
// mirrors the phone producer's sendOpenFrame: child-native codec
// (L16@16000 - the NEGOTIATED wire codec belongs to the splice), the
// call's CL4-resolved provider profile, and the local leg marker with
// its RMS threshold. Providers never ride the open frame (they ride the
// keys line, tierA's wire - the wiring emits keys at EventSetup, which
// the child answers the open frame with).
func (l *LocalLeg) sendOpenFrame(st *localCallState) error {
	f := Frame{
		CallID:     st.ccid,
		Event:      EventOpen,
		To:         st.target,
		Direction:  "local",
		Codec:      "L16",
		SampleRate: 16000,
		Local:      true,
		BargeInRms: l.cfg.BargeInRms,
	}
	if l.deps.Profile != nil {
		p := l.deps.Profile(st.employee)
		f.SttVendor = p.SttVendor
		f.SttModel = p.SttModel
		f.TtsVendor = p.TtsVendor
		f.TtsModel = p.TtsModel
		f.TtsVoice = p.TtsVoice
	}
	// Tool override (CL4 top layer) beats the resolved profile voice.
	if st.ttsVoiceOverride != "" {
		f.TtsVoice = st.ttsVoiceOverride
	}
	return l.deps.TextServer.Send(f)
}

// cancelDraft is the splice's barge-in CancelDraft seam: drop the talk
// loop's in-flight draft for THIS call. The talk loop lives in the
// daemon wiring (routeFrame armed it from Registry.Live() when the child
// answered the open frame with setup), so the leg reaches it through the
// hook the glue installs. LocalSpliceFor(ccid).HandleInterrupt already
// latches per barge-in (leaf 05), so this runs at most once per barge-in
// per trigger source: the RMS trip path calls it once via the splice's
// CancelDraft dep, and the child interrupt frame path runs the SAME
// latched HandleInterrupt through the talk loop (bughunt 2026-11-11 C-1)
// — which also fires this hook, idempotently, after the latch.
func (l *LocalLeg) cancelDraft(ccid string) {
	l.mu.Lock()
	hook := l.cancelDraftHook
	l.mu.Unlock()
	if hook != nil {
		hook(ccid)
	}
}

// wiredAudioDrain resolves one local call's drain-edge callback: the
// splice fires it (AudioDrained dep) when the turn's paced audio has
// fully left the client socket, and the coordinator's floor releases
// after its tail pad. Resolution happens ONCE at Open, keyed by the
// call's REGISTRY TARGET through the same LinkTurnRegistry the talk
// loops use (link targets are pre-registered by LinkManager.Open before
// Leg.Open; a plain local target never matches -> nil dep -> the
// splice never arms a watch). The returned closure re-checks the
// coordinator per fire (the link may have closed between arm and fire;
// NoteAudioDrained on a closed coordinator is a guarded no-op).
func wiredAudioDrain(reg *LinkTurnRegistry, target string) func(string) {
	if reg == nil {
		return nil
	}
	coord, _ := reg.Lookup(target)
	if coord == nil {
		return nil
	}
	return func(drainCcid string) { coord.NoteAudioDrained(drainCcid) }
}

// End tears the call down: Manager.FailOutcome(ccid, OutcomeHangup,
// reason) runs the once-per-outcome release fan-out (childproc note,
// loop cancel, registry release, binding end - identical to a phone
// hangup minus the carrier POST); the splice ctx cancel and the audio
// pair Close complete the leg-local teardown. Double End is idempotent
// (second call is a nil no-op); a foreign id is a named error.
func (l *LocalLeg) End(ccid, reason string) error {
	l.mu.Lock()
	st, ok := l.calls[ccid]
	if ok {
		delete(l.calls, ccid)
	}
	l.mu.Unlock()
	if !ok {
		// A ccid this leg never opened is a named refusal; a ccid it
		// already reaped is the idempotent nil the contract pins. The
		// prefix distinguishes the two: only "local-*" ids can ever be
		// ours, so a re-End of a local-shaped id is the idempotent path.
		if !isLocalCcid(ccid) {
			return fmt.Errorf("%w: %s", ErrUnknownLocalCall, ccid)
		}
		return nil
	}

	// Fan-out FIRST (phone-path order: the manager owns idempotent
	// teardown and fires the release hook exactly once), then the
	// leg-local teardown. The childproc note is NOT duplicated here:
	// in production the wiring's releaseCall (the manager's release
	// hook) already notes the end for this ccid, and the once-per-outcome
	// contract is the manager's. The wiring's releaseCall covers local
	// ccids exactly like phone ccids because TrackOutbound registered
	// the ccid with the manager (step 4).
	l.deps.Manager.FailOutcome(ccid, OutcomeHangup, reason)
	st.cancel()
	if err := st.pair.Close(); err != nil {
		l.log.Debug("voice: local leg: pair close", "ccid", ccid, "err", err)
	}
	if st.cliPair != nil {
		if err := st.cliPair.Close(); err != nil {
			l.log.Debug("voice: local leg: client pair close", "ccid", ccid, "err", err)
		}
	}
	// Control lane down LAST-ish but before the log line: unregister +
	// close the ev listener (removes <ccid>.c.ev). A CLI still reading
	// the lane sees socket EOF; no clear can be emitted after this (the
	// splice is already cancelled, so routeFrame's latch is a no-op).
	unregisterClientEvents(ccid)
	l.log.Info("voice: local call ended", "ccid", ccid, "reason", reason)
	return nil
}

// Live snapshots the live local calls (leaf 08's voice.local.status
// reads this).
func (l *LocalLeg) Live() []LocalCall {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]LocalCall, 0, len(l.calls))
	for _, st := range l.calls {
		out = append(out, LocalCall{
			CallID:    st.ccid,
			SessionID: st.sessionID,
			InPath:    pairInPath(st.cliPair),
			OutPath:   pairOutPath(st.cliPair),
			EvPath:    st.ev.Path(),
		})
	}
	return out
}

// isLocalCcid reports whether ccid carries the local-leg prefix shape.
func isLocalCcid(ccid string) bool {
	const prefix = "local-"
	return len(ccid) > len(prefix) && ccid[:len(prefix)] == prefix
}

// isNotConnected reports whether err is the AudioPair's pre-accept
// "not connected" shape. The phone splice polls this condition
// (splice.go's pump); the local splice surfaces it as a terminal read
// error, so the leg's teardown filters it: during the pre-dial window
// the pair is healthy, just not yet joined. The JoinTimeout grace still
// bounds a never-joined leg (ErrNoClient is a distinct error and stays
// terminal).
func isNotConnected(err error) bool {
	return err != nil && strings.Contains(err.Error(), "not connected")
}

// rawTextSender adapts *TextServer onto TextSender (the splice's raw
// seam): frames are decoded then re-sent through the Frame-typed Send,
// the same shape as the wiring's rawFrameSender.
type rawTextSender struct {
	srv *TextServer
}

// Send implements TextSender.
func (s rawTextSender) Send(raw []byte) error {
	f, err := DecodeFrame(raw)
	if err != nil {
		return fmt.Errorf("voice: local leg: decode frame for child: %w", err)
	}
	return s.srv.Send(f)
}

// localClientIO adapts the AudioPairs onto the io.ReadWriter the local
// splice wants. TWO pairs stay the transport: the CHILD dials the main
// pair (<ccid>.in/.out, one accept each - its conn is the audio side);
// the CLIENT dials its own pair (<ccid>.cli.in/.cli.out). The daemon's
// accepted ends on the CLIENT pair are the client-facing surface: the
// .cli.in accept carries mic bytes client -> daemon, the .cli.out accept
// carries speaker bytes daemon -> client. The child pair paths never
// leave the daemon; open RPC/LocalCall expose only the client pair.
type localClientIO struct {
	cli *AudioPair // the client pair (mic in / speaker out)
}

// Read reads client mic bytes from the pair's accepted .cli.in conn (the
// client dialed the client InPath and writes its mic there).
func (c localClientIO) Read(p []byte) (int, error) {
	return c.cli.ReadFromMic(p)
}

// Write writes paced child PCM to the client over the pair's accepted
// .cli.out conn (the client dialed the client OutPath and reads its
// speaker there).
func (c localClientIO) Write(p []byte) (int, error) {
	return c.cli.WriteToClient(p)
}

// Compile-time: localClientIO satisfies the splice's client seam.
var _ io.ReadWriter = localClientIO{}
