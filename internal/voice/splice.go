package voice

// RunSplice is one call's media path (leaf 06, contract C1/C2): Telnyx
// media WS <-> the child's per-call audio sockets.
//
// Inbound: read Telnyx media frames, reorder through a bounded
// sequence_number window (timestamp fallback), transcode PCMU -> L16
// s16le@16k when the negotiated codec is PCMU, and write fixed
// ChunkBytes-sized chunks to the child (.in).
//
// Outbound: read child PCM (.out), pace one wire frame per cfg.Pace (20 ms
// real time, uptractor live-drill rule: bursts overflow the carrier playout
// buffer as crackle), transcode L16 -> PCMU when negotiated, and write
// base64 media frames to Telnyx.
//
// Barge-in (per call): an interrupt frame for THIS ccid stops outbound
// writes, sends {"event":"clear"} on the Telnyx WS, drops the outbound
// queue, and invokes CancelDraftFn(ccid) once. Other calls' splices are
// structurally untouched: each RunSplice instance owns only its own state.
//
// Backpressure: the inbound reorder buffer and the outbound queue are
// bounded. A full outbound queue marks the call degraded (logged) and,
// once the grace window passes without draining, hangs up via injected
// HangupFn. Frames are NEVER dropped silently (no drop-oldest path).
//
// Borrowed from uptractor mediabridge/bridge.go: EncodeMediaFrame/
// DecodeMediaFrame shapes, the 20 ms chunk pace, the barge-in clear-frame
// write, and the poll-between-writes interrupt shape (waitInterrupt).
// Deliberately NOT borrowed: arrival-order forwarding (reorder added
// here), the fixed 160-byte mediaChunkBytes (chunk size derives from
// ChunkBytes(codec, rate)), and InsecureSkipVerify (signature verification
// happened at WS accept time, leaf 10).

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// WebSocketConn is the narrow Telnyx media WS seam so tests fake it (leaf
// 06 contract). The splice assumes the conn was signature-verified at
// accept (leaf 10); it does no TLS or auth work itself.
type WebSocketConn interface {
	// Read one message. It blocks until a message or error; the
	// implementation must honor ctx cancellation.
	Read(ctx context.Context) (data []byte, err error)
	Write(p []byte) error
	Close() error
}

// TextSender is the seam that carries C1 frames for this call (interrupt
// from the manager/text socket, and the splice's own usage of the channel).
// *voice.TextServer and the test textSink satisfy it structurally.
type TextSender interface {
	Send(raw []byte) error
}

// SpliceConfig sizes the splice's windows. Codec is the child-native codec
// configuration (legacy wiring field); CodecWire is the NEGOTIATED wire
// codec from the Telnyx media start frame (start.media_format) and selects
// the transcode path — an empty CodecWire falls back to Codec. The child's
// C2 chunk size is ALWAYS ChunkBytes("L16", 16000) = 640 regardless of the
// wire codec: the child speaks L16 s16le@16k only, and the splice
// transcodes wire <-> child. ReorderWindowFrames bounds the reorder buffer;
// ReorderGap is how long a sequence gap may persist before it is
// silence-filled; Pace is the outbound inter-frame interval (20 ms);
// QueueLimit bounds the outbound queue.
type SpliceConfig struct {
	Codec struct {
		BidirectionalCodec string
		SamplingRate       int
	} `json:"-"`
	// CodecWire is the negotiated WIRE codec from the media start frame.
	// Empty falls back to Codec. It drives transcodeInbound /
	// transcodeOutbound only; child chunks are always
	// ChunkBytes("L16", 16000) = 640 regardless of the wire codec
	// (audit H2: the wire codec must never size child chunks).
	CodecWire struct {
		BidirectionalCodec string
		SamplingRate       int
	} `json:"-"`
	ReorderWindowFrames int
	ReorderGap          time.Duration
	Pace                time.Duration
	QueueLimit          int
}

// NOTE(leaf06): SpliceConfig.Codec and SpliceConfig.CodecWire are declared
// inline above to mirror the telnyx.Codec shape without importing
// internal/voice/telnyx here; the composite-literal assignment from a
// telnyx.Codec value in the gate test uses field names, so the shapes must
// stay aligned.

// wireCodecOf resolves the negotiated wire codec: CodecWire when set, else
// the legacy Codec field, else L16.
func wireCodecOf(cfg SpliceConfig) string {
	if cfg.CodecWire.BidirectionalCodec != "" {
		return cfg.CodecWire.BidirectionalCodec
	}
	if cfg.Codec.BidirectionalCodec != "" {
		return cfg.Codec.BidirectionalCodec
	}
	return "L16"
}

