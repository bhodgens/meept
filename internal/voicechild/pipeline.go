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

	// minTurnWords (playMu, keys-frame guarded, like turnConfirm): the
	// voice-microfrag short-final gate. A FINAL whose stripped text has
	// FEWER than this many real words drops before evidence stamping and
	// aggregation — tuned for loud rooms where TV fragments ("The other",
	// "This is...") dispatch turns seconds apart (aggregation cannot
	// merge arrivals 3-8 s apart, and word filters are wrong by default:
	// an operator's "Stop." is one word). 0 = disabled (the default;
	// byte-identical legacy behavior — opt-in defaults rule).
	minTurnWords int

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
	turnConfirm       time.Duration // confirm window; 0 = legacy per-final dispatch (backward-compat)
	pendingFinal      string        // aggregated trimmed final texts, single-space joined
	pendingFinalParts int           // how many finals are in pendingFinal (telemetry)
	pendingFirstAt    time.Time     // arrival of the FIRST pending part (8 s cap reference)
	pendingFinalTimer *time.Timer   // armed/re-armed per arriving final; stopped on dispatch/clear
	pendingFinalSeq   uint64        // timer identity: bumped on every arm AND every retire, so a stale closure that fired just before its Stop sees a mismatch and returns (never dispatches a fresh window early)
	micHoldRearms     int           // bounded mic-hold re-arms for the current pending (maxMicHoldRearms)
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
	// cleared on its exit). It gates the enqueueSay prefetch hook: a say
	// that arrives while sentence N's audio is actively pacing may start
	// N+1's synthesis immediately; one that arrives while the worker is
	// between frames (synthesizing N, or parked) stays queued and gets
	// its prefetch from the worker's own write-start hook instead —
	// keeping synth depth at ONE frame past the current write.
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
	agg, parts := p.pendingFinal, p.pendingFinalParts
	p.clearPendingFinalLocked()
	p.playMu.Unlock()
	p.dispatchAggregated(p.ctx, agg, parts, "confirm-window")
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
			// Short-final gate (voice-microfrag, live call
			// local-0627960006a7f3d9): in a loud room the TV's 2-word
			// tails ("The other", "This is...") arrive seconds apart —
			// aggregation cannot merge them and each starts an employee
			// reply. When voice.min_turn_words > 0, a FINAL whose
			// STRIPPED text (bracket/asterisk artifacts removed — the
			// same residue isRealSpeech classifies on) carries fewer
			// than N whitespace-separated words drops entirely here,
			// AFTER the non-speech filter and BEFORE evidence stamping
			// and aggregation: a dropped final is invisible to
			// lastFinalAt/lastFinalText AND to the pending buffer, so it
			// neither re-authorizes a stale barge-in trip nor seeds an
			// aggregation. N=0 (default) skips the gate — byte-identical
			// legacy behavior. Operator commands like "Stop." survive
			// only at N<=1; the threshold is operator-tunable by design.
			p.playMu.Lock()
			minN := p.minTurnWords
			p.playMu.Unlock()
			if minN > 0 {
				if words := wordCount(stripNonSpeechTokens(trimmed)); words < minN {
					p.logger.Info("voicechild: short final dropped",
						"call_id", p.ccid, "words", words, "preview", transcriptPreview(trimmed))
					continue
				}
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
				if speaking || interruptedLatched {
					agg, parts := p.mergeAndTakeFinal(trimmed)
					p.dispatchAggregated(p.ctx, agg, parts, "barge-in-final")
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
		if err := p.child.emitPrompt(p.ctx, p.ccid, promptText, tr.Final, rawText); err != nil {
			p.logger.Error("voicechild: emit prompt", "err", err, "call_id", p.ccid)
		} else if tr.Final {
			// Dispatch watermark (defect 2026-11-11): a successfully
			// dispatched FINAL prompt STARTS the in-flight turn, so
			// the turn's dispatch epoch is stamped right after the
			// emit. rmsBargeInCheck then refuses to let evidence that
			// predates this watermark authorize a trip: it is the
			// prompt that started this turn, never new speech (see
			// lastDispatchAt).
			p.playMu.Lock()
			p.lastDispatchAt = time.Now()
			p.playMu.Unlock()
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
			// Turn aggregation C1 point 4: the LOOSER speaking-floor tap
			// (bargeInRms/4) stamps lastMicLoudAt so a confirm-window
			// expiry during a breath can tell "silence resumed" from "the
			// mic is still live". No-op on phone legs (bargeInRms == 0).
			p.noteMicEnergy(data)
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
		// fully drain; the bounded synth queue keeps depth at TWO. The
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
	if !p.playWorkerUp || p.ctx.Err() != nil {
		p.playMu.Unlock()
		return
	}
	if fromEnqueue && !p.writingNow {
		p.playMu.Unlock()
		return
	}
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
	p.tryLaunchPrefetch(false)

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
// DISPATCH-WATERMARK rule (defect 2026-11-11, live call
// local-611f46b1c3fe5c5b): the evidence must also POST-DATE the current
// turn's dispatch (lastDispatchAt, stamped by consumeTranscripts right
// after a final prompt emit succeeds). Evidence older than the dispatch
// is the user's OWN prompt that STARTED this turn, never new speech —
// without the watermark any echo/noise RMS window inside the 10 s tail
// after dispatch was auto-authorized by that prompt's own final and
// killed the reply mid-sentence (observed 2.9 s and 1.2 s into answers).
// Genuine new caller speech re-stamps lastFinalAt via consumeTranscripts
// and re-earns the trip. A zero (or pre-first-dispatch) watermark
// suppresses too: no turn has been dispatched, so there is no
// evidence-backed proof a new utterance is talking over a live answer.
// The C5 speaking-final path (consumeTranscripts) carries a final's
// interrupt directly and is untouched — RMS remains only the
// early-warning lane, which must never self-cancel.
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
	// Dispatch watermark (defect 2026-11-11, live call
	// local-611f46b1c3fe5c5b): the recent final above must also POST-DATE
	// the current turn's dispatch. Evidence older than lastDispatchAt is
	// the user's OWN prompt that STARTED this turn — never new speech —
	// so an echo/noise window in the first 10 s of a fast answer was
	// auto-authorized by that prompt's own final and cancelled the reply
	// mid-sentence. A zero watermark (no final has successfully dispatched
	// yet this call) suppresses too: RMS is only the early-warning lane,
	// and the C5 speaking-final path handles a real final's interrupt
	// directly. Genuine new caller speech re-stamps lastFinalAt via
	// consumeTranscripts, lands after the watermark, and re-earns the
	// trip. Same suppression shape as the window gate above: latch and
	// playback untouched, the window accumulators were already reset.
	if p.lastDispatchAt.IsZero() || !p.lastFinalAt.After(p.lastDispatchAt) {
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
