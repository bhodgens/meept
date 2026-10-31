# Test Gate: Tool Provider Seam - Implementation Leaf

> **For the implementing agent:** You are the test-gate implementer for this
> leaf. Write TESTS ONLY - no implementation. Every test must fail RED for
> missing-implementation reasons (undefined symbols are NOT valid RED - the
> tests must compile against stubs if needed; prefer compiling against the
> planned contract signatures placed as minimal stubs in the SAME test-only
> commit only where the compiler requires it). Do NOT implement the seam or
> gate logic. Do NOT commit - the orchestrator handles all git operations
> after review. Do NOT use read_file on existing source files - explore with
> search_files or terminal cat. After writing a file, do NOT read it back.

## Meta

- **Parent:** ../master.md
- **Scope:** Full RED test suite for contracts C1 (provider seam), C2
  (registry gate), C3 (provider config), and the migration/e2e features
  F7-F9 - the complete gate matrix in master.md.
- **Dependencies:** none
- **Estimated Context:** 60K
- **Concurrency Group:** A
- **Audit references:** GitHub issue #78

## Goal

Produce the failing test suite the implementation leaves make pass. Tests
pin: provider registration/discovery, per-category gate enforcement, audit
emission, ledger reservation for mutating tools only, fail-closed posture
inheritance, nil-gate byte-identical compatibility, migrated-group routing,
config enable/disable, doctor reporting.

## Context

Repo: /Users/caimlas/git/meept (Go, toolchain pinned go1.26.5 via make).
Key existing surfaces:
- internal/tools/registry.go - Registry, Execute (line ~303),
  ValidateToolArgs already enforced before Execute.
- internal/tools/interface.go:24 - Tool interface (Name, Description,
  Parameters, Execute, IsReadOnly, IsConcurrencySafe, Category).
- internal/tools/mcp/tool.go:105-111 - the fail-closed posture to inherit
  (IsReadOnly=false, IsConcurrencySafe=false).
- internal/githubsvc/register.go:23 - the factory-seam pattern C1 mirrors.
- internal/config/schema.go - config struct home (C3 lands here).
- internal/security/audit.go + internal/auditlog - audit-chain targets.
- internal/effects - idempotency ledger (G3).

## Verified Anchors

- internal/tools/registry.go:303 - `func (r *Registry) Execute(...)` - the
  gate injection point; tests call Execute on a registry with a stub gate.
- internal/tools/interface.go:24 - `type Tool interface` - stub tools
  implement this.
- internal/githubsvc/register.go:23 - `var ToolFactories func(...)` - the
  init-populated pattern C1 generalizes.

## Interface Contracts (From Parent)

### What This Leaf Exposes

Test files (package-internal test files, not _test packages, so they can
touch unexported symbols):
- internal/tools/registry_gate_test.go - F2, F3, F4, F5, F6 tests
- internal/toolprov/provider_test.go - F1 tests (file created even though
  package is new; contains the contract-shape assertions)
- internal/config/toolproviders_test.go - F8, F9 schema tests
- internal/daemon/toolproviders_wiring_test.go - F7 routing assertions

### What This Leaf Consumes

- C1 Provider (01-seam.md) - asserted via compile-against-shape; where the
  package does not yet exist, create `internal/toolprov/provider.go` as a
  MINIMAL types-only stub (struct + interface + Register map, zero logic)
  so the RED fails on assertion, not compilation. Mark the file's head with
  `// stub: implementation lands in 01-seam.md`.
- C2 ExecGate (02-gate.md) - same rule: stub interface in registry.go only
  if required to compile; logic tests must fail RED.
- C3 ToolProvidersConfig (04-config.md) - stub struct in schema.go if
  required; defaults tests fail RED.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | F1 seam tests | Create: internal/toolprov/provider_test.go; Stub: internal/toolprov/provider.go | `go test -p 2 ./internal/toolprov/` | FAIL: Register/Discover/PermissionClass not implemented | All green post-01 | Given two providers registered via init; When RegisterAll walks factories; Then both Discover() tool sets land in a Registry and duplicate names error |
| T2 | F2-F6 gate tests | Create: internal/tools/registry_gate_test.go | `go test -p 2 ./internal/tools/ -run TestExecGate` | FAIL: SetGate undefined or gate never invoked | All green post-02 | Given a registry with stub gate + stub tools across categories; When Execute runs; Then BeforeExecute/AfterExecute fire for EVERY category (F2), audit hook observes result (F3), ledger reserved only when IsReadOnly=false (F4), posture-undeclared tool reports read-only=false (F5), and a nil-gate registry behaves byte-identically to today (F6) |
| T3 | F8-F9 config tests | Create: internal/config/toolproviders_test.go | `go test -p 2 ./internal/config/ -run TestToolProviders` | FAIL: ToolProvidersConfig missing | All green post-04 | Given meept.json5 with providers.A disabled; When config loads; Then entry parses with Enabled=false and default-true applies when absent (F8); doctor report lists provider states without mutating anything (F9) |
| T4 | F7 wiring tests | Create: internal/daemon/toolproviders_wiring_test.go | `go test -p 2 ./internal/daemon/ -run TestProviderWiring` | FAIL: components.go registers groups directly, not via providers | All green post-03 | Given a Components built with gate; When registry lists tools; Then github/scheduler/hermes/acp groups resolve through their providers (presence + category unchanged) |

## Self-Verification Checklist

- [ ] Every test fails RED for an implementation-shaped reason (assertion
      on missing behavior), never compile error
- [ ] Compiles: `go build ./internal/...` green with stubs in place
- [ ] No implementation logic written
- [ ] All files at exact specified paths
- [ ] ASCII-only files

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created path]
2. **Verify command + output:** [last 20 lines showing RED failures]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Each test observed RED with the expected missing-behavior message
- [ ] Mutation check: stubbing the planned behavior makes tests GREEN
      (guard verified by its failure mode)
- [ ] No implementation logic smuggled in
- [ ] Exact paths per task table

Output: APPROVED or specific gaps with file + line references.

## Notes

- The nil-gate test (F6) is the compatibility pin: it must pass BOTH
  before and after 02 lands (it pins today's behavior), so author it to
  pass immediately - it is the only non-RED test in this leaf.
- gofmt every file; no em-dashes anywhere (pre-commit ascii gate).
