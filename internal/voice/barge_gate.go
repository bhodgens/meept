package voice

// barge_gate.go: the shared fresh-audio generation gate for barge-in
// re-arm (bughunt 2026-11-10 A6, generalized from the local leg to BOTH
// media splices).
//
// The contract (internal/voice/AGENTS.md, Media splice): "the splice
// re-arms when FRESH child PCM arrives". Before A6 only the local leg
// enforced freshness; the phone splice re-armed its barge-in latch on
// ANY pump read, so stale in-flight audio of the interrupted utterance
// re-armed the latch and replayed over the new turn (bounded only by
// QueueLimit and the backpressure grace hangup). Both splices now ride
// this one type:
//
//   - Snapshot(): the read pump takes it BEFORE each child-socket read
//     and treats it as the chunk's birth certificate.
//   - Bump(): the cancel path (HandleInterrupt / bargeIn) invalidates
//     every snapshot taken so far. When a stream was actively flowing at
//     cancel time (an outbound touch within the residue window), it also
//     stamps the residue deadline: socket-buffer backlog read shortly
//     after the cancel is still the interrupted stream's tail.
//   - ReadStale(): the pump's read-side verdict — a chunk whose snapshot
//     is behind the CURRENT generation was in flight when the cancel
//     landed; a post-cancel read inside a live residue window may still
//     be the interrupted stream's tail (the child wrote it before it saw
//     the clear frame). Both are DISCARDED: they can neither re-arm the
//     latch nor reach the wire.
//   - ConsumeStale(): the pacer's consume-time re-check (the pump
//     backlog can hold pre-cancel chunks) over the generation, plus —
//     on the local leg — the chunk's carried READ-TIME residue verdict
//     (bughunt 2026-11-11 C-2: the pacer judges by the verdict of the
//     read that produced the chunk, never by the gate's residue state
//     at consume time).
//   - FreshSnapshot(): the resume gate — only a snapshot equal to the
//     CURRENT generation may re-arm the latch.
//
// The no-residue path preserves the documented silent-then-say posture:
// with NO stream flowing at cancel (no recent outbound touch) no residue
// is possible, so the very first post-cancel PCM is unconditionally fresh
// and re-arms immediately.
//
// Clock (bughunt 2026-11-11 C-19): the gate's REAL clock runs at
// MONOTONIC pace. Deadline arithmetic on the wall clock stretched or
// killed the residue window on an NTP step (a backward step extended the
// window; a forward step expired it early). The gate anchors a wall↔mono
// base pair lazily on its first real reading and computes
// wallBase + monoDelta: values stay numerically in the unix-nano
// neighborhood the producer touch stamps live in, but advance with the
// monotonic clock, so a wall step after the anchor cannot move a
// deadline. The injectable test clock is unchanged (nowNano alone keeps
// the pure-fake domain); a test simulating a wall step sets monoNow too.

import (
	"sync"
	"time"
)

// bargeResidueQuiet is the post-cancel residue window shared by both
// media splices (localBargeResidueQuiet before the gate was extracted;
// bughunt 2026-11-10 A6): chunks READ within this window after a barge-in
// cancel are treated as socket-buffer residue of the interrupted stream
// even when they carry the new generation — the child had audio in
// flight when its abort path ran, and that audio must not re-arm the
// latch or replay over the new answer. Chunks read after the window are
// FRESH (the interrupted stream is long dead; only a new say writes PCM
// by then). The window is stamped ONLY when playback was live at the
// cancel (an outbound touch within this window).
const bargeResidueQuiet = 250 * time.Millisecond

// bargeGate is the shared fresh-audio gate state. The zero value is
// ready to use (generation 0, no residue); every method is nil-safe so a
// caller without a gate keeps pre-gate semantics (everything fresh).
type bargeGate struct {
	mu               sync.Mutex
	gen              uint64 // bumped on every barge-in cancel
	residueUntilNano int64  // 0 = no residue window live
	// nowNano is injectable so tests can walk the residue deadline
	// without real sleeps; zero (or nil) means the monotonic real
	// clock (clockNano).
	nowNano func() int64
	// monoNow is the SECOND test seam (C-19): the monotonic reading the
	// real clock pairs with nowNano. Both nil (the zero value) = real
	// time; both set = a fake clock advanced in lockstep. When ONLY
	// nowNano is set, the fake domain is self-consistent (deadlines and
	// reads all come from the same fn) and monoNow stays nil.
	monoNow func() time.Duration
	// wallBaseNano / monoBase are the lazily-anchored wall↔mono pair:
	// taken together on the FIRST real reading, after which the clock
	// advances at monotonic pace (wallBaseNano + mono-since-anchor).
	// This is what makes an NTP wall step after the anchor unable to
	// stretch or kill the residue window (C-19). All three fields are
	// owned by clockNano under anchorMu — a DEDICATED lock distinct
	// from the gate state mu, so Bump (which holds mu across its
	// two clock reads) can never self-deadlock on the anchor.
	wallBaseNano int64
	monoBase     time.Duration
	anchored     bool
	anchorMu     sync.Mutex
}

