# Voice audio path vs. industry practice — findings report

Date: 2026-11-11 · Scope: read-only audit of the **local leg** audio path
(`voice local` live mode: mic → transport → STT → turn → TTS → pacing →
playback), compared stage-by-stage against published industry standards and
production voice-agent practice. Phone-leg (Telnyx/Deepgram) differences are
noted only where they show the local leg diverging from meept's own better
path. No code was changed.

Line references are `git HEAD` (branch tip, 2026-11-11 working tree).

---

## 1. The measured path (from code, not from the brief)

```
ffmpeg avfoundation :0  -> raw s16le 16k mono      internal/voice/clientaudio.go:143-151
  io.Copy -> <ccid>.c.in unix socket               cmd/meept/voice.go:614,637
daemon LocalSplice: client .c.in -> child .in      internal/voice/localsplice.go (pump; no reorder by contract, :9-16)
  child .in -> pumpAudioIn -> WhisperSTT.Stream    internal/voicechild/pipeline.go:540 (rmsBargeInCheck hook)
  energy-gate segmentation (500 ms silence / 500 ms wall-idle / 10 s cap)
                                                   internal/voicechild/providers/stt_whisper.go:31-49
  per-utterance WAV + whisper-cli SUBPROCESS       internal/voicechild/providers/stt_whisper.go:292-305
  FINAL-only transcript -> prompt frame (last:true)
                                                   internal/voicechild/providers/stt_whisper.go:65-68
daemon talk loop -> employee LLM turn -> sentences internal/voice/talkloop.go
  supervisor sentence gate: code default true (internal/config/schema.go:5085),
  DISABLED in the installed config
  (~/.meept/meept.json5:917 — LFM-1.2B denied ~1/3 of sentences = audible chop)
say frames (C1 text) -> child pipeline playbackWorker
                                                   internal/voicechild/pipeline.go:943-961 (played edge)
  TTS: POST /audio/speech response_format=pcm stream=true, 24 kHz
                                                   internal/voicechild/providers/tts_openai.go:19-22,102-110
  streaming resample 24k->16k (jargo stateful)     internal/voicechild/providers/resample.go:27-35
  synth prefetch depth 2                            internal/voicechild/pipeline.go:658-667
  whole-utterance-burst server symptom: gaps "1-4 s", synth "0.7-5.5 s/sentence"
                                                   internal/voicechild/pipeline.go:115-125,660-662
  child writes .out as fast as synthesis completes internal/voicechild/pipeline.go:1103-1105 (echo reference recorded at WRITE time)
daemon pacer: one 640 B chunk per 20 ms .out -> .c.out
                                                   internal/voice/localsplice.go:63-68
client drainLiveSpeaker: backlog cap 375 ms        cmd/meept/voice.go:483 (liveMaxPendingAudioBytes = 12000 B)
  SpeakerSink burst gap 350 ms                     internal/voice/clientaudio.go:283
  oto player @16 kHz                               internal/voice/clientaudio.go:254-256
barge-in: RMS gate, 10-chunk (200 ms) window, echo guard = incoming
  window avg > 130 % of outbound reference avg + STT-final evidence
                                                   internal/voicechild/pipeline.go:1207,1293-1338,1418-1426
link floor coordinator fallback margin 12 s        internal/voice/link.go:124,1023-1030
drain quiescence: 1 s blind / 500 ms played-armed  internal/voice/localsplice.go:85-104
```

## 2. Stage-by-stage: current vs. standard

