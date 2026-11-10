package voice

// RunLocalSplice is the carrier-free local-leg media path (leaf 05,
// contract CL7): the child's per-call C2 audio pair on one side, a plain
// io.ReadWriter the caller supplies on the other (leaf 07 hands unix
// conn(s); tests hand net.Pipe). No carrier, no Telnyx anywhere.
//
// Inbound: read mic bytes from deps.Client and write them chunk-sized
// (640 B, L16 s16le@16k) to the child .in socket. Unix sockets preserve
// order, so there is NO reorder window. The local leg negotiates
// L16@16000 only and REFUSES anything else (named error at start), so
// there is NO transcode.
//
// Outbound: read child PCM (.out), split into 640 B chunks, and pace one
// chunk per cfg.Pace (20 ms real time - the same uptractor live-drill
// rule RunSplice documents: bursts overflow the client playout buffer as
// crackle; here the local player's underruns are the crackle equivalent).
//
// Barge-in (per call, per BARGE-IN - same latch contract the phone splice
// pins after bughunt F-1): HandleInterrupt for THIS ccid stops outbound
// writes, sends Frame{call_id, event:"clear"} to the CHILD via deps.Text
// (there is no carrier playout to clear - the child's TTS queue is what
// needs flushing), invokes deps.CancelDraft once, and re-arms when fresh
// child PCM arrives. One RunLocalSplice instance owns only its own state,
// so other calls' splices are structurally untouched; HandleInterrupt also
// refuses a foreign ccid outright.
//
// Join: the client must produce its first byte within cfg.JoinTimeout
// (0 -> 10s) or the splice ends with ErrNoClient (the Open-side grace -
// a client that never dials the pair must not wedge the leg).
//
// Backpressure: a socket write error ends the leg with a named error
// (CL7); there is no drop-oldest path.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// ErrNoClient is returned by RunLocalSplice when the client produced no
// first byte within cfg.JoinTimeout.
var ErrNoClient = errors.New("voice: local client never connected")

// ErrMicSpeechEnded is the CLEAN inbound end: the client half-closed its
// mic direction (end of speech - file mode after the utterance drains).
// It is NOT a call end: the outbound side stays live so the reply path
// (STT flush -> employee turn -> TTS) still reaches the client. The
// child learns end-of-speech through the half-closed CHILD conn
// (CloseWriteChild): its audio pump sees EOF, closes the PCM channel,
// and the STT adapter flushes the buffered utterance.
var ErrMicSpeechEnded = errors.New("voice: local mic stream ended (end of speech)")

// ErrL16Only is returned by RunLocalSplice when cfg.ChunkBytes names any
// chunk size other than the L16 s16le@16k native 640 bytes.
var ErrL16Only = errors.New("voice: local leg negotiates L16 s16le@16000 only")

// localPaceDefault is the outbound inter-chunk interval (20 ms per 640 B
// L16@16k chunk, the uptractor pacing rule RunSplice mirrors).
const localPaceDefault = 20 * time.Millisecond

// localChunkBytes is the child-native 20 ms chunk (L16 s16le@16k).
const localChunkBytes = 640

// localJoinTimeoutDefault bounds the Open-side client join grace.
const localJoinTimeoutDefault = 10 * time.Second

// localDrainQuietDefault is the outbound quiescence window: watch
// armed + pump backlog empty + NO child read and NO
// pacer consume/write for this long = the turn's audio is DONE (every
// whole chunk the child sent has been written to the client socket;
// the pump's sub-chunk residue is NOT part of the predicate - a real
// child stream's byte total is never a multiple of 640, so gating on
// carry==0 made the drain unreachable in production).
// Deliberately conservative (better late than early): the window must
// exceed a realistic inter-sentence cloud-TTS gap (a turn streams
// sentence after sentence; a mid-turn synthesis lull shorter than this
// must never fake a drain). The coordinator adds its own tail pad on
// top, and the long fallback margin bounds the worst case if this never
// fires.
const localDrainQuietDefault = 1 * time.Second

// localDrainQuietFinal is the QUIET window of a played-armed watch (armed
// by the C1 played edge — see runLocalSplice.drainArmedFinal). After the
// child's final-byte marker, no more audio for the turn CAN arrive, so the
// window only needs to absorb the last paced writes draining through the
// pacer: 500 ms of silence past pending==0 is terminal for the turn. The
// full DrainQuiet stays in force for the blind (non-final) arm shape that
// non-link/no-marker callers keep.
const localDrainQuietFinal = 500 * time.Millisecond

// LocalSpliceConfig sizes the local splice. Zero values pick the CL7
// defaults; ChunkBytes other than 640 (any non-L16@16k negotiation) is a
// named start error.
type LocalSpliceConfig struct {
	Pace        time.Duration // 20ms default; 0 -> 20ms
	ChunkBytes  int           // 0 -> 640 (L16@16k enforced)
	JoinTimeout time.Duration // 0 -> 10s; client must connect/first-byte within it or ErrNoClient
	// DrainQuiet overrides localDrainQuietDefault for the audio-drain
	// watch (0 -> default). Tests lower it; production never sets it.
	DrainQuiet time.Duration
	// DrainQuietFinal overrides localDrainQuietFinal for the played-armed
	// (post-C1-played-edge) watch window (0 -> default). Tests lower it;
	// production never sets it.
	DrainQuietFinal time.Duration
}

