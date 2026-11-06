# Jev Command-Safety Score Gate - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Root
- **Parent:** none (root)
- **Children:** 4 (R0-research.md, 01-scorer-gate.md, 02-capture-export.md, 03-eval.md)
- **Scope:** A graded Jev command-safety score with a FAIL-CLOSED threshold gate
  on shell commands, a capture-and-feed-back loop that pairs human verdicts with
  prior model scores at exactly two chokepoints, approvals-as-training-data DPO
  export, and a classifier-eval harness with a teacher-gate report (proposal 12,
  close the gap).

## Goal

Today the daemon has NO numeric command-safety score anywhere: risk is
categorical (`classifyRisk`), confidence is binary-ish, and the builtin
decision gate FAILS OPEN (deliberate polarity divergence: the ACP lane is
fail-closed, `internal/llm/AGENTS.md` decision-lane notes). High-stakes local
side effects deserve permission-table ASK semantics, not fail-open.

This tree delivers four things:

1. **Graded safety score** - a Jev score question on the decision path (fixed
   rubric; text context = command + cwd + session facts) gating shell commands
   by score threshold with FAIL-CLOSED polarity: score below threshold OR
   scorer unavailable/parse-degraded escalates to the human via the existing
   confirmation flow. Opt-in, default false.
2. **Capture-and-feed-back loop** - records at exactly two chokepoints where a
   human verdict pairs with the model's prior score: decision-gate
   deny->human-resolves, and permission-table ask->confirmation. Stored beside
   the shadow sqlite store; exported via the EXISTING shadow DPO exporter as
   (command+context, chosen=human decision) pairs; near-miss contrastive pairs
   derived programmatically.
3. **Front matter = the Jev contract file** - frozen questions/labels +
   byte-stable command serialization template, hashed into the training
   manifest. NOT skills `requires-tools:` front matter (that is
   tool-availability gating, a different mechanism).
4. **Eval harness OUTSIDE the repo runtime path** - extends
   `tools/classifier-eval` patterns: teacher-gate (teacher must beat a
   rule-based floor before student distillation) + replay-ruler reporting. The
   training loop itself lives outside the repo; the tree ships capture/export/
   contract surfaces + the harness. Threshold frozen from the teacher sweep,
   never re-tuned on the student.

## Architecture

`internal/llm.DecisionClient` (rides the classifier resolution, same as the
existing decision lane) grows a `ScoreCommand` method answering the frozen
contract question over a byte-stable serialized context. A new
`internal/jevgate` package implements the fail-closed `BeforeToolCallHook`
(shell calls only), wired in `internal/daemon/components.go` next to the
decision gate; a deny converts to the existing confirmation flow. A new
`internal/gatecapture` package owns the sqlite capture store (beside the
shadow store) with DPO export + programmatic near-miss pairing. The frozen
question/labels/serialization template live in
`config/jev-command-gate.contract.json5`. The eval harness
(`tools/jev-command-gate-eval/`) replays captured records, computes the
rule-based floor from `shell.GetRiskLevel`, and gates student distillation on
a teacher sweep that beats that floor.

## Interface Contracts

### C1: CommandScoreResult + ScoreCommand

```
// internal/llm (decision client, rides classifier resolution)
type CommandScoreResult struct {
    Score      float64 // 0.0 (benign) .. 1.0 (catastrophic), rubric in C6
    Confidence float64 // model self-reported calibration 0.0 .. 1.0
    Degraded   bool    // true on argmax-only / parse-shape fallback
    Answer     string  // raw answer text (capture + audit only)
}
func (d *DecisionClient) ScoreCommand(ctx context.Context,
    serializedContext string) (*CommandScoreResult, error)
// Owner: 01-scorer-gate.md
// Consumers: 02-capture-export.md (prior score/confidence/degraded fields),
//            03-eval.md (replay semantics)
```

### C2: Byte-stable command context serialization

