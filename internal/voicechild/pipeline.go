package voicechild

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/voiceframe"
)

// STTProvider turns a stream of PCM chunks into transcripts. Stubbed in
// this leaf; leaf 08 implements real adapters (Deepgram et al).
type STTProvider interface {
	Stream(ctx context.Context, pcm <-chan []byte) (<-chan Transcript, error)
}

// TTSProvider synthesizes speech audio for one text unit. Stubbed in this
// leaf; leaf 08 implements real adapters.
type TTSProvider interface {
	Speak(ctx context.Context, text string) (io.ReadCloser, error)
}

// Transcript is one recognition result from an STTProvider.
type Transcript struct {
	Text  string
	Final bool
}

// pipeline is one live call: its jargo worker and the per-call audio pair
// client sockets (C2). All public entry points are safe for concurrent use;
// a child hosts N pipelines concurrently (map[ccid]*pipeline in child.go).
type pipeline struct {
	ccid     string
	codec    string
	sampleRt int
	child    *Child
	logger   *slog.Logger
	stt      STTProvider    // stub this leaf
	tts      TTSProvider    // stub this leaf
	sttKey   string         // provider keys, in memory only (leaf 08 consumes)
	ttsKey   string         // provider keys, in memory only (leaf 08 consumes)
	sttCfg   ProviderConfig // per-call vendor/model/base_url/voice (C1 keys frame)
	ttsCfg   ProviderConfig // per-call vendor/model/base_url/voice (C1 keys frame)

	// STT wiring (leaf 05, C5 prompt half). pcmCh carries daemon->child
	// PCM from pumpAudioIn into the STT Stream session; transcripts is the
	// session's output, consumed by consumeTranscripts. transMu guards the
	// exactly-once transcript startup (Stream + consumer exactly once per
	// pipeline, whichever of start/handleKeys runs first).
	pcmCh        chan []byte       // fed by pumpAudioIn, consumed by Stream (bounded, oldest-chunk drop)
	transcripts  <-chan Transcript // STT session output (nil until startTranscripts binds)
	transMu      sync.Mutex        // guards transStarted
	transStarted bool              // Stream session + consumer launched
	trStart      chan struct{}     // closed when the transcript consumer exits (teardown wait)
	pumpOnce     sync.Once         // start() is idempotent: exactly one pump goroutine
	audioOut     net.Conn          // child->daemon PCM (C2 <ccid>.out)
	audioIn      net.Conn          // daemon->child PCM (C2 <ccid>.in)
	audioDir     string

	// open-frame profile (local-leg plan CL1/CL3), set once by handleOpen
	// before the pipeline starts; read-only afterwards (no lock needed).
	// Providers themselves keep arriving over the keys frame (Tier A owns
	// that wire), so this carries ONLY what the local leg needs early:
	// the leg marker (RMS barge-in runs when local) and its threshold.
	local      bool
	bargeInRms int

	// playback state, guarded by playMu
	playMu      sync.Mutex
	dropQueued  bool   // clear received: drop queued audio, stop writes
	interrupted bool   // one interrupt per barge-in until playback restarts
	speaking    bool   // playback active (say in flight)
	outCh       []byte // queued PCM awaiting write to audioOut
	// playGen (bughunt 2026-11-07 voice F-1): bumped by clear(); each
	// playback goroutine carries the gen it started under and aborts when
	// the gen moves. dropQueued becomes per-barge-in (reset by the next
	// say), so the stale stream stays dead via gen mismatch while a new
	// utterance plays again — without this reset the agent was muted for
	// the whole call after its first interrupt.
	playGen uint64
	// rmsRun (local-leg leaf 02): consecutive over-threshold chunk count
	// of the playback-side RMS VAD; guarded by playMu, reset by latch
	// state changes (see rmsBargeInCheck).
	rmsRun int

	// cancel cancels the pipeline's internal context (workers, streams).
	cancel context.CancelFunc
	ctx    context.Context
}