| # | Stage | meept local leg (file:line) | Industry standard | Gap class |
|---|-------|------------------------------|-------------------|-----------|
| 1 | **Capture / input conditioning** | ffmpeg `avfoundation :0 -f s16le -ar 16000 -ac 1`, nothing between device and socket (clientaudio.go:143-151). No high-pass, no NS, **no AGC**, no level normalization; hardcoded first device, not system default. | WebRTC APM places processing "as close to the audio HAL as possible": HP-filter → AEC → NS → AGC, with AGC holding active speech near **-19 dBm0 ≈ RMS 2600 int16** (RFC 7874 §4). 16k mono s16le is the exact Kaldi/Whisper/DeepSpeech wire convention — format is right, conditioning is absent. | **Missing** (AEC/NS/AGC) + pitfall (unbounded input level) |
| 2 | **Transport (client↔daemon↔child)** | unix socket pairs, 640 B / 20 ms framing, no reorder and **no jitter buffer by contract** (localsplice.go:9-16, AGENTS C11 "do not harden with a reorder buffer"). Backpressure: socket write error ends the leg. | RFC 3551 §4.2: 20 ms packetization default; RFC 3550 contiguous 20 ms playout. Jitter buffers (Asterisk adaptive JB: reorder + interpolate + playout target = jitter + min lateness + 40 ms extra; FreeSWITCH `jitterbuffer_msec` elastic mode) exist to absorb **network** loss/reorder. | **Aligned by design.** Unix sockets are ordered, lossless, local — a jitter buffer here would add pure delay. The pacer's fixed 20 ms cadence is the canonical telephony convention. |
| 3 | **Endpointing / turn detection** | energy gate closes an utterance after 500 ms trailing silence OR 500 ms wall-no-bytes; 10 s hard window; only then one FINAL transcript (stt_whisper.go:28-52, 65-68). Phone leg instead uses Deepgram streaming partials. | LiveKit: VAD → audio-native turn-detector (prosody+semantics, no transcript needed), endpointing min_delay 0.5 s → 0.3 s when confident; Pipecat Smart Turn scores last ~8 s of turn audio **during** the STT window; OpenAI Realtime `silence_duration_ms` default 500 ms, `semantic_vad` caps 2-8 s; Deepgram pause_detection 300-500 ms + speech_final. Energy gating alone mis-classifies breaths/pauses as turn end and long low-level speech as silence. | **Weaker heuristic** (pure RMS energy, no semantic/prosodic EOT); the 500 ms *value* itself is in-family. |
| 4 | **STT** | whisper.cpp `whisper-cli` spawned as a **fresh subprocess per utterance** — process spawn + model load + decode, after the endpoint wait; finals-only, no partials (stt_whisper.go:292-305). | Whisper is explicitly non-streaming (arXiv 2307.14743: "Whisper is not designed for real-time transcription"; naive chunking has "large latency and low quality at boundaries"). Production wraps: persistent model + VAD-segmented decode (faster-whisper `vad_filter`, common flush 500 ms) or streaming partials (whisper-streaming LocalAgreement-2 ~3.3 s avg latency; Deepgram: sub-300 ms partials, is_final false/true, speech_final). The post-speech critical path should be *finalization-only* (~50-200 ms), not endpoint-wait + cold-start + full decode. | **Architectural mismatch** (per-segment cold subprocess vs. resident model; finals-only vs. streaming partials) |
| 5 | **Turn / reply path** | one employee LLM turn on the pinned voice endpoint; per-sentence supervisor gate (fail-open, 400 ms timeout, fallback line "I cannot confirm that yet." — talkloop.go:73-76) **disabled in installed config** (~/.meept/meept.json5:917). Link floor: half-duplex, played-armed drain watch (500 ms), fallback margin 12 s (link.go:124). | Frameworks overlap turn stages: LiveKit "preemptive generation" starts the LLM on the final transcript *before* turn commit; Deepgram Flux EagerEndOfTurn speculatively preps generation (EOT latency 100-500 ms, claimed 200-600 ms saved). Fallback/release timers in LiveKit/Pipecat/OpenAI top out at 2.5-8 s; a 12 s floor fallback is 1.5-4× any published worst-case cap. | Half-duplex + long margins (standard is full-duplex with AEC-backed barge-in); supervisor timeout adds a serial per-sentence LLM hop when re-enabled |
| 6 | **TTS synthesis** | OpenAI-compatible `POST /audio/speech`, `response_format=pcm`, `stream=true` (tts_openai.go:102-110); 24 kHz s16le streamed through a **stateful incremental resampler** to 16k (resample.go — good practice) — but the qwen3 server (uvicorn faster-qwen3-tts) delivers **whole-utterance bursts**, so first-audio = whole-sentence synth time ("0.7-5.5 s per sentence", pipeline.go:660-662). Mitigated by prefetch depth 2 (pipeline.go:658-667), which removes the *next*-sentence gap but not the *first*-sentence gap of every turn. | ElevenLabs: "with the standard endpoint, TTFA = time to synthesise the *entire* text; with streaming, TTFA ≈ first chunk"; Flash v2.5 ~75-100 ms p50 server-side; NVIDIA Riva measures "latency to first audio chunk" and sets a <300 ms conversational bar; huggingface/speech-to-speech ships a 196 ms startup buffer for exactly the OpenAI-compatible "burst then gap" class; a documented OpenAI-compat case study (NousResearch/hermes-agent #149699) measured 2.85-3.7 s first-audio buffered vs 0.92-0.95 s with true SSE streaming on the *same server*. | **Server-side mismatch** (per-request whole-utterance buffering); client side already correct |
| 7 | **Pacing / smoothing** | daemon pacer emits exactly one 640 B chunk / 20 ms regardless of arrival burstiness (localsplice.go:63-68); pending==0 + quiet-window drain detection (localsplice.go:85-104). | Pacing-at-source is the RFC 3551/3550 convention and correctly bounds downstream jitter. But the industry pairs it with a *receive-side* elastic playout buffer (Asterisk target = measured jitter + 40 ms; FreeSWITCH elastic expand/collapse at 3× estimate). meept's "jitter buffer" is purely the fixed 20 ms metronome plus a client backlog cap — fine for ordering, but it **converts burst delivery into seconds of hidden backlog** (an utterance written in 0.5 s drains for its whole duration), which poisons latency accounting and the echo reference (see §4.1). | Pacer **aligned**; using it as the only smoothing stage while measuring echo against write-time, not play-time, is the mismatch |
| 8 | **Playback** | client caps unplayed backlog at 375 ms (voice.go:483: `liveMaxPendingAudioBytes = 16000*2*375/1000 = 12000 B`), burst gap 350 ms (clientaudio.go:283), oto raw-PCM player pinned to 16 kHz (clientaudio.go:254-256; the historical 22050 Hz context played ~38 % fast). Barge-in Stop pauses **all** live bursts (leaf 01 recall contract). | 196-500 ms startup cushions are standard for burst-y TTS (huggingface 196 ms; ElevenLabs notes 500 ms default player buffers, 50-150 ms safe on stable links); oto sub-20 ms recall ≈ instant playout stop, better than most. | **Aligned / strong** |
| 9 | **Barge-in / echo** | child RMS gate: 10 × 20 ms sustained chunks over `voice.local.barge_in_rms`, AND window avg > 130 % of outbound reference avg (pipeline.go:1207,1293-1338,1418-1426), AND one-shot STT-final evidence within a 10 s bound (pipeline.go:1428+). Outbound reference = energy at the moment the child **writes** to .out (pipeline.go:1103-1105). | RFC 7874: endpoints SHOULD implement echo control; AEC correlates a **time-aligned render reference** (WebRTC ProcessReverseStream, 10 ms frames) and replaces level-ratio tests with correlation-based double-talk detection; AEC must tolerate mic/speaker clock drift. Chrome: hardware NS before the canceller impedes it. | **Architectural mismatch**: an energy-ratio heuristic with an unaligned (write-time) reference is precisely what AEC+DTD exists to replace; no delay-line alignment, no correlation, no clock-drift handling |

## 3. Latency-chain budget — where the observed numbers sit

Industry budget (AWS "800 ms Rule"; Soniox; ElevenLabs voice-agent post; Twig — all
converging on **≤800 ms p50 / <1.5 s p95** speech-to-speech; human turn-offset mode
0-200 ms, mean +208 ms, Stivers et al. 2009 PNAS):

| Segment | Industry budget | meept local-leg composition | Code evidence |
|---|---|---|---|
| Endpoint wait after speech | 150-300 ms | **≥500 ms** (energy silence OR wall-idle, no early semantic EOT) | stt_whisper.go:34,46 |
| STT finalization | 100-250 ms (residents model finalizing) | **+ full whisper-cli cold-start per utterance**: process spawn + `whisper-cli -m <model.ggml>` model load + whole-utterance decode, serial after the endpoint | stt_whisper.go:297 |
| LLM TTFT → first sentence | 300-600 ms | on-par in principle (pinned voice endpoint); +400 ms/sentence if the supervisor gate is re-enabled without overlap (supervisor_timeout_ms=400, ~/.meept/meept.json5:919) | talkloop.go:73-76 |
| TTS first audio | 100-250 ms | **0.7-5.5 s for sentence 1** of every turn (whole-utterance server burst; prefetch depth 2 only protects sentences 2+); observed inter-sentence residual gaps **1.5-4 s** in the field / "1-4 s" and synth gaps 4620/6963/1238 ms in the prefetch test header | pipeline.go:115-125,660-662; pipeline_prefetch_test.go:9 |
| Network/player | 50-150 ms | ~20-150 ms (pacer 20 ms granularity + 375 ms client backlog cap, played continuously) | voice.go:483; localsplice.go:65 |
| **Sum (serial, first reply)** | **~0.8-1.5 s** | **~2.7-10 s typical** — endpoint(0.5) + whisper cold-start+decode(~1-3) + LLM(0.5-2) + sentence-1 synth(0.7-5.5) | composition of rows above |

The reported **12.7 s turn**: consistent with that serial sum on a long multi-sentence
reply (prefetch hides sentence 2+ gaps, but a synth burst longer than its predecessor's
*playback* re-exposes serialization — with 0.7-5.5 s synth vs. short first sentences,
the queue stalls), plus possible interaction with the 12 s floor-fallback margin
(link.go:124) on a linked call. Caveat: 12.7 s comes from the task brief's field
measurement; the repo's own verified measurements in the same class are the overlap
figures 13.70 s / 10.94 s / 6.70 s in internal/voice/AGENTS.md and the synth gaps
4620/6963/1238 ms in pipeline_prefetch_test.go:9. It is ~8-15× the published
real-world median voice-agent turn (1.4-1.7 s cited secondhand by sippulse.ai).

## 4. Ranked weaknesses

1. **Echo reference is write-time, playback is seconds behind it (correctness, not just
   latency).** The child records outbound chunk energy when it *writes* to `.out`
   (pipeline.go:1103-1105), but the daemon pacer drains that audio at real-time
   (localsplice.go:63-68). On a 20 s reply the reference can lead the actual speaker
   output by up to ~20 s. The 130 %-ratio guard (pipeline.go:1336, const at :1425) is then comparing the
   mic against audio that isn't currently playing — the exact class of failure that made
   the operator-near-speakers trips documented in the 10 s evidence window necessary. No
   production stack does this: AEC requires a *time-aligned* render stream (RFC 7874,
   WebRTC ProcessReverseStream). This is the root architectural flaw underneath the
   barge-in whack-a-mole in AGENTS.md (fixed 615808e3, A6, one-shot evidence…).
2. **TTS first-audio is whole-sentence (0.7-5.5 s).** The burst-y qwen3 server serializes
   the one stage the industry overlaps. Prefetch depth 2 is the right client-side bandage
   for *inter*-sentence gaps but structurally cannot fix *first*-sentence latency, which
   is the metric the whole 800 ms bar is measured on (ElevenLabs TTFA math; Riva
   first-audio-chunk methodology).
3. **Whisper subprocess-per-utterance = cold model load on the critical path**
   (stt_whisper.go:297). Finals-only + post-endpoint decode stacks two serial penalties
   (0.5 s wait + spawn/load/decode) where whisper-streaming/faster-whisper patterns pay
   ~50-200 ms finalization because the model is resident and partials already did the
   work during speech. The phone leg already has the better architecture (Deepgram
   partials) — the local leg is the outlier inside meept itself.
4. **No input conditioning: no AGC/NS/high-pass, no level target.** The whisper energy
   gate is fixed at RMS 0.001 ≈ -60 dBFS (stt_whisper.go:31): a quiet mic never counts
   as voiced (utterances silently dropped or truncated), a loud noisy one closes
   endpointing on breath gaps. RFC 7874's -19 dBm0/AGC convention exists precisely
   because STT features degrade off-level.
5. **Half-duplex floor + 12 s fallback margin.** 12 s (link.go:124) is 1.5-4× the worst
   published caps (LiveKit max_delay 3.0/2.5 s; OpenAI semantic-vad 8 s eager-caps); when
   the played-edge never arrives the user is soft-muted for up to 24 s (double-margin
   re-arm, AGENTS C11). The elaborate holder/non-holder/chaining machinery in AGENTS.md
   is the cost of doing turn-taking without AEC + semantic EOT.
6. **Endpoint-only heuristics (energy, no semantics).** 500 ms is in-family with
   Deepgram `endpointing=300`-500 / OpenAI 500, but the *signal* is amplitude, not
   prosody/semantics: mid-thought pauses ≥0.5 s cut turns; trailing laughs/noise open
   them. LiveKit/Pipecat show the standard answer (audio-native EOT model scored in the
   STT window, ~10-100 ms inference).
7. Minor: `avfoundation -i :0` hardcodes the *first* device, not the system default
   (clientaudio.go:145); the supervisor gate is currently disabled in installed config
   (a latency win, honesty risk — its 400 ms per-sentence serial check is exactly why
   it must run *concurrent with synthesis*, which the code already does for N+1);
   ffmpeg→16k resample quality is swresample default (polyphase, adequate — verified no
   nearest/linear override).

## 5. Adoption recommendations

| # | Recommendation | Effort | Risk | Expected audible effect |
|---|---|---|---|---|
| R1 | **Fix the echo reference clock**: have the client report play-position (or the pacer stamp chunks), so the RMS gate compares the mic window against the audio actually audible ± a drift buffer — minimal step: reference = bytes *written by the pacer to .c.out*, not bytes written by the child to .out (localsplice knows both). | **S** | Low (local leg only; phone leg untouched) | Far fewer phantom self-trips and missed real barges during long replies; the 10 s evidence hack can shrink |
| R2 | **True incremental TTS chunks**: make the qwen3 server emit per-chunk SSE audio (verified case study: same server class went 2.85-3.7 s → 0.92-0.95 s first audio once the streaming flag was honored), or shard sentence 1 into first-clause synthesis client-side. Keep prefetch depth 2. | **M** | Server-side (faster-qwen3-tts) or first-clause prosody slightly worse | Kills the dominant term of every reply: first words audible in <1 s instead of 1-5 s; the "12.7 s turn" class mostly disappears |
| R3 | **Resident whisper + partials**: replace the subprocess-per-utterance with a persistent whisper.cpp server loop / `whisper_full_n_ctx` reuse (or faster-whisper-style: load once, VAD-flush segments), and emit *interim* transcripts on a 250-500 ms partial cadence so (a) barge-in gets the phone leg's STT-partial trigger instead of RMS, and (b) the LLM can be primed speculatively (LiveKit preemptive-generation pattern). | **L** | Medium (memory-resident model in the child; process supervision exists via llm.RuntimeProcess) | Endpoint→final drops ~0.5-2 s; reply often starts before the user fully stops |
| R4 | **Input AGC (and light NS/high-pass)**: ffmpeg `alimiter`/`loudnorm` or a small Go AGC before the .c.in pump, targeting RFC 7874's -19 dBm0 (RMS ≈ 2600 int16); make `whisperSilenceRMS` adaptive to measured noise floor. | **S** | Low (ffmpeg filter graph is local; overdriven mics need the limiter half) | Quiet-mic users stop getting dropped utterances; fewer mis-endpoints; whisper accuracy improves on off-level input |
| R5 | **Shrink the fallback margin** once R1/R2 land: 12 s → 5 s (still above the 3.0 s LiveKit worst case with margin for self-hosted synth), and add an early semantic EOT scorer at VAD-pause (Pipecat Smart Turn runs in the STT finalization window, 10-100 ms) so endpointing stops paying the flat 500 ms. | **M** | Low for the margin (config value); Medium for EOT model integration | Turn hand-back tightens toward the 800 ms p50 bar; less dead air after each reply |
| R6 | Device selection: `:0` → system default / `--mic` flag (S, low risk, fixes "wrong device" first-run failures); document the 375 ms sink cap as intentional startup cushion in the 196-500 ms industry range. | **S** | Low | fewer dead-greeting sessions |

Explicit non-recommendations (don't "fix" what matches standard): the 20 ms pacer
cadence and the 640 B L16 framing are RFC 3551/Kaldi-canonical; the no-jitter-buffer
local-socket contract is correct (AGENTS C11 pins it); the oto recall-barge-in and
raw-PCM burst contract are ahead of typical stacks, not behind.

## 6. Sources

- WebRTC APM (processing near HAL, 10 ms frames, ProcessReverseStream reference):
  https://webrtc.googlesource.com/src/+/adf823269b536b5f5c4f19bb67bcf23dd294b801/webrtc/modules/audio_processing/include/audio_processing.h
- Pipeline order HP→AEC→NS→AGC teardown: https://sora-doc.shiguredo.jp/LIBWEBRTC_AUDIO ;
  hardware NS impedes AEC: https://developer.chrome.com/blog/disabling-hardware-noise-suppression
- RFC 7874 (AEC SHOULD, clock drift, -19 dBm0 ≈ RMS 2600): https://www.rfc-editor.org/rfc/rfc7874.html
- RFC 3551 §4.2 20 ms packetization: https://www.rfc-editor.org/info/rfc3551/ ; RFC 3550: https://www.rfc-editor.org/rfc/rfc3550.pdf
- Asterisk adaptive jitterbuffer (target=jitter+min_lateness+40 ms extra): https://docs.asterisk.org/Latest_API/API_Documentation/Dialplan_Functions/JITTERBUFFER/ ; https://github.com/asterisk/asterisk/blob/master/main/jitterbuf.c
- FreeSWITCH jitterbuffer/elastic: https://developer.signalwire.com/freeswitch/media-and-codecs/handling/ ; https://github.com/signalwire/freeswitch/pull/2069
- 16k mono s16le STT convention: https://kaldi-asr.org/doc/online_programs.html ; https://github.com/openai/whisper/blob/main/whisper/audio.py ; https://github.com/ggerganov/whisper.cpp/issues/909 ; https://deepspeech.readthedocs.io/en/latest/USING.html
- Downsampling aliasing: https://ccrma.stanford.edu/~jos/sasp/Downsampling_Aliasing.html ; EBU R128 -23 LUFS: https://tech.ebu.ch/docs/r/r128.pdf
- ITU-T G.114 one-way <150 ms: https://www.itu.int/rec/dologin_pub.asp?id=T-REC-G.114-200005-S!!PDF-E&lang=e&type=items
- Stivers et al. 2009 PNAS (mean +208 ms): https://www.pnas.org/doi/10.1073/pnas.0903616106 (mirror https://pmc.ncbi.nlm.nih.gov/articles/PMC2705608/)
- LiveKit turn detection (min/max delay 0.5-3.0 s, 0.3/2.5 confident; 250/550 ms VAD floors): https://docs.livekit.io/agents/logic/turns/turn-detector/ ; tuning/interruption/preemptive gen: https://docs.livekit.io/agents/logic/turns/tuning/ ; why audio-native EOT: https://livekit.com/blog/solving-end-of-turn-detection
- Pipecat VAD/smart turn: https://docs.pipecat.ai/pipecat/learn/speech-input ; https://docs.pipecat.ai/api-reference/server/utilities/turn-detection/smart-turn-overview ; https://github.com/pipecat-ai/smart-turn
- OpenAI Realtime VAD (500 ms default, semantic_vad caps): https://developers.openai.com/api/docs/guides/realtime-vad
- Whisper non-streaming + LocalAgreement (~3.3 s): https://arxiv.org/abs/2307.14743 ; https://github.com/ufal/whisper_streaming ; faster-whisper VAD filter: https://github.com/SYSTRAN/faster-whisper
- Deepgram endpointing/partials: https://developers.deepgram.com/docs/understand-endpointing-interim-results ; streaming latency/EOT: https://developers.deepgram.com/docs/measuring-streaming-latency ; Flux eager EOT: https://developers.deepgram.com/docs/flux/voice-agent-eager-eot
- OpenAI TTS streaming formats: https://developers.openai.com/api/docs/guides/text-to-speech ; `stream_format` param: https://github.com/openai/openai-python/blob/main/src/openai/resources/audio/speech.py ; OpenAI-compat whole-burst case study (2.85-3.7 s vs 0.92-0.95 s): https://github.com/NousResearch/hermes-agent/issues/149699
- ElevenLabs TTFA math / Flash figures / budget table: https://elevenlabs.io/docs/eleven-api/concepts/audio-streaming ; https://elevenlabs.io/docs/eleven-api/concepts/latency-methodology ; https://elevenlabs.io/blog/voice-agent-latency-optimization
- Riva first-audio-chunk methodology, <300 ms bar: https://docs.nvidia.com/deeplearning/riva/archives/2-24-0/public/tts/tts-performance.html ; https://developer.nvidia.com/blog/introducing-riva-a-gpu-accelerated-sdk-for-developing-speech-ai-applications/ ; ASR deployment: https://developer.nvidia.com/blog/solving-automatic-speech-recognition-deployment-challenges/
- 196 ms startup buffer for burst-then-gap OpenAI-compat TTS: https://github.com/huggingface/speech-to-speech/blob/main/docs/openai-compatible-tts.md
- 800 ms budget framings: https://builder.aws.com/content/3JDFAfXBiuwPP5MPzgf4RUWSAIp/the-800ms-rule-budgeting-latency-for-a-real-time-voice-agent-on-aws ; https://soniox.com/wiki/voice-agent-latency-budget ; https://www.twig.so/blog/voice-ai-agents-latency-budget-800ms ; real-world median 1.4-1.7 s (secondary): https://www.sippulse.ai/blog/voice-ai-agent-architecture-stt-llm-tts
- Repo-local: internal/voice/AGENTS.md (contracts + measured overlaps 13.70/10.94/6.70 s), docs/workflows/voice-sidecar.md, docs/workflows/voice.md, scripts/voice-gap-analyze.py (gap measurement tool for acceptance evidence)

*Unverified inherited figure, flagged: the 12.7 s turn latency came from the audit
brief's field measurement, not from repo evidence; repo-verified numbers in the same
class are listed in §3.*