```
// internal/jevgate/serialize.go
func SerializeCommandContext(command, cwd string, facts map[string]string) string
// Byte-stable: keys sorted, exact template in C6. Same inputs -> same bytes,
// always (training manifest hashes depend on it).
// Owner: 01-scorer-gate.md
// Consumers: 02-capture-export.md (ContextBlob), 03-eval.md (replay reproduces bytes)
```

### C3: Fail-closed score gate hook

```
// internal/jevgate/gate.go
type ScoreGateHook struct{ /* scorer, threshold, confirmer, logger */ }
func NewScoreGateHook(scorer ScoreClient, threshold float64,
    confirm ConfirmFlow, logger *slog.Logger) *ScoreGateHook
// Implements agent.BeforeToolCallHook (same interface as DecisionGateHook,
// internal/tools/builtin/decision_gate.go:66). Shell tool calls ONLY.
// ALLOW (fall through to lower security stages): parsed answer AND
//   !result.Degraded AND result.Score >= threshold AND result.Confidence >= confThreshold.
// EVERYTHING ELSE IS FAIL-CLOSED: nil result, error, Degraded, unparseable
// answer, score < threshold, or low confidence -> escalate to the existing
// confirmation flow (same deny-to-confirmation conversion the decision gate
// uses). Scorer unavailable == ASK, never allow.
// Owner: 01-scorer-gate.md; wired in internal/daemon/components.go
// Consumers: 02-capture-export.md (records at the two chokepoints)
```

### C4: CommandGateRecord + capture store

```
// internal/gatecapture/store.go
type CommandGateRecord struct {
    ID              string    // pkg/id.Generate(), never time.Now/rand
    Ts              time.Time
    Chokepoint      string    // "decision_gate_deny" | "permission_table_ask"
    Command         string
    ContextBlob     string    // C2 serialization, verbatim
    PriorScore      float64
    PriorConfidence float64
    PriorDegraded   bool
    HumanDecision   string    // "allowed" | "denied"
    Approver        string
}
func OpenStore(path string) (*Store, error) // sqlite, beside the shadow store:
func (s *Store) Record(ctx context.Context, rec *CommandGateRecord) error
func (s *Store) ListRecords(ctx context.Context, opts ListOptions) ([]CommandGateRecord, error)
// Owner: 02-capture-export.md
// Consumers: 03-eval.md (replay corpus)
```

### C5: DPO export + near-miss pairing

```
// internal/gatecapture/export.go - reuses the EXISTING shadow DPO exporter
// machinery (internal/shadow/exporter.go exportDPO patterns, PairMessages/
// preference-pair shapes) via an adapter; no fork of the format.
func (s *Store) ExportDPO(ctx context.Context, w io.Writer, opts ExportOptions) (int, error)
// Row shape: {"prompt": ContextBlob, "chosen": human decision text,
//             "rejected": model position text, "pair_kind": "record"|"near_miss"}
// Near-miss derivation (programmatic, deterministic): same command prefix
// (rule frozen in C6) across records with DIFFERENT HumanDecision ->
// one contrastive pair per minimal differing prefix group.
// Owner: 02-capture-export.md; Consumers: 03-eval.md
```

### C6: Jev contract file (the front matter)

```
// config/jev-command-gate.contract.json5  (OWNER: 02 creates, 03 freezes threshold into)
// FROZEN content: score question text; 0.0-1.0 label rubric (anchor
// descriptions per band); C2 serialization template; near-miss prefix rule;
// gate threshold field (provisional until 03's teacher sweep freezes it).
// sha256 of this file goes into the training manifest (03).
// Owner: 02-capture-export.md (file) / 03-eval.md (threshold freeze + hash)
// Consumers: 01-scorer-gate.md reads template+rubric; training loop (external)
```

### C7: Eval harness + teacher-gate report

