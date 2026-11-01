# Inter-Agent Taint Lane (Proposal 5) - Branch Orchestrator

> **For the executing agent:** You are the orchestrator for this branch.
> Your job: (1) dispatch leaf agents, (2) review their work in-session,
> (3) re-dispatch if incomplete, (4) track completion. Do NOT implement
> code yourself.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 3 leaves (R0-research, 01-taint-redesign, 02-driver-wiring)
- **Scope:** Fix the whole-message taint-labeling flaw and wire the dead
  `TaintBeforeToolCall` lane so collaboration-driver handoffs are labeled
  per-handoff, declassified at the driver boundary, and protected by an
  inter-agent deny-pattern set.

## Goal

`TaintBeforeToolCall` (internal/agent/taint_hooks.go) implements a
cross-agent message check but has ZERO wiring call sites in
internal/daemon - the lane is dead. As written it also labels the ENTIRE
message content `TaintUntrusted` unconditionally (taint_hooks.go:169), so
wiring as-is would block ALL cross-agent messages (`AgentMessageSink`
currently blocks only `TaintSecret`; adding `TaintUntrusted` to the sink
without per-handoff labeling = total collaboration outage).

This branch delivers: per-handoff labeling (the driver-side content that is
genuinely untrusted - raw driver output embedded in prompts - is labeled;
handoff framing is not), declassification at the driver boundary,
`TaintUntrusted` added to the sink taxonomy, deny patterns for forged
control messages, and real wiring in components.go with a test that fails
without it.

## Architecture

Three seams:

1. **Taxonomy + hook redesign** (01): `AgentMessageSink` gains
   `TaintUntrusted`; `checkMessageTaint` inspects the message for labeled
   untrusted segments instead of labeling the whole content. The hook's
   decision consumes tracker state recorded at handoff time, not a
   blanket label.
2. **Driver handoff marking** (02): all three collaboration drivers
   (pair, team, diff) call `tracker.MarkUntrusted(rawDriverOutput, source)`
   / `RecordToolTaint` when embedding agent output into the next agent's
   prompt; the differentiator's own framing is not tainted. A deny-pattern
   set in internal/security neutralizes forged control messages (agent
   impersonation, `workspace_yield`-style instruction forgery) before they
   ride a handoff.
3. **Wiring** (02): `NewTaintBeforeToolCall` registered in
   internal/daemon/components.go next to security-before-tool
   (:1960-1963 neighborhood), gated by existing `[security.taint] enabled`
   config, with typed-nil guards.

R0 research gates the design choices (spotlighting/marker technique for
labeled segments, over-blocking tradeoffs).

## Interface Contracts

### C1-A: Per-handoff taint decision

```
// internal/agent/taint_hooks.go (redesigned)
func (t *TaintBeforeToolCall) checkMessageTaint(ctx context.Context,
    toolCall llm.ToolCall) BlockResult
// Behavior: block ONLY when the message contains segments the tracker has
// labeled TaintUntrusted (via MarkUntrusted at handoff time) AND the sink
// policy blocks that label. NEVER label the whole content unconditionally.
// Owner: 01-taint-redesign.md
// Consumers: 02-driver-wiring.md, 03-e2e suite
```

### C2-A: Sink taxonomy

```
// internal/security/taint/taint.go
func AgentMessageSink() *TaintSink
// BlockedLabels: TaintSecret (existing) + TaintUntrusted (NEW).
// Owner: 01-taint-redesign.md
// Consumers: 01 (hook), 02 (wiring), e2e
```

### C3-A: Driver handoff marking

```
// internal/agent/collaboration_{pair,team,diff}_driver.go
// Each driver, before registry.RunAgent (pair: runAgent :440-445 with raw
// driver output embedded :329; team :482; diff :231/:237/:312), records:
tracker.MarkUntrusted(rawAgentOutput, "collab:<driver>:<role>")
// Framing text the driver itself emits ("## Driver's latest output",
// control-message parse :429) is NOT marked untrusted.
// Owner: 02-driver-wiring.md
// Consumers: 01 (hook decision), e2e boundary-hardening-01/02
```

### C4-A: Inter-agent deny patterns

