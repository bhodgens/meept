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
	"regexp"
	"strings"
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
	//
	// Synthesis is SPLIT from pacing (TTS pipeline serialization fix): the
	// worker starts frame N+1's Speak() on a dedicated synth goroutine the
	// moment frame N's audio enters the write loop, so the provider's
	// full-generation latency (the qwen3 server does not stream despite
	// stream=true; Speak blocks 0.7-4.8 s) overlaps the daemon pacer's
	// real-time drain of N. Without it, synthesis of N+1 only began after
	// N had FULLY PLAYED and every sentence break was a dead 1-4 s gap at
	// the speakers (daemon log: "localsplice pacer stall gap_ms=4620").
	// Prefetch depth is bounded to ONE frame: PCM still leaves audioOut
	// strictly in dequeue order (one writer), at most one extra utterance
	// is ever synthesizing/open, and a clear retires the lookahead with
	// the same playGen gate (its reader is cancelled AND closed, so no
	// stale bytes reach the socket after the barge-in).
	playMu      sync.Mutex
	dropQueued  bool // clear received: drop queued audio, stop writes
	interrupted bool // one interrupt per barge-in until playback restarts
	speaking    bool // playback active (say in flight)
	// writingNow (playMu) is true while the worker is INSIDE a frame's
	// chunk write loop (set with speaking==true in playbackStream,
	// cleared on its exit). It gates the enqueueSay prefetch hook: a say
	// that arrives while sentence N's audio is actively pacing may start
	// N+1's synthesis immediately; one that arrives while the worker is
	// between frames (synthesizing N, or parked) stays queued and gets
	// its prefetch from the worker's own write-start hook instead —
	// keeping synth depth at ONE frame past the current write.
	writingNow   bool
	sayQueue     []queuedSay // pending say frames (gen-stamped), drained FIFO by the worker (audit fix 8: replaces the never-consumed outCh placeholder)
	playWorkerUp bool        // a playback worker goroutine is draining sayQueue
	// synthJob (playMu) is the ONE in-flight or prefetched synthesis the
	// worker owns: nil while no Speak is outstanding. clear() cancels it
	// (its Speak ctx) so an interrupted turn stops burning provider
	// latency; awaitSynth/retireSynth nil the slot. See synthJob,
	// registerSynthLocked, tryLaunchPrefetch.
	synthJob *synthJob
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
	// lastFinalAt (playMu) is when the STT session produced its most
	// recent DISPATCHED non-empty FINAL transcript. The local leg's RMS
	// VAD trip is gated on it (see rmsBargeInCheck): loudness alone is
	// echo/noise evidence, not speech evidence, so a trip without a
	// recent final must not cancel an in-flight answer. The trip
	// CONSUMES the evidence (one-shot, defect 2026-11-09): an
	// authorized trip zeroes this field, so a second trip needs a NEW
	// final — a stale stamp must never re-authorize cancelling the
	// very turn the final already dispatched. Non-speech finals (see
	// isRealSpeech) and duplicate finals never stamp it (see
	// lastFinalText).
	lastFinalAt time.Time
	// lastFinalText (playMu) is the trimmed text of the most recent
	// DISPATCHED final transcript. whisper repeats the same final when
	// the caller re-voices an utterance's tail; an exact duplicate
	// inside the evidence window is that SAME utterance, not new
	// speech, so consumeTranscripts drops it (defect 2026-11-09: the
	// repeat re-stamped lastFinalAt and re-authorized stale barge-in
	// trips).
	lastFinalText string

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

// nonSpeechTranscript reports whether a FINAL transcript is one of
// whisper's no-speech markers rather than caller words (defect
// 2026-11-09, call local-2d84c384fd93f447: three "[BLANK_AUDIO]" finals
// and a "(sighs)" were dispatched as user prompts and the employee
// answered silence). The match is on the TRIMMED text, case-insensitive,
// against a fixed marker set; whitespace-only text counts as non-speech.
// consumeTranscripts drops these finals entirely (no prompt frame, no
// RMS barge-in evidence stamp). Partials never route through this —
// they stay preview-only prompts and the daemon never starts a turn on
// one.
func nonSpeechTranscript(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" {
		return true
	}
	switch t {
	case "[blank_audio]", "<blank_audio>", "[blank audio]", "<blank audio>",
		"(sighs)", "(sigh)",
		"(laughing)", "(laughs)",
		"(no speech)", "[no speech]",
		"(background noise)", "(background noises)", "[background noise]", "[background noises]":
		return true
	}
	return false
}

