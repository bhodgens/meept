# Result Floor and Deterministic Map Compression - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on
> existing source files — explore with search_files or terminal cat.
> After writing a file, do NOT read it back to verify — write once and
> stop. After completing, report what you built, what files you touched,
> and any deviations from the spec.

## Meta

- **Parent:** master.md
- **Scope:** (a) ResultSizer optional interface + GetMaxResultTokens
  helper; (b) agent-loop per-result budget floor consultation;
  (c) compressMapResult rewrite: deterministic order + always-whole
  metadata keys; (d) transcript_fetch declares a 1400-token floor.
- **Dependencies:** none
- **Estimated Context:** 40K
- **Concurrency Group:** A (alone). You own internal/tools/interface.go,
  internal/agent/loop.go, internal/agent/executor.go,
  internal/tools/builtin/transcript_fetch.go(+test).

## Goal

The dynamic tool-result budget (loop.go:4027-4035, decaying
3000→600 tokens) silently clips paginated pages (the model then skips
the clipped tail forever), makes results order-dependent
(non-reproducible), and — because compressMapResult iterates Go maps
in randomized key order and truncates wholesale — randomly drops
metadata keys like the `path` pointing at the full transcript file.
This leaf makes the floor tool-declarable and map compression
deterministic and metadata-preserving.

## Context

- Optional-interface precedent: Categorizer (interface.go:167) +
  GetCategory helper. Registry optional-capability assert precedent:
  loop.go:1043.
- Consumption site: loop.go ~4027 (dynamicToolBudget computed once),
  ~4058 (compression pipeline call, has toolName), ~4070
  (result.ToCompressedJSON(dynamicToolBudget), the SAME loop iterating
  results with toolName available from response.ToolCalls[i]).
  l.registry exists (loop.go:1001-1005); guard nil.
- compressMapResult: executor.go:527; ONE caller (executor.go:494).
  Keep the (map[string]any, int) → map[string]any signature.
- truncateWithMarker / compressCodeResult / looksLikeCode: reuse as-is.
- transcript_fetch constants: TranscriptMaxOutputLength etc. at
  transcript_fetch.go:29-43.
- The 3 chars/token heuristic is repo-wide (conversation.go:669).

Key files:
- internal/tools/interface.go
- internal/agent/loop.go
- internal/agent/executor.go
- internal/agent/executor_test.go (compressMapResult tests — extend;
  grep for existing coverage first)
- internal/tools/builtin/transcript_fetch.go(+test)

## Interface Contracts (From Parent)

```
// internal/tools/interface.go (new, after Categorizer):
// ResultSizer is an optional interface tools implement to declare a
// minimum token budget for their results. The agent loop never
// compresses a declared tool's result below this floor regardless of
// the dynamic budget; declared floors are capped at
// ToolResultMaxTokens (tools cannot exceed the global ceiling).
type ResultSizer interface {
    MaxResultTokens() int
}
func GetMaxResultTokens(t Tool) int // 0 when unimplemented or <= 0;
                                    // capped at ToolResultMaxTokens
                                    // (import cycle check: cap const
                                    // lives in tools pkg — verify;
                                    // if ToolResultMaxTokens is in
                                    // agent, cap in the CALLER, not
                                    // here. GetMaxResultTokens then
                                    // returns the raw declared value
                                    // and the loop caps.)

// internal/agent/loop.go — at the consumption site, per result i:
//   budget := dynamicToolBudget
//   if l.registry != nil && toolName != "" {
//       if tool := l.registry.Get(toolName); tool != nil {
//           if floor := tools.GetMaxResultTokens(tool); floor > budget {
//               budget = min(floor, ToolResultMaxTokens)
//           }
//       }
//   }
//   ... compression-pipeline call uses budget; ToCompressedJSON(budget)
// The pipeline call and ToCompressedJSON MUST agree (one budget var).

// internal/agent/executor.go — compressMapResult rewrite:
//   primary := first present string value among "content","output",
//   "result"; else the longest string value (tie -> lexicographically
//   first key). No string values -> primary = "" (copy-all path).
//   Keys sorted ascending; primary processed last.
//   Non-primary keys: copied WHOLE, counted against maxChars. If
//   non-primary keys alone exceed maxChars: keep them all anyway,
//   set _truncated=true (metadata integrity outranks budget).
//   Primary: truncates with existing marker mechanics when remaining
//   budget < len; sets _truncated=true on clip. Fits -> copied whole.
//   _truncated=true exactly when anything (primary or metadata
//   overflow) was clipped relative to the input.

// internal/tools/builtin/transcript_fetch.go:
//   const TranscriptResultTokens = 1400
//   func (t *TranscriptFetchTool) MaxResultTokens() int { return TranscriptResultTokens }
//   (+ interface assertion var _ tools.ResultSizer)
```

