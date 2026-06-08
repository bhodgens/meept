# Cluster A: Hashline Editing and Recovery Enhancements

## Goal
Achieve parity with oh-my-pi's hashline implementation, including block-level operations, improved stale-anchor recovery, and content-hash anchoring.

## Background
Meept's `file_edit` tool already uses `LINE:HASH` anchors, but the implementation is simpler than oh-my-pi's. oh-my-pi supports:
- `replace block N:` / `delete block N` via tree-sitter block resolution
- `insert head:` / `insert tail:` (already in Meept as BOF/EOF)
- Snapshot store with four-hex tags (`#0A3B`)
- Multi-section input (already supported)
- More robust recovery with `fuzzFactor` and merge-based strategies

## Feature Checklist

### 1. Snapshot-Based Hashline Tags
- Replace or supplement `LINE:HASH` with `¶PATH#TAG` where `TAG` is a per-session snapshot ID (4 hex chars)
- Snapshots are minted per `read`/`search` call and stored in `ReadCache`
- Benefits: Tags don't drift with edits (snapshot captures exact state at read time)

### 2. Block Operations via Tree-Sitter
- `replace block N:` — Replace entire syntactic block starting at line N
  - Resolve block span from tree-sitter parse tree
  - Supported: Go functions, Python blocks, JS/TS constructs, etc.
  - Fallback to `replace N..M:` on unresolvable blocks
- `delete block N` — Delete entire syntactic block
  - Same resolution logic, no body rows required

### 3. Enhanced Stale-Anchor Recovery
- oh-my-pi uses three recovery strategies:
  - **External cache recovery** — Compare against cached snapshot, try merge
  - **Session chain recovery** — Walk back through session edit history
  - **Session replay recovery** — Replay all edits on original snapshot
- Our current recovery is simple: ±5 line fuzzy search with exact content match
- We should implement at minimum **external cache recovery** with 3-way merge

### 4. Grammar-Constrained Parsing
- Define formal grammar for hashline patch language
- Use constrained decoding grammar (like oh-my-pi's Lark grammar)
- Validate at parse time rather than apply time

## Implementation Plan

### Phase 1: Snapshot Tag System
1. Add `snapshotTag` field to `file_read` output format
2. Update `ReadCache` to store `{path} -> {lines, snapshotTag}`
3. Update `file_edit` parameters to accept `tag` alongside `anchor`
4. Backward compatibility: Support both old `LINE:HASH` and new `¶PATH#TAG` formats

### Phase 2: Tree-Sitter Block Resolution
1. Reuse existing `internal/code/ast` package for parsing
2. Add `FindBlockSpan(path, lang, lineNum)` to query block boundaries
3. Add `SyntacticEdit` op type variant in `editOp`
4. Resolve at apply time; if unresolvable, error with suggested explicit range

### Phase 3: Recovery Enhancement
1. Implement external cache merge: compare cached vs current lines
2. Build diff between cached and current
3. If user edits applied cleanly to current, accept; if conflict, report mismatch
4. Add `fuzzFactor` config (0 = exact, >0 = fuzzy tolerance)

### Phase 4: Constrained Decoding Grammar
1. Define formal EBNF grammar for patch language
2. Validate tool input against grammar before processing
3. Reject malformed patches with line-specific error messages

## Files to Modify / Create
- `internal/tools/builtin/hashline.go` — Snapshot tags, recovery
- `internal/tools/builtin/file_edit.go` — Block operations, grammar validation
- `internal/tools/builtin/file_edit_test.go` — Comprehensive block/recovery tests
- `internal/code/ast/query.go` — Block span resolution
- `internal/tools/builtin/hashline_parser.go` (new) — Grammar parser

## Success Criteria
- [x] `replace block 5:` works for Go function blocks
- [x] Stale anchor with cached file changes is recovered 90%+ of the time
- [x] Snapshot tags prevent corruption from interleaved edits
- [x] All existing tests pass; new tests cover block ops and recovery
