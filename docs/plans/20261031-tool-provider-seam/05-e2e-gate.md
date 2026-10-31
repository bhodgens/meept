# Hermetic E2E Gate: tool-provider-seam suite - Implementation Leaf

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
- **Scope:** New hermetic e2e suite `e2e/suites/tool-provider-seam/`
  proving the integrated tree live: a test provider's tools pass the
  identical gates as native tools, disabled providers vanish from the
  live registry, and audit-chain entries land for provider tool calls.
- **Dependencies:** 01, 02, 03 (all REVIEWED); 04 for the disable case
- **Estimated Context:** 70K
- **Concurrency Group:** C
- **Audit references:** GitHub issue #78; R1/R3/R5 in master.md

## Goal

Close the wiring-claim gap: hermetic unit gates cannot prove daemon
components.go wiring (proven class: gates wired only onto the template loop
look complete in units). This suite boots a scratch daemon and asserts
through the live RPC surface. It is the live-gate evidence cited when the
tree's leaves mark COMPLETE.

## Context

- e2e/suites/ - existing suites to mirror (read one recent suite, e.g.
  activity-views, for harness patterns: WithPreBootHook seeding, scratch
  MEEPT_HOME, port remapping, Stack.HealthOK polling).
- e2e/manifest.json - MUST gain the path_map entry for any new package dir
  AND the suites[] entry IN THE SAME COMMIT (fail-closed pre-commit hook).
- Harness: e2e/harness - builds meept-daemon/meept into $TMPDIR scratch;
  detached watchdog reaps scratch daemons; never spawn raw exec.Command.
- Scratch-boot facts: daemon reads config from $MEEPT_HOME/meept.json5 at
  the HOME ROOT (not $MEEPT_HOME/config/); a models-less host needs
  orchestrator.classifier_boot_fail_fast=false and a remapped
  transport.http.addr (template 8081 collides with live daemons).
- The suite is hermetic: local-only, no cloud, no live models.

## Verified Anchors

- e2e/manifest.json - suites[] + path_map; add entries here.
- e2e/harness/daemon.go - scratch-daemon build + watchdog (use the
  harness, never raw exec).
- docs/plans/20261030-agent-activity-audit-views/06-e2e-suite.md - the
  most recent proven suite-leaf pattern in this repo (seeding via
  WithPreBootHook, assertions via live RPC).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
e2e/suites/tool-provider-seam/
  main_test.go        // suite entry, build tag `e2e`
  scenarios.go        // the three scenarios below
manifest.json delta  // path_map: internal/toolprov + suites[] entry
```

Scenario contract: each scenario boots its own scratch daemon with its own
scratch MEEPT_HOME and remapped ports; asserts via RPC only; leaves no
process or dir behind (watchdog verified by suite teardown output).

### What This Leaf Consumes

- C1 (01): the test provider registered under a name like "e2etest" from a
  tiny provider in the e2e suite's own test package (init-registered into
  toolprov factories) - proving third-party-style registration works.
- C2 (02): the gate - every tool call through the daemon passes G1-G4.
- C3 (04): config keys for enable/disable.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Suite skeleton + enabled-provider scenario | Create: e2e/suites/tool-provider-seam/main_test.go, scenarios.go; Modify: e2e/manifest.json | `make e2e-fast-area AREA=tool-provider-seam` | FAIL: suite missing or provider tool absent from live registry | Scratch daemon boots with e2etest provider enabled; a tool call to the provider's echo tool via RPC succeeds AND an audit-chain entry exists for it | Given a scratch daemon with provider enabled; When the RPC executes the provider tool; Then the call passes the gate and the audit chain contains the execution record |
| T2 | Security-gate parity scenario | Modify: scenarios.go | same | FAIL: provider tool skips the gate | A provider tool whose args fail the G1 scan is blocked with the same typed error shape a native tool gets; audit records the block | Given the e2etest provider exposes a tool triggering the scanner; When Execute runs through the daemon; Then the block reason + error shape match the native-tool blocked case byte-for-shape |
| T3 | Disabled-provider scenario | Modify: scenarios.go | same | FAIL: disabled provider still listed | Config disabling e2etest => RPC tool listing omits its tools; boot log names the skip | Given a scratch daemon with e2etest disabled; When the RPC lists tools; Then no e2etest tool appears |

## Self-Verification Checklist

- [ ] All scenarios green: `make e2e-fast-area AREA=tool-provider-seam`
- [ ] manifest.json entries added in the same change (suite + path_map)
- [ ] Suite runs green twice back-to-back (no fixed sleeps; bounded polls
      on readiness endpoints only)
- [ ] No scratch daemons leak: `ps -axo pid=,ppid=,command=` shows no
      ppid=1 meept-daemon under the scratch bin dir after the run
- [ ] gofmt clean; ASCII-only

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [paths]
2. **Verify command + output:** [last 20 lines of the suite run]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Suite proves LIVE wiring (RPC surface), not unit seams
- [ ] Audit-chain assertion reads the real chain store on disk
- [ ] Disable case proves absence, not presence-of-error
- [ ] Manifest complete; no fixed sleeps; watchdog verified
- [ ] Gate tests untouched

Output: APPROVED or specific gaps with file + line references.

## Notes

- If the e2etest provider cannot live in the e2e suite's test package (Go
  init registration into toolprov factories needs the package linked into
  the daemon binary), the fallback is a build-tagged tiny provider package
  under internal/toolprov/e2etestprov linked only by the e2e build - record
  the choice in the report. Keep it out of production binaries either way.
- Suite timeout budget: keep total under ~3 minutes; two scratch boots max
  (enabled + disabled scenarios can share one boot only if config toggling
  is not required - prefer two boots for isolation; budget allows).
