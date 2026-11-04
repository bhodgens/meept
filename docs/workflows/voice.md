---
title: Voice
---

# Voice (phone sidecar)

Status: Implemented (plan docs/plans/20261102-voice-sidecar/ complete;
see the as-built section of the spec). Authoritative design:
docs/workflows/voice-sidecar.md. Implementation plan:
docs/plans/20261102-voice-sidecar/.

## Overview

Voice is the phone channel for meept employees. An employee answers
inbound calls and dials out through Telnyx, talks through a separate
jargo voice child (ears, turn-taking, barge-in, mouth), and speaks only
sentences a supervisor employee allows. Multiple voice instances run at
once to different numbers.

## Problem

Push channels are one-way text. A phone call is realtime audio with
interrupt, and a caller expects the assistant to know the day's context.
Neither exists in the text chat path.

## Behavior (key components)

- internal/voice/telnyx: call control client, webhook + media socket
  Ed25519 signature verification, frame ordering handled above it.
- internal/voice: call manager (lifecycle keyed by call_control_id),
  media splice (reorder window, silence fill, 20 ms pacing, barge-in
  clear), supervisor gate (fail-closed, hard timeout, never parks),
  outbound dialer (allowlist, effects reservation, audit before dial),
  day brief (local-midnight activity + call transcripts), registry
  (one live call per employee and per target number).
- cmd/meept-voice + internal/voicechild: the separate child binary.
  jargo is imported ONLY here. Keys reach the child over the unix
  socket, never env or argv.

## Configuration options

All under `voice` in meept.json5. enabled defaults false. See
docs/workflows/voice-sidecar.md Config section for the full key list.

## Observability

voice.call.started / progress / ended bus events (WS class event).
Child RSS logged at spawn, first call, idle stop. Call rows and
transcripts in $MEEPT_HOME/voice/calls.db.

## Edge Cases

- No-answer outbound releases the plan slot within timeout_secs.
- Barge-in cancels only the interrupted call's drafts.
- Missing ONNX runtime or public key fails closed: no call is answered.
- Untracked call IDs are refused; hangups are idempotent.