// LocalSpliceDeps carries the injected edges. CancelDraft is ALWAYS
// injected by callers (never a direct manager call from the splice); a
// nil value is tolerated as a counted no-op for tests.
//
// AudioDrained is the LINK half-duplex drain seam (optional, nil for
// every plain local/phone call): fired per turn with the ccid once the
// outbound pacer has WRITTEN every child chunk of the turn to the
// client socket and the child stream has gone quiet. The turn-final
// say frame only flushes the talk loop's TEXT pipeline - the daemon
// pacer still has seconds of paced audio to write after it - so the
// coordinator must not release the floor on turn_final alone; this
// edge is the audio-truth release signal (measured talk-over: 6.7 s
// across 335 windows on link-1108d776b0a1efb3).
type LocalSpliceDeps struct {
	Audio       *AudioPair
	Client      io.ReadWriter // mic bytes in, speaker bytes out
	Text        TextSender    // clear frame to child on barge-in
	CancelDraft func(ccid string)
	// AudioDrained fires once per turn when the turn's audio is fully
	// written + quiescent (see ArmDrainWatch). Nil (every plain call) =
	// zero emissions, byte-identical splice.
	AudioDrained func(ccid string)
	// OnClientJoin fires EXACTLY ONCE when the client produces its
	// first mic byte — the same edge that satisfies the join gate
	// (leaf 02, tree 20261110-voice-callleg: the local CallLeg adapter
	// maps it to Connected). Observability only: nil = zero emissions,
	// the join gate is unchanged and never waits on an observer.
	OnClientJoin func(ccid string)
	Logger       *slog.Logger
}

// localReadItem is one unit on the inbound pump channel: mic bytes, or a
// terminal read error. (A single channel of interface items keeps the
// pump join race-free without a second select arm per event.)
type localReadItem struct {
	data []byte
	err  error
}

// runLocalSplice is the live state of one local call's splice.
type runLocalSplice struct {
	ccid string
	cfg  LocalSpliceConfig
	deps LocalSpliceDeps
	log  *slog.Logger

	chunkBytes int

	// Pump-side counters (same hunt): first bytes from the child, chunk
	// count, and paced-to-client count - the missing middle when the
	// CLI sees 0 bytes and the child's stream stalls. pumpChunks is
	// written by the pump goroutine and read by the loop's summary:
	// atomic. The pace* fields are owned by the single outboundLoop
	// goroutine (paceOne runs there), no lock needed.
	pumpChunks      atomic.Uint64
	lastClientWrite time.Time
	paceWrites      int // client writes completed this call
	paceFirstAt     time.Time
	paceDropped     int // frames abandoned at the barge-in latch
	paceSamples     int
	paceSumMs       int64
	paceMaxMs       int64
	paceOverruns    int

	mu        sync.Mutex
	interrupt bool // outbound writes latched off for the current barge-in
	cancelOne bool // CancelDraft fired exactly once per barge-in
	ended     bool // teardown latched: the latch must never re-arm after end
	wake      chan struct{}

	// Drain watch (link half-duplex floor release, turn-taking leaf 02).
	// The link coordinator's turn_final edge now sets EXPECT mode
	// (ExpectFinalDrain); the child's C1 played edge arms the watch via
	// MarkFinalPlayed (resolved through the package-level LocalSpliceFor
	// registry, the same seam HandleInterrupt rides). ArmDrainWatch (the
	// blind turn_final-shape arm) is kept for non-link callers and the
	// existing drain tests. The watch goroutine starts lazily on the
	// first arm — plain local/phone splices (deps.AudioDrained nil,
	// never armed) spawn nothing and behave byte-identically. Fired
	// evidence (atomics, no lock: writers must never take s.mu for a
	// counter, the watch polls off the pacer loop):
	//   audioPending - child chunks handed to the pump channel minus
	//     chunks the pacer finished consuming (the backlog still to
	//     WRITE to the client socket);
	//   audioCarry   - sub-chunk residue the read pump holds (bytes not
	//     yet a whole 20 ms frame). NOT part of the drain predicate:
	//     a real child's TTS stream length is never a multiple of 640
	//     (measured: every playback cadence line had bytes % 640 != 0),
	//     so the pump's held residue ends every turn non-zero and the
	//     old `carry==0` gate made the fire UNREACHABLE in production
	//     (link-6eb490abd8349958: 0/5 drained releases, every floor
	//     release rode the margin). The residue is never playable audio
	//     mid-call (the pump contract holds partials, never flushes a
	//     torn frame), so it must not pin the floor.
	//   audioLastAct - unix-nano of the last outbound touch (pump read
	//     OR pacer consume/write).
	// Drain (fire once, then the latch self-disarms) = armed && the
	// barge-in latch is NOT set && not ended && pending<=0 &&
	// a touch happened AFTER the arm (drainArmedAtNano floor - the
	// early-fire guard: at turn_final the LAST sentence has only just
	// reached the child, its TTS may take hundreds of ms to produce a
	// byte, and the splice cannot tell "synthesizing" from "will never
	// speak" (fully denied draft) - so it NEVER fires on a silent
	// stream; denied turns ride the coordinator's fallback margin) &&
	// no touch for cfg.DrainQuiet. Conservative by construction: every
	// new child byte reopens the window, so a mid-turn synthesis gap
	// shorter than the window never fakes a drain; "late" only costs
	// floor-hold, "early" is the bug being fixed.
	//
	// PLAYED-ARMED MODE (drainArmedFinal): the true end-of-turn marker.
	// The blind arm at the turn_final TEXT edge was still EARLY in
	// production (link-2fdc3f5fd8d81805: the child's inter-sentence
	// synth gaps of 1.4-3.8 s exceed the 1 s quiet window, so mid-reply
	// quiescence released the floor while the speaker still had queued
	// sentences -> 60 short both-audible runs, 10.94 s overlap). The
	// child now sends C1 played (EventPlayed, last:true) after the
	// FINAL byte of the turn's last:true stream reaches the .out
	// socket; routeFrame hands that edge to MarkFinalPlayed, which
	// arms the watch in final mode. Final mode drops the post-arm-touch
	// guard (the played edge IS the touch: nothing more can arrive for
	// the turn by construction) and shrinks the quiet window to
	// localDrainQuietFinal - pending==0 + 500 ms of silence after the
	// edge. ExpectFinalDrain is the coordinator's turn_final edge: it
	// records that the played edge is expected WITHOUT arming anything;
	// if it never arrives (old child binary, aborted final stream) the
	// coordinator's fallback margin still bounds the hold.
	drainArmed       bool
	drainArmedFinal  bool // armed by MarkFinalPlayed (played edge), not blindly
	drainExpectFinal bool // coordinator turn_final: awaiting the played edge
	watchRunning     bool
	drainArmedAtNano atomic.Int64
	audioPending     atomic.Int64
	audioCarry       atomic.Int64
	audioLastAct     atomic.Int64
	// loopCtx bounds the watch goroutine (assigned BEFORE the splice
	// registers itself in localSpliceByCcid: LocalSpliceFor readers get
	// a happens-before edge through the registry mutex).
	loopCtx context.Context
}

