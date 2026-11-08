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

// STT Stream-open retry policy (audit fix 5): the daemon sends the keys
// frame exactly once per call, so a failed session open has no external
// hook to lean on — the pipeline retries on its own, bounded, with a short
// capped delay, honoring the pipeline ctx. Package vars so tests can tune
// the delay; production defaults are the constants.
var (
	sttOpenAttempts = 3
	trRetryDelay    = 500 * time.Millisecond
)

// sttProvider returns the pipeline's STT adapter under the playMu regime
// (audit fix 2: ONE lock for provider fields), reporting nil for the stub
// or a nil provider — callers treat "no real STT yet" identically.
func (p *pipeline) sttProvider() STTProvider {
	p.playMu.Lock()
	defer p.playMu.Unlock()
	if p.stt == nil {
		return nil
	}
	if _, isStub := p.stt.(stubSTT); isStub {
		return nil // keys not yet arrived: no real provider installed
	}
	return p.stt
}

// ttsProvider returns the pipeline's TTS adapter under the playMu regime
// (audit fix 2). The stub IS a valid provider for playback (deterministic
// audio in tests), so no stub filter here.
func (p *pipeline) ttsProvider() TTSProvider {
	p.playMu.Lock()
	defer p.playMu.Unlock()
	return p.tts
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
	// PCM from pumpAudioIn into the STT Stream session; the session's
	// output channel is consumed directly by the retry-chain goroutine
	// (consumeTranscripts takes it as a parameter). transMu guards the
	// exactly-once transcript startup (Stream + consumer exactly once per
	// pipeline, whichever of start/handleKeys runs first).
	pcmCh        chan []byte   // fed by pumpAudioIn, consumed by Stream (bounded, oldest-chunk drop)
	transMu      sync.Mutex    // guards transStarted
	transStarted bool          // Stream session claim made (retry chain owns it)
	trStart      chan struct{} // closed when the transcript consumer exits (teardown wait)
	trStartOnce  sync.Once     // exactly-once close of trStart (consumer exit OR permanent open failure)
	pumpOnce     sync.Once     // start() is idempotent: exactly one pump goroutine
	audioOut     net.Conn      // child->daemon PCM (C2 <ccid>.out)
	audioIn      net.Conn      // daemon->child PCM (C2 <ccid>.in)
	audioDir     string

	// open-frame profile (local-leg plan CL1/CL3), set once by handleOpen
	// before the pipeline starts; read-only afterwards (no lock needed).
	// Providers themselves keep arriving over the keys frame (Tier A owns
	// that wire), so this carries ONLY what the local leg needs early:
	// the leg marker (RMS barge-in runs when local) and its threshold.
	local      bool
	bargeInRms int

	// Playback state, guarded by playMu.
	//
	// playMu is ALSO the single lock regime for p.stt/p.tts: constructProviders
	// writes them, the playback worker and startTranscripts read them — all
	// through playMu (via sttProvider/ttsProvider), never bare. (Audit fix 2:
	// the three old regimes — transMu read in startTranscripts, playMu write
	// in constructProviders, lock-free read in playback — were inconsistent.)
	//
	// FIFO playback (audit fix 1): one worker goroutine per pipeline drains
	// sayQueue; each say frame is ENQUEUED, never spawned concurrently, so
	// two utterances can never interleave PCM writes on the same audioOut
	// socket. Concurrent says are the NORMAL path (supervisor checks
	// sentence N+1 while N plays), so this serialization is load-bearing.
	playMu       sync.Mutex
	dropQueued   bool        // clear received: drop queued audio, stop writes
	interrupted  bool        // one interrupt per barge-in until playback restarts
	speaking     bool        // playback active (say in flight)
	sayQueue     []queuedSay // pending say frames (gen-stamped), drained FIFO by the worker (audit fix 8: replaces the never-consumed outCh placeholder)
	playWorkerUp bool        // a playback worker goroutine is draining sayQueue
	// playGen (bughunt 2026-11-07 voice F-1): bumped by clear(); each
	// stream carries the gen it started under and aborts when
	// the gen moves. dropQueued becomes per-barge-in (reset by the next
	// say), so the stale stream stays dead via gen mismatch while a new
	// utterance plays again — without this reset the agent was muted for
	// the whole call after its first interrupt.
	playGen uint64
	// rmsRun (local-leg leaf 02): sustained loud-SAMPLE counter of the
	// INCOMING-audio RMS VAD (caller mic); rmsEnergyRun accumulates the
	// energy of the same counted samples. Both guarded by playMu, reset
	// by latch state changes and by under-threshold chunks (see
	// rmsBargeInCheck).
	rmsRun       uint64
	rmsEnergyRun uint64
	// outEnergySum/outEnergySample (playMu): the energy and sample count
	// of the most recent chunk WRITTEN to the .out socket — the echo
	// reference the incoming VAD compares against (see rmsBargeInCheck).
	outEnergySum    uint64
	outEnergySample uint64 // samples of the most recent outbound chunk
	// pendingInterrupt (playMu): non-nil while an RMS-trip's interrupt
	// frame is in flight to the text socket. The transcript consumer
	// waits on it (closed when the write completes) before emitting a
	// prompt, preserving C5 (interrupt strictly before the partial's
	// prompt frame) even when the VAD trips on the audio-pump goroutine
	// a moment before the STT partial lands.
	pendingInterrupt chan struct{}

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
//
// Bounded retry (audit fix 5): an STT session-open FAILURE used to reset
// the once-guard expecting a "retry on the next hook", but the daemon
// sends the keys frame exactly once per call (internal/daemon/voice_keys.go
// — "Called once per call"), so no further hook ever fired and the call
// stayed permanently deaf. Now a failed open retries up to sttOpenAttempts
// times with capped backoff (trRetryDelay between attempts, ctx-honoring)
// while the pipeline is live; transcripts never start without a REAL STT
// provider (the stub guard still short-circuits before any Stream call),
// so retrying without a provider present stays impossible. The daemon's
// keys protocol is unchanged.
func (p *pipeline) startTranscripts() {
	stt := p.sttProvider()
	if stt == nil {
		// No real provider yet: the keys-arrives hook will call again.
		return
	}
	// The guard check and the claim are ONE critical section: two
	// concurrent callers (start + a retry chain) cannot both pass it.
	p.transMu.Lock()
	if p.transStarted {
		p.transMu.Unlock()
		return
	}
	p.transStarted = true // claimed; the retry chain owns the slot
	p.transMu.Unlock()

	go p.openTranscriptSession(stt, sttOpenAttempts)
}

// openTranscriptSession runs the bounded Stream-open retry chain claimed
// by startTranscripts. Each attempt is an atomic read of the CURRENT
// provider (a mid-call provider swap via constructProviders is honoured,
// never captured stale). On permanent failure it closes trStart via the
// once so teardown never waits on a consumer that never ran.
func (p *pipeline) openTranscriptSession(stt STTProvider, attempts int) {
	for attempt := 1; attempt <= attempts; attempt++ {
		// Re-read the provider each attempt (playMu regime) so a newer
		// constructed adapter wins over a stale captured one.
		if live := p.sttProvider(); live != nil {
			stt = live
		}
		trans, err := stt.Stream(p.ctx, p.pcmCh)
		if err == nil {
			// The session channel is consumed directly here — no field,
			// no second synchronization regime for it.
			p.consumeTranscripts(trans)
			p.trStartOnce.Do(func() { close(p.trStart) })
			return
		}
		if p.ctx.Err() != nil {
			// Pipeline torn down: stop trying, unblock teardown.
			p.trStartOnce.Do(func() { close(p.trStart) })
			return
		}
		if attempt == attempts {
			p.logger.Error("voicechild: stt stream open permanently failed; call is deaf (no transcript retry hooks remain)",
				"err", err, "call_id", p.ccid, "attempts", attempts)
			p.trStartOnce.Do(func() { close(p.trStart) })
			return
		}
		p.logger.Warn("voicechild: stt stream open failed; retrying",
			"err", err, "call_id", p.ccid, "attempt", attempt, "max_attempts", attempts)
		slept := false
		timer := time.NewTimer(trRetryDelay)
		select {
		case <-p.ctx.Done():
		case <-timer.C:
			slept = true
		}
		timer.Stop()
		if !slept {
			p.trStartOnce.Do(func() { close(p.trStart) })
			return
		}
	}
}

// consumeTranscripts turns STT transcripts into C1 prompt frames: a final
// transcript is last:true (starts an employee turn), a partial is last:false
// (preview only), and empty-text silence results emit nothing (C5). A
// partial while playback is active trips the barge-in latch (leaf 06, C5
// order): the interrupt frame is emitted BEFORE the partial's prompt frame
// so playback stops before the new turn's say can start; the latch keeps
// it exactly-once per barge-in until a new say restarts playback. No lock
// is held across channel reads or socket writes (mutexio).
func (p *pipeline) consumeTranscripts(trans <-chan Transcript) {
	for tr := range trans {
		if tr.Text == "" {
			continue // Deepgram silence results produce no frame
		}
		if !tr.Final {
			// Barge-in first (C5): interrupt stops playback before the
			// partial preview prompt flows. noteBargeIn enforces
			// exactly-once per barge-in and reports false when idle or
			// already latched (leaf 06). On the local leg the RMS VAD
			// usually latched first (it sees raw audio before STT has a
			// partial), so noteBargeIn returns false here and the
			// pending-interrupt write below is what C5 must wait on.
			if p.noteBargeIn() {
				if err := p.child.emitInterrupt(p.ctx, p.ccid); err != nil {
					p.logger.Error("voicechild: emit interrupt", "err", err, "call_id", p.ccid)
				}
			}
		}
		// C5: never let a prompt overtake an in-flight interrupt frame.
		p.awaitPendingInterrupt()
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
			// Local-leg VAD (CL3): the caller's microphone PCM arriving
			// on .in is the barge-in signal — tap it BEFORE forwarding
			// to STT. No-op unless local && bargeInRms > 0; takes playMu
			// briefly, never blocks the socket drain beyond that.
			p.rmsBargeInCheck(data)
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

// enqueueSay queues speech text for synthesis and playback (audit fix 1:
// FIFO, one worker). The frame is appended to sayQueue under playMu and ONE
// playback worker goroutine per pipeline drains the queue serially — no
// per-say goroutine any more, so two concurrent utterances can NEVER
// interleave PCM writes on the same audioOut socket. Concurrent says are
// the normal path (the supervisor checks sentence N+1 while N plays), so
// serialization here is load-bearing, not defensive.
//
// The F-1 re-arm semantics keep working: a fresh say clears dropQueued
// (the drop applies to the INTERRUPTED generation only — the stale stream
// aborts on its gen mismatch, see playbackStream), and the worker re-arms
// the barge-in latch when it starts streaming a frame whose gen is current.
//
// Audit fix 8: the old outCh placeholder field is gone; sayQueue IS the
// queue.
func (p *pipeline) enqueueSay(frame voiceframe.Frame) error {
	p.playMu.Lock()
	// bughunt 2026-11-07 voice F-1: `dropQueued` used to be a once-per-call
	// latch (never reset), so every say after the first clear was silently
	// dropped — the agent was muted for the rest of the call. A NEW say is
	// a new utterance: the drop applies to the INTERRUPTED generation only,
	// so clear the gate here. The frame carries the CURRENT playGen as its
	// retirement stamp: a clear after this enqueue bumps playGen and the
	// draft is dropped at dequeue time ("clear drops queued drafts").
	p.dropQueued = false
	p.sayQueue = append(p.sayQueue, queuedSay{frame: frame, gen: p.playGen})
	workerUp := p.playWorkerUp
	if !workerUp {
		p.playWorkerUp = true
	}
	p.playMu.Unlock()

	if !workerUp {
		go p.playbackWorker()
	}
	return nil
}

// playbackWorker is the single drain loop for sayQueue (audit fix 1). It
// processes frames strictly one at a time, so audioOut.Write is only ever
// reached from one goroutine at a time per pipeline. The worker exits when
// the queue empties or the pipeline ctx dies; a concurrent enqueueSay
// re-launches it (playWorkerUp is released under playMu in the same
// critical section that observes an empty queue, so no frame is ever
// stranded).
func (p *pipeline) playbackWorker() {
	for {
		p.playMu.Lock()
		if len(p.sayQueue) == 0 || p.ctx.Err() != nil {
			// Release the slot atomically with the emptiness check:
			// an enqueueSay after this point spawns a fresh worker.
			p.playWorkerUp = false
			p.playMu.Unlock()
			return
		}
		item := p.sayQueue[0]
		p.sayQueue = p.sayQueue[1:]
		// A clear received between enqueue and dequeue bumped playGen:
		// this queued draft is dropped (C1 barge-in contract).
		retired := item.gen != p.playGen
		p.playMu.Unlock()
		if retired {
			continue
		}
		p.playbackStream(item.frame, item.gen)
	}
}

// queuedSay is one say frame waiting in the pipeline FIFO, stamped with
// the playGen at enqueue time. clear() bumps playGen: a draft whose stamp
// no longer matches was queued BEFORE the interrupt, so the worker drops
// it at dequeue time ("clear drops queued drafts for THAT call only"),
// even if a later say already re-opened the drop gate (F-1 re-arm) — the
// re-arm belongs to the newer utterance, not to a retired draft. Drafts
// stamped with the CURRENT gen always play: the re-arm's "a new utterance
// plays again" is exactly their stamp surviving.
type queuedSay struct {
	frame voiceframe.Frame
	gen   uint64
}

// playbackStream synthesizes one frame's text and writes its PCM to the
// .out socket (child->daemon). Called ONLY from playbackWorker — never
// concurrently for the same pipeline, so audioOut writes are serialized by
// construction (audit fix 1). A clear received mid-stream drops the
// remaining audio and stops writes (T4): the stream carries the playGen it
// started under, and clear() bumps the gen, so an interrupted stream aborts
// even after a later say re-opens the dropQueued gate (bughunt 2026-11-07
// voice F-1).
func (p *pipeline) playbackStream(frame voiceframe.Frame, gen uint64) {
	tts := p.ttsProvider()
	if tts == nil {
		p.logger.Error("voicechild: tts provider not installed; dropping say", "call_id", p.ccid)
		return
	}
	pcm, err := tts.Speak(p.ctx, frame.Text)
	if err != nil {
		p.logger.Error("voicechild: tts speak", "err", err, "call_id", p.ccid)
		return
	}
	defer func() { _ = pcm.Close() }() //nolint:mutexio // deferred close, no lock held

	p.playMu.Lock()
	if p.playGen != gen {
		// A clear retired this stream's generation between dequeue and
		// here (or between Speak() and here): it must not start. The
		// stamped-gen check subsumes the old dropQueued gate — a clear
		// always bumps playGen, so dropQueued is never true at a
		// matching gen.
		p.playMu.Unlock()
		return
	}
	// Fresh playback restarts the barge-in VAD latch (one interrupt per
	// barge-in, T5) — the path playbackRestarted() documented but never
	// took in production. The incoming-RMS window accumulators are part
	// of that latch state: a partial run from the retired utterance must
	// not feed into the new one's window.
	p.dropQueued = false
	p.interrupted = false
	p.rmsRun = 0
	p.rmsEnergyRun = 0
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
			// Local-leg echo reference (CL3): record this outbound
			// chunk's energy so the INCOMING-side VAD tap can tell a
			// speaker echo from a caller talking over the agent. This is
			// NOT the barge-in trigger any more — the old bug fed the
			// RMS VAD this TTS audio and the agent interrupted itself.
			// No-op unless local && bargeInRms > 0. Called with no lock
			// held (the hook takes playMu itself).
			p.noteOutgoingChunk(buf[:n])
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
// re-opens the dropQueued gate: the stale stream sees its gen mismatch
// and aborts instead of double-writing. Queued drafts not yet dequeued are
// dropped at dequeue time by the same gate (audit fix 1 keeps the barge-in
// semantics: "clear stops the current stream and drops queued audio").
func (p *pipeline) clear() {
	p.playMu.Lock()
	p.dropQueued = true
	p.playGen++
	// Playback stopped with the clear: drop the stale `speaking` flag.
	// The aborted stream's deferred reset skips itself on the gen
	// mismatch (playbackStream), so without this the flag would stay true
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

// rmsEnergy reports an s16le mono PCM chunk's loudness as (sum of squared
// |sample|, sample count); a trailing odd byte is ignored. This is the
// integer form of rms^2 * n used by the threshold compare in
// rmsChunkOver and the echo compare in rmsBargeInCheck, so neither needs a
// division or a float.
func rmsEnergy(b []byte) (uint64, uint64) {
	var sum uint64
	var n uint64
	for i := 0; i+1 < len(b); i += 2 {
		v := int16(binary.LittleEndian.Uint16(b[i:]))
		u := uint64(uint16(v)) // |v| without float/negation games
		if v < 0 {
			u = uint64(-u) // two's complement wrap: -32768 -> 32768
		}
		sum += u * u
		n++
	}
	return sum, n
}

// rmsChunkOver reports whether an s16le mono PCM chunk's loudness exceeds
// threshold, comparing the integer sum of squares against threshold^2 *
// sampleCount (integer-sqrt-free: sum >= thr*thr*n <=> rms >= thr; at
// least one full sample required - a trailing odd byte is ignored).
// Threshold <= 0 means the VAD is disabled (named no-op: never loud).
func rmsChunkOver(b []byte, threshold int) bool {
	if threshold <= 0 {
		return false
	}
	sum, n := rmsEnergy(b)
	if n <= 0 {
		return false
	}
	thr := uint64(threshold)
	return sum >= thr*thr*n
}

// rmsBargeInCheck is the INCOMING-audio VAD hook (local leg only, CL3
// matrix): called by pumpAudioIn with each caller-microphone chunk read
// from the .in socket, never with outbound TTS audio. Feeding the agent's
// own outgoing PCM (the pre-fix wiring) made loud speech interrupt itself
// and could never hear a caller talking over playback; the tap lives on
// the incoming path now — product parity with the phone leg's STT-partial
// trigger. No-op unless local && bargeInRms > 0.
//
// Trip rule: while playback is live, incoming chunks whose per-chunk RMS
// meets the threshold accumulate for rmsWindowChunks chunk-lengths
// (200 ms at the L16/16k 20 ms chunk size); any under-threshold chunk or
// latch-state change resets the run.
//
// Echo guard (energy compare, no clock): the local-leg mic can pick up the
// speakers playing the agent's own audio, so a faithful echo must not
// trip. At the window boundary the run's AVERAGE energy is compared
// against the average energy of the most recent .out chunk (recorded by
// noteOutgoingChunk) scaled by rmsEchoMarginPct: the caller must be
// CLEARLY louder than what the speakers are emitting. Chosen over a
// 150-300 ms suppress window after each outbound chunk because the pump
// and the playback worker are separate goroutines and outbound chunks land
// every 20 ms: a time-based ignore window would starve the sustained
// counter (10 consecutive incoming chunks can never all fall outside a
// repeating 150-300 ms suppression) and add cross-goroutine clock races.
// Zero-energy reference (agent silent / no chunk written yet) sets the
// ceiling to 0, so a caller talking over quiet playback trips normally.
//
// The latch trip happens under playMu with the SAME semantics as
// noteBargeIn (one interrupt per barge-in until the next say re-arms it;
// speaking drops with the trip so a watcher sees the interruption). The
// interrupt frame is emitted off-lock, and pendingInterrupt signals the
// transcript consumer to wait for it before any prompt, preserving the C5
// "interrupt strictly before the partial's prompt frame" order even when
// the VAD trip and an STT partial race on different goroutines.
func (p *pipeline) rmsBargeInCheck(chunk []byte) {
	if !p.local || p.bargeInRms <= 0 {
		return // phone legs keep the STT-partial trigger; VAD never runs
	}
	// Per-chunk gate via the named helper (threshold <= 0 already
	// short-circuited above); on a loud chunk accumulate its energy and
	// sample count for the sustained window + echo compare.
	over := rmsChunkOver(chunk, p.bargeInRms)
	var sum, samples uint64
	if over {
		sum, samples = rmsEnergy(chunk)
	}
	p.playMu.Lock()
	if !p.speaking || p.interrupted || p.dropQueued || !over {
		p.rmsRun = 0
		p.rmsEnergyRun = 0
		p.playMu.Unlock()
		return
	}
	p.rmsRun += samples
	p.rmsEnergyRun += sum
	if p.rmsRun < rmsWindowChunks*samples {
		p.playMu.Unlock()
		return
	}
	// Sustained window reached: echo guard. Compare window average energy
	// against the outbound reference average scaled by the echo margin,
	// integer cross-multiplied (no division, no float):
	//   windowSum/windowSamples > outSum*outSamples * (margin/100)
	// bounded so overflow cannot occur at s16 chunk sizes (~1e17 < 1.8e19).
	windowTotal := p.rmsEnergyRun
	windowSamples := p.rmsRun
	echoSum := p.outEnergySum
	echoSamples := p.outEnergySample
	p.rmsRun = 0
	p.rmsEnergyRun = 0
	// Zero reference (agent silent: no chunk written yet, or the last
	// chunk was digital silence) means the ceiling is 0 — a loud incoming
	// window trips. Otherwise cross-multiply the average-energy compare
	// (no division, no float): trip only when
	//   windowTotal/windowSamples > echoSum/echoSamples * (margin/100).
	// Bounds: at 20 ms chunks the products stay under 1.8e19 (s16 energy
	// max 32768^2 * 3200 samples * 130 * 100 ~ 4.4e15), no overflow.
	if echoSum > 0 && windowTotal*echoSamples*100 <= echoSum*windowSamples*rmsEchoMarginPct {
		p.playMu.Unlock()
		return // looks like self-echo: latch stays armed, keep listening
	}
	// Latch trip under playMu (noteBargeIn cannot be called re-entrantly:
	// it would deadlock on playMu). Same field transitions as
	// noteBargeIn: interrupted latches one-interrupt-per-barge-in,
	// speaking drops so playback stops presenting as live (R3).
	p.interrupted = true
	p.speaking = false
	// C5 guard: the write below is in flight; the transcript consumer
	// waits on this channel before emitting any prompt frame.
	done := make(chan struct{})
	p.pendingInterrupt = done
	p.playMu.Unlock()
	// Off-lock so the audio pump never stalls on a text-socket write
	// (writeMu can hold up to its 2 s deadline): the C5 order is kept by
	// the pendingInterrupt handshake — the transcript consumer waits on
	// the same channel before emitting any prompt frame.
	go func() {
		err := p.child.emitInterrupt(p.ctx, p.ccid)
		close(done)
		p.playMu.Lock()
		if p.pendingInterrupt == done {
			p.pendingInterrupt = nil
		}
		p.playMu.Unlock()
		if err != nil {
			p.logger.Error("voicechild: emit interrupt", "err", err, "call_id", p.ccid)
		}
	}()
}

// awaitPendingInterrupt blocks until an in-flight RMS-trip interrupt frame
// has reached the text socket (no-op when none is pending). consumeTranscripts
// calls it before emitting a prompt, so the local leg keeps the C5 order —
// interrupt strictly before the partial's prompt frame — even though the
// VAD trip fires on the audio-pump goroutine, typically a moment before
// the STT partial for the same caller utterance lands.
func (p *pipeline) awaitPendingInterrupt() {
	p.playMu.Lock()
	done := p.pendingInterrupt
	p.playMu.Unlock()
	if done == nil {
		return
	}
	<-done
}

// rmsEchoMarginPct is the echo-guard headroom: the incoming window's
// average energy must exceed the last outbound chunk's average energy by
// this percentage (i.e. be >130% of the speaker reference) before the
// local leg's RMS VAD trips. The mic/speaker loop is passive and
// attenuated, so a faithful echo lands well under the reference; the 30%
// headroom absorbs room gain variation while a caller talking over the
// agent's own voice clears it.
const rmsEchoMarginPct = 130

// noteOutgoingChunk records the energy of one chunk written to the .out
// socket as the echo reference for rmsBargeInCheck (local leg only — a
// no-op elsewhere; phone legs never run the RMS VAD). Called with no lock
// held by playbackStream after each write. The RMS tap moved OFF this
// path (the old bug: outgoing TTS was fed to the VAD, so the agent
// interrupted itself); this only maintains the echo reference now.
func (p *pipeline) noteOutgoingChunk(chunk []byte) {
	if !p.local || p.bargeInRms <= 0 {
		return
	}
	sum, samples := rmsEnergy(chunk)
	p.playMu.Lock()
	p.outEnergySum = sum
	p.outEnergySample = samples
	p.playMu.Unlock()
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
// session ends, and the consumer drains out (T4: no goroutine leak). A
// retry chain that never opened a session exits on the ctx cancel and
// closes trStart through the same once, so teardown never waits on a
// consumer that never ran. Idempotent.
func (p *pipeline) close(ctx context.Context) {
	p.cancel()
	_ = ctx // kept for future graceful-drain variants
	_ = p.audioIn.Close()
	_ = p.audioOut.Close()
	p.transMu.Lock()
	started := p.transStarted
	p.transMu.Unlock()
	if !started {
		return // no session claim ever ran; nothing to wait for
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
