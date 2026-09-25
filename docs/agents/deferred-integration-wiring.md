# Deferred Integration Wiring — When Parallel Leaves Leave Stubs

## The Pattern

A plan tree specifies two leaves dispatched in parallel:

- **Leaf A (runner/controller):** Creates the loop, handles flow. References "leaf B types" via stubs marked `// stub for now — leaf 02`.
- **Leaf B (LLM clients/types):** Implements the types Leaf A's stubs were stand-ins for.

Both leaves complete and pass review independently. Leaf A's tests use stubs. Leaf B's tests use mocks. Neither touches the other's files.

**Result:** Both leaves compile and test green. But Leaf A still calls stubs at runtime — it never imports or uses Leaf B's real types. The integration was deferred.

## Why This Happens

The plan author intended a post-review integration pass but:
1. The leaf dispatch instructions said "Do NOT modify files from other leaves" (correct for isolation).
2. The orchestrator reviewed each leaf against its own spec (correct for quality).
3. Neither the plan nor the orchestrator explicitly scheduled the wiring step.

## The Fix

After both leaves are REVIEWED and COMMITTED, the orchestrator must do an **integration wiring pass** — a small, targeted edit to the runner that:

1. Imports the LLM client types from leaf B.
2. Adds optional fields to the runner struct (with nil = stub fallback).
3. Adds builder methods (`WithAdversary()`, `WithTeacher()`) for optional injection.
4. Updates the CLI to accept model configuration flags and create LLM clients.
5. Verifies: `go build && go test -race`.

This is NOT a new leaf — it's 50-100 lines of wiring code the orchestrator writes directly after both leaves land.

## Recognition Signal

A plan that says "Leaf 01 — stub for now (leaf 02)" or "deferred to leaf 02" or "integration pass after leaves land" is the signal. When you see this language in a plan, budget 10-15 minutes for the wiring pass after both leaves are committed.

## Variant: sibling leaf names a deferral in its report — assign it, don't absorb silently

A leaf report may end with "wiring left to leaf 04/05" or "daemon wiring deferred to a later leaf." Treat that line as a dispatch decision, not a note:
- If the named consumer leaf exists in the tree and its tasks cover the wiring, fold the deferral context into THAT leaf's dispatch brief (leaf 05/06 dispatches must state what leaf 03 left unwired, so the gap closes inside the tree).
- If NO remaining leaf covers it, the orchestrator does the wiring pass itself before the integration gate — a feature that compiles but is never reachable from production code is not COMPLETE (repo wiring-requirement invariant).

Worked case: a loop-branch leaf left `SetGlobalRefusalModel` daemon wiring as "left to leaf 04/05"; neither leaf's file list covered the daemon package. The orchestrator added the 8-line boot wiring directly and verified the daemon package build+tests before committing the leaf.

## Example from rebellion-the-game

- Leaf 01 (adversarial/runner.go): `generateScenario()` was a hardcoded stub. `Run()` had `_ = trace` no-op for teacher evaluation.
- Leaf 02 (adversarial/adversary.go + evaluator.go): `AdversaryLLM` and `TeacherLLM` were implemented with full prompt templates and mock tests.
- Both reviewed, committed, 37 tests passing.
- **Missing:** Runner never wired to LLM clients. CLI had no model flags. Interactive REPL had no `ask`/`why` LLM support.
- **Wiring pass** (5 files, 168 lines): Added `WithAdversary()`/`WithTeacher()` builder methods, updated `Run()` to call real LLMs when configured, added `--adversary-endpoint`/`--teacher-port`/`--adversary-model` CLI flags, wired REPL handlers.