// Registry of live local splices by ccid so leaf 07's frame path can reach
// the HandleInterrupt entry point without a second manager seam. One
// RunLocalSplice per ccid by contract (CL7), so the map never collides;
// a duplicate start is refused with a named error.
var (
	localSpliceMu     sync.Mutex
	localSpliceByCcid = map[string]*runLocalSplice{}
)

// LocalSpliceFor returns the live local splice registered under ccid, or
// nil. Leaf 07's C1 frame path calls HandleInterrupt on the result.
func LocalSpliceFor(ccid string) *runLocalSplice {
	localSpliceMu.Lock()
	defer localSpliceMu.Unlock()
	return localSpliceByCcid[ccid]
}

// notify pokes the outbound pacer awake on state changes (barge-in
// tripped or re-armable, in-flight pace wait, terminal pump error).
// Capacity 1 with drop-on-full is safe: senders mutate state before
// notifying and every receiver re-checks state after receiving, so a
// dropped notify only means a wake was already pending (same shape as
// RunSplice's audit m3 wake).
func (s *runLocalSplice) notify() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// RunLocalSplice runs one local call's splice until ctx is cancelled or a
// terminal socket error fires on either side. It owns its goroutines; the
// caller closes deps.Audio and deps.Client.
func RunLocalSplice(ctx context.Context, ccid string, cfg LocalSpliceConfig, deps LocalSpliceDeps) error {
	if deps.Audio == nil {
		return errors.New("voice: RunLocalSplice requires an AudioPair")
	}
	if deps.Client == nil {
		return errors.New("voice: RunLocalSplice requires a client ReadWriter")
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if cfg.Pace <= 0 {
		cfg.Pace = localPaceDefault
	}
	if cfg.JoinTimeout <= 0 {
		cfg.JoinTimeout = localJoinTimeoutDefault
	}
	if cfg.DrainQuiet <= 0 {
		cfg.DrainQuiet = localDrainQuietDefault
	}
	if cfg.DrainQuietFinal <= 0 {
		cfg.DrainQuietFinal = localDrainQuietFinal
	}
	// L16@16k only (CL7): refuse any other negotiation at start, before
	// any goroutine exists.
	if cfg.ChunkBytes == 0 {
		cfg.ChunkBytes = localChunkBytes
	}
	if cfg.ChunkBytes != localChunkBytes {
		return fmt.Errorf("voice: RunLocalSplice %s: %w (got ChunkBytes=%d, want %d)",
			ccid, ErrL16Only, cfg.ChunkBytes, localChunkBytes)
	}

	s := &runLocalSplice{
		ccid:       ccid,
		cfg:        cfg,
		deps:       deps,
		log:        deps.Logger.With("ccid", ccid),
		chunkBytes: cfg.ChunkBytes,
		wake:       make(chan struct{}, 1),
	}

	// Drain-watch parent context: assigned BEFORE the registration
	// below so a LocalSpliceFor reader (which takes localSpliceMu, the
	// same mutex the writer holds) sees a fully initialized struct,
	// loopCtx included - no lock-free field race on the arm path.
	loopCtx, loopCancel := context.WithCancel(ctx)
	s.loopCtx = loopCtx

	localSpliceMu.Lock()
	if _, dup := localSpliceByCcid[ccid]; dup {
		localSpliceMu.Unlock()
		loopCancel()
		return fmt.Errorf("voice: RunLocalSplice %s: a splice is already live for this ccid", ccid)
	}
	localSpliceByCcid[ccid] = s
	localSpliceMu.Unlock()
	defer func() {
		localSpliceMu.Lock()
		delete(localSpliceByCcid, ccid)
		localSpliceMu.Unlock()
	}()

	defer loopCancel()

	inboundDone := make(chan error, 1)
	outboundDone := make(chan error, 1)
	go func() { inboundDone <- s.inboundLoop(loopCtx) }()
	go func() { outboundDone <- s.outboundLoop(loopCtx) }()

	// Whichever loop ends first must stop the peer NOW. The old shape
	// read inboundDone FIRST and blocked there: an outbound terminal
	// (a real child-read failure) left the loop half-dead — inbound
	// parked on an alive client forever, RunLocalSplice never
	// returned, local.go's teardown never fired, and the link ran 90 s
	// with one side mute (link-72a04aebac963cdb post-mortem: B logged
	// no 'splice ended err=' line because the splice never returned).
	var errIn, errOut error
	select {
	case errIn = <-inboundDone:
		if !errors.Is(errIn, ErrMicSpeechEnded) {
			// A terminal inbound error stops the peer loop now; a
			// clean end-of-speech does NOT - the outbound side must
			// stay live so the reply (whisper flush -> employee turn
			// -> TTS) still reaches the client after the mic stream
			// closed (file mode half-closes at end of utterance;
			// live mode never EOFs).
			loopCancel()
		}
		errOut = <-outboundDone
	case errOut = <-outboundDone:
		// Outbound terminal (child socket broke / leg closed): the
		// reply path is dead, so the whole call ends - tear the
		// inbound down with it.
		loopCancel()
		errIn = <-inboundDone
	}

	// Caller cancellation wins, mirroring RunSplice's contract.
	if ctx.Err() != nil && (errors.Is(errIn, context.Canceled) || errors.Is(errOut, context.Canceled)) {
		return ctx.Err()
	}
	for _, e := range []error{errIn, errOut} {
		if e != nil && !errors.Is(e, context.Canceled) {
			return e
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

// ---------- inbound path (client -> child) ----------

// inboundLoop reads mic bytes from the client and writes them to the child
// .in in exact chunkBytes units. A partial tail that never fills a chunk
// is held (never flushed): the child speaks in whole 640 B units, and a
// trailing fragment at teardown is a torn frame, not audio.
//
// The join gate lives here: the FIRST read must produce at least one byte
// within cfg.JoinTimeout or the leg ends with ErrNoClient. A client that
// never writes must not wedge the call open.
func (s *runLocalSplice) inboundLoop(ctx context.Context) error {
	pending := make([]byte, 0, s.chunkBytes)

	// JoinTimeout grace: the first byte must land inside the window. A
	// ctx cancel ends the wait as ctx.Err (not ErrNoClient).
	joined := make(chan struct{})
	var joinOnce sync.Once
	recordFirst := func() {
		joinOnce.Do(func() {
			close(joined)
			// Leaf 02 observability edge: the client's first byte IS
			// the join — fire the optional observer after the gate
			// channel closes (the splice itself never waits on it).
			if s.deps.OnClientJoin != nil {
				s.deps.OnClientJoin(s.ccid)
			}
		})
	}

	pump := make(chan localReadItem, 1)
	go func() {
		buf := make([]byte, s.chunkBytes)
		for {
			n, err := s.deps.Client.Read(buf)
			if n > 0 {
				cp := make([]byte, n)
				copy(cp, buf[:n])
				select {
				case pump <- localReadItem{data: cp}:
				case <-ctx.Done():
					return
				}
				recordFirst()
			}
			if err != nil {
				select {
				case pump <- localReadItem{err: err}:
				case <-ctx.Done():
				}
				return
			}
		}
	}()

	joinTimer := time.NewTimer(s.cfg.JoinTimeout)
	defer joinTimer.Stop()

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-joinTimer.C:
			// The window only bounds the FIRST byte; once the client
			// joined, expiry is impossible (the timer is stopped below
			// on join) and this arm is dead.
			return ErrNoClient
		case <-joined:
			// Client joined: kill the join grace so it can never fire
			// mid-call (a timer left running would tear down a healthy
			// leg exactly JoinTimeout after start).
			if !joinTimer.Stop() {
				select {
				case <-joinTimer.C:
				default:
				}
			}
			// Loop back to the select; the timer arm is now dead and
			// the pump drives everything.
			continue
		case item := <-pump:
			if item.err != nil {
				if errors.Is(item.err, io.EOF) {
					// Client EOF = END OF SPEECH, not a hangup: forward
					// the close to the CHILD (its pump EOF triggers the
					// STT flush) and keep the splice alive for the
					// reply. The ended latch stays UNSET - the barge-in
					// path must stay armed for the turn that follows.
					if cwErr := s.deps.Audio.CloseWriteChild(); cwErr != nil {
						s.log.Debug("child half-close", "err", cwErr)
					}
					return ErrMicSpeechEnded
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return fmt.Errorf("voice: localsplice %s client read: %w", s.ccid, item.err)
			}
			pending = append(pending, item.data...)
			for len(pending) >= s.chunkBytes {
				chunk := make([]byte, s.chunkBytes)
				copy(chunk, pending[:s.chunkBytes])
				pending = pending[s.chunkBytes:]
				if err := s.deps.Audio.WriteToChild(chunk); err != nil {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					return fmt.Errorf("voice: localsplice %s write child: %w", s.ccid, err)
				}
			}
		}
	}
}

// ---------- outbound path (child -> client) ----------

// outboundLoop mirrors RunSplice's shape: ONE long-lived read pump keeps
// draining the child .out socket and feeds chunked PCM to a paced pacer.
// The split matters for the per-barge-in re-arm contract: while the latch
// is set the PACER idles, but the pump must KEEP READING so fresh child
// PCM (the resume signal) is observed and resumeBargeIn fires. A serial
// read->pace loop could never see the resume signal - it would be blocked
// waiting on the latch it alone must clear.
func (s *runLocalSplice) outboundLoop(ctx context.Context) error {
	pump := make(chan localReadItem, 64)
	var pumpFirst time.Time // pump-goroutine-owned (instrumentation)
	// Jitter instrumentation: one INFO summary per call splice (the
	// middle hop's totals between the child's cadence line and the CLI's
	// trace line).
	defer func() {
		s.log.Info("voice: localsplice cadence summary", "ccid", s.ccid,
			"pump_chunks", s.pumpChunks.Load(), "pace_writes", s.paceWrites,
			"pace_dropped", s.paceDropped, "pace_stalls", s.paceOverruns,
			"pace_max_gap_ms", s.paceMaxMs, "first_child_pcm_set", !pumpFirst.IsZero())
	}()
	go func() {
		carry := make([]byte, 0, 4*s.chunkBytes)
		for {
			rb := make([]byte, s.chunkBytes)
			n, err := s.deps.Audio.ReadFromChild(rb)
			if n == 0 && err != nil && isNotConnected(err) {
				// Pre-accept: the child has not dialed .out yet.
				// LocalLeg.Open starts the splice the instant the
				// pair exists, while the child dials only after it
				// builds its pipeline and answers setup — seconds of
				// head start for the pump's first read. This is a
				// WAIT, never a call end (the phone splice polls the
				// same condition at splice.go's read pump). Treating
				// it as terminal was the link-72a04aebac963cdb
				// half-death: the leg that lost the race had its
				// outboundLoop return at +0 ms (cadence summary with
				// pump_chunks=0 first_child_pcm_set=false), its
				// speech never reached the bridge, and the OTHER
				// leg's client never saw a first byte.
				select {
				case <-ctx.Done():
					return
				case <-time.After(5 * time.Millisecond):
				}
				continue
			}
			if n > 0 {
				// Jitter instrumentation: first child PCM arrival +
				// chunk count (the daemon-side middle the chop hunt
				// was missing).
				s.pumpChunks.Add(1)
				// Drain evidence: the child produced bytes NOW (reopens
				// the quiet window — the store happens before carry/
				// pending are touched, so the watch can never read a
				// quiet window over a just-filled backlog). The carry
				// residue is stored accurately after the chunk-forming
				// loop below.
				s.audioLastAct.Store(time.Now().UnixNano())
				if pumpFirst.IsZero() {
					pumpFirst = time.Now()
					s.log.Info("voice: localsplice first child PCM", "ccid", s.ccid)
				}
				// Fresh child PCM means the call-level side started
				// speaking again: re-arm the barge-in latch so the
				// pacer resumes writing toward the client.
				s.resumeBargeIn()
				carry = append(carry, rb[:n]...)
				for len(carry) >= s.chunkBytes {
					chunk := make([]byte, s.chunkBytes)
					copy(chunk, carry[:s.chunkBytes])
					carry = carry[s.chunkBytes:]
					s.audioCarry.Store(int64(len(carry)))
					s.audioPending.Add(1)
					select {
					case pump <- localReadItem{data: chunk}:
					case <-ctx.Done():
						s.audioPending.Add(-1)
						return
					}
				}
				s.audioCarry.Store(int64(len(carry)))
			}
			if err != nil {
				select {
				case pump <- localReadItem{err: err}:
				case <-ctx.Done():
				}
				return
			}
		}
	}()

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		s.mu.Lock()
		stopped := s.interrupt
		s.mu.Unlock()
		if stopped {
			// Barge-in latched: wait for re-arm (fresh child PCM via
			// the pump), end, or ctx. No idle polling.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-s.wake:
				continue
			}
		}

		// Surface a terminal pump error if it died.
		select {
		case item := <-pump:
			// Drain evidence: a DATA chunk consumed off the pump channel
			// is audio the pacer now OWNS (either written to the client
			// or abandoned at the latch — either way it is no longer
			// backlog). Error items never incremented pending, so they
			// must not decrement it. The activity stamp records the
			// consume; paceOne stamps again after the socket write.
			if item.err == nil {
				s.audioPending.Add(-1)
			}
			s.audioLastAct.Store(time.Now().UnixNano())
			if item.err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				// CL7 backpressure rule: a socket error ends the leg
				// with a named error.
				return fmt.Errorf("voice: localsplice %s child read: %w", s.ccid, item.err)
			}
			if perr := s.paceOne(ctx, item.data); perr != nil {
				return perr
			}
		default:
		}

		// Idle or latched: wait for a state change instead of polling.
		s.mu.Lock()
		stopped = s.interrupt
		s.mu.Unlock()
		if stopped {
			// Deliberate stop: drop the cadence anchor (see paceOne) so
			// the resume is not recorded as one giant stall.
			s.paceDropped++
			s.lastClientWrite = time.Time{}
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case item := <-pump:
			if item.err == nil {
				s.audioPending.Add(-1)
			}
			s.audioLastAct.Store(time.Now().UnixNano())
			if item.err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return fmt.Errorf("voice: localsplice %s child read: %w", s.ccid, item.err)
			}
			if perr := s.paceOne(ctx, item.data); perr != nil {
				return perr
			}
		case <-s.wake:
			// Re-check the latch on the next turn.
		}
	}
}

