# Branch 01 - Output Caps + Tool-Layer Spill - Orchestrator

> **For the executing agent:** You are the orchestrator for this branch.
> Run the Test Gate leaf to RED first, commit gate tests, then dispatch
> implementation leaves. Review in-session. Implementation agents never commit.

## Meta

- **Parent:** ../master.md
- **Gap:** GAP-1 (docs/research/token-efficiency-2026-10/pi-evidence.md). Pi parity: tail-keep + spill pointer.
- **Contracts:** C1 (ShellResult.FullOutputPath), C2 (spill path/TTL/cap).

## Child Index

| # | Child | Type | Depends on | Est. context | Concurrency |
|---|-------|------|-----------|--------------|-------------|
| 00T | 00T-gate.md | test leaf | none | 45K | solo |
| 01 | 01-shell-tail-spill.md | leaf | 00T VALIDATED | 85K | solo |
| 02 | 02-spill-sweep.md | leaf | none (touches internal/llm/spill.go + daemon wiring) | 70K | parallel with 01 |

Leaf 01 touches internal/tools/builtin/shell.go (+spill call into internal/llm).
Leaf 02 touches internal/llm/spill.go + internal/daemon wiring. Disjoint files:
safe in parallel. Neither touches the same function; C2 pins the shared path
convention so they meet without coordination.

## Dispatch Protocol

1. Dispatch 00T: "Write tests ONLY, no implementation. RED for
   missing-implementation reasons. Do NOT commit."
2. Validate gate against Test Gate matrix; commit gate tests; mark TESTS_VALIDATED.
3. Dispatch 01 and 02 in one batch. Context per dispatch: leaf path + contracts
   C1/C2 + "Do NOT commit / git add. go test -p 2."
4. Review in-session: run verify commands, check C1 JSON tag
   `full_output_path,omitempty`, tail-not-head behavior, spill path matches C2,
   sweep bounds (24h/6h/512MB) implemented as pure funcs + daemon wiring.
5. Commit each leaf separately with explicit paths; then run branch integration
   (../master.md Integration Test Plan items 1, 3).

## Interface Contracts

C1 and C2 are defined in ../master.md and are FROZEN for this branch.
Additionally:
- Spill write is best-effort: a spill failure MUST NOT fail the shell tool
  (log warn, omit FullOutputPath). Truncation still applies.
- Spilled content = combined captured output (stdout+stderr as the tool
  already interleaves), raw bytes, no re-encoding.
- Reuse `internal/llm.SpillService`; do not create a second spill writer.
  The builtin package may import internal/llm (check import cycle; if one
  exists, expose a narrow `SpillWriter` interface in builtin and wire the
  llm.SpillService implementation from the daemon - clean fix, no workaround).

## Test Gate

| Feature | Gate leaf | Test target | Status |
|---------|-----------|-------------|--------|
| tail-keep truncation + full_output_path | 00T-gate.md | internal/tools/builtin/shell_test.go (bug-fix class, existing file) | PENDING |
| sweep pure funcs (age/size bounds) | 00T-gate.md | internal/llm/spill_test.go (existing file if present, else new test in bug-fix-adjacent spill area; if a brand-new non-test file would be needed, put sweep logic in spill.go and test in spill_test.go) | PENDING |

## Coding Conventions

Inherited from ../master.md Coding Conventions. Branch-specific:
- Truncation must cut on a line boundary (never split a UTF-8 rune - see
  scripts/audit-utf8-byte-arithmetic.py; use byte-safe last-newline search).
- No behavior change when output is under the cap (byte-identical result).

## Completion Tracking Table

| Child | Status | Commit | Notes |
|-------|--------|--------|-------|
| 00T-gate | TESTS_VALIDATED | 83571ede | T1/T2 RED behaviorally; T3 green; T4 deferred to leaf 02 |
| 01-shell-tail-spill | COMPLETE | f71bbe8e | tail-keep both sites; C1 note; FullOutputPath; best-effort spill; SetSpillWriter nil-guard pinned. components.go wiring swept into HEAD by sibling hitl commit 71312c96 - this leaf's spill_wiring.go repairs the resulting broken clean-checkout build |
| 02-spill-sweep | COMPLETE | f71bbe8e | SweepToolSpills pure fn (age+size passes, 1h in-flight guard, deterministic) + DefaultToolSpillRoot + daemon startup/6h ticker wiring in spill_wiring.go |

## Integration Test Plan

1. `go test -p 2 ./internal/tools/builtin/ ./internal/llm/` green.
2. Manual: run a shell command producing >50KB via the CLI chat path; confirm
   the model-visible result ends with the TAIL and names the spill file;
   confirm the file exists and contains the full output.
3. `make lint-ci` green (utf8 audit included).

## Review Checklist

- [ ] Truncation keeps tail; header note matches C1 format
- [ ] full_output_path JSON tag exact; omitted when not truncated
- [ ] Spill best-effort (failure -> no path, no tool error)
- [ ] Sweep: 24h startup + 6h ticker + 512MB cap, oldest-first; pkg/id for filenames
- [ ] Under-cap outputs byte-identical to pre-change (regression test)
