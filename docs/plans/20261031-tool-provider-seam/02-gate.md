# Centralized Execution Gate (Registry.SetGate) - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The committed gate tests already
> exist and fail RED - make them pass; do NOT delete, skip, weaken, or
> rewrite any gate test (report suspected-wrong gate tests to the
> orchestrator instead). Do NOT commit - the orchestrator handles all git
> operations after review. Do NOT use read_file on existing source files -
> explore with search_files or terminal cat. After writing a file, do NOT
> read it back - write once and stop.

## Meta

- **Parent:** ../master.md
- **Scope:** C2: the `ExecGate` interface + `Registry.SetGate`, and the
  daemon-side gate implementation wiring G1 (security scan per category),
  G2 (audit-chain emission), G3 (effects ledger for mutating tools),
  G4 (fail-closed posture inheritance).
- **Dependencies:** 00T-gate-tests.md (TESTS_VALIDATED)
- **Estimated Context:** 90K
- **Concurrency Group:** A
- **Audit references:** GitHub issue #78; C2 in master.md

## Goal

Move per-execution enforcement into `Registry.Execute` via an injected gate
so EVERY tool category passes identical checks - closing the gap where
`SecurityBeforeToolCall` (internal/agent/security_hooks.go:31) scans only
shell. The gate is the enforcement point; the claim-gate rule "a gate
enforced by only ONE caller of a shared sink is not enforced at all" is the
design driver: gates live in the sink (Execute), not in callers.

## Context

- internal/tools/registry.go:303-355 - `Execute`: ValidateToolArgs runs
  first (keep); the gate's BeforeExecute runs between validation and
  tool.Execute; AfterExecute runs after result construction. Both must fire
  even when the tool returns an error result.
- internal/tools/interface.go:24 - Tool interface; IsReadOnly/Category feed
  G3/G1 classification.
- internal/tools/mcp/tool.go:105-111 - fail-closed posture being inherited
  by G4.
- internal/agent/security_hooks.go:31-58 - existing shell scan; its
  orchestrator.ScanShellCommand call becomes the shell tier of G1 (the hook
  stays; it becomes redundant-but-harmless for shell, or delegates - do not
  delete the hook in this leaf).
- internal/security/orchestrator.go - Orchestrator.ScanShellCommand and
  engine; G1's scanner.
- internal/security/audit.go, internal/auditlog - G2's chain emission.
- internal/effects - Ledger; github_register.go shows the reservation
  pattern to mirror for G3.
- IMPORT RULE: internal/tools must NOT import internal/security or
  internal/auditlog. C2's ExecGate interface uses only tools-package types;
  the concrete gate implementing it lives in internal/daemon (or a small
  internal/toolgate package if daemon grows too large) and imports the
  security packages.

## Verified Anchors

- internal/tools/registry.go:303 - `func (r *Registry) Execute(...)` -
  injection point.
- internal/tools/registry.go:315-331 - existing ValidateToolArgs block -
  unchanged, gate goes after it.
- internal/agent/security_hooks.go:31 - `SecurityBeforeToolCall` - shell
  only today; reference for G1 shell tier.
- internal/tools/mcp/tool.go:105 - `IsReadOnly returns false for MCP tools
  (fail-closed)` - the G4 posture source.