```
// tools/jev-command-gate-eval/ (python, extends tools/classifier-eval patterns)
//   eval.py, replay.py, report JSON schema:
//   {"floor": {...rule-based baseline metrics from GetRiskLevel mapping...},
//    "teacher": {...}, "student": {...}|null,
//    "teacher_gate": "PASS"|"FAIL",   // teacher beats floor on held-out replay
//    "frozen_threshold": float}
// Teacher MUST beat the rule-based floor before any student run; the
// threshold is frozen from the teacher sweep and never re-tuned on the student.
// Owner: 03-eval.md
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| R0 | R0-research.md | leaf (research) | none | 25K | A |
| 01 | 01-scorer-gate.md | leaf | R0 (rubric/threshold defaults) | 75K | B |
| 02 | 02-capture-export.md | leaf | 01 (C1-C3), R0 | 80K | C |
| 03 | 03-eval.md | leaf | 01, 02 (C4-C6), R0 (methodology) | 60K | D |

Same-letter children dispatch together; here every code leaf is sequential
(shared files: components.go, e2e manifest). R0 carries no code and no shared
files.

## Dispatch Protocol

For each concurrency group, in dependency order. Full recipe: skill
`hierarchical-plan-execution`.

### Phase 0: Test Gate

N/A as a separate gate phase - tests ride each leaf in the hermetic e2e tier
(per repo e2e policy: no new unit test files for new features). See
`## Test Gate` below; the orchestrator validates each leaf's e2e tasks against
that matrix during review.

### Phase 1: Dispatch Concurrency Group A

Dispatch R0 via `delegate_task`: goal = "Execute the research plan in
R0-research.md"; context = full leaf text + the do-not-commit rule. R0 uses
`web_search`; its deliverable is `docs/research/jev-command-gate-calibration.md`.

### Phase 2: Dispatch 01 (Group B), review, commit

`delegate_task` with the full 01 leaf text + contracts C1, C2, C3, C6 + coding
conventions + "Do NOT commit / Do NOT git add / no read_file on existing
sources / write once and stop" rules. Review in-session (main model, never a
delegated reviewer): leaf spec, contracts, review checklist, `go build ./...`,
`go test -p 2 ./internal/llm/... ./internal/jevgate/...`, the new e2e suite via
`make e2e-fast-area AREA=<suite>`. Re-dispatch with findings on gaps (max 3
cycles). On pass: stage exactly the leaf's files, commit
`feat(jev-gate): fail-closed command score gate (01-scorer-gate)`, update the
tracking table.

### Phase 3: Dispatch 02 (Group C), review, commit

Same shape with 02 + contracts C2, C4, C5, C6. Verify the two chokepoints
actually record (e2e), DPO export rows match C5, records contain no secret
values. Commit `feat(jev-gate): capture-and-feed-back store + DPO export
(02-capture-export)`.

### Phase 4: Dispatch 03 (Group D), review, commit

Same shape with 03 + contracts C4-C7 + R0 findings path. Verify teacher-gate
logic gates student runs, threshold freeze lands in C6 + harness report, hash
lands in the training manifest, selftest target passes. Commit
`feat(jev-gate): classifier-eval teacher gate + replay ruler (03-eval)`.

### Phase 5: Integration Review

After all children REVIEWED: run `go build ./...`, `go test -p 2 ./...`
(short), `make e2e-fast`, `make graphs-check` (components.go wiring changed),
`make classifier-eval-selftest` (sibling harness unaffected), and the
line-number corruption check (`grep -rn '^\s\+[0-9]\+|' internal/jevgate
internal/gatecapture tools/jev-command-gate-eval` -> zero hits). Verify C1-C7
end to end (Integration Test Plan). Gaps: re-dispatch the failing child.

## Review Checklist

The orchestrator (main model) verifies each child in-session:

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts satisfied (signatures, types, paths, byte-stability)
- [ ] All specified files created/modified at exact paths
- [ ] Tests written and passing (e2e tier; manifest entry present)
- [ ] **Polarity check (01/02): gate is FAIL-CLOSED; scorer-unavailable == ASK**
- [ ] **Answer+score check (01): gate checks the parsed SCORE ANSWER, not
      confidence alone (decision-gate Confidence-only bug class)**
