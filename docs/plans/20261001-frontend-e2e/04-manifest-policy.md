# Manifest Coverage Policy - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below. Do NOT commit - the orchestrator handles all
> git operations after review. After completing, report what you changed.

## Meta

- **Parent:** ../master.md
- **Scope:** Amend `e2e/manifest.json`'s coverage-policy comment to name
  the Flutter surface and its Dart-side suite; register the new TUI and
  GUI scenario ids.
- **Dependencies:** 01 (tui-steer-01, tui-plans-01, tui-tasks-01 ids),
  03 (gui-terminal-01, gui-session-01, gui-quota-01, gui-stream-01 ids)
- **Estimated Context:** ~20K
- **Concurrency Group:** C

## Goal

The manifest's policy comment enumerates deliberately-thin mappings but
never mentions the Flutter surface at all, which is why the GUI shipped
with zero e2e unnoticed. This leaf makes the coverage policy honest.

## Context

File: `e2e/manifest.json` (single JSON file; scenarios array holds the
inventory). The Go pre-commit hook only scans Go packages, so Flutter
coverage cannot be a path_map entry - it must be a documented policy
sentence plus the scenario registrations.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
manifest.json changes:
1. comment: one added sentence documenting that ui/flutter_ui/** is
   covered Dart-side by `cd ui/flutter_ui && flutter test test/e2e`
   (gui-terminal-01, gui-session-01, gui-quota-01, gui-stream-01) and
   that the Go pre-commit-e2e hook does NOT scan Dart files - the gate
   is the flutter test invocation, to be added to CI/lint-ci when the
   Flutter toolchain is present.
2. scenarios: 7 new entries appended near their siblings:
   tui-steer-01, tui-plans-01, tui-tasks-01 (suite: tui-flows)
   gui-terminal-01, gui-session-01, gui-quota-01, gui-stream-01
   (suite: tui-flows for the TUI trio; for the GUI quartet use
   suite "gui-flows" AND add a suites[] entry:
   {"name":"gui-flows","dir":"ui/flutter_ui/test/e2e","status":"implemented",
    "note":"Dart-side: run via flutter test, not go test"})
```

### What This Leaf Consumes

```
Final scenario names from leaves 01 and 03 (verify by reading the test
files; adjust ids if implementations deviated, keeping the prefix).
```

## Tasks

### Task 1: Register the 7 scenarios + gui-flows suite entry

Read the actual test files to confirm ids and paths (leaves may have
renamed); insert the scenario objects following the existing entry shape
(id, suite, diff, title, paths). For GUI scenarios use paths
`ui/flutter_ui/test/e2e/...` and `ui/flutter_ui/lib/providers/chat_provider.dart`.

### Task 2: Amend the policy comment

Append the Flutter sentence to the existing comment string. Keep it
valid JSON (the file is parsed by scripts and the hook). Validate:
`python3 -c "import json; json.load(open('e2e/manifest.json'))"`.

### Task 3: Verify hook tolerance

Run `bash -n .githooks/pre-commit-e2e` and, if quick, simulate the hook's
package-classification on an unrelated Go change to confirm the new
suite entry (non-Go dir) is ignored, not mis-parsed.

## Self-Verification Checklist

- [ ] Manifest valid JSON; 7 scenarios added; gui-flows suite entry added
- [ ] Policy comment documents the Dart-side gate honestly
- [ ] No existing scenario modified

**DO NOT COMMIT.**

**Deviations from spec:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Scenario ids match the real test function names
- [ ] The comment states the Go hook does not scan Dart (honesty clause)
- [ ] JSON valid

## Notes

Keep titles one line; follow the existing tone (behavior-specific, not
generic).
