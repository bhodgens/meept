# Gate Capture + DPO Export + Contract File - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD (e2e tier per repo policy). Do NOT
> commit - the orchestrator handles all git operations after review. Do NOT
> use read_file on existing source files - explore with search_files or
> terminal cat. After writing a file, do NOT read it back - write once and
> stop. Report what you built, files touched, and any deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** Capture human verdicts paired with prior model scores at exactly
  two chokepoints, store them in sqlite beside the shadow store, export DPO
  pairs via the existing exporter machinery, and create the frozen Jev
  contract file.
- **Dependencies:** 01-scorer-gate.md (C1-C3 must exist: score results,
  serializer, gate hook); R0-research.md (near-miss guidance informs T5)
- **Estimated Context:** 80K
- **Concurrency Group:** C
- **Audit references:** proposal 12 (close the gap)

## Goal

Every time a human resolves a gate escalation - decision-gate deny resolved by
the user, or a permission-table ask answered by a confirmation - record the
command text, the serialized context (C2 bytes), the model's prior
score/confidence/degraded flag, the human decision, and the Approver. Store in
a new sqlite DB beside the shadow store. Export (prompt=ContextBlob,
chosen=human decision) DPO pairs through the EXISTING shadow exporter
machinery, deriving near-miss contrastive pairs programmatically (same command
prefix, different outcomes). Create `config/jev-command-gate.contract.json5`
with frozen questions, labels, and the byte-stable serialization template.

## Context

- internal/shadow/manager.go:259 `CaptureInteraction` and :172 `ProcessRecord`
  - the shadow capture pattern to mirror (store beside it, not inside it).
- internal/shadow/store_sqlite.go - sqlite store shape; the new
  internal/gatecapture store follows the same conventions (schema migration
  style, path resolution via MEEPT_HOME).
- internal/shadow/exporter.go:208 `exportDPO` + ListPreferencePairs /
  MinMargin / dedup - the EXISTING DPO machinery; reuse its pair/message
  shapes via an adapter in internal/gatecapture. Do NOT fork the format.
- The two chokepoints:
  - internal/tools/builtin/decision_gate.go (deny -> user resolves the
    confirmation) - attach capture at the human-resolution point.
  - internal/security/engine.go:504-510 Stage 1.5 permission-table ask ->
    confirmation flow - attach at the confirmation resolution point.
- Secrets two-lane spirit: captured records NEVER contain secret values - no
  env values, no credential material, no file contents beyond the command
  text itself; the ContextBlob is command + cwd + session facts only.
- 01-scorer-gate.md leaves a nil-tolerant Recorder seam on ScoreGateHook;
  attach the capture store there WITHOUT changing gate logic.

## Verified Anchors

- internal/shadow/manager.go:259 - `CaptureInteraction` (primary capture entry)
- internal/shadow/manager.go:172 - `ProcessRecord` (mutex only across store writes)
- internal/shadow/store_sqlite.go - sqlite store conventions
- internal/shadow/exporter.go:208 - `exportDPO` with ListPairsOptions{MinMargin, UnexportedOnly, Since}
- internal/tools/builtin/decision_gate.go:56-66 - DecisionGateHook deny/confirm conversion
- internal/security/engine.go:504-510 - Stage 1.5 permission-table ask
- internal/daemon/components.go:6796 - permission table wiring neighborhood

## Interface Contracts (From Parent)

### What This Leaf Exposes