// paceOne writes one chunk to the client after the pace interval, waiting
// on the wake channel instead of polling timers. The wait happens BEFORE
// the write so a barge-in landing mid-wait stops the very next frame
// (interrupted audio is abandoned - the child aborted its stream, so the
// held frame is dropped, matching RunSplice's resume semantics). A wake
// consumed mid-wait only re-checks the latch; the deadline is honored
// regardless, so the send rate never accelerates.
func (s *runLocalSplice) paceOne(ctx context.Context, chunk []byte) error {
	deadline := time.Now().Add(s.cfg.Pace)
	for {
		s.mu.Lock()
		stopped := s.interrupt || s.ended
		s.mu.Unlock()
		if stopped {
			// The barge-in dead window is a DELIBERATE stop, not
			// jitter: drop the cadence anchor so the first post-resume
			// write does not record one giant "stall" gap.
			s.paceDropped++
			s.lastClientWrite = time.Time{}
			return nil // frame abandoned: barge-in means interrupted audio is dead
		}
		wait := time.Until(deadline)
		if wait <= 0 {
			break
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-s.wake:
			timer.Stop()
			// Re-loop: re-check the latch, keep the original deadline.
		case <-timer.C:
		}
	}

	s.mu.Lock()
	stopped := s.interrupt || s.ended
	s.mu.Unlock()
	if stopped {
		return nil
	}
	if _, err := s.deps.Client.Write(chunk); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// CL7: backpressure (a failed client write) ends the leg with a
		// named error.
		return fmt.Errorf("voice: localsplice %s client write: %w", s.ccid, err)
	}
	// Drain evidence: this chunk has LEFT the pacer for the client
	// socket. Stamp AFTER the write so a slow client socket write keeps
	// the quiet window open (conservative: late, never early).
	s.audioLastAct.Store(time.Now().UnixNano())
	// Jitter instrumentation: the pacer's client-write cadence. A gap of
	// more than 2x the pace interval means the pacer STARVED waiting for
	// child PCM (upstream jitter) rather than paced deliberately.
	now := time.Now()
	if !s.lastClientWrite.IsZero() {
		if g := now.Sub(s.lastClientWrite); g > 2*s.cfg.Pace {
			s.paceOverruns++
			s.log.Warn("voice: localsplice pacer stall", "ccid", s.ccid,
				"gap_ms", g.Milliseconds(), "pace_ms", s.cfg.Pace.Milliseconds())
		} else if g > 0 {
			s.paceSumMs += g.Milliseconds()
			if g.Milliseconds() > s.paceMaxMs {
				s.paceMaxMs = g.Milliseconds()
			}
			s.paceSamples++
		}
	}
	s.paceWrites++
	if s.paceFirstAt.IsZero() {
		s.paceFirstAt = now
	}
	s.lastClientWrite = now
	return nil
}