- internal/effects/effects.go (verify path) - Ledger interface for G3.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/tools/registry.go
type ExecGate interface {
    BeforeExecute(ctx context.Context, tool Tool, args map[string]any) error
    AfterExecute(ctx context.Context, tool Tool, args map[string]any, res *ToolResult, err error)
}
func (r *Registry) SetGate(g ExecGate)  // nil restores today's behavior byte-identically
func (r *Registry) Gate() ExecGate      // nil-safe read for tests
```

```go
// internal/daemon/toolgate.go (new; or internal/toolgate - implementer
// picks based on components.go size, record choice in report)
type GateConfig struct {
    Orch   *intsecurity.Orchestrator
    Audit  AuditEmitter      // minimal interface satisfied by the auditlog chain store
    Ledger effects.Ledger
    PostureLookup func(name string) (toolprov.RiskClass, bool)  // nil = nothing declared
}
func NewExecGate(cfg GateConfig) *ExecGateImpl   // implements tools.ExecGate
```

G-order inside BeforeExecute: G4 posture resolve -> G1 scan -> (G3
reservation happens in BeforeExecute for mutating tools). G2 emission
happens in AfterExecute for every execution, including errors.

### What This Leaf Consumes

- C1 RiskClass (01-seam.md) - `toolprov.RiskClass` used in PostureLookup
  signature. toolgate imports toolprov + security + effects; tools imports
  neither.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | ExecGate + SetGate + per-category invocation | Modify: internal/tools/registry.go | `go test -p 2 ./internal/tools/ -run TestExecGateAppliesToAllCategories` | FAIL: SetGate undefined or gate skips categories | Every stub category triggers Before+After | Given a stub gate + stub tools of categories shell/file/mcp/custom; When each Execute runs; Then BeforeExecute+AfterExecute fire for every one, BeforeExecute error blocks tool.Execute and returns a typed error result naming the reason |
| T2 | G2 audit emission in daemon gate | Create: internal/daemon/toolgate.go; Test additions in internal/tools/registry_gate_test.go where interface-only | `go test -p 2 ./internal/daemon/ -run TestExecGateAudit` | FAIL: no audit call on execution | AuditEmitter receives tool name, args hash, outcome, duration for success AND error results | Given a gate wired with a recording emitter; When a tool succeeds and another errors; Then two chain-shaped emission calls land with outcome success/error respectively |
| T3 | G3 ledger reservation, mutating only | Modify: internal/daemon/toolgate.go | `go test -p 2 ./internal/daemon/ -run TestExecGateLedger` | FAIL: reservation missing or fires for read-only | Reservation before Execute iff IsReadOnly=false; read-only skips | Given mutating + read-only stub tools through the gate; When each executes; Then exactly one Ledger reserve+commit pair for the mutating call, zero for read-only |
| T4 | G4 fail-closed posture inheritance | Modify: internal/daemon/toolgate.go | `go test -p 2 ./internal/daemon/ -run TestUndeclaredPosture` | FAIL: undeclared tools treated as trusted | PostureLookup miss => IsReadOnly reported false to G1/G3 regardless of tool claim | Given a stub tool claiming IsReadOnly=true with an undeclared provider; When Execute runs; Then G3 still reserves (treated mutating) and G1 applies privileged-tier scan |
| T5 | Nil-gate byte-identical compatibility | Modify: internal/tools/registry.go | `go test -p 2 ./internal/tools/ -run TestNilGate` (already green from 00T - keep green) | n/a (compatibility pin) | All pre-existing internal/tools tests green unchanged | Given a registry with no gate; When any Execute runs; Then behavior and results match today's code path exactly |

## Self-Verification Checklist

- [ ] All tasks implemented; F2-F6 gate tests passing
- [ ] Contract C2 satisfied exactly; internal/tools imports no security,
      auditlog, effects, or toolprov package
- [ ] ValidateToolArgs still runs BEFORE the gate
- [ ] AfterExecute fires on error results (not just Go errors)
- [ ] Nil gate path untouched: full pre-existing internal/tools suite green
- [ ] gofmt clean; ASCII-only; no scope creep

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [paths]
2. **Verify command + output:** [last 20 lines]
3. **Deviations:** [none / list; include toolgate package-location choice]

## Review Checklist (For Review Agent)

- [ ] Gate fires for every category; error results also emit audit (F2/F3)
- [ ] Ledger reserved for mutating only; posture miss fails closed (F4/F5)
- [ ] Nil gate = byte-identical (F6 + full suite green)
- [ ] Import direction verified; no cycle
- [ ] No gate-test modifications

Output: APPROVED or specific gaps with file + line references.

## Notes

- The shell hook in internal/agent/security_hooks.go stays; a follow-up
  tree may deduplicate it against G1. Do not remove or weaken it here.
- Scan tiers: shell = orchestrator.ScanShellCommand (deepest); other
  categories = engine rules keyed by Category(); unknown category = most
  restrictive tier. Fail CLOSED: a scanner error blocks, never allows.
- SetGate must be called BEFORE any Execute; document that ordering in the
  SetGate doc comment. Typed-nil guard per repo setters rule.