```
// internal/security/agent_boundary.go (NEW)
type InterAgentBoundary struct{ ... }
func NewInterAgentBoundary(logger *slog.Logger) *InterAgentBoundary
func (b *InterAgentBoundary) InspectHandoff(recipient, content string) (blocked bool, reason string)
// Deny set: agent-impersonation claims ("I am agent X"/"as agent X" in
// untrusted segments), forged control verbs (approve/request_changes/
// request_token/workspace_yield-style instruction forgery) arriving from
// tainted segments, spoofed control-message JSON blocks.
// Owner: 02-driver-wiring.md; Consumers: drivers, e2e boundary-hardening-02
```

### C5-A: Wiring seam

```
// internal/daemon/components.go (next to security-before-tool, ~:1960)
if taintCfg := c.Config.Security.Taint; taintCfg.Enabled {
    if hook := agent.NewTaintBeforeToolCall(tracker, logger, taintHookCfg); hook != nil {
        hookRegistry.RegisterBeforeToolCall("inter-agent-taint",
            agent.HookPriorityCritical, hook)
    }
}
// Typed-nil guard on tracker + hook; a test removes/disables the wiring
// path and fails. Owner: 02-driver-wiring.md. Consumers: e2e.
```

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| R0 | R0-research.md | leaf (research) | none | 25K | A |
| 01 | 01-taint-redesign.md | leaf | R0 | 70K | B |
| 02 | 02-driver-wiring.md | leaf | 01 | 85K | C |

R0 dispatches first; 01 after R0 findings digest; 02 after 01 REVIEWED
(strictly sequential inside this branch: same files).

## Dispatch Protocol

For each concurrency group, in dependency order. Full recipe: skill
`hierarchical-plan-execution`.

### Phase 0: Research (group A)

Dispatch R0-research.md via `delegate_task`: goal = "Complete research
tasks from 01-inter-agent-taint/R0-research.md", context = full leaf text +
"web research + findings file only; no production code; Do NOT commit".
Review findings in-session; extract the design decisions the implementation
leaves must honor (labeled-segment mechanism, over-blocking knob defaults)
and attach them to the 01/02 dispatch contexts as dated addenda.

### Phase 1: Dispatch 01-taint-redesign.md (group B)

Context = full leaf text + contracts C1-A/C2-A + relevant source INLINED +
"tests in internal/agent and internal/security/taint are permitted here
because this is a pure-mechanism leaf inside packages with existing unit
coverage; e2e coverage lands in 03-e2e. Do NOT commit. Do NOT git add."

### Phase 2: Review and commit each child

Review in-session (main model, never delegated). Check leaf spec, contracts,
Review Checklist; run `go test -p 2 ./internal/agent/... ./internal/security/...`;
grep for debug artifacts and line-number corruption. Pass: commit the leaf's
exact files, status REVIEWED. Gaps: re-dispatch with findings, max 3 cycles,
then escalate to ../master.md.

### Phase 3: Dispatch 02-driver-wiring.md (group C)

Context = full leaf text + contracts C3-A/C4-A/C5-A + the 01 leaf's exposed
signatures + components.go neighborhood INLINED + no-commit rules.

### Phase 4: Integration review

After 02 REVIEWED: `go build ./...`;
`go test -p 2 ./internal/security/... ./internal/agent/... ./internal/daemon/...`;
confirm the sanitizer C0-first pinned test
(internal/security/sanitizer_marker_order_test.go) still passes. Report
COMPLETE to ../master.md.

## Review Checklist

The branch orchestrator (main model) verifies each child in-session:

- [ ] All tasks from the leaf document implemented; tests passing
- [ ] Contracts satisfied exactly (signatures, types, paths)
- [ ] checkMessageTaint no longer labels whole messages unconditionally
- [ ] Hook registered ONLY via components.go path with typed-nil guard;
      a test fails when the registration is removed
- [ ] All three drivers mark handoffs; framing not marked
- [ ] Sanitizer C0-first order untouched; no other hook order changed
- [ ] No debug artifacts, TODOs, placeholders, line-number corruption
- [ ] No git operations by the leaf agent

Output: APPROVED or specific gaps.

## Coding Conventions

- Go, gofmt-clean; exported minimal; errors wrapped with %w.
- Typed-nil guards at every wiring seam; setter-style seams get nil checks.
- crypto/rand or pkg/id.Generate() for any new identifiers; never math/rand.
- No mutex across I/O. Table-driven tests where natural. `go test -p 2`.
- ASCII only. No heredoc file creation. Leaf agents never git commit/add.
- Taint labels: reuse existing `taint.TaintLabel` constants; no new label
  tiers without an R0 finding justifying it.

