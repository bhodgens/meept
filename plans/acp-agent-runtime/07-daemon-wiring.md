# Daemon Wiring — Implementation Leaf

> **For the implementing agent:** Implement ALL tasks below using TDD.
> Do NOT commit. Do NOT use read_file on existing source files.

## Meta

- **Parent:** ../master.md
- **Scope:** internal/daemon/components.go construction + shutdown wiring for acp.Manager and its tool registration
- **Dependencies:** 04, 05
- **Estimated Context:** 55K
- **Concurrency Group:** D (file-disjoint from 05: owns internal/daemon/components.go hunks only)

## Goal

The rule from AGENTS.md: implementations MUST include wiring. This leaf makes ACP real: when `[acp] enabled=true`, the daemon builds the Manager, registers the acp_agent tool, and stops sessions on shutdown. When disabled (default), the construction path is provably inert — zero allocations, zero log lines, zero tools.

## Context

- internal/daemon/components.go — the giant constructor. KNOWN HAZARDS (meept-development skill): (1) never inline `acp.SetX(cfg...)` style global switches here — use an apply-helper + one call line, pattern: applyGBNFConstrainedFromConfig / SetSchemaModeConfig wiring; (2) `return fmt.Errorf(...)` inside this constructor is wrong when the enclosing signature is (*X, error) — check the enclosing function signature before ANY new return; (3) the file carries sibling-session WIP — your diff must contain ONLY ACP hunks.
- How MCP client manager wires (grep "MCPServerLister" / mcp manager construction) — the acp wiring mirrors it: construct-if-enabled, register tools, lister for prompt context, shutdown hook.
- Daemon lifecycle: Start/Stop in internal/daemon — where StopAll-style hooks live.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/daemon/components.go (exact shape per surrounding code)
// 1. applyACPFromConfig(cfg) -> (*acp.Manager, error)-or-nil helper (file-local),
//    mirroring the applyGBNFConstrainedFromConfig pattern:
//      cfg.ACP.Enabled false -> return nil, nil (NO log line about acp at all)
//      true -> NewManagerFromFiles(cfg.ACP); error -> wrapped, containing "acp"
// 2. One construction line in the components build path + nil-guard:
//      if acpMgr != nil { register acp_agent tool via the same registry path MCP uses;
//      attach StopAll to the daemon shutdown sequence }
// 3. Optional WithACPManager(...) on whatever struct holds cross-component deps,
//    with the repo's typed-nil guard rule (nil check before storing).
```

### What This Leaf Consumes

From 04: acp.NewManagerFromFiles, Manager.StopAll, Manager.Enabled/Tools-bridge needs. From 05: the acp_agent tool constructor (NewACPAgentTool(getMgr func() *acp.Manager)).

## Tasks

### Task 1: Failing tests — construction behavior

**Files:** Create: internal/daemon/acp_wiring_test.go (do NOT add to any existing giant test file)

Harness note: the skill says tests never construct NewComponents. Instead: test the applyACPFromConfig helper directly (package-internal test): disabled config -> nil manager, nil error; enabled + missing catalog file -> error mentioning "acp"; enabled + valid temp catalog -> manager non-nil, Enabled() true. Save/restore any package globals the helper touches via t.Cleanup (repo test convention).

### Task 2: Failing tests — tool registration path

If tool registration happens through a testable seam (a registry passed into the helper or a small RegisterACPTools(reg, mgr) function — PREFER adding this small function in components.go or a new internal/daemon/acp_register.go file to keep the giant constructor untouched): test that RegisterACPTools adds exactly one tool named "acp_agent" when manager enabled, zero when disabled/nil. If the codebase pattern forces registration inline in components.go, test the smallest extracted function instead and document the constraint in your report.

### Task 3: Wiring implementation

**Files:** Modify: internal/daemon/components.go (minimal hunks) and/or Create: internal/daemon/acp_register.go (preferred home for RegisterACPTools)

Exact steps:
1. Helper applyACPFromConfig + RegisterACPTools
2. Two-to-four construction lines in the build path (helper call, nil-guard registration, shutdown hook append)
3. Shutdown: Manager.StopAll() wired to the existing daemon stop sequence (grep where other Stop/Close hooks run — MCP manager shutdown is the model)
4. NO system-prompt changes in this leaf (lister/text belongs to leaf 08's surface work)

### Task 4: Disabled-path proof test

**Files:** Extend: internal/daemon/acp_wiring_test.go

With the DEFAULT config (no [acp] section at all — zero-value Config struct from DefaultConfig): helper returns nil/nil; RegisterACPTools no-ops; a constructed-with-defaults components path shows no acp artifacts. This test is the executable proof of "disabled by default."

## Self-Verification Checklist

- [ ] go build ./internal/daemon/ green (also ./... targeted build if siblings permit — poll per skill if broken)
- [ ] go test ./internal/daemon/ -run TestACP -race -count=1 green
- [ ] git diff internal/daemon/components.go contains ONLY acp hunks (foreign hunks reported, not staged)
- [ ] Disabled path: default config produces zero acp log lines — asserted in test
- [ ] Shutdown hook verified (test calls the stop path, sessions closed)
- [ ] No TODOs

**DO NOT COMMIT.**

**Deviations from spec:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Construction is helper+few-lines pattern, NOT inline config logic in the constructor
- [ ] Enclosing-signature check done for any new return (repo hazard)
- [ ] Registration goes through one small function; giant constructor diff is minimal
- [ ] Typed-nil guards on any With* setter used
- [ ] Disabled-path test proves inertness
- [ ] make graphs output unchanged (no new bus topics from this leaf — if you added any bus publish/subscribe, STOP and report: scope violation)

## Notes

- The daemon is the wiring point of record: after this leaf, `meept doctor`-style status MAY mention acp only if a status surface exists — it does not yet (leaf 08). Do not invent status output here.
- If NewManagerFromFiles needs the catalog file path expanded (~), use the same path-expansion helper MCP config loading uses; do not hand-roll tilde expansion.
