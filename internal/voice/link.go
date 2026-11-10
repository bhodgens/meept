package voice

// link.go - the two-agent voice link (deadair-link plan leaf 03,
// docs/plans/20261110-voice-deadair-link/03-voice-link.md): cross-connect
// TWO local-leg calls in-process so one employee's spoken PCM becomes the
// other employee's microphone and vice versa. Real TTS -> real socket PCM
// -> real STT -> real employee turns on both sides; no human, no ffmpeg,
// no oto. This is the loop-testing harness for the jitter + dead-air
// acceptance.
//
// Architecture (in-daemon bridge; the CLI never touches audio):
//
//   - LocalLeg.Open is used UNCHANGED (leaf 03 touches no local.go line).
//     Two calls open with labels "link-a-<id>" / "link-b-<id>" so the
//     registry targets ("local:link-a-...", "local:link-b-...") stay
//     distinct from each other AND from phone/local: labels; NormalizeE164
//     never runs for local labels (dial.go leg rules, C-registry/CL6).
//   - The bridge then dials BOTH client pair sockets for each leg exactly
//     like the CLI does (net.Dial("unix", InPath/OutPath)): A's OutPath
//     conn READS A's speaker bytes, B's InPath conn WRITES B's mic bytes,
//     and io.Copy crosses them; the second pump mirrors the direction.
//     Each leg sees precisely what a human client would - no splice,
//     frame, or registry change.
//   - Close ends both calls (LocalLeg.End, idempotent), cancels the pump
//     ctx, and closes the four dialed conns.
//   - Optional audible monitor (Open opts.Monitor): two extra unix
//     listeners <pair dir>/<link id>.mon-a / .mon-b carry a NON-BLOCKING
//     tee of each pump direction so the operator's CLI can play BOTH
//     employees through speakers. The tap drops bytes whenever no
//     client is attached or a tap write fails - monitor traffic NEVER
//     stalls or gates the bridge itself.
//
// Deliberate link-mode notes (pinned here, not accidents):
//
//   - Barge-in. The local leg's child-side RMS barge-in is driven by the
//     voice.local.barge_in_rms knob baked into LocalLegConfig AT wiring
//     construction (internal/daemon/voice_wiring.go NewLocalLeg) - there
//     is NO per-call override seam on LocalLeg.Open (the variadic is the
//     TtsVoice arg only). So machine speech arriving at the OTHER leg's
//     mic CAN trip that leg's RMS window. The trip is STT-gated with
//     one-shot evidence (AGENTS.md C11: a trip zeroes lastFinalAt, a
//     second window needs a NEW final transcript, and the echo guard
//     requires window energy > 130% of the last outbound chunk), so a
//     real transcript interrupts and the interrupted chain re-forms from
//     what the other employee said - which is exactly the conversational
//     semantics a link wants. Where a run must NOT interrupt, the
//     operator applies the config-level workaround (voice.employee_leg
//     rows / voice.local.barge_in_rms=0); leaf 03 deliberately ships no
//     per-call knob because the seam does not exist and local.go is
//     frozen for this leaf.
//   - Greetings. The half-duplex coordinator (turn-taking leaf) hands
//     side B a SuppressGreeting loop: exactly ONE side (A) greets on
//     connect, and A's greeting audio crossing into B's mic is the
//     conversation SEED prompt for B (both-greeting was the live-run
//     defect link-6a3f3b65: two employees saying hello at once). The
//     splice's client-join grace (a first mic byte within JoinTimeout,
//     default 10 s) is satisfied by the bridge's deterministic silence
//     seed at dial (linkSilenceSeedChunks) - with B silent on open, A's
//     mic would otherwise starve until B's first LLM reply, which a
//     slow model could push past the gate.
//   - The silence reaper (45 s talk-loop timer) stays ENABLED: it is the
//     dead-air circuit breaker, and the acceptance run must prove it
//     fires or never has to.
//   - Registry capacity: max_concurrent_calls caps the instance; a link
//     consumes TWO slots. With the default cap 2 a link fills the
//     registry (no further calls until it closes).
//
// Teardown rides LocalLeg.End -> Manager.FailOutcome -> the normal
// release fan-out (registry release, binding end, childproc note) exactly
// once per call; the link adds only the conn closes and pump cancel.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caimlas/meept/pkg/id"
)

// maxMonitorSocketPathLen bounds a monitor socket path's byte length.
// Unix sockaddr_un sun_path is 103 usable bytes on macOS (socket.go C2
// note); the monitor suffix is checked at CREATE time and a longer path
// is the named ErrLinkMonitorPathTooLong refusal (zero side effects:
// no listener is created, both legs are unwound).
const maxMonitorSocketPathLen = 100

// ErrLinkMonitorPathTooLong is the named Monitor refusal when the
// derived monitor socket path (<pair dir>/<link id>.mon-a|.mon-b) would
// overflow the kernel sun_path limit.
var ErrLinkMonitorPathTooLong = errors.New("voice: voice link: monitor socket path exceeds the unix sun_path limit")

// linkFloorFallbackDefault is the FALLBACK drain window the half-duplex
// coordinator arms on a side's turn_final when the audio-drain signal
// never arrives (splice gone, seam nil, lost race, old child binary with
// no played edge). The real release is drain-driven: turn_final sets the
// splice's EXPECTED-final state (LocalSpliceFor(ccid).ExpectFinalDrain),
// the child's C1 played edge arms the watch (MarkFinalPlayed), and the
// AudioDrained fire releases the floor after linkFloorTailPad. turn_final
// alone no longer frees the floor after the short legacy 600 ms, because
// the daemon pacer can still hold seconds of that turn's paced audio
// (measured on link-1108d776b0a1efb3: 335 talk-over windows, 6.70 s
// total), and BLIND-arming the watch at turn_final was still early — the
// child's inter-sentence synth gaps (1.4-3.8 s) exceed the quiescence
// window and fake a mid-reply drain (link-2fdc3f5fd8d81805: 10.94 s
// overlap). A stale extra hold is harmless; early release is the bug.
//
// 12 s, not the original 4 s: measured tails run 5-10 s (link-
// 6eb490abd8349958: child cadence write_ms=11136 on one turn — synth
// backlog + paced audio AFTER the daemon-side turn_final is real, since
// turn_final means the last say FRAME left the daemon, not that the
// child finished playing). A 4 s watchdog beat the drain edge to the
// release on every long turn (all 5 margin releases landed at exactly
// +4 s while audio still paced), reintroducing the talk-over the drain
// seam exists to kill. The drain must be the NORMAL release path; the
// margin only bounds a dead-splice worst case.
const linkFloorFallbackDefault = 12 * time.Second

// linkFloorTailPad is the small gap added after the audio-drain edge
// before the floor releases: the last paced chunk plus bridge transit
// must fully leave the peer's mic path before the parked listener
// dispatches.
const linkFloorTailPad = 250 * time.Millisecond

// linkSilenceSeedChunks is the mic-join seed length in exact 640 B
// L16@16k chunks (5 chunks = 100 ms of digital silence per side).
// Enough to satisfy the splice's first-byte join gate, short enough to
// stay inside a unix socket buffer alongside the first pumped speech
// (the pumps must never block on an unread seeded mic).
const linkSilenceSeedChunks = 5

