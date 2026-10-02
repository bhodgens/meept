# Frontend E2E Coverage - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.

## Meta

- **Role:** Root
- **Parent:** none (root)
- **Children:** 4 leaves
- **Scope:** Close the two frontend e2e gaps: extend `tui-flows` with the
  untested operator flows, create a real-daemon `gui-flows` e2e tier for the
  Flutter GUI, and record the coverage policy so the Flutter surface stops
  being invisible.

## Goal

The hermetic e2e tier (51 suites, all green) covers the daemon exhaustively
but barely touches the frontends. `tui-flows` drives the real TUI through
only 5 flows; the Flutter GUI has zero e2e - 63 Dart widget tests all run
against mocks, so the bug class that actually shipped this month (dropped
terminal events, pendingTurns rebuild wiping tracking, ack-race delivery)
is invisible to CI on the GUI side and only partially covered on the TUI
side. This tree: (1) extends `tui-flows` with chat steer/interrupt,
plans-view, and tasks-view flows; (2) builds a Flutter e2e suite whose
widget tests run against the hermetic harness daemon's REAL HTTP/SSE
surface instead of mocks; (3) amends the manifest coverage policy comment
to name Flutter as an explicitly tracked surface.

## Architecture

Three seams, no new daemon code:

1. **TUI flows** reuse the existing `tuiDriver` (headless bubbletea v2
   Program + `tui.NewApp` + `harness.Stack`) - new flows are new test
   functions in the existing suite, same mechanics.
2. **Flutter flows** are Dart widget tests (`flutter test`) under
   `ui/flutter_ui/test/e2e/` that pump the REAL providers
   (`ChatProvider` etc.) wired to a lightweight in-process HTTP/SSE stub
   server speaking the daemon's wire protocol (the same envelopes the
   harness fake-LLM serves). This is "real client code, stub server" -
   the inverse of today's "real widgets, mock providers". Dart cannot
   import the Go harness; the stub re-implements only the few endpoints
   the flows need, with shapes pinned by contract comments referencing
   the Go handlers.
3. **Manifest policy** is a comment amendment + two path_map additions
   (`ui/flutter_ui/` -> gui-flows as a Dart-side tracked surface noted in
   the policy comment; the Go-side e2e hook only scans Go packages, which
   the comment must state).

## Interface Contracts

### Contract 1: Daemon chat wire shapes (stub server must match)

```
POST /api/v1/chat/submit            -> {"turn_id": "...", "session_id": "..."}
GET  /api/v1/chat/stream?turn_id=   -> SSE: data: {"type":"agent_progress"|"chat_message", ...}
POST /api/v1/sessions               -> {"id": "...", ...}
GET  /api/v1/sessions               -> {"sessions": [...]}
Owner: internal/comm/http handlers (existing, do not modify)
Consumer: 02-gui-flows-infrastructure.md, 03-gui-flows-tests.md
```

### Contract 2: TUI flow assertions surface

```
Driver:    e2e/suites/tui-flows/driver_test.go (tuiDriver, existing)
Assertion: FINISHED app model fields + harness.Tasks()/Steps() store reads
New flows MUST NOT modify driver_test.go except additive tuiOption
constructors if a flow needs one.
Owner: 01-tui-flows-extension.md
```

### Contract 3: Manifest coverage policy keys

```
e2e/manifest.json "comment" field gains: Flutter surface tracking sentence.
path_map gains NO new Go keys (Dart files are not scanned by the Go
pre-commit hook); instead the comment documents that ui/flutter_ui/** maps
to the Dart-side suite via `flutter test test/e2e`.
Owner: 04-manifest-policy.md
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-tui-flows-extension.md | leaf | none | ~70K | A |
| 02 | 02-gui-flows-infrastructure.md | leaf | none | ~85K | A |
| 03 | 03-gui-flows-tests.md | leaf | 02 | ~75K | B |
| 04 | 04-manifest-policy.md | leaf | 01, 03 (scenario ids) | ~20K | C |

## Dispatch Protocol

### Phase 1: Dispatch Concurrency Group A

Dispatch 01 and 02 simultaneously. Include in each dispatch: the full leaf
text, the contracts above, and this conventions block:

- Go 1.26, gofmt clean, table-driven tests where natural, `//go:build e2e`
  tag on all e2e files, no new daemon code, explicit-path commits only
  (orchestrator commits, leaf does NOT commit).
