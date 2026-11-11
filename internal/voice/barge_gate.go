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
//     backlog can hold pre-cancel chunks), mirroring ReadStale against
//     the gate's own stored residue deadline.
//   - FreshSnapshot(): the resume gate — only a snapshot equal to the
//     CURRENT generation may re-arm the latch.
//
// The no-residue path preserves the documented silent-then-say posture:
// with NO stream flowing at cancel (no recent outbound touch) no residue
// is possible, so the very first post-cancel PCM is unconditionally fresh
// and re-arms immediately.

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
	// without real sleeps; zero (or nil) means time.Now.
	nowNano func() int64
}

// clockNano resolves the gate clock.
func (g *bargeGate) clockNano() int64 {
	if g.nowNano != nil {
		return g.nowNano()
	}
	return time.Now().UnixNano()
}

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
// in flight when the cancel landed; a chunk consumed inside the gate's
// stored residue window is socket-buffer residue of the interrupted
// stream even though it carries the current generation. Stale chunks are
// dropped, never written. Nil gate: never stale.
func (g *bargeGate) ConsumeStale(gen uint64) bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return gen != g.gen ||
		(g.residueUntilNano != 0 && g.clockNano() < g.residueUntilNano)
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