// Link turn side ids - side A greets, side B opens silent (the
// single-greeting contract: A's greeting audio crosses into B's mic and
// arrives as B's first final prompt, the conversation seed).
const (
	LinkSideA = "a"
	LinkSideB = "b"
)

// linkTurnSideState is one side's half-duplex slot: its bound talk loop
// (nil until the wiring's routeFrame constructs it, and again on
// teardown), whether it is mid-turn (busy: turn_start..turn_final),
// whether it still HOLDS THE AUDIO FLOOR (floorHeld: turn_start..floor
// release — turn_final alone does NOT clear it because the daemon pacer
// may still hold seconds of that turn's paced audio), the ccid of the
// side's last turn (the AudioDrained signal resolves by ccid), and the
// armed floor-release timer. releaseGen is the timer identity: every
// re-arm bumps it, and a stale timer's callback fire (Stop racing a
// buffered signal) compares its captured gen and no-ops, so a
// superseded margin can never release on the wrong edge.
type linkTurnSideState struct {
	loop         *TalkLoop
	busy         bool
	floorHeld    bool
	ccid         string
	seq          uint64
	releaseTimer *time.Timer
	releaseGen   uint64
}

// LinkTurnCoordinator enforces one-speaker-at-a-time on ONE voice link
// (turn-taking leaf, live-run defect link-6a3f3b65: two independent
// talk loops greeted at once and each side's STT dispatched while the
// other was still speaking -> simultaneous audio + half-answers;
// link-1108d776b0a1efb3: releasing the floor on turn_final + 600 ms
// still let side B start while A's pacing tail ran -> 335 talk-over
// windows, 6.70 s total).
//
// Contract:
//   - at most ONE side holds the floor (mid-turn OR draining its
//     audio tail) at a time: the peer's final prompt parks while
//     floorHeld is set — both the parked-prompt path AND the fresh
//     claim path check the floor;
//   - the floor releases on AUDIO DRAIN, not on the turn_final text
//     edge: turn_final sets the splice's EXPECTED-final state
//     (LocalSpliceFor(ccid).ExpectFinalDrain) AND arms a long FALLBACK
//     margin timer; the child's C1 played edge then arms the watch
//     (MarkFinalPlayed), and the first of (drain edge + linkFloorTailPad)
//     or (fallback fire) releases the floor and flushes the peer's parked
//     prompt. An idle edge (barge-in cancel, unbind, teardown) releases
//     IMMEDIATELY — nothing is playing;
//   - side B never greets (TalkLoopConfig.SuppressGreeting honored by
//     the wiring when it constructs B's loop).
//
// Thread shape: every method takes c.mu ONLY for state swaps and never
// invokes a callback or performs I/O under it (repo mutex-scope rule).
// The flush edge into a loop is TalkLoop.FlushHeld - a non-blocking send
// on a buffered channel; the parked dispatch always runs on the loop's
// own Run goroutine. ExpectFinalDrain / MarkFinalPlayed on the splice
// take localSpliceMu + the splice's own mutex briefly - never under c.mu.
type LinkTurnCoordinator struct {
	id     string
	log    *slog.Logger
	margin time.Duration // fallback floor-release window after turn_final
	pad    time.Duration // release delay after the drain edge (linkFloorTailPad)

	mu     sync.Mutex
	a      linkTurnSideState
	b      linkTurnSideState
	closed bool // link Closed/unwound: parks DROP at loop exit, never dispatch into a dying peer
}