// bracketedTokenRe matches one whisper paralinguistic bracket token: an
// opening ( [ followed by 1-60 non-']' / non-newline characters and a
// closing ) ]. The 60-char cap keeps a bracket token bracket-sized — a long
// parenthetical containing real words is caller speech, not an artifact.
// (?i) is spec fidelity: the class is shape-based, so case never binds.
var bracketedTokenRe = regexp.MustCompile(`(?i)[\[(][^\]\n]{1,60}[\])]`)

// punctuationOnlyRe matches residue that carries no word characters at all:
// only Unicode punctuation, symbols, and whitespace.
var punctuationOnlyRe = regexp.MustCompile(`^[\p{P}\p{S}\s]*$`)

// isRealSpeech reports whether a FINAL transcript carries real words
// OUTSIDE whisper's bracket artifacts (defect 2026-11-10, call
// local-376ee373022f7663: the exact-marker set of nonSpeechTranscript only
// covers a fixed list, whisper emits dozens of paralinguistic tokens —
// "(farting)", "(mouse clicking)", "(squeaking)", "[clears throat]",
// "(inhales deeply)" — and the companion answered each as a user prompt).
// The guard classifies by SHAPE, wrapping the exact set:
//
//  1. exact no-speech marker set (fast path — blank/sigh variants land
//     here already, and whitespace-only text counts as non-speech);
//  2. strip every maximal bracketed-token run (regex above). Paren
//     characters live INSIDE the body class, so nesting collapses into
//     one run ending at the FIRST closer: "[music (fade)]" and the
//     unbalanced "[music (fade)" both strip whole; an opener with NO
//     closer anywhere ("hello [music (fade") matches nothing and stays
//     as residue — leftover words outside any run always keep the
//     transcript real, so the rule errs toward hearing the caller;
//  3. punctuation/symbol/whitespace-only residue is not speech.
//
// A single-bracket word ("yeah", "mmm") stays REAL: only bracketed and
// blank forms drop, so filler words the caller genuinely voiced still
// dispatch a turn.
func isRealSpeech(text string) bool {
	// Fast path: the exact set from leaf 02 (3a755a84) stays first — its
	// semantics and TestNonSpeechTranscriptTable are unchanged.
	if nonSpeechTranscript(text) {
		return false
	}
	residue := strings.TrimSpace(bracketedTokenRe.ReplaceAllString(text, " "))
	return !punctuationOnlyRe.MatchString(residue)
}

