# Jev Score Gate (scorer + fail-closed threshold hook) - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD (e2e tier per repo policy). Do NOT
> commit - the orchestrator handles all git operations after review. Do NOT
> use read_file on existing source files - explore with search_files or
> terminal cat. After writing a file, do NOT read it back - write once and
> stop. Report what you built, files touched, and any deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** The graded command-safety score on the decision client plus the
  FAIL-CLOSED threshold gate hook on shell calls, opt-in via config.
- **Dependencies:** R0-research.md (findings inform default threshold band
  comments only; contracts are frozen regardless)
- **Estimated Context:** 75K
- **Concurrency Group:** B
- **Audit references:** proposal 12 (close the gap)

## Goal

Add `DecisionClient.ScoreCommand` answering a fixed rubric question over a
byte-stable serialized command context, and a new `internal/jevgate` package
implementing a fail-closed `agent.BeforeToolCallHook` for shell tool calls:
score + confidence + parsed answer all clear the thresholds -> allow through
to lower security stages; ANYTHING else (error, nil, degraded, unparseable,
below threshold) escalates to the existing confirmation flow. Gate is opt-in
(config, default false).

## Context

- internal/llm/decision_client.go - `DecisionClient.Ask` (line ~138) rides the
  classifier resolution; ScoreCommand follows the same pattern with a
  numeric-rubric question.
- internal/tools/builtin/decision_gate.go:150-158 - the two-bar gate pattern
  (exact affirmative answer AND Confidence >= threshold). The Confidence-only
  gate was a real bug class (degraded argmax-only clients always report
  Confidence 1.0). This leaf's gate checks the parsed SCORE ANSWER too.
- internal/tools/builtin/decision_gate.go:56-66 - `DecisionGateHook`
  implements `agent.BeforeToolCallHook`; deny converts to the existing
  confirmation flow. Mirror that conversion.
- internal/security/engine.go:490+ `CheckForAgent` - staged pipeline; the gate
  hook runs upstream at the tool-call layer, same as the decision gate.
- internal/tools/builtin/shell.go:841 classifyRisk / :963 GetRiskLevel -
  categorical risk; the gate complements it, never replaces it.
- internal/llm/AGENTS.md decision-lane notes - polarity divergence is
  deliberate (ACP fail-closed, builtin gate fail-open). This new surface is
  FAIL-CLOSED by documented decision: high-stakes local side effects match
  permission-table ASK semantics. Put that rationale in the package doc.
- internal/daemon/components.go:6796 - `shellTool.SetPermissionTable(permTable)`
  shows the wiring seam neighborhood; the decision gate hook wiring is nearby.

## Verified Anchors

- internal/llm/decision_client.go:138 - `func (d *DecisionClient) Ask(ctx context.Context, q DecisionQuestion) (*DecisionResult, error)`
- internal/tools/builtin/decision_gate.go:150-158 - answer AND confidence two-bar check (grep: `decisionGateAffirmative`)
- internal/tools/builtin/decision_gate.go:33-56 - deny-to-confirmation conversion comment + hook struct
- internal/security/engine.go:504-510 - Stage 1.5 shell permission table (ask routes to confirmation flow)
- internal/tools/builtin/shell.go:841,963 - classifyRisk / GetRiskLevel
- internal/daemon/components.go:6796 - `shellTool.SetPermissionTable(permTable)` wiring site
- config/models.json5:88,186 - `lfm-combined-sft` classifier A/B alternate (score question rides classifier resolution)

## Interface Contracts (From Parent)

### What This Leaf Exposes

- [C1 CommandScoreResult + ScoreCommand] - full definition in master.md
- [C2 SerializeCommandContext] - full definition in master.md (byte-stable)
- [C3 ScoreGateHook] - full definition in master.md (fail-closed semantics)
- Config keys (JSON5, opt-in default false): `jev_command_gate.enabled`,
  `jev_command_gate.score_threshold`, `jev_command_gate.confidence_threshold`
  (config template + drift annotation in the same change).

### What This Leaf Consumes

- Existing `agent.BeforeToolCallHook` interface + confirmation flow (as used by
  `DecisionGateHook`, internal/tools/builtin/decision_gate.go).
- The contract file template (C6) is created by 02; this leaf reads the
  serialization template shape from master.md C6 only - no file dependency.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Add `CommandScoreResult` + `ScoreCommand` to DecisionClient: fixed rubric question (0.0-1.0 bands per C6), parse score+confidence from the answer, set Degraded=true exactly when the lane degrades to argmax-only/parse-fallback (production-degraded shape - never invent values the real client cannot produce) | Create: internal/llm/command_score.go; Modify: internal/llm/decision_client.go; Test: e2e suite (T6) | `go build ./... && go vet ./internal/llm/` | package absent | builds; ScoreCommand returns parsed result or error; Degraded only on the real degradation paths | Given a serialized context; When ScoreCommand runs; Then the result carries Score, Confidence, Degraded, Answer with Degraded mirroring the existing decision-client degradation signal |