func NewLinkTurnCoordinator(linkID string, fallbackMargin time.Duration, logger *slog.Logger) *LinkTurnCoordinator {
	if fallbackMargin <= 0 {
		fallbackMargin = linkFloorFallbackDefault
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &LinkTurnCoordinator{id: linkID, log: logger.With("sub", "link-turn", "link_id", linkID), margin: fallbackMargin, pad: linkFloorTailPad}
}

// side returns the state slot for one side id. Callers hold c.mu.
func (c *LinkTurnCoordinator) sideLocked(side string) *linkTurnSideState {
	if side == LinkSideB {
		return &c.b
	}
	return &c.a
}

func (c *LinkTurnCoordinator) peerLocked(side string) *linkTurnSideState {
	if side == LinkSideB {
		return &c.a
	}
	return &c.b
}

// Bind attaches a constructed talk loop to one side (the wiring calls it
// right after the loop registers). Re-binding replaces the loop (a
// duplicate setup was already refused upstream; this is the defensive
// shape, not a live path).
func (c *LinkTurnCoordinator) Bind(side string, loop *TalkLoop) {
	c.mu.Lock()
	st := c.sideLocked(side)
	st.loop = loop
	c.mu.Unlock()
}

// Unbind detaches a side at loop exit: the side goes idle (it will never
// finish another turn, and no audio tail can still be pacing on its
// behalf - the leg teardown closes the pair), its floor-release timer
// stands down, and the PEER's parked prompt is released immediately - a
// prompt held by a dead side would otherwise wait forever. FlushHeld on
// a dead peer loop is a no-op (the loop's Run already dropped its park
// with the prompt_dropped telemetry line).
func (c *LinkTurnCoordinator) Unbind(side string) {
	c.mu.Lock()
	st := c.sideLocked(side)
	st.loop = nil
	st.busy = false
	st.floorHeld = false
	st.ccid = ""
	if st.releaseTimer != nil {
		st.releaseTimer.Stop()
		st.releaseTimer = nil
	}
	st.releaseGen++ // any armed release from this side is now stale
	peer := c.peerLocked(side)
	peerLoop := peer.loop
	release := peerLoop != nil && !c.closed
	c.mu.Unlock()
	if release {
		// The link is still alive: a dead side can no longer answer, so
		// the peer's parked prompt must not strand - flush it. On a
		// CLOSED coordinator the park instead rides the dying peer loop's
		// own exit drop (never dispatched into a torn-down call).
		peerLoop.FlushHeld()
	}
	c.log.Info("voice: link turn: side unbound", "ccid_side", side)
}

// SuppressesGreeting is the single-greeting rule the wiring consults
// when constructing a side's loop: side A greets, side B opens silent.
func (c *LinkTurnCoordinator) SuppressesGreeting(side string) bool {
	return side == LinkSideB
}

// HoldPromptFor returns the side's TalkDeps.HoldPrompt closure (nil on a
// nil coordinator: plain local/phone calls never bind here). Allow is a
// CLAIM, not just a vote: when the peer holds NEITHER the turn NOR the
// audio floor, this side marks itself busy + floor-held BEFORE returning
// true, so two prompts arriving at both sides near-simultaneously cannot
// each observe an idle peer and dispatch together - the loser of the
// mutex race parks instead. The floor check (floorHeld) is the
// talk-over fix: under the old busy-only rule a peer could CLAIM the
// floor the moment turn_final fired, while seconds of paced audio from
// the finished turn were still leaving the bridge (measured 6.70 s of
// both-sides-audible on link-1108d776b0a1efb3). The claim is confirmed
// by the loop's own turn_start (same side, idempotent) and cleared by
// every terminal edge (floor release after drain/margin, idle, unbind);
// a claimed dispatch always runs synchronously in the loop's
// EventPrompt branch right after this returns true.
func (c *LinkTurnCoordinator) HoldPromptFor(side string) func(Instance, func()) bool {
	if c == nil {
		return nil
	}
	return func(call Instance, _ func()) bool {
		c.mu.Lock()
		peer := c.peerLocked(side)
		if peer.busy || peer.floorHeld {
			c.mu.Unlock()
			c.log.Info("voice: link turn: hold decision", "side", side,
				"kind", "hold_decision", "ccid", call.CallControlID, "peer_side_held", true)
			return false
		}
		st := c.sideLocked(side)
		st.busy = true
		st.floorHeld = true
		st.ccid = call.CallControlID
		c.mu.Unlock()
		return true
	}
}

// TurnStateFor returns the side's TalkDeps.TurnState closure - the
// half-duplex observation edge (turn_start -> this side is the speaker
// and holds the floor; turn_final -> its say stream flushed TEXT-side,
// arm the splice drain watch + the long fallback margin - the floor
// stays held until one of them releases it; idle -> it stopped speaking
// without a final, release the peer now). A second turn_start on the
// same side (supersede inside one side) cancels a previously armed
// release: the side is speaking again.
func (c *LinkTurnCoordinator) TurnStateFor(side string) func(ccid string, seq uint64, kind string) {
	if c == nil {
		return nil
	}
	return func(ccid string, seq uint64, kind string) {
		c.mu.Lock()
		st := c.sideLocked(side)
		peer := c.peerLocked(side)
		switch kind {
		case TurnKindStart:
			st.busy = true
			st.floorHeld = true
			st.ccid = ccid
			st.seq = seq
			if st.releaseTimer != nil {
				st.releaseTimer.Stop()
				st.releaseTimer = nil
			}
			st.releaseGen++ // invalidate any armed release from this side
			c.mu.Unlock()
		case TurnKindFinal:
			st.busy = false
			if st.releaseTimer != nil {
				st.releaseTimer.Stop()
				st.releaseTimer = nil
			}
			gen := st.releaseGen + 1
			st.releaseGen = gen
			// floorHeld STAYS set: the pacer still owns this turn's
			// audio tail. The release arms below.
			c.mu.Unlock()
			// Expect the splice-side played edge OFF the lock: resolve the
			// live local splice by ccid (the same package-level registry
			// HandleInterrupt rides) and record that THIS turn's drain
			// rides the child's C1 played edge (EventPlayed, sent after
			// the final byte of the last:true stream left the .out
			// socket), which arms the watch through MarkFinalPlayed.
			//
			// NO BLIND ARM anymore (link-2fdc3f5fd8d81805): at turn_final
			// the child still has queued sentences synthesizing, and the
			// inter-sentence gaps (measured 1.4-3.8 s) exceed the watch's
			// quiescence window — the blind arm fired MID-reply and the
			// peer started talking over the tail (60 short both-audible
			// runs, 10.94 s overlap). The played edge is the true end-of-
			// turn marker; a turn whose edge never arrives (old child
			// binary, aborted/denied final stream) rides the fallback
			// margin below — late, never early. A plain local call's
			// splice has no AudioDrained dep -> expect is a no-op; no
			// splice (already torn down) -> nil receiver. Either way the
			// margin still bounds the hold.
			//
			// Instrumentation: when the drain signal cannot fire for
			// this turn — no splice under the ccid, or the splice's drain
			// deps are unwired / the splice ended — WARN so the next
			// live run distinguishes "never armed" from "armed, never
			// quiescent" without log archaeology.
			sp := LocalSpliceFor(ccid)
			if !sp.ExpectFinalDrain() {
				reason := "no live splice for ccid"
				if sp != nil {
					reason = "splice drain deps unwired/ended"
				}
				c.log.Warn("voice: link turn: drain watch NOT armed",
					"side", side, "kind", "drain_arm_missed", "reason", reason, "ccid", ccid, "seq", seq)
			}
			c.mu.Lock()
			if st.releaseGen == gen && !st.busy {
				// Fallback margin: protects against the drain signal
				// never firing (splice died mid-turn, child stalled
				// without audio). Stale-drain guard: the fire compares
				// the generation, so a turn_start on this side between
				// arm and fire invalidates it.
				st.releaseTimer = time.AfterFunc(c.margin, func() {
					c.releaseFloor(side, ccid, seq, gen, "margin")
				})
			}
			c.mu.Unlock()
		case TurnKindIdle:
			st.busy = false
			st.floorHeld = false
			if st.releaseTimer != nil {
				st.releaseTimer.Stop()
				st.releaseTimer = nil
			}
			st.releaseGen++ // no pending release rides an idle side
			peerLoop := peer.loop
			c.mu.Unlock()
			if peerLoop != nil {
				peerLoop.FlushHeld()
			}
		default:
			c.mu.Unlock()
		}
	}
}

// NoteAudioDrained is the floor's audio-truth release input: the local
// splice fires it (wired per-call through LocalDeps -> the daemon
// wiring's hook -> here) once the turn's last paced chunk has been
// WRITTEN to the client socket and the child stream went quiet. A
// stale/foreign ccid (no side holds it, or the side went mid-turn
// again) is a no-op. Matching drains schedule the release after
// linkFloorTailPad - the tiny transit gap so the last chunk clears the
// bridge before the peer may speak.
func (c *LinkTurnCoordinator) NoteAudioDrained(ccid string) {
	if c == nil || ccid == "" {
		return
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	var match *linkTurnSideState
	var side string
	for _, s := range []struct {
		id string
		st *linkTurnSideState
	}{{LinkSideA, &c.a}, {LinkSideB, &c.b}} {
		st := s.st
		if st.ccid == ccid && st.floorHeld && !st.busy {
			match = st
			side = s.id
		}
	}
	if match == nil {
		c.mu.Unlock()
		return
	}
	seq := match.seq
	if match.releaseTimer != nil {
		match.releaseTimer.Stop()
	}
	gen := match.releaseGen + 1
	match.releaseGen = gen
	match.releaseTimer = time.AfterFunc(c.pad, func() {
		c.releaseFloor(side, ccid, seq, gen, "drained")
	})
	c.mu.Unlock()
	c.log.Info("voice: link turn: drain noted", "side", side,
		"kind", "floor_drain_noted", "ccid", ccid, "seq", seq, "pad_ms", c.pad.Milliseconds())
}

// releaseFloor is the single floor-release funnel: the pad-after-drain
// and the fallback-margin timers both route here. It re-checks under
// the lock that the fire belongs to the LIVE arm (generation match,
// side not mid-turn again, floor still held), clears the floor, logs
// the telemetry edge (kind=floor_released, reason=drained|margin), and
// flushes the peer's parked prompt. The release itself is idempotent
// (FlushHeld is a buffered non-blocking signal and releaseHeld no-ops
// with nothing parked).
func (c *LinkTurnCoordinator) releaseFloor(side, ccid string, seq, gen uint64, reason string) {
	c.mu.Lock()
	st := c.sideLocked(side)
	stillCurrent := st.releaseGen == gen && !st.busy && st.floorHeld && !c.closed
	if stillCurrent {
		st.floorHeld = false
		st.releaseTimer = nil
	}
	peerLoop := c.peerLocked(side).loop
	c.mu.Unlock()
	if !stillCurrent {
		return
	}
	c.log.Info("voice: link turn: floor released", "side", side,
		"kind", "floor_released", "reason", reason, "ccid", ccid, "seq", seq)
	if peerLoop != nil {
		peerLoop.FlushHeld()
	}
}

// LinkTurnRegistry maps a live call's REGISTRY TARGET (the
// deterministic "local:link-a-<suffix>" / "local:link-b-<suffix>" the
// leg records) to its coordinator + side. The pre-registration rides
// the target, NOT the ccid, because the child's setup frame can reach
// the daemon's frame router while LinkManager.Open is still executing -
// by target both sides are knowable BEFORE either leg opens (the suffix
// is minted in Open). Non-link calls (phone, plain local) never match:
// Lookup returns nil and the wiring builds a legacy uncoordinated loop.
type LinkTurnRegistry struct {
	mu       sync.Mutex
	byTarget map[string]linkedTurnSide
	log      *slog.Logger
}

type linkedTurnSide struct {
	coord *LinkTurnCoordinator
	side  string
}

// NewLinkTurnRegistry constructs the shared registry. A nil logger falls
// back to slog.Default.
func NewLinkTurnRegistry(logger *slog.Logger) *LinkTurnRegistry {
	if logger == nil {
		logger = slog.Default()
	}
	return &LinkTurnRegistry{
		byTarget: make(map[string]linkedTurnSide),
		log:      logger,
	}
}

// Register pre-binds one link side's target to its coordinator. Safe to
// call before the leg opens (that is the point). Re-registering a target
// overwrites (defensive; the registry's one-live-per-target rule makes
// collisions a stale-entry path only).
func (r *LinkTurnRegistry) Register(target, linkID, side string, coord *LinkTurnCoordinator) {
	if r == nil || target == "" || coord == nil {
		return
	}
	r.mu.Lock()
	r.byTarget[target] = linkedTurnSide{coord: coord, side: side}
	r.mu.Unlock()
}

// Lookup resolves a target to its coordinator + side (nil = no
// coordinator, the legacy construction path).
func (r *LinkTurnRegistry) Lookup(target string) (*LinkTurnCoordinator, string) {
	if r == nil || target == "" {
		return nil, ""
	}
	r.mu.Lock()
	entry, ok := r.byTarget[target]
	r.mu.Unlock()
	if !ok {
		return nil, ""
	}
	return entry.coord, entry.side
}

// Unregister drops a link's two targets, flushing both sides (a parked
// prompt must not sit on a dead coordinator). Idempotent.
func (r *LinkTurnRegistry) Unregister(targets ...string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	var coords []*LinkTurnCoordinator
	for _, t := range targets {
		if entry, ok := r.byTarget[t]; ok {
			delete(r.byTarget, t)
			seen := false
			for _, c := range coords {
				if c == entry.coord {
					seen = true
					break
				}
			}
			if !seen {
				coords = append(coords, entry.coord)
			}
		}
	}
	r.mu.Unlock()
	for _, c := range coords {
		c.Shutdown()
	}
}

// Shutdown marks the coordinator closed and stops every margin timer
// (link Close / unwind path). It does NOT flush parked prompts: both
// loops die moments later through the Leg.End fan-out, and each run's
// exit drops its own park with the prompt_dropped line - dispatching a
// held turn into a call the operator just closed would be a half-answer
// on a dead line.
func (c *LinkTurnCoordinator) Shutdown() {
	c.mu.Lock()
	c.closed = true
	for _, side := range []string{LinkSideA, LinkSideB} {
		st := c.sideLocked(side)
		if st.releaseTimer != nil {
			st.releaseTimer.Stop()
			st.releaseTimer = nil
		}
		st.busy = false
		st.floorHeld = false
		st.ccid = ""
		st.releaseGen++ // no pending release rides a dead coordinator
	}
	c.mu.Unlock()
}

// monitorWriteDeadline bounds one tap write. A monitor client that
// stops draining must NEVER stall the bridge: the write fails at the
// deadline and the tap drops permanently (logged once).
const monitorWriteDeadline = 250 * time.Millisecond

// monitorFailThreshold disarms a tap only after this many CONSECUTIVE
// write failures - one backpressured/slow write must not silence the
// operator's monitor for the rest of the link.
const monitorFailThreshold = 8

// ErrLinkSameEmployee is the named up-front refusal when both sides name
// the same employee: the registry's one-live-call-per-employee rule
// would reject the second Open anyway, but the link checks BEFORE any
// side effect so the harness gets a clear message instead of ErrBusy.
var ErrLinkSameEmployee = errors.New("voice: voice link: employee_a and employee_b must be different employees")

// ErrLinkNotFound is returned by Close/Status for a link id this manager
// never opened (and no longer retains).
var ErrLinkNotFound = errors.New("voice: voice link: unknown link")

const (
	// linkDialTimeoutDefault bounds one pair-socket dial.
	linkDialTimeoutDefault = 5 * time.Second
	// linkRetainEndedDefault keeps a CLOSED link's entry queryable so a
	// CLI that closes then polls once more still gets the final turn
	// counts and transcripts (the calls.db row survives; the entry only
	// carries routing ids).
	linkRetainEndedDefault = 30 * time.Minute
)

// LinkLegOpener is the leg seam the manager drives. The concrete
// *LocalLeg satisfies it; tests inject a fake. (Typed-nil note per the
// repo rule: callers MUST pass a non-nil concrete - the daemon adapter
// checks w.Local != nil before constructing the manager.)
type LinkLegOpener interface {
	Open(ctx context.Context, employeeID, label string, voiceOverride ...string) (LocalCall, error)
	End(ccid, reason string) error
}

// LinkOpenOptions tunes one Open. Zero value = the pre-monitor shape
// (byte-compatible: the Open variadic keeps old callers compiling).
type LinkOpenOptions struct {
	// Monitor stands up two unix listeners (one per side) that carry a
	// NON-BLOCKING tee of each direction's PCM to the operator's
	// speakers. The listener paths derive from the pair-socket dir:
	// <dir>/<linkID>.mon-a / .mon-b. A monitor client is OPTIONAL:
	// nothing ever stalls the bridge waiting for one, and tap bytes are
	// dropped while no client is attached or after a tap write error.
	Monitor bool
}

// LinkTranscriptReader is the call-store seam for per-side turn counts.
// *CallStore satisfies it (EndedCall is read-only and reads live rows
// just as well as ended ones - the name is historical). Nil degrades
// Status to zero counts with empty transcripts (never a failure).
type LinkTranscriptReader interface {
	EndedCall(ctx context.Context, callID string) (CallRecord, []TranscriptLine, bool, error)
}

// LinkDeps carries the injected edges. All optional except Leg
// (constructor-guarded): Store is the transcript reader, Dial replaces
// the unix dialer (tests), DialTimeout/RetainEnded tune the defaults.
type LinkDeps struct {
	Leg         LinkLegOpener
	Store       LinkTranscriptReader
	Dial        func(ctx context.Context, path string) (net.Conn, error)
	DialTimeout time.Duration
	RetainEnded time.Duration
	// Turns is the half-duplex coordinator registry shared with the
	// daemon's frame router: the wiring's VoiceWiring owns the registry
	// (it is the only component that constructs talk loops, on setup)
	// and passes the SAME instance here, so a link's two talk loops
	// resolve their coordinator by registry target at construction.
	// Nil: the manager constructs its own private registry - links stay
	// half-duplex among themselves but nothing in the wiring can bind
	// loops to it (tests-only shape; production wiring always shares).
	Turns *LinkTurnRegistry
	// TurnMargin is the FALLBACK floor-release window the half-duplex
	// coordinator arms on a side's turn_final when the audio-drain
	// signal never fires (splice died mid-turn, no played edge from an
	// old child binary, aborted final stream). The primary release is
	// drain-driven: the local splice's ExpectFinalDrain at turn_final ->
	// the C1 played edge arms MarkFinalPlayed -> AudioDrained edge ->
	// floor after linkFloorTailPad. Zero = linkFloorFallbackDefault (12 s).
	TurnMargin time.Duration
	Logger     *slog.Logger
}

// LinkSide is one side's live status: routing ids plus turn counts read
// through the transcript seam.
type LinkSide struct {
	CallID         string
	EmployeeID     string
	AssistantTurns int
	UserTurns      int
	Transcript     []TranscriptLine
}

// LinkStatusResult is one link's status snapshot (both sides + ended).
type LinkStatusResult struct {
	LinkID string
	Ended  bool
	A      LinkSide
	B      LinkSide
}

// linkEntry is the manager's per-link state. Conns and ctx are set once
// at Open; mu (the manager's) guards the ended flag and retention clock.
// monA/monB are the optional monitor listeners (nil when Monitor=false);
// the tapA/tapB writers read the accepted conn through each listener's
// own mutex, never the manager's.
type linkEntry struct {
	id         string
	aCall      LocalCall
	bCall      LocalCall
	aEmployee  string
	bEmployee  string
	aMic       net.Conn // A's client InPath - the bridge WRITES B's speech here
	aSpk       net.Conn // A's client OutPath - the bridge READS A's speech from here
	bMic       net.Conn
	bSpk       net.Conn
	cancel     context.CancelFunc
	pumps      sync.WaitGroup
	ended      bool
	endedAt    time.Time
	closeConns sync.Once
	monA       *monitorListener     // tee sink for the a->b direction
	monB       *monitorListener     // tee sink for the b->a direction
	turns      *LinkTurnCoordinator // half-duplex coordinator (never nil)
	aTarget    string               // registry targets the coordinator
	bTarget    string               // is registered/unregistered under
}

// tapA/tapB hand the pump the tee sink for one direction. A nil
// listener (Monitor off) yields a nil tap, which pumpStream skips
// entirely - the zero-monitor path stays the plain io.Copy it always
// was.
func (e *linkEntry) tapA() *monitorTap {
	if e.monA == nil {
		return nil
	}
	return &monitorTap{ln: e.monA}
}

func (e *linkEntry) tapB() *monitorTap {
	if e.monB == nil {
		return nil
	}
	return &monitorTap{ln: e.monB}
}

// monitorListener is one direction's monitor unix listener: a single
// accept goroutine stores exactly ONE accepted conn (set-once; a second
// client is accepted and immediately closed - the pair-socket posture
// from AudioPair, mirrored). close() is idempotent.
type monitorListener struct {
	path string
	ln   net.Listener

	mu     sync.Mutex
	conn   net.Conn // set once by the accept goroutine; nil until a client dials
	closed bool
}

func newMonitorListener(path string) (*monitorListener, error) {
	// Stale files from a crashed previous daemon must not wedge Listen
	// (NewAudioPair's defensive remove, mirrored).
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("voice: voice link: remove stale monitor socket %s: %w", path, err)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("voice: voice link: listen monitor socket %s: %w", path, err)
	}
	// Owner-only: the tap carries call audio, the pair sockets' dir-mode
	// posture applies. A failed chmod is cosmetic (the dir is already
	// user-owned), not fatal - mirror AudioPair's tolerance.
	_ = os.Chmod(path, 0o600)
	ml := &monitorListener{path: path, ln: ln}
	go func() {
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			// Listener closed during teardown: the expected path.
			return
		}
		ml.mu.Lock()
		if ml.closed || ml.conn != nil {
			ml.mu.Unlock()
			_ = conn.Close()
			return
		}
		ml.conn = conn
		ml.mu.Unlock()
		// A second client still needs retiring: after the first is
		// stored, keep accepting ONLY to close extras fast (no backlog
		// of half-open monitor conns).
		for {
			extra, err := ln.Accept()
			if err != nil {
				return
			}
			_ = extra.Close()
		}
	}()
	return ml, nil
}

