# Ingress Speech Detection: VAD, Endpointing, and Barge-In in Established Stacks

Research date: 2026-11-11. Method: web research against primary sources (whisper.cpp /
Silero / faster-whisper / WebRTC source, Asterisk & FreeSWITCH docs+source, LiveKit /
Pipecat / OpenAI Realtime docs, double-talk-detection literature) plus a source-verified
look at meept's own local-leg contract (`internal/voice/AGENTS.md`) and the pinned
jargo module in the Go module cache. Read-only: no code changes. Report-only: no
install recommendations.

Convention note: `docs/research/` has no README/index file. The CI-gated catalog in
this dir is `harness-techniques.json` + `harness-techniques.md` (verified by
`scripts/research-harness-lit.py`, `make research-harness-check`) and is scoped to
harness techniques only — this doc is outside that catalog and needs no entry there.
If a future index is added, this file belongs under a "voice/audio" heading.

## Scope

meept's local voice leg: client mic → ffmpeg avfoundation raw s16le@16k → unix socket →
voice child → whisper.cpp (**FINALS ONLY**, no partials) for STT; barge-in is a
hand-rolled RMS gate — 200 ms sustained mic RMS > `voice.local.barge_in_rms` (3000),
echo-guard requiring window-average energy > 130% of the last outbound chunk's average,
plus a 10 s "STT evidence" window on recent whisper finals that must post-date the turn
dispatch. Observed live defects: (a) self-echo/noise killing replies, (b) dead-air
between sentence finals, (c) slow endpointing.

---

## 1. Findings per system

### 1.1 WebRTC APM — what runs BEFORE any endpointing decision

The WebRTC Audio Processing Module applies a **fixed-order chain** on the capture path:
high-pass filter → acoustic echo cancellation (AEC3) → noise suppression (NS) →
automatic gain control (AGC2). Each stage assumes the previous one already ran;
reordering breaks their internal assumptions (forasoft pipeline write-up; confirmed by
the order in `audio_processing_impl.cc`, which shows echo_controller processing then NS
`Process()` then gain control on the capture buffer).

- **AEC3** subtracts a modeled copy of the *known* render/playback signal (the
  "reference") from the mic signal using an adaptive filter + delay estimation. It is
  fed by the outbound path — you must route your own TTS audio back in as reference.
  Source: forasoft WebRTC pipeline article; WebRTC APM docs.
- **Double-talk** (both parties speaking) is AEC's hard problem: the canceller must
  remove the echo of the far end without chopping the near end's words. Weak
  cancellers "chop your words when the far end is also speaking" — the #1 reported
  artifact, worse on speakerphones/Bluetooth (forasoft article).