- [ ] **Degraded-shape check: stubs use the production-degraded result shape;
      no test value the real client cannot produce**
- [ ] **No secret values in captured records or exports (02)**
- [ ] Code follows project conventions; no scope creep; no debug artifacts
- [ ] No line-number corruption (`     N|` prefixes) in source files

Output: APPROVED or a list of specific gaps.

## Coding Conventions

- **Language:** Go 1.22 (leaves 01, 02), Python 3 (leaf 03 harness), JSON5 config.
- **Naming:** exported types only where consumed cross-package; unexported else.
- **IDs:** `pkg/id.Generate()` only; never `time.Now().UnixNano()` or
  `math/rand` (predid analyzer).
- **Error handling:** wrap with context, return early; no ignored errors, no
  bare `panic(err)`.
- **Mutex scope:** never hold a mutex across sqlite or LLM I/O.
- **Typed-nil:** concrete pointers into interface fields only behind
  `if p != nil` (internal/daemon/AGENTS.md).
- **Config:** JSON5; new gate config is OPT-IN, default false (opt-in-defaults
  doc); template + drift annotation in the same commit.
- **Tests:** hermetic e2e tier only for new features (`e2e/suites/<name>/`,
  `e2e` build tag, manifest.json `suites` + `path_map`); `go test -p 2`.
