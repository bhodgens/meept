# Sequence Comparator Package - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on
> existing source files - explore with search_files or terminal cat.
> After writing a file, do NOT read it back - write once and stop.
> Report what you built, files touched, and any deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** internal/eval/trajectorydiff: normalizer + graded sequence
  comparator with exact / in_order / any_order modes and threshold.
- **Dependencies:** R0-research.md (digest; C2 verdict expected KEEP -
  proceed on the frozen C2 regardless; orchestrator amends only BEFORE
  dispatch)
- **Estimated Context:** 60K
- **Concurrency Group:** B
- **Audit references:** master C1, C2, C3

## Goal

Give the repo its first graded sequence comparator: take a golden list of
normalized tool calls and an actual list, normalize both (tool name +
argument shape, variable scrubbing, sorted-key canonical JSON), score
them under a configurable match mode with a pass threshold, and return
diagnostic step diffs. This is pure library code - no wiring, no suite,
no CLI (those are sibling leaves).

## Context

- internal/session/store.go:29 - ToolCall struct
  {ID, MessageID, ToolName, ToolCallID, Arguments, Result, Seq} -
  the recorded form of a tool call the normalizer consumes.
- internal/session/store_sqlite.go:206-219 - session_tool_calls schema:
  (tool_name, tool_call_id, arguments, result, seq, UNIQUE(message_id,
  seq)) - sequences are ORDER BY seq.
- internal/eval/record.go - eval.Step {Name, Err, Failed} - the shape
  StepsFromGolden maps into for judge reuse.
- internal/eval/judge.go:152 - Judge() - existing outcome grading; the
  comparator complements it, never replaces it.
- Pattern reference: internal/eval/judge.go contentLooksFailed honesty -
  same spirit of deterministic, honest grading.

## Verified Anchors

- [internal/session/store.go:29-37] - ToolCall struct - normalizer input
- [internal/session/store_sqlite.go:206-219] - session_tool_calls
  schema - source of ordered recorded calls
- [internal/session/store_sqlite.go:2271] - GetToolCalls, ORDER BY seq
- [internal/eval/record.go] - Step struct - StepsFromGolden output shape
- [internal/eval/judge.go:152] - Judge() - sibling grading surface

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
package trajectorydiff // internal/eval/trajectorydiff