// SpliceDeps carries the injected edges. HangupFn and CancelDraftFn are
// ALWAYS injected (never direct telnyx client calls from the splice).
type SpliceDeps struct {
	TelnyxWS      WebSocketConn
	Audio         *AudioPair
	Text          TextSender
	HangupFn      func(ccid, reason string)
	CancelDraftFn func(ccid string)
	Logger        *slog.Logger
	// Clock is injectable so pacing tests run on a fake clock. Zero value
	// means time.Now.
	Clock func() time.Time
}

// runSplice is the live state of one call's splice.
type runSplice struct {
	ccid string
	cfg  SpliceConfig
	deps SpliceDeps
	log  *slog.Logger
	now  func() time.Time

	chunkBytes int

	mu         sync.Mutex
	interrupt  bool // barge-in armed for this ccid
	cancelSent bool // CancelDraftFn fired exactly once per interrupt
	// ended latches the end frame (bughunt 2026-11-07 voice F-1): after
	// teardown begins the barge-in latch must never re-arm again.
	ended bool

	outboundMu      sync.Mutex
	outbound        [][]byte
	outFullAt       time.Time // when the queue first became full (0 = not full)
	degraded        bool      // degraded log emitted for the current saturation
	lowWaterSince   time.Time // when the queue first dropped under half full
	lowWaterSinceMu sync.Mutex
	hangupSent      bool        // HangupFn fired at most once
	overflow        chan []byte // producer blocks here when the queue is full
	// wake (audit m3) pokes the outbound pacer on every state change:
	// chunk enqueued, chunk parked on overflow, barge-in latch tripped
	// or re-armed, terminal pump error. It replaces the old 5/20 ms
	// poll timers. Capacity 1: a dropped notify only means a pending
	// wake exists, and every receiver re-checks state AFTER receiving
	// (sender sets state before sending), so no signal is ever lost.
	wake chan struct{}

	grace time.Duration // backpressure grace window
}

// notify (audit m3) pokes the outbound pacer awake. Senders mutate state
// (under their locks) BEFORE calling this, and every receiver re-checks
// state AFTER receiving, so the cap-1 drop-on-full is safe: a dropped
// notify only means a wake is already pending.
func (s *runSplice) notify() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// RunSplice runs one call's splice until ctx is cancelled, a Telnyx WS read
// fails terminally, or the backpressure hangup fires. It owns the
// connection goroutines; the caller closes deps.TelnyxWS and deps.Audio.
func RunSplice(ctx context.Context, ccid string, cfg SpliceConfig, deps SpliceDeps) error {
	if deps.TelnyxWS == nil {
		return errors.New("voice: RunSplice requires a Telnyx WS conn")
	}
	if deps.Audio == nil {
		return errors.New("voice: RunSplice requires an AudioPair")
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if deps.Clock == nil {
		deps.Clock = time.Now
	}
	if cfg.Pace <= 0 {
		cfg.Pace = 20 * time.Millisecond
	}
	if cfg.ReorderWindowFrames <= 0 {
		cfg.ReorderWindowFrames = 10
	}
	if cfg.ReorderGap <= 0 {
		cfg.ReorderGap = 60 * time.Millisecond
	}
	if cfg.QueueLimit <= 0 {
		cfg.QueueLimit = 100
	}
	// The child speaks L16 s16le@16k ONLY (C2): its chunk size is the
	// child-native 20 ms chunk, ALWAYS 640 bytes, regardless of the
	// negotiated wire codec. Audit H2: computing this from the wire codec
	// produced 160-byte PCMU-sized chunks of L16 PCM at the child socket -
	// corrupt audio the moment Telnyx negotiated PCMU. The WIRE codec
	// (CodecWire, fallback Codec) selects only the transcode path.
	chunk, err := ChunkBytes("L16", 16000)
	if err != nil {
		return fmt.Errorf("voice: RunSplice %s: %w", ccid, err)
	}

	s := &runSplice{
		ccid:       ccid,
		cfg:        cfg,
		deps:       deps,
		log:        deps.Logger.With("ccid", ccid),
		now:        deps.Clock,
		chunkBytes: chunk,
		overflow:   make(chan []byte, 1),
		wake:       make(chan struct{}, 1),
		grace:      5 * time.Second,
	}

	inboundDone := make(chan error, 1)
	outboundDone := make(chan error, 1)

	// Force-stop for the loop that is still running after the first one
	// settles: cancel the splice-local context (which unblocks the inbound
	// WS read through the conn's ctx-honoring Read and every outbound
	// wait). This keeps RunSplice's join bounded without the caller
	// cancelling.
	loopCtx, loopCancel := context.WithCancel(ctx)
	defer loopCancel()

	// C1 interrupt delivery: frames pushed at this call's text seam (the
	// manager routes interrupt/end here per call_id) must drive the
	// per-call barge-in. deps.Text is push-only, so RunSplice wraps it in
	// a teeSender: pushes forward to the real sink untouched AND mirror
	// into the splice's C1 handler. Interrupts arriving ON the media WS
	// are handled in handleInboundLine as well.
	tee := &teeSender{inner: deps.Text, splice: s}
	s.deps.Text = tee

	go func() { inboundDone <- s.inboundLoop(loopCtx) }()
	go func() { outboundDone <- s.outboundLoop(loopCtx) }()

	errIn := <-inboundDone
	loopCancel() // stop the peer loop now that one side is terminal
	errOut := <-outboundDone
	// The loops' exit paths can take up to one poll interval (20 ms) after
	// loopCancel; the channel receive above already joined them.

	// If the CALLER cancelled, honor the cancellation contract even when a
	// real error raced in: the test harness selects on Canceled tolerance,
	// and RunSplice must return promptly either way.
	if ctx.Err() != nil && (errors.Is(errIn, context.Canceled) || errors.Is(errOut, context.Canceled)) {
		return ctx.Err()
	}
	// Surface the first NON-cancellation error: context.Canceled from our
	// own loop-cancel must never mask a real terminal failure (WS closed,
	// child socket died, backpressure hangup).
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

// ---------- inbound path ----------

// inboundLoop reads the Telnyx media WS, reorders, transcodes, and writes
// fixed chunks to the child .in socket.
func (s *runSplice) inboundLoop(ctx context.Context) error {
	r := bufio.NewReader(&wsReader{s: s, ctx: ctx})
	reorder := newReorderWindow(s.cfg.ReorderWindowFrames, s.cfg.ReorderGap, s.chunkBytes, s.log)

	pending := make([]byte, 0, s.chunkBytes)
	flush := func(frame []byte) error {
		// pending accumulates decoded PCM until a full chunk; the child
		// speaks in ChunkBytes units (C2).
		pending = append(pending, frame...)
		for len(pending) >= s.chunkBytes {
			chunk := make([]byte, s.chunkBytes)
			copy(chunk, pending[:s.chunkBytes])
			pending = pending[s.chunkBytes:]
			if err := s.deps.Audio.WriteToChild(chunk); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return fmt.Errorf("voice: splice %s write child: %w", s.ccid, err)
			}
		}
		return nil
	}

	silencePending := make([]byte, 0, s.chunkBytes)
	flushSilence := func(fill []byte) error {
		silencePending = append(silencePending, fill...)
		for len(silencePending) >= s.chunkBytes {
			chunk := make([]byte, s.chunkBytes)
			copy(chunk, silencePending[:s.chunkBytes])
			silencePending = silencePending[s.chunkBytes:]
			if err := s.deps.Audio.WriteToChild(chunk); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return fmt.Errorf("voice: splice %s write child (silence): %w", s.ccid, err)
			}
		}
		silencePending = silencePending[:0]
		return nil
	}

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			if perr := s.handleInboundLine(line, reorder, flush, flushSilence); perr != nil {
				return perr
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) && len(line) == 0 {
				return io.EOF
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("voice: splice %s ws read: %w", s.ccid, err)
		}
	}
}