// clockNano resolves the gate clock. Safe to call with or without mu
// held (Bump holds it; the lock-free verdict readers don't) — the anchor
// state rides the dedicated anchorMu, never the gate state lock. With a
// fake nowNano (and no monoNow) the value is passed through — a single
// self-consistent fake domain. With both seams set (fake wall + fake
// mono) the mono reading drives the pace over the anchored base. The
// REAL clock (both seams nil) is MONOTONIC: the wall↔mono base pair
// anchors on the first reading and every later reading is
// wallBase + monotonic-since-anchor, so a wall-clock step (NTP
// correction) after the anchor cannot move residue deadlines (C-19).
func (g *bargeGate) clockNano() int64 {
	if g.nowNano == nil {
		// Real clock, monotonic pace (C-19).
		g.anchorMu.Lock()
		mono := time.Since(processStartMono)
		if !g.anchored {
			g.wallBaseNano = time.Now().UnixNano()
			g.monoBase = mono
			g.anchored = true
		}
		wall := g.wallBaseNano + (mono - g.monoBase).Nanoseconds()
		g.anchorMu.Unlock()
		return wall
	}
	if g.monoNow != nil {
		// Fake wall + fake mono (the C-19 test shape): the mono
		// reading drives the pace over the wall base exactly like
		// the real clock above, so a test can step the wall
		// (nowNano) and prove the deadline does not move.
		g.anchorMu.Lock()
		mono := g.monoNow()
		if !g.anchored {
			g.wallBaseNano = g.nowNano()
			g.monoBase = mono
			g.anchored = true
		}
		wall := g.wallBaseNano + (mono - g.monoBase).Nanoseconds()
		g.anchorMu.Unlock()
		return wall
	}
	return g.nowNano()
}

// processStartMono is the package-level monotonic anchor the real clock
// computes deltas against (a zero time.Time yields the zero duration).
var processStartMono = time.Now()

// Snapshot returns the pre-read view the pump stamps onto each chunk:
// the current generation and the residue deadline (0 when none) live at
// snapshot time. Nil gate: (0, 0).
func (g *bargeGate) Snapshot() (readGen uint64, residueUntilNano int64) {
	if g == nil {
		return 0, 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.gen, g.residueUntilNano
}

// Current returns the live generation. Nil gate: 0.
func (g *bargeGate) Current() uint64 {
	if g == nil {
		return 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.gen
}

// Bump invalidates every snapshot taken so far (the cancel path). When a
// stream was actively flowing at cancel time — lastTouchNano non-zero and
// within bargeResidueQuiet of now — the residue deadline is stamped so
// post-cancel reads of the interrupted stream's socket-buffer tail stay
// stale until it passes. With no recent touch there is no in-flight
// stream, no residue is possible, and the next PCM is unconditionally
// fresh (the silent-then-say shape). The window is owned HERE, not a
// parameter: both media splices must share one residue policy, and a
// call site cannot silently tune its own. Nil gate: no-op.
func (g *bargeGate) Bump(lastTouchNano int64) {
	if g == nil {
		return
	}
	const residueWindow = bargeResidueQuiet
	g.mu.Lock()
	g.gen++
	if lastTouchNano != 0 &&
		g.clockNano()-lastTouchNano < int64(residueWindow) {
		g.residueUntilNano = g.clockNano() + int64(residueWindow)
	} else {
		g.residueUntilNano = 0
	}
	g.mu.Unlock()
}

// ReadStale is the read-side verdict for a chunk carrying the given
// pre-read snapshot: stale when the snapshot generation is behind the
// CURRENT generation (in-flight audio of the interrupted stream, read
// before the cancel landed) or when the snapshot's residue deadline is
// still live at read time (socket-buffer residue read inside the
// window). Stale chunks are discarded by the caller — they never re-arm
// the latch and never reach the wire. Nil gate: never stale (pre-gate
// semantics).
func (g *bargeGate) ReadStale(readGen uint64, residueUntilNano int64) bool {
	if g == nil {
		return false
	}
	if readGen != g.Current() {
		return true
	}
	return residueUntilNano != 0 && g.clockNano() < residueUntilNano
}

// ConsumeStale is the pacer's consume-time re-check for one queued chunk
// (single-consumer goroutine by contract, no lock needed on the caller
// side). A chunk whose pre-read generation is behind the current one was
// in flight when the cancel landed — stale, dropped, never written. The
// RESIDUE half of the verdict lives on the chunk (bughunt 2026-11-11
// C-2): the pump stamps its read-time ReadStale result onto
// localReadItem.stale (local leg), and the phone leg's pump discards
// stale reads pre-enqueue — the consume side never re-reads the gate's
// residue state, so a pacer backlog delay cannot convert a stale verdict
// into a write by outliving the window. Nil gate: never stale.
func (g *bargeGate) ConsumeStale(gen uint64) bool {
	if g == nil {
		return false
	}
	return gen != g.Current()
}

// FreshSnapshot reports whether the given pre-read snapshot equals the
// CURRENT generation — the resumeBargeIn gate. Only genuinely fresh PCM
// (read strictly after the cancel, outside the residue window) may
// re-arm the latch. Nil gate: true (pre-gate semantics).
func (g *bargeGate) FreshSnapshot(readGen uint64) bool {
	if g == nil {
		return true
	}
	return readGen == g.Current()
}