## Tasks

### Task 1: ResultSizer + helper

**Files:** internal/tools/interface.go; internal/tools/interface_test.go
(create/extend).

**Step 1: Failing tests** — GetMaxResultTokens: nil-safety not needed
(takes non-nil Tool; passing a tool without the method → 0; negative →
0; a stub with 1400 → 1400). Use small local stub types in the test.

**Step 2:** FAIL (compile). **Step 3:** implement. **Step 4:** PASS.

### Task 2: transcript_fetch declares the floor

**Files:** internal/tools/builtin/transcript_fetch.go(+test).

**Step 1: Failing test** — TranscriptResultTokens == 1400;
MaxResultTokens() == 1400; tool satisfies tools.ResultSizer.
**Step 2:** FAIL. **Step 3:** implement. **Step 4:** PASS.

### Task 3: compressMapResult determinism + metadata preservation

**Files:** internal/agent/executor.go; internal/agent/executor_test.go.

**Step 1: Failing tests** (table + property):
- Determinism: map with 8 keys (mixed sizes), maxChars generous → run
  100×, byte-identical JSON every time (this FAILS today — Go random
  order; when generous-budget copying preserves input order it may
  accidentally pass, so ALSO assert sorted-key order explicitly on the
  compressed output's key sequence).
- Primary selection: keys content/output/result present → primary is
  "content"; absent → longest string wins; tie → lexically first.
- Metadata integrity: content 50k + path/offset/total_chars at
  maxChars=2000 → path/offset/total_chars FULL, content truncated,
  _truncated=true.
- Metadata overflow: 30 non-primary keys totalling > maxChars → ALL
  present whole + _truncated=true.
- No-string map / empty map → copy-all, no _truncated.
- Primary fits: no _truncated, content whole.

**Step 2:** FAIL. **Step 3:** implement per contract. **Step 4:** PASS.

### Task 4: loop floor consultation

**Files:** internal/agent/loop.go; internal/agent/loop_test.go or a new
loop_budget_test.go (grep for the existing tool-result test harness
first — reuse its fake registry/tool pattern).

**Step 1: Failing tests**
- Floor honored: registry with a ResultSizer tool (floor 1400);
  simulate budget-decay state (totalTokens ≈ convBudget so
  dynamicToolBudget would be 600); assert the produced conversation
  tool-result text contains the full 4200-char content (not clipped)
  and that a 5000-char content IS clipped to the 1400-token cap
  (never above ToolResultMaxTokens).
- Undeclared tool same position: clipped at ~600-token floor (today's
  behavior preserved).
- Unknown tool name + nil registry: no panic, today's behavior.

If constructing the full loop in tests is heavy, extract the per-result
budget computation into a small pure helper
`effectiveResultBudget(dynamic int, tool tools.Tool) int` in loop.go and
table-test THAT plus wire it at the single call site — the loop-level
test then only needs to assert the helper is called (or rely on the
compression path test). Choose the seam that keeps the diff minimal;
state the choice in the report.

**Step 2:** FAIL. **Step 3:** implement. **Step 4:** PASS.

### Task 5: gofmt + suites

gofmt -l on touched files. `go build ./internal/... && go vet
./internal/agent/ ./internal/tools/...` clean. `go test
./internal/tools/... ./internal/agent/ -count=1 -p 2` — pass
(pre-existing failures attributed separately; baseline them FIRST with
a scoped run before your changes and report any pre-existing reds).

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Contract match: interface shape, cap location, loop consultation,
      compressMapResult determinism + metadata rules
- [ ] Pipeline call and ToCompressedJSON use the SAME budget value
- [ ] transcript_fetch floor == 1400 via named constant
- [ ] No changes to ToolResultMaxTokens / window / turn budget
- [ ] Existing suites green; pre-existing reds baselined and reported
- [ ] gofmt clean; no line-number corruption; no debug artifacts

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- If loop.go's consumption loop shape makes per-result toolName
  unavailable at the ToCompressedJSON line, hoist the budget
  computation INTO the existing `for i, result := range results` loop
  (toolName is derived there today for the pipeline call — same loop,
  same variable). Keep it one budget var flowing to both consumers.
- GetMaxResultTokens cap placement: ToolResultMaxTokens lives in
  internal/agent — the tools package MUST NOT import agent (cycle).
  Cap in the loop (contract shows this). State it in a comment.
- The determinism test asserting sorted-key ORDER is the anti-vacuous
  guard: a generous-budget copy-all could pass a byte-identical check
  by luck; the order assertion cannot.