- **NS** has levels mapping to 6/12/18/21 dB target suppression (visible in
  `audio_processing_impl.cc`'s `map_level`). **AGC2** normalizes level with a limiter
  and internally uses an **RNN VAD (GRU-based)** to distinguish speech from noise
  (DeepWiki APM pages). Key takeaway: even the *gain* stage gates on a learned VAD,
  and a dedicated `echo_detector` analyzes capture audio to detect echo return loss.
- Practical echo-aware pipeline caution (RoomKit docs, measured on WebRTC AEC3):
  bypassing/pausing the canceller between responses made it resume with capture and
  render timelines out of step, and "the echo it left reached the provider's server VAD
  as the user speaking" (residual echo 0–5 dB below the mic vs 34–44 dB kept running).
  Corollary for meept: **if you can't cancel echo, you must at least never pause the
  echo reference bookkeeping** — the residual must be measured against a continuous
  playback-energy history, not a single last chunk.

### 1.2 whisper.cpp — built-in VAD and streaming parameters (verified defaults)

**CLI VAD** (`examples/cli/cli.cpp`, `whisper_params` — verified in source):

| Param | Default | Meaning |
|---|---|---|
| `--vad` | `false` | opt-in; uses a Silero ONNX model |
| `vad_threshold` | `0.5` | frame speech probability cutoff |
| `vad_min_speech_duration_ms` | `250` | shorter segments discarded (noise filter) |
| `vad_min_silence_duration_ms` | `100` | silence required to END a speech segment |
| `vad_max_speech_duration_s` | `FLT_MAX` | force-split cap (splits at last silence >98 ms) |
| `vad_speech_pad_ms` | `30` | pad each side of a segment (edge clipping insurance) |
| `vad_samples_overlap` | `0.1` | overlap carried between segment windows |
| `no_speech_thold` | `0.6` | segment-level `no_speech_prob` above which output is rejected |
| `entropy_thold` / `logprob_thold` | `2.40` / `-1.00` | decoder-fail heuristics |

**Streaming example** (`examples/stream/stream.cpp`, verified in source): sliding-window
mode defaults `step_ms=3000`, `length_ms=10000`, `keep_ms=200` (200 ms of the previous
window carried into the next inference to mitigate word-boundary splits); with
`--keep-context` the previous segment's tokens are fed as the next prompt. VAD-triggered
mode (`step_ms<=0`) polls a 2 s buffer every 2 s with the legacy energy-based
`vad_simple()` (`vad_thold=0.6`, `freq_thold=100`). There is **no trailing-silence
flush timer** in stream.cpp — that pattern (send the buffer when a ~500 ms silence gap
is found looking back from the newest sample) is the community workaround documented in
whisper.cpp discussion #211. This is exactly meept's "dead-air between sentence finals"
problem: whisper's windowed decoder emits a final at segment boundaries, and nothing in
the stock examples forces a flush at VAD offset.

**faster-whisper** (`faster_whisper/vad.py`, verified): same Silero decision logic but
deliberately **conservative defaults** tuned for Whisper's training distribution:
`threshold=0.5`, `neg_threshold = max(threshold-0.15, 0.01)`,
`min_silence_duration_ms=2000`, `speech_pad_ms=400` (current master; the author
explains in issue #477 that Silero's tight defaults "compress" pauses that Whisper was
not trained on, and abrupt speech edges hurt accuracy/hallucination — hence larger pads
and ≥2 s silence removal only). A maintainer-adjacent user report on non-English audio
(issue #925): pad 1000 ms / threshold 0.4 to stop clipped syllables. Takeaway:
**segmenter VAD (what to send to STT) and endpointing VAD (when the user is done) want
different time constants** — 2000 ms min-silence is for not mangling transcripts, not
for turn timing.

### 1.3 Silero VAD — the de-facto standard ML VAD (verified defaults)

Upstream `get_speech_timestamps` (utils_vad.py, verified via silero-vad discussion #562
and the ONNX example): `threshold=0.5`, `neg_threshold=threshold-0.15` (a **dual
threshold / hysteresis**: enter speech above `threshold`, stay in speech until prob
drops below `neg_threshold`), `min_speech_duration_ms=250`, `min_silence_duration_ms=100`
(silence hangover before declaring offset), `speech_pad_ms=30`, fixed 512-sample window
(32 ms @ 16 kHz). whisper.cpp's port additionally merges gaps shorter than 0.2 s (hardcoded, not configurable)
(praat manual describing the whisper.cpp implementation).

Boundary-latency reality (silero-vad issue #798, measured on 8,966 files): at
`speech_pad_ms=0` the raw onset lags the objective acoustic onset by P50 ≈ +39–43 ms
(tail to >1 s on soft initial syllables), offset lags P50 ≈ +47–58 ms; the maintainers
confirm "the VAD needs at least 1–1.5 frames to trigger, 1.5–2 to detrigger" and
recommend asymmetric padding (currently symmetric). Design consequence: any
onset-confirmed gate inherits ~32–64 ms model lag plus the start-confirmation window;
budget for it, and pad the *onset* side more than the offset side when feeding STT.

**Pipecat's SileroVADAnalyzer** exposes exactly the four-knob shape the industry settled
on (docs "Speech Input & Turn Detection", verified): `confidence` (model probability;
their example config uses 0.7), `start_secs` (sustained speech before SPEECH-START,
0.2), `stop_secs` (sustained silence before SPEECH-END, 0.2–0.8 depending on stack),
`min_volume` (energy floor gate, 0.6) — an **ML VAD ANDed with a volume floor**, so a
confident guess about barely-audible audio never opens a turn. Phone-profile variants
(OsmTalk agent config doc): `vadConfidence` 0.7 (web) / 0.75 (phone), `min_volume` 0.6
/0.7 — telephony raises both.

### 1.4 Asterisk — server-side endpointing policy (verified from docs + source)

Asterisk's DSP layer is pure **energy + duration**, and its knobs encode decades of
field experience:

- `TALK_DETECT(set)=<dsp_silence_threshold>,<dsp_talking_threshold>`
  (`func_talkdetect.c` / docs): talking = average magnitude per sample in a frame ≥
  `dsp_talking_threshold` (**default 256**, valid 1–32768, overridable globally via
  `dsp.conf` `silencethreshold`); stopped-talking = `dsp_silence_threshold` ms of
  sub-threshold sound (**default 2500 ms**), with explicit docs guidance: "set it
  slightly above the maximum amount of ms of silence a user may generate during natural
  speech" — i.e., a deliberate silence **hangover** well above mid-sentence pauses.
- Ordering doctrine: audiohooks that *modify* media (`DENOISE(rx)`, `AGC(rx)`,
  `JITTERBUFFER`) must be placed **before** `TALK_DETECT` "as this will yield better
  results" — the same "clean before detect" chain ordering as WebRTC.
- `WaitForSilence(silencerequired[,iterations[,timeout]])`: default 1000 ms of silence;
  `iterations` ≥2 for answering-machine detection (a single gap is not trusted).
- `BackgroundDetect(filename,[sil,[min,[max,[analysistime]]]])`: **interruption
  policy** during playback — a non-silence period longer than `min` ms (default 100)
  but shorter than `max`, *followed by* at least `sil` ms (default 1000) of silence,
  aborts playback. Note the shape: onset persistence AND a trailing-silence
  confirmation, not a raw instantaneous trip.
- `AMD` (answering-machine detection) exposes `minimumWordLength`,
  `betweenWordsSilence`, `maximumWordLength`, `silenceThreshold` — word-granularity
  speech/noise segmentation on top of the same energy DSP.
- Field failure mode (serverfault thread): on noisy lines `WaitForSilence` **never**
  detects silence because the line always has energy above threshold — the fix is
  raising/retuning `silencethreshold`. This is precisely why a fixed absolute RMS
  (meept's 3000) breaks on a different mic/room.

### 1.5 FreeSWITCH — threshold + hits counters and switch_vad

`mod_dptools` (verified in module source): `detect_silence <thresh> <silence_hits>
<timeout_ms>`, `detect_audio <thresh> <audio_hits> <timeout_ms>`, `wait_for_silence
<thresh> <silence_hits> <listen_hits> <timeout_ms>` — the same energy-threshold-plus-N-
consecutive-20 ms-frame "hits" model (a `hits` count IS the hangover/persistence
window), and a freeswitch-users thread shows a USER's `voicemail.conf.xml`
edit setting `record-silence-threshold` 200 with `record-silence-hits` 10 —
those two numbers are that operator's tuned values, NOT a shipped FreeSWITCH
default. Session-level VAD is driven by
channel variables `vad_thresh`, `vad_silence_ms`, `vad_voice_ms` feeding
`switch_vad_set_param`, with `vad=smart` selecting the energy+spectral ("smart") mode
over plain energy. For barge-in during playback, `break`-flagged plays (`-break` arg /
`playback_break` on DTMF) plus `flush_dtmf` before listening show the in-band-signalling
route to interruption; telephony stacks pair in-band DTMF ("press to interrupt") with
acoustic detection as an escape hatch. Community voice-bot practice (huggingface
speech-to-speech repo, verified in `vad_arguments.py`): Silero `thresh=0.6`,
`min_silence_ms=64`, `min_speech_ms=384`, **`min_speech_continuation_ms=192` as an
explicit hysteresis** (cheaper to accept speech that *continues* an open turn than to
open a new one), a `speech_pad_ms=500` pre-roll buffer, and a **speculative reopen
window** (`speculative_reopen_ms=800`, `unanswered_reopen_ms=7000`): a soft-ended turn
stays reopenable — resumed speech reopens the same turn instead of starting a new one.
That is a direct pattern for meept's dead-air-between-finals problem.

### 1.6 Production voice-agent frameworks — how "the user is interrupting the bot" is decided

Common decomposition (LiveKit blog "Turn detection and interruption handling"): three
orthogonal questions — *is there speech now* (VAD), *is the user done* (turn detection
+ endpointing delays), *is the user interrupting* (interruption policy) — each
separately configurable.

- **OpenAI Realtime `server_vad`** (verified in API reference): `threshold` 0.5
  (activation, 0–1, "higher requires louder audio"), `prefix_padding_ms` **300** (audio
  kept before VAD onset), `silence_duration_ms` **500** (trailing silence to close a
  turn), `interrupt_response` default true (barge-in on VAD *start* event), plus
  `semantic_vad` with `eagerness` low/medium/high mapping to **8 s/4 s/2 s max timeouts**
  — when the utterance trails off ("uhhm…"), the model *extends* the silence wait
  instead of closing the turn. `idle_timeout_ms` (5–30 s) exists specifically for phone
  dead air. LiveKit's realtime plugin overrides with `prefix_padding_ms=600,
  silence_duration_ms=350` — these are tunables, not magic.
- **LiveKit Agents** (docs, verified): `endpointing.min_delay`/`max_delay` — a *range*:
  close the turn no sooner than min_delay, force-close at max_delay "so it never hangs
  forever"; `mode="dynamic"` adapts within the range to the user's pause patterns.
  Interruptions: `interruption.mode "adaptive"` (an acoustic model that separates real
  barge-ins from **backchannels** — "uh-huh", "okay") vs `"vad"` (any speech
  interrupts). The quantitative interruption knobs are **Deepgram Agent API fields,
  NOT LiveKit's** (attribution corrected 2026-11-11): Deepgram's
  `interruption.min_duration` (default 0.5 s of speech before an overlap counts) and
  `false_interruption_timeout` (default 2.0 s, paired with `resume_false_interruption`):
  after an interruption that yields no transcript, the agent **resumes its line** once
  the configured silence elapses. LiveKit's own interruption surface is the
  mode selector plus `backchannel_boundary` `(1.0, 1.0)` s cooldown: in the first
  second of agent speech, adaptive classification is suppressed (use raw VAD) so a
  real early correction isn't swallowed as backchannel.
- **Pipecat** (docs, verified): barge-in = broadcast `InterruptionFrame` on user-turn
  start — cancels in-flight LLM, clears TTS buffers, and **flushes unplayed transport
  audio** ("the bot goes silent within roughly one audio write"). Controls:
  `MinWordsUserTurnStartStrategy(min_words=3)`, user-mute strategies
  (`AlwaysUserMuteStrategy` — discard all user audio while bot speaks;
  `FirstSpeechUserMuteStrategy` — protect just the greeting), a model-based
  interruption predictor (Krisp VIVA) to filter acknowledgments, and a **turn-open
  priority rule**: with provider-side endpointing (e.g. Soniox), open the turn on the
  *local VAD* signal rather than the first transcript token, because transcript-based
  barge-in "adds the network round-trip plus model latency".
- **AssemblyAI's framing** (blog, verified): three endpointing families — energy VAD
  (fastest; fails on thoughtful pauses and background speech), forced endpoint timer
  (predictable; fails on long/hesitant answers), semantic end-of-turn (fails mainly on
  heavy overlap; ~307 ms median to final on Pipecat's benchmark in their U3-Pro
  pipeline). And a one-sentence verdict on tuning: "VAD … only hears energy, not
  meaning, so it cuts a caller who pauses to think and triggers on background speech."
- **Hugging Face speech-to-speech** (Pipecat-based, verified defaults above): adds
  Smart Turn v3.2 after Silero finalizes a turn (`smart_turn_threshold=0.5`,
  `smart_turn_max_wait_ms=2000`, `smart_turn_incomplete_delay_ms=600` — delay STT/LLM
  work while a turn might still resume).

### 1.7 Double-talk detection literature — why pure RMS fails, and what DTDs do

Echo cancellation research (all verified): the adaptive filter must **freeze/slow
adaptation whenever near-end speech is present**, else it diverges. Two families:
double-talk detectors (hard decision) and step-size control (soft decision).

- **Geigel algorithm** (Canadian Acoustics DTD survey): compare current near-end
  magnitude with the max of the L most recent far-end samples; declare double-talk when
  `|d(n)| > T·max{|x(n)|…|x(n−L+1)|}` with **T = 0.5** (assuming 6 dB hybrid
  attenuation). The survey's warnings map 1:1 onto meept's echo guard: the threshold T
  is *assumption-bound* (echo path attenuation), the echo path is **time-varying**, and
  a change of echo path gets falsely detected as double-talk.
- All practical DTDs add a **hold time / hangover**: once declared, stay declared for a
  minimum period because the detection variable is noisy; the survey is explicit that
  hold time exists "to suppress detection dropouts". (VSS-NSA paper uses a 200-sample
  = 25 ms hangover on the Geigel DTD as a worked example.)
- Spectral-divergence detectors (Benesty & Gänsler, Bell Labs) replace level
  comparison with a coherence/spectral test statistic vs a constant threshold —
  i.e., **compare *shape*, not just energy**, between reference and suspect signal.
- The general decision-variable shape (survey): compute a statistic from near-end,
  far-end, and residual signals; compare against a preset constant; declare, hold,
  resume. Nothing in this literature declares on a single energy ratio of the last
  outbound chunk.

### 1.8 meept's current position, source-verified

`internal/voice/AGENTS.md` §local-leg (458–486) pins: RMS trip = 10 sustained 20 ms
chunks (200 ms) over `voice.local.barge_in_rms`; echo guard requires window-average
energy > 130% of the **last outbound chunk's** average; STT evidence is one-shot (trip
zeroes `lastFinalAt`; a second window needs a NEW whisper final that post-dates the
dispatch; `[BLANK_AUDIO]`-class finals never stamp evidence); the splice discards
pre-cancel and post-cancel-residue chunks via a barge generation counter; mic PCM keeps
flowing to STT during playback. Config: `voice.local.barge_in_rms=3000`
(`config/meept.json5:1392`, default in `internal/config/voice_config_test.go:99`).
**jargo is already a pinned dependency** (`github.com/gojargo/jargo v0.1.0`, imported
only by `cmd/meept-voice` / `internal/voicechild` per the confinement invariant), and —
verified in the module cache — **jargo v0.1.0 embeds `audio/vad/silero_vad.onnx` plus a
streaming state machine** (`quiet → starting → speaking → stopping → quiet`, defaults
Confidence 0.7 / StartSecs 0.2 / StopSecs 0.2 / MinVolume 0.6, 8k/16k mono s16le input,
5 s recurrent resets, single-thread ONNX session) **and `audio/turn/smart-turn-v3.2-cpu.onnx`**.
Runtime needs the ONNX shared library discoverable (purego, `JARGO_ONNXRUNTIME_LIB`);
without it jargo falls back to STT endpointing and loses barge-in. There is also a pure-Go
no-runtime option in the wider ecosystem (zserge/govad, ~632 µs/32 ms frame on M1).
So "Silero reachable via jargo" is not hypothetical — the exact analyzer the local leg
needs is one import inside the already-confined child, subject to the child-confinement
rule.

---

## 2. Distilled principles

1. **Clean before you detect.** Every serious stack puts HPF/echo-cancel/noise-
   suppression/gain-normalization *before* the VAD/endpointer (WebRTC fixed chain;
   Asterisk's place-DENOISE-before-TALK_DETECT doctrine). meept currently does
   nothing between ffmpeg raw and the RMS gate.
2. **Dual thresholds + hangover, never a single level.** Silero (thr / thr−0.15 +
   100 ms min-silence), FreeSWITCH silence_hits, OpenAI prefix/silence pair, LiveKit
   min/max_delay, Geigel hold times — independent convergence on hysteresis. An
   instantaneous one-sided threshold with no exit condition is the known-bad design.
3. **Onset persistence is required but not sufficient for *endpointing*; for
   *barge-in* the onset trigger IS the interrupt, gated by duration/words.** Deepgram
   Agent API: `interruption.min_duration` 0.5 s (plus optional word-count policy);
   Pipecat: `MinWordsUserTurnStartStrategy`;
   Asterisk BackgroundDetect: min-ms non-silence *followed by* sil-ms silence even for
   interruptions during playback. meept's 200 ms persistence is in the right range;
   what's missing is *what kind of energy* (ML speech probability, not RMS) and the
   minimum-words/backchannel layer.
4. **Echo-aware double-talk compares the suspect signal against a window of the
   reference, with an explicit echo-path gain assumption and a hold time.** Geigel T=0.5
   over L past far-end samples; AEC literature is unanimous that single-sample level
   ratios are not a DTD. meept's ">130% of last outbound chunk" is a one-tap Geigel with
   T=1.3 — both the tap count and the constant are off the standard design.
5. **False interruptions must be reversible.** Deepgram Agent API:
   `resume_false_interruption` + `false_interruption_timeout` (default 2.0 s); HF
   speech-to-speech: speculative
   reopen windows. A barge-in that produces no transcript should not burn the turn —
   the agent resumes its line. meept's one-shot evidence latch makes the *second* false
   trip cheap but the *first* one terminal.
6. **Endpointing = min-delay + max-delay + semantic escape.** Close on silence ≥ N ms,
   never earlier than min_delay, force-close at max_delay so no call hangs; let an
   end-of-turn model extend (semantic_vad eagerness, Smart Turn) when the utterance is
   clearly incomplete. The STT's own final timing is not the endpointer — whisper's
   30 s window segments + temperature fallback make "finals only" endpointing
   structurally slow and gappy (faster-whisper's conservative 2 s min-silence exists
   because Whisper needs natural pauses intact).
7. **The STT itself is a speech detector.** Whisper gives per-segment
   `no_speech_prob`, mean logprob, entropy — validated rejects (defaults: nsp > 0.6 →
   discard). Use them as the evidence-quality filter instead of matching
   `[BLANK_AUDIO]` strings.
8. **Absolute thresholds rot; floors must adapt.** Asterisk's own field threads are
   "noisy line never goes silent → retune threshold"; VoIPmonitor: noisy environments
   drop silence% to 0–9%; the energy-VAD critique: "very sensitive to noise and volume
   changes" (WebRTC VAD exists because GMM spectral features beat raw energy). A fixed
   3000-RMS across mics/rooms/gains is the classic anti-pattern.

---

## 3. Gap analysis vs meept's current RMS + evidence gate

| Symptom | Mechanism in meept today | What established stacks do instead |
|---|---|---|
| Self-echo kills replies | Echo guard = window avg > 1.3× **last** outbound chunk energy. TTS chunks vary hugely within a sentence (plosives, inter-word dips); a mic sample spanning chunk N and N+1 compares against the wrong single tap; and the mic signal during playback contains echo *plus* room reverb tail not in the last chunk. | Geigel-style compare vs max of a **window** of recent reference energy (L samples covering echo delay + tail) with an explicit path-gain constant and a hold time; or actual AEC with the outbound stream as reference. RoomKit's numbers: paused/misaligned AEC leaves echo 0–5 dB below mic vs 34–44 dB when continuous. |
| Noise kills replies | 3000 absolute s16 RMS is ~−20 dBFS full-scale; a desk fan or laptop hiss at moderate gain clears it; nothing distinguishes speech-shaped energy from noise-shaped energy. | ML VAD probability (Silero: 0.5–0.7 confidence, phone profiles push 0.75) ANDed with a relative volume floor; adaptive noise-floor estimation (percentile tracker); spectral features. WebRTC NS before detection. |
| Dead-air between sentence finals | Endpointing is inherited from whisper finals: segment boundaries appear on whisper's schedule, not the speaker's; a mid-sentence pause still blocks dispatch until the windowed decode emits. | Decouple: VAD offset (stop_secs 0.2–0.5 s) closes the *audio* segment; feed that segment to STT; optionally extend only when a turn-completeness model says incomplete (Smart Turn / semantic_vad / reopen windows). faster-whisper's 400 ms pad exists because Whisper mis-transcribes abruptly-cut speech edges. |
| Slow endpointing | Finals-only whisper: no VAD-triggered flush, no speculative early dispatch; 10 s evidence window + post-dispatch constraint further serialize. | min_delay/max_delay pair (close at 0.3–0.5 s of confirmed silence, force at 2–3 s); provider-side endpoint tokens or semantic EOT at ~0.3 s median; VAD-start opens barge-in immediately while STT continues asynchronously (Pipecat: open the turn on local VAD, not on first token). |
| First false barge-in is unrecoverable | One-shot evidence latch zeroes lastFinalAt; the turn is already killed; agent silence follows until a new final. | resume_false_interruption after false_interruption_timeout of continued silence; backchannel boundary cooldown; min_words/adaptive interruption model to prevent the false trip. |
| Evidence quality by string match | nonSpeechTranscript enumerates `[BLANK_AUDIO]`/`(sighs)` classes. | no_speech_prob / logprob / entropy thresholds from whisper's own per-segment metadata (cli defaults: nsp 0.6, logprob −1.0, entropy 2.4). |

---

## 4. Ranked concrete recommendations

Ranked by leverage/effort. None require installing new system components today; #1's
ONNX-runtime availability question is stated as a risk, not an install plan.

1. **Swap the barge-in gate from raw RMS to jargo's embedded Silero analyzer** —
   `audio/vad.NewSilero()` + its state machine (defaults Confidence 0.7 / StartSecs 0.2 /
   StopSecs 0.2 / MinVolume 0.6), already vendored inside the pinned jargo v0.1.0 and
   inside the voicechild's confinement boundary. Keep the existing 200 ms persistence
   semantics (it equals StartSecs 0.2). Trip on `StateStarting→StateSpeaking`, not on
   energy. *Why:* kills both false-trip classes at once (noise and near-echo that isn't
   speech-shaped — Silero is explicitly "the best discriminator against music, tones,
   and noise"); every production agent stack (Pipecat, LiveKit, OpenAI server_vad,
   OsmTalk, HF speech-to-speech) gates on ML VAD, not RMS. *Effort:* M. *Risk:* the
   analyzer needs the ONNX runtime shared library at child runtime (purego-loaded;
   jargo degrades gracefully to no-VAD when absent — the child needs an explicit
   fall-back-to-RMS posture, and this rides the existing compile-time-pin/child-confinement
   invariants). A pure-Go fallback (zserge/govad-class port, ~632 µs/frame) exists if
   the ONNX lane is unacceptable in the constrained child.

2. **Replace the 1.3×-last-chunk echo guard with a proper double-talk statistic.**
   Compute the reference window as max/percentile of outbound chunk energies over the
   last echo-tail span (e.g. 500–1000 ms, not one 20 ms chunk), declare echo-containable
   ingress when `ingress ≤ T·reference_window_max` with T tuned per device, plus a
   short hold time (25–100 ms — Geigel/VSS-NSA practice). Longer term: feed the .out
   stream to a real canceller (speex AEC / WebRTC APM bindings exist in Go/CGO-adjacent
   form) and gate on the *residual*, per the WebRTC chain — but treat that as a separate
   L-sized spike; the window-based guard alone recovers most of defect (a).
   *Why:* meept's current rule is a single-tap Geigel with a fixed constant; the
   literature and RoomKit's measured failure (paused AEC → server VAD hears echo as
   user) both say continuous reference history is the load-bearing part. *Effort:* S/M.
   *Risk:* over-suppression of quiet real barge-ins; mitigate by making the ML-VAD
   trip (#1) able to override a marginal echo verdict when speech probability is high
   and sustained (double-talk decision = VAD ∧ ¬echo, never ¬energy).

3. **VAD-offset-triggered flush for endpointing (fixes (b) and (c)).** On
   `speaking→quiet` after StopSecs (start at 0.3–0.5 s for phone-like audio;
   OpenAI ships 500 ms, LiveKit min_delay 0.3–0.5 s, Asterisk hangs over 2.5 s for
   pure silence-timeout UX), immediately dispatch the buffered segment to whisper for a
   final — and cap with a max_turn_silence force-flush (2–3 s) so a lost final can't
   hang the turn. Keep whisper decode windows speech-shaped: carry ~200–400 ms of
   trailing audio into the next segment (stream.cpp `keep_ms=200`; faster-whisper pads
   300–400 ms for the same reason — abrupt edges degrade Whisper accuracy/hallucinate).
   *Why:* turns stop being gated by whisper's internal segmentation; dead-air shrinks
   by roughly the gap between speech offset and the next forced window boundary.
   *Effort:* M. *Risk:* cutting on very short pauses (user thinking) — add the reopen
   pattern from HF speech-to-speech (`speculative_reopen_ms`-style: speech resuming
   within ~0.8 s after a soft end extends the same utterance rather than starting a
   new turn).

4. **Filter whisper evidence by `no_speech_prob`/logprob instead of transcript strings.**
   whisper.cpp already reports per-segment `no_speech_prob` (CLI rejects > 0.6), mean
   logprob (−1.0) and entropy (2.4) gates. Use them: barge-in evidence = segment with
   nsp below threshold AND post-dispatch timestamp; keep the string list only as a
   belt-and-braces. *Why:* the current `[BLANK_AUDIO]` pattern list is a proxy for
   exactly the metadata the decoder produces directly; it can't catch confident-sounding
   hallucinated text on echo/noise, which logprob can. *Effort:* S. *Risk:* needs the
   child's whisper binding to surface segment metadata (verify `whisper_full_get_...`
   exposure in the jargo local-whisper provider or the cgo layer in use).

5. **Normalize ingress energy before any remaining RMS fallback (AGC-style), with an
   adaptive noise floor.** For the fall-back path and for MinVolume: track a running
   percentile floor of frame levels (Silero-issue-798 methodology used floor = 30th
   percentile of frame dB, onset = run of ≥3 frames above floor+6 dB); replace
   `barge_in_rms=3000` with a relative rule "above floor + margin", optionally
   soft-limited to a target level (WebRTC AGC2 / RoomKit SimpleAGC: attack ~10 ms,
   release ~100 ms, never amplify below a −60 dBFS floor). *Why:* fixes the
   "works-in-office-fails-on-that-laptop" class — absolute s16 thresholds rot across
   devices/gains (Asterisk's field threads, VoIPmonitor's noise-vs-silence data).
   *Effort:* S. *Risk:* slow floor adaptation during sustained loud ambient → floor
   drifts up and real speech never trips; clamp adaptation rate and freeze floor updates
   while VAD says speech.

6. **Make false barge-ins recoverable and backchannel-shaped trips ignorable.**
   Add: (i) `resume_false_interruption` semantics — if X ms (2 s Deepgram-default
   style, `false_interruption_timeout`) pass
   with no qualifying whisper final after a trip, replay/resume the interrupted line's
   tail or re-dispatch the pending say (meept already tracks per-barge generations; the
   splice's stale-discard logic is the hard part — keep it, resume from the word
   boundary not the byte offset); (ii) minimum-content policy for the first second of
   agent speech: treat short low-confidence trips inside a 1 s agent-speech onset
   window as backchannels (LiveKit `backchannel_boundary(1.0,1.0)`), and optionally
   require `min_words≥1` from the in-flight final (Pipecat `MinWordsUserTurnStartStrategy`).
   *Why:* "the agent yields when it should not" is the mirror defect to "talks over the
   user"; meept's one-shot latch was built for the second trip, not the first. *Effort:*
   M/L (needs resume UX contract in the splice). *Risk:* resuming over a user who
   restarted their interruption — bound with the same generation counter.

7. **(Sized spike, do last) Real AEC on the local leg.** Feed the .out chunk stream as
   reference to a canceller (SpeexDSP-style, WebRTC APM port, or the newer pure-Rust
   sonora stack as a Go-sidecar option) and run the VAD/barge gate on the cleaned
   signal — the full WebRTC order (HPF→AEC→NS→AGC→VAD). *Why:* every other measure in
   this list is compensation; AEC removes the root cause of the self-interrupt class,
   and lets the echo margin (rec #2) become residual-based instead of energy-ratio
   based. *Effort:* L (CGO or sidecar; rides the child-confinement invariants — it
   must land in the jargo-confined child, not the daemon). *Risk:* convergence on
   changing rooms/devices; RoomKit's documented trap — never pause the canceller
   between responses, run it continuously with a stable output stream, or residual
   echo jumps 10×.

Suggested adoption order: 4 → 5 → 2 → 1 (or 1 first if the ONNX lane verifies quickly) →
3 → 6 → 7. Items 4 and 5 are strictly additive to the current gate and de-risk the rest.

---

## Sources

1. whisper.cpp CLI VAD + thresholds — https://github.com/ggml-org/whisper.cpp/blob/master/examples/cli/cli.cpp
2. whisper.cpp README VAD options — https://github.com/ggml-org/whisper.cpp
3. whisper.cpp stream example (step/length/keep, vad_simple) — https://github.com/ggml-org/whisper.cpp/blob/master/examples/stream/stream.cpp
4. Community sentence-flush-on-silence pattern — https://github.com/ggml-org/whisper.cpp/discussions/211
5. faster-whisper VAD (VadOptions defaults, neg_threshold) — https://github.com/SYSTRAN/faster-whisper/blob/master/faster_whisper/vad.py
6. faster-whisper VAD conservatism rationale — https://github.com/SYSTRAN/faster-whisper/issues/477 ; padding tuning report — https://github.com/SYSTRAN/faster-whisper/issues/925
7. Silero VAD parameter defaults — https://deepwiki.com/snakers4/silero-vad/5-advanced-usage ; get_speech_timestamps signature — https://github.com/snakers4/silero-vad/discussions/562 ; ONNX usage example — https://github.com/snakers4/silero-vad/blob/master/examples/java-example/src/main/java/org/example/App.java
8. Silero boundary-lag measurements & maintainer replies — https://github.com/snakers4/silero-vad/issues/798
9. whisper.cpp Silero port semantics (0.2 s merge, negative threshold) — https://praat.org/manual/speech_activity_detection_with_Silero_VAD.html
10. WebRTC APM chain order + AEC3/NS/AGC2 explanations — https://www.forasoft.com/learn/audio-for-video/articles-audio/webrtc-audio-pipeline-end-to-end ; source order — https://webrtc.googlesource.com/src/+/refs/heads/main/modules/audio_processing/audio_processing_impl.cc ; AGC2 rnn_vad — https://deepwiki.com/jiemojiemo/webrtc-audio-processing-cmake/3-audio-processing-module-(apm)
11. Continuous-AEC-required field evidence — https://www.roomkit.live/docs/guides/audio-pipeline-stages/
12. Geigel DTD, hold times, time-varying echo path — https://jcaa.caa-aca.ca/index.php/jcaa/article/view/1669 ; MDF DTD — https://doi.org/10.1155/s1110865703305037 ; Benesty spectral DTD — https://www.iwaenc.org/proceedings/2001/main/data/benesty.pdf ; VSS-NSA 25 ms hangover example — https://scholarsmine.mst.edu/cgi/viewcontent.cgi?article=7286&context=ele_comeng_facwork
13. Asterisk TALK_DETECT (defaults 256 / 2500 ms, audiohook ordering) — https://docs.asterisk.org/Latest_API/API_Documentation/Dialplan_Functions/TALK_DETECT/ ; WaitForSilence — https://github.com/asterisk/asterisk/blob/master/apps/app_waitforsilence.c ; BackgroundDetect — https://docs.asterisk.org/Latest_API/API_Documentation/Dialplan_Applications/BackgroundDetect/ ; noisy-line threshold failure — https://serverfault.com/questions/680500/asterisk-waitforsilence-never-detects-silence
14. FreeSWITCH mod_dptools detect_silence/detect_audio/wait_for_silence + vad_* vars — https://github.com/signalwire/freeswitch/blob/master/src/mod/applications/mod_dptools/mod_dptools.c ; voicemail record-silence params — https://lists.freeswitch.org/pipermail/freeswitch-users/2015-January/110675.html
15. OpenAI Realtime server_vad/semantic_vad (defaults 0.5 / 300 / 500 ms; eagerness timeouts) — https://developers.openai.com/api/docs/guides/realtime-vad ; full field defs — https://developers.openai.com/api/reference/resources/realtime/client-events ; LiveKit realtime-plugin overrides — https://cdn.jsdelivr.net/npm/@livekit/agents-plugin-openai@1.7.0/src/stt.ts
16. LiveKit turn handling/interruption config (interruption.mode adaptive vs vad, backchannel_boundary; the quantitative min_duration/min_words/false_interruption_timeout knobs are Deepgram Agent API fields) — https://livekit.com/blog/turn-detection-and-interruption-handling ; https://docs.livekit.io/agents/logic/turns/adaptive-interruption-handling/
17. Pipecat interruptions & turn strategies (InterruptionFrame, MinWords, mute strategies) — https://docs.pipecat.ai/pipecat/fundamentals/interruptions ; VADParams + Smart Turn — https://docs.pipecat.ai/pipecat/learn/speech-input ; VAD-first turn opening — https://github.com/pipecat-ai/pipecat/blob/main/examples/voice/voice-soniox-turn-detection.py
18. AssemblyAI endpointing taxonomy — https://www.assemblyai.com/blog/voice-agent-turn-detection
19. Barge-in failure-mode corpus (echo bleed, 8 kHz regressions) — https://github.com/quantumCF/voice-agent-barge-in-tests
20. HF speech-to-speech VAD/turn defaults (hysteresis, reopen windows, Smart Turn) — https://github.com/huggingface/speech-to-speech/blob/3d3f7479/src/speech_to_speech/arguments_classes/vad_arguments.py
21. OsmTalk phone VAD profile (0.75 confidence / 0.7 min volume) — https://github.com/osm-API/osmtalk-examples/blob/main/AGENT_CONFIG.md
22. Energy VAD weaknesses survey text — https://ziyanglin.netlify.app/en/post/vad-documentation/ ; noisy-line silence stats — https://www.voipmonitor.org/doc/Silence_detection
23. jargo Silero/Smart-Turn embedding + params (module-cache read, v0.1.0) — https://github.com/gojargo/jargo ; pure-Go Silero port performance — https://github.com/zserge/govad
24. meept local-leg contract — internal/voice/AGENTS.md:458–496; config/meept.json5:1392
