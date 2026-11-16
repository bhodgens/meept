# Plan Review Report: 20261115-inter-agent-coordination

Tree: docs/plans/20261115-inter-agent-coordination/
Reviewed against: issue #139 (trimmed 3-feature scope) + source code at HEAD
Date: 2026-11-15
Reviewer: orchestrator (in-session, per plan-tree-review skill Phase 1b - PLANNED tree, design-review mode)

## Verdict

The tree was structurally compliant and directionally sound, but carried 6 factual
defects against the source code: 2 majors (an impossible contract and an invented
detection mechanism) and 4 anchor/schema corrections. All 6 are fixed in the tree
now. The tree is dispatch-ready.

## Majors

### M1 - The refusal/max_iterations mapping was invented (would have shipped string matching)

Where: master.md C2 mapping table; 02-acp-emit/00T-unit-tests.md T1 expansion;
02-acp-emit/01-delegate-pair-emit.md T2 mapping table.

What the plan said: map ChildState to StopReason by refining "failed" with a
"refusal marker" in the result text and a "max iterations" marker in the error text.

What the code says: no such markers exist anywhere in the delegate path.
- internal/llm/errors_refusal.go:35 defines the typed `*llm.RefusalError` - refusal
  is a typed error, never a text marker.
- ChildRecord (delegate.go:116-131) stores err as a STRING. By the time
  resultFromRecord (delegate.go:847) runs, the typed error is gone. Any
  implementation of the plan as written would have greped error text - exactly the
  pattern the repo bans elsewhere (bughunt fixes replaced string matching with
  typed classification repeatedly).
- max_iterations: no typed signal exists in the child result path at all. The enum
  value would have been dead.

Fix applied: mapping table now says - completed->end_turn; interrupted->cancelled;
failed + typed *llm.RefusalError (classified at settle time, where the typed error
is in scope, stored on the record)->refusal; failed otherwise->error;
max_iterations reserved, not emitted. Tests updated to match (no text fixtures).

### M2 - The ACP stopReason contract had an import cycle

Where: master.md C3; 02-acp-emit/02-acp-updates.md (the leaf had it right, the
master had it wrong - the master said "Owner: 02-01 (session.go)" AND implied
agent.StopReason flows through the acp package).

What the code says: internal/agent imports internal/acp (acp_executor.go:10,
delegate.go:22, delegate_descriptor.go:20). If acp.StopReason were agent.StopReason,
acp would import agent - cycle. Also: Session.Send has THREE production callers
(acp_executor.go:295, delegate.go:638, delegate_descriptor.go:605), not the one
the leaf named.

Fix applied: C3 now specifies acp.StopReason (own string type, identical values),
names the owner as 02-02, and lists all three caller sites for the signature change.

## Corrections (minors)

### C1 - DelegateManager has no bus; the emitter premise was wrong

02-acp-emit/01-delegate-pair-emit.md T3 said "publishTurnState(d.bus, ...)". There
is no d.bus: DelegateManager holds a generic `publisher func(topic, payload)` seam
(wired at dsh_wiring.go:307). Fixed: turn-state emission goes through the existing
publisher seam; the typed bus.PublishT path is reserved for pair (which does hold
*bus.MessageBus via PairOrchestratorDeps.Bus, pair_orchestrator.go:39-43) and ACP.

### C2 - Pair phase line anchors were wrong

The leaf cited actor_turn/reviewer_turn/completed/failed transitions without line
numbers; actual verified lines: :234/:264/:313+:327/:225+:239+:270. Fixed with
verified anchors.

### C3 - The e2e manifest schema was guessed

04-wire/01-ws-docs-graphs.md T4 expansion described a manifest entry shape
("suite", "paths", "build_tags") that does not exist. Actual schema (verified
against delegate-seam): {name, dir, status, diff, scenarios[], note} plus a
SEPARATE top-level path_map key. Fixed with the real schema.

### C4 - Pattern-fixture guidance would have invited invented regexes

03-inbox/00T-unit-tests.md T3 told the test writer to fixture "a deny pattern for
exfiltration" without naming the classes. Fixed: names the four real classes
(agent_boundary.go:40-49) and directs the fixture to the compiled exfiltration
pattern, not an invented string.

## Verified-clean items (checked, no defect)

- wsclass.WSProgress exists and TurnStateEvent.WSClass() mirrors the
  TurnTerminalEvent marker pattern correctly (handler.go:320).
- internal/agent already imports internal/bus and internal/comm/wsclass - the
  contract file compiles with the planned imports.
- bus.PublishT signature matches the leaf usage (source param, non-blocking).
- The agent-does-not-import-employee constraint in 03 is real and correctly
  handled with the local JSON mirror (C7).
- Enqueue stores Body verbatim (messaging.go:249-278) - the typed-body design needs
  no store change, as the leaf now states.
- llm.ParameterProperty supports nested object schemas (models.go:492-501) - the
  structured tool arg is expressible.
- PairOrchestratorDeps.Bus exists - the pair emitter premise holds.
- The file-serialization constraint (02-01 before 02-02 on session.go) is correct
  and load-bearing; kept.
- Session struct (acp/session.go:76-86) has no bus field - the nil-safe
  SetTurnStatePublisher design in 02-02/04-01 is the right seam.

## Process notes

- Both structural checkers re-run after edits: ALL TREES COMPLIANT: True;
  structural checks passed.
- Corrections followed the skill's rule: fix the document the WORK lands in
  (leaves), then make the master match (C2/C3 owner + mapping).
- #140 (bot result/inbox) remains open and consumes the same vocabulary; not part
  of this tree by design.

## Disposition

All findings fixed in-tree. Ready to execute via the branch dispatch protocol
(wave A1: contract test gate -> contract leaf; then A2 test gates; then impl
waves per the master's Dispatch Protocol with the 02-01/02-02 serialization).