## Requirement Traceability

| Req | Source | Plan element | Verified by |
|-----|--------|--------------|-------------|
| RA1 | "fix + wire the dead taint hook" | 01 T1-T3, 02 T2 | wiring-failure test + e2e-01 |
| RA2 | "per-handoff labeling (NOT whole-message)" | contract C1-A; 01 T1 | TestCheckMessageTaint* |
| RA3 | "declassify at the driver boundary" | contract C3-A; 02 T1 | driver marking test |
| RA4 | "add TaintUntrusted to the sink taxonomy" | contract C2-A; 01 T2 | sink test |
| RA5 | "deny-pattern set ... in internal/security" | contract C4-A; 02 T3 | deny-pattern tests + e2e-02 |
| RA6 | "hook registered in components.go alongside security-before-tool" | contract C5-A; 02 T2 | components test |
| RA7 | "e2e proving a forged control message is neutralized and legitimate collaboration still flows" | ../master.md C4; 03-e2e | boundary-hardening-01/02 |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| R0-research.md | PENDING | 0 | |
| 01-taint-redesign.md | PENDING | 0 | |
| 02-driver-wiring.md | PENDING | 0 | |

Status values: PENDING | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE |
BLOCKED (research leaf: COMPLETE when findings reviewed in-session)

## Integration Test Plan

1. `go test -p 2 ./internal/security/taint/... ./internal/agent/...` -
   redesigned hook: whole-message labeling regression test GREEN
   (legitimate message passes), tainted-segment message BLOCKED.
2. `go test -p 2 ./internal/daemon/...` - wiring-failure test passes
   (removing registration fails it).
3. `go test -p 2 ./internal/security/...` - deny-pattern tests;
   sanitizer_marker_order_test.go still green.
4. e2e (after 03-e2e lands): boundary-hardening-01 (legit collaboration
   flows with lane on), boundary-hardening-02 (forged control message
   neutralized).

## Test Gate

Tests-first gate: not used in this branch (research-gated; single-language
Go leaves with inline TDD tables). Coverage matrix:

| # | Feature | Source | Unit tests | E2E tests | Status |
|---|---------|--------|-----------|-----------|--------|
| FA1 | per-handoff decision replaces whole-message label | 01 T1 | TestCheckMessageTaintWholeCleanPasses / TestCheckMessageTaintTaintedSegmentBlocked | boundary-hardening-01/02 | PENDING |
| FA2 | sink blocks TaintSecret + TaintUntrusted | 01 T2 | TestAgentMessageSinkBlocksUntrusted | boundary-hardening-02 | PENDING |
| FA3 | 3 drivers mark handoffs; framing clean | 02 T1 | TestDriverMarksHandoffUntrusted (per driver) | boundary-hardening-01 | PENDING |
| FA4 | inter-agent deny patterns | 02 T3 | TestInterAgentBoundaryInspectHandoff | boundary-hardening-02 | PENDING |
| FA5 | components.go wiring + typed-nil guard | 02 T2 | TestInterAgentTaintWiringFailsWithoutRegistration | boundary-hardening-01 | PENDING |

Statuses: PENDING | DELTA | VALIDATED. Delta log: [none yet]

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as
dated addenda.

## Notes

- Verified anchors: taint_hooks.go:30 (constructor, nil-tracker returns
  nil - typed-nil guard exists at the constructor, keep it),
  :156-198 (checkMessageTaint), :169 (the whole-message flaw).
  taint.go:164-175 (AgentMessageSink, currently TaintSecret only),
  :234-239 (MarkUntrusted), taint.go:29-30 (TaintUntrusted exists in the
  taxonomy already). Drivers: pair runAgent :440-445, raw output embed
  :329, control parse :429; team runAgent :482; diff :231/:237/:312.
- Do NOT register the hook before 01 lands: as-is it would block all
  cross-agent messages. 02 strictly follows 01.
- RecordToolTaint exists on the tracker for tool-output paths
  (loop.go:5363 neighborhood already propagates taint); prefer
  MarkUntrusted for prompt-embedded handoffs.