// newPipeline builds the per-call pipeline from a setup frame: dials the
// C2 audio pair and constructs the (stubbed) jargo worker chain.
func (c *Child) newPipeline(frame voiceframe.Frame) (*pipeline, error) {
	if c.cfg.AudioDir == "" {
		return nil, errors.New("voicechild: audio dir is empty")
	}
	inPath := filepath.Join(c.cfg.AudioDir, frame.CallID+".in")
	outPath := filepath.Join(c.cfg.AudioDir, frame.CallID+".out")
	// Short-lived dials with deadlines; the daemon listeners accept one
	// connection per socket (AudioPair doc).
	dialer := net.Dialer{}
	inConn, err := dialer.DialContext(context.Background(), "unix", inPath)
	if err != nil {
		return nil, fmt.Errorf("voicechild: dial audio in %s: %w", inPath, err)
	}
	outConn, err := dialer.DialContext(context.Background(), "unix", outPath)
	if err != nil {
		_ = inConn.Close() //nolint:mutexio // error path, no lock held
		return nil, fmt.Errorf("voicechild: dial audio out %s: %w", outPath, err)
	}

	pctx, cancel := context.WithCancel(context.Background())
	p := &pipeline{
		ccid:     frame.CallID,
		codec:    frame.Codec,
		sampleRt: frame.SampleRate,
		child:    c,
		logger:   c.logger,
		stt:      stubSTT{},
		tts:      stubTTS{},
		// Bounded to 8 chunks (160 ms at the L16/16k default). On overflow
		// the OLDEST chunk is dropped (see pumpAudioIn): for a phone call,
		// audio latency beats completeness.
		pcmCh:    make(chan []byte, 8),
		trStart:  make(chan struct{}),
		audioIn:  inConn,
		audioOut: outConn,
		audioDir: c.cfg.AudioDir,
		ctx:      pctx,
		cancel:   cancel,
	}
	return p, nil
}

// start launches the pipeline's background work: the daemon->child audio
// pump and, if a real STT provider is already installed (keys arrived
// before setup), the transcript session. When keys arrive later, handleKeys
// calls startTranscripts at the hook point; the once-guard makes both
// orders behave identically (exactly one Stream + one consumer).
func (p *pipeline) start(ctx context.Context) error {
	p.pumpOnce.Do(func() { go p.pumpAudioIn() })
	p.startTranscripts()
	return nil
}

// startTranscripts binds the STT Stream session and spawns the transcript
// consumer, exactly once per pipeline. When the pipeline still carries the
// stub STT (keys not yet arrived), it is a no-op: handleKeys retries at the
// constructProviders hook point. Safe to call from start and handleKeys in
// either order (T3).
func (p *pipeline) startTranscripts() {
	p.transMu.Lock()
	if p.transStarted {
		p.transMu.Unlock()
		return
	}
	if _, isStub := p.stt.(stubSTT); isStub || p.stt == nil {
		// No real provider yet: the keys-arrives hook will call again.
		p.transMu.Unlock()
		return
	}
	p.transStarted = true
	stt := p.stt
	p.transMu.Unlock()

	// Stream once per pipeline (C5). The session dies with p.ctx on
	// teardown; closing pcmCh is the pump's job (socket EOF or ctx cancel).
	trans, err := stt.Stream(p.ctx, p.pcmCh)
	if err != nil {
		p.logger.Error("voicechild: stt stream open", "err", err, "call_id", p.ccid)
		// Release the once-guard: a transient adapter failure should not
		// strand the call silent forever (one retry on the next hook).
		p.transMu.Lock()
		p.transStarted = false
		p.transMu.Unlock()
		return
	}
	p.transMu.Lock()
	p.transcripts = trans
	p.transMu.Unlock()
	go func() {
		defer close(p.trStart)
		p.consumeTranscripts()
	}()
}

// consumeTranscripts turns STT transcripts into C1 prompt frames: a final
// transcript is last:true (starts an employee turn), a partial is last:false
// (preview only), and empty-text silence results emit nothing (C5). A
// partial while playback is active trips the barge-in latch (leaf 06, C5
// order): the interrupt frame is emitted BEFORE the partial's prompt frame
// so playback stops before the new turn's say can start; the latch keeps
// it exactly-once per barge-in until a new say restarts playback. No lock
// is held across channel reads or socket writes (mutexio).
func (p *pipeline) consumeTranscripts() {
	for tr := range p.transcripts {
		if tr.Text == "" {
			continue // Deepgram silence results produce no frame
		}
		if !tr.Final {
			// Barge-in first (C5): interrupt stops playback before the
			// partial preview prompt flows. noteBargeIn enforces
			// exactly-once per barge-in and reports false when idle or
			// already latched (leaf 06).
			if p.noteBargeIn() {
				if err := p.child.emitInterrupt(p.ctx, p.ccid); err != nil {
					p.logger.Error("voicechild: emit interrupt", "err", err, "call_id", p.ccid)
				}
			}
		}
		if err := p.child.emitPrompt(p.ctx, p.ccid, tr.Text, tr.Final); err != nil {
			p.logger.Error("voicechild: emit prompt", "err", err, "call_id", p.ccid)
		}
	}
}

