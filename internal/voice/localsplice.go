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
	"time"
)

// ErrNoClient is returned by RunLocalSplice when the client produced no
// first byte within cfg.JoinTimeout.
var ErrNoClient = errors.New("voice: local client never connected")

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

// LocalSpliceConfig sizes the local splice. Zero values pick the CL7
// defaults; ChunkBytes other than 640 (any non-L16@16k negotiation) is a
// named start error.
type LocalSpliceConfig struct {
	Pace        time.Duration // 20ms default; 0 -> 20ms
	ChunkBytes  int           // 0 -> 640 (L16@16k enforced)
	JoinTimeout time.Duration // 0 -> 10s; client must connect/first-byte within it or ErrNoClient
}

// LocalSpliceDeps carries the injected edges. CancelDraft is ALWAYS
// injected by callers (never a direct manager call from the splice); a
// nil value is tolerated as a counted no-op for tests.
type LocalSpliceDeps struct {
	Audio       *AudioPair
	Client      io.ReadWriter // mic bytes in, speaker bytes out
	Text        TextSender    // clear frame to child on barge-in
	CancelDraft func(ccid string)
	Logger      *slog.Logger
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

	mu        sync.Mutex
	interrupt bool // outbound writes latched off for the current barge-in
	cancelOne bool // CancelDraft fired exactly once per barge-in
	ended     bool // teardown latched: the latch must never re-arm after end
	wake      chan struct{}
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

	localSpliceMu.Lock()
	if _, dup := localSpliceByCcid[ccid]; dup {
		localSpliceMu.Unlock()
		return fmt.Errorf("voice: RunLocalSplice %s: a splice is already live for this ccid", ccid)
	}
	localSpliceByCcid[ccid] = s
	localSpliceMu.Unlock()
	defer func() {
		localSpliceMu.Lock()
		delete(localSpliceByCcid, ccid)
		localSpliceMu.Unlock()
	}()

	loopCtx, loopCancel := context.WithCancel(ctx)
	defer loopCancel()

	inboundDone := make(chan error, 1)
	outboundDone := make(chan error, 1)
	go func() { inboundDone <- s.inboundLoop(loopCtx) }()
	go func() { outboundDone <- s.outboundLoop(loopCtx) }()

	errIn := <-inboundDone
	loopCancel() // stop the peer loop now that one side is terminal
	errOut := <-outboundDone

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
	recordFirst := func() { joinOnce.Do(func() { close(joined) }) }

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
				// io.EOF from the client is the client hanging up: a
				// clean end for the leg (the outbound side stops via
				// loopCancel). End is terminal: the barge-in latch
				// must never re-arm afterward (F-1 spirit).
				if errors.Is(item.err, io.EOF) {
					s.mu.Lock()
					s.ended = true
					s.mu.Unlock()
					return nil
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
	go func() {
		carry := make([]byte, 0, 4*s.chunkBytes)
		for {
			rb := make([]byte, s.chunkBytes)
			n, err := s.deps.Audio.ReadFromChild(rb)
			if n > 0 {
				// Fresh child PCM means the call-level side started
				// speaking again: re-arm the barge-in latch so the
				// pacer resumes writing toward the client.
				s.resumeBargeIn()
				carry = append(carry, rb[:n]...)
				for len(carry) >= s.chunkBytes {
					chunk := make([]byte, s.chunkBytes)
					copy(chunk, carry[:s.chunkBytes])
					carry = carry[s.chunkBytes:]
					select {
					case pump <- localReadItem{data: chunk}:
					case <-ctx.Done():
						return
					}
				}
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
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case item := <-pump:
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
