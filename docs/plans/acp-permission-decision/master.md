# ACP Permission Decision Model — Master Plan

Tree: `docs/plans/acp-permission-decision/` (master.md + leaves 01–03).
Repos basis: `docs/plans/decision-model/` (COMPLETE 2026-10-15) — this tree
adds a THIRD consumer of that substrate. No changes to the decision-model
tree's shipped contracts.

## Problem statement

`internal/acp/session.go` `handlePermission` (line 317) answers every ACP
`session/requestPermission` by mode only: `permissive` approves everything
(`allow-once`), `deny` cancels everything. Neither uses the decision model.
The user wants permission requests gated by a Jev-style typed decision
(`llm.DecisionClient`), tunable INDEPENDENTLY of the other decision-model
consumers (`decision_gate_enabled`, `compaction.decision_enabled`), while
reusing the existing substrate (slot, resolver accessor, DecisionClient,
noul-question pattern from `internal/tools/builtin/decision_gate.go`).

## User decisions (normative, 2026-10-19)

1. Third mode, not new machinery: `permission_mode` gains `"decision"`.
   `permissive`/`deny` keep byte-identical behavior.
2. Knobs live under `[acp]` only. Enabling ACP decision mode requires
   decision_model slot set, but does NOT read or flip
   `decision_gate_enabled` / `decision_escalation_threshold` /
   `compaction.decision_*`. Independence is by config-section ownership.
3. Fail-closed. When the decision layer is unavailable, errors, or returns
   `no`, the request is cancelled (deny). This is the OPPOSITE polarity of
   the tool-call decision gate (fail-open) and is deliberate: a permission
   request guards an external side effect inside someone else's agent
   session; a down decider must not silently grant it.

## Frozen contracts

### Contract A — config (leaf 01 owns)

```go
// internal/config/schema.go ACPConfig gains ONE field:
DecisionThreshold float64 `json:"decision_threshold" toml:"decision_threshold"`
// Valid range 0-1. Default 0.5 (DefaultConfig). Only read when
// permission_mode == "decision".
```

- `ACPConfig.Validate()`: switch gains `case "permissive", "deny", "decision"`;
  plus `DecisionThreshold` bounds check (0 < t <= 1 required when mode is
  "decision"; outside the decision mode, 0 is accepted = unused).
- DefaultConfig: `PermissionMode: "permissive"` stays; `DecisionThreshold: 0.5`.
- `config/meept.json5` template `[acp]` section: add
  `decision_threshold: 0.5` + comment documenting mode "decision" and
  fail-closed semantics.
- `make docs-generate` after the schema change.

### Contract B — acp decision seam + handlePermission (leaf 02 owns)

```go
// internal/acp/decision.go (new file) — Decisioner is the session seam:
type Decisioner interface {
    Available() bool
    Ask(ctx context.Context, q llm.DecisionQuestion) (*llm.DecisionResult, error)
}
// *llm.DecisionClient satisfies it. Mirrors builtin.decisionAsker
// (decision_gate.go:51) — same shape, NOT shared code: builtin and acp
// are sibling consumers; acp must not import builtin.
```

- `SessionConfig` gains `Decision Decisioner` + `DecisionThreshold float64`.
  Zero values = disabled lane.
- `Manager` gains `SetDecisionClient(d Decisioner)` (nil-safe setter, repo
  setter rule) + field; `GetOrCreate` stamps both into `SessionConfig` before
  calling `startFn`. `startFn` override path in tests still wins.
- `handlePermission` rewrite, dispatch on `s.cfg.PermissionMode`:
  - `permissive` / `deny`: CURRENT behavior, byte-identical (no new parses
    on those paths).
  - `decision`:
    1. `Decision == nil || !Decision.Available()` → deny (cancelled outcome),
       emit event, Warn log `"acp permission denied: decision layer unavailable"`.
    2. Build noul question: `Type: "noul"`, `Options: ["allow","deny"]`,
       `Prompt: "May this action run?"`, `State`: agent id + trimmed raw
       permission params (bounded 300 runes, same budget as
       summarizeToolArgs) — no params schema parsing in v1.
    3. Ask with `context.Background()` + constant `decisionAskTimeout = 15s`.
    4. Approve ONLY when `err == nil && res.Answer == "allow" && res.Confidence >= threshold`.
       Every other result (error, parse failure, answer "deny", low
       confidence) → cancel. Never an infinite wait; the timeout IS bounded.
    5. Emit `SessionEvent{Kind: "permission_decision",
       Text: "allow"|"deny"|"<reason>"}`. Existing `permission_request`
       event stays, now carrying the decision text instead of hardcoded
       approved/denied on the decision lane only.
  - Confidence note (pinned so nobody "fixes" it wrong): today's
    DecisionClient degrades to `Confidence = 1.0` argmax-only
    (decision_client.go:171-188). So the ANSWER is the real signal now;
    the threshold becomes a live second bar only when logprobs land
    (upstream TODO decision-model leaf 03/04). Contract keeps BOTH checks
    forward-compatible.