// pumpAudioIn reads daemon->child PCM from <ccid>.in and feeds the STT
// stage. Chunk sizes are codec-derived (C2); each Read's bytes forward as
// one chunk (a stream socket has no write boundaries - a coalesced Read
// arrives as one chunk, which is fine for a 20 ms-unit producer). Chunks
// forward into pcmCh for the Stream session (installed by startTranscripts);
// before it binds, the bounded channel absorbs a few chunks and then drops
// the oldest - the drain never blocks the socket read loop, so the daemon
// never blocks writing. On overflow the OLDEST chunk is dropped: for a
// phone call, audio latency beats completeness.
func (p *pipeline) pumpAudioIn() {
	chunk, err := voiceframe.ChunkBytes(p.codec, p.sampleRt)
	if err != nil {
		p.logger.Error("voicechild: audio pump chunk size", "err", err, "call_id", p.ccid)
		return
	}
	buf := make([]byte, chunk)
	for {
		n, err := p.audioIn.Read(buf)
		if n > 0 {
			// Copy: buf is reused by the next Read.
			data := make([]byte, n)
			copy(data, buf[:n])
			p.forwardPCM(data)
		}
		if err != nil {
			close(p.pcmCh) // socket closed by teardown or daemon: end the Stream feed
			return
		}
		select {
		case <-p.ctx.Done():
			return
		default:
		}
	}
}

// forwardPCM sends one chunk into pcmCh without ever blocking the socket
// drain. Selects first on delivery, then on overflow (drop the OLDEST
// queued chunk to make room), then on teardown (pump exits, chunks left in
// the channel die with it).
func (p *pipeline) forwardPCM(data []byte) {
	select {
	case p.pcmCh <- data:
		return
	default:
	}
	select {
	case p.pcmCh <- data:
		return
	case <-p.ctx.Done():
		return
	default:
	}
	// Full: drop the oldest chunk, then deliver this one.
	select {
	case <-p.pcmCh:
	default:
	}
	select {
	case p.pcmCh <- data:
	case <-p.ctx.Done():
	}
}

// enqueueSay queues speech text for synthesis and playback. Frames carry
// last:true to mark the end of one utterance; the stub TTS starts playing
// immediately (leaf 08 replaces the synthesis itself).
func (p *pipeline) enqueueSay(frame voiceframe.Frame) error {
	p.playMu.Lock()
	// bughunt 2026-11-07 voice F-1: `dropQueued` used to be a once-per-call
	// latch (never reset), so every say after the first clear was silently
	// dropped — the agent was muted for the rest of the call. A NEW say is
	// a new utterance: the drop applies to the INTERRUPTED stream only
	// (the stale playback goroutine aborts on its gen mismatch, see
	// playback()), so clear the gate here.
	p.dropQueued = false
	p.outCh = append(p.outCh, []byte(frame.Text)...) // placeholder queue marker; real audio queued below
	p.playMu.Unlock()

	go p.playback(frame)
	return nil
}

