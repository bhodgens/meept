# Tool Provider Seam + Centralized Security Gating - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Root
- **Parent:** none
- **Children:** 6 leaf documents under this node
- **Scope:** A compiled-in tool provider seam (generalizing the proven
  githubsvc.ToolFactories factory pattern) plus centralized per-execution
  security gating in Registry.Execute, so every capability - native or
  provider-contributed - passes identical security mechanisms.

GitHub issue: https://github.com/bhodgens/meept/issues/78

## Goal

Meept's native capabilities register through hundreds of direct calls in
internal/daemon/components.go with no grouping seam. Security enforcement is
fragmented: `SecurityBeforeToolCall` (internal/agent/security_hooks.go:31)
scans only `shell`; `Registry.Execute` (internal/tools/registry.go:303)
enforces schema validation only; executions are slog-logged, never
audit-chain anchored; the effects idempotency ledger is used only by the
github_* tools. Adding a native capability today means editing daemon wiring
and inheriting whatever gates the author remembered to add.

This tree builds: (1) a `ToolProvider` seam in a new `internal/toolprov`
package, generalizing the factory inversion already proven by
`githubsvc.ToolFactories`; (2) centralized gating inside `Registry.Execute`
- security scan for every tool category, audit-chain emission, effects
ledger for mutating tools, fail-closed defaults; (3) migration of existing
capability groups into providers; (4) a config surface (enable/disable per
provider, doctor reporting); (5) hermetic e2e proof that a test provider's
tools pass the identical gates as native tools.

Source discussion (2026-10-31): user asked whether meept is pluggable and
required that "everything added use the same security mechanisms and
constraints that native functionality has." Verbatim requirements: a
provider seam abstracting existing capabilities; forced identical security
constraints; MCP remains the separate dynamic/external plugin path
(subprocess, GPL/AGPL-safe) - this seam is the compiled native path.

## Architecture

New package `internal/toolprov` defines `Provider` (Name, Discover,
PermissionClass, secrets broker binding) and a package-level
`Register(name, factory)` map populated by provider packages' `init()` -
the exact inversion already proven by `githubsvc.ToolFactories`
(internal/githubsvc/register.go:23, populated by
internal/tools/builtin/github_register.go:24). `builtin` stays the only
package allowed to import provider packages plus `toolprov`; binaries that
do not link `builtin` get a descriptive registration error, never a panic.
`internal/daemon/components.go` replaces each migrated capability group's
direct `Register` calls with one `toolprov.RegisterAll(registry, cfg)` walk.

Centralized gating lives in `Registry.Execute` (registry.go:303), NOT in
hooks: per the wave-proven rule "a claim gate enforced by only ONE caller of
a shared sink is not enforced at all," every execution passes through four
ordered gates after existing `ValidateToolArgs` - (G1) security orchestrator
scan keyed by tool category (shell keeps its deepest Tirith tier), (G2)
auditlog hash-chain emission per execution, (G3) effects ledger reservation
for mutating tools, (G4) fail-closed posture inheritance: any tool from a
provider with no declared posture gets MCPTool's defaults
(IsReadOnly=false, concurrency-unsafe). Gating is injected via a
`Registry.SetGate(...)` option wired in daemon components; a nil gate
preserves today's behavior byte-identically for non-daemon test usage.

Config: `[tool_providers]` map in meept.json5 (enabled/disabled per
provider), surfaced by `meept doctor` report-only.

## Interface Contracts

### C1: Provider seam

```go
// internal/toolprov/provider.go
package toolprov

type RiskClass string
const (
    RiskReadOnly   RiskClass = "read_only"
    RiskMutating   RiskClass = "mutating"
    RiskPrivileged RiskClass = "privileged"
)

type Provider interface {
    Name() string
    Discover() []tools.Tool
    PermissionClass() RiskClass   // ceiling for ALL tools the provider contributes
    Broker() *secrets.Broker      // two-lane secret access; placeholder-only toward children
}

// Registered by provider packages via init(); consumed by daemon wiring.
var factories map[string]func() Provider  // + Register(name string, f func() Provider)
```

- Owner: 01-seam.md
- Consumers: 02-gate.md, 03-migrate.md, 04-config.md, 05-e2e-gate.md

### C2: Registry gate injection

```go
// internal/tools/registry.go
type ExecGate interface {
    BeforeExecute(ctx context.Context, tool tools.Tool, args map[string]any) error
    AfterExecute(ctx context.Context, tool tools.Tool, args map[string]any, res *ToolResult, err error)
}
func (r *Registry) SetGate(g ExecGate)  // nil = today's behavior, byte-identical
```

- Owner: 02-gate.md
- Consumers: 03-migrate.md, 05-e2e-gate.md
- G1/G2/G3/G4 run inside the daemon's gate implementation (02), not in the
  seam; the gate interface carries no security-package import so
  internal/tools never imports internal/security (import-cycle rule).

