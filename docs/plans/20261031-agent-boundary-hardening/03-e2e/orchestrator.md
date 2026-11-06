# Shared Boundary-Hardening E2E Suite - Branch Orchestrator

> **For the executing agent:** You are the orchestrator for this branch.
> Your job: (1) dispatch the leaf agent, (2) review in-session,
> (3) re-dispatch if incomplete, (4) track completion. Do NOT implement
> code yourself.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 1 leaf (01-boundary-hardening-suite.md)
- **Scope:** Hermetic e2e suite proving both security lanes end to end:
  collaboration flows with taint lane on, forged control messages
  neutralized, PII roundtrip with session-scoped restore, secret
  placeholder restore; plus manifest integration.

## Goal

Branches 01 (taint) and 02 (pii) land mechanisms + wiring with unit
coverage. This branch adds the required hermetic e2e tier
(e2e/suites/boundary-hardening/, `//go:build e2e`, manifest entries) so
the pre-commit-e2e hook and `make e2e-fast` gate both features. Per repo
policy (root AGENTS.md): all new FEATURE tests live here; the unit tests
in the branches exist only for pure mechanisms inside packages that
already carry unit suites.

## Architecture

One suite, four scenarios, driven through the e2e harness
(e2e/harness - builds meept-daemon/meept into $TMPDIR scratch bins with
the reaper watchdog; never raw exec without it). Scenarios map 1:1 to
master contract C4 keys boundary-hardening-01..04. The suite runs the
daemon with `[security.taint] enabled=true` and `[security.pii]
enabled=true` scratch configs.

## Interface Contracts

### C1-C: Scenario manifest keys

```
scenarios (e2e/manifest.json), suite "boundary-hardening":
- boundary-hardening-01 (diff M): legitimate collaboration round
  (pair driver shape) completes with the taint lane ON; the handoff
  prompt's driver framing reaches the worker; raw output marked.
- boundary-hardening-02 (diff M): a forged control message
  (approve/request_token JSON + agent-impersonation preamble) injected
  into a handoff is neutralized: turn finalizes failed/blocked, no
  workspace_yield-style instruction executes.
- boundary-hardening-03 (diff M): chat turn containing email+phone+card:
  provider-bound context carries only [PII:*] tokens (assert at the
  recorded-request/trace layer), reply + stored history + captured trace
  contain the original values (restored, session-scoped).
- boundary-hardening-04 (diff S): response containing #XXXX# secret
  placeholders returns the original secret to the user; other sessions/
  fresh daemons never see it.
// Owner: 01-boundary-hardening-suite.md; Consumers: manifest, CI gates
```

### C2-C: Manifest integration

```
e2e/manifest.json gains:
- suites += {name: "boundary-hardening", dir: "e2e/suites/boundary-hardening",
  status: "implemented"}
- path_map entries: "internal/security/pii/" and
  "internal/agent/taint_hooks.go" -> boundary-hardening (plus the
  three collaboration driver files if not already mapped)
- scenarios += the four above (paths[] pointing at the touched sources)
// Owner: 01-boundary-hardening-suite.md
```

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-boundary-hardening-suite.md | leaf | branches 01+02 REVIEWED | 90K | A |

## Dispatch Protocol

For each concurrency group, in dependency order.

### Phase 0: Prerequisite check

Confirm in ../master.md tracking tables: all branch 01 + 02 leaves
REVIEWED/COMPLETE. If any is BLOCKED, mark this leaf BLOCKED and
escalate upward - do not dispatch against half-landed wiring.

### Phase 1: Dispatch 01-boundary-hardening-suite.md (group A)

Context = full leaf text + contracts C1-C/C2-C + the harness entry points
(e2e/harness package docs) + "Do NOT commit. Do NOT git add. The scratch
bin watchdog/reaper is mandatory - never raw os.MkdirTemp/exec without
the harness."

### Phase 2: Review and commit

In-session review: leaf spec + contracts; run
`go test -p 2 -tags e2e ./e2e/suites/boundary-hardening/` and
`make e2e-fast-area AREA=boundary-hardening`; verify manifest keys exist
and `scripts/e2e-affected.sh` maps the new source paths to this suite;
grep for debug artifacts. Pass: commit (suite + manifest together),
REVIEWED, then COMPLETE. Gaps: re-dispatch (max 3), escalate to
../master.md.

### Phase 3: Branch integration review

`make e2e-fast` full sweep - no regression in existing suites; the new
suite green in CI shape (hermetic, no network). Report COMPLETE to
../master.md.

## Review Checklist

The branch orchestrator (main model) verifies in-session:

- [ ] All four scenarios implemented and GREEN via the harness
- [ ] Manifest: suite + path_map + scenarios present and well-formed
- [ ] Suite is hermetic (no external network; scratch configs only)
- [ ] Scratch-bin hygiene: harness-managed bins only, reaper active
- [ ] `go vet` clean on the suite; `//go:build e2e` tag present
- [ ] No debug artifacts; no line-number corruption; no commits by the leaf

Output: APPROVED or specific gaps.

## Coding Conventions

- Go test files under e2e/suites/boundary-hardening/, `//go:build e2e`,
  package name matching dir; harness imports
  github.com/caimlas/meept/e2e/harness.
- Assertions on observable surfaces: task/step rows, recorded requests,
  chat replies, trace rows - not internal function calls.
- Manifest JSON stays formatted like the existing file (2-space indent,
  same key order per entry).
- ASCII only; no heredocs; leaf never git commit/add; `go test -p 2`.

## Requirement Traceability

| Req | Source | Plan element | Verified by |
|-----|--------|--------------|-------------|
| RC1 | "e2e proving a forged control message is neutralized and legitimate collaboration still flows" | C1-C scenarios 01/02 | suite GREEN |
| RC2 | "tests in hermetic e2e tier + manifest entry" | C2-C | manifest check |
| RC3 | "PII roundtrip + secret restore" | C1-C scenarios 03/04 | suite GREEN |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-boundary-hardening-suite.md | COMPLETE | 1 | landed 1781716d; 4/4 scenarios PASS (issue 84) |

Status values: PENDING | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE |
BLOCKED

## Integration Test Plan

1. `go test -p 2 -tags e2e ./e2e/suites/boundary-hardening/ -v` - four
   scenarios green.
2. `make e2e-fast-area AREA=boundary-hardening` - green through the Make
   target (CI shape).
3. `make e2e-fast` - full hermetic sweep green (no cross-suite damage).
4. Manifest sanity: `python3 -c "import json;json.load(open('e2e/manifest.json'))"`
   parses; new keys present.

## Test Gate

Tests-first gate: N/A - this branch IS the integration test leaf; its own
TDD table is the gate (tests are the deliverable, RED before branch wiring
cannot be observed since wiring already landed - the RED state applies to
scenario logic against a deliberately disabled-config variant, see leaf).

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as
dated addenda.

## Notes

- Verified anchors: suites live in e2e/suites/<name>/ with main_test.go
  shape (see e2e/suites/output-filters/output_filters_test.go for the
  harness idiom); manifest at e2e/manifest.json (keys: version,
  generated, comment, path_map dict, suites list, scenarios list).
- The taint scenarios need a collaboration driver path reachable in the
  hermetic rig; if scripted-planner support for pair-driver sessions does
  not exist, drive the driver directly through the registry seam inside
  the e2e process (harness-permitted) rather than inventing daemon HTTP
  surface - state the chosen approach in the report.
- Keep scenarios independent: any one failing must not mask the others.