// playback synthesizes text (stub) and writes PCM to the .out socket
// (child->daemon). A clear received mid-playback drops the remaining audio
// and stops writes (T4). Each stream carries the playGen it started under:
// clear() bumps the gen, so the interrupted goroutine aborts even after a
// later say re-opens the dropQueued gate (bughunt 2026-11-07 voice F-1).
func (p *pipeline) playback(frame voiceframe.Frame) {
	pcm, err := p.tts.Speak(p.ctx, frame.Text)
	if err != nil {
		p.logger.Error("voicechild: tts speak", "err", err, "call_id", p.ccid)
		return
	}
	defer func() { _ = pcm.Close() }() //nolint:mutexio // deferred close, no lock held

	p.playMu.Lock()
	gen := p.playGen
	if p.dropQueued && p.playGen == gen {
		// cleared between Speak() and here, before a newer say re-opened
		// the gate: this stream must not start.
		p.playMu.Unlock()
		return
	}
	// Fresh playback restarts the barge-in VAD latch (one interrupt per
	// barge-in, T5) — the path playbackRestarted() documented but never
	// took in production.
	p.dropQueued = false
	p.interrupted = false
	p.speaking = true
	p.playMu.Unlock()
	defer func() {
		p.playMu.Lock()
		// Only the current generation may clear `speaking` (a stale,
		// aborted stream must not mute the barge-in VAD of a newer say).
		if p.playGen == gen {
			p.speaking = false
		}
		p.playMu.Unlock()
	}()

	chunk, err := voiceframe.ChunkBytes(p.codec, p.sampleRt)
	if err != nil {
		p.logger.Error("voicechild: playback chunk size", "err", err, "call_id", p.ccid)
		return
	}
	buf := make([]byte, chunk)
	for {
		p.playMu.Lock()
		dropped := p.dropQueued || p.playGen != gen
		p.playMu.Unlock()
		if dropped {
			return // clear: drop queued audio, stop writes (or gen moved on)
		}
		n, readErr := pcm.Read(buf)
		if n > 0 {
			p.playMu.Lock()
			dropped := p.dropQueued || p.playGen != gen
			p.playMu.Unlock()
			if dropped {
				return // clear arrived while reading: drop the chunk
			}
			if _, werr := p.audioOut.Write(buf[:n]); werr != nil {
				p.logger.Error("voicechild: audio out write", "err", werr, "call_id", p.ccid)
				return
			}
			// Local-leg VAD (leaf 02, CL3): the chunk just written to the
			// .out socket is the caller-heard audio; feed it to the RMS
			// hook. No-op unless local && bargeInRms > 0. Called with no
			// lock held (the hook takes playMu itself).
			p.rmsBargeInCheck(buf[:n])
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				p.logger.Error("voicechild: tts stream read", "err", readErr, "call_id", p.ccid)
			}
			return
		}
		select {
		case <-p.ctx.Done():
			return
		default:
		}
	}
}

// clear drops queued TTS audio for this call and stops pending .out writes
// (C1 Frame{clear}; T4). Idempotent. Bumping playGen (bughunt 2026-11-07
// voice F-1) retires the in-flight playback stream even after a later say
// re-opens the dropQueued gate: the stale goroutine sees its gen mismatch
// and aborts instead of double-writing.
func (p *pipeline) clear() {
	p.playMu.Lock()
	p.dropQueued = true
	p.playGen++
	p.outCh = nil
	// Playback stopped with the clear: drop the stale `speaking` flag.
	// The aborted stream's deferred reset skips itself on the gen
	// mismatch (playback()), so without this the flag would stay true
	// across the interrupt and let noteBargeIn observe a speaking
	// pipeline that no longer exists (also makes a watcher that waits
	// for speaking==true synchronize with the next playback's latch
	// re-arm, which happens in the same critical section).
	p.speaking = false
	p.playMu.Unlock()
}

// noteBargeIn records caller speech during playback and reports whether an
// interrupt frame must be emitted: exactly once per barge-in, until the
// next say restarts playback (T5).
func (p *pipeline) noteBargeIn() bool {
	p.playMu.Lock()
	defer p.playMu.Unlock()
	if !p.speaking || p.interrupted || p.dropQueued {
		return false
	}
	p.interrupted = true
	// The trip STOPS the playback (R3: caller speech stops playout): the
	// interrupted stream aborts on clear/gen bump, but until that frame
	// lands the pipeline must not present itself as speaking — otherwise
	// a watcher or a later say cannot tell the aborted stream from a
	// live one and the latch re-arm becomes unobservable.
	p.speaking = false
	return true
}

// rmsWindowChunks is the sustained-loudness window (CL3): 10 consecutive
// over-threshold chunks = 200 ms at the L16/16k 20 ms chunk size.
const rmsWindowChunks = 10

// rmsChunkOver reports whether an s16le mono PCM chunk's loudness exceeds
// threshold, comparing the integer sum of squares against threshold^2 *
// sampleCount (integer-sqrt-free: sum >= thr*thr*n <=> rms >= thr; at
// least one full sample required - a trailing odd byte is ignored).
// Threshold <= 0 means the VAD is disabled (named no-op: never loud).
func rmsChunkOver(b []byte, threshold int) bool {
	if threshold <= 0 {
		return false
	}
	n := len(b) / 2 // whole int16 samples; trailing odd byte ignored
	if n <= 0 {
		return false
	}
	var sum uint64
	for i := 0; i+1 < len(b); i += 2 {
		v := int16(binary.LittleEndian.Uint16(b[i:]))
		u := uint64(uint16(v)) // |v| without float/negation games
		if v < 0 {
			u = uint64(-u) // two's complement wrap: -32768 -> 32768
		}
		sum += u * u
	}
	thr := uint64(threshold)
	return sum >= thr*thr*uint64(n)
}

