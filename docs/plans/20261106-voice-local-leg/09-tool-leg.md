# voice_call tool: leg + voice args - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks below using TDD. Do
> NOT commit — the orchestrator handles git. Explore with search_files or
> terminal cat, never read_file. Do NOT read back files you just wrote.

## Meta

- **Parent:** master.md
- **Scope:** Extend the employee-facing voice_call tool with `leg`
  ("phone"|"local") and `voice` (TTS voice/model override) per CL4's
  selection precedence, routed through the same gate stack.
- **Dependencies:** leaf 07 (LocalLeg) for the local branch; leaf 04
  (ResolveProfile override param) for the voice arg.
- **Estimated Context:** 55K
- **Concurrency Group:** D
- **Files this leaf owns:** internal/tools/builtin/voice_call.go (+ its
  test), internal/tools/builtin voice-provider factory glue (grep
  `builtin.SetVoiceProvider` or the "voice" toolprov provider for the
  construction site), internal/voice/dial.go DialFlow ONLY if the leg
  needs a DialDeps voice-override field (additive).

## Goal

An employee can decide, per call: "call this human on the phone with voice
X" or "run a local voice session (the device-side leg) with voice Y for my
goal loop". Policy gates (allowlist, registry, effects, audit) stay
entirely in the dialer/LocalLeg - the tool remains a thin shim (its doc
comment's founding rule).

## Context

Key file: internal/tools/builtin/voice_call.go - VoiceCallTool with
DialerFunc + AgentIDFunc, JSON schema map, Execute arg validation.
LocalLeg routing needs a second injected seam:
`LocalFunc func(ctx, employeeID, label, ttsVoice string) (LocalSession, error)`
(shape: returns call_id/session_id) - defined structurally in the builtin
package to keep the import direction builtin -> voice allowed (builtin
already imports voice elsewhere? verify; if not, use a func-shaped seam
returning plain strings, never a voice type, to avoid a new import edge).
Disabled-local behavior: LocalFunc nil -> tool call fails closed with
"voice local leg disabled" (mirrors the nil-dial rule).

## Interface Contracts (From Parent)

CL4 precedence (verbatim): voice = arg `voice` > per-leg config > provider
default; leg = arg `leg` > voice.default_leg; phone-leg `target` required
(E.164); local-leg target OPTIONAL (default label = employee id, and the
local leg's client still picks the device via the RPC open it initiates -
NOTE: an employee-initiated local call opens the leg for WHATEVER client
calls voice.local.open next with matching session, i.e. the tool starts
the session-side half; document that semantics in the tool Description).

## Tasks

| Task ID | Objective | Files | Verify | RED reason | PASS |
|---|---|---|---|---|---|
| T1 | Test: schema exposes leg + voice (optional); invalid leg refused; phone path unchanged when leg omitted + default_leg phone (byte-compatible results incl. request_id idempotency) | internal/tools/builtin/voice_call_test.go | go test -p 2 ./internal/tools/builtin/ -run TestVoiceCall | schema lacks keys | PASS |
| T2 | Test: leg=local routes to LocalFunc with the voice override; nil LocalFunc fails closed; target not required for local | same | -run TestVoiceCallLocal | no seam | PASS |
| T3 | Implement schema + validation + routing | voice_call.go | T1/T2 | - | PASS |
| T4 | Wire the seam: daemon passes LocalLeg-backed LocalFunc into SetVoiceProvider (additive param) | builtin factory glue + internal/daemon wiring site (grep SetVoiceProvider) | go build ./... && go test -p 2 ./internal/tools/builtin/ -run TestVoiceCall | - | PASS |
| T5 | Tool Description updated (lowercase UI text rule for user-visible strings; ASCII) | voice_call.go | existing tool-description test if any | - | PASS |

## Interface Contract (What This Leaf Exposes)

- New optional tool args leg/voice; new LocalFunc seam shape; provider
  registration param.

## Self-Verification Checklist

- [ ] No policy logic added to the tool (grep: no registry/effects calls)
- [ ] Existing voice_call tests pass unchanged
- [ ] Description length reasonable for the tool list

## Review Checklist

- [ ] Precedence order matches CL4 exactly
- [ ] nil-guard on the new seam (repo setters rule if it's a Set*)
- [ ] ASCII only; no artifacts
