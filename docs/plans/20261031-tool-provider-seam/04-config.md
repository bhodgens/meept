# Provider Config + Doctor Surface - Implementation Leaf

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
- **Scope:** C3: `[tool_providers]` config schema in meept.json5, the
  config-to-enabled-names mapping consumed by RegisterAll, and a
  report-only `meept doctor` provider section.
- **Dependencies:** 01-seam.md (REVIEWED)
- **Estimated Context:** 55K
- **Concurrency Group:** B
- **Audit references:** GitHub issue #78; R5 in master.md

## Goal

Operators enable/disable providers in config; `meept doctor` reports each
registered provider's state (enabled/disabled/unknown-name) without
mutating anything - matching the established doctor report-only posture.

## Context

- internal/config/schema.go - all config structs live here (JSON5 tags).
- config/meept.json5 - the shipped template; add a commented example block.
- meept doctor command (locate with search_files: `doctor` in cmd/meept) -
  report-only per the user-normative rule; never suggests installs without
  opt-in.
- internal/toolprov.Names() (01) - the registered-factory list doctor
  reads.
- Config template drift: annotate the new template block with the
  `// drift:` convention scripts/config-drift-check.py understands if the
  block is absent from installed homes by design (fresh templates copy on
  install; existing homes keep working via default-true).

## Verified Anchors

- internal/config/schema.go - struct home (grep for a similar small map
  config, e.g. MCP servers, to mirror tag style).
- config/meept.json5 - template block target.
- internal/toolprov/provider.go - Names()/RegisterAll signatures from 01.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/config/schema.go
type ToolProvidersConfig struct {
    Providers map[string]ToolProviderEntry `json:"providers" toml:"providers"`
}
type ToolProviderEntry struct {
    Enabled *bool `json:"enabled,omitempty" toml:"enabled,omitempty"`
}
// Semantics: absent map or absent entry = enabled (default-true: migrated
// native groups keep working on existing installs). Explicit false = off.
func (c ToolProvidersConfig) EnabledNames(all []string) []string
```

### What This Leaf Consumes

- C1 toolprov.Names() (01-seam.md) - doctor lists registered factories.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Schema + EnabledNames mapping | Modify: internal/config/schema.go | `go test -p 2 ./internal/config/ -run TestToolProviders` | FAIL: type missing | Absent map = all enabled; explicit false honored; unknown enabled=true names pass through (RegisterAll errors on them downstream) | Given configs with (a) no block, (b) providers.A.enabled=false, (c) enabled=true on unknown Z; When EnabledNames runs over [A,B,Z]; Then (a) yields [A,B,Z], (b) yields [B,Z], (c) yields [A,B,Z] |
| T2 | Daemon wiring consumes config | Modify: internal/daemon/components.go (RegisterAll call sites take config.EnabledNames(toolprov.Names())) | `go test -p 2 ./internal/daemon/ -run TestProviderConfigDisable` | FAIL: disable not honored | Disabled provider's tools absent from registry | Given config disabling "scheduler"; When Components builds; Then no schedule_* tool is in the registry and the boot log names the skipped provider at debug |
| T3 | Doctor report + template block | Modify: cmd/meept doctor section; Modify: config/meept.json5 | `go test -p 2 ./cmd/meept/ -run TestDoctorReportsProviders` (or the existing doctor test pattern) | FAIL: doctor silent on providers | Doctor lists each factory: name, risk class (via a constructed instance), enabled state; template gains a commented example | Given a built CLI config; When doctor runs; Then output includes one line per registered provider with state, and exits 0 |

## Self-Verification Checklist

- [ ] F8/F9 gate tests passing; no gate-test modifications
- [ ] Default-true preserves existing installs (no config -> no behavior change)
- [ ] Doctor report-only: no prompts, no installs, exit 0
- [ ] Template block commented; drift annotation where needed
- [ ] gofmt clean; ASCII-only; no scope creep

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [paths]
2. **Verify command + output:** [last 20 lines]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Absent-config = byte-identical behavior (F8 default case)
- [ ] Disabled provider's tools provably absent from a live registry (T2)
- [ ] Doctor lines carry provider name + state; report-only
- [ ] Template + drift annotation correct

Output: APPROVED or specific gaps with file + line references.

## Notes

- EnabledNames takes `all` as a parameter (not a toolprov call) so
  internal/config never imports internal/toolprov - cycle safety; the
  daemon joins the two.
- Unknown names in config are NOT a config error: they flow through and
  RegisterAll rejects them with the known-names list (01 T2). Doctor marks
  them unknown in its report.
