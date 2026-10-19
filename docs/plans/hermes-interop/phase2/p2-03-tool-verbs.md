# Leaf p2-03 — tool verbs + capability gating

Contract P2-C in p2-master.md. OWN: internal/tools/builtin/hermes_agent.go
(+test), internal/daemon/hermes_register.go ONLY if the AutoApprove knob
must reach the tool (prefer passing config through the existing
RegisterHermesTools signature — read it first; extend minimally).
Depends on p2-01 (CreateSession/RunApproval/Jobs/SessionKey landed) —
READ the landed client API and trust code over this doc.

Tasks:
1. `approve` verb with the confirmation two-phase (AutoApprove false ->
   builtin.ConfirmationResponse shape first; confirmed=true re-invoke ->
   RunApproval). AutoApprove true -> forward directly.
2. `jobs` verb {action: list|get|create|pause|resume|run}.
3. `chat` auto-create session when session_id empty; result includes
   created_session_id. `session_key` arg on chat+run.
4. Capability gating: mutating verbs check cached Capabilities flags
   (HasFeature helper from p2-01); flag false -> readable error naming
   the feature, no HTTP. Read-only verbs ungated.
5. Update tool Description + schema for new verbs/args; keep the
   verify-before-acting note.
6. Tests: per-verb dispatch, approve two-phase (both AutoApprove modes),
   gating matrix (flag true/false per verb), chat auto-create path,
   error-mapping reuse. httptest stubs, no live gateway.
7. Sweep: go test -p 2 ./internal/tools/... ./internal/daemon/ ; both
   binaries build. Explicit-path commit:
   'feat(tools): hermes approve/jobs verbs + capability gating (hermes-interop p2-03)'.

Report: RED/GREEN, verb+arg table as landed, gating matrix, files.
