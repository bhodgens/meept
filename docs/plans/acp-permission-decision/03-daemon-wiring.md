# Leaf 03 — daemon wiring + docs (Contract C)

Parent: `master.md` (Contract C). Depends on leaves 01+02 (fields +
`SetDecisionClient` exist — read their committed APIs first; on-disk
signatures win over this doc).
Branch: daemon.

## Scope

Connect the resolved `decision_model` slot to the ACP manager when
`permission_mode == "decision"`, sharing ONE DecisionClient instance with the
tool-call gate where both are enabled.

## Tasks

### Task 1: failing tests (existing file)

`internal/daemon/acp_wiring_test.go` (package daemon, never NewComponents —
existing convention):
- mode `decision` + non-nil Decisioner → `applyACPFromConfig` returns manager
  whose sessions get the decider (assert via exported inspection or the
  tool-side `read` event stream in an existing test seam; if none, assert the
  setter was applied through a small exported test hook you add to
  acp_register.go only).
- mode `permissive` → manager gets nil decider even when a Decisioner is
  passed.
- `[acp] enabled=false` → nil manager, nil error, ZERO log lines (keep
  existing contract green).

### Task 2: acp_register.go

- Signature: `applyACPFromConfig(cfg config.ACPConfig, dec acp.Decisioner)`
  (update call site in Task 3; keep the old zero-log disabled contract).
- After manager construction: if `cfg.PermissionMode == "decision"` &&
  dec != nil → `mgr.SetDecisionClient(dec)`; log ONE Info line
  `acp permission decision lane armed threshold=<t>` (only in decision mode).

### Task 3: components.go minimal hunks

- Hoist/locate the existing `llm.NewDecisionClient(c.LLMResolver, ...)`
  (gate lane, ~line 973). Refactor so ONE client instance is constructed when
  EITHER the tool gate OR `acp.permission_mode == "decision"` is on
  (decision_model slot empty ⇒ Available()==false ⇒ still inert, zero
  overhead). Pass the same pointer to gate hook and `applyACPFromConfig`.
- `git diff internal/daemon/components.go` must show feature-only hunks.
- Nil-DecisionClient ACP path must remain byte-identical for permissive/deny.

### Task 4: docs + surfaces

- `docs/workflows/acp.md`: permission modes section gains `decision` with the
  fail-closed table (answer allow + confidence ≥ threshold → approve; else
  cancel) and the independence note (knobs under [acp], separate from
  decision_gate_*).
- Root AGENTS.md review per maintenance rule (ACP config bullet may gain one
  clause; no package change).
- `make graphs` only if payload keys change (expect none; verify
  `make graphs-check`).

## Verification

```
go build ./... && go test -p 2 ./internal/daemon/ -run 'ACP|Decision' -count=1
make e2e-fast-area AREA=acp-wire
```

## Live gate (required before COMPLETE)

Scratch daemon (MEEPT_HOME temp, remapped ports incl. classifier runtime
port; local models only): set `[acp] permission_mode="decision"` + a fake
catalog agent, boot, `acp_agent launch/send` to trigger a requestPermission,
assert `permission_decision` event in the tool read output and the daemon
log line. Cite evidence in the tracking table.

## Stop clause

If the gate-lane client construction cannot be hoisted without touching the
decision-model tree's committed semantics, STOP and report — do NOT change
`decision_gate_enabled` behavior.