- **ASCII only** in all new files (docs, Go, Python, JSON5).
- **Formatting:** gofmt / python stdlib style (no new deps in the harness
  beyond classifier-eval's existing ones).

## Requirement Traceability

| Req | Source (scope, verbatim intent) | Plan element | Verified by |
|-----|--------------------------------|--------------|-------------|
| R1 | Graded Jev safety score, fixed rubric, text context = command + cwd + session facts | C1, C2, C6; 01 T1-T3 | e2e scorer-gate suite |
| R2 | Score threshold gate on shell, FAIL-CLOSED, scorer unavailable -> prompt; opt-in default false | C3; 01 T4-T6 | e2e fail-closed cases |
| R3 | Capture at exactly two chokepoints; record fields; store beside shadow sqlite | C4; 02 T1-T3 | e2e capture cases |
| R4 | DPO export via existing exporter; programmatic near-miss pairs | C5; 02 T4-T5 | export unit-of-e2e assertions |
| R5 | Contract file = frozen questions/labels + byte-stable serialization, hashed into training manifest | C6; 02 T6, 03 T4 | hash check in harness |
| R6 | Teacher-gate + replay-ruler harness outside runtime; threshold frozen from teacher sweep | C7; 03 T1-T3 | make selftest target |
| R7 | Command-safety calibration research (thresholds, precision/recall, near-miss curation, teacher-gate methodology) | R0 tasks | findings doc exists + cited by 03 |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| R0-research | COMPLETE | 1 | docs/research/jev-command-gate-calibration.md (c5ef67e9); cited by 03 threshold band |
| 01-scorer-gate | COMPLETE | 1 | 14b21b5e (jevgate pkg + ScoreCommand); daemon wiring was NOT in that wave - see closeout |
| 02-capture-export | COMPLETE | 1 | c1a75549 (gatecapture store, DPO export, contract hash 691f5fdf) |
| 03-eval | COMPLETE | 1 | 989e4ea5 (teacher sweep, frozen threshold 0.90, selftest green) |

Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS |
IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

**Closeout (2026-11-05, issue #87):** the missing piece this tree carried was
the DAEMON WIRING: commit 26a7764b (wiring-gap leaf 04) claimed construction +
registration but never touched internal/daemon/components.go (audit-verified:
`git log -S JevScoreGate -- internal/daemon/components.go` empty; the
jevgate/gate.go surface was listed unreachable in the deadcode baseline).
Landed now in 5e01e286: Components.JevScoreGate + ScoreGateHook
construction gated on security.jev_command_gate.enabled +
RegisterBeforeToolCall("jev-score-gate", HookPriorityHigh) + untagged
daemon_jevgate_pending (TestJevGateWiring / TestJevGateRegistrationNilWhenDisabled
green against the real build) + deadcode regen (10 jevgate/gate.go entries
cleared).

## Integration Test Plan

1. `go build ./...` - clean.
2. `go test -p 2 ./internal/llm/... ./internal/jevgate/... ./internal/gatecapture/...` - pass.
3. `make e2e-fast` - the new `jev-command-gate` suite passes: (a) score above
   threshold + parsed answer -> shell proceeds; (b) score below threshold OR
   disabled scorer OR Degraded result -> confirmation flow triggers, never
   silent allow; (c) a deny-resolution at the decision gate and a
   permission-table ask/confirm both append CommandGateRecords with prior
   score fields populated; (d) `ExportDPO` emits valid DPO rows including a
   derived near-miss pair.
4. `make jev-gate-eval-selftest` (03) - harness self-test passes with fixtures;
   teacher-gate FAILs a sub-floor teacher and PASSes an above-floor one.
5. `make graphs-check` - fresh after components.go wiring.
6. Cross-boundary: serialized context bytes from 01's serializer match the C6
   template and appear verbatim in exported prompts (C2/C5/C6 chain).

## Test Gate

Not a separate gate phase - the repo's e2e policy puts feature tests in the
hermetic e2e tier, authored inside each leaf. Matrix (validated per child at
review; delta protocol: the LESSER artifact updates to the GREATER):

| # | Feature | Source | Unit tests | E2E tests | Status |
|---|---------|--------|-----------|-----------|--------|
| F1 | ScoreCommand result shape + degraded class | 01 T1 | n/a (e2e policy) | jev-command-gate suite, scorer cases | VALIDATED (14b21b5e, 41efd41c) |
| F2 | Byte-stable serialization | 01 T2 | n/a | suite asserts stable bytes | VALIDATED (14b21b5e) |
| F3 | Fail-closed threshold gate | 01 T4-T6 | n/a | suite fail-closed cases | VALIDATED (14b21b5e; daemon wiring + wiring-failure e2e landed 5e01e286, issue #87) |
| F4 | Two-chokepoint capture | 02 T1-T3 | n/a | suite capture cases | VALIDATED (c1a75549) |
| F5 | DPO export + near-miss pairs | 02 T4-T5 | n/a | suite export case | VALIDATED (c1a75549) |
| F6 | Contract file + hash | 02 T6, 03 T4 | n/a | suite hash check | VALIDATED (989e4ea5; freeze --check green) |
| F7 | Teacher gate + replay ruler | 03 T1-T3 | n/a (python) | make selftest target | VALIDATED (989e4ea5; `make jev-gate-eval-selftest` green 2026-11-05) |

Delta log: 2026-11-05 issue #87 - the F3 daemon-wiring half was false-complete
at 26a7764b (never touched components.go; jevgate/gate.go dead in the deadcode
baseline; wiring test build-tagged daemon_jevgate_pending). Landed + re-
validated at 5e01e286 (construction gated on security.jev_command_gate.enabled,
RegisterBeforeToolCall("jev-score-gate", HookPriorityHigh), tagged test untagged
and green, e2e jev-command-gate suite green, deadcode baseline regenerated).

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as dated
addenda. Never edit a dispatched leaf document mid-execution.

## Notes

- Polarity is a DOCUMENTED DECISION (proposal 12): high-stakes local side
  effects match permission-table ASK semantics (fail-closed), deliberately
  opposite the builtin decision gate's fail-open. Record the rationale in the
  gate's package doc comment.
- The builtin decision gate (fail-open) and the ACP lane (fail-closed) keep
  their existing polarity; this tree adds a THIRD surface, it does not flip
  either existing one.
- `lfm-combined-sft` (config/models.json5:88,186) is the current classifier
  A/B alternate; the score question rides the same classifier resolution as
  the decision client - no new runtime endpoint.
- e2e scratch-bin hygiene and `-p 2` port-exhaustion rules apply to every
  suite (root AGENTS.md).