// rmsBargeInCheck is the playback-side VAD hook (local leg only, CL3
// matrix): called with each outgoing chunk's bytes. On a non-local leg it
// returns immediately without touching the audio data (RMS unreachable on
// phone legs); with bargeInRms <= 0 it is likewise a named no-op.
// Sustained loudness - rmsWindowChunks consecutive over-threshold chunks,
// the counter reset by any under-threshold chunk and by latch state - trips
// the noteBargeIn latch: the SAME p.interrupted latch field noteBargeIn
// guards (inlined here because noteBargeIn itself takes playMu; no second
// dedup mechanism), and the trip fires emitInterrupt off-lock.
func (p *pipeline) rmsBargeInCheck(chunk []byte) {
	if !p.local || p.bargeInRms <= 0 {
		return // phone legs keep the STT-partial trigger; VAD never runs
	}
	p.playMu.Lock()
	defer p.playMu.Unlock()
	if !p.speaking || p.interrupted || p.dropQueued {
		p.rmsRun = 0
		return
	}
	if rmsChunkOver(chunk, p.bargeInRms) {
		p.rmsRun++
		if p.rmsRun >= rmsWindowChunks {
			p.rmsRun = 0
			// Latch trip happens under playMu: noteBargeIn cannot be
			// called re-entrantly (it would deadlock on playMu).
			p.interrupted = true
			go func() {
				if err := p.child.emitInterrupt(p.ctx, p.ccid); err != nil {
					p.logger.Error("voicechild: emit interrupt", "err", err, "call_id", p.ccid)
				}
			}()
		}
		return
	}
	p.rmsRun = 0
}

// playbackRestarted resets the one-interrupt-per-barge-in latch when a new
// say begins playback.
func (p *pipeline) playbackRestarted() {
	p.playMu.Lock()
	p.interrupted = false
	p.speaking = true
	p.playMu.Unlock()
}

// close tears the pipeline down: cancels its context, closes both audio
// sockets, and waits (bounded) for the transcript consumer to exit - the
// pump dies on the socket close/ctx cancel and closes pcmCh, Stream's
// session ends, and the consumer drains out (T4: no goroutine leak).
// Idempotent.
func (p *pipeline) close(ctx context.Context) {
	p.cancel()
	_ = ctx // kept for future graceful-drain variants
	_ = p.audioIn.Close()
	_ = p.audioOut.Close()
	p.transMu.Lock()
	started := p.transStarted
	p.transMu.Unlock()
	if !started {
		return // no consumer ever ran; nothing to wait for
	}
	select {
	case <-p.trStart:
	case <-time.After(5 * time.Second):
		p.logger.Warn("voicechild: transcript consumer did not exit", "call_id", p.ccid)
	}
}

// stubSTT is the leaf-07 stand-in for the leaf-08 STT adapter: it emits
// nothing, so no prompt frames originate from it in this leaf. Tests inject
// transcripts directly through emitPrompt.
type stubSTT struct{}

func (stubSTT) Stream(ctx context.Context, pcm <-chan []byte) (<-chan Transcript, error) {
	out := make(chan Transcript)
	go func() {
		defer close(out)
		for range pcm {
			// stub: consume and drop; leaf 08 forwards to a real provider
		}
		<-ctx.Done()
	}()
	return out, nil
}

// stubTTS is the leaf-07 stand-in for the leaf-08 TTS adapter: it returns
// deterministic PCM (one 640-byte L16 chunk per say) so tests can observe
// exact writes on the .out socket.
type stubTTS struct{}

func (stubTTS) Speak(ctx context.Context, text string) (io.ReadCloser, error) {
	// Deterministic placeholder audio: two chunks of silence sized for the
	// negotiated codec is not required here; the tests assert on the first
	// chunk arriving before a clear drops the rest.
	pcm := make([]byte, 1280)
	for i := range pcm {
		pcm[i] = byte(i % 251) // deterministic, non-zero pattern
	}
	return io.NopCloser(newSliceReader(pcm)), nil
}

// newSliceReader returns a reader over b.
func newSliceReader(b []byte) *sliceReader { return &sliceReader{b: b} }

type sliceReader struct {
	b []byte
	i int
}

func (r *sliceReader) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}