- Test lane: extend EXISTING files only (repo e2e policy — no new unit test
  files for features): `internal/acp/session_test.go` gains decision-lane
  cases via a stub Decisioner; `e2e/suites/acp-wire` gains a decision-mode
  scenario (fake agent sends requestPermission; injected stub decider allows/
  denies; assert wire reply + event).

### Contract C — daemon wiring (leaf 03 owns)

- `internal/daemon/acp_register.go`: `applyACPFromConfig(cfg config.ACPConfig,
  dec acp.Decisioner)` — pass the decision client only when
  `cfg.ACP.PermissionMode == "decision"`; manager.SetDecisionClient(nil-safe).
  Disabled path stays zero-log (existing contract).
- `internal/daemon/components.go:2840`: build/reuse the DecisionClient.
  components.go ALREADY constructs `llm.NewDecisionClient(c.LLMResolver, ...)`
  for the gate (line ~973). Reuse the same client instance for ACP — construct
  it once and hand the pointer to both consumers (Optimization Posture:
  share genuinely-sharable state). If the gate construction is conditional on
  `DecisionGateEnabled`, hoist client construction so it exists when EITHER
  consumer needs it (gate OR acp decision mode), still zero-overhead when
  neither does (Available()==false, slot empty).
- `acp_wiring_test.go` extended (existing file) for: mode=decision + slot set
  → manager carries decider; mode=permissive → nil decider; disabled → inert.
- `docs/workflows/acp.md` + `docs/configuration/` gain the decision mode.
- AGENTS.md root review per maintenance rule (no new package; likely no
  change beyond config bullet — leaf verifies).

## Non-goals

- No per-agent-catalog permission overrides (`acp_agents.json5` entry fields
  stay untouched; global mode only in v1).
- No human-in-the-loop relay of ACP permission requests to the GUI/TUI
  (separate surface decision; documented as deferred).
- No change to `permissive`/`deny` semantics.
- No params-schema parsing of requestPermission payloads in v1.

## Leaves

- **01** `01-config.md` — Contract A only. Files: internal/config/schema.go,
  config/meept.json5, docs/configuration/*, generated docs. Branch: config.
- **02** `02-acp-decision-lane.md` — Contract B. Files: internal/acp/
  decision.go (new), session.go, manager.go, session_test.go,
  e2e/suites/acp-wire/* (extend existing suite + manifest entry already maps
  internal/acp/ -> acp-wire). Branch: acp.
- **03** `03-daemon-wiring.md` — Contract C. Files: internal/daemon/
  acp_register.go, components.go (minimal hunks), acp_wiring_test.go,
  docs/workflows/acp.md. Depends on 01+02. Branch: daemon.

Wave order: 01 → 02 → 03 → integration gates. SERIAL (02 consumes 01's
field; 03 consumes 02's seam).

## Tracking table

| Leaf | Status | Commit | Notes |
|------|--------|--------|-------|
| 01 | PENDING | | |
| 02 | PENDING | | |
| 03 | PENDING | | |
| Integration | PENDING | | |

## Integration gates

- `go build -o bin/meept-daemon ./cmd/meept-daemon && go build -o bin/meept ./cmd/meept`
- `go test -p 2 ./internal/config/... ./internal/acp/... ./internal/daemon/...`
- `make e2e-fast-area AREA=acp-wire`
- `make docs-generate` + `make docs-check`; `make graphs` if bus/HTTP payload
  keys changed (permission_decision events ride the existing acp tool-result
  path — expect NO topology change; verify graphs-check green either way).
- Pre-commit hooks per commit; single-line commit messages (consent-gate
  trap), explicit pathspecs.
