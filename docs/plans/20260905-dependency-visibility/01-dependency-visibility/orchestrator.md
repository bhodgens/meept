# orchestrator.md — 01-dependency-visibility branch

## Goal

Deliver Contracts A-D from the parent master: per-server install hints in
the catalog, doctor mcp-dependency checks, the canonical daemon PATH
(closing issue #32), and the menubar PATH fix. This branch closes #32.

## Architecture Overview

Four leaves, three of them parallel:

- 01-install-hint-field: `InstallHint string` on `mcp.ServerConfig`
  (internal/tools/mcp/manager.go), all catalog entries annotated, loader
  test. This is the spine — 02 consumes it.
- 02-doctor-mcp-checks: new doctor check block per Contract B.
- 03-daemonpath-go: `DaemonPath()` + the three Go consumers (launchd
  plist :329, kardianos svc.Config env, manager.go failure log). With
  leaf 04, closes #32.
- 04-menubar-path: Swift DaemonController environment. With leaf 03,
  closes #32.

01, 03, 04 are file-disjoint and dispatch in parallel. 02 follows 01.

## Interface Contracts

Contracts A-D are frozen in the parent master.md. Leaf dispatch contexts
must inline the relevant contract verbatim. The cross-leaf seam is
Contract A's field name (`InstallHint`) and Contract C's function name
(`daemon.DaemonPath() string`) — leaf 02 and leaf 03's manager-warning
both render from these; do not rename during implementation without
amending here AND in the parent.

## Child Index

| Doc | Scope | Est. context | Dependencies |
|-----|-------|--------------|--------------|
| 01-install-hint-field.md | field + catalog + test | ~35K | none |
| 02-doctor-mcp-checks.md | doctor check | ~40K | 01 |
| 03-daemonpath-go.md | DaemonPath + 3 consumers | ~50K | none |
| 04-menubar-path.md | Swift env | ~20K | none |

Wave 1: 01 + 03 + 04 parallel. Wave 2: 02.

## Dispatch Protocol

Per parent master.md. Extra: leaf 03's closing commit (made by this
orchestrator after both 03 and 04 review PASS) must end `Closes #32`.
Before closing the issue, verify each of the issue body's three failure
sites (launchd.go:329 hardcoded PATH; kardianos plist with no env;
menubar launchctl spawn) has a code-level fix in the diff — do not close
on partial coverage.

## Coding Conventions

Per parent master.md. Extra: `exec.LookPath` errors are expected-path
failures — wrap them as data (detail strings), not as errors that abort
doctor. In launchd.go keep the plist template a const string; substitute
PATH via the existing Sprintf, adding no new verbs.

## Completion Tracking Table

| Doc | Status | Notes |
|-----|--------|-------|
| 01-install-hint-field.md | PENDING | |
| 02-doctor-mcp-checks.md | PENDING | blocked by 01 |
| 03-daemonpath-go.md | PENDING | |
| 04-menubar-path.md | PENDING | |

## Review Checklist (branch)

- [ ] Contract A field name/json tag verbatim; every stdio catalog entry
      has install_hint; http entries don't
- [ ] Contract B output shape exact (mcp: prefix, both ok/fail forms)
- [ ] Contract C: DaemonPath() exists with doc comment; plist PATH uses
      it; kardianos env set (API verified via go doc); manager failure
      log includes effective PATH
- [ ] Contract D: Swift env merge compiles (orchestrator runs
      `swiftc -parse` or swift build on the menubar package)
- [ ] go vet clean on touched Go packages; existing doctor tests green
- [ ] No TODOs; strings lowercase; no line-number corruption

## Integration Test Plan

After all four REVIEWED: run the full doctor command against the real
catalog (`go run ./cmd/meept doctor`) and paste the mcp: block into the
branch tracking table notes. Verify `launchctl print` PATH after
`make install` if launchd service is active on this machine. Mark branch
COMPLETE in parent master.md only when both 03 and 04 are COMPLETE and
issue #32's close is queued in the final commit message.