| T2 | Add `SerializeCommandContext(command, cwd, facts)` - byte-stable: fixed key order, sorted facts map, exact template from master.md C6 | Create: internal/jevgate/serialize.go; Test: e2e suite (T6) | `go build ./...` | package absent | identical inputs -> identical bytes; facts iteration order never leaks | Given the same command/cwd/facts in different map iterations; When SerializeCommandContext runs; Then output bytes are identical every run |
| T3 | Package doc for internal/jevgate stating the fail-closed polarity decision and its rationale (permission-table ASK semantics for high-stakes local side effects; deliberate contrast with the fail-open builtin decision gate) | Modify: internal/jevgate/doc.go | `go doc ./internal/jevgate` | package absent | doc comment names the decision + rationale | Given a reader of the package; When they read the doc; Then the polarity choice and why are stated |
| T4 | Implement `ScoreGateHook` (C3): shell calls only; allow iff parsed answer affirmative-shape AND !Degraded AND Score >= threshold AND Confidence >= confidence threshold; every other path (error, nil result, Degraded, parse failure, below-threshold score OR confidence) -> escalate to the existing confirmation flow; scorer unavailable == ASK (never allow); hook disabled passthrough when not enabled | Create: internal/jevgate/gate.go; Test: e2e suite (T6) | `go build ./...` | package absent | all fail-closed branches covered; never a silent allow | Given a shell tool call; When the scorer returns each of: valid-allow, valid-deny, degraded, error, nil; Then the hook allows ONLY the valid-allow case and escalates the rest to confirmation |
| T5 | Config + daemon wiring: `jev_command_gate` JSON5 block (enabled default FALSE), construct + wire ScoreGateHook in internal/daemon/components.go next to the decision gate wiring; disabled by default means zero behavior change | Modify: internal/config (gate block), internal/daemon/components.go, config template; Test: e2e suite (T6) | `go build ./... && make graphs` (fresh graphs commit-free) | wiring absent | daemon builds; with enabled=false the hook is not registered; with enabled=true it is | Given default config; When the daemon starts; Then no score gate hook is active and the suite's baseline passes unchanged |
| T6 | Hermetic e2e suite covering T1-T5: scorer stub uses the production-degraded result shape (constant-degraded gate field class - only values the real client can produce); cases: allow-path, low-score, low-confidence, degraded, scorer-error, disabled; byte-stability assertion on serialization | Create: e2e/suites/jev-command-gate/ (suite dir, `e2e` build tag); Modify: e2e/manifest.json (suites entry + path_map) | `make e2e-fast-area AREA=jev-command-gate` | suite absent | all cases GREEN; manifest entry present | Given the suite; When run hermetically; Then every gate branch asserts the C3 table and serialization is byte-stable across runs |

## Self-Verification Checklist

- [ ] All tasks implemented; e2e suite GREEN (`make e2e-fast-area AREA=jev-command-gate`)
- [ ] Contracts C1, C2, C3 satisfied exactly (names, signatures, semantics)
- [ ] Gate is FAIL-CLOSED on every non-clean path; scorer unavailable == ASK
- [ ] Gate checks the parsed SCORE ANSWER, not confidence alone
  (decision-gate.go:150-158 bug class)
- [ ] Test stubs produce only production-possible result shapes
- [ ] Config defaults false; no behavior change when disabled
- [ ] All files at exact paths; ASCII only; gofmt clean
- [ ] No deviations (or deviations documented in the report)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** last 20 lines of
   `make e2e-fast-area AREA=jev-command-gate`
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; e2e suite passing; manifest entry present
- [ ] C1/C2/C3 signatures and semantics match master.md exactly
- [ ] Fail-closed polarity verified per branch; no silent allow path
- [ ] Answer+score two-bar check present (not Confidence-only)
- [ ] Degraded-shape stubs match the real client's degraded class
- [ ] Conventions: pkg/id only, no mutex across I/O, gofmt, ASCII
- [ ] No scope creep; no debug artifacts; no line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

- Do NOT create internal/jevgate capture code - that is 02's leaf. Keep the
  hook's record emission point a no-op seam (a nil-tolerant `Recorder`
  interface field) so 02 can attach without touching gate logic.
- Do NOT edit internal/shadow or internal/security - gate lives at the
  tool-call hook layer.
- Threshold values stay config-driven; 03 freezes the recommended default into
  the contract file later. Comment the default band from R0 findings if
  available at dispatch (rides dispatch context), else leave a TODO(03) marker
  naming the contract file.
