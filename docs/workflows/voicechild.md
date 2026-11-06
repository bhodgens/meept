---
title: Voicechild
---

# Voicechild (jargo voice child)

Status: In progress. Part of the voice phone channel; see
docs/workflows/voice.md and docs/workflows/voice-sidecar.md.

## Overview

internal/voicechild and cmd/meept-voice are the child side of the voice
sidecar: a separate binary that owns the ears (streaming speech-to-text),
turn-taking and barge-in detection, and the mouth (text-to-speech) for a
phone call. The child never sees Telnyx and never holds secrets across
restarts: the daemon hands it provider keys over the unix socket at call
setup, and the child keeps them in memory only.

## Problem

The daemon must not run audio models or vendor sockets inline (a linked
model cannot be unloaded, and a slow call would block chat traffic). A
separate child isolates the model heap and lets the daemon kill it idle
without touching the main process.

## Behavior

- internal/voiceframe: the C1 frame codec shared by daemon and child
  (stdlib-only; pinned by a test).
- internal/voicechild: Child connects the shared text socket, routes
  setup/say/clear/end per call, one jargo-backed pipeline per call.
  Providers: Deepgram streaming STT and OpenAI TTS adapters behind the
  STTProvider/TTSProvider seams (internal/voicechild/providers), with a
  single reconnect per call, streamed first audio, and chunked s16le
  resampling to the negotiated rate.
- cmd/meept-voice: --text-socket and --audio-dir flags; the start token
  arrives on stdin, never argv or environment.

## Configuration options

Provider selection and model names come from the daemon's voice config
(see docs/workflows/voice.md) and ride the setup and keys frames. The
child reads no meept config files.

## Observability

Child RSS is logged by the daemon at spawn, first call, and idle stop.
Key material never appears in logs: the keys frame redacts itself.

## Edge Cases

- Missing provider key: that call fails with a named error; other calls
  and the child keep running.
- Vendor websocket drop: exactly one reconnect per call, then the stream
  ends; audio spoken during the gap is lost (same trade as jargo's
  wsservice).
- jargo is imported only here (and internal/voiceframe never): pinned by
  TestNoJargoImport.
