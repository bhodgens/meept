# Rumble native streaming docs - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks. Do NOT write code.
> Do NOT commit. Explore with search_files/terminal. Write once and stop.

## Meta

- **Parent:** ../master.md
- **Scope:** Document native streaming and the proxy-workaround change
- **Dependencies:** gates validated
- **Estimated Context:** 40K
- **Concurrency Group:** C
- **Audit references:** none

## Goal

Explain to operators and agents how native Rumble streaming replaces the
client-side pseudo-stream proxy, and document that the live server is a
transformers FastAPI server (not vLLM) with its own protocol.

## Context

- `docs/workflows/voicechild.md` - provider docs
- `docs/workflows/voice-sidecar.md` - sidecar as-built
- `config/meept.json5` - config template (source tree; the INSTALLED
  `~/.meept/meept.json5` is edited by the orchestrator in Phase 4)
- root `AGENTS.md`, `internal/voice/AGENTS.md` - invariant files
- `docs/research/rumble2flash-evaluation.md` - evaluation record

## Verified Anchors

- `docs/workflows/voicechild.md` - Rumble STT section
- `docs/workflows/voice-sidecar.md` - Rumble sidecar section

## Interface Contracts (From Parent)

### What This Leaf Exposes

Documentation only; no code symbols.

### What This Leaf Consumes

- C2 WS protocol, C4 factory posture, C5 operator config - see ../master.md

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Voicechild docs | Modify: `docs/workflows/voicechild.md` | `git diff --check` | n/a | PASS | Given docs; When read; Then native WS protocol and proxy removal explained |
| T2 | Sidecar docs | Modify: `docs/workflows/voice-sidecar.md` | `git diff --check` | n/a | PASS | Given docs; When read; Then live server endpoint + operator config documented |
| T3 | Config template + invariants | Modify: `config/meept.json5`, root `AGENTS.md`, `internal/voice/AGENTS.md` | `git diff --check` | n/a | PASS | Given files; When read; Then stt_provider=rumble example and native-streaming invariant present |

T1 detail: replace the batch/chat.completions Rumble description with:
- provider connects to `ws://<stt_base_url>/v1/audio/transcriptions/stream`
- streams s16le mono 16 kHz PCM as binary WS frames (no base64/WAV)
- server emits self-segmented `final` events (words stable across consecutive
  decodes) and `partial` for the changing tail; `end` terminates
- proxy workaround change: `PseudoStreamSTT` snapshots + re-POSTs a growing
  buffer (O(n^2), no real incremental input). With native streaming it is
  BYPASSED for rumble: no re-POST, genuine server-stable finals for barge-in
  and endpointing, and `stt_partial_ms` should be 0. Native vendors (deepgram,
  rumble) are never wrapped; whisper can still opt into the proxy.
- `chat_template_*` selectors are inert on the transformers server; kept for
  a future vLLM deployment.

T2 detail: document the live server facts: FastAPI + transformers, host
socrates:8000, GPU1 beside qwen3-tts, no vLLM, no Authorization, 0.8 s decode
cadence after >=1.0 s of audio, 45 s cap, silence produces no hallucinated
text. Operator example: `stt_provider: "rumble"`, `stt_base_url:
"http://socrates:8000"`, `stt_partial_ms: 0`.

T3 detail: config template comment update (stt_base_url vendor list already
generic; add a note that rumble now uses a native WS stream endpoint). Add ONE
compact invariant per AGENTS file matching style: Rumble is a native streaming
STT vendor (partial/final/end), pseudo-stream proxy bypassed for native vendors,
whisper default unchanged.

IMPORTANT: use ASCII hyphen '-', not em dash. Keep prose short.

## Self-Verification Checklist

- All tasks done; cross-references resolve; diff check clean
- No code files touched; no scope creep; no line-number corruption

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched
2. Verify commands + output
3. Deviations

## Review Checklist (For Review Agent)

- Docs accurate to contracts; invariants updated; proxy change explained
- No code changes; no debug artifacts

Output: APPROVED or specific gaps with file + line references.

## Notes

The orchestrator edits the INSTALLED `~/.meept/meept.json5` separately; do not
touch any file under `~/.meept`.
