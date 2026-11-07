# Flutter GUI voice parity (leaf 13b, part 2) - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks below using TDD where
> the repo's Flutter test rig supports it. Do NOT commit - the orchestrator
> handles git. Explore with search_files or terminal cat, never read_file.
> GATE: RESOLVED - master.md OQ1 answered 2026-11-07: parity is an explicit
> requirement. This leaf ships the GUI half of push-to-talk; the TUI half
> is leaf 13a (13a-tui-voice.md). Neither is a deviation.

## Meta

- **Parent:** master.md
- **Scope:** A voice feature in the Flutter GUI that starts, monitors, and
  stops a local voice session, driving the existing `voice.local.*` RPC
  control plane through the daemon's HTTP RPC-call bridge. Browser-side
  audio capture/playback uses the Flutter web APIs; no daemon audio code
  changes.
- **Dependencies:** leaf 08 (voice.local.open/end/status registered on the
  RPC server, internal/rpc/voice_local.go:128-130), leaf 12 (docs already
  describe the control plane). Does NOT depend on leaf 13a.
- **Estimated Context:** 90K
- **Concurrency Group:** F (part 2; no file overlap with 13a - 13a owns
  internal/tui, this leaf owns ui/flutter_ui)
- **Files this leaf owns:** ui/flutter_ui/lib/features/voice/ (new),
  ui/flutter_ui/lib/services/voice_service.dart (new), route/nav
  registration files touched by existing features (match their pattern),
  docs/workflows/voice.md GUI section (small)

## Transport contract (what already exists - reuse, do not rebuild)

- The daemon enables `/api/v1/bus/call` when the RPC server exists: it
  dispatches ANY registered RPC method over HTTP
  (internal/daemon/daemon.go:1588-1593, `http.WithRPCCall(rpcServer.CallMethod)`).
- `voice.local.open`, `voice.local.end`, `voice.local.status` are
  registered handlers (internal/rpc/voice_local.go:128-130). Their
  request/result shapes are documented at the top of that file
  (voice.local.status -> {calls:[{call_id, employee_id, target,
  since_rfc3339, leg}]}) and in docs/workflows/voice-sidecar.md (CL8,
  rev 3).
- The GUI already talks HTTP + WebSocket to the daemon (chat_provider,
  metrics_provider patterns; base host from
  ui/flutter_ui/lib/core/constants.dart).

## Tasks

| Task ID | Objective | Files | Verify | RED reason | PASS |
|---|---|---|---|---|---|
| T1 | VoiceService: typed open/end/status calls to POST /api/v1/bus/call with method voice.local.*; auth/header handling matches chat_provider's pattern | services/voice_service.dart | flutter analyze + unit test with a fake HTTP client | no service | PASS |
| T2 | Voice feature page: employee picker (from voice.local-backed employee list or existing agents provider), start button -> open, live session list from status poll (2s), end button per session | features/voice/voice_page.dart + widgets | flutter analyze; widget test for start/end states with fake VoiceService | no feature | PASS |
| T3 | Route + nav registration following the existing feature pattern (grep how sessions/ or tasks/ register); permission/error shapes surface the disabled-leg envelope verbatim (voice.local.enabled=false) | route registration file(s) | flutter analyze; manual route assertion in widget test | unreachable page | PASS |
| T4 | Browser audio capture/playback: mic capture via Flutter web (getUserMedia / record package IF already a dependency - do NOT add new pub dependencies without orchestrator approval), playback of returned WAV via existing pattern | services/voice_audio.dart | flutter analyze; capture/playback lifecycle behind a fake in widget test | no audio path | PASS |
| T5 | docs/workflows/voice.md: short GUI section (page path, how to open, parity note: same RPC plane as TUI/CLI) | docs/workflows/voice.md | cited paths exist | - | PASS |
| T6 | gofmt-equivalent (dart format), analyze clean, no TODO/FIXME/debug artifacts; ASCII-only in all hand-written added lines (hyphens not em-dashes) | - | dart format --output=none --set-exit-if-changed, flutter analyze, non-ASCII sweep on added lines | - | clean |

## Interface Contract (What This Leaf Exposes)

- `VoiceService` (services/voice_service.dart): open(employeeId, label) ->
  call id; end(callId); status() -> list of live local sessions.
- The voice feature page is reachable from the existing nav; every RPC
  error shape passes through verbatim (no GUI-side re-mapping).
- No daemon-side changes: zero Go files touched. If the bridge or a
  handler shape is wrong for the GUI, STOP and report - do not patch the
  daemon in this leaf.

## Self-Verification Checklist

- [ ] Zero Go files modified (transport reuse, not transport change)
- [ ] No new pubspec dependencies without orchestrator approval
- [ ] ASCII-only in hand-written added lines
- [ ] Error envelopes from voice.local.* surface verbatim (esp. disabled leg)

## Review Checklist

- [ ] Parity satisfied: TUI (13a) + GUI (this leaf) both drive voice.local.*
- [ ] docs/workflows/voice.md GUI section matches the landed page
- [ ] No audio code duplicated from leaf 08 (browser-side capture only)