// current returns the accepted conn (nil when no client has dialed).
func (ml *monitorListener) current() net.Conn {
	ml.mu.Lock()
	defer ml.mu.Unlock()
	return ml.conn
}

// close tears down the listener + accepted conn and removes the socket
// file. Idempotent; safe concurrently with the accept goroutine.
func (ml *monitorListener) close() {
	ml.mu.Lock()
	if ml.closed {
		ml.mu.Unlock()
		return
	}
	ml.closed = true
	conn := ml.conn
	ml.conn = nil
	ln := ml.ln
	ml.mu.Unlock()
	if ln != nil {
		_ = ln.Close()
	}
	if conn != nil {
		_ = conn.Close()
	}
	_ = os.Remove(ml.path)
}

// monitorTap is pumpStream's tee destination for one direction. It is
// NOT an io.Writer with normal semantics: a Write NEVER returns an error
// the pump should act on (the bridge must not stall or die over monitor
// audio). Instead it resolves the accepted conn per write, bounds every
// write with monitorWriteDeadline, and on ANY failure (no conn yet,
// deadline, socket error) marks itself dead - later Writes become
// no-ops, and the drop is logged once by pumpStream's observer. Bytes
// written before a client dials are simply gone (the monitor is a live
// tap, not a recorder - the bytes-may-drop contract).
type monitorTap struct {
	ln    *monitorListener
	dead  atomic.Bool
	fails atomic.Uint32 // consecutive write failures (reset on success)
}