// transcriptPreview bounds a dropped transcript to <=40 runes for the
// drop log. Transcript text carries nothing sensitive by contract, but the
// log line stays bounded regardless (telemetry leaf 02 counts these drops
// from the same line).
func transcriptPreview(text string) string {
	r := []rune(text)
	if len(r) > 40 {
		return string(r[:40])
	}
	return text
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
		if tr.Final {
			trimmed := strings.TrimSpace(tr.Text)
			// Non-speech filter (defect 2026-11-09 exact set, extended to
			// the SHAPE class 2026-11-10): whisper emits no-speech markers
			// and paralinguistic bracket tokens — "[BLANK_AUDIO]",
			// "(sighs)", "(farting)", "[clears throat]" — as FINAL
			// transcripts. Those are not user prompts; dispatching them
			// made the employee answer silence and mouse clicks. Drop them
			// entirely: no prompt frame, no speech-evidence stamp, no
			// dedupe state change. Partials keep passing through as
			// previews (the daemon never starts a turn on one, and the
			// partial branch below never touches lastFinalAt — evidence
			// stamping is finals-only by contract).
			if !isRealSpeech(trimmed) {
				p.logger.Info("voicechild: non-speech final dropped",
					"call_id", p.ccid, "text", transcriptPreview(trimmed))
				continue
			}
			// Speech evidence for the local leg's RMS barge-in gate
			// (see rmsBargeInCheck): whisper heard actual words.
			// Exact-duplicate dedupe (defect 2026-11-09): whisper
			// repeats the same final when the caller re-voices an
			// utterance's tail. That repeat is the SAME utterance, not
			// new speech: dropping it here is the single choke point —
			// the daemon talk loop never sees a duplicate final prompt,
			// so no daemon-side guard is needed (the repeat cannot
			// dispatch a second turn or re-authorize a stale RMS trip).
			p.playMu.Lock()
			if trimmed == p.lastFinalText && !p.lastFinalAt.IsZero() &&
				time.Since(p.lastFinalAt) <= rmsSttEvidenceWindow {
				p.playMu.Unlock()
				continue
			}
			p.lastFinalAt = time.Now()
			p.lastFinalText = trimmed
			speaking := p.speaking
			p.playMu.Unlock()
			// C5 for the local leg's whisper adapter: whisper emits
			// FINALS only (no partials), and the RMS gate now suppresses
			// loudness-only trips. A final arriving while playback is
			// live must therefore carry the interrupt itself - stop the
			// old playback BEFORE the new prompt's reply can start, or
			// the old say plays over the new turn's answer. Same
			// exactly-once latch as the partial path.
			if speaking && p.noteBargeIn() {
				if err := p.child.emitInterrupt(p.ctx, p.ccid); err != nil {
					p.logger.Error("voicechild: emit interrupt", "err", err, "call_id", p.ccid)
				}
			}
		} else {
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
	} else {
		// OVERLAP POINT 2: this draft arrived while the worker is
		// actively pacing an earlier frame's audio (writingNow) — the
		// NORMAL supervisor path (sentence N+1 is checked while N
		// plays). Start its synthesis now instead of waiting for N to
		// fully drain; the single synth slot keeps depth at one. The
		// hook is playMu-guarded and lock-free inside Speak; safe to
		// call from the frame handler. No-op (draft stays queued) when
		// the worker is between frames — its write-start hook (OVERLAP
		// POINT 1) will pick it up.
		p.tryLaunchPrefetch(true)
	}
	return nil
}

// synthResult is what a synthJob delivers when the provider's Speak call
// returns: the per-frame PCM reader (nil on failure), the error, and how
// long synthesis itself took (telemetry: synth_ms vs write_ms per stream
// proves the prefetch overlap in the cadence log).
type synthResult struct {
	pcm      io.ReadCloser
	err      error
	synthDur time.Duration
}

// synthJob is one in-flight synthesis: Speak runs on a dedicated
// goroutine (never on the worker, never across playMu), the worker owns
// the result via done, and cancel retires the provider request (a clear
// during synthesis aborts Speak at the transport: the providers all build
// requests on the passed ctx — http.NewRequestWithContext / killed
// subprocesses). done is buffered 1 so the synth goroutine never blocks:
// the job's OWNER — awaitSynth (worker) or retireSynth (background
// drain) — receives it exactly once per job.
type synthJob struct {
	frame  voiceframe.Frame
	gen    uint64
	done   chan synthResult
	ctx    context.Context
	cancel context.CancelFunc
}

// registerSynthLocked claims the playMu synth slot for one dequeued
// frame and returns the job (unstarted). Caller holds playMu. ONE slot
// at a time per pipeline is the prefetch bound: while a job is
// registered, no second synthesis can start, and the worker only
// registers a prefetch for the frame whose PREDECESSOR has entered the
// write loop. p.ctx never changes after construction, so deriving the
// job's cancellable ctx under the lock is safe and keeps clear()
// (which reads p.synthJob under the same lock) able to cancel a Speak
// that has launched by the time it fires.
func (p *pipeline) registerSynthLocked(item queuedSay) *synthJob {
	jobCtx, cancel := context.WithCancel(p.ctx)
	job := &synthJob{
		frame:  item.frame,
		gen:    item.gen,
		done:   make(chan synthResult, 1),
		ctx:    jobCtx,
		cancel: cancel,
	}
	p.synthJob = job
	return job
}

// runSynth starts the registered job's Speak on a dedicated goroutine
// (or resolves it synchronously when no provider is installed). Captures
// the provider the caller read under playMu — the same regime as every
// other p.tts read (audit fix 2). No lock is held across Speak (mutexio).
func (p *pipeline) runSynth(tts TTSProvider, job *synthJob) {
	if tts == nil {
		job.done <- synthResult{err: errNoTTSProvider}
		return
	}
	go func() {
		start := time.Now()
		r, err := tts.Speak(WithTTSContext(job.ctx, p.ccid), job.frame.Text)
		job.done <- synthResult{pcm: r, err: err, synthDur: time.Since(start)}
	}()
}