// ---------- barge-in (per call, per barge-in) ----------

// HandleInterrupt is the leaf 07 entry point: the C1 interrupt frame path
// resolves the live splice by ccid (LocalSpliceFor) and calls this. A
// foreign ccid is a no-op (per-call isolation), and a second interrupt
// for the same barge-in latches (no duplicate clear, no second
// CancelDraft).
func (s *runLocalSplice) HandleInterrupt(ccid string) {
	if ccid != s.ccid {
		return
	}
	s.mu.Lock()
	if s.cancelOne || s.ended {
		s.mu.Unlock()
		return
	}
	s.cancelOne = true
	s.interrupt = true
	s.mu.Unlock()
	s.notify() // stop the pacer's in-flight wait now

	// Byte-exact clear frame to the CHILD via the text seam (review
	// checklist pins the exact bytes; there is no carrier playout to
	// clear - the child's TTS queue is the sink that must flush). The
	// latch is already set, so a send failure is logged, not retried -
	// same shape as RunSplice's audit m1. EncodeFrame's trailing newline
	// is part of the C1 wire contract (the child's read loop is a
	// bufio.Scanner over '\n'-delimited lines); a wiring that writes raw
	// bytes straight to the socket must still see a complete frame here,
	// so the newline is kept.
	frame, encErr := EncodeFrame(Frame{CallID: s.ccid, Event: EventClear})
	if encErr != nil {
		s.log.Warn("voice: barge-in clear frame encode failed", "err", encErr)
	} else {
		if err := s.deps.Text.Send(frame); err != nil {
			s.log.Warn("voice: barge-in clear frame send failed", "err", err)
		}
	}

	if s.deps.CancelDraft != nil {
		s.deps.CancelDraft(s.ccid)
	}
	s.log.Info("voice: local barge-in")
}