- Dart: flutter_test idioms (`tester.pumpWidget`, `tester.runAsync` for
  real async I/O), no live network - stub server binds 127.0.0.1 ephemeral.
- Both: file edits are append/additive; never rewrite existing test files.

### Phase 2: Dispatch Group B (after 02 returns and reviews APPROVED)

Dispatch 03 with 02's delivered stub-server API surface inlined.

### Phase 3: Dispatch Group C (after 01 and 03 return)

Dispatch 04 with the final scenario IDs from 01 and 03.

### Review and Commit

Per child: orchestrator reviews in-session against the leaf's Review
Checklist, runs the leaf's verification command, then commits the leaf's
exact paths: `git commit -m "test(e2e): <leaf scope>" -- <paths>`.
Max 3 re-dispatch cycles per child; escalate gaps to the user after that.

### Integration Review

After all four children reach REVIEWED:

1. `go test -count=1 -tags e2e ./e2e/suites/tui-flows` - green.
2. `cd ui/flutter_ui && flutter test test/e2e` - green.
3. `python3 -c "import json; json.load(open('e2e/manifest.json'))"` - valid.
4. `make e2e-fast` - full tier still green (regression guard).
5. gofmt -l on all changed .go files - empty.

## Review Checklist

- [ ] Leaf tasks all implemented; no scope creep beyond the leaf
- [ ] Contract 1 shapes: stub server responses match the Go handlers'
      field names exactly (compare against internal/comm/http handler code)
- [ ] All new tests pass with `-count=1` (no cached ok)
- [ ] No daemon/production code modified by any leaf
- [ ] No wall-clock sleeps without an accompanying bounded poll
- [ ] Manifest stays valid JSON; scenario IDs unique
- [ ] No line-number corruption; gofmt clean on changed Go files

## Coding Conventions

- Go 1.26; package names lowercase; `//go:build e2e` tag; errors wrapped
  with %w; no bare panic
- Tests: stdlib `testing` + harness helpers (no testify in this repo)
- Dart: flutter_lints; `tester.runAsync` required around real socket I/O
- Formatting: gofmt (Go), `dart format` (Dart) before reporting

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-tui-flows-extension | COMPLETE | 2 | plans/tasks flows landed (ea5e8d17); steer exposed 3 production bugs, fixed (4f92b6ca, 61726448); flow SKIPs on the last seam (headless EventStream delivery) with a self-healing gate |
| 02-gui-flows-infrastructure | COMPLETE | 1 | 42035372; scheme injection seam + HttpOverrides note; wire shapes verified against Go handlers |
| 03-gui-flows-tests | COMPLETE | 1 | 4 GUI flows green; 2 production findings filed (addStreamMessage replace-by-id; loadMessages:415 pendingTurns wipe) |
| 04-manifest-policy | COMPLETE | 1 | 7ac003b3; gui-flows Dart-side entry + policy sentence |

Integration gates: E2E_EXIT=0 (52 ok), FLUTTER_EXIT=0 (14 tests), UNIT_EXIT=0
(tui + agent). Commits: 7bfcd074 (plan), ea5e8d17, 4f92b6ca, 42035372,
61726448, 7ac003b3 + session-switch determinism fix (see 61726448 follow-up
commit list).

Status values: PENDING | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

Commands in order (all from repo root unless noted):

1. `go test -count=1 -tags e2e ./e2e/suites/tui-flows` -> ok
2. `cd ui/flutter_ui && flutter test test/e2e` -> All tests passed
3. `python3 -c "import json; json.load(open('e2e/manifest.json'))"` -> no output
4. `make e2e-fast` -> exit 0 (full tier regression guard)

## Structural Completeness Check

All five required orchestrator sections present above (Dispatch Protocol,
Interface Contracts, Review Checklist, Coding Conventions, Completion
Tracking Table, Integration Test Plan).

## Notes

- Shared-tree discipline: sibling sessions may hold edits; commit ONLY by
  explicit path; re-check `git log` before each commit.
- The Flutter leaves deliberately choose widget tests + stub server over
  `flutter integration_test` device runs: integration_test requires a
  device/emulator in CI which this repo does not have; widget tests run in
  the standard `flutter test` gate. This is a documented scope decision.
- The 25-minute child cap: leaf 02 is the biggest; if it times out, audit
  surviving files with git status and re-dispatch only the missing half.
