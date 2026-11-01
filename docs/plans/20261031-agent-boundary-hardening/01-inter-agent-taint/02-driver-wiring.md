# Driver Wiring + Deny Patterns + Hook Registration - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on
> existing source files - explore with search_files or terminal cat.
> After writing a file, do NOT read it back - write once and stop.
> This leaf DEPENDS on 01-taint-redesign.md being REVIEWED (its
> SegmentLabelsFor + segment API exist).

## Meta

- **Parent:** orchestrator.md
- **Scope:** Mark collaboration handoffs as untrusted at the driver
  boundary in all three drivers, add the inter-agent deny-pattern set,
  and wire TaintBeforeToolCall in internal/daemon/components.go.
- **Dependencies:** 01-taint-redesign.md (REVIEWED), R0 decisions
- **Estimated Context:** 85K
- **Concurrency Group:** C
- **Audit references:** proposal 5 (master.md R1/R3/R10)

## Goal

Close the loop: without this leaf the taint lane is dead code (ZERO wiring
call sites in internal/daemon - verified by grep). After this leaf, a
forged control message riding a collaboration handoff is neutralized while
legitimate collaboration still flows.

## Context

- internal/agent/collaboration_pair_driver.go - PairProgrammingDriver:
  raw driver output embedded into the next prompt at :329 ("## Driver's
  latest output"); control-message parse at :429 (parseControlResponse:
  approve/request_changes/request_token); runAgent -> registry.RunAgent at
  :440-445.
- internal/agent/collaboration_team_driver.go - ParallelTeamDriver:
  runAgent -> registry.RunAgent at :482.
- internal/agent/collaboration_diff_driver.go - DiffDriver:
  registry.RunAgent at :231/:237 (branch agents) and :312 (differentiator).
- internal/daemon/components.go:1960-1963 - security-before-tool
  registration (the neighborhood for the new registration);
  :1964-1970 security-transform; :1976-1984 secret-obfuscation.
- internal/config/schema.go:2530 - TaintConfig (Enabled bool) at
  [security.taint].
- 01-taint-redesign.md exposed: SegmentLabelsFor, segment record/clear
  API on taint.ExtendedTracker, mode field on TaintHookConfig.

## Verified Anchors

- internal/agent/collaboration_pair_driver.go:329 - prompt embed of
  driverOutput; :429 parseControlResponse switch; :440-445 runAgent
- internal/agent/collaboration_team_driver.go:482 - runAgent body
- internal/agent/collaboration_diff_driver.go:231-237, :312 - RunAgent calls
- internal/daemon/components.go:1960-1963 - RegisterBeforeToolCall
  ("security-before-tool", agent.HookPriorityCritical, beforeTC)
