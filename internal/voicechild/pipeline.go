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
	"unicode"

	"github.com/caimlas/meept/internal/voice/clipstore"
	"github.com/caimlas/meept/internal/voiceframe"
)

// STTProvider turns a stream of PCM chunks into transcripts. Stubbed in
// this leaf; leaf 08 implements real adapters (Deepgram et al).
type STTProvider interface {
	Stream(ctx context.Context, pcm <-chan []byte) (<-chan Transcript, error)
}

// sttFlusher is the optional early-flush seam (BARGE-IN CONTRACT
// (2026-11-11)): an STT adapter that segments utterances itself (the
// whisper CLI adapter buffers a voiced window and finalizes on trailing
// silence) can force-finalize the OPEN window on demand, so the C5
// speaking-final barge-in lane lands a full segment-recognition earlier
// when the energy lane is mid-trip. Implementations must never fire
// recognition on an empty window and never inject silence.
type sttFlusher interface {
	FlushNow()
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
	// Endpoint is the vendor's authoritative speech-end signal (contract
	// C2, F4): Deepgram's speech_final / UtteranceEnd. Additive and
	// optional — false leaves every existing path byte-identical. The
	// pipeline consumes it as a fast-dispatch trigger only while turn
	// aggregation is active (a confirm window > 0).
	Endpoint bool
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

	// minTurnWords (playMu, keys-frame guarded, like turnConfirm): the
	// voice-microfrag short-final gate. A FINAL whose stripped text has
	// FEWER than this many real words drops before evidence stamping and
	// aggregation — tuned for loud rooms where TV fragments ("The other",
	// "This is...") dispatch turns seconds apart (aggregation cannot
	// merge arrivals 3-8 s apart, and word filters are wrong by default:
	// an operator's "Stop." is one word). 0 = disabled (the default;
	// byte-identical legacy behavior — opt-in defaults rule).
	minTurnWords int

	// ---- voice-peragent leaf 01: short-final gate improvements ----
	// All keys-frame guarded under playMu like minTurnWords, and ALL
	// strictly inert while minTurnWords is 0 — the whole block below
	// is only ever consulted inside the gate's N>0 branch, so an
	// old-daemon keys line reproduces the plain word-count gate for
	// finals that are NOT an exemption-set phrase or a reply-window
	// arrival (the two behavioral deltas the master's default set is
	// exactly meant to buy; every existing pre-leaf gate pin holds.
	//
	// exemptPhrases: nil/empty (absent on an old-daemon keys line) =
	// NO exemptions — the pre-leaf gate shape, byte-identical (pinned
	// by TestShortFinalGateOneWordCommandKept, which drops "Stop." at
	// N=2 on a lines-without-selector fixture). Real deployments never
	// see this: the daemon's emit site always sends the effective set
	// (DefaultConfig ships DefaultVoiceExemptPhrases; a nil resolved
	// list normalizes to it at sendKeysFrame).
	//
	// energyGate + minRMS: with the gate on, a short final drops ONLY
	// when the utterance window's average mic RMS is BELOW minRMS —
	// far-field TV fragments are quiet, a near-field "Thank you." is
	// loud and passes. Absent = false/0 = plain word-count gate.
	//
	// fragmentDecayS: after K consecutive drops within this many
	// seconds, the effective N rises by min(K, 3) — chatter detected
	// => stricter for a moment. 0 = off. gateDropStreak/gateDropAt are
	// per-pipeline-instance state (a new call starts fresh: bounded and
	// stateless-safe across calls by construction).
	//
	// replyExemptS: a short final arriving within this many seconds of
	// the CHILD's own last spoken-audio write (lastSayActivity — the
	// pipeline already tracks the playback write loop; no new wire
	// traffic) is a reply to the agent, not background: exempted.
	// Absent-on-wire decodes to defaultReplyExemptS (15); 0 disables.
	exemptPhrases   []string
	energyGate      bool
	minRMS          int
	fragmentDecayS  int
	replyExemptS    int
	gateDropStreak  int
	gateDropAt      time.Time
	lastSayActivity time.Time // playMu: last PCM chunk written to .out by playbackStream
	// utterEnergySum/utterEnergySamples (playMu): mic-chunk energy
	// accumulated over the current utterance window for the energy
	// gate; window = mic chunks since the last gate decision (or the
	// last dispatch), reset at each decision so each short final is
	// measured against its own speech, not the call average.
	utterEnergySum    uint64
	utterEnergySample uint64

	// Turn aggregation (voice-startguard C1, live call
	// local-496a3fd0d2ab3fc6): the whisper provider flushes a segment
	// after 500 ms of wall-idle — right for segmentation latency, wrong
	// as a DISPATCH threshold: a normal mid-sentence breath-pause is
	// 300-600 ms, so each breath fragment finalized a full employee
	// turn and the agent answered pieces of one sentence while the
	// operator was still talking. Fix at the dispatch layer: consecutive
	// finals arriving inside voice.turn_confirm_ms aggregate into ONE
	// prompt frame, dispatched only after the window passes with no new
	// final (and no active mic, see lastMicLoudAt). All fields below are
	// guarded by playMu; no I/O under the lock (the timer closure and
	// the cap-overflow path call flushPendingFinal, which takes playMu,
	// releases it, THEN emits — the same off-lock emit posture as the
	// rmsBargeInCheck interrupt goroutine).
	turnConfirm time.Duration // confirm window; 0 = legacy per-final dispatch (backward-compat)
	// deepgramEndpointMs (F4, playMu, keys-frame guarded): 0 keeps the
	// legacy fixed-timer endpointing; > 0 enables the authoritative
	// speech_final / UtteranceEnd fast-dispatch path.
	deepgramEndpointMs int
	// Smart Turn opt-in mode (C1, from the keys frame): 0 = current
	// behavior (byte-identical), 1 = kill switch (install NO analyzer),
	// 2 = consult AnalyzeEndOfTurn at confirm-window expiry. Set in
	// applyKeys before keysReady closes; read under playMu.
	smartTurnMode int
	// smartTurnDeferred is true once mode 2 has used its single bounded
	// re-arm for the current pending buffer (read/written under playMu).
	smartTurnDeferred bool
	pendingFinal      string      // aggregated trimmed final texts, single-space joined
	pendingFinalParts int         // how many finals are in pendingFinal (telemetry)
	pendingFirstAt    time.Time   // arrival of the FIRST pending part (8 s cap reference)
	pendingFinalTimer *time.Timer // armed/re-armed per arriving final; stopped on dispatch/clear
	pendingFinalSeq   uint64      // timer identity: bumped on every arm AND every retire, so a stale closure that fired just before its Stop sees a mismatch and returns (never dispatches a fresh window early)
	micHoldRearms     int         // bounded mic-hold re-arms for the current pending (maxMicHoldRearms)
	// lastMicLoudAt (playMu) is when pumpAudioIn last saw an incoming
	// mic chunk above the loose speaking floor (bargeInRms/4). Timer
	// expiry with a fresh stamp (< micLoudHoldWindow) means the operator
	// likely paused for breath and may resume: the flush re-arms once,
	// bounded (maxMicHoldRearms), instead of dispatching. Local-leg only
	// in practice: phone legs carry bargeInRms == 0, the floor compare
	// never trips, and the field stays zero — a plain confirm window.
	lastMicLoudAt time.Time

	audioOut net.Conn // child->daemon PCM (C2 <ccid>.out)
	audioIn  net.Conn // daemon->child PCM (C2 <ccid>.in)
	audioDir string

	// open-frame profile (local-leg plan CL1/CL3), set once by handleOpen
	// before the pipeline starts; read-only afterwards (no lock needed).
	// Providers themselves keep arriving over the keys frame (Tier A owns
	// that wire), so this carries ONLY what the local leg needs early:
	// the leg marker (RMS barge-in runs when local) and its threshold.
	local      bool
	bargeInRms int

	// vad is the input-side speech-vs-noise analyzer (jargo Silero VAD),
	// built lazily in handleOpen for the LOCAL leg only when a VADFactory
	// is wired and the ONNX runtime loads. nil = legacy RMS + STT-evidence
	// gate (byte-identical). vadSpeaking is the smoothed speaking state
	// the pump updates; both are guarded by playMu. (Restored 2026-11-12:
	// a sibling revert had dropped the fields while open.go still wired them.)
	vad         VADAnalyzer
	vadSpeaking bool

	// turn is the input-side COMPLETE-THOUGHT analyzer (jargo Smart Turn),
	// built alongside the VAD for the local leg. All analyzer calls are
	// guarded by turnMu (the pipeline's own lock — jargo's turn base has no
	// internal lock): the audio pump calls AppendAudio, and the dispatch
	// paths + teardown call Clear so the model's per-turn buffer never
	// spans utterances. (The completion VERDICT lane - AnalyzeEndOfTurn +
	// a stored turn state - was dropped with the peragent merge's
	// confirm-window rework; re-add it here if endpointing ever consults
	// the model again.)
	turn   TurnAnalyzer
	turnMu sync.Mutex

	// analyzerInit tracks the async ONNX analyzer construction started by
	// handleOpen (VAD + Smart Turn build on goroutines so the single C1
	// frame-dispatch loop is never blocked by dlopen/model load). close()
	// cancels p.ctx then waits on this barrier BEFORE taking/releasing the
	// analyzer fields, so an in-flight factory result is either installed
	// (and closed here) or observes p.ctx.Err() and closes itself — no ONNX
	// session leaks on a fast call teardown. installVAD/installTurn use
	// installOnce so a late factory result after close can never re-arm a
	// nil field the teardown already took.
	analyzerInit  sync.WaitGroup
	vadInstalled  sync.Once
	turnInstalled sync.Once

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
	// Prefetch depth is bounded to TWO frames (prefetchDepth): PCM still
	// leaves audioOut strictly in dequeue order (one worker drains
	// synthJobs FIFO — the await order, never the completion order), at
	// most TWO extra utterances are ever synthesizing/open, and a clear
	// retires BOTH queued synths with the same playGen gate (their
	// readers are cancelled AND closed, so no stale bytes reach the
	// socket after the barge-in). Reader-serialization is unchanged:
	// Speak CALLS may overlap, but a reader is only ever opened inside
	// playbackStream, which the single worker runs one frame at a time.
	playMu      sync.Mutex
	dropQueued  bool // clear received: drop queued audio, stop writes
	interrupted bool // one interrupt per barge-in until playback restarts
	speaking    bool // playback active (say in flight)
	// writingNow (playMu) is true while the worker is INSIDE a frame's
	// chunk write loop (set with speaking==true in playbackStream,
	// cleared on its exit). It is telemetry/state only: the enqueueSay
	// prefetch hook starts N+1's synthesis unconditionally (see
	// tryLaunchPrefetch) — a say that arrives while the worker is
	// between frames (synthesizing N head-of-burst, or parked) must
	// overlap IMMEDIATELY too, otherwise the daemon pacer drains its
	// ~1.28 s buffer and starves for the full non-streaming qwen3
	// generation (the multi-second dead-air gaps in local-611897f3).
	// Depth stays bounded at TWO frames past the current write.
	writingNow   bool
	sayQueue     []queuedSay // pending say frames (gen-stamped), drained FIFO by the worker (audit fix 8: replaces the never-consumed outCh placeholder)
	playWorkerUp bool        // a playback worker goroutine is draining sayQueue
	// synthJobs (playMu) is the bounded FIFO queue of in-flight or
	// prefetched syntheses the worker owns (prefetchDepth entries max):
	// empty while no Speak is outstanding. The HEAD is always the next
	// frame to play — launches append in dequeue order and only the
	// worker pops/retires — so await order is strict FIFO regardless of
	// Speak completion order. clear() cancels ALL queued jobs (their
	// Speak ctx) so an interrupted turn stops burning provider latency;
	// awaitSynth/retireSynth remove the slot. See synthJob,
	// registerSynthLocked, tryLaunchPrefetch.
	synthJobs []*synthJob
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
	// normalizePreserve (leaf 03, C6): the caller-supplied preserve hook
	// for NormalizeUtterance — employee context names/subjects that must
	// survive the noise-token drop (garbled names feed the fuzzy match
	// downstream). Set once at pipeline construction (SetNormalizePreserve,
	// before transcripts start); read-only in consumeTranscripts, so no
	// lock guards it. nil = nothing preserved (id hook semantics).
	normalizePreserve func(token string) bool
	// lastDispatchAt (playMu) is when consumeTranscripts last succeeded
	// dispatching a FINAL prompt frame (emitPrompt with tr.Final==true)
	// — the start of the currently in-flight turn. The RMS STT-evidence
	// gate additionally requires lastFinalAt to POST-DATE it (defect
	// 2026-11-11, live call local-611f46b1c3fe5c5b): evidence older
	// than the current turn's dispatch is the user's OWN prompt that
	// started that turn, never new speech — so any echo/noise RMS
	// window inside the 10 s tail after dispatch was auto-authorized
	// and cancelled the reply mid-sentence (observed at 2.9 s and 1.2 s
	// into answers). Genuine new caller speech re-stamps lastFinalAt
	// through consumeTranscripts and re-earns the trip; whisper finals
	// also carry their own interrupt directly via the C5 speaking-final
	// path in consumeTranscripts, so the RMS lane stays purely an
	// early-warning lane that must never self-authorize.
	lastDispatchAt time.Time
	// echoWin (playMu) is the rolling window of normalized recent AGENT
	// speech — one entry per say frame the child is asked to synthesize
	// (see echo_window.go). It is the text-provenance lane of the local
	// leg's fast barge-in: the second authorization source for energy
	// trips AND the filter that keeps agent-echo whisper finals from
	// dispatching self-answer turns.
	//
	// BARGE-IN CONTRACT (2026-11-11): the dispatch watermark (e0dbce12)
	// closed the prompt-final auto-authorize hole by making the RMS lane
	// wait for a post-dispatch final — which put the real barge-in floor
	// at ~850-1000 ms (whisperSilenceMs 500 + ~350 ms cold subprocess per
	// segment), because that final always carries its own C5 interrupt
	// and the RMS lane can never beat it. The watermark ALSO unmasked the
	// self-answer loop: a whisper final that IS the agent's own voice
	// (TTS -> speaker -> mic) dispatched a full employee turn (exact-match
	// dedupe only). This window is what makes restoring the fast lane safe
	// to re-open — do not delete it to "fix" phantom barge-in again; see
	// the file header of echo_window.go for the full contract.
	echoWin echoWindow

	// ---- filler-clip cache (plan 20261113-filler-clips leaf 03) ----
	// clipStore is the content-addressed PCM cache (Contract C2), shared
	// across pipelines (the Child constructs it once). Set at pipeline
	// construction, read-only afterwards — like codec, no lock guards it.
	clipStore *clipstore.Store
	// ttsRefHash (playMu) is the decoded keys-frame tts_ref_hash: the
	// sha256 of the clone reference audio (nil = non-clone, Contract C1).
	// The child does not recompute incoming clip keys today (the daemon
	// planner pins them), but the identity input stays stored so the
	// child-side ClipKey computation is byte-testable against the daemon
	// path, and the render meta carries the same identity (adjudication A4).
	ttsRefHash []byte
	// clipMu guards the background render queue ONLY (never held across
	// file I/O or Speak — mutexio). clipQueue holds pending
	// voiceframe.ClipSpec entries; clipWorkerUp is released under clipMu
	// in the same critical section that observes the empty queue, so a
	// concurrent enqueueClipRenders re-launches the worker (the sayQueue
	// playWorkerUp discipline mirrored for renders).
	clipMu       sync.Mutex
	clipQueue    []voiceframe.ClipSpec
	clipWorkerUp bool
	// clipRekickTimer is the one-shot busy-retire re-kick (bughunt
	// 2026-11-19 L1): when the render worker retires because live synth
	// is busy with specs still queued, this timer relaunches it after
	// clipRekickDelay if the queue is still non-empty and the pipeline
	// alive — a chatty call no longer defers prewarm for its whole life
	// waiting for a NEW clip_render frame. Guarded by clipMu; stopped
	// on teardown and whenever the queue empties / a worker takes over.
	clipRekickTimer *time.Timer
	// keysReady is closed when the keys frame has been applied (renders
	// need the provider identity; adjudication A3). Sync.Once so
	// applyKeys is idempotent on a keys re-send.
	keysReady     chan struct{}
	keysReadyOnce sync.Once

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
		// Clip cache: the store is shared across calls (constructed once
		// per child); keysReady gates the background render worker.
		clipStore: c.clipStoreFor(),
		keysReady: make(chan struct{}),
		// Aggregation on by default (C1): the keys frame may override
		// this with the daemon's voice.turn_confirm_ms (0 = legacy)
		// BEFORE any transcript can arrive (the STT session only binds
		// once a real provider constructs after applyKeys).
		turnConfirm: time.Duration(defaultTurnConfirmMs) * time.Millisecond,
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
// initAnalyzersAsync starts the local-leg ONNX analyzer construction off
// the caller's goroutine. handleOpen calls this AFTER it has registered the
// pipeline and sent setup, so a slow dlopen/model load cannot head-of-line
// block the C1 dispatch loop (and every other live call's frames behind it).
// Each factory result installs under installOnce; after close() cancels the
// pipeline ctx, a late result closes itself instead of re-arming a field the
// teardown already took.
func (p *pipeline) initAnalyzersAsync(vadFactory VADFactory, turnFactory TurnFactory, sampleRate int) {
	startFactory := func(build func() (io.Closer, error), name string) {
		p.analyzerInit.Add(1)
		go func() {
			defer p.analyzerInit.Done()
			closer, err := build()
			if err != nil {
				p.logger.Info("voicechild: analyzer unavailable; local leg degrades",
					"analyzer", name, "call_id", p.ccid, "err", err)
				return
			}
			installed := false
			switch a := closer.(type) {
			case VADAnalyzer:
				p.vadInstalled.Do(func() {
					if p.ctx.Err() != nil {
						return
					}
					p.playMu.Lock()
					p.vad = a
					p.playMu.Unlock()
					installed = true
				})
			case TurnAnalyzer:
				p.turnInstalled.Do(func() {
					if p.ctx.Err() != nil {
						return
					}
					a.SetSampleRate(sampleRate)
					p.turnMu.Lock()
					p.turn = a
					p.turnMu.Unlock()
					installed = true
				})
			}
			if !installed {
				// Pipeline closed before install; close the session here.
				if cerr := closer.Close(); cerr != nil {
					p.logger.Warn("voicechild: late analyzer close", "analyzer", name, "err", cerr, "call_id", p.ccid)
				}
			}
		}()
	}
	if vadFactory != nil {
		startFactory(func() (io.Closer, error) {
			return vadFactory(sampleRate)
		}, "silero vad")
	}
	// Smart Turn (F1, 02-smart-turn.md T1): the mode arrives on the keys
	// frame, which on a live call follows open. Build the turn analyzer
	// ONLY after keys land, then skip entirely for the mode-1 kill switch
	// (no analyzer installed; VAD stays intact). Modes 0 and 2 install
	// exactly as today. The keysReady gate keeps a mode-0 old-daemon line
	// byte-identical (keys-less calls never build a turn analyzer, same
	// as a factory that is absent), and never blocks the C1 loop or
	// close()'s analyzerInit barrier.
	if turnFactory != nil {
		p.analyzerInit.Add(1)
		go func() {
			defer p.analyzerInit.Done()
			select {
			case <-p.keysReady:
			case <-p.ctx.Done():
				return
			}
			p.playMu.Lock()
			mode := p.smartTurnMode
			p.playMu.Unlock()
			if mode == 1 {
				p.logger.Info("voicechild: smart turn kill switch; analyzer not installed", "call_id", p.ccid)
				return
			}
			startFactory(func() (io.Closer, error) {
				return turnFactory(sampleRate)
			}, "smart turn")
		}()
	}
}

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

// bracketedTokenRe matches one whisper paralinguistic token run: either a
// bracket token — opening ( [ followed by 1-60 non-']' / non-newline
// characters and a closing ) ] — or an asterisk marker — * followed by 1-60
// non-'*' / non-newline characters and a closing * (voice-microfrag defect
// 2026-11-11, live call local-0627960006a7f3d9: whisper emits
// "*unintelligible*", "*static*", "*music*", "*laughter*" and the bracket-
// only strip let a bare "*unintelligible*" final dispatch as a user
// prompt). The two classes are ONE alternation so the nesting/unclosed
// rules carry over identically: the body excludes the closer, so an
// unclosed run ("*unintelligible" with no second "*") matches nothing and
// keeps its residue REAL — the rule errs toward hearing the caller, and a
// lone "*" (empty body, needs 1+ chars) never matches. The 60-char cap
// keeps a token token-sized — a long parenthetical containing real words
// is caller speech, not an artifact. (?i) is spec fidelity: the class is
// shape-based, so case never binds.
var bracketedTokenRe = regexp.MustCompile(`(?i)[\[(][^\]\n]{1,60}[\])]|\*[^*\n]{1,60}\*`)

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
	residue := stripNonSpeechTokens(text)
	// A surviving "*" is an unclosed asterisk run (bare "*", "hello *"):
	// the shape rule cannot prove it is an artifact — a bracketed empty
	// marker "()" is a COMPLETE marker shape, a lone asterisk is not —
	// so it stays REAL, the same "errs toward hearing the caller" rule
	// that keeps an unclosed bracket's word residue real (leaf doc
	// 20261111-voice-microfrag pins bare "*" and "hello *" real).
	if strings.ContainsRune(residue, '*') {
		return true
	}
	return !punctuationOnlyRe.MatchString(residue)
}