### C3: Provider config schema

```go
// internal/config/schema.go
type ToolProvidersConfig struct {
    Providers map[string]ToolProviderEntry  // key = provider Name()
}
type ToolProviderEntry struct {
    Enabled bool            // default true for migrated native groups; opt-in for new
}
```

- Owner: 04-config.md
- Consumers: 01-seam.md, 03-migrate.md

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | 00T-gate-tests.md | leaf (test gate) | none | 60K | A |
| 01 | 01-seam.md | leaf | 00T | 80K | A |
| 02 | 02-gate.md | leaf | 00T | 90K | A |
| 03 | 03-migrate.md | leaf | 01, 02 | 85K | B |
| 04 | 04-config.md | leaf | 01 | 55K | B |
| 05 | 05-e2e-gate.md | leaf | 01, 02, 03 | 70K | C |

Group A dispatches together after the test gate validates. Group B after
Group A reviews. 05 is last (proves the integrated tree live).

## Dispatch Protocol

For each concurrency group, in dependency order. Full recipe: skill
`hierarchical-plan-execution`.

### Phase 0: Test Gate

Dispatch 00T-gate-tests.md first: tests only, fail RED for
missing-implementation reasons. Validate in-session (never delegate review)
against the Test Gate matrix below; apply the delta protocol; commit gate
tests (parked under .staged extension only if the tree's own leaves cannot
compile alongside sibling WIP - see references/red-gate-test-parking.md);
set all children TESTS_VALIDATED.

### Phase 1: Dispatch Concurrency Group A

Dispatch 01 and 02 simultaneously via `delegate_task`: goal = "Implement
all tasks from <leaf>"; context = full leaf text + C1/C2 contracts +
conventions + relevant source INLINED; include the no-commit, no-git-add,
no-read_file, write-once-and-stop rules.

### Phase 2: Review and Commit Each Child

Review in-session (the main model - never a delegated reviewer). Check
leaf spec, contracts C1/C2, Review Checklist; run the leaf's verify
commands; gate-test diff must be empty. Pass: commit with explicit
pathspecs + status REVIEWED. Gaps: re-dispatch with findings, max 3
cycles, then escalate.

### Phase 3: Groups B and C

04 + 03 dispatch after 01/02 reach REVIEWED (04 needs only 01). 05
dispatches last. Same dispatch/review/commit loop.

### Phase 4: Integration Review

After ALL children REVIEWED: run the full scoped suite
(`go test -p 2 ./internal/tools/... ./internal/toolprov/... ./internal/daemon/...`),
run `make graphs-check` if any RPC/HTTP surface changed (it should not),
verify no import cycle (internal/toolprov must not import builtin;
internal/tools must not import internal/security), regenerate docs per
AGENTS.md feature-doc rule, set children COMPLETE, close the issue with
the evidence summary.

## Review Checklist

The orchestrator (main model) verifies each child in-session:

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts C1/C2/C3 satisfied exactly
- [ ] All specified files created/modified at exact paths
- [ ] Gate tests pass GREEN; `git diff` shows no gate-test modification
- [ ] Tests written and passing (TDD followed)
- [ ] No import cycle introduced (toolprov<-builtin direction preserved)
- [ ] No scope creep; no obvious bugs or security issues
- [ ] No debug artifacts, TODOs, placeholders, commented-out code
- [ ] No line-number corruption in source files

Output: APPROVED or a list of specific gaps.

## Coding Conventions

- **Language:** Go (repo toolchain pinned: go.mod declares go1.26.5; make
  targets and hooks export it - bare `go` outside make/hooks breaks x/tools)
- **Naming:** exported symbols only where consumed cross-package; provider
  names are lowercase strings matching config keys
- **Error handling:** wrap with %w, return early; no ignored errors
  (pre-commit enforced); typed-nil guards on all Set* accessors
- **Testing:** table-driven; new feature tests go in the hermetic e2e tier
  (e2e/suites/<name>/ + manifest entry in the SAME commit - fail-closed
  pre-commit hook); unit tests only for bug-fix classes
- **IDs:** never time.Now().UnixNano() / math/rand; pkg/id.Generate()
- **Mutex:** never hold across I/O (mutexio analyzer enforced)
- **Formatting:** gofmt before reporting; ASCII-only files (pre-commit
  ascii gate - no em-dashes in new files)

## Requirement Traceability