// awaitSynth blocks for the job's Speak result and releases the playMu
// slot (the worker owns the reader from here). ok=false means the
// pipeline died while Speak was running: a background drain still
// receives the result (exactly one receiver per job, always) and closes
// any late reader, so nothing leaks and nothing hangs.
func (p *pipeline) awaitSynth(job *synthJob) (synthResult, bool) {
	select {
	case res := <-job.done:
		p.releaseSynthSlot(job)
		return res, true
	case <-p.ctx.Done():
		p.releaseSynthSlot(job)
		go func() {
			res := <-job.done
			if res.pcm != nil {
				_ = res.pcm.Close() //nolint:mutexio // teardown drain, no lock held
			}
		}()
		return synthResult{err: p.ctx.Err()}, false
	}
}

// releaseSynthSlot clears the playMu slot if it still points at this job
// (clear() never nils it; retireSynth's drain may have done it first).
func (p *pipeline) releaseSynthSlot(job *synthJob) {
	p.playMu.Lock()
	if p.synthJob == job {
		p.synthJob = nil
	}
	p.playMu.Unlock()
}

// retireSynth drops a job the worker will never play (a prefetch killed
// by a clear or a torn-down ctx): take the slot synchronously — the
// worker is the ONLY caller, and it never retires the same job twice —
// then cancel Speak at the transport and drain the result in the
// background, closing a reader that lands too late to play. The
// prefetched PCM NEVER reaches the socket. Safe on an already-delivered
// job (cancel is idempotent; done is buffered 1, so the drain receive
// never blocks on a Speak that already answered).
func (p *pipeline) retireSynth(job *synthJob) {
	job.cancel()
	p.releaseSynthSlot(job)
	go func() {
		res := <-job.done
		if res.pcm != nil {
			_ = res.pcm.Close() //nolint:mutexio // retire drain, no lock held
		}
	}()
}

// errNoTTSProvider surfaces the "provider not installed" drop through
// the job result so the worker logs the same error line the old inline
// check did.
var errNoTTSProvider = errors.New("voicechild: tts provider not installed; dropping say")

// tryLaunchPrefetch starts the NEXT queued draft's synthesis while a
// frame's audio is being (or is about to be) paced to the .out socket.
// This is the overlap: sentence N+1's Speak begins during sentence N's
// playback instead of after it has fully drained, so the provider's full-
// generation latency (qwen3 does not stream; 0.7-4.8 s) hides inside the
// daemon pacer's real-time drain of N. The draft STAYS-POP from sayQueue
// but keeps its gen stamp: a clear retires it exactly like an unpopped
// queued draft, and the prefetched reader is cancelled and closed,
// never written.
//
// Depth is ONE frame (the single p.synthJob slot): nothing is
// synthesized while a synthesis is outstanding, so frame N+2 never
// starts before N+1's write phase began — memory stays ~one utterance.
//
// fromEnqueue=true is the enqueueSay path: the new draft may only
// prefetch while the worker is actively writing (writingNow) — the same
// depth contract. fromEnqueue=false is the worker's own hook fired
// right before it enters the write loop for frame N (N+1 may start
// generating even though N's first chunk has not left yet — there is no
// second reader to collide with, and Speak calls that overlap a live
// READER would need a real pacing stall to occur, which is exactly the
// gap this prefetch removes).
func (p *pipeline) tryLaunchPrefetch(fromEnqueue bool) {
	p.playMu.Lock()
	if !p.playWorkerUp || p.synthJob != nil || p.ctx.Err() != nil {
		p.playMu.Unlock()
		return
	}
	if fromEnqueue && !p.writingNow {
		p.playMu.Unlock()
		return
	}
	if len(p.sayQueue) == 0 || p.sayQueue[0].gen != p.playGen {
		p.playMu.Unlock()
		return
	}
	item := p.sayQueue[0]
	p.sayQueue = p.sayQueue[1:]
	tts := p.tts
	job := p.registerSynthLocked(item)
	p.playMu.Unlock()
	p.runSynth(tts, job)
}

