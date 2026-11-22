# 06 - Unpinned Follow-up Tests (03-context T4 + F-NEW-14)

Residual-fix wave for plan 20261119-usability-remediation. The acceptance
trail flagged two follow-ups whose data/GUI implementation had landed but no
test pinned the contract. Both now have tests; both are GREEN; zero new
failures. **Nothing committed** (tests only, per lane).

## Files created (tests only - no implementation changes)

| File | Pins |
|------|------|
| `internal/session/resume_contract_test.go` | 03-context T4 resume contract (session store) |
| `ui/flutter_ui/test/features/chat/liveness_widget_test.dart` | F-NEW-14 GUI turn-liveness (widget/provider seam) |

No Go implementation files touched; no Dart lib files touched. The lane
constraint (internal/agent + internal/tui owned by a sibling) was respected -
nothing outside internal/session tests + ui/flutter_ui tests was modified.

## 1. 03-context T4 - Resume Contract

Leaf: `03-context/01-session-history.md` T4 row. The data layer (detection
context persistence via `SetDetectionContext`, `EnsureSessionRow`) landed in
leaf 01, but no test pinned the resume contract.

`TestResumeContract_DetectionContextAloneSurvivesRestart` - given a daemon
restart (SQLite store closed and reopened on the same db file), a session
whose ONLY working-dir binding is its persisted detection_context resolves
that CWD via `session.ResolveWorkingDir` with source
`WorkingDirFromDetection`. Follows the existing
`working_dir_persistence_test.go` restart-simulation precedent.

`TestResumeContract_WorktreeBeatsDetectionContextAfterRestart` - the
repo-wide precedence (worktree > project > detection CWD) holds across a
restart: a session with BOTH bindings keeps resolving the worktree.

`TestResumeContract_UnboundSessionResolvesNothing` - the negative branch: a
session with no worktree/project/detection CWD resolves ("", none) after a
restart, which is the path callers fail with the `tools.ErrNoWorkingDir`
sentinel (never the daemon's own process CWD). The sentinel identity is
imported in this _test.go file only (package session has no internal/tools
import in non-test code; verified no import cycle).

Note: the leaf doc's T4 wording also mentions "stale non-terminal tasks
carry interrupted marker (boot id), resume API returns last session +
pending state". That daemon/agent-side boot-id work belongs to the 01-safety
watchdog lane (sibling-owned files); this test pins the SESSION-STORE half
of the contract, which is what landed in this leaf. Flagged to the
orchestrator in case the boot-id marker wants its own pin in a later wave.

## 2. F-NEW-14 - GUI Liveness Widget Test

Contract source: `docs/plans/20261119-usability-remediation-findings/REPORT-GUI-RERUN.md`
F-NEW-14 + recommendation 13 ("GUI chat completion must be observable on the
GUI's own transport ... add a GUI-path liveness test"); the 05-navigation/02
acceptance report flagged "liveness widget test not added". The daemon-side
re-arm landed in 01-safety/04 (watchdog exemption); 05-navigation/02 landed
the GUI correlation fields and the provider re-arm path (`noteTurnProgress`
fed from the agent_progress subscription, scopes-2 fix).

`liveness_widget_test.dart` (fake_async, provider-level widget test) pins:

1. **Stall is honest**: a submitted turn with NO progress traffic goes
   `PendingTurnStatus.stalled` after `kTurnLivenessTimeout` (120 s) with the
   "no progress for ... task may still complete" text - and is NOT
   terminal: a late `turn.terminal` still renders the reply bubble and
   clears the pending state (F-NEW-14's core requirement: the completion is
   observable on the GUI's own transport, however late).
2. **Re-arm works**: progress frames re-arm the timer via
   `noteTurnProgress` - a turn that keeps reporting progress for 700 s
   (5.8x the window) never false-stalls; once progress stops, the re-armed
   timer fires and the turn stalls again (honest degradation, not a lie in
   either direction).

No liveness test existed before (checked test/providers/ and
test/features/chat/ - only the leaf-05 flow test touches the stall state);
this is a new file, nothing extended.

## Verification evidence

```
go test ./internal/session/ -run 'TestResumeContract' -count=1
  -> ok  github.com/caimlas/meept/internal/session  0.413s  (3/3 PASS)

go test ./internal/session/ -count=1
  -> ok  github.com/caimlas/meept/internal/session  2.158s  (full package GREEN)

cd ui/flutter_ui && flutter test test/features/chat/liveness_widget_test.dart
  -> 00:00 +2: All tests passed!  (2/2)

cd ui/flutter_ui && flutter test   (full suite, 838 files)
  -> +835 -3: Some tests failed.
     Failing: chat_submit_flow_test.dart (parked quota text),
              restore_test.dart x2 (GUI restore policy alignment)
     Baseline run started BEFORE any of my files existed returned the
     IDENTICAL 3 failures -> pre-existing, zero NEW failures.
```

ASCII: both new files byte-clean (no chars >= 0x7F). `gofmt -l` clean;
`dart format` reports 0 changed. No commit made; both files are untracked
(`git status --porcelain` shows only `??` for the two test files).

## Deviations / flags for the orchestrator

- T4's "interrupted marker (boot id)" half is daemon/agent-side and outside
  this lane; only the session-store resume contract is pinned here (see
  note in section 1).
- The 3 pre-existing Flutter failures (parked quota text, restore policy x2)
  are NOT new and NOT mine - confirmed by the pre-change baseline run.
