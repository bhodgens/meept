# Agent Boundary Hardening - Implementation Orchestrator (Root)

> **For the executing agent:** You are the root orchestrator for this tree.
> Your job: (1) dispatch branch orchestrators / leaf agents, (2) review their
> work in-session, (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.

## Meta

- **Role:** Root
- **Parent:** none
- **Children:** 3 branch documents (01-inter-agent-taint/, 02-pii-proxy/, 03-e2e/)
- **Scope:** Close two agent-boundary security gaps in the meept daemon:
  wire and fix the dead inter-agent taint lane, and ship a PII unmasking
  proxy with a session-scoped restore lane (plus the dead secret
  Deobfuscate lane it exposes).

## Goal

Two security proposals are consolidated here because they share the same
boundary discipline (sanitizer pass order, secrets-in-memory-only, typed-nil
wiring guards) and one integration suite:

1. **Inter-agent sanitization (proposal 5).** `TaintBeforeToolCall`
   (internal/agent/taint_hooks.go) exists but has ZERO wiring call sites in
   internal/daemon - the inter-agent taint lane is dead code. Worse, its
   `checkMessageTaint` labels the ENTIRE cross-agent message `TaintUntrusted`
   unconditionally (taint_hooks.go:169), so wiring it as-is would block every
   legitimate collaboration. This branch redesigns the lane to per-handoff
   labeling, wires it in components.go, and adds a deny-pattern set for
   forged control messages.
2. **PII unmasking proxy with restore (proposal 8, implement fully).**
   No PII detection exists today (credential patterns only). The secret
   obfuscator (internal/security/secrets.go) replaces secrets with
   `#XXXX#` placeholders pre-LLM, but `Deobfuscate`/`DeobfuscateMessages`
   have zero callers outside internal/security - obfuscated tokens leak to
   users as literal `#XXXX#` today. This branch builds a PII detector layer,
   a durable session-scoped swap store, a restore-on-output lane at the
   `ScanOutput` choke point, and fixes the dead secret restore lane.

Estimated total: 600-1,400 LOC, 2.5-4 days with branches A and B in
parallel. Go tests run with `-p 2` (macOS ephemeral-port exhaustion; root
AGENTS.md).

## Architecture

Both branches follow the same shape: a research leaf (R0) gates design
decisions, then two implementation leaves per branch build the mechanism and
wire it. Branch A works in `internal/security/taint`,
`internal/agent/taint_hooks.go`, the three collaboration drivers, and
`internal/daemon/components.go`. Branch B creates `internal/security/pii`,
touches `internal/security/secrets.go`, `internal/agent/security_hooks.go` /
a new restore hook, `internal/agent/loop.go` (ScanOutput choke point only),
and `internal/config/schema.go`. Branch C is the shared hermetic e2e suite
(`e2e/suites/boundary-hardening/`) proving both lanes end to end, plus its
`e2e/manifest.json` entries.

The root does not dispatch leaves directly except the two R0 research
leaves, which are independent and dispatch together (concurrency group A).
Branch implementation leaves dispatch only after their branch's R0 lands
(research-gated pattern). 03-e2e dispatches after both branches' wiring
leaves reach REVIEWED.

## Interface Contracts

<!-- Cross-branch contracts. Branch-internal contracts live in the branch
     orchestrators; leaves never restate full definitions. -->

### C1: Hook ordering (frozen, both branches + e2e)

```
Sanitizer pass order: C0 strip FIRST (pinned by
internal/security/sanitizer_marker_order_test.go - never reorder).
TransformContext pass order (registration order within HookPriorityCritical):
  1. security-transform   (SecurityTransformContext)
  2. secret-obfuscation   (SecretObfuscationHook)      [existing]
  3. pii-restore          (PiiRestoreTransformContext) [NEW, branch B]
BeforeToolCall order:
  1. security-before-tool (existing, HookPriorityCritical)
  2. inter-agent-taint    (TaintBeforeToolCall redesign, HookPriorityCritical,
                           registered by branch A)
// Owners: 02-pii-proxy/01-detector-store.md (order test),
//          01-inter-agent-taint/02-driver-wiring.md (taint registration),
//          03-e2e/01-boundary-hardening-suite.md (order assertions)
// Consumers: all branches
```

### C2: In-memory-only swap-map discipline (both branches)

```
PII swap values and secret deobfuscation maps are plaintext-at-rest in
process memory ONLY - same rule as secrets Broker.resolve, pinned by
scripts/audit-secrets-value-callers.py in spirit. They never reach:
logs, env vars, audit rows, bus events, or trace/LoRA capture.
// Owners: 02-pii-proxy/01-detector-store.md, 02-pii-proxy/02-restore-lane.md
// Consumers: 03-e2e/01-boundary-hardening-suite.md (leakage assertions)
```

### C3: Placeholder leakage classes (branch B; e2e asserts)

```
Named and closed: (a) user-visible replies, (b) conversation history
persistence, (c) LoRA/trace capture rows (loop.go:5353+ records tool
output), (d) auditlog output rows (auditlog redacts to SHA-256 already),
(e) logs. Restore happens at the ScanOutput choke point
(internal/agent/loop.go:3213 neighborhood) BEFORE any of these surfaces.
// Owner: 02-pii-proxy/02-restore-lane.md
// Consumers: 03-e2e/01-boundary-hardening-suite.md
```

### C4: E2E suite name and manifest keys

```
Suite dir:  e2e/suites/boundary-hardening/   (build tag: e2e)
Manifest:   e2e/manifest.json -> suites entry {name: boundary-hardening},
            path_map entries covering internal/security/pii/ and
            internal/agent/taint_hooks.go, scenarios boundary-hardening-01..04
// Owner: 03-e2e/01-boundary-hardening-suite.md
// Consumers: pre-commit-e2e hook, make e2e-fast
```

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-inter-agent-taint/orchestrator.md | branch | R0-A | - | B |
| 02 | 02-pii-proxy/orchestrator.md | branch | R0-B | - | C |
| 03 | 03-e2e/orchestrator.md | branch | branches 01+02 REVIEWED | - | D |

Research leaves inside branches A and B dispatch first (parallel, group A of
each branch); implementation leaves are research-gated.

## Dispatch Protocol

For each concurrency group, in dependency order. Full recipe: skill
`hierarchical-plan-execution`.

### Phase 0: Research gate

Dispatch BOTH R0 research leaves (01-inter-agent-taint/R0-research.md and
02-pii-proxy/R0-research.md) in parallel via `delegate_task` (group A of
each branch). These are web-research + design-note leaves: web_search for
prompt-injection defense (branch A) and PII redaction practice (branch B),
written to the leaf's specified findings file, no production code. Review
in-session; findings gate the implementation leaves' design decisions
(over-blocking knobs for A; detector layering + restore scoping for B).

### Phase 1: Branch implementation

Each branch orchestrator runs its own dispatch protocol (its orchestrator.md
is authoritative). Branches A and B are file-disjoint EXCEPT both register
hooks in internal/daemon/components.go - serialize the components.go edits:
branch A registers the inter-agent-taint BeforeToolCall hook; branch B
registers the pii-restore TransformContext hook. If running concurrently,
whoever lands second re-reads components.go fresh before patching (Pitfall
19) and keeps registration adjacency: taint hook near security-before-tool
(components.go:1960-1963 neighborhood), pii-restore after secret-obfuscation
(components.go:1976-1984 neighborhood).

### Phase 2: Integration suite

After both branches reach REVIEWED, dispatch 03-e2e/01-boundary-hardening-suite.md
via its branch orchestrator.

### Phase 3: Root integration review

- Verify contract C1 ordering tests exist and pass.
- Run: `go build ./... && go test -p 2 ./internal/security/... ./internal/agent/... ./internal/daemon/... ./internal/config/...`
- Run: `make e2e-fast-area AREA=boundary-hardening`
- Gaps: re-dispatch the failing branch leaf with contract fixes (uncommitted).
- Pass: orchestrators commit per leaf (implementers NEVER commit), mark
  children COMPLETE, report COMPLETE upward.

## Review Checklist

The root orchestrator (main model) verifies each branch in-session:

- [ ] Every leaf of the branch implemented; tests written and passing
- [ ] Cross-branch contracts C1-C4 satisfied exactly
- [ ] Sanitizer C0-first order untouched (pinned test still passes)
- [ ] No swap-map value reaches logs / env / audit / trace (C2)
- [ ] Every new hook registered in internal/daemon/components.go with a
      typed-nil guard, proven by a test that fails without the wiring
- [ ] No scope creep; no debug artifacts; no line-number corruption
- [ ] All new feature tests live in the hermetic e2e tier (unit tests only
      for pure mechanisms inside internal/security packages where the
      existing suite already has unit coverage); new package dirs have
      manifest path_map entries in the SAME change

Output: APPROVED or a list of specific gaps.

## Coding Conventions

- **Language:** Go (module github.com/caimlas/meept); gofmt before reporting.
- **Naming:** exported only where a sibling package consumes; otherwise unexported.
- **Error handling:** wrap with %w; never ignore errors; no bare panic(err).
- **Typed-nil guards:** concrete-pointer-into-interface-field only behind
  `if p != nil`; every wiring seam gets a nil-dependency test through the
  REAL wiring path (internal/daemon/AGENTS.md rule).
- **IDs:** never time.Now().UnixNano()/math/rand - pkg/id.Generate();
  PII swap tokens use crypto/rand (predid analyzer enforced).
- **Mutexes:** never held across I/O (mutexio analyzer enforced).
- **Config:** JSON5, both `json:` and `toml:` struct tags (schema.go style).
- **Tests:** table-driven where natural; new feature tests hermetic e2e tier
  (e2e/suites/<name>/, `//go:build e2e`, manifest entry); `go test -p 2`.
- **ASCII only** in all files; no heredoc file creation; no git commit/add
  by leaf agents.
- **Do NOT commit. Do NOT run git add.** Orchestrators commit after review.

## Requirement Traceability

| Req | Source (user words, verbatim when possible) | Plan element (leaf/task/contract) | Verified by |
|-----|---------------------------------------------|-----------------------------------|-------------|
| R1 | "inter-agent sanitization (fix + wire the dead taint hook, proposal 5)" | branch 01: R0, 01-taint-redesign, 02-driver-wiring | e2e boundary-hardening-01/02 |
| R2 | "per-handoff labeling (NOT whole-message), declassify at the driver boundary" | 01-taint-redesign.md T1-T3 | unit + e2e |
| R3 | "deny-pattern set (agent impersonation, workspace_yield-style instruction forgery)" | 02-driver-wiring.md T3 | unit + e2e boundary-hardening-02 |
| R4 | "PII unmasking proxy with restore lane (proposal 8, implement fully)" | branch 02: R0, 01-detector-store, 02-restore-lane | e2e boundary-hardening-03 |
| R5 | "UUID-swap map with DURABLE session-scoped storage; restore bounded to the ORIGINATING session" | 01-detector-store.md T2; 02-restore-lane.md T2 | unit + e2e |
| R6 | "fix the existing dead Deobfuscate lane (secret placeholders)" | 02-restore-lane.md T4 | e2e boundary-hardening-04 |
| R7 | "config [security.pii] opt-in default false" | 02-restore-lane.md T3 | config unit test |
| R8 | "hook ordering must respect HookPriorityCritical peers - pin the pass order with tests" | contract C1; 01-detector-store.md T4 | sanitizer-ordering-class test |
| R9 | "placeholder leakage classes named and closed (logs, audit, trace capture)" | contract C2/C3 | e2e boundary-hardening-03 |
| R10 | "every hook must be WIRED in internal/daemon/components.go and proven by a test that would fail without the wiring" | branch leaves T-wiring tasks | wiring-failure tests |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-inter-agent-taint/R0-research.md | COMPLETE | 1 | landed c5ef67e9 (agent-boundary-taint-findings.md, reviewed) |
| 01-inter-agent-taint/01-taint-redesign.md | COMPLETE | 1 | landed 46661bbe (issue 84) |
| 01-inter-agent-taint/02-driver-wiring.md | COMPLETE | 1 | landed 1c573f80 (issue 84) |
| 02-pii-proxy/R0-research.md | COMPLETE | 1 | landed c5ef67e9 (pii-unmasking-findings.md, reviewed) |
| 02-pii-proxy/01-detector-store.md | COMPLETE | 1 | landed 46661bbe (issue 84) |
| 02-pii-proxy/02-restore-lane.md | COMPLETE | 1 | landed 1e4912cc (issue 84) |
| 03-e2e/01-boundary-hardening-suite.md | COMPLETE | 1 | landed 1781716d; 4/4 scenarios PASS (16.3s re-run at audit) |

Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS |
IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

1. `go build ./...` - clean build.
2. `go test -p 2 ./internal/security/... ./internal/agent/... ./internal/daemon/... ./internal/config/...` - all green, including the new
   ordering tests (C1) and wiring-failure tests (R10).
3. `go test -p 2 -tags e2e ./e2e/suites/boundary-hardening/` - scenarios:
   - boundary-hardening-01: legitimate collaboration flows with taint lane on
   - boundary-hardening-02: forged control message (agent impersonation /
     workspace_yield-style) neutralized
   - boundary-hardening-03: PII roundtrip with session-scoped restore + leak-class closure
   - boundary-hardening-04: secret `#XXXX#` placeholder restored at output
4. `make e2e-fast-area AREA=boundary-hardening` - green via harness.
5. `make e2e-fast` - no regression in existing suites.

## Test Gate

Tests-first gate: NOT used for this tree (research-gated pattern instead).
Coverage is pinned by the matrix below; each implementation leaf's TDD
table is the unit gate, and 03-e2e/01-boundary-hardening-suite.md is the
integration gate dispatched last.

| # | Feature (from plan) | Source (leaf/spec section) | Unit tests | E2E tests | Status |
|---|---------------------|----------------------------|-----------|-----------|--------|
| F1 | per-handoff taint labeling + declassify at driver boundary | 01-taint-redesign.md T1-T3 | TestCheckMessageTaint* in internal/agent | boundary-hardening-01 | VALIDATED |
| F2 | driver MarkUntrusted/RecordToolTaint at all 3 drivers + hook registration | 02-driver-wiring.md T1-T2 | wiring-failure test in internal/daemon | boundary-hardening-01 | VALIDATED |
| F3 | inter-agent deny patterns | 02-driver-wiring.md T3 | internal/security deny-pattern tests | boundary-hardening-02 | VALIDATED |
| F4 | PII detector + durable session swap store | 01-detector-store.md T1-T2 | internal/security/pii tests | boundary-hardening-03 | VALIDATED |
| F5 | restore-on-output, session-bounded + config [security.pii] | 02-restore-lane.md T1-T3 | restore + cross-session tests | boundary-hardening-03 | VALIDATED |
| F6 | secret Deobfuscate restore lane | 02-restore-lane.md T4 | secret-restore test | boundary-hardening-04 | VALIDATED |
| F7 | hook pass order pinned | contract C1; 01-detector-store.md T4 | ordering test | boundary-hardening-03 | VALIDATED |

Statuses: PENDING | DELTA (mismatch, resolution in progress) | VALIDATED

Delta log: [none yet]

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as
dated addenda. Never edit a dispatched leaf document mid-execution.

## Notes

- Verified anchors (grep-confirmed 2026-10-31) live in each leaf's
  "Verified Anchors" section; re-grep before editing - the file may have
  moved.
- Branch A's whole-message labeling flaw (taint_hooks.go:169) means the
  hook MUST NOT be registered before 01-taint-redesign lands - the
  wiring leaf (02) comes after the redesign leaf (01) inside branch A.
- Branch B's restore lane is also a latent-bug fix: today users literally
  see `#XXXX#` in replies.
- scripts/audit-secrets-value-callers.py may need its allowlist extended
  for the PII store's resolve-adjacent accessors - same review discipline.
- Forest closeout 2026-11-05: tables reconciled against HEAD by read-only audit; all leaves verified landed + gates green.
