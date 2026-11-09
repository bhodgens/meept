# NEXT-STEPS: voice local-leg audio clarity fix (2026-11-09)

User-reported: `meept voice local --employee companion --label fresh1`
produced jittery, unintelligible reply audio. Root-caused to THREE stacked
defects; all fixed, committed, and whisper-verified end to end.

## Done (committed)

1. **oto context rate** — internal/tts/player.go + synthesizer.go.
   oto v3 plays raw bytes at the CONTEXT rate (no WAV parsing, no
   resampling); context was hardcoded 22050 while the leg carries 16k.
   `PlaybackConfig.SampleRate` added; OtoPlayer passes MicSampleRate.
   Committed in c8650bc0 (sibling tree) as "16k speaker rate fix".
2. **SpeakerSink per-burst WAV headers** — THE jitter. Bursts are now RAW
   PCM; CLI drains flush at stream end only. Was swept into a sibling's
   deadcode commit 28580cf8 (whole-index sweep of uncommitted work — the
   changes ARE in the tree, attributed to that commit; content verified).
3. **tools_forbidden wildcard** — 3bb7243e. Hire validation stripped
   `["*"]` as an unknown tool and enforcement matched literals only, so a
   talk-only employee ran schedule_list mid-call.
4. **Reply-guard generic JSON keys** — 50b4309e. schedule_list's
   `{"jobs": [...]}` shape now classifies as a machine dump.

## Verification evidence

- Live file-mode call (isolated daemon, --label fresh5): whisper-cli
  transcribes the reply WAV verbatim ("Hold on. Let me check. ... I don't
  have access to your calendar..."), zero nested RIFF markers.
- internal/employee green; reply-guard tests green; internal/voice green
  except a transient sibling breakage (see below).
- Real-home daemon boot fixed: ~/.meept/models.json5 "local" provider now
  declares lfm-1.2b-q8 / lfm-8b-mlx-4bit / lfm-combined-sft with
  context_limit 32768 so the F-D8 gate passes when the bundled repo
  config merges (dev-binary-from-repo-cwd case). Boot verified from repo
  cwd: "Local classifier runtime healthy" x3, "daemon: running".

## Open items

1. **Tree churn (NOT mine)**: parallel deadcode-wave commits broke
   internal/daemon (agent.ApplyPreset undefined — sibling editing
   components.go) and internal/agent tool-action completeness tests
   (retain/recall/reflect missing from executor.go ToolActionMap).
   TestChildProcParentDeath fails only because of the daemon compile
   break. Re-run `go test -p 2 ./internal/voice` after the wave lands.
2. **qwen3 TTS latency**: socrates:8001 took 90-120 s per sentence — the
   45 s silence timeout ends the call before audio arrives. For
   deterministic local audio use tts_provider piper (HAL voice models at
   ~/git/piper-voice-models/). socrates server load needs investigation
   (network probes need operator consent).
3. **Commit hygiene incident**: my uncommitted audio fix was swept into a
   sibling's commit 28580cf8 by a bare `git commit` without pathspec.
   Content is intact at HEAD; attribution is wrong. If history matters,
   note it in the eventual PR description — no rebase recommended on a
   shared tree.