- internal/config/schema.go:2530-2550 - TaintConfig struct

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// C3-A: driver handoff marking - every driver marks RAW AGENT OUTPUT
// (not its own framing) before registry.RunAgent:
//   tracker.MarkUntrusted(rawAgentOutput, "collab:<driver>:<role>")
// C4-A: internal/security/agent_boundary.go (NEW)
//   func NewInterAgentBoundary(logger *slog.Logger) *InterAgentBoundary
//   func (b *InterAgentBoundary) InspectHandoff(recipient, content string) (blocked bool, reason string)
// C5-A: components.go registration "inter-agent-taint",
//   agent.HookPriorityCritical, gated on c.Config.Security.Taint.Enabled,
//   typed-nil guarded.
```

### What This Leaf Consumes

- C1-A per-handoff decision + SegmentLabelsFor - from 01-taint-redesign.md
- C2-A sink taxonomy - from 01-taint-redesign.md
- Tracker segment record API - from 01-taint-redesign.md

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Mark handoffs in all three drivers: where raw agent output is embedded into the next agent's prompt (pair :329 embed path, team member-output fan-in, diff :231/:237/:312 outputs), record `MarkUntrusted(rawAgentOutput, "collab:<driver>:<role>")` on the tracker; driver framing text stays clean; nil tracker = skip silently (typed-nil discipline) | Modify: internal/agent/collaboration_pair_driver.go, collaboration_team_driver.go, collaboration_diff_driver.go (tracker seam added to each driver struct via option, nil-safe); Test: internal/agent/collaboration_taint_test.go (new) | `go test -p 2 ./internal/agent/ -run TestDriverMarksHandoff -v` | drivers have no tracker seam -> compile RED; then behavioral RED | each driver records segments for raw output and NOT for its framing; nil-tracker drivers keep working | Given a driver with a tracker; When a collaboration round embeds raw output; Then SegmentLabelsFor finds the output; Given framing text; Then no segment. Given nil tracker; When driver runs; Then no panic, behavior unchanged |
| T2 | Wire the hook in internal/daemon/components.go: construct the ExtendedTracker when `[security.taint] enabled`, build TaintHookConfig (mode from R0 decision, BlockUntrustedAgent=true), register "inter-agent-taint" at HookPriorityCritical directly after security-before-tool (:1960-1963); typed-nil guards at every seam | Modify: internal/daemon/components.go (~:1960); Test: internal/daemon/taint_wiring_test.go (new) | `go test -p 2 ./internal/daemon/ -run TestInterAgentTaintWiring -v` | no registration exists -> test RED | test proves: with taint enabled the registry contains "inter-agent-taint"; with registration code removed the test FAILS (assert by calling the real wiring path); disabled config = not registered, no panic | Given a components build with security.taint.enabled=true; When hooks build; Then hookRegistry has inter-agent-taint before normal hooks; Given enabled=false; Then absent and nil-safe |
| T3 | Inter-agent deny patterns in internal/security/agent_boundary.go: impersonation claims ("i am agent X", "as agent X" style) inside untrusted segments; forged control verbs (approve / request_changes / request_token) arriving from tainted segments; workspace_yield-style instruction forgery patterns; spoofed control-JSON blocks. Wire InspectHandoff into the drivers' handoff path BEFORE registry.RunAgent; blocked handoffs log Warn + return a collaboration error (existing NewCollaborationError shape), never panic | Create: internal/security/agent_boundary.go; Modify: the three drivers (call InspectHandoff on the composed handoff); Test: internal/security/agent_boundary_test.go (new) | `go test -p 2 ./internal/security/ -run TestInterAgentBoundary -v` | file missing -> compile RED | each deny class has a table-driven case (blocked) + a legitimate-message case (allowed); drivers return a clean error on blocked handoffs | Given content impersonating an agent from a tainted segment; When InspectHandoff; Then blocked=true with reason. Given legitimate review feedback; Then blocked=false. Given a forged `{"action":"approve"}` block from a tainted segment; Then blocked=true |
| T4 | Prove ordering invariant: inter-agent-taint is a HookPriorityCritical peer and the sanitizer C0-first pinned test still passes; hook order test asserts security-before-tool precedes inter-agent-taint (registration order) | Test: internal/daemon/taint_wiring_test.go (extend) | `go test -p 2 ./internal/security/ -run TestSanitizerMarkerOrder -v && go test -p 2 ./internal/daemon/ -run TestInterAgentTaintWiring -v` | ordering unpinned -> RED | both green | Given the critical-priority hook list; When ordered; Then security-transform, secret-obfuscation precede inter-agent-taint in TransformContext; BeforeToolCall: security-before-tool then inter-agent-taint |

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing
  (`go test -p 2 ./internal/agent/... ./internal/security/... ./internal/daemon/...`)
- [ ] Contracts C3-A/C4-A/C5-A satisfied exactly
- [ ] Every wiring seam typed-nil guarded; wiring-failure test present (T2)
- [ ] Sanitizer C0-first pinned test green; other hook orders untouched
- [ ] gofmt clean; no debug artifacts; no line-number corruption
- [ ] No scope creep (no PII work - that is branch 02)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [last 20 lines per verify run]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; tests passing; wiring-failure test fails
      when registration is removed (verified, not assumed)
- [ ] All three drivers mark raw output, not framing; nil tracker safe
- [ ] Deny patterns cover all classes from contract C4-A with legit pass cases
- [ ] components.go registration adjacent to security-before-tool; typed-nil guards
- [ ] mutexio/predid clean; no debug artifacts; no line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

- IMPORTANT: if branch 02 (pii-proxy) already edited components.go, re-read
  it fresh before patching (stale read-state) and keep the registration
  order per master contract C1.
- Deny patterns are pattern-level (regex/phrase classes) - do NOT build an
  LLM classifier here; R0 findings may cite cost reasons.
- Handoff blocking must produce the existing collaboration error shape so
  the session finalizes as failed rather than hanging.