// stripNonSpeechTokens removes every bracket/asterisk paralinguistic run
// (bracketedTokenRe) and trims: the residue is what the caller actually
// said, MINUS whisper's non-speech markers. Shared by isRealSpeech and the
// min_turn_words gate so the word count and the real-speech check see the
// SAME stripped text (a "[music] stop that" counts 2 words, not 4).
func stripNonSpeechTokens(text string) string {
	return strings.TrimSpace(bracketedTokenRe.ReplaceAllString(text, " "))
}

// wordCount counts whitespace-separated tokens in already-stripped text
// (the min_turn_words gate's unit: one "real word" = one token).
func wordCount(stripped string) int {
	return len(strings.Fields(stripped))
}

// gateVerdict is one short-final gate decision.
type gateVerdict int

const (
	// gatePass: not short at the effective threshold (or gate off) —
	// NO log, byte-identical to the pre-leaf hot path.
	gatePass gateVerdict = iota
	// gateDrop: short and unsaved — INFO 'short final dropped' with
	// reason=wordcount|energy (master telemetry).
	gateDrop
	// gateExempt: short but saved by an improvement — INFO 'short
	// final exempted' with reason=phrase|reply|energy (loud-pass).
	gateExempt
)

// evaluateShortFinal runs the min_turn_words gate plus the four leaf-01
// improvements for one trimmed FINAL, returning (verdict, word count,
// reason) — words/reason are only meaningful for non-pass verdicts.
// The whole improvement block is strictly inert while minTurnWords is
// 0 (first-line return; zero-config = byte-identical legacy, the pin).
//
// Decision order (master: exemptions run against the WORD-COUNT drop,
// energy only when the gate would otherwise drop):
//  1. effective N = base N + min(consecutive-drop streak, 3) while a
//     drop happened within fragment_decay_s (decay off => streak never
//     binds). words >= effective N passes silently (and resets the
//     streak — any dispatched final clears the chatter signal).
//  2. exempt phrases: the whole stripped residue, lowercased and outer
//     punctuation trimmed, matches one configured phrase — exempted
//     ("stop!" -> "stop"). Absent/empty set = NO exemptions (legacy).
//  3. reply window: the final arrived within reply_exempt_s of the
//     CHILD's own last spoken-audio write (lastSayActivity — the
//     playback write loop already exists; no new wire traffic). That
//     is a REPLY to the agent, not background chatter.
//  4. energy gate: with energy_gate on, a short final drops ONLY when
//     the utterance window's average mic RMS is below min_rms (quiet
//     far-field TV -> drop reason=energy; loud near-field "Thank you."
//     -> exempted reason=energy). With the gate off: drop
//     reason=wordcount.
//
// Streak bookkeeping: a drop increments it and stamps gateDropAt; a
// pass/exempt resets it. State is per pipeline instance — a new call
// starts clean (bounded, stateless-safe across calls by construction).
func (p *pipeline) evaluateShortFinal(trimmed string) (gateVerdict, int, string) {
	p.playMu.Lock()
	base := p.minTurnWords
	if base <= 0 {
		p.playMu.Unlock()
		return gatePass, 0, ""
	}
	now := time.Now()
	effN := base
	if p.fragmentDecayS > 0 && p.gateDropStreak > 0 &&
		!p.gateDropAt.IsZero() && now.Sub(p.gateDropAt) < time.Duration(p.fragmentDecayS)*time.Second {
		bump := min(p.gateDropStreak, 3)
		effN = base + bump
	}
	words := wordCount(stripNonSpeechTokens(trimmed))
	if words >= effN {
		p.gateDropStreak = 0
		// A dispatched utterance ends the current mic window: reset
		// the utterance-energy accumulator so the NEXT short final is
		// measured over its own speech, not this loud one's tail.
		p.utterEnergySum, p.utterEnergySample = 0, 0
		p.playMu.Unlock()
		return gatePass, words, ""
	}
	// Short at the effective threshold: run the improvements against
	// the would-be drop. nil/empty exempt set = no exemptions
	// (the legacy gate shape on an absent-on-wire line).
	if matchExemptPhrase(stripNonSpeechTokens(trimmed), p.exemptPhrases) {
		p.gateDropStreak = 0
		// Consume the window on the exempt path too (defect
		// 2026-11-14): a phrase exemption is judged on THIS final's
		// speech — leaving the accumulator intact let a loud
		// dispatched utterance's energy bleed into the NEXT short
		// final's energy verdict (stale mixed window). Same reset
		// posture as the energyGate branch below and the pass
		// branch above.
		p.utterEnergySum, p.utterEnergySample = 0, 0
		p.playMu.Unlock()
		return gateExempt, words, "phrase"
	}
	if p.replyExemptS > 0 && !p.lastSayActivity.IsZero() &&
		now.Sub(p.lastSayActivity) < time.Duration(p.replyExemptS)*time.Second {
		p.gateDropStreak = 0
		// Consume the window on the reply-exempt path too (same
		// stale-window defect as the phrase branch: the exempted
		// final's own speech must not pollute the next verdict).
		p.utterEnergySum, p.utterEnergySample = 0, 0
		p.playMu.Unlock()
		return gateExempt, words, "reply"
	}
	if p.energyGate {
		quiet := utteranceWindowQuiet(p.utterEnergySum, p.utterEnergySample, p.minRMS)
		// Consume the window either way: each short final is judged
		// on its own speech.
		p.utterEnergySum, p.utterEnergySample = 0, 0
		if !quiet {
			p.gateDropStreak = 0
			p.playMu.Unlock()
			return gateExempt, words, "energy"
		}
		p.gateDropStreak++
		p.gateDropAt = now
		p.playMu.Unlock()
		return gateDrop, words, "energy"
	}
	p.gateDropStreak++
	p.gateDropAt = now
	p.playMu.Unlock()
	return gateDrop, words, "wordcount"
}

