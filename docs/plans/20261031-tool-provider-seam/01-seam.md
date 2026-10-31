# Provider Seam (internal/toolprov) - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The committed gate tests already
> exist and fail RED - your job is to make your leaf's tests pass; do NOT
> delete, skip, weaken, or rewrite any gate test (report suspected-wrong
> gate tests to the orchestrator instead). Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing a
> file, do NOT read it back - write once and stop.

## Meta

- **Parent:** ../master.md
- **Scope:** The `internal/toolprov` package: Provider interface, RiskClass,
  init-populated factory map, RegisterAll walk, duplicate-name protection.
- **Dependencies:** 00T-gate-tests.md (TESTS_VALIDATED)
- **Estimated Context:** 80K
- **Concurrency Group:** A
- **Audit references:** GitHub issue #78; C1 in master.md

## Goal

Deliver contract C1: a compiled-in provider seam generalizing the proven
`githubsvc.ToolFactories` inversion. Provider packages self-register via
init(); daemon wiring walks `toolprov.RegisterAll` instead of per-tool
direct calls. A binary that links no providers registers none; a duplicate
provider name is a registration-time error, never a silent overwrite.

## Context

- internal/githubsvc/register.go:10-30 - the seam pattern to mirror
  (package-level factory var, init-populated by builtin, nil = descriptive
  error in the enabled case only).
- internal/tools/builtin/github_register.go:24 - the init() side.
- internal/tools/registry.go:75 - `Registry.Register` (the walk's target).
- internal/secrets - Broker two-lane contract: ChildValue/ChildValues give
  placeholders only; Value is in-process-allowlisted. The Provider's
  Broker() binding carries the same rule - providers authenticate via
  Value in-process, never pass values to children.
- IMPORTANT import rule: `internal/toolprov` imports `internal/tools` (for
  the Tool type) and `internal/secrets` only. It must NEVER import
  `internal/tools/builtin`, `internal/githubsvc`, or any provider package
  - the direction is provider -> toolprov -> tools, with builtin populating
  factories from its own init().

## Verified Anchors

- internal/githubsvc/register.go:23 - `var ToolFactories func(...)` - the
  pattern being generalized (read for shape, do not modify).
- internal/tools/registry.go:75 - `func (r *Registry) Register(tool Tool)` -
  the target of RegisterAll's walk.
- internal/tools/registry.go:149 - `func CanonicalName(tool Tool) string` -
  use for duplicate detection.
- internal/secrets/broker.go (verify exact path with search_files) -
  Broker type for the C1 Broker() return.

## Interface Contracts (From Parent)

### What This Leaf Exposes

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
    PermissionClass() RiskClass
    Broker() *secrets.Broker
}

func Register(name string, f func() Provider) error  // duplicate name -> error
func Factory(name string) (func() Provider, bool)    // lookup, no construction
func Names() []string                                // sorted, for doctor/config
func RegisterAll(r *tools.Registry, names []string) (int, error)
// RegisterAll constructs each named provider, registers every Discover()
// tool into r, returns count; unknown name -> error listing known names.
```

### What This Leaf Consumes

- C3 ToolProvidersConfig (04-config.md) - NOT consumed here; 01 accepts a
  plain []string of enabled provider names so toolprov never imports
  internal/config (cycle safety; 04 does the mapping).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Types + Register/Factory/Names with duplicate protection | Create: internal/toolprov/provider.go (replaces 00T stub) | `go test -p 2 ./internal/toolprov/ -run TestProviderRegister` | FAIL: Register accepts duplicates / Names unsorted | Duplicate errors; sorted Names; Factory returns construction thunk without invoking | Given two Register calls with the same name; When the second runs; Then it returns an error naming the clash and the first factory is untouched |
| T2 | RegisterAll walk | Modify: internal/toolprov/provider.go; Test: internal/toolprov/provider_test.go | `go test -p 2 ./internal/toolprov/ -run TestRegisterAll` | FAIL: RegisterAll undefined | Unknown-name error lists known names; tools land in registry; count correct | Given a registry + names ["alpha","missing"]; When RegisterAll runs; Then alpha's tools register, count=alpha tool count, error names "missing" and lists ["alpha"] |
| T3 | builtin populates factories; daemon walks the seam for ONE group as proof | Modify: internal/tools/builtin/github_register.go (register a "github" provider via toolprov.Register in init alongside the existing githubsvc.ToolFactories fill - do not remove it); Modify: internal/daemon/components.go (github group's Register calls routed through toolprov.RegisterAll) | `go test -p 2 ./internal/daemon/ -run TestProviderWiring` | FAIL: wiring still direct-registers | F7 gate test green for the github group | Given a daemon Components build; When the registry lists github_* tools; Then they arrive via the github provider (same names, same categories) |

## Self-Verification Checklist

- [ ] All tasks implemented and gate tests passing
- [ ] Contract C1 satisfied exactly (signatures, types, paths)
- [ ] No import of builtin/githubsvc from toolprov (verify with
      `go list -deps`; toolprov deps = tools + secrets + stdlib)
- [ ] githubsvc.ToolFactories path untouched and still green
- [ ] All files at exact specified paths; no deviations
- [ ] No scope creep; gofmt clean; ASCII-only

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [last 20 lines]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; gate tests pass, gate tests untouched
- [ ] C1 signatures match master.md exactly
- [ ] Import direction verified (go list -deps)
- [ ] No debug artifacts; no line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

- Keep RegisterAll construction lazy: Factory() returns the thunk; only
  RegisterAll (and config mapping in 04) constructs. This keeps disabled
  providers from ever running init()-side work beyond map insertion.
- If a provider's Discover() returns a tool whose name collides with an
  existing registry entry, surface the registry error verbatim; do not
  rename on the provider's behalf.