// playbackWorker is the single drain loop for sayQueue (audit fix 1). It
// writes frames strictly one at a time, so audioOut.Write is only ever
// reached from one goroutine at a time per pipeline — PCM leaves in
// dequeue order, one writer. Synthesis is split off the write path: a
// frame's Speak ran on a dedicated goroutine while the PREVIOUS frame
// was still pacing (claimed from the synth slot at the top of the
// iteration), so the worker only blocks on generation when there was no
// playback to overlap it with (head of a burst). The worker exits when
// the queue empties or the pipeline ctx dies; a concurrent enqueueSay
// re-launches it (playWorkerUp is released under playMu in the same
// critical section that observes an empty queue and no outstanding
// synth slot, so no frame is ever stranded).
func (p *pipeline) playbackWorker() {
	for {
		var item queuedSay
		var job *synthJob
		var tts TTSProvider
		p.playMu.Lock()
		job = p.synthJob
		if job != nil {
			// A prefetch launched during the previous write — this
			// frame's synthesis is already running. The slot stays
			// registered until awaitSynth completes so a clear racing
			// the remaining Speak latency still cancels it.
			item = queuedSay{frame: job.frame, gen: job.gen}
			retired := job.gen != p.playGen || p.ctx.Err() != nil
			p.playMu.Unlock()
			if retired {
				p.retireSynth(job)
				continue
			}
		} else {
			if len(p.sayQueue) == 0 || p.ctx.Err() != nil {
				// Release the slot atomically with the emptiness check:
				// an enqueueSay after this point spawns a fresh worker.
				p.playWorkerUp = false
				p.playMu.Unlock()
				return
			}
			item = p.sayQueue[0]
			p.sayQueue = p.sayQueue[1:]
			retired := item.gen != p.playGen
			if retired {
				p.playMu.Unlock()
				continue
			}
			// Head of a burst (nothing was playing to overlap against):
			// synthesize now and wait — the old behavior for the first
			// sentence of every turn.
			job = p.registerSynthLocked(item)
			tts = p.tts
			p.playMu.Unlock()
			p.runSynth(tts, job)
		}
		res, ok := p.awaitSynth(job)
		if !ok {
			return // pipeline torn down mid-synth; awaitSynth drained
		}
		p.playMu.Lock()
		genRetired := p.playGen != job.gen
		p.playMu.Unlock()
		if genRetired {
			// A clear landed while Speak was generating: the draft is
			// retired — close the reader, never touch the socket, and
			// do NOT error-log: a cancelled Speak is the EXPECTED
			// outcome of a barge-in during synthesis (clear() cancels
			// the registered job).
			if res.pcm != nil {
				_ = res.pcm.Close() //nolint:mutexio // abort path, no lock held
			}
			continue
		}
		if res.err != nil {
			p.logger.Error("voicechild: tts speak", "err", res.err, "call_id", p.ccid)
			if res.pcm != nil {
				_ = res.pcm.Close() //nolint:mutexio // error path, no lock held
			}
			continue
		}

		// The synth slot (if a prefetch is registered) is claimed at the
		// top of the next iteration; the write-start prefetch hook lives
		// inside playbackStream (OVERLAP POINT 1) so it fires in the
		// same critical section that raises writingNow.
		p.playbackStream(res.pcm, item.gen, res.synthDur)
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

// playbackStream writes one frame's already-synthesized PCM to the .out
// socket (child->daemon). Called ONLY from playbackWorker — never
// concurrently for the same pipeline, so audioOut writes are serialized
// by construction (audit fix 1). A clear received mid-stream drops the
// remaining audio and stops writes (T4): the stream carries the playGen
// it started under, and clear() bumps the gen, so an interrupted stream
// aborts even after a later say re-opens the dropQueued gate (bughunt
// 2026-11-07 voice F-1). `pcm` is the synth goroutine's reader — this
// function owns closing it. synthDur is how long Speak took before this
// audio existed (telemetry; see the cadence report).
func (p *pipeline) playbackStream(pcm io.ReadCloser, gen uint64, synthDur time.Duration) {
	defer func() { _ = pcm.Close() }() //nolint:mutexio // deferred close, no lock held

	p.playMu.Lock()
	if p.playGen != gen {
		// A clear retired this stream's generation between synth and
		// here: it must not start. The stamped-gen check subsumes the
		// old dropQueued gate — a clear always bumps playGen, so
		// dropQueued is never true at a matching gen.
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
	p.writingNow = true
	p.playMu.Unlock()
	defer func() {
		p.playMu.Lock()
		p.writingNow = false
		// Only the current generation may clear `speaking` (a stale,
		// aborted stream must not mute the barge-in VAD of a newer say).
		if p.playGen == gen {
			p.speaking = false
		}
		p.playMu.Unlock()
	}()

	// OVERLAP POINT 1: this frame's audio is about to enter the pacing
	// loop, so the NEXT queued draft's full generation may start now —
	// sentence N+1's Speak runs while the daemon pacer drains N at real
	// time instead of starting only after N fully played (the serial
	// chain that put a 0.7-4.8 s dead gap at every sentence break).
	// Depth one: the slot is empty here (awaitSynth released it), so
	// this registers at most ONE lookahead synth; nothing further starts
	// until this draft's write phase begins.
	p.tryLaunchPrefetch(false)

	chunk, err := voiceframe.ChunkBytes(p.codec, p.sampleRt)
	if err != nil {
		p.logger.Error("voicechild: playback chunk size", "err", err, "call_id", p.ccid)
		return
	}
	// Jitter instrumentation (2026-11-09 chop hunt; extended by the
	// prefetch fix): synth_ms is how long Speak blocked before this
	// audio existed; write_ms is this stream's wall-clock pacing time;
	// ttsRead measures the provider stream's chunk ARRIVALS (network +
	// synth side), outWrite the .out socket hand-off cadence. With the
	// one-frame prefetch working, stream N+1's synth_ms overlaps stream
	// N's write_ms, so the next runs can prove the overlap from the log
	// alone: a dead gap at a sentence break survives only when
	// synth_ms(N+1) > write_ms(N). One INFO report per finished stream
	// goes to the child's stderr (inherited into the daemon log under
	// -f).
	ttsRead := voiceframe.NewCadence(20*time.Millisecond, 8192)
	outWrite := voiceframe.NewCadence(20*time.Millisecond, 8192)
	streamStart := time.Now()
	defer func() {
		tr := ttsRead.Report()
		ow := outWrite.Report()
		p.logger.Info("voicechild: playback cadence", "call_id", p.ccid,
			"events", tr.Events, "bytes", tr.BytesTotal,
			"synth_ms", synthDur.Milliseconds(),
			"write_ms", time.Since(streamStart).Milliseconds(),
			"read_mean_ms", tr.MeanGap.Milliseconds(),
			"read_med_ms", tr.MedianGap.Milliseconds(),
			"read_p95_ms", tr.P95Gap.Milliseconds(),
			"read_max_ms", tr.MaxGap.Milliseconds(),
			"read_overruns", tr.Overruns,
			"write_mean_ms", ow.MeanGap.Milliseconds(),
			"write_max_ms", ow.MaxGap.Milliseconds(),
			"write_overruns", ow.Overruns)
	}()
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
			if rg := ttsRead.Mark(time.Now()); rg > 100*time.Millisecond {
				p.logger.Warn("voicechild: tts chunk arrival stall", "call_id", p.ccid, "gap_ms", rg.Milliseconds())
			}
			ttsRead.AddBytes(int64(n))
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
			outWrite.Mark(time.Now())
			outWrite.AddBytes(int64(n))
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

// ttsCallIDKey stamps the voice call id on the Speak context so the TTS
// provider package (which imports voicechild, never the reverse) can name
// the call in its transport-retry WARN (leaf 04 hygiene).
type ttsCallIDKey struct{}

// WithTTSContext returns ctx carrying the call id for provider logging.
func WithTTSContext(ctx context.Context, callID string) context.Context {
	if callID == "" {
		return ctx
	}
	return context.WithValue(ctx, ttsCallIDKey{}, callID)
}

// TTSCallIDFromContext extracts the stamped call id ("" when unset).
// Used by internal/voicechild/providers.
func TTSCallIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(ttsCallIDKey{}).(string); ok {
		return id
	}
	return ""
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
	// Prefetch fix: the one in-flight synthesis (the prefetched frame,
	// or the head-of-burst Speak still generating) belongs to the
	// retired generation — cancel its Speak ctx so an interrupted turn
	// stops burning provider latency, and its reader — if one still
	// lands — is closed by the worker's retire path, never written.
	job := p.synthJob
	p.playMu.Unlock()
	if job != nil {
		job.cancel()
	}
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
		v := int16(binary.LittleEndian.Uint16(b[i:])) //nolint:gosec // G115: PCM s16le decode — byte-pair truncation is the codec contract
		u := uint64(uint16(v))                        //nolint:gosec // G115: |v| magnitude cast of a 16-bit sample (always in uint16 range)
		if v < 0 {
			u = -u // two's complement wrap: -32768 -> 32768
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
//
// STT evidence gate (defect 2026-11-08): a trip only EMITS when the STT
// session produced a non-empty final transcript within
// rmsSttEvidenceWindow (tracked as lastFinalAt by consumeTranscripts).
// Loudness without recent STT evidence is echo/noise — it must never
// cancel an in-flight answer or stop playback — so those trips are
// suppressed with a Debug log and latch/playback state untouched.
// The evidence is ONE-SHOT (defect 2026-11-09): an authorized trip
// clears lastFinalAt, so re-tripping inside the same 10 s tail needs a
// NEW final; the final that authorized the first trip already dispatched
// the very turn the trip cancelled.
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
	// STT evidence gate (defect 2026-11-08): the trip must be evidence of
	// new SPEECH, not just loud sound. Only emit the interrupt when a
	// non-empty final transcript arrived within rmsSttEvidenceWindow —
	// the phone leg's trigger (STT partials) inherently carries that
	// evidence; the RMS trip does not, so the gate supplies it. A
	// trip without recent evidence is echo/noise: suppress entirely
	// (latch stays armed, playback and any in-flight answer keep
	// running); the window accumulators were already reset above, so
	// a fresh window must re-earn the trip from scratch.
	// Latch semantics are unchanged: only emission is gated; the next say
	// re-arms the latch exactly as before.
	if p.lastFinalAt.IsZero() || time.Since(p.lastFinalAt) > rmsSttEvidenceWindow {
		p.logger.Debug("voicechild: rms barge-in suppressed (no recent stt final)",
			"call_id", p.ccid,
			"since_last_final_ms", func() int64 {
				if p.lastFinalAt.IsZero() {
					return -1
				}
				return time.Since(p.lastFinalAt).Milliseconds()
			}())
		p.playMu.Unlock()
		return
	}
	// Latch trip under playMu (noteBargeIn cannot be called re-entrantly:
	// it would deadlock on playMu). Same field transitions as
	// noteBargeIn: interrupted latches one-interrupt-per-barge-in,
	// speaking drops so playback stops presenting as live (R3).
	p.interrupted = true
	p.speaking = false
	// Evidence is CONSUMED (defect 2026-11-09, one-shot): the final
	// that authorized this trip already dispatched its own turn — the
	// live in-flight answer this trip is cancelling. Leaving the stamp
	// set let a second sustained-loud window re-trip off the SAME stale
	// evidence inside the 10 s tail (five barge-in kills in call
	// local-2d84c384fd93f447). Zeroing lastFinalAt means a second trip
	// needs a NEW final from consumeTranscripts; genuine new caller
	// speech re-stamps within seconds of talking, so real barge-ins
	// keep working.
	p.lastFinalAt = time.Time{}
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

// rmsSttEvidenceWindow bounds how recently the STT session must have
// produced a non-empty FINAL transcript before an RMS VAD trip may emit
// an interrupt (defect 2026-11-08: stale echo reference + a speaking
// operator near speakers kept tripping the RMS guard while the answer
// generated, and every trip cancelled the in-flight turn via
// HandleBargeIn -> cancelPipeline). Loudness is echo/noise evidence, not
// SPEECH evidence; real barge-in is "the user is actually talking", and
// whisper then emits a final within seconds. 10 s comfortably covers a
// final landing after the VAD window (whisper emits finals every few
// seconds of continuous speech) while suppressing pure echo/noise trips.
const rmsSttEvidenceWindow = 10 * time.Second

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