// matchExemptPhrase reports whether the stripped residue's WHOLE text
// equals one exemption phrase, case-insensitively and ignoring outer
// punctuation/whitespace ("Stop!" and "thanks." match; "stop that"
// does not match "stop" — whole-residue only, per master). Internal
// whitespace collapses (STT double-spaces between words), so
// "thank  you" matches "thank you" while inter-word punctuation
// inside a phrase binds ("mm-hm" needs the hyphen class intact).
func matchExemptPhrase(residue string, phrases []string) bool {
	normalized := normalizePhrase(residue)
	if normalized == "" {
		return false
	}
	for _, ph := range phrases {
		// The configured phrases are matched on their own normalized
		// form too, so "Stop" and "stop." in config behave alike.
		cand := normalizePhrase(ph)
		if cand != "" && cand == normalized {
			return true
		}
	}
	return false
}

// normalizePhrase is the exemption-match canonical form: outer
// punctuation/whitespace trimmed, inner runs of whitespace collapsed
// to one space, lowercased.
func normalizePhrase(s string) string {
	trimmed := strings.TrimFunc(s, func(r rune) bool {
		return unicode.IsPunct(r) || unicode.IsSpace(r)
	})
	return strings.ToLower(strings.Join(strings.Fields(trimmed), " "))
}

// utteranceWindowQuiet reports whether the accumulated mic window's
// average energy sits BELOW the minRMS floor (integer form, same
// posture as rmsChunkOver: sum < thr^2 * n <=> rms < thr). A window
// with no samples counts QUIET (nothing loud was heard — the master's
// far-field posture; minRMS <= 0 never drops: everything reads loud,
// the documented exemptions-only posture).
func utteranceWindowQuiet(sum, samples uint64, minRMS int) bool {
	if minRMS <= 0 {
		return false
	}
	thr := uint64(minRMS)
	return sum < thr*thr*samples
}

// noteUtteranceEnergy accumulates one incoming mic chunk's energy for
// the energy gate's utterance window (voice-peragent leaf 01). Called
// by pumpAudioIn with EVERY daemon->child chunk BEFORE forwarding,
// like the barge-in taps. Cheap when the gate is off: one playMu
// critical section, no PCM math unless energy_gate is on AND the
// short-final gate is armed (minTurnWords > 0) — so zero-config legs
// never pay a per-chunk cost beyond the uncontended lock the existing
// VAD taps already take on local legs (phone legs: a ~20 ns no-op
// lock per 20 ms chunk, below measurement floor).
func (p *pipeline) noteUtteranceEnergy(chunk []byte) {
	p.playMu.Lock()
	needed := p.minTurnWords > 0 && p.energyGate
	p.playMu.Unlock()
	if !needed {
		return
	}
	sum, samples := rmsEnergy(chunk)
	p.playMu.Lock()
	p.utterEnergySum += sum
	p.utterEnergySample += samples
	p.playMu.Unlock()
}

