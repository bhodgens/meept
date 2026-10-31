# Migrate Existing Capability Groups into Providers - Implementation Leaf

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
- **Scope:** Migrate the scheduler/cron, hermes-agent, and ACP-agent tool
  groups from direct components.go Register calls into toolprov providers
  (github lands in 01-seam.md T3 as the proof group).
- **Dependencies:** 01-seam.md, 02-gate.md (both REVIEWED)
- **Estimated Context:** 85K
- **Concurrency Group:** B
- **Audit references:** GitHub issue #78; R2 in master.md

## Goal

Route existing capability groups through the seam so registration is
provider-shaped and each group declares one PermissionClass ceiling. After
this leaf: every migrated group's tools arrive via toolprov.RegisterAll,
names and categories unchanged, and the daemon wiring in components.go is
one RegisterAll call per group.

## Context

- internal/daemon/components.go:4125-4132 - scheduler/cron group (seven
  consecutive Register calls - the cleanest first target).
- internal/daemon/components.go:6643-6738 - skill/file groups (do NOT
  migrate - too broad for this leaf; see Notes).
- internal/daemon/acp_register.go:66 - ACP agent tool registration.
- internal/tools/builtin/tool_cron_create.go, tool_schedule_create.go,
  hermes_agent.go, acp_agent.go - the tool constructors each provider wraps.
- Provider packages live NEXT TO their tools: scheduler provider in
  internal/tools/builtin (init-populated into toolprov factories). Keep the
  provider thin: Name + Discover (calls the existing constructors) +
  PermissionClass + Broker binding.
- IMPORT RULE preserved: providers register via builtin's init(); daemon
  calls toolprov.RegisterAll with enabled names from config.

## Verified Anchors

- internal/daemon/components.go:4125-4132 - `c.ToolRegistry.Register(builtin.NewScheduleCreateTool(...))`
  block - replaced by the scheduler provider walk.
- internal/daemon/acp_register.go:66 - `reg.Register(builtin.NewACPAgentTool(mgr))` -
  replaced by the acp provider walk.
- internal/toolprov/provider.go (from 01) - Register/RegisterAll.
- internal/tools/builtin/github_register.go - the init() registration
  pattern 01 established for the github provider.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// In internal/tools/builtin (init-populated):
//   provider "scheduler": tools = schedule_create/list/get/delete/pause/
//     resume/run_now + cron_create; PermissionClass = RiskMutating
//   provider "hermes": tools = hermes agent tool(s); RiskMutating
//   provider "acp": tools = acp agent tool; RiskPrivileged (subprocess spawn)
// Provider names are the config keys 04-config.md maps.
```

### What This Leaf Consumes

- C1 Provider/RegisterAll (01-seam.md) - exact signatures.
- C2 gate already wired by 02 - no changes to gate code here.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | scheduler/cron provider | Modify: internal/tools/builtin/github_register.go (or sibling *_register.go file - keep one registration file per concern; scheduler may get schedule_register.go); Modify: internal/daemon/components.go:4125-4132 | `go test -p 2 ./internal/daemon/ -run TestProviderWiring` | FAIL: scheduler tools not provider-routed | Scheduler+cron tools present via provider; names/categories unchanged; direct Register calls gone | Given a Components build; When the registry lists schedule_* and cron_create; Then each arrives via provider "scheduler" and no direct Register call remains at the old site |
| T2 | hermes provider | Modify: registration file; Modify: the components.go hermes registration site (locate with search_files: `NewHermesAgentTool`) | same | FAIL: hermes tool not provider-routed | Same shape as T1 | Given a Components build; When the registry lists the hermes tool; Then it arrives via provider "hermes" |
| T3 | acp provider | Modify: internal/daemon/acp_register.go; registration file | same | FAIL: acp tool not provider-routed | acp tool via provider "acp" with RiskPrivileged ceiling | Given a Components build with acp enabled; When the registry lists the acp tool; Then it arrives via provider "acp"; given acp disabled, the provider is absent and no subprocess path arms |

## Self-Verification Checklist

- [ ] Gate tests F7 passing; no gate-test modifications
- [ ] Tool names, categories, and behaviors unchanged (diff the registries:
      before/after lists identical)
- [ ] Each provider declares exactly one PermissionClass
- [ ] No direct Register calls remain at migrated sites
- [ ] gofmt clean; ASCII-only; no scope creep

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [paths]
2. **Verify command + output:** [last 20 lines]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Before/after registry tool lists identical for migrated groups
- [ ] Providers registered in builtin init(); daemon uses RegisterAll only
- [ ] acp provider gated on enabled (default-off posture preserved)
- [ ] No debug artifacts; line-number corruption check clean

Output: APPROVED or specific gaps with file + line references.

## Notes

- Scope boundary: skill/file/memory groups (components.go:6643-6738 and
  ~3100) stay direct-registered this tree. They are too broad; migrating
  them is a follow-up leaf set after this tree proves the pattern.
- The acp provider must keep `[acp] enabled` default-false semantics: the
  provider registers in the factory map at init, but its Discover returns
  empty (or RegisterAll skips it) when the manager is disabled - match the
  existing acp_register.go guard, do not change when subprocesses may arm.