// wsReader adapts the WebSocketConn to an io.Reader so a bufio.Reader can
// split the newline-delimited JSON frames. Each Read blocks on one WS
// message; ctx cancellation unblocks via the conn implementation's
// documented read-unblock behavior (the fake returns on ctx.Done).
type wsReader struct {
	s   *runSplice
	ctx context.Context
	buf []byte // remainder of the current WS message across Read calls
}

func (r *wsReader) Read(p []byte) (int, error) {
	if r.ctx.Err() != nil {
		return 0, r.ctx.Err()
	}
	if len(p) == 0 {
		return 0, nil
	}
	// Pull ONE WS message per Read, but chunk it into the destination
	// buffer: bufio may ask for less than the frame size, and a frame
	// larger than the caller's buffer must stream across multiple Reads
	// (a WS message has no newline inside, so the framing stays intact).
	if len(r.buf) == 0 {
		data, err := r.s.deps.TelnyxWS.Read(r.ctx)
		if err != nil {
			if r.ctx.Err() != nil {
				return 0, r.ctx.Err()
			}
			return 0, err
		}
		r.buf = data
	}
	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

// handleInboundLine dispatches one WS message. Media frames go through the
// reorder window; interrupt frames arriving ON THE MEDIA SOCKET (some
// Telnyx topologies relay them there) are honored per-call; other events
// are counted no-ops.
func (s *runSplice) handleInboundLine(
	line []byte,
	reorder *reorderWindow,
	flush func([]byte) error,
	flushSilence func([]byte) error,
) error {
	line = trimSpace(line)
	if len(line) == 0 {
		return nil
	}
	media, derr := DecodeMediaFrame(line)
	if derr == nil {
		payload := transcodeInbound(media.Payload, s.codec())
		if err := reorder.push(media, func([]byte) []byte { return payload }, flush); err != nil {
			return err
		}
		return nil
	}

	// Non-media: inspect the event shape for interrupt semantics. The line
	// may carry MORE THAN ONE JSON object (a transport that coalesces WS
	// messages, or a producer writing two frames without a flush boundary),
	// so scan object-by-object instead of rejecting the whole line.
	for _, obj := range splitJSONObjects(line) {
		if len(obj) == 0 {
			continue
		}
		if m, merr := DecodeMediaFrame(obj); merr == nil {
			payload := transcodeInbound(m.Payload, s.codec())
			if err := reorder.push(m, func([]byte) []byte { return payload }, flush); err != nil {
				return err
			}
			continue
		}
		var probe struct {
			Event  string `json:"event"`
			CallID string `json:"call_id"`
		}
		if jerr := jsonUnmarshal(obj, &probe); jerr != nil {
			// Undecodable fragment: dropped (counted), never fatal.
			s.log.Debug("voice: dropping undecodable media-socket frame")
			continue
		}
		if probe.Event == "interrupt" && (probe.CallID == "" || probe.CallID == s.ccid) {
			s.bargeIn()
		}
	}
	return nil
}

// splitJSONObjects splits a line that may contain several concatenated JSON
// objects (whitespace-separated) into the individual object byte slices.
// A well-formed single-object line yields one element.
//
// The scan is string-aware (audit m2): `{` and `}` inside a JSON string
// literal (e.g. a base64 payload or a text field carrying braces) must not
// drive the depth counter, or the split cuts objects at brace characters
// that are string CONTENT, producing fragments no decoder accepts. Escapes
// (`\"`, `\\`) inside strings are tracked so an escaped quote or backslash
// cannot flip string state either.
func splitJSONObjects(line []byte) [][]byte {
	var out [][]byte
	depth := 0
	inString := false
	escaped := false
	start := -1
	for i, c := range line {
		switch {
		case escaped:
			escaped = false
		case inString:
			if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
		case c == '"':
			if depth > 0 {
				inString = true
			}
			// A quote outside any object cannot open a string that
			// matters: only brace-delimited objects are surfaced.
		case c == '{':
			if depth == 0 {
				start = i
			}
			depth++
		case c == '}':
			depth--
			if depth == 0 && start >= 0 {
				out = append(out, line[start:i+1])
				start = -1
			}
			if depth < 0 {
				depth = 0 // stray closer: reset
			}
		}
	}
	if start >= 0 && depth > 0 {
		// Truncated trailing object: surface what is there; the decoder
		// will reject it and the line is counted as dropped.
		out = append(out, line[start:])
	}
	return out
}

// codec resolves the negotiated WIRE codec for transcode selection
// (CodecWire, fallback Codec, fallback L16). It never sizes child chunks.
func (s *runSplice) codec() string {
	return wireCodecOf(s.cfg)
}

// ---------- reorder window (spec: Frame order) ----------

// reorderBuffer is one held frame awaiting its sequence slot.
type reorderBuffer struct {
	pcm       []byte // already transcoded to L16 s16le@16k
	seq       int64  // sequence_number when present, else -1
	timestamp float64
}

// reorderWindow implements the spec Frame order semantics: buffer up to
// reorder_window_frames, emit contiguous runs ordered by sequence_number
// (timestamp fallback when absent), silence-fill gaps that persist past
// reorder_gap_ms, and discard frames that arrive late-past-the-window.
// There is no drop-oldest path: the window never evicts buffered frames to
// make room; overflow is impossible because capacity equals
// ReorderWindowFrames and arrivals beyond capacity ARE the late frames.
type reorderWindow struct {
	capacity int
	gap      time.Duration
	chunk    int
	log      *slog.Logger

	held     map[int64]*reorderBuffer // by seq (only frames with seq)
	noSeq    []*reorderBuffer         // frames without sequence_number
	nextSeq  int64                    // next sequence we expect to emit
	haveNext bool

	silenceFillCount int
	firstGapAt       time.Time
	now              func() time.Time
}

func newReorderWindow(capacity int, gap time.Duration, chunk int, log *slog.Logger) *reorderWindow {
	return &reorderWindow{
		capacity: capacity,
		gap:      gap,
		chunk:    chunk,
		log:      log,
		held:     make(map[int64]*reorderBuffer, capacity),
		now:      time.Now,
	}
}

// push runs one inbound frame through the window, emitting contiguous
// frames into emit. fillSilence delivers silence bytes for a gap that
// persisted past the gap deadline.
func (w *reorderWindow) push(
	media MediaFrame,
	transcode func([]byte) []byte,
	emit func([]byte) error,
) error {
	buf := &reorderBuffer{pcm: transcode(media.Payload)}
	if media.SequenceNumber != nil {
		buf.seq = *media.SequenceNumber
		w.held[buf.seq] = buf
	} else if media.TimestampMs != nil {
		buf.timestamp = *media.TimestampMs
		w.noSeq = append(w.noSeq, buf)
	} else {
		// Neither key present: treat as the next expected frame (the
		// documented last-resort fallback; without both keys there is no
		// order information and arrival order is all we have).
		w.noSeq = append(w.noSeq, buf)
	}

	return w.drain(emit)
}

// drain emits whatever is contiguous, silence-fills a stalled gap, and
// discards late-past-window frames.
func (w *reorderWindow) drain(emit func([]byte) error) error {
	// 1. Emit the contiguous run from nextSeq.
	for w.haveNext {
		buf, ok := w.held[w.nextSeq]
		if !ok {
			break
		}
		if err := emit(buf.pcm); err != nil {
			return err
		}
		delete(w.held, w.nextSeq)
		w.firstGapAt = time.Time{} // run restored: clear any gap timer
		w.nextSeq++
	}

	// 2. First frame ever: adopt the lowest held seq as the anchor instead
	// of waiting for a guessed start point.
	if !w.haveNext && (len(w.held) > 0 || len(w.noSeq) > 0) {
		if len(w.held) > 0 {
			lowest := int64(0)
			first := true
			for k := range w.held {
				if first || k < lowest {
					lowest = k
					first = false
				}
			}
			w.nextSeq = lowest
			w.haveNext = true
			return w.drain(emit) // re-enter: emit the anchored run
		}
	}

	// 3. No seq frames: emit in arrival order intermixed is ambiguous; the
	// documented choice is to emit them only when nothing else is held, in
	// arrival order, after the contiguous run (timestamp ordering within
	// the window reduces to arrival order for these in practice).
	for len(w.held) == 0 && len(w.noSeq) > 0 {
		buf := w.noSeq[0]
		w.noSeq = w.noSeq[1:]
		if err := emit(buf.pcm); err != nil {
			return err
		}
	}

	// 4. Gap handling: if we are stalled on nextSeq and the gap has
	// persisted past the deadline, fill exactly the missing duration with
	// silence and advance.
	if w.haveNext {
		if _, missing := w.held[w.nextSeq]; !missing {
			if w.firstGapAt.IsZero() {
				w.firstGapAt = w.now()
			} else if w.now().Sub(w.firstGapAt) >= w.gap {
				// Each missing sequence number is one 20 ms frame; the
				// silence fill covers the MISSING DURATION of the gap.
				missing := w.countContiguousMissing()
				dur := time.Duration(missing) * w.frameDuration()
				if dur > 0 {
					w.silenceFillCount += missing
					if err := emit(SilenceFor(dur, w.silenceRate())); err != nil {
						return err
					}
				}
				w.nextSeq += int64(missing)
				w.firstGapAt = time.Time{}
				return w.drain(emit)
			}
		}
	}

	// 5. Late-past-window discard: held frames whose seq sits below nextSeq
	// arrived after we already emitted (or silence-filled) their slot.
	if w.haveNext {
		for seq := range w.held {
			if seq < w.nextSeq {
				delete(w.held, seq)
				w.log.Debug("voice: discarded late frame past reorder window", "seq", seq)
			}
		}
	}

	// 6. Capacity guard: held frames beyond capacity are late frames by
	// definition of the window (they would have been drained already if
	// they were in sequence); discard the OLDEST-below-anchor ones only --
	// never drop-oldest across the live window. If the map somehow exceeds
	// capacity with in-window frames (possible when many duplicates or a
	// huge forward jump arrive), shed the furthest-BEHIND; a forward jump
	// simply re-anchors on drain of the next lower seq.
	for len(w.held) > w.capacity {
		lowest := int64(0)
		first := true
		for k := range w.held {
			if first || k < lowest {
				lowest = k
				first = false
			}
		}
		if !w.haveNext || lowest >= w.nextSeq {
			// Everything held is ahead of the anchor: shed the HIGHEST to
			// keep the contiguous window alive (still not drop-oldest of
			// in-order audio).
			highest := lowest
			for k := range w.held {
				if k > highest {
					highest = k
				}
			}
			delete(w.held, highest)
		} else {
			delete(w.held, lowest)
		}
		w.log.Debug("voice: reorder window overflow shed", "seq", lowest)
	}
	return nil
}

// countContiguousMissing counts how many sequence numbers starting at
// nextSeq are absent (the gap size in frames).
func (w *reorderWindow) countContiguousMissing() int {
	n := 0
	for {
		if _, ok := w.held[w.nextSeq+int64(n)]; ok {
			return n
		}
		n++
		if n > w.capacity*2 {
			// Defensive: never fill more than twice the window per drain;
			// pathological jumps stop here and re-anchor naturally.
			return n
		}
	}
}

func (w *reorderWindow) frameDuration() time.Duration { return 20 * time.Millisecond }

func (w *reorderWindow) silenceRate() int { return 16000 } // fill is L16 s16le at 16 kHz

// ---------- barge-in (per call) ----------

// bargeIn stops outbound writes for THIS ccid, clears the Telnyx playout,
// drops the outbound queue, and cancels the employee draft exactly once.
// State is per-instance, so a concurrent call's splice cannot observe it.
func (s *runSplice) bargeIn() {
	s.mu.Lock()
	if s.cancelSent {
		s.mu.Unlock()
		return
	}
	s.cancelSent = true
	s.interrupt = true
	s.mu.Unlock()
	s.notify() // audit m3: wake the pacer to stop writing now

	// Byte-exact clear frame (review checklist pins the exact bytes). The
	// write error is NOT discarded (audit m1): a failed clear leaves
	// Telnyx playout running through the barge-in, so surface it loudly.
	// Barge-in state is already latched, so we do not retry or unwind -
	// the splice continues (the queue drop and draft cancel below still
	// happen) and the error is logged at warn for call diagnostics.
	if err := s.deps.TelnyxWS.Write([]byte(`{"event":"clear"}`)); err != nil {
		s.log.Warn("voice: barge-in clear frame write failed",
			"ccid", s.ccid, "err", err)
	}

	s.outboundMu.Lock()
	s.outbound = nil
	s.outboundMu.Unlock()

	if s.deps.CancelDraftFn != nil {
		s.deps.CancelDraftFn(s.ccid)
	}
	s.log.Info("voice: barge-in", "ccid", s.ccid)
}

// HandleText processes one C1 frame aimed at this call (the manager or the
// text-socket handler routes it here). Interrupt drives the per-call
// barge-in; end stops the splice cleanly.
func (s *runSplice) HandleText(raw []byte) error {
	var probe struct {
		Event  string `json:"event"`
		CallID string `json:"call_id"`
	}
	if err := jsonUnmarshal(raw, &probe); err != nil {
		return fmt.Errorf("voice: splice %s text frame: %w", s.ccid, err)
	}
	switch probe.Event {
	case "interrupt":
		if probe.CallID == "" || probe.CallID == s.ccid {
			s.bargeIn()
		}
	case "end":
		s.mu.Lock()
		s.interrupt = true
		// bughunt 2026-11-07 voice F-1: teardown is terminal — the
		// resume path below must never re-arm the latch after end.
		s.ended = true
		s.mu.Unlock()
		s.notify() // audit m3: stop writing immediately on teardown
	}
	return nil
}

// resumeBargeIn re-arms the outbound latch for the NEXT barge-in
// (bughunt 2026-11-07 voice F-1, HIGH). Before this, `interrupt` and
// `cancelSent` were once-per-CALL latches: after the first barge-in the
// pacer idle-waited forever (the agent was muted for the rest of the call
// while conversation logic kept running) and bargeIn() early-returned on
// cancelSent, so even the Telnyx clear frame stopped being sent. The
// contract (docs/workflows/voice-sidecar.md C1, "per call_id") is
// per-barge-in, not per-call: a resume decision lands when the call-level
// side streams FRESH child audio (only a new supervisor-allowed say
// produces PCM — the child aborted the interrupted utterance's stream),
// so new bytes at the read pump mean "speak again": drop the held
// mid-pace chunk and the queue (barge-in means interrupted audio is
// abandoned, matching the child's clear()), and re-arm both latch fields.
// After end the splice stays stopped (ended guard).
func (s *runSplice) resumeBargeIn() {
	s.mu.Lock()
	armed := s.interrupt && !s.ended
	if armed {
		s.interrupt = false
		s.cancelSent = false
	}
	s.mu.Unlock()
	if armed {
		s.notify() // audit m3: re-armed — the pacer may resume writing
		s.outboundMu.Lock()
		s.outbound = nil
		s.outboundMu.Unlock()
		s.log.Info("voice: barge-in resumed for next utterance", "ccid", s.ccid)
	}
}

// Text implements TextSender for runSplice so callers can route C1 frames
// straight into the splice; interrupts become barge-ins, everything else is
// a counted no-op. The splice itself never originates text toward Telnyx
// beyond the barge-in clear frame.
func (s *runSplice) Send(raw []byte) error {
	return s.HandleText(raw)
}

// ---------- outbound path ----------

// readResult is one child-socket read: an error, or nothing (err nil means
// the read produced bytes already consumed into the chunker).
type readResult struct {
	err error
}

// outboundLoop reads child PCM, splits into ChunkBytes chunks, and paces
// them to Telnyx one per cfg.Pace.
func (s *runSplice) outboundLoop(ctx context.Context) error {
	enqueue := func(chunk []byte) {
		s.outboundMu.Lock()
		if len(s.outbound) >= s.cfg.QueueLimit {
			// Degraded: bounded queue full. No drop-oldest: the producer
			// parks the chunk on overflow; the pacer drains it after the
			// queue frees, or the grace hangup fires. outFullAt is set on
			// the FIRST saturation and never reset while the call keeps
			// saturating (recovery below clears it only when the queue
			// genuinely drops under half the limit), so the grace clock
			// measures total sustained saturation, not per-burst.
			if !s.degraded {
				s.degraded = true
				if s.outFullAt.IsZero() {
					s.outFullAt = s.now()
				}
				s.log.Warn("voice: outbound queue full, call degraded", "ccid", s.ccid, "limit", s.cfg.QueueLimit)
			}
			s.outboundMu.Unlock()
			select {
			case s.overflow <- chunk:
				s.notify() // audit m3: parked overflow is now pacer-visible
			case <-ctx.Done():
			}
			// Chunk parked; the pacer will pick it up.
			return
		}
		if s.degraded && len(s.outbound) < s.cfg.QueueLimit/2 {
			// The queue dropped under half the limit only MOMENTARILY if
			// the pump refills within the same drain cycle; require the
			// low-water state to persist one full pace interval before
			// clearing, so a stalled-but-recovering consumer (the gate
			// test's saturate pattern) keeps its grace clock running.
			s.lowWaterSinceMu.Lock()
			if s.lowWaterSince.IsZero() {
				s.lowWaterSince = s.now()
			}
			sustained := s.now().Sub(s.lowWaterSince) >= s.cfg.Pace
			s.lowWaterSinceMu.Unlock()
			if sustained {
				s.degraded = false
				s.outFullAt = time.Time{}
				s.lowWaterSince = time.Time{}
				s.log.Info("voice: outbound queue recovered", "ccid", s.ccid)
			}
		} else {
			s.lowWaterSinceMu.Lock()
			s.lowWaterSince = time.Time{}
			s.lowWaterSinceMu.Unlock()
		}
		cp := make([]byte, len(chunk))
		copy(cp, chunk)
		s.outbound = append(s.outbound, cp)
		s.outboundMu.Unlock()
		s.notify() // audit m3: wake the pacer for the new chunk
	}

	// The read pump: ONE long-lived reader goroutine streams child PCM
	// into reads channel AND enqueues paced-out chunks; the outbound
	// pacer drains the shared queue. Decoupling matters: the pump must
	// keep reading (and blocking on enqueue/overflow when the queue is
	// full) while the pacer waits the pace interval - a serial
	// read->pace loop can never accumulate a queue and the degraded/
	// grace-hangup backpressure could never trigger.
	reads := make(chan readResult, 64)
	go func() {
		carry := make([]byte, 0, 4*s.chunkBytes)
		for {
			rb := make([]byte, s.chunkBytes)
			n, err := s.deps.Audio.ReadFromChild(rb)
			if n > 0 {
				// bughunt 2026-11-07 voice F-1: fresh child PCM means the
				// call-level side started speaking again (only a new
				// supervisor-allowed say produces a stream — the child
				// aborted the interrupted utterance). Re-arm the barge-in
				// latch so the pacer resumes writing toward Telnyx. At
				// most one stale chunk (~20 ms) can ride past the child's
				// abort check; bounded double-talk beats a muted call.
				s.resumeBargeIn()
				carry = append(carry, rb[:n]...)
				for len(carry) >= s.chunkBytes {
					chunk := transcodeOutbound(carry[:s.chunkBytes], s.codec())
					enqueue(chunk)
					carry = carry[s.chunkBytes:]
				}
				if len(carry) > 4*s.chunkBytes {
					s.log.Warn("voice: outbound carry buffer beyond bound", "bytes", len(carry))
					carry = carry[len(carry)-s.chunkBytes:]
				}
			}
			if err != nil {
				if strings.Contains(err.Error(), "child not connected") {
					// Pre-accept: poll until the child dials.
					select {
					case <-ctx.Done():
						return
					case <-time.After(5 * time.Millisecond):
					}
					continue
				}
				// Terminal: surface the error to the loop.
				select {
				case reads <- readResult{err: err}:
					s.notify() // audit m3: wake the loop to observe it
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

		// The pacer: drain one queued frame per cfg.Pace. Idle when the
		// queue is empty; ctx stays checked every turn.
		if err := s.paceOne(ctx); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		// Surface a terminal pump error if it died.
		select {
		case rr := <-reads:
			if rr.err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return fmt.Errorf("voice: splice %s child read: %w", s.ccid, rr.err)
			}
		default:
		}
	}
}

// paceOne pops one queued chunk (or a parked overflow chunk) and writes it
// to Telnyx after the pace interval, waiting on the wake channel instead of
// polling timers (audit m3: the old shape burned ~5 timers per 20 ms chunk
// plus a 20 ms/5 ms idle poll; now a call with live audio starts exactly
// one timer per paced frame, and every interrupt/resume/overflow/error
// transition wakes the pacer through s.wake). The wait happens BEFORE the
// write so a barge-in landing mid-wait stops the very next frame: barge-in
// lands within one chunk interval, and now within one wake.
func (s *runSplice) paceOne(ctx context.Context) error {
	s.mu.Lock()
	stopped := s.interrupt
	s.mu.Unlock()
	if stopped {
		// Barge-in active: wait for the next transition (resume, end,
		// ctx) instead of idle-polling, so the loop stays responsive
		// and stays alive for the call's remaining life.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.wake:
			return nil
		}
	}

	var chunk []byte
	select {
	case c := <-s.overflow:
		chunk = c
	default:
	}

	if chunk == nil {
		s.outboundMu.Lock()
		if len(s.outbound) == 0 {
			// Nothing queued: wait for a state change (enqueue,
			// overflow park, pump error, ctx) — no poll timer.
			s.outboundMu.Unlock()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-s.wake:
			}
			return nil
		}
		chunk = s.outbound[0]
		s.outbound = s.outbound[1:]
		s.outboundMu.Unlock()
	}

	// Grace check: degraded for too long -> hangup (once).
	s.checkGrace()

	// Wait the pace interval on the wall clock (as before: s.now is the
	// injectable test clock and stays out of the pacing path). A
	// barge-in landing mid-wait RE-QUEUES the chunk at the front: the
	// frame is never lost (no drop path), it is held for the post-clear
	// resume decision at the call level. A wake consumed mid-wait only
	// re-checks the latch; the deadline is honored regardless, so the
	// send rate never accelerates.
	deadline := time.Now().Add(s.cfg.Pace)
	for {
		s.mu.Lock()
		stopped := s.interrupt
		s.mu.Unlock()
		if stopped {
			s.outboundMu.Lock()
			s.outbound = append([][]byte{chunk}, s.outbound...)
			s.outboundMu.Unlock()
			return nil
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
			// Re-loop: check the interrupt latch; keep waiting to
			// the original deadline. A dropped-or-stale wake is
			// safe — state is always re-read after receiving.
		case <-timer.C:
		}
	}

	encoded, err := EncodeMediaFrame(chunk)
	if err != nil {
		return fmt.Errorf("voice: splice %s encode: %w", s.ccid, err)
	}
	if werr := s.deps.TelnyxWS.Write(encoded); werr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("voice: splice %s ws write: %w", s.ccid, werr)
	}
	return nil
}