// markSayActivity stamps the child's own last spoken-audio write time
// (voice-peragent leaf 01): called by playbackStream after each PCM
// chunk reaches the .out socket. Only the reply-window exemption reads
// it, and only when the short-final gate is armed — inert otherwise.
// The pipeline ALREADY tracks playback writes here, so this reuses
// existing state (no new wire traffic, master improvement #1).
func (p *pipeline) markSayActivity() {
	p.playMu.Lock()
	if p.minTurnWords > 0 && p.replyExemptS > 0 {
		p.lastSayActivity = time.Now()
	}
	p.playMu.Unlock()
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

// Turn-aggregation constants (voice-startguard C1).
const (
	// defaultTurnConfirmMs is the child-side fallback when the keys
	// frame carries no turn_confirm_ms selector (an old daemon): the
	// same default as config voice.turn_confirm_ms. A 900 ms window
	// covers the 300-600 ms breath-pause that fragmented the live call
	// while staying far under a "did you mean...?" turn latency budget.
	defaultTurnConfirmMs = 900
	// defaultReplyExemptS is the child-side fallback for an ABSENT
	// reply_exempt_s selector on an old daemon's keys line
	// (voice-peragent leaf 01): a short final within 15 s of the
	// child's own last spoken-audio write is treated as a reply and
	// exempted from the short-final gate. 0 (explicit on the wire)
	// disables the exemption.
	defaultReplyExemptS = 15
	// turnAggregationMaxAge bounds the buffer at ~8 s of speech: a
	// caller who never pauses 900 ms for eight seconds force-dispatches
	// what has aggregated (defensive; a monologue still reaches the
	// employee). The bound is the AGE of the first pending part.
	turnAggregationMaxAge = 8 * time.Second
	// micLoudHoldWindow: if the incoming mic was loud within this window
	// when the confirm timer expires, the pause looks like a breath
	// mid-sentence — re-arm instead of dispatching.
	micLoudHoldWindow = 400 * time.Millisecond
	// micHoldRecheckExtra is the small grace added to a mic-hold re-arm
	// so the re-check lands at confirm+50 ms, past the breath that
	// triggered it (contract C1 point 4).
	micHoldRecheckExtra = 50 * time.Millisecond
	// maxMicHoldRearms bounds the hold: at most TWO re-arms per pending
	// aggregation, so a perpetually-noisy line can never stall dispatch
	// forever (the dispatch must land within 3 windows).
	maxMicHoldRearms = 2
)

// confirmWindow reads the aggregation window (playMu regime). <= 0
// (unset or explicitly legacy) means per-final dispatch.
func (p *pipeline) confirmWindow() time.Duration {
	p.playMu.Lock()
	defer p.playMu.Unlock()
	if p.turnConfirm <= 0 {
		return 0
	}
	return p.turnConfirm
}

// accumulateFinal adds one trimmed FINAL transcript to the pending
// aggregation buffer and re-arms the confirm timer. It reports
// (aggregatedText, parts, flushed=true) when the 8 s age cap forced an
// immediate dispatch (the returned text INCLUDES this final); otherwise
// the caller must not emit anything — the timer owns the dispatch.
// No I/O under playMu: the AfterFunc closure only ever calls
// finalConfirmFired, which captures under the lock and emits OFF it.
func (p *pipeline) accumulateFinal(text string) (string, int, bool) {
	now := time.Now()
	p.playMu.Lock()
	starting := p.pendingFinal == ""
	overflow := !starting && !p.pendingFirstAt.IsZero() && now.Sub(p.pendingFirstAt) >= turnAggregationMaxAge
	if starting {
		p.pendingFirstAt = now
		p.micHoldRearms = 0
		p.smartTurnDeferred = false // fresh window resets the mode-2 defer budget
	}
	if starting {
		p.pendingFinal = text
	} else {
		p.pendingFinal = p.pendingFinal + " " + text
	}
	p.pendingFinalParts++
	agg, parts := p.pendingFinal, p.pendingFinalParts
	if overflow {
		p.clearPendingFinalLocked()
		p.playMu.Unlock()
		return agg, parts, true
	}
	if p.pendingFinalTimer != nil {
		p.pendingFinalTimer.Stop()
	}
	// Timer identity (pendingFinalSeq): the closure captures the seq
	// VALUE (no shared mutable cell — race-free), and finalConfirmFired
	// only acts while the seq still matches. A stale AfterFunc that
	// fired just before this Stop (and blocked on playMu) sees the
	// bumped seq and returns instead of dispatching the freshly
	// re-armed window early.
	p.pendingFinalSeq++
	seq := p.pendingFinalSeq
	p.pendingFinalTimer = time.AfterFunc(p.turnConfirm, func() { p.finalConfirmFired(seq) })
	p.playMu.Unlock()
	return "", 0, false
}

// mergeAndTakeFinal appends one trimmed final to the pending buffer and
// captures+clears the merged text in ONE playMu critical section,
// returning (aggregatedText, parts). Used for the barge-in final: the
// dispatch must land before any clear arriving over the daemon round-trip
// can race a separate capture (see the consumeTranscripts aggregation
// branch). A lone barge-in final with an empty buffer merges to just its
// own text (parts=1).
func (p *pipeline) mergeAndTakeFinal(text string) (string, int) {
	p.playMu.Lock()
	if p.pendingFinal == "" {
		p.pendingFinal = text
	} else {
		p.pendingFinal = p.pendingFinal + " " + text
	}
	p.pendingFinalParts++
	agg, parts := p.pendingFinal, p.pendingFinalParts
	p.clearPendingFinalLocked()
	p.playMu.Unlock()
	return agg, parts
}

// clearPendingFinalLocked drops the pending buffer, its timer, and the
// mic-hold budget. The seq bump (on every retire, not only on arm)
// makes the identity total: a closure captured under an old seq can
// never match after any clear/dispatch, so no stale timer can ever
// dispatch a buffer that a newer window owns. Caller holds playMu.
func (p *pipeline) clearPendingFinalLocked() {
	if p.pendingFinalTimer != nil {
		p.pendingFinalTimer.Stop()
		p.pendingFinalTimer = nil
	}
	p.pendingFinalSeq++
	p.pendingFinal = ""
	p.pendingFinalParts = 0
	p.pendingFirstAt = time.Time{}
	p.micHoldRearms = 0
	p.smartTurnDeferred = false
}

// finalConfirmFired is the confirm-timer closure (firedSeq is the
// pendingFinalSeq captured when the window was armed; any arm/retire
// under playMu bumps the seq, so a stale closure that raced a Stop
// sees the mismatch and returns). Mic hold: a fresh lastMicLoudAt stamp
// (< micLoudHoldWindow) re-arms the window once more (bounded
// maxMicHoldRearms) instead of dispatching, so the fragment does not
// land the instant a breath ends while speech resumes; the re-arms are
// the ONLY bound and dispatch is guaranteed after three windows.
// Otherwise it captures the buffer under playMu and dispatches off-lock
// (mutexio: no I/O under the lock).
func (p *pipeline) finalConfirmFired(firedSeq uint64) {
	p.playMu.Lock()
	if p.pendingFinal == "" || p.pendingFinalSeq != firedSeq {
		// clear()/cap-flush retired the buffer, or a newer window owns
		// it (this closure is the stale timer of a re-armed window).
		p.playMu.Unlock()
		return
	}
	if !p.lastMicLoudAt.IsZero() && time.Since(p.lastMicLoudAt) < micLoudHoldWindow && p.micHoldRearms < maxMicHoldRearms {
		p.micHoldRearms++
		window := p.turnConfirm
		if window <= 0 {
			window = time.Duration(defaultTurnConfirmMs) * time.Millisecond
		}
		p.pendingFinalSeq++
		rearmSeq := p.pendingFinalSeq
		p.pendingFinalTimer = time.AfterFunc(window+micHoldRecheckExtra, func() { p.finalConfirmFired(rearmSeq) })
		p.playMu.Unlock()
		return
	}
	// Smart Turn (F1, 02-smart-turn.md T3): mode 2 consults the model's
	// end-of-turn verdict at confirm-window expiry. The verdict is read
	// under turnMu ONLY — never while holding playMu (turn.go lock rule,
	// mutexio-safe). Snapshot the policy under playMu, RELEASE it, run the
	// model, then re-acquire playMu to act — so no analyzer I/O ever
	// happens under playMu. A concurrent final/clear between the two
	// playMu sections bumps pendingFinalSeq; the re-check below makes
	// this closure a no-op in that case (the new window owns the buffer).
	mode, deferred := p.smartTurnMode, p.smartTurnDeferred
	p.playMu.Unlock()

	complete, prob, verdictErr := false, 0.0, error(nil)
	analyzerPresent := false
	if mode == 2 {
		p.turnMu.Lock()
		t := p.turn
		analyzerPresent = t != nil
		if t != nil {
			complete, prob, verdictErr = t.AnalyzeEndOfTurn()
		}
		p.turnMu.Unlock()
		decision := "dispatch"
		switch {
		case !analyzerPresent:
			// No analyzer installed (phone legs build analyzers for
			// frame.Local only; the mode-1 kill switch; a factory that
			// failed to load): a verdict CANNOT exist, so reading the
			// zero values as complete=false would defer EVERY turn one
			// extra window for nothing (bughunt 2026-11-19 M1). Treat
			// it as the error-fallback lane: dispatch on schedule.
			decision = "error-fallback"
			p.logger.Info("voicechild: smart turn mode 2 with no analyzer installed; dispatching",
				"call_id", p.ccid)
		case verdictErr != nil:
			decision = "error-fallback" // degrade to today's behavior
		case !complete && !deferred:
			decision = "defer" // one bounded re-arm
		}
		p.logger.Info("voicechild: smart turn verdict applied",
			"call_id", p.ccid, "verdict", complete, "probability", prob,
			"decision", decision, "err", verdictErr)
	}

	p.playMu.Lock()
	if p.pendingFinal == "" || p.pendingFinalSeq != firedSeq {
		p.playMu.Unlock()
		return
	}
	// complete=false defers dispatch by ONE bounded re-arm; the second
	// expiry (deferred already true) or any complete=true/error result
	// dispatches. No infinite hold, no caller-cutoff regression. A nil
	// analyzer never defers (bughunt M1): defer requires a real verdict.
	// analyzerPresent was snapshotted in the turnMu section above — no
	// turnMu acquisition under playMu (lock-order rule).
	deferDispatch := mode == 2 && analyzerPresent && verdictErr == nil && !complete && !deferred
	if deferDispatch {
		p.smartTurnDeferred = true
		window := p.turnConfirm
		if window <= 0 {
			window = time.Duration(defaultTurnConfirmMs) * time.Millisecond
		}
		p.pendingFinalSeq++
		rearmSeq := p.pendingFinalSeq
		p.pendingFinalTimer = time.AfterFunc(window+micHoldRecheckExtra, func() { p.finalConfirmFired(rearmSeq) })
		p.playMu.Unlock()
		return
	}
	agg, parts := p.pendingFinal, p.pendingFinalParts
	p.clearPendingFinalLocked()
	p.playMu.Unlock()
	p.dispatchAggregated(p.ctx, agg, parts, "confirm-window")
}

// endpointEnabled reports whether the authoritative Deepgram endpoint fast
// path is opted in (contract C1, F4). 0 (the default and the wire-absent
// value) keeps the legacy fixed-timer endpointing byte-identical.
func (p *pipeline) endpointEnabled() bool {
	p.playMu.Lock()
	on := p.deepgramEndpointMs > 0
	p.playMu.Unlock()
	return on
}

// flushPendingFinal dispatches any pending aggregated text NOW (EOF of
// the transcript stream, or pipeline close/hangup — contract C1 point
// 5). It is a no-op when nothing is pending or aggregation is off
// (legacy mode never buffers). clear() deliberately does NOT call this:
// an interrupted operator's partial utterance is cancelled exactly like
// the old per-final dispatch cancel (the barge-in that produced the
// clear means the daemon is already tearing the turn down).
func (p *pipeline) flushPendingFinal(ctx context.Context, reason string) {
	p.playMu.Lock()
	agg, parts := p.pendingFinal, p.pendingFinalParts
	p.clearPendingFinalLocked()
	p.playMu.Unlock()
	if agg == "" {
		return
	}
	p.dispatchAggregated(ctx, agg, parts, reason)
}

// dispatchAggregated emits ONE final prompt frame (last:true) for the
// aggregated buffer and logs the C1 telemetry line. The emit runs
// off-lock like every other frame path; awaitPendingInterrupt keeps the
// C5 interrupt-before-prompt order even when the flush races an RMS trip.
func (p *pipeline) dispatchAggregated(ctx context.Context, text string, parts int, reason string) {
	if text == "" {
		return
	}
	p.awaitPendingInterrupt()
	// Leaf 03 (T3, normalize wiring): the aggregated buffer holds RAW
	// final texts (accumulateFinal runs before normalization), so the
	// LLM-facing prompt is normalized HERE at the single choke point;
	// the raw text rides the frame's From field for the daemon binding
	// (AppendTranscript keeps RAW — same contract as the inline path).
	promptText := NormalizeUtterance(text, openVocabularyPreserve)
	rawText := strings.TrimSpace(text)
	if promptText == "" {
		// The aggregation collapsed to pure filler — drop AFTER the
		// evidence stamp (same semantics as the inline path).
		p.logger.Info("voicechild: aggregated final normalized to empty; dropped",
			"call_id", p.ccid, "raw", transcriptPreview(rawText), "reason", reason)
		return
	}
	if err := p.child.emitPrompt(ctx, p.ccid, promptText, true, rawText); err != nil {
		p.logger.Error("voicechild: emit aggregated prompt", "err", err, "call_id", p.ccid)
		return
	}
	p.playMu.Lock()
	p.lastDispatchAt = time.Now()
	p.playMu.Unlock()
	// Smart Turn hygiene (defect 2026-11-13): the aggregated dispatch ENDS
	// the turn its parts described. Drop the analyzer's buffered audio +
	// sticky speech state NOW (turnMu only, never nested under playMu —
	// see the lock-order note in turn.go) so turn N+1's endpointing never
	// rides leftover audio from the aggregated parts.
	p.turnMu.Lock()
	if t := p.turn; t != nil {
		t.Clear()
	}
	p.turnMu.Unlock()
	p.logger.Info("voicechild: final prompt dispatched aggregated",
		"call_id", p.ccid, "parts", parts, "chars", len([]rune(text)), "reason", reason)
}

// noteMicEnergy stamps lastMicLoudAt when an incoming mic chunk is above
// the loose speaking floor (bargeInRms/4 — a floor WELL below the barge
// threshold, catching conversational-level speech, not just shouting).
// This is the active-mic hold evidence for finalConfirmFired (C1 point
// 4). Phone legs carry bargeInRms == 0 (the open frame sets it on the
// local leg only), so the floor never trips there and the confirm window
// dispatches on schedule — documented limitation of the hold lane.
func (p *pipeline) noteMicEnergy(chunk []byte) {
	floor := p.bargeInRms / 4
	if floor <= 0 {
		return
	}
	if !rmsChunkOver(chunk, floor) {
		return
	}
	now := time.Now()
	p.playMu.Lock()
	p.lastMicLoudAt = now
	p.playMu.Unlock()
}

// consumeTranscripts turns STT transcripts into C1 prompt frames: a final
// transcript is last:true (starts an employee turn), a partial is last:false
// (preview only), and empty-text silence results emit nothing (C5). A
// partial while playback is active trips the barge-in latch (leaf 06, C5
// order): the interrupt frame is emitted BEFORE the partial's prompt frame
// so playback stops before the new turn's say can start; the latch keeps
// it exactly-once per barge-in until a new say restarts playback. No lock
// is held across channel reads or socket writes (mutexio).
//
// Turn aggregation (voice-startguard C1): when the confirm window is on,
// FINAL transcripts are NOT dispatched inline — they accumulate into one
// pending buffer (see accumulateFinal) and ONE last:true prompt frame
// leaves when the window expires without a newer final or active mic.
// The per-final filters (non-speech, dedupe, blank) keep working per
// FINAL, before aggregation; the barge-in interrupt for a final that
// arrives during live playback still fires on ARRIVAL (the operator
// talking over the answer must stop it immediately — only the turn
// dispatch waits for the window). Partials are untouched previews.
// Transcript-stream EOF flushes the pending buffer exactly once.
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
			// Echo-provenance drop — BARGE-IN CONTRACT (2026-11-11):
			// defect 2 of the 2026-11-11 regression review — a whisper
			// final that fuzzy-matches the agent's own recent speech (the
			// echo window, fed by every say frame) is the TTS coming back
			// through speaker -> mic — NOT a caller utterance. Before
			// this drop existed, the self-answer loop dispatched a FULL
			// employee turn on every re-heard sentence (the lastFinalText
			// dedupe below is EXACT-match only: the next sentence, a
			// paraphrase, or another call's voice always dispatched), and
			// the dispatch watermark unmasked the loop by removing the
			// accidental fast cancels that used to kill it. Drop it here
			// at the single choke point, exactly like the non-speech path:
			// NO prompt frame, NO evidence stamp (lastFinalAt/Text), NO
			// watermark, NO C5 interrupt — an echo must neither start a
			// turn nor authorize/cancel anything.
			//
			// Known, ACCEPTED trade-off: a human quoting the agent
			// verbatim mid-reply can be dropped (short quotes under the
			// echoMinContentTokens provenance floor pass through
			// unaffected). The daemon roster leaf carries the rest of the
			// short-tail policy; this layer only needs to stop the
			// unbounded self-answer loop.
			p.playMu.Lock()
			echoDrop := p.echoWin.matches(trimmed)
			p.playMu.Unlock()
			if echoDrop {
				p.logger.Info("voicechild: echo final dropped",
					"call_id", p.ccid, "text", transcriptPreview(trimmed))
				continue
			}
			// Short-final gate (voice-microfrag, live call
			// local-0627960006a7f3d9), extended with the four
			// improvements of voice-peragent leaf 01 (exempt
			// phrases, reply window, energy, fragment decay —
			// see evaluateShortFinal). When voice.min_turn_words
			// > 0, a FINAL whose STRIPPED text (bracket/asterisk
			// artifacts removed — the same residue isRealSpeech
			// classifies on) carries fewer than the effective N
			// whitespace-separated words drops entirely here,
			// AFTER the non-speech filter and BEFORE evidence
			// stamping and aggregation: a dropped final is
			// invisible to lastFinalAt/lastFinalText AND to the
			// pending buffer, so it neither re-authorizes a stale
			// barge-in trip nor seeds an aggregation. N=0
			// (default) skips the gate AND every improvement —
			// byte-identical legacy behavior, the pin. Operator
			// commands like "Stop." survive via the exemption set.
			if verdict, words, reason := p.evaluateShortFinal(trimmed); verdict != gatePass {
				if verdict == gateDrop {
					p.logger.Info("voicechild: short final dropped",
						"call_id", p.ccid, "words", words,
						"reason", reason,
						"preview", transcriptPreview(trimmed))
					continue
				}
				// gateExempt: an improvement SAVED a would-be
				// word-count drop — the INFO line is the operator's
				// tuning telemetry (leaf 01: log what the exemptions
				// bought, reason=phrase|reply).
				p.logger.Info("voicechild: short final exempted",
					"call_id", p.ccid, "words", words,
					"reason", reason,
					"preview", transcriptPreview(trimmed))
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
			// Both filters run per-FINAL, before aggregation (C1): a
			// dropped part never enters the pending buffer.
			p.playMu.Lock()
			if trimmed == p.lastFinalText && !p.lastFinalAt.IsZero() &&
				time.Since(p.lastFinalAt) <= rmsSttEvidenceWindow {
				p.playMu.Unlock()
				continue
			}
			p.lastFinalAt = time.Now()
			p.lastFinalText = trimmed // dedupe compares RAW (leaf 03 T3: barge-in/dedupe behavior unchanged)
			speaking := p.speaking
			// interruptedLatched: the RMS VAD (or an earlier partial)
			// already tripped this barge-in — speaking dropped with the
			// trip, but the caller is clearly ON the floor. A final
			// landing in this state dispatches inline like a
			// speaking-final: aggregating it could let the barge-in's
			// clear (in flight from the daemon) drop the caller's
			// utterance before the window ever lands.
			interruptedLatched := p.interrupted
			p.playMu.Unlock()
			// C5 for the local leg's whisper adapter: whisper emits
			// FINALS only (no partials), and the RMS gate now suppresses
			// loudness-only trips. A final arriving while playback is
			// live must therefore carry the interrupt itself - stop the
			// old playback BEFORE the new prompt's reply can start, or
			// the old say plays over the new turn's answer. Same
			// exactly-once latch as the partial path. The interrupt
			// stays on ARRIVAL even with aggregation on: the operator
			// talking over the answer must stop it now; only the turn
			// DISPATCH waits for the confirm window.
			if speaking && p.noteBargeIn() {
				if err := p.child.emitInterrupt(p.ctx, p.ccid); err != nil {
					p.logger.Error("voicechild: emit interrupt", "err", err, "call_id", p.ccid)
				}
			}
			// Turn aggregation (voice-startguard C1): inside the confirm
			// window, finals append to one pending buffer and the timer
			// owns the dispatch; nothing emits here. Legacy mode
			// (window == 0) falls through to the per-final emit below,
			// byte-identical to pre-C1 behavior (emits tr.Text, not the
			// trim). A final that arrives while playback is LIVE is
			// dispatched immediately (merged with any pending buffer):
			// the operator talking over the answer must start a new
			// turn NOW, and waiting the window would let the daemon's
			// clear-for-barge-in (the reply to the interrupt we just
			// emitted) DROP the pending aggregation — losing the
			// caller's interrupting utterance entirely. Aggregating a
			// barge-in final into one merged dispatch keeps the fence
			// ("one sentence, one turn") and the barge-in contract
			// (C5 speaking-final path) intact.
			if p.confirmWindow() > 0 {
				// The speaking/mic-hold branch runs FIRST: a final that
				// lands while playback is live (or the barge-in latch is
				// tripped) rides the merged barge-in-final dispatch, with
				// its interrupt already emitted on arrival above. The
				// endpoint fast path must NOT bypass this branch (bughunt
				// 2026-11-19 H4 / 02-deepgram-endpoint T3 TestEndpointGuards):
				// an endpoint final during playback takes the SAME guarded
				// route as any speaking final — one merged dispatch, never
				// an early flush that races the daemon's clear-for-barge-in.
				if speaking || interruptedLatched {
					agg, parts := p.mergeAndTakeFinal(trimmed)
					p.dispatchAggregated(p.ctx, agg, parts, "barge-in-final")
				} else if tr.Endpoint && p.endpointEnabled() {
					// Authoritative endpoint (F4): when the vendor says the
					// utterance ended, flush ALL accumulated finals through
					// the SAME guarded choke point (dispatchAggregated) in ONE
					// prompt — never just the last response. The knob and the
					// flag both have to be on; with ms=0 (default) the flag is
					// inert and the confirm timer remains the only dispatcher
					// (byte-identical legacy behavior, R4). Echo/non-speech/
					// dedupe/short-final guards above already ran per final;
					// this branch is reached only when playback is idle and
					// the barge-in latch is untripped, so the mic-hold /
					// speaking semantics are respected.
					agg, parts, flushed := p.accumulateFinal(trimmed)
					if flushed {
						// 8 s age cap already retired the buffer; dispatch
						// its captured contents rather than a stale timer.
						p.dispatchAggregated(p.ctx, agg, parts, "aggregation-cap")
					} else {
						// All accumulated parts (this endpoint final
						// included), ONE aggregated prompt, off-lock.
						p.flushPendingFinal(p.ctx, "deepgram-endpoint")
					}
				} else if agg, parts, flushed := p.accumulateFinal(trimmed); flushed {
					// 8 s age cap: dispatch the aggregated buffer NOW
					// (already cleared) rather than waiting the window.
					p.dispatchAggregated(p.ctx, agg, parts, "aggregation-cap")
				}
				continue
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
		// Leaf 03 (T3, normalize wiring): ONLY the emitPrompt argument is
		// normalized — filler collapse + noise-token drop happen here so
		// the LLM turn receives the cleaned utterance. Everything else in
		// this loop stays RAW: the dedupe compare above (lastFinalText)
		// and the non-speech guard both ran on the raw text, and the raw
		// final rides the From field (below) so the daemon's binding
		// persists the caller's ACTUAL words (AppendTranscript keeps RAW;
		// eval needs the un-normalized transcript).
		//
		// The preserve hook is the OPEN-VOCABULARY default (leaf 03): the
		// child has no employee-context catalog, so every token the
		// recognizer produced is presumed meaningful (preserved) EXCEPT
		// the known filler set — normalization here collapses duplicate
		// words and filler runs, and drops only standalone fillers.
		// Callers with a real context catalog (leaf 05 intake) install a
		// narrower hook via SetNormalizePreserve.
		promptText := tr.Text
		rawText := ""
		if tr.Final && p.confirmWindow() > 0 {
			// Leaf 03 (T3) normalization is AGGREGATION-MODE ONLY: the
			// legacy per-final path (window == 0) must stay byte-identical
			// to pre-C1 behavior (ORIGINAL untrimmed text per the
			// backward-compat pin). In aggregation mode the LLM-facing
			// prompt is normalized here; the raw final rides From for the
			// daemon binding (AppendTranscript keeps RAW).
			promptText = NormalizeUtterance(tr.Text, openVocabularyPreserve)
			rawText = strings.TrimSpace(tr.Text)
			if promptText == "" {
				// Normalization collapsed the final to nothing (pure
				// filler) — an empty prompt would start a turn with an
				// empty user message. Drop here, AFTER the evidence
				// stamp: the caller did speak (RMS/barge-in semantics
				// above already ran on the raw text).
				p.logger.Info("voicechild: final normalized to empty; dropped",
					"call_id", p.ccid, "raw", transcriptPreview(rawText))
				continue
			}
		}
		// Dispatch watermark (defect 2026-11-12): stamped for a FINAL that was
		// ATTEMPTED, not only one that emitted successfully. The watermark's
		// meaning is "a turn started here": rmsBargeInCheck refuses to let
		// evidence that predates the current turn's dispatch authorize a trip
		// (see lastDispatchAt). An attempted final therefore has to move it —
		// a transient text-socket write error (2 s deadline) used to leave the
		// watermark zero, so EVERY later RMS trip hit
		// `p.lastDispatchAt.IsZero()` and was suppressed with "evidence
		// predates turn dispatch": one failed emit disabled the RMS barge-in
		// lane for the rest of the call. Exactly-once semantics are unchanged
		// (finals only, partials never stamp) and the error log below keeps its
		// severity.
		if tr.Final {
			p.playMu.Lock()
			p.lastDispatchAt = time.Now()
			p.playMu.Unlock()
			// Smart Turn hygiene (defect 2026-11-13): a dispatched final
			// ENDS the turn the buffered audio described. Drop the
			// analyzer's turn buffer + sticky SpeechTriggered state NOW,
			// under turnMu (never nested under playMu), so turn N+1's
			// endpointing verdict never rides turn N's leftover audio.
			p.turnMu.Lock()
			if t := p.turn; t != nil {
				t.Clear()
			}
			p.turnMu.Unlock()
		}
		if err := p.child.emitPrompt(p.ctx, p.ccid, promptText, tr.Final, rawText); err != nil {
			p.logger.Error("voicechild: emit prompt", "err", err, "call_id", p.ccid)
		}
	}
	// Transcript-stream EOF (C1 point 5): the provider's session ended
	// (whisper wss drop, pcmCh close from the pump) with text still
	// aggregated — dispatch it exactly once rather than swallowing the
	// caller's last utterance. The pipeline ctx may already be dead
	// (close() flushed + cancelled first): the emit fails with an
	// error log and the buffer is already cleared, so this stays
	// exactly-once and never re-arms anything.
	p.flushPendingFinal(p.ctx, "stream-eof")
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
			// Input-side ML speech discriminator (jargo Silero VAD, local
			// leg): advance the speaking state each chunk so
			// rmsBargeInCheck can require real SPEECH. No-op when p.vad
			// is nil (legacy gate). The model call is sub-ms on the pump
			// goroutine — never under playMu (mutexio).
			// (Restored 2026-11-12: a sibling merge removed the feed.)
			// Race fix (2026-11-13): the analyzer fields are playMu-
			// guarded; handleOpen/installVAD write them under playMu, so
			// the pump SNAPSHOTs the reference under playMu and then
			// releases before the model I/O — the bare reads used to race
			// the install. turnMu is taken alone (never nested under
			// playMu), per the lock-order notes in turn.go.
			p.playMu.Lock()
			vad := p.vad
			p.playMu.Unlock()
			sp := false
			if vad != nil {
				sp = vad.AnalyzeAudio(data)
				p.playMu.Lock()
				p.vadSpeaking = sp
				p.playMu.Unlock()
			}
			// Smart Turn feed (bughunt 2026-11-19 L9): the turn analyzer
			// is fed INDEPENDENTLY of the VAD install. When the VAD
			// factory failed but the turn factory succeeded, gating the
			// feed on vad != nil starved the analyzer — mode 2 then ran
			// AnalyzeEndOfTurn over an EMPTY buffer (jargo returns
			// Incomplete/nil-metrics for no audio) and deferred every
			// turn. Without a VAD there is no speech-probability source,
			// so feed the raw chunk with isSpeech=false: jargo's
			// smartTurnBase still buffers the audio (pre-speech trimming
			// bounds growth) and the model judges the buffered turn;
			// only the silence safety-net stays inert, which is the
			// documented degradation, never an unfed verdict. turnMu
			// alone, never under playMu.
			p.turnMu.Lock()
			if t := p.turn; t != nil {
				t.AppendAudio(data, sp)
			}
			p.turnMu.Unlock()
			// Turn aggregation C1 point 4: the LOOSER speaking-floor tap
			// (bargeInRms/4) stamps lastMicLoudAt so a confirm-window
			// expiry during a breath can tell "silence resumed" from "the
			// mic is still live". No-op on phone legs (bargeInRms == 0).
			p.noteMicEnergy(data)
			// Short-final energy gate (voice-peragent leaf 01): the
			// utterance-window mic energy the gate measures. No-op
			// unless the gate is armed AND energy_gate is on.
			p.noteUtteranceEnergy(data)
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
	// BARGE-IN CONTRACT (2026-11-11): record the agent's own speech into
	// the rolling echo window as each say frame enqueues. Say frames
	// arrive sentence-granular from the daemon talk loop, so the window
	// tracks exactly what the speakers are (about to be) emitting — the
	// text the mic's echo will fuzzy-match when whisper re-hears it. Pure
	// string bookkeeping, no I/O (playMu-safe).
	p.echoWin.add(frame.Text)
	p.sayQueue = append(p.sayQueue, queuedSay{frame: frame, gen: p.playGen})
	workerUp := p.playWorkerUp
	if !workerUp {
		p.playWorkerUp = true
	}
	p.playMu.Unlock()

	if !workerUp {
		go p.playbackWorker()
	} else {
		// OVERLAP POINT 2 (starve fix): this draft arrived while an
		// earlier utterance is still generating or parked between
		// frames. Start its synthesis NOW rather than waiting for N
		// to finish playing — otherwise a short opening sentence
		// drains the daemon pacer before the remainder's slow
		// voice-clone synthesis even starts (multi-second dead air).
		// Depth stays bounded and clear cancels every registered job,
		// so barge-in recallability is unchanged. Safe to call from
		// the frame handler: the hook is playMu-guarded and Speak
		// runs lock-free on its own goroutine.
		p.tryLaunchPrefetch()
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
	// clipKey is non-empty when the reader came from the filler-clip
	// store instead of a live Speak (leaf 03, adjudication A2) — drives
	// the "say played from clip" INFO line, nothing else. The playback
	// lane treats a clip reader exactly like a provider reader.
	clipKey string
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

// prefetchDepth is the bounded synth queue: at most TWO utterances may
// be synthesizing (or synthesized-but-unplayed) beyond the frame the
// worker is playing. Depth 1 left the second lookahead waiting for the
// first's Speak to RETURN before starting, so a long generation (qwen3:
// 0.7-5.5 s per sentence) still leaked into the next sentence break;
// depth 2 keeps one more utterance in flight. Memory stays ~two
// utterances of unplayed audio, and the reader-serialization invariant
// is untouched: only the single playback worker ever opens a reader,
// strictly after the previous reader closed.
const prefetchDepth = 2

// registerSynthLocked claims one playMu synth queue slot for one
// dequeued frame and returns the job (unstarted). Caller holds playMu,
// and the queue must have a free slot (len(p.synthJobs) <
// prefetchDepth — tryLaunchPrefetch and the worker check before
// calling). The job is APPENDED: the queue stays in dequeue order, so
// the head is always the next frame to play. p.ctx never changes after
// construction, so deriving the job's cancellable ctx under the lock is
// safe and keeps clear() (which reads p.synthJobs under the same lock)
// able to cancel a Speak that has launched by the time it fires.
func (p *pipeline) registerSynthLocked(item queuedSay) *synthJob {
	jobCtx, cancel := context.WithCancel(p.ctx)
	job := &synthJob{
		frame:  item.frame,
		gen:    item.gen,
		done:   make(chan synthResult, 1),
		ctx:    jobCtx,
		cancel: cancel,
	}
	p.synthJobs = append(p.synthJobs, job)
	return job
}

// runSynth starts the registered job's Speak on a dedicated goroutine
// (or resolves it synchronously when no provider is installed). Captures
// the provider the caller read under playMu — the same regime as every
// other p.tts read (audit fix 2). No lock is held across Speak (mutexio).
//
// Filler-clip branch (leaf 03, adjudication A2): when the frame carries
// clip_id AND the store has a valid entry, the stored PCM REPLACES the
// provider call entirely (zero Speak) and rides the unchanged playback
// lane — pacing, barge-in clear, played edge all inherit from the reader
// swap. A miss (or invalid key / open error / rate mismatch without a
// resampler) falls through to today's tts.Speak(frame.Text) path
// byte-identically (no opportunistic backfill — orchestrator decision).
// The store I/O runs OFF every lock (mutexio posture: the file read
// shares the synth goroutine's "no lock across I/O" rule).
func (p *pipeline) runSynth(tts TTSProvider, job *synthJob) {
	if job.frame.ClipID != "" {
		// File I/O on a goroutine, never on the caller (the prefetch
		// path reaches here from the frame-handler/serve goroutine):
		// a slow disk must not stall frame routing.
		go func() {
			if rc, key, ok := p.openClip(job.frame.ClipID); ok {
				job.done <- synthResult{pcm: rc, clipKey: key}
				return
			}
			p.speakFallback(tts, job)
		}()
		return
	}
	if tts == nil {
		job.done <- synthResult{err: errNoTTSProvider}
		return
	}
	go func() {
		p.speakFallback(tts, job)
	}()
}

// speakFallback runs today's live Speak path for a job (frame text,
// provider ctx stamped with the call id). Safe to call from any synth
// goroutine; a nil provider surfaces the named no-provider error the
// worker logs unchanged.
func (p *pipeline) speakFallback(tts TTSProvider, job *synthJob) {
	if tts == nil {
		job.done <- synthResult{err: errNoTTSProvider}
		return
	}
	start := time.Now()
	r, err := tts.Speak(WithTTSContext(job.ctx, p.ccid), job.frame.Text)
	job.done <- synthResult{pcm: r, err: err, synthDur: time.Since(start)}
}

// awaitSynth blocks for the job's Speak result and removes it from the
// playMu synth queue (the worker owns the reader from here; the job is
// the queue HEAD — the worker only ever awaits the head, so a retired
// Speak of a LATER slot never jumps the queue on the write path).
// ok=false means the pipeline died while Speak was running: a background
// drain still receives the result (exactly one receiver per job, always)
// and closes any late reader, so nothing leaks and nothing hangs.
func (p *pipeline) awaitSynth(job *synthJob) (synthResult, bool) {
	select {
	case res := <-job.done:
		p.unqueueSynth(job)
		return res, true
	case <-p.ctx.Done():
		p.unqueueSynth(job)
		go func() {
			res := <-job.done
			if res.pcm != nil {
				_ = res.pcm.Close() //nolint:mutexio // teardown drain, no lock held
			}
		}()
		return synthResult{err: p.ctx.Err()}, false
	}
}

// unqueueSynth removes the job from the playMu synth queue if still
// queued (the worker may have retired it first via the clear path).
func (p *pipeline) unqueueSynth(job *synthJob) {
	p.playMu.Lock()
	p.unqueueSynthLocked(job)
	p.playMu.Unlock()
}

// unqueueSynthLocked drops one job from the bounded synth queue.
// Caller holds playMu. The slice is rebuilt with a fresh backing array
// (three-index head slice) so the removed job never keeps its frame/
// reader slot alive through slice aliasing.
func (p *pipeline) unqueueSynthLocked(job *synthJob) {
	for i, j := range p.synthJobs {
		if j == job {
			p.synthJobs = append(p.synthJobs[:0:0], p.synthJobs[i+1:]...)
			return
		}
	}
}

// retireSynth drops a job the worker will never play (a prefetch killed
// by a clear or a torn-down ctx): take it out of the queue synchronously —
// the worker is the ONLY caller, and it never retires the same job twice —
// then cancel Speak at the transport and drain the result in the
// background, closing a reader that lands too late to play. The
// prefetched PCM NEVER reaches the socket. Safe on an already-delivered
// job (cancel is idempotent; done is buffered 1, so the drain receive
// never blocks on a Speak that already answered).
func (p *pipeline) retireSynth(job *synthJob) {
	job.cancel()
	p.unqueueSynth(job)
	go func() {
		res := <-job.done
		if res.pcm != nil {
			_ = res.pcm.Close() //nolint:mutexio // retire drain, no lock held
		}
	}()
}

// drainSynthQueue retires every job still in the synth queue (the worker
// is exiting on a torn-down ctx: late readers must be cancelled and
// closed, never written). The worker is the only caller.
func (p *pipeline) drainSynthQueue() {
	p.playMu.Lock()
	jobs := p.synthJobs
	p.synthJobs = nil
	p.playMu.Unlock()
	for _, job := range jobs {
		job.cancel()
		go func(j *synthJob) {
			res := <-j.done
			if res.pcm != nil {
				_ = res.pcm.Close() //nolint:mutexio // teardown drain, no lock held
			}
		}(job)
	}
}

// errNoTTSProvider surfaces the "provider not installed" drop through
// the job result so the worker logs the same error line the old inline
// check did.
var errNoTTSProvider = errors.New("voicechild: tts provider not installed; dropping say")

// tryLaunchPrefetch starts queued drafts' synthesis while a frame's
// audio is being (or is about to be) paced to the .out socket. This is
// the overlap: sentence N+1's Speak begins during sentence N's playback
// instead of after it has fully drained, so the provider's full-
// generation latency (qwen3 does not stream; 0.7-4.8 s) hides inside the
// daemon pacer's real-time drain of N. The draft STAYS-POP from sayQueue
// but keeps its gen stamp: a clear retires it exactly like an unpopped
// queued draft, and the prefetched reader is cancelled and closed,
// never written.
//
// Depth is bounded by prefetchDepth (TWO frames beyond the one playing):
// launches append to the synth queue in strict dequeue order, so the
// head stays the next frame to play and a slow Speak for N+1 never lets
// N+2 jump ahead on the write path. Memory stays ~two utterances.
//
// enqueueSay calls it for every new draft: synthesis of the next
// utterance starts the moment the frame arrives, even while the worker is
// between frames (synthesizing N head-of-burst, or parked after a prior
// turn). That is the starve fix — waiting for writingNow meant a short
// opening sentence finished playing (and the daemon pacer drained its
// ~1.28 s buffer) before the remainder's slow voice-clone synthesis even
// started, leaving multi-second dead air (local-611897f3). The worker's
// own write-start hook remains as a backstop for drafts that arrived
// before the worker existed. Depth is still bounded
// at prefetchDepth (TWO beyond the frame playing); a clear cancels every
// registered job (playGen gate) so recallability is unchanged.
func (p *pipeline) tryLaunchPrefetch() {
	p.playMu.Lock()
	if !p.playWorkerUp || p.ctx.Err() != nil {
		p.playMu.Unlock()
		return
	}
	// Depth is bounded strictly by prefetchDepth and the gen gate below.
	// (The old `fromEnqueue && !p.writingNow` early-return was the dead-air
	// source: it suppressed prefetch when the worker was synthesizing the
	// head or parked between frames — exactly when overlap is needed most.)
	// Fill the bounded synth queue from the FIFO head, current-gen
	// drafts only (a retired stamp means a clear raced the queue — the
	// draft drops at dequeue, same as the unpopped path).
	tts := p.tts
	for len(p.synthJobs) < prefetchDepth && len(p.sayQueue) > 0 && p.sayQueue[0].gen == p.playGen {
		item := p.sayQueue[0]
		p.sayQueue = p.sayQueue[1:]
		job := p.registerSynthLocked(item)
		p.playMu.Unlock()
		p.runSynth(tts, job)
		p.playMu.Lock()
	}
	p.playMu.Unlock()
}

// playbackWorker is the single drain loop for sayQueue (audit fix 1). It
// writes frames strictly one at a time, so audioOut.Write is only ever
// reached from one goroutine at a time per pipeline — PCM leaves in
// dequeue order, one writer. Synthesis is split off the write path: a
// frame's Speak ran on a dedicated goroutine while the PREVIOUS frame
// was still pacing (claimed from the synth queue HEAD at the top of the
// iteration), so the worker only blocks on generation when there was no
// playback to overlap it with (head of a burst). The worker exits when
// the queue empties with NO outstanding synth jobs (a queued prefetch is
// an unplayed frame — exiting would strand it) or the pipeline ctx
// dies; a concurrent enqueueSay re-launches it (playWorkerUp is released
// under playMu in the same critical section that observes an empty
// queue AND empty synth queue, so no frame is ever stranded).
func (p *pipeline) playbackWorker() {
	for {
		var item queuedSay
		var job *synthJob
		var tts TTSProvider
		p.playMu.Lock()
		if len(p.synthJobs) > 0 {
			// The synth queue HEAD is the next frame to play: its
			// synthesis is already running (prefetched during the
			// previous write, or registered below on the last
			// turn). The entry stays queued until awaitSynth
			// completes so a clear racing the remaining Speak
			// latency still cancels it. FIFO by construction:
			// launches only APPEND in dequeue order.
			job = p.synthJobs[0]
			item = queuedSay{frame: job.frame, gen: job.gen}
			retired := job.gen != p.playGen || p.ctx.Err() != nil
			p.playMu.Unlock()
			if retired {
				p.retireSynth(job)
				continue
			}
		} else {
			if len(p.sayQueue) == 0 || p.ctx.Err() != nil {
				if p.ctx.Err() != nil {
					// Torn-down pipeline: nothing is queued here,
					// but drain on the exit paths uniformly.
					p.playWorkerUp = false
					p.playMu.Unlock()
					p.drainSynthQueue()
					return
				}
				// Release the slot atomically with the emptiness
				// check: an enqueueSay after this point spawns a
				// fresh worker.
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
			p.drainSynthQueue() // late prefetched readers: cancel + close, never write
			return              // pipeline torn down mid-synth; awaitSynth drained its job
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
		if res.clipKey != "" {
			// Cached-playback telemetry (leaf 03 A5): the store served
			// this utterance's PCM — zero Speak ran. Logged on the
			// worker right before the stream enters pacing, so the line
			// ordering in the log matches the audible order.
			p.emitClipPlayed(res.clipKey)
		}
		drained := p.playbackStream(res.pcm, item.gen, res.synthDur)

		// End-of-turn AUDIO marker (C1 played edge): a last:true say
		// frame whose stream reached its natural EOF has now handed its
		// FINAL byte to the .out socket, and no later frame of this turn
		// can follow it (the turn-final flush is the draft's last say), so
		// THIS is the truth signal the daemon's drain watch arms from —
		// not the turn_final TEXT edge, where the child still has queued
		// sentences whose 1.4-3.8 s synth gaps fake a mid-reply quiescence
		// (link-2fdc3f5fd8d81805: 10.94 s both-audible overlap). Emitted
		// ONLY on a natural drain: a stream aborted by clear/barge-in, a
		// Speak error, or a socket write failure sends nothing (the barge-
		// in path releases the floor through the coordinator's idle edge;
		// a denied/aborted final stream rides the 12 s fallback margin —
		// late, never early, the same posture the old silent-stream guard
		// took). audioOut.Write is happens-before this frame on the same
		// goroutine, so the daemon can never see played before the last
		// PCM byte was handed off.
		if drained && item.frame.Last != nil && *item.frame.Last && p.child != nil {
			if perr := p.child.emitPlayed(p.ctx, p.ccid); perr != nil {
				p.logger.Warn("voicechild: emit played frame", "err", perr, "call_id", p.ccid)
			}
		}
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
//
// The bool reports whether the stream DRAINED NATURALLY: every byte the
// provider produced was handed to audioOut and the reader hit io.EOF.
// Only that shape may feed the played edge (see playbackWorker); a
// barge-in drop, a Speak read error, or a socket write failure returns
// false — none of them is "the turn finished speaking".
func (p *pipeline) playbackStream(pcm io.ReadCloser, gen uint64, synthDur time.Duration) bool {
	defer func() { _ = pcm.Close() }() //nolint:mutexio // deferred close, no lock held

	p.playMu.Lock()
	if p.playGen != gen {
		// A clear retired this stream's generation between synth and
		// here: it must not start. The stamped-gen check subsumes the
		// old dropQueued gate — a clear always bumps playGen, so
		// dropQueued is never true at a matching gen.
		p.playMu.Unlock()
		return false
	}
	// Fresh playback restarts the barge-in VAD latch (one interrupt per
	// barge-in, T5) — this inlined re-arm IS that path (the old
	// playbackRestarted() helper had no production caller and was
	// removed). The incoming-RMS window accumulators are part
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
	// Depth TWO: the current frame's entry left the queue at
	// awaitSynth, so this tops the bounded synth queue up to two
	// lookahead synths (N+1, N+2); nothing beyond prefetchDepth starts
	// until a slot frees.
	p.tryLaunchPrefetch()

	chunk, err := voiceframe.ChunkBytes(p.codec, p.sampleRt)
	if err != nil {
		p.logger.Error("voicechild: playback chunk size", "err", err, "call_id", p.ccid)
		return false
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
			return false // clear: drop queued audio, stop writes (or gen moved on)
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
				return false // clear arrived while reading: drop the chunk
			}
			if _, werr := p.audioOut.Write(buf[:n]); werr != nil {
				p.logger.Error("voicechild: audio out write", "err", werr, "call_id", p.ccid)
				return false
			}
			outWrite.Mark(time.Now())
			outWrite.AddBytes(int64(n))
			// Reply-window exemption (voice-peragent leaf 01): stamp
			// the child's own last spoken-audio activity. No-op unless
			// the short-final gate is armed with reply_exempt_s > 0.
			p.markSayActivity()
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
			if errors.Is(readErr, io.EOF) {
				// Natural drain: every byte the provider produced is now
				// on the socket (an EOF that carried a final partial read
				// wrote it above before this return).
				return true
			}
			p.logger.Error("voicechild: tts stream read", "err", readErr, "call_id", p.ccid)
			return false
		}
		select {
		case <-p.ctx.Done():
			return false
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
	// Turn aggregation (C1 point 5): a clear DROPS the pending
	// aggregated buffer — the documented choice. Old semantics: the
	// final had already dispatched a prompt and the barge-in cancel
	// killed that turn; an interrupted operator's partial utterance
	// rides the same cancel. An aggregated (undispatched) buffer at a
	// barge-in is exactly that partial utterance: dropping it keeps
	// "interrupted turn's text is cancelled" byte-faithful. A
	// speaking-final is NOT aggregated (consumeTranscripts dispatches
	// it inline), so a barge-in never loses its own triggering text.
	p.clearPendingFinalLocked()
	// Playback stopped with the clear: drop the stale `speaking` flag.
	// The aborted stream's deferred reset skips itself on the gen
	// mismatch (playbackStream), so without this the flag would stay true
	// across the interrupt and let noteBargeIn observe a speaking
	// pipeline that no longer exists (also makes a watcher that waits
	// for speaking==true synchronize with the next playback's latch
	// re-arm, which happens in the same critical section).
	p.speaking = false
	// Prefetch fix: every in-flight synthesis (the queued prefetches,
	// or the head-of-burst Speak still generating) belongs to the
	// retired generation — cancel their Speak ctxs so an interrupted
	// turn stops burning provider latency, and their readers — if any
	// still land — are closed by the worker's retire path, never
	// written. The queue itself stays intact: the worker drains it
	// iteration by iteration (each head fails the gen check and
	// retires), which preserves the single-owner receive discipline on
	// every job's done channel. Cancel OFF the lock (same posture as
	// the depth-1 code: no provider-side teardown inside playMu).
	jobs := p.synthJobs
	p.playMu.Unlock()
	for _, job := range jobs {
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

// rmsChunkSamples is the per-chunk sample count a FULL 20 ms chunk
// normally carries (L16 s16le: 640 bytes = 320 samples), the codec-derived
// count the pipeline was set up with. The sustained window is sized off
// this FIXED constant, never off the last chunk's byte length: short
// unix-socket reads (pumpAudioIn hands pumpAudioInCheck whatever a single
// read returned) must not shrink the bar — a window of ten half-read
// 1 ms slivers is not 200 ms of sustained loudness. Sizing off the
// constant makes the window a true duration regardless of read
// fragmentation; under-reading only means the window takes more reads
// to fill, never a premature trip.
const rmsChunkSamples = 320

// rmsEnergy reports an s16le mono PCM chunk's loudness as (sum of squared
// |sample|, sample count); a trailing odd byte is ignored. The magnitude
// is taken in int32 BEFORE squaring (|v| fits int32 even at full scale:
// -32768 -> 32768), so a negative sample's energy is its TRUE magnitude
// squared — never a two's-complement wrap artifact. (The pre-fix
// arithmetic wrapped -u mod 2^64 and squared that, making every non-silent
// chunk with a negative sample read astronomically loud: room tone at
// rms~40 fabricated a 7.0e11 sum against a 2.88e9 threshold.) This is the
// integer form of rms^2 * n used by the threshold compare in
// rmsChunkOver and the echo compare in rmsBargeInCheck, so neither needs a
// division or a float.
func rmsEnergy(b []byte) (uint64, uint64) {
	var sum uint64
	var n uint64
	for i := 0; i+1 < len(b); i += 2 {
		v := int16(binary.LittleEndian.Uint16(b[i:])) //nolint:gosec // G115: PCM s16le decode — byte-pair truncation is the codec contract
		x := int32(v)                                 //nolint:gosec // G115: int16 widened to int32 is lossless
		if x < 0 {
			x = -x // |sample| magnitude; -32768 -> 32768 fits int32
		}
		u := uint64(x) //nolint:gosec // G115: x >= 0 by the abs above, so the widening is lossless
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
// DISPATCH-WATERMARK rule (defect 2026-11-11, live call
// local-611f46b1c3fe5c5b): evidence older than the current turn's
// dispatch (lastDispatchAt, stamped by consumeTranscripts right after a
// final prompt emit succeeds) is the user's OWN prompt that STARTED this
// turn — never NEW speech. Without the watermark any echo/noise RMS
// window inside the 10 s tail after dispatch was auto-authorized by that
// prompt's own final and killed the reply mid-sentence (observed 2.9 s
// and 1.2 s into answers).
//
// BARGE-IN CONTRACT (2026-11-11): TWO authorization sources, no raw-
// energy lane: a trip needs (1) a post-dispatch final (the e0dbce12
// watermark, unchanged) OR (2) text provenance — the most recent final
// (even pre-dispatch) whose text does NOT fuzzy-match the echo window
// (echo_window.go), i.e. proof that the speech is not the agent's own
// voice. (2) restores the ~200 ms fast lane the absolute watermark froze
// at ~850-1000 ms; bare loudness without a recent non-echo final stays
// suppressed on both lanes. Genuine new caller speech re-stamps
// lastFinalAt via consumeTranscripts either way.
//
// The C5 speaking-final path (consumeTranscripts) carries a final's
// interrupt directly and is untouched — RMS remains only the
// early-warning lane; a suppressed trip never consumes the one-shot
// evidence.
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
	// The bar is FIXED at the codec-derived full-chunk sample count
	// (rmsWindowChunks * rmsChunkSamples), never the LAST chunk's
	// length: short unix-socket reads must not shrink the sustained
	// window (a 10-sliver window is not 200 ms of loudness).
	if p.rmsRun < rmsWindowChunks*rmsChunkSamples {
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
	// Whisper early-flush kick (BARGE-IN CONTRACT (2026-11-11): trip
	// attempt in progress — this window cleared energy + echo-guard, the
	// text-evidence gates below decide). A STT adapter that can force-
	// finalize its open voiced window (WhisperSTT.FlushNow) gets kicked
	// here so the C5 speaking-final path — the primary barge-in lane —
	// lands ~350 ms (one cold subprocess + up to 500 ms trailing
	// silence) sooner, whether or not this RMS trip itself fires. The
	// adapter's contract: FlushNow never fires recognition on an empty
	// window, so a kick during pure echo/noise costs nothing. Provider
	// field read under playMu (audit fix 2 regime), the seam call itself
	// OFF the lock (mutexio posture, same as the interrupt emit below);
	// a non-blocking, coalescing send either way. Re-check the latch
	// state after re-locking: if playback died during the kick the trip
	// is moot (evidence untouched, one-shot intact).
	flusher, _ := p.stt.(sttFlusher)
	if flusher != nil {
		p.playMu.Unlock()
		flusher.FlushNow()
		p.playMu.Lock()
		if !p.speaking || p.interrupted || p.dropQueued {
			p.playMu.Unlock()
			return
		}
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
	// Echo-provenance gate (BARGE-IN CONTRACT (2026-11-11)): the recent
	// final above is the trip's only speech proof. If its text
	// fuzzy-matches the agent's own recent speech (the echo window, see
	// echo_window.go), it is the AGENT'S VOICE coming back through the
	// speakers — never caller speech — and must not authorize a trip on
	// EITHER lane (post- or pre-dispatch). This is what makes the
	// pre-dispatch fast lane below safe to re-open: without it, the
	// self-answer echo finals would re-authorize the very cancellations
	// the watermark exists to stop.
	if p.echoWin.matches(p.lastFinalText) {
		p.logger.Debug("voicechild: rms barge-in suppressed (evidence is agent echo)",
			"call_id", p.ccid)
		p.playMu.Unlock()
		return
	}
	// Dispatch watermark (defect 2026-11-11, live call
	// local-611f46b1c3fe5c5b) and the RESTORED FAST LANE (BARGE-IN
	// CONTRACT (2026-11-11)). e0dbce12 made the recent final above also
	// POST-DATE the current turn's dispatch: evidence older than
	// lastDispatchAt is the user's OWN prompt that STARTED this turn.
	// That closed the hole where any echo/noise window in the 10 s tail
	// after dispatch was auto-authorized and killed the reply mid-
	// sentence (observed 2.9 s / 1.2 s into answers) — but it also froze
	// the real barge-in detection floor at ~850-1000 ms, because a
	// post-dispatch final ALWAYS carries its own C5 interrupt (the
	// consumeTranscripts speaking-final path), so the energy lane could
	// never again beat the whisper flush (500 ms trailing silence + a
	// ~350 ms cold subprocess per segment).
	//
	// Two authorization sources now exist, per the contract: (1) the
	// e0dbce12 watermark — evidence post-dating the dispatch is new
	// speech (unchanged path: fall through to the latch below); (2) the
	// echo-window rejection — a PRE-dispatch (or zero-watermark) final
	// whose text is PROVEN not to be agent speech. Case (2) is the fast
	// lane: the prompt was spoken by a HUMAN on this call; combined with
	// sustained energy that independently cleared the 130% speaker-
	// reference echo guard, it is legitimate barge-in evidence — the
	// human is still talking / restarting over the answer (~200 ms trips
	// restored, no raw-energy lane returns: bare loudness without a
	// non-echo final is still suppressed above).
	//
	// RESIDUAL RISK, stated honestly: this re-opens part of the
	// e0dbce12 hole. The hole was "ANY noise within 10 s of the prompt
	// got authorized by the prompt final". Now the prompt final counts
	// only when the energy ALSO independently passes the echo guard
	// (130% ratio against the last outbound chunk) AND the prompt is
	// recent (the existing 10 s window) AND a later echo final has not
	// overwritten the evidence (the provenance gate above). Loud NON-
	// speech that clears the energy guard inside that window can still
	// cancel the reply — the remaining mitigation is the one-shot
	// evidence consume.
	if p.lastDispatchAt.IsZero() || !p.lastFinalAt.After(p.lastDispatchAt) {
		// The fast lane needs USABLE text evidence: a final with no
		// recorded text (or a pre-first-dispatch call where the only
		// stamp came without one) proves nothing about provenance, so
		// the watermark stays absolute and the trip is suppressed with
		// the original e0dbce12 reason (the pin in
		// TestRMSTripZeroWatermarkSuppresses).
		if strings.TrimSpace(p.lastFinalText) == "" {
			p.logger.Debug("voicechild: rms barge-in suppressed (evidence predates turn dispatch)",
				"call_id", p.ccid,
				"since_dispatch_ms", func() int64 {
					if p.lastDispatchAt.IsZero() {
						return -1
					}
					return time.Since(p.lastDispatchAt).Milliseconds()
				}())
			p.playMu.Unlock()
			return
		}
		p.logger.Debug("voicechild: rms barge-in fast lane (pre-dispatch final, provenance-verified non-echo)",
			"call_id", p.ccid,
			"since_dispatch_ms", func() int64 {
				if p.lastDispatchAt.IsZero() {
					return -1
				}
				return time.Since(p.lastDispatchAt).Milliseconds()
			}())
	}
	// ML speech gate (jargo Silero VAD, local leg): when an analyzer is
	// installed, the trip must ALSO be real SPEECH — not just loud sound
	// that cleared RMS + echo-guard + evidence (background noise, a door
	// slam, the agent's own TTS bleed that STT mis-heard). vadSpeaking is
	// the smoothed speaking state the audio pump writes from the same
	// chunk; a window that is loud but the model rates not-speech is
	// suppressed (latch + playback untouched), so a false trip can never
	// cancel the answer. When no analyzer is present (lib absent / phone
	// leg) this is a no-op and the legacy gate is byte-identical.
	// (Restored 2026-11-12: a sibling merge dropped the gate.)
	if p.vad != nil && !p.vadSpeaking {
		p.logger.Debug("voicechild: rms barge-in suppressed (silero vad not speech)",
			"call_id", p.ccid)
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
//
// Teardown escape (defect 2026-11-13): the done handshake is closed by
// the emit goroutine, whose socket write carries only a 2 s deadline — a
// consumer parked here during pipeline teardown must not ride that worst
// case out. p.ctx is cancelled by close() BEFORE the sockets drop, so the
// select releases as soon as the pipeline is dying; the pendingInterrupt
// field itself is cleared under playMu inside close() so later callers
// see the no-op path.
func (p *pipeline) awaitPendingInterrupt() {
	p.playMu.Lock()
	done := p.pendingInterrupt
	p.playMu.Unlock()
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-p.ctx.Done():
	}
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

// close tears the pipeline down: cancels its context, closes both audio
// sockets, and waits (bounded) for the transcript consumer to exit - the
// pump dies on the socket close/ctx cancel and closes pcmCh, Stream's
// session ends, and the consumer drains out (T4: no goroutine leak). A
// retry chain that never opened a session exits on the ctx cancel and
// closes trStart through the same once, so teardown never waits on a
// consumer that never ran. Idempotent.
func (p *pipeline) close(ctx context.Context) {
	// Hangup flush (C1 point 5): dispatch the pending aggregated text
	// while the ctx and the text socket are still live — BEFORE the
	// cancel below, which would fail the emit. EOF of the transcript
	// stream takes the same path from inside consumeTranscripts; the
	// clear-pending on the flush makes it idempotent (a racing
	// confirm-timer sees an empty buffer and returns).
	p.flushPendingFinal(p.ctx, "hangup")
	p.cancel()
	// Clip render re-kick (bughunt L1): a busy-retire timer armed before
	// teardown must not relaunch a worker into a dead pipeline. Stop it
	// under clipMu; the fire's ctx check is the race backstop.
	p.clipMu.Lock()
	p.stopClipRekickLocked()
	p.clipMu.Unlock()
	// Analyzer-init barrier: wait for any async ONNX factory that started
	// in handleOpen to finish installing or closing itself. This MUST run
	// after cancel (so a late result sees p.ctx.Err() and closes itself)
	// and before the field take below (so a session installed just before
	// cancel is still closed here). No ONNX session leaks on fast teardown.
	p.analyzerInit.Wait()
	// Teardown escape (defect 2026-11-13): retire any in-flight RMS-trip
	// handshake NOW — a consumer already parked in awaitPendingInterrupt
	// releases via the ctx cancel above (the select arms the escape), and
	// a later caller sees the nil no-op path instead of waiting on a done
	// whose emit goroutine died with the sockets.
	p.playMu.Lock()
	p.pendingInterrupt = nil
	p.playMu.Unlock()
	_ = ctx // kept for future graceful-drain variants
	_ = p.audioIn.Close()
	_ = p.audioOut.Close()
	// Release the input-side analyzers' ONNX sessions (the VAD's Silero +
	// the turn's Smart Turn). take/release their locks to own the
	// read-clear; Close runs off-lock (restored 2026-11-12: a sibling
	// merge had dropped the release, leaking the ONNX session per call).
	p.playMu.Lock()
	vad := p.vad
	p.vad = nil
	p.vadSpeaking = false
	p.playMu.Unlock()
	if vad != nil {
		if err := vad.Close(); err != nil {
			p.logger.Warn("voicechild: silero vad close", "err", err, "call_id", p.ccid)
		}
	}
	p.turnMu.Lock()
	turn := p.turn
	p.turn = nil
	if turn != nil {
		// Smart Turn hygiene (defect 2026-11-13): discard the buffered
		// turn audio + sticky speech state under turnMu BEFORE releasing
		// the session, so teardown mirrors the dispatch-path Clear and a
		// fake analyzer observes exactly-once-per-call reset semantics.
		turn.Clear()
	}
	p.turnMu.Unlock()
	if turn != nil {
		if err := turn.Close(); err != nil {
			p.logger.Warn("voicechild: smart turn close", "err", err, "call_id", p.ccid)
		}
	}
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