- [C4 CommandGateRecord + Store] - full definition in master.md
- [C5 ExportDPO + near-miss pairing] - full definition in master.md
- [C6 contract file] - creates config/jev-command-gate.contract.json5:
  score question text (exact prompt sent by 01's ScoreCommand), 0.0-1.0 label
  rubric bands, C2 serialization template, near-miss prefix rule, gate
  threshold field (provisional value; 03 freezes it)

### What This Leaf Consumes

- [C1 CommandScoreResult] - fields for PriorScore/PriorConfidence/PriorDegraded (01)
- [C2 SerializeCommandContext] - ContextBlob bytes (01)
- [C3 ScoreGateHook Recorder seam] - nil-tolerant attach point (01)
- Existing shadow DPO pair/exporter shapes (internal/shadow/exporter.go)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | `CommandGateRecord` + sqlite `Store` (OpenStore/Record/ListRecords) in internal/gatecapture, mirroring shadow store conventions; ID via pkg/id.Generate(); DB path beside the shadow store under MEEPT_HOME | Create: internal/gatecapture/store.go, internal/gatecapture/store_sqlite.go | `go build ./... && go vet ./internal/gatecapture/` | package absent | builds; Record/List round-trip in e2e (T6) | Given a record; When Record then ListRecords run; Then the record round-trips with all fields intact |
| T2 | Wire capture at chokepoint 1: decision-gate deny -> human resolves confirmation -> Record{Chokepoint:"decision_gate_deny", prior score fields from C1 result, HumanDecision, Approver}; nil-tolerant (no store = no-op, never blocks the flow) | Modify: internal/tools/builtin/decision_gate.go (resolution point), internal/daemon/components.go (wiring) | `go build ./...` | no capture code | record lands only when a deny is human-resolved; allow path unaffected | Given a deny escalated to confirmation; When the human resolves it; Then exactly one record with the prior score/degraded fields and the human decision is stored |
| T3 | Wire capture at chokepoint 2: permission-table ask -> confirmation resolution -> Record{Chokepoint:"permission_table_ask", same fields; prior score fields carry the jevgate score when the score gate produced one for this command, else the documented zero-value} | Modify: internal/security/engine.go Stage 1.5 resolution path (or the confirmation flow entry it routes to - pick the single narrowest point), internal/daemon/components.go | `go build ./...` | no capture code | ask-resolutions record exactly once; denies/allows that never ask record nothing | Given a permission-table ask answered by confirmation; When resolution lands; Then exactly one record exists with the serialized ContextBlob and human decision |
| T4 | DPO export adapter: `ExportDPO` over the capture store reusing the EXISTING shadow exporter pair shapes/format (no format fork); rows: prompt=ContextBlob, chosen=human decision text, rejected=model position text, pair_kind="record" | Create: internal/gatecapture/export.go | `go build ./...` | package absent | export emits valid rows against the existing DPO format expectations | Given N records; When ExportDPO runs; Then N DPO rows emit with the frozen field mapping |
| T5 | Programmatic near-miss derivation: deterministic same-command-prefix rule (frozen in C6) across records with different HumanDecision -> one contrastive pair per minimal differing prefix group, pair_kind="near_miss" | Modify: internal/gatecapture/export.go | `go build ./...` | n/a (T4 exists) | near-miss pairs derive deterministically from the same input set | Given records sharing a command prefix with divergent decisions; When export runs; Then a near_miss contrastive pair (allowed vs denied) emits for the group |
| T6 | Create the Jev contract file `config/jev-command-gate.contract.json5` (frozen question text, label rubric, serialization template, near-miss prefix rule, provisional threshold) + secrets scrub assertion in the e2e suite | Create: config/jev-command-gate.contract.json5; Test: e2e suite (T7) | `python3 -c "import json,re,sys; s=open('config/jev-command-gate.contract.json5').read(); sys.exit(0)"` (parses as JSON5) | file absent | file parses; content matches C6 spec; template matches 01's serializer output | Given the contract file; When the serializer runs on a sample; Then its bytes match the file's template section |
| T7 | Hermetic e2e suite cases (extend the jev-command-gate suite from 01 or add a sibling suite + manifest entry): both chokepoints record; DPO export valid; near-miss pair derived; NO record/export ever contains a secret value (assert scrub against seeded secret fixtures); sqlite file lands beside the shadow store path | Create/Modify: e2e/suites/jev-command-gate/ (or sibling), e2e/manifest.json | `make e2e-fast-area AREA=jev-command-gate` | cases absent | all cases GREEN including the secret-scrub assertion | Given seeded secrets in env/session facts; When capture + export run; Then no secret value appears in any stored record or exported row |

## Self-Verification Checklist

- [ ] All tasks implemented; e2e suite GREEN
- [ ] Contracts C4, C5 satisfied exactly; C6 file created to spec
- [ ] Capture happens at EXACTLY the two chokepoints - no third capture point
- [ ] Nil-tolerant: missing store never blocks or fails a gate/confirmation flow
- [ ] NO secret values in any record or export row (two-lane spirit)
- [ ] DPO format reuses the existing shadow exporter shapes (no fork)
- [ ] Near-miss derivation is deterministic
- [ ] IDs via pkg/id only; no mutex across sqlite I/O; gofmt; ASCII only
- [ ] No deviations (or documented in the report)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** last 20 lines of
   `make e2e-fast-area AREA=jev-command-gate`
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; e2e suite passing; manifest fresh
- [ ] C4/C5 signatures match master.md; C6 file matches the contract spec
- [ ] Both chokepoints verified recording; nothing else records
- [ ] Secret-scrub assertion present and meaningful (seeded fixtures)
- [ ] Exporter reuses shadow DPO shapes; near-miss rule deterministic
- [ ] Conventions followed; no scope creep; no debug artifacts

Output: APPROVED or specific gaps with file + line references.

## Notes

- The Approver field comes from the confirmation flow's existing identity
  context; when the flow carries no identity, store the documented empty value
  - never a guessed name.
- The provisional threshold in C6 gets frozen by 03; mark the field with a
  `// frozen-by: 03-eval` style comment so the handoff is greppable.