func (t *monitorTap) Write(p []byte) (int, error) {
	if t == nil || t.dead.Load() {
		return len(p), nil
	}
	conn := t.ln.current()
	if conn == nil {
		// No monitor client yet: drop silently (attaching is
		// on-demand; this is not an error state).
		return len(p), nil
	}
	_ = conn.SetWriteDeadline(time.Now().Add(monitorWriteDeadline))
	n, err := conn.Write(p)
	if err != nil {
		// Stale/unread/backpressured monitor conn: drop THIS batch and
		// disarm only after a run of failures (the monitor client may
		// reschedule and catch up - one slow write must not silence the
		// rest of the link; a permanently dead socket keeps failing and
		// disarms via the threshold, logged once by the pump).
		if t.fails.Add(1) >= monitorFailThreshold {
			t.dead.Store(true)
		}
		return n, err
	}
	t.fails.Store(0)
	return n, nil
}

// Link is one live link handle: both ccids plus Close/Status sugar.
// MonitorAPath/MonitorBPath name the monitor tee sockets when
// LinkOpenOptions.Monitor was set ("" otherwise); a CLI monitor client
// dials them like any pair socket. The a->b direction (A's speech into
// B's mic) rides MonitorAPath; b->a rides MonitorBPath.
type Link struct {
	ID           string
	ACCID        string
	BCCID        string
	AEmployee    string
	BEmployee    string
	MonitorAPath string
	MonitorBPath string

	mgr *LinkManager
}