// resumeBargeIn re-arms the outbound latch for the NEXT barge-in. Fresh
// child PCM is the resume signal (only a new supervisor-allowed say
// produces PCM - the child aborted the interrupted stream). After end the
// splice stays stopped (ended guard, bughunt F-1 spirit).
func (s *runLocalSplice) resumeBargeIn() {
	s.mu.Lock()
	armed := s.interrupt && !s.ended
	if armed {
		s.interrupt = false
		s.cancelOne = false
	}
	s.mu.Unlock()
	if armed {
		s.notify()
		s.log.Info("voice: local barge-in resumed for next utterance")
	}
}

// ---------- audio-drain watch (link half-duplex floor release) ----------

// ArmDrainWatch arms the per-call audio-drain signal for one turn in the
// BLIND shape (the pre-played-edge form): it arms straight away and the
// fire then waits for a post-arm audio touch + the full quiet window.
// The LINK coordinator no longer calls this on turn_final - that edge is
// ExpectFinalDrain now, and the arm comes from the child's C1 played
// frame via MarkFinalPlayed (a blind arm fired MID-REPLY on inter-
// sentence synth gaps: link-2fdc3f5fd8d81805). Kept for non-link callers
// and the drain-watch test shapes.
//
// Idempotent while a watch is armed (a duplicate arm cannot
// double-fire). A splice with no AudioDrained dep (plain local / phone
// calls — the seam is link-only) is a silent no-op: nothing arms, the
// coordinator rides its fallback margin alone. Safe from any goroutine;
// takes s.mu only for the latch swap (repo mutex-scope rule: no I/O,
// no callback under it).
//
// The bool reports whether the drain signal is now LIVE for this turn
// (armed fresh or joined an armed watch): false means the fire can
// NEVER come (nil dep / ended splice), so the caller can log the
// degraded path instead of silently waiting for a margin release.
func (s *runLocalSplice) ArmDrainWatch() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	if s.deps.AudioDrained == nil || s.ended || s.drainArmed {
		armed := s.deps.AudioDrained != nil && !s.ended
		s.mu.Unlock()
		return armed
	}
	s.drainArmed = true
	s.drainArmedFinal = false
	// Quiet-window floor stored INSIDE the same critical section as the
	// latch: a running watch that observes drainArmed=true through the
	// mutex is happens-after this store. A stale floor (previous turn's
	// arm time) would let the very first tick read "audio touched long
	// ago, now quiet" and fire BEFORE this turn's last say even reached
	// the child - the early-release bug this watch exists to kill.
	s.drainArmedAtNano.Store(time.Now().UnixNano())
	// watchRunning and drainArmed flip in the SAME critical section as
	// any exit transition: an arm can never observe a stale
	// watch-running slot whose goroutine already decided to return.
	startWatch := !s.watchRunning
	s.watchRunning = true
	s.mu.Unlock()

	if startWatch {
		go s.runDrainWatch(s.loopCtx)
	}
	s.log.Info("voice: localsplice drain watch armed", "ccid", s.ccid)
	return true
}