type NormalizedCall struct { ... }      // C1 - full definition in master
type MatchMode string                   // C2 - "exact" | "in_order" | "any_order"
type ArgRule string                     // C2 - "exact" | "subset" | "ignore"
type Options struct { ... }             // C2 - full definition in master
type StepDiff struct { ... }            // C2
type Result struct { ... }              // C2
func Compare(golden, actual []NormalizedCall, opts Options) Result
func Normalize(name string, argumentsJSON string) (NormalizedCall, error)
func NormalizeSessionCalls(calls []session.ToolCall) ([]NormalizedCall, error)
func StepsFromGolden(calls []NormalizedCall) []eval.Step
```
Signatures, constants, and scoring semantics are FROZEN in master C1-C3.
If R0's verdict requires an amendment, the orchestrator supplies it in
your dispatch context as a dated addenda - it overrides the master text.

### What This Leaf Consumes

- session.ToolCall (internal/session/store.go:29) - read-only input type
- eval.Step (internal/eval/record.go) - output shape for StepsFromGolden

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Normalize: parse arguments JSON into ArgsShape with sorted-key canonical form; scrub variable values (keys matching id|uuid|timestamp|time|path|token|session case-insensitive -> "<var>", recursive); error on invalid JSON | Create: internal/eval/trajectorydiff/trajectorydiff.go; Test: internal/eval/trajectorydiff/trajectorydiff_test.go | `go test -p 2 ./internal/eval/trajectorydiff/` | package does not exist | all normalize tests pass | Given arguments {"path":"/tmp/x1","op":"read"}; When Normalize; Then ArgsShape == {"path":"<var>","op":"read"} and key order is sorted in canonical JSON |
| T2 | NormalizeSessionCalls: map []session.ToolCall -> []NormalizedCall preserving seq order; StepsFromGolden: map []NormalizedCall -> []eval.Step (Failed=false, Err="") | same files as T1 | same as T1 | types undefined | mapping tests pass | Given 3 ToolCalls with seq 2,0,1; When NormalizeSessionCalls on seq-ordered input; Then output order matches seq order |
| T3 | Compare EXACT: same length + position-wise equality -> Score 1.0/Passed; any difference -> failed with FirstDiff set | same files as T1 | same as T1 | Compare undefined | exact-mode tests pass | Given golden [A,B] vs actual [A,C]; When Compare exact; Then Passed=false, FirstDiff=2 |
| T4 | Compare IN_ORDER + ANY_ORDER with threshold and epsilon: in_order Score = |LCS|/len(golden) (classic DP subsequence); any_order Score = matched multiset/len(golden); Passed = Score >= Threshold with 1e-9 epsilon; extra actual steps never fail in_order/any_order; extra golden steps do | same files as T1 | same as T1 | Compare undefined | tolerance tests pass incl. threshold boundary (Score == Threshold passes) | Given golden [A,B,C] vs actual [X,A,B,C,Y]; When Compare in_order threshold 1.0; Then Passed=true Score=1.0; Given same golden vs [A,B]; Then Score~=0.667 Passed=false |
| T5 | Options plumbing: ArgRules per tool (exact/subset/ignore), IgnoreCallSet exclusion, Mode default in_order, Threshold default 1.0; Summary one-liner; StepDiff diagnostic list in stable order | same files as T1 | `go test -p 2 ./internal/eval/trajectorydiff/ && go vet ./internal/eval/trajectorydiff/` | options unhandled | options tests pass; go vet clean | Given ArgRules{"web_search":"ignore"}; When Normalize+Compare; Then the search_path argument never affects the verdict; Given IgnoreCallSet{"memory_write"}; When Compare; Then such calls are excluded from both sides |

**Worked example (canonical row expansion):**

T3:
1. Write failing test:

```go
func TestCompareExactModeMismatchFails(t *testing.T) {
    g := []NormalizedCall{mustNorm(t, "read_file", `{"op":"read"}`)}
    a := []NormalizedCall{mustNorm(t, "write_file", `{"op":"read"}`)}
    r := Compare(g, a, Options{Mode: MatchExact})
    if r.Passed || r.FirstDiff != 1 {
        t.Fatalf("got %+v, want failed with FirstDiff=1", r)
    }
}
```

2. Verify failure: `go test -p 2 ./internal/eval/trajectorydiff/`
   Expected: FAIL - Compare undefined.
3. Minimal implementation: Compare with exact branch (length check then
   position-wise NormalizedCall equality on canonical ArgsShape JSON).
4. Verify pass: same command, PASS.

Tests-first gate branch: n/a - leaf-internal TDD.

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts satisfied exactly (C1-C3 signatures)
- [ ] All files at exact specified paths (package only; no wiring)
- [ ] Float threshold uses epsilon; canonical JSON sorts keys (Pitfall 26)
- [ ] No deviations (or deviations documented in the report)
- [ ] No scope creep - no CLI, no suite, no fixture files
- [ ] gofmt clean; `go vet` clean

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [verify commands; last 20 lines of output]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; tests passing
- [ ] Interface contracts match exactly (C1-C3: names, types, JSON tags)
- [ ] Scoring semantics match C2 frozen definitions (LCS in_order,
      multiset any_order, epsilon threshold)
- [ ] Normalizer variable-key scrubbing matches C3 rule exactly
- [ ] Conventions followed; no bugs; no predictable IDs; no ignored
      errors; no import cycle (imports internal/eval + internal/session
      types only)
- [ ] No scope creep; no debug artifacts or line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

- LCS is the classic O(n*m) DP over NormalizedCall equality; equality
  compares Name plus canonical ArgsShape JSON (deterministic bytes).
- any_order matching consumes actual entries (multiset semantics): two
  golden A calls need two actual A calls.
- Keep the package dependency-light: stdlib + internal/eval +
  internal/session types. It will later run inside the hermetic e2e tier.
- ASCII only, including test fixtures and error strings.
