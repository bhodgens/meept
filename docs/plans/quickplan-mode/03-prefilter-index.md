# Leaf 03 — Prefilter Index: quickplan examples + cue guard

## DISPATCH INSTRUCTION

> **Implementing agent:** implement ALL tasks below via TDD. Do NOT
> commit. Do NOT run git add. Write code, run tests, report results only.
> Explore with search_files or `terminal cat` — never feed read_file
> output into write_file.

- **Parent:** docs/plans/quickplan-mode/master.md
- **Scope:** orchestration-cue regex (shared), prefilter cue guard for
  quickplan predictions, quickplan examples in the centroid builder
- **Dependencies:** 01-intent-type.md (constant); Contract 3 regex is
  pinned in master.md — implement it verbatim
- **Estimated context:** ~50K

## Interface Contract (exposed)

```go
// internal/agent/quickplan_cue.go
package agent

var QuickPlanCuePattern = regexp.MustCompile(`(?i)\b(subagents?|tasks? \d|` +
    `task list|waves?|leaves?|leaf \d|plan\.md|handoff|checklist|in order|` +
    `one at a time|sealed plan|tracking table|as you (find|go)|, then\b|` +
    `and correct them|and fix them|without (asking|stopping)|no check-?ins?|` +
    `just (do|make|apply)|make it happen|to completion|finish the remaining|` +
    `carry on with the plan|execute (the|what)|implement (the|all) plan|` +
    `implement tasks?|work (through|items)|knock out|carry out|` +
    `complete the outstanding)\b`)

// Consumers: prefilter vote(), LLM-chain post-check (leaf 02 optional).
```

## Tasks

### Task 1: Cue pattern file

File: `internal/agent/quickplan_cue.go`

The pattern exactly as in the contract (package-level compiled var, one
line-wrapped expression). Add a doc comment explaining WHY the guard
exists: quickplan-vs-code/git is not decidable from message text alone
(adjudicated campaign finding, iter-20); the cue requires orchestration
evidence before a quickplan direct route fires. Deriving from session
state happens at the orchestrator (leaf 02), not here.

### Task 2: Prefilter cue guard

File: `internal/agent/embedding_prefilter.go`, in `vote()`:

A quickplan unanimous vote requires cue support. Implementation shape:

```go
first := p.examples[neighbors[0].idx].Intent
if first == "quickplan" && !QuickPlanCuePattern.MatchString(p.lastInput) {
    return kNNVote{}, false // quickplan route without orchestration
                            // evidence: fall through to the chain
}
```

`p.lastInput` does not exist — `Match()` (line ~306) receives the input
string; pass it through to vote (change `vote(vec)` →
`vote(vec, input string)` at this call site only; it is unexported, grep
for callers — one call site in Match + tests).

Rationale comment required at the guard:

```go
// QuickPlan cue guard: quickplan-vs-code/git is not decidable from
// message text (session-state signal — adjudication record 2026-09-09).
// A quickplan vote without orchestration cues falls through to the LLM
// chain, which has conversation context.
```

### Task 3: Centroid builder gains quickplan examples

File: `scripts/build_prefilter_centroids.py` — no code change needed
(classes are data), but the QUICKPLAN EXAMPLES must live in the tracked
corpus (already done: iters 19-20 added 44 quickplan cases to
testdata/eval/classifier-adversarial-corpus.json5). Verify by running:

```bash
python3 scripts/build_prefilter_centroids.py \
  --corpus testdata/eval/classifier-adversarial-corpus.json5 --sweep
```

Expected: quickplan appears in the per-intent sweep output. Do NOT write
the centroid store to ~/.meept (that is a deployment action, not eval).

### Task 4: Tests

File: `internal/agent/quickplan_cue_test.go`

1. Cue-positive table: "using subagents, fix them", "implement tasks 3
   and 4", "commit, push, then run tests", "as you find them",
   "no check-ins needed", "finish the remaining waves" → match.
2. Cue-negative table: "review the json files for completeness",
   "compare the top 3 databases", "why is the test failing?", "add
   pagination to the API" → NO match.
3. Prefilter guard test (embedding_prefilter_quickplan_test.go): build a
   prefilter with quickplan examples (all-identical vectors like the
   existing fakes); query WITH cue → vote ok=true, intent=quickplan;
   same query WITHOUT cue → vote ok=false. Use the existing
   `knnPrefilter`/`exampleSet` test helpers.

### Task 5: Regression

Existing prefilter tests must pass unchanged (`go test
./internal/agent/ -run TestPrefilter -count=1`). If any test's fake
examples use intent "quickplan", they now need the cue — grep first;
none should today (the intent is new).

## Self-Verification Checklist

- [ ] `go build ./internal/agent/...` passes
- [ ] `go test ./internal/agent/ -run 'QuickPlan|Prefilter' -count=1` passes
- [ ] `go test ./internal/agent/ -short -count=1` passes
- [ ] Sweep script shows quickplan class (task 3 command)
- [ ] gofmt clean; regex is a single compiled package var
- [ ] No debug prints; guard comment present

## Review Checklist (orchestrator)

- [ ] Regex verbatim per Contract 3 (diff against master.md)
- [ ] Guard in vote() uses input passed from Match (signature change
      scoped to vote + its call sites)
- [ ] Cue-positive/negative tests match the adjudication record rules
- [ ] No deployment action taken (no centroid store write)
- [ ] Existing prefilter tests untouched and green