// ExpectFinalDrain records the coordinator's turn_final edge WITHOUT
// arming the watch (link half-duplex, played-edge release). The early
// release it kills: the child synthesizes sentence-by-sentence, and the
// inter-sentence gaps (measured 1.4-3.8 s) exceed the blind watch's
// quiescence window, so turn_final-armed quiescence fires MID-reply while
// queued sentences still have audio to play (link-2fdc3f5fd81805: 60
// short both-audible runs, 10.94 s overlap). With this flag set, the
// floor releases only from the C1 played edge (MarkFinalPlayed) or the
// coordinator's fallback margin. A live blind arm (a non-link caller that
// still uses ArmDrainWatch, or a race where ArmDrainWatch landed first)
// is downgraded to expect-mode here: the pending==0 + quiet predicate
// cannot distinguish "mid-reply synth gap" from "turn over", so the
// played edge replaces it. Safe from any goroutine; takes s.mu only for
// the flag swap (no I/O, no callback under the lock — mutex-scope rule).
// The bool reports drain-signal liveness (same shape as ArmDrainWatch):
// false = no drain edge can EVER fire for this turn (nil dep / ended
// splice), which the coordinator turns into its drain_arm_missed WARN.
func (s *runLocalSplice) ExpectFinalDrain() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	if s.deps.AudioDrained == nil || s.ended {
		s.mu.Unlock()
		return false
	}
	// A fresh turn_final invalidates ANY live arm: a blind arm is the
	// early-fire shape this leaf exists to kill, and a live FINAL arm
	// belongs to the PREVIOUS turn's played edge (its fire would be
	// cross-turn). This turn releases only from its own played edge —
	// or the coordinator's margin. Residual (bounded by design): an
	// edge arriving MORE than a full margin late (11 s+ audio backlog)
	// could arm during the next turn's drain; the coordinator's busy/
	// generation guards cover the mid-turn shape, and the release
	// still requires pending==0 + the quiet window, so the worst case
	// equals the old blind-arm shape, never worse.
	if s.drainArmed {
		s.drainArmed = false
		s.drainArmedFinal = false
	}
	s.drainExpectFinal = true
	// Keep (or start) the watch goroutine in expect mode: the exit check
	// holds the slot open while drainExpectFinal is set, so a played edge
	// arriving on any later tick is picked up without a lost wake-up.
	startWatch := !s.watchRunning
	s.watchRunning = true
	s.mu.Unlock()

	if startWatch {
		go s.runDrainWatch(s.loopCtx)
	}
	s.log.Info("voice: localsplice drain expect-final set", "ccid", s.ccid)
	return true
}

// MarkFinalPlayed arms the drain watch from the child's C1 played edge —
// the true end-of-turn audio marker, sent after the FINAL byte of the
// turn's last:true stream was written to the .out socket. Unlike the
// blind arm it fires on a SILENT post-arm stream: the played edge itself
// is the post-arm touch (the emit happens strictly after the final
// audioOut.Write on the same worker goroutine, and the routed frame
// stamps audioLastAct here), and by construction no more audio for this
// turn can arrive — the quiet window only needs to absorb the last paced
// writes still in the pacer.
//
// Ordering is race-free in both directions: if the played edge beats the
// coordinator's turn_final (tiny turns; the socket and the bus are
// separate paths), the arm happens immediately with the expect flag set
// or not (the edge is authoritative either way); if turn_final lands
// first, ExpectFinalDrain has the watch ready to accept the edge. A
// second played edge while a final arm is live re-stamps the quiet floor
// (conservative: a newer marker means newer audio truth). Returns false
// when no drain signal can ever fire (nil dep / ended splice), mirroring
// ArmDrainWatch's liveness report. Safe from any goroutine.
func (s *runLocalSplice) MarkFinalPlayed() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	if s.deps.AudioDrained == nil || s.ended {
		s.mu.Unlock()
		return false
	}
	s.drainExpectFinal = true
	// The played edge IS the post-arm touch: stamp activity first, then
	// store the floor so the watch's `last < armedAt` guard can never
	// strand a genuinely drained silent stream.
	now := time.Now().UnixNano()
	s.audioLastAct.Store(now)
	if s.drainArmed {
		// A final arm is already live: re-stamp the floor to the new
		// edge (an upgrade from a residual blind arm re-arms final).
		s.drainArmedFinal = true
		s.drainArmedAtNano.Store(now)
		s.mu.Unlock()
		return true
	}
	s.drainArmed = true
	s.drainArmedFinal = true
	s.drainArmedAtNano.Store(now)
	// watchRunning and drainArmed flip in the SAME critical section as
	// any exit transition (same invariant as ArmDrainWatch): an arm can
	// never observe a stale watch-running slot whose goroutine already
	// decided to return.
	startWatch := !s.watchRunning
	s.watchRunning = true
	s.mu.Unlock()

	if startWatch {
		go s.runDrainWatch(s.loopCtx)
	}
	s.log.Info("voice: localsplice drain watch armed on played edge", "ccid", s.ccid)
	return true
}