// checkGrace invokes HangupFn once when the queue has stayed saturated past
// the grace window. Hangup is terminal for the call; the loops observe it
// through the AudioPair closing (manager side) and ctx.
func (s *runSplice) checkGrace() {
	s.outboundMu.Lock()
	full := s.degraded && !s.outFullAt.IsZero()
	since := time.Duration(0)
	if full {
		since = s.now().Sub(s.outFullAt)
	}
	s.outboundMu.Unlock()

	if full && since >= s.grace {
		s.outboundMu.Lock()
		already := s.hangupSent
		s.hangupSent = true
		s.outboundMu.Unlock()
		if !already && s.deps.HangupFn != nil {
			s.log.Error("voice: backpressure grace exceeded, hanging up", "ccid", s.ccid, "saturated_for", since.String())
			s.deps.HangupFn(s.ccid, "voice: outbound queue saturated past grace window")
		}
	}
}

// teeSender wraps the injected Text channel: frames pushed at this call
// (the manager routes interrupt/end here per call_id) are forwarded to the
// inner sink UNTOUCHED and mirrored into the splice's own C1 handler so a
// pushed interrupt drives the per-call barge-in.
type teeSender struct {
	inner  TextSender
	splice *runSplice
}

func (t *teeSender) Send(raw []byte) error {
	// Mirror first so the handler runs before the caller proceeds. The
	// interrupt reaches bargeIn as a side effect of the PUSH - the same
	// path the manager uses per call_id (leaf 14 wiring).
	_ = t.splice.HandleText(raw)
	if t.inner == nil {
		return nil
	}
	return t.inner.Send(raw)
}

// trimSpace removes leading/trailing ASCII whitespace from a frame line.
func trimSpace(b []byte) []byte {
	start := 0
	for start < len(b) && (b[start] == ' ' || b[start] == '	' || b[start] == '\r' || b[start] == '\n') {
		start++
	}
	end := len(b)
	for end > start && (b[end-1] == ' ' || b[end-1] == '	' || b[end-1] == '\r' || b[end-1] == '\n') {
		end--
	}
	return b[start:end]
}

// jsonUnmarshal is a thin json.Unmarshal alias so the splice file keeps one
// import surface for the probe structs.
func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