| Req | Source (user words, verbatim when possible) | Plan element (leaf/task/contract) | Verified by |
|-----|---------------------------------------------|-----------------------------------|-------------|
| R1 | "add additional native capabilities" via a pluggable architecture | 01-seam.md (C1 Provider) | 05-e2e-gate.md suite: test provider registers and serves |
| R2 | "abstract existing capabilities into" the seam | 03-migrate.md (github, scheduler, hermes, acp groups) | 00T gate tests: migrated groups serve via provider path |
| R3 | "force everything added to use the same security mechanisms and constraints that native functionality has" | 02-gate.md (C2 ExecGate; G1-G4 in Registry.Execute) | 00T gate tests: gate runs for EVERY tool category; 05 e2e: provider tool blocked/audited identically to native |
| R4 | fail-closed posture for undeclared providers | 02-gate.md G4 (MCPTool posture inheritance) | 00T gate test: undeclared-posture tool is read-only=false |
| R5 | config + doctor surfacing | 04-config.md (C3; doctor report-only) | 00T gate test: disabled provider's tools absent from registry |
| R6 | MCP stays the separate external path (no linking external plugins) | Architecture constraint, all leaves | Review checklist: no MCP import into toolprov path |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-gate-tests.md | PENDING | 0 | |
| 01-seam.md | PENDING | 0 | |
| 02-gate.md | PENDING | 0 | |
| 03-migrate.md | PENDING | 0 | |
| 04-config.md | PENDING | 0 | |
| 05-e2e-gate.md | PENDING | 0 | |

Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS |
IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

Gate rule: implementation children stay PENDING until 00T reaches
TESTS_VALIDATED.

## Integration Test Plan

1. `go test -p 2 ./internal/tools/... ./internal/toolprov/...` - seam +
   gate units green.
2. `go test -p 2 ./internal/daemon/...` - wiring green; registry built with
   gate; migrated groups present.
3. `make e2e-fast-area AREA=tool-provider-seam` - the 05 suite: scratch
   daemon boots with a test provider enabled AND disabled; enabled case
   proves a provider tool call passes security scan + lands an auditlog
   chain entry + a disabled provider's tool is absent from the registry
   (asserted through the live RPC surface, per the proven live-gate rule).
4. `make lint-ci` scoped to touched packages if full sweep is blocked by
   sibling WIP (record the blocked gate + scoped result in the commit body).

## Test Gate

Test leaf: 00T-gate-tests.md - dispatched in Phase 0, validated by the
control agent (orchestrator, in-session).

| # | Feature (from plan) | Source (leaf/spec section) | Unit tests | E2E tests | Status |
|---|---------------------|----------------------------|-----------|-----------|--------|
| F1 | Provider registration + discovery | 01-seam.md T1-T2 | TestProviderRegisterDiscover | n/a (units cover the seam contract) | PENDING |
| F2 | Gate runs for every tool category, not just shell | 02-gate.md T1 | TestExecGateAppliesToAllCategories | 05 suite: provider tool gated | PENDING |
| F3 | Audit chain entry per execution | 02-gate.md T2 | TestExecGateAuditEmission | 05 suite: chain entry after RPC tool call | PENDING |
| F4 | Effects ledger reservation for mutating tools | 02-gate.md T3 | TestExecGateLedgerMutatingOnly | n/a | PENDING |
| F5 | Fail-closed posture inheritance (G4) | 02-gate.md T4 | TestUndeclaredPostureFailClosed | n/a | PENDING |
| F6 | Nil gate = byte-identical legacy behavior | 02-gate.md T5 | TestNilGatePreservesBehavior | n/a | PENDING |
| F7 | Migrated groups serve via provider path | 03-migrate.md T1-T3 | TestMigratedGroupsViaProviders | 05 suite | PENDING |
| F8 | Config enable/disable per provider | 04-config.md T1-T2 | TestProviderConfigDisable | 05 suite: disabled = absent from registry | PENDING |
| F9 | Doctor surfaces provider states (report-only) | 04-config.md T3 | TestDoctorReportsProviders | n/a | PENDING |

Statuses: PENDING | DELTA (mismatch, resolution in progress) | VALIDATED

Delta log: (empty - fill on any mismatch with date, feature ID, direction,
one-line reason)

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as
dated addenda. Never edit a dispatched leaf document mid-execution.

## Notes

- Registry.Execute changes touch every tool's test suite - expect broad
  but shallow breakage; the nil-gate compatibility test (F6) is the
  pin that keeps non-daemon usages green.
- Known sibling-WIP hazard: internal/agent and internal/daemon carry WIP
  from the activity-views tree in some worktrees; use the overlay-worktree
  pattern for gates blocked by sibling state and record it in commit bodies.
- internal/tools must NEVER import internal/security (the ConfigLoads test
  precedent: import cycles are broken via seam interfaces - C2 carries no
  security import).
- MCP tools keep their own fail-closed adapter; they do not migrate into
  the seam this tree (R6) - a follow-up may wrap MCPTool as a provider
  later if desired.