// TailActive reports whether this splice's OUTBOUND stream was audio-
// active within the last `window`: bytes still queued in the pacer
// (audioPending > 0) or an outbound touch (pump read / paced client
// write, audioLastAct) fresher than window. The link coordinator's
// fallback-margin fire consults it before cutting a holder's floor:
// a live tail means the drain edge is merely slow or lost (old child
// binary, dropped C1 played frame), and a text-side timer releasing
// mid-tail is the measured talk-over (link-b9d221d8d8cddcce). A nil
// receiver (no live splice) reports false — nothing is playing. Takes
// NO lock: audioPending and audioLastAct are atomics the pump/pacer
// stamp lock-free by design (same reads runDrainWatch makes).
func (s *runLocalSplice) TailActive(window time.Duration) bool {
	if s == nil {
		return false
	}
	if s.audioPending.Load() > 0 {
		return true
	}
	last := s.audioLastAct.Load()
	if last == 0 {
		return false
	}
	return time.Since(time.Unix(0, last)) < window
}

// TailAge is the time since the splice's last outbound touch (0 when
// never touched). Telemetry only (margin_rearmed's tail_ms); nil-safe.
func (s *runLocalSplice) TailAge() time.Duration {
	if s == nil {
		return 0
	}
	last := s.audioLastAct.Load()
	if last == 0 {
		return 0
	}
	return time.Since(time.Unix(0, last))
}

// runDrainWatch polls the drain evidence every window/4 and fires
// deps.AudioDrained exactly once per arm. It never takes s.mu across
// the callback (mutex-scope rule), and it exits cleanly when the latch
// goes unconsumed, the splice ends, or the loop ctx dies - no goroutine
// outlives RunLocalSplice. The quiet window is per arm mode: full
// cfg.DrainQuiet for a blind (turn_final-shaped) arm, the shorter
// cfg.DrainQuietFinal for a played-armed watch — after the C1 played
// edge nothing more CAN arrive for the turn, so the window only absorbs
// the last paced writes.
func (s *runLocalSplice) runDrainWatch(ctx context.Context) {
	window := s.cfg.DrainQuiet
	if window <= 0 {
		window = localDrainQuietDefault
	}
	tick := time.NewTicker(window / 4)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			s.releaseWatchSlot()
			return
		case <-tick.C:
		}
		s.mu.Lock()
		armed := s.drainArmed
		armedFinal := s.drainArmedFinal
		expect := s.drainExpectFinal
		stopped := s.interrupt || s.ended
		drainFn := s.deps.AudioDrained
		if (!armed && !expect) || drainFn == nil {
			s.watchRunning = false
			s.mu.Unlock()
			return
		}
		if !armed {
			// Expect-mode park: the played edge has not landed yet and
			// no watch is armed — hold the slot (never evaluate the fire
			// predicate against a stale arm floor), keep ticking so
			// MarkFinalPlayed's arm is visible on the next tick.
			s.mu.Unlock()
			continue
		}
		quietWindow := s.cfg.DrainQuiet
		if armedFinal {
			quietWindow = s.cfg.DrainQuietFinal
			if quietWindow <= 0 {
				quietWindow = localDrainQuietFinal
			}
		}
		s.mu.Unlock()
		if stopped {
			// Barge-in latched (audio deliberately not pacing) or the
			// call ended: keep waiting - the resume re-arms pacing and
			// the next quiet window drains. An ENDED splice never
			// resumes, so the ctx-cancel exit above reaps it.
			continue
		}
		if s.audioPending.Load() > 0 {
			continue // backlog still inside the pacer
		}
		// audioCarry is deliberately NOT in this predicate (measured
		// root cause of the never-firing drain, link-6eb490abd8349958):
		// a real child's TTS byte total is never a multiple of 640, so
		// the pump's sub-chunk residue sits non-zero after EVERY turn
		// and `carry==0` could never be met — zero reason=drained
		// releases while the 4 s margin fired each time. The residue
		// is bytes the pump will never emit as a torn frame; the floor
		// must ride pending==0 + the quiet window instead.
		last := s.audioLastAct.Load()
		armedAt := s.drainArmedAtNano.Load()
		if last < armedAt {
			// NO outbound touch since the arm. Two indistinguishable
			// shapes: the final sentence's cloud TTS has not produced a
			// single byte yet (synth latency - firing now would be
			// EARLY, the exact bug), or the turn genuinely produces no
			// audio at all (fully denied draft / empty reply). The
			// watch never fires on silence-only: the denied-turn case
			// rides the coordinator's fallback margin instead (late,
			// never early - a stale extra hold is harmless).
			continue
		}
		if time.Since(time.Unix(0, last)) < quietWindow {
			continue // quiet window not yet elapsed (conservative)
		}
		// Fire exactly once: consume BOTH latch fields under one lock
		// so a concurrent arm either joins this watch (slot still
		// held) or starts a fresh one (slot released) - never a lost
		// arm with no goroutine behind it. The arm-identity re-check
		// closes the gap between the lock-free predicate read above and
		// this consumption: a FRESH ArmDrainWatch in that window stores
		// a new floor under s.mu, and firing against it without its own
		// quiet window would be an early release for the NEW turn.
		// (Re-reading audioLastAct here is still conservative: the
		// lock-free writers only ever push the fire LATER.)
		s.mu.Lock()
		if !s.drainArmed || s.interrupt || s.ended ||
			s.drainArmedAtNano.Load() != armedAt ||
			s.audioLastAct.Load() < armedAt ||
			time.Since(time.Unix(0, s.audioLastAct.Load())) < quietWindow {
			s.mu.Unlock()
			continue
		}
		s.drainArmed = false
		s.drainArmedFinal = false
		s.drainExpectFinal = false
		s.watchRunning = false
		s.mu.Unlock()
		s.log.Info("voice: localsplice audio drained", "ccid", s.ccid,
			"quiet_ms", quietWindow.Milliseconds())
		drainFn(s.ccid)
		return
	}
}

// releaseWatchSlot frees the running slot (ctx-cancel exit). A later
// arm starts a fresh goroutine; the splice is dying anyway.
func (s *runLocalSplice) releaseWatchSlot() {
	s.mu.Lock()
	s.watchRunning = false
	s.mu.Unlock()
}