// Close tears the link down (idempotent; a second Close is nil).
func (l *Link) Close(reason string) error { return l.mgr.Close(l.ID, reason) }

// Status snapshots both sides' turn counts + transcripts.
func (l *Link) Status(ctx context.Context) (LinkStatusResult, error) {
	return l.mgr.Status(ctx, l.ID)
}

// LinkManager owns the bridge. All mutable state is mutex-guarded; deps
// are immutable after construction.
type LinkManager struct {
	deps        LinkDeps
	log         *slog.Logger
	dialTimeout time.Duration
	retain      time.Duration
	turns       *LinkTurnRegistry
	turnMargin  time.Duration

	mu    sync.Mutex
	links map[string]*linkEntry
}

// NewLinkManager constructs the manager. A nil leg is a construction
// failure (the bridge is meaningless without it); every other zero
// field takes its default.
func NewLinkManager(deps LinkDeps) (*LinkManager, error) {
	if deps.Leg == nil {
		return nil, errors.New("voice: voice link: local leg is required")
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	m := &LinkManager{
		deps:        deps,
		log:         deps.Logger,
		dialTimeout: deps.DialTimeout,
		retain:      deps.RetainEnded,
		turns:       deps.Turns,
		turnMargin:  deps.TurnMargin,
		links:       make(map[string]*linkEntry),
	}
	if m.turns == nil {
		// Private registry: links are half-duplex among themselves but
		// nothing outside the manager can resolve their coordinators
		// (tests; production wiring always shares VoiceWiring's).
		m.turns = NewLinkTurnRegistry(deps.Logger)
	}
	if m.turnMargin <= 0 {
		m.turnMargin = linkFloorFallbackDefault
	}
	if m.dialTimeout <= 0 {
		m.dialTimeout = linkDialTimeoutDefault
	}
	if m.retain <= 0 {
		m.retain = linkRetainEndedDefault
	}
	if m.deps.Dial == nil {
		m.deps.Dial = m.dialUnix
	}
	return m, nil
}

// dialUnix is the default dialer: exactly the CLI's
// net.Dial("unix", path) pattern, ctx-bounded.
func (m *LinkManager) dialUnix(ctx context.Context, path string) (net.Conn, error) {
	d := net.Dialer{Timeout: m.dialTimeout}
	dialCtx, cancel := context.WithTimeout(ctx, m.dialTimeout)
	defer cancel()
	conn, err := d.DialContext(dialCtx, "unix", path)
	if err != nil {
		return nil, fmt.Errorf("dial pair socket %s: %w", path, err)
	}
	return conn, nil
}

// Open cross-connects two local-leg calls. Refusal order (zero side
// effects before each): empty ids -> same employee. Every later failure
// unwinds fully (dialed conns closed, opened legs ended) so a partial
// link never leaks a live call.
//
// opts (variadic; first entry only) carries LinkOpenOptions. With
// Monitor=true the bridge additionally stands up two unix listeners in
// the pair-socket dir and tees each pump direction's PCM into them
// (see monitorTap below). The variadic keeps every pre-monitor caller
// byte-compatible.
func (m *LinkManager) Open(ctx context.Context, employeeA, employeeB string, opts ...LinkOpenOptions) (*Link, error) {
	var option LinkOpenOptions
	if len(opts) > 0 {
		option = opts[0]
	}
	if employeeA == "" || employeeB == "" {
		return nil, fmt.Errorf("%w: both employee ids are required", ErrNoEmployee)
	}
	if employeeA == employeeB {
		return nil, ErrLinkSameEmployee
	}

	linkID := id.Generate("link-")
	// Label suffix keeps two concurrent links from colliding on the
	// registry's one-live-per-TARGET rule while preserving the distinct
	// "link-a"/"link-b" shapes the leaf doc pins.
	suffix := linkID[len("link-"):]

	// Half-duplex coordinator (turn-taking leaf): registered under the
	// deterministic registry targets BEFORE either leg opens - the
	// child's setup frame can reach the daemon's frame router while Open
	// is still executing, and by target both sides are knowable now. The
	// wiring's routeFrame binds each constructed talk loop to its side
	// through this registry (shared instance via LinkDeps.Turns).
	aTarget := "local:link-a-" + suffix
	bTarget := "local:link-b-" + suffix
	coord := NewLinkTurnCoordinator(linkID, m.turnMargin, m.log)
	m.turns.Register(aTarget, linkID, LinkSideA, coord)
	m.turns.Register(bTarget, linkID, LinkSideB, coord)

	aCall, err := m.deps.Leg.Open(ctx, employeeA, "link-a-"+suffix)
	if err != nil {
		m.turns.Unregister(aTarget, bTarget)
		return nil, fmt.Errorf("voice: voice link: open side a (%s): %w", employeeA, err)
	}
	bCall, err := m.deps.Leg.Open(ctx, employeeB, "link-b-"+suffix)
	if err != nil {
		// Unwind the first leg: End is idempotent, the release fan-out
		// does the rest. The unwind error never masks the refusal.
		m.turns.Unregister(aTarget, bTarget)
		if endErr := m.deps.Leg.End(aCall.CallID, "link: side b open failed"); endErr != nil {
			m.log.Debug("voice: link unwind: end side a", "ccid", aCall.CallID, "err", endErr)
		}
		return nil, fmt.Errorf("voice: voice link: open side b (%s): %w", employeeB, err)
	}

	// Monitor listeners BEFORE dialing (the pair dir comes from the
	// opened legs; both legs share SocketDir so either OutPath works).
	// A sun_path overflow refuses here with ZERO side effects beyond the
	// two legs, which the shared unwind below ends.
	var monA, monB *monitorListener
	var monAPath, monBPath string
	if option.Monitor {
		dir := filepath.Dir(aCall.OutPath)
		monAPath = filepath.Join(dir, linkID+".mon-a")
		monBPath = filepath.Join(dir, linkID+".mon-b")
		if len(monAPath) > maxMonitorSocketPathLen || len(monBPath) > maxMonitorSocketPathLen {
			m.turns.Unregister(aTarget, bTarget)
			m.unwindBoth(aCall, bCall, "link: monitor path too long")
			return nil, fmt.Errorf("%w: %s", ErrLinkMonitorPathTooLong, monAPath)
		}
		var lerr error
		monA, lerr = newMonitorListener(monAPath)
		if lerr != nil {
			m.turns.Unregister(aTarget, bTarget)
			m.unwindBoth(aCall, bCall, "link: monitor listen failed")
			return nil, fmt.Errorf("voice: voice link: %w", lerr)
		}
		monB, lerr = newMonitorListener(monBPath)
		if lerr != nil {
			monA.close()
			m.turns.Unregister(aTarget, bTarget)
			m.unwindBoth(aCall, bCall, "link: monitor listen failed")
			return nil, fmt.Errorf("voice: voice link: %w", lerr)
		}
	}

	conns, derr := m.dialAll(ctx, aCall, bCall)
	if derr != nil {
		m.turns.Unregister(aTarget, bTarget)
		m.closeConns(conns)
		if monA != nil {
			monA.close()
		}
		if monB != nil {
			monB.close()
		}
		m.unwindBoth(aCall, bCall, "link: dial failed")
		return nil, fmt.Errorf("voice: voice link: %w", derr)
	}

	pumpCtx, pumpCancel := context.WithCancel(context.Background())
	// Mic silence seed (turn-taking leaf): with B's greeting suppressed,
	// A's mic would receive NOTHING until B's first LLM reply is spoken
	// - and the local splice's JoinTimeout (first client byte, 10 s)
	// used to be satisfied by the OTHER side's greeting crossing the
	// bridge (link.go header note). A short exact-chunk run of zero PCM
	// into each mic joins both legs deterministically at dial time.
	// Safe by the local-leg contract: full 640 B chunks never tear the
	// splice's chunk framing (written BEFORE the pumps start, so nothing
	// interleaves), zero energy never trips the RMS barge-in window or
	// its echo guard, and a non-speech ([BLANK_AUDIO]) STT final from
	// silence never dispatches a turn (C11 nonSpeechTranscript guard).
	seedMic := make([]byte, linkSilenceSeedChunks*640)
	for _, mic := range []net.Conn{conns[0], conns[2]} {
		if _, serr := mic.Write(seedMic); serr != nil {
			m.log.Debug("voice: link mic silence seed write", "err", serr)
		}
	}
	entry := &linkEntry{
		id:        linkID,
		aCall:     aCall,
		bCall:     bCall,
		aEmployee: employeeA,
		bEmployee: employeeB,
		aMic:      conns[0],
		aSpk:      conns[1],
		bMic:      conns[2],
		bSpk:      conns[3],
		cancel:    pumpCancel,
		monA:      monA,
		monB:      monB,
		turns:     coord,
		aTarget:   aTarget,
		bTarget:   bTarget,
	}
	// CROSS-connect, byte-exact: A's speaker -> B's mic, B's speaker ->
	// A's mic. io.Copy is the whole contract (no re-chunking: the daemon
	// already paces each leg's outbound at one 640 B chunk per 20 ms).
	// The monitor tap (nil when Monitor=false) copies every byte the
	// pump moves, non-blocking: a slow or absent monitor client only
	// ever loses tap bytes, never bridge bytes.
	entry.pumps.Add(2)
	go func() {
		defer entry.pumps.Done()
		m.pumpStream(pumpCtx, entry.aSpk, entry.bMic, linkID, "a->b", entry.tapA())
	}()
	go func() {
		defer entry.pumps.Done()
		m.pumpStream(pumpCtx, entry.bSpk, entry.aMic, linkID, "b->a", entry.tapB())
	}()

	m.mu.Lock()
	m.sweepEndedLocked()
	m.links[linkID] = entry
	m.mu.Unlock()

	m.log.Info("voice: link open", "link_id", linkID,
		"a_ccid", aCall.CallID, "b_ccid", bCall.CallID,
		"a_employee", employeeA, "b_employee", employeeB,
		"monitor", option.Monitor)
	return &Link{
		ID:           linkID,
		ACCID:        aCall.CallID,
		BCCID:        bCall.CallID,
		AEmployee:    employeeA,
		BEmployee:    employeeB,
		MonitorAPath: monAPath,
		MonitorBPath: monBPath,
		mgr:          m,
	}, nil
}

// unwindBoth ends both legs with the same reason (each End is
// idempotent; a failure on one never masks the other - debug-logged).
func (m *LinkManager) unwindBoth(aCall, bCall LocalCall, reason string) {
	if endErr := m.deps.Leg.End(aCall.CallID, reason); endErr != nil {
		m.log.Debug("voice: link unwind: end side a", "ccid", aCall.CallID, "err", endErr)
	}
	if endErr := m.deps.Leg.End(bCall.CallID, reason); endErr != nil {
		m.log.Debug("voice: link unwind: end side b", "ccid", bCall.CallID, "err", endErr)
	}
}

// dialAll dials the four client pair sockets in a fixed order:
// [A.mic, A.speaker, B.mic, B.speaker]. A failure mid-way closes
// whatever already dialed and surfaces the named dial error.
func (m *LinkManager) dialAll(ctx context.Context, aCall, bCall LocalCall) ([4]net.Conn, error) {
	paths := [4]string{aCall.InPath, aCall.OutPath, bCall.InPath, bCall.OutPath}
	var conns [4]net.Conn
	for i, p := range paths {
		conn, err := m.deps.Dial(ctx, p)
		if err != nil {
			for j := 0; j < i; j++ {
				if conns[j] != nil {
					_ = conns[j].Close()
				}
			}
			return conns, fmt.Errorf("dial side %s (%s): %w", []string{"a.mic", "a.speaker", "b.mic", "b.speaker"}[i], p, err)
		}
		conns[i] = conn
	}
	return conns, nil
}

func (m *LinkManager) closeConns(conns [4]net.Conn) {
	for _, c := range conns {
		if c != nil {
			_ = c.Close()
		}
	}
}

// pumpStream copies src -> dst until EOF, a socket error (the leg ended:
// the pair Close breaks the copy - the expected teardown path, logged at
// debug only), or ctx cancel (which closes both conns to unblock the
// copy, never holding a lock across that I/O).
//
// Telemetry (link-72a04aebac963cdb post-mortem: a half-dead splice was
// only inferable from a +0 ms cadence summary): every pump logs its
// FIRST byte at INFO (the moment a direction demonstrably carries PCM)
// and its end with the total bytes copied, so the next live link run
// self-explains which direction died and when.
//
// tee (optional, nil = no monitor) is the monitor tap: every byte read
// from src is copied into it through a separate writer goroutine fed by
// a capacity-1 channel (latest-batch-wins when the tap lags), so a
// stalled monitor client can never slow or stall the io.Copy - the
// bridge's byte-exact contract is untouched by the tap. A tap write
// error marks the tap dead (drop for the rest of the link, logged
// once). Monitor bytes NEVER gate link correctness.
func (m *LinkManager) pumpStream(ctx context.Context, src, dst net.Conn, linkID, direction string, tee *monitorTap) {
	var bytes atomic.Uint64
	var firstOnce sync.Once
	started := time.Now()
	cs := &countingSrc{src: src, n: &bytes, onFirst: func() {
		firstOnce.Do(func() {
			m.log.Info("voice: link pump first byte", "link_id", linkID,
				"dir", direction, "after_ms", time.Since(started).Milliseconds())
		})
	}}

	var (
		teeDone  chan struct{}
		teeBatch chan []byte
		teeStop  chan struct{}
		teeDrop  atomic.Uint64
	)
	if tee != nil {
		teeBatch = make(chan []byte, 1)
		teeDone = make(chan struct{})
		teeStop = make(chan struct{})
		cs.onTee = func(p []byte) {
			// Copy first: the buffer belongs to io.Copy's read loop and
			// is reused on the next read.
			cp := make([]byte, len(p))
			copy(cp, p)
			select {
			case teeBatch <- cp:
			default:
				// Tap writer is behind: drop THIS batch (live-tap
				// semantics, never block the bridge).
				teeDrop.Add(1)
			}
		}
		go func() {
			defer close(teeDone)
			for {
				select {
				case batch := <-teeBatch:
					if _, err := tee.Write(batch); err != nil {
						// First (and only) tap socket death: log once,
						// the tap self-disarms to drops afterwards.
						m.log.Info("voice: link monitor tap dropped", "link_id", linkID,
							"dir", direction, "err", err)
					}
				case <-teeStop:
					// Pump finished (EOF, socket error, or teardown):
					// drain whatever already queued best-effort - each
					// tap Write is deadline-bounded, so this never
					// hangs the pump's exit.
					for {
						select {
						case batch := <-teeBatch:
							_, _ = tee.Write(batch)
						default:
							return
						}
					}
				}
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := io.Copy(dst, cs)
		copied := bytes.Load()
		if teeDrop.Load() > 0 {
			m.log.Debug("voice: link monitor tap drops", "link_id", linkID,
				"dir", direction, "drops", teeDrop.Load())
		}
		if err != nil && ctx.Err() == nil {
			// A real mid-stream break while the link is supposed to be
			// live. Copy stops either way; the legs stay up (their own
			// reapers bound the wreckage) but this is loud enough to
			// catch in a harness run.
			m.log.Warn("voice: link pump ended with error", "link_id", linkID, "dir", direction,
				"bytes", copied, "err", err)
		} else {
			m.log.Info("voice: link pump ended", "link_id", linkID, "dir", direction, "bytes", copied)
		}
	}()
	select {
	case <-done:
	case <-ctx.Done():
		_ = src.Close()
		_ = dst.Close()
		<-done
	}
	if teeStop != nil {
		// Pump is finished: stop the tap writer (it drains the last
		// queued batch), then join it before returning.
		close(teeStop)
		<-teeDone
	}
	_ = started
}

// countingSrc is an io.Reader wrapper that counts bytes and fires onFirst
// the first time it hands out a non-empty read. onTee (optional) mirrors
// every non-empty read to the monitor tap.
type countingSrc struct {
	src     io.Reader
	n       *atomic.Uint64
	onFirst func()
	onTee   func([]byte)
}

func (c *countingSrc) Read(p []byte) (int, error) {
	n, err := c.src.Read(p)
	if n > 0 {
		c.n.Add(uint64(n))
		c.onFirst()
		if c.onTee != nil {
			c.onTee(p[:n])
		}
	}
	return n, err
}

// Close ends both calls and kills the pumps. Idempotent (a second Close
// is nil); an unknown link id is ErrLinkNotFound. The entry stays
// retained (queryable via Status) for RetainEnded.
func (m *LinkManager) Close(linkID, reason string) error {
	m.mu.Lock()
	entry := m.links[linkID]
	if entry == nil {
		m.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrLinkNotFound, linkID)
	}
	if entry.ended {
		m.mu.Unlock()
		return nil
	}
	entry.ended = true
	entry.endedAt = time.Now()
	aCCID, bCCID := entry.aCall.CallID, entry.bCall.CallID
	m.mu.Unlock()

	// Half-duplex coordinator teardown: drop the registry entries (a
	// stale target must never bind a FUTURE call) and release/stop any
	// parked prompt + margin timer. The talk loops themselves end via
	// the Leg.End fan-out below (loop ctx cancel -> Run exit drops any
	// park with the prompt_dropped line).
	m.turns.Unregister(entry.aTarget, entry.bTarget)
	if entry.turns != nil {
		entry.turns.Unbind(LinkSideA)
		entry.turns.Unbind(LinkSideB)
	}

	// Teardown OUTSIDE the lock (repo rule: no I/O under mutex). Order
	// mirrors a client hangup: kill the pumps + conns first so nothing
	// writes into a closing pair, then End the legs (idempotent; the
	// FailOutcome fan-out owns registry/binding/childproc release). The
	// monitor listeners + tapped conns go last: closing them while a
	// pump still drains its tee channel only drops bytes (the tap is
	// non-gating by contract).
	entry.cancel()
	entry.closeConns.Do(func() {
		m.closeConns([4]net.Conn{entry.aMic, entry.aSpk, entry.bMic, entry.bSpk})
		if entry.monA != nil {
			entry.monA.close()
		}
		if entry.monB != nil {
			entry.monB.close()
		}
	})
	if err := m.deps.Leg.End(aCCID, reason); err != nil {
		m.log.Debug("voice: link close: end side a", "ccid", aCCID, "err", err)
	}
	if err := m.deps.Leg.End(bCCID, reason); err != nil {
		m.log.Debug("voice: link close: end side b", "ccid", bCCID, "err", err)
	}
	m.log.Info("voice: link closed", "link_id", linkID, "a_ccid", aCCID, "b_ccid", bCCID, "reason", reason)
	return nil
}

// Status snapshots both sides. Works after Close for the retention
// window (transcripts ride the calls.db row, which outlives the entry);
// an unknown/expired link is ErrLinkNotFound.
func (m *LinkManager) Status(ctx context.Context, linkID string) (LinkStatusResult, error) {
	m.mu.Lock()
	entry := m.links[linkID]
	if entry == nil {
		m.mu.Unlock()
		return LinkStatusResult{}, fmt.Errorf("%w: %s", ErrLinkNotFound, linkID)
	}
	aCCID, bCCID := entry.aCall.CallID, entry.bCall.CallID
	aEmp, bEmp := entry.aEmployee, entry.bEmployee
	ended := entry.ended
	m.mu.Unlock()

	res := LinkStatusResult{LinkID: linkID, Ended: ended}
	res.A = m.sideStatus(ctx, aCCID, aEmp)
	res.B = m.sideStatus(ctx, bCCID, bEmp)
	return res, nil
}

// sideStatus reads one call's transcript through the store seam. A nil
// or failing seam degrades to zero counts (a status poll must never
// abort a harness run mid-link).
func (m *LinkManager) sideStatus(ctx context.Context, ccid, employee string) LinkSide {
	side := LinkSide{CallID: ccid, EmployeeID: employee}
	if m.deps.Store == nil {
		return side
	}
	_, lines, _, err := m.deps.Store.EndedCall(ctx, ccid)
	if err != nil {
		m.log.Debug("voice: link status: transcript read", "ccid", ccid, "err", err)
		return side
	}
	for _, l := range lines {
		switch l.Role {
		case "assistant":
			side.AssistantTurns++
		case "user":
			side.UserTurns++
		}
	}
	side.Transcript = lines
	return side
}

// sweepEndedLocked drops closed entries past the retention window.
// Called under m.mu from Open (cheap: the map is tiny); callers must
// hold the mutex.
func (m *LinkManager) sweepEndedLocked() {
	cutoff := time.Now().Add(-m.retain)
	for id, entry := range m.links {
		if entry.ended && entry.endedAt.Before(cutoff) {
			delete(m.links, id)
		}
	}
}

// LiveLinks snapshots the currently-open (not yet closed) links for
// observability (voice.link status from the CLI's side).
func (m *LinkManager) LiveLinks() []Link {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Link, 0, len(m.links))
	for _, entry := range m.links {
		if entry.ended {
			continue
		}
		out = append(out, Link{
			ID:        entry.id,
			ACCID:     entry.aCall.CallID,
			BCCID:     entry.bCall.CallID,
			AEmployee: entry.aEmployee,
			BEmployee: entry.bEmployee,
			mgr:       m,
		})
		if entry.monA != nil {
			out[len(out)-1].MonitorAPath = entry.monA.path
		}
		if entry.monB != nil {
			out[len(out)-1].MonitorBPath = entry.monB.path
		}
	}
	return out
}
