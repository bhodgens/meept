# Working Tier Store: Conversation-Buffer Memory - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing a
> file, do NOT read it back - write once and stop. Report what you built,
> files touched, and any deviations.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** New `internal/memory/working` package: token-bounded
  conversation-buffer working-tier store with eviction-to-episodic flush
  and concurrent-safe recall (contract C4).
- **Dependencies:** 00-research-tiers.md (cite findings-memory-tiers.md)
- **Estimated Context:** 85K
- **Concurrency Group:** B
- **Audit references:** R5

## Goal

Build the WORKING tier: a bounded, in-memory ring of recent conversation
turns that (a) accepts appends from a session hook, (b) evicts its own
oldest content when over budget, (c) answers Recall(query, limit) for the
C1 seam, and (d) Flushes evicted/overflow turns into the episodic store so
nothing is silently dropped (the demotion/eviction path, not deletion).
This is the true working tier the brief demands: the conversation buffer
integrated WITH memory. All opt-in: it does nothing unless wired, and its
config (leaf 02 owns config plumbing for policy; this leaf owns the store
Config struct only) defaults inert.

## Context

- internal/memory/episodic.go - existing FTS5 episodic store; Flush's
  EpisodicSink is satisfied by an adapter over it (write the adapter HERE
  if a turn-append method is missing; reuse existing APIs first).
- internal/memory/types.go:13-29 - MemoryType constants; turns are NOT
  Memory records - Turn is a distinct struct (contract C4); do not force
  turns into MemoryType.
- internal/llm/tokenizer.go - TokenCounter implementations; tests use a
  deterministic stub (chars/4), NOT the real tokenizer.
- internal/memory/manager.go - manager surface; do not couple Store to it.
- findings-memory-tiers.md - R-Bx citations for ring sizing/eviction.
- internal/session/ store - prior art for turn shapes (reference only).

## Verified Anchors

- internal/memory/episodic.go - FTS5 episodic store - Flush target
- internal/memory/types.go:13-29 - MemoryType constants - tier taxonomy
- internal/config/schema.go:1593-1597 - l0_enabled opt-in prior art - opt-in pattern
- internal/memory/consolidation.go:60,96 - single-start Consolidator - do not construct another

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/memory/working/store.go  (contract C4, verbatim from parent)
package working

type Config struct {
    MaxTokens   int
    MaxItems    int
    TTL         time.Duration
}
type Turn struct { ID, SessionID, Role, Content string; At time.Time }
type TokenCounter interface{ CountTokens(string) int }
type EpisodicSink interface{ AppendTurns(ctx context.Context, turns []Turn) error }

func New(cfg Config, tk TokenCounter) *Store
func (s *Store) Append(ctx context.Context, turn Turn) error
func (s *Store) Recall(ctx context.Context, query string, limit int) []string
func (s *Store) Snapshot() []Turn
func (s *Store) Flush(ctx context.Context, dst EpisodicSink) error

// internal/memory/working/episodic_adapter.go
func NewEpisodicSink(store *memory.EpisodicStore /* existing type; adapt name by grep */) EpisodicSink
```

Rules pinned by the parent: Append MUST NOT do I/O (memory + eviction
only; Flush is the only I/O); Recall MUST be concurrency-safe (RWMutex)
and MUST NOT run consolidation; token accounting via TokenCounter
deterministically; eviction order: expired-by-TTL first, then oldest
(approximate LRU by At); evicted turns are queued in-memory for Flush
(Flush drains the queue; idempotent when queue empty).

### What This Leaf Consumes

- C1 TierBudget/RecallFunc (../../master.md) - Recall must be usable as a
  RecallFunc (signature-compatible; no adapter needed in-package).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Types + New with validation: MaxTokens>0, MaxItems>0, TTL>=0; New returns (*Store, error) | Create: internal/memory/working/store.go; Test: internal/memory/working/store_test.go | `go test -p 2 ./internal/memory/working/ -run TestNew` | package absent | rejects zero/negative; accepts valid | Given Config{MaxTokens:0}; When New; Then error mentions MaxTokens |
| T2 | Append with token-budgeted eviction: over-budget appends evict TTL-expired then oldest; evicted queued for Flush; queue bounded (drop-oldest with counter if sink never flushes) | Modify: store.go; Test: store_test.go | `go test -p 2 ./internal/memory/working/ -run TestAppendEviction` | no eviction logic | store token load <= MaxTokens after every Append; queue preserves eviction order | Given MaxTokens=100, stub counter, append 10x30-token turns; When inspected; Then live set <= 100 tokens, queue holds evicted in order |
| T3 | Recall: term-overlap scoring over live turns, top-limit, concurrency-safe; empty query returns newest `limit` | Modify: store.go; Test: store_test.go | `go test -p 2 ./internal/memory/working/ -run TestRecall` | Recall absent | relevant turn ranked first; concurrent Append+Recall race-clean under -race | Given turns mentioning "postgres" and query "postgres migration"; When Recall; Then postgres turn first; `go test -p 2 -race` green |
| T4 | Flush + EpisodicSink adapter: drains queue into sink in eviction order; idempotent on empty queue; adapter over existing episodic store append path (grep for its turn/message append API; if none exists, adapter implements AppendTurns by storing each turn via the episodic store's existing Add path with MemoryTypeEpisodic) | Create: episodic_adapter.go; Modify: store.go; Test: flush_test.go | `go test -p 2 ./internal/memory/working/ -run TestFlush` | Flush absent | queue drained in order; second Flush = 0 turns; adapter compiles against real episodic store | Given 3 queued turns and a recording sink; When Flush; Then sink got 3 in order; repeat Flush sends 0 |
| T5 | Snapshot read-only copy + session scoping: Snapshot returns a copy (mutation-safe), Turns carry SessionID; Recall/Snapshot accept no session filter (whole-store) but Append records SessionID for the adapter | Modify: store.go; Test: store_test.go | `go test -p 2 ./internal/memory/working/ -run TestSnapshot` | Snapshot absent | mutating the returned slice does not affect store | Given Snapshot result mutated; When store queried; Then store unchanged |
| T6 | Race + determinism gate: full package under -race; deterministic stub counter documented; no wall-clock calls outside Turn.At fields (TTL uses injected clock func for tests) | Modify: store.go; Test: store_test.go | `go test -p 2 -race ./internal/memory/working/` | - | all green under -race; clock injectable (New accepts optional clock or Store.SetClock with typed-nil guard) | Given injected fake clock; When TTL expiry tested; Then deterministic eviction without sleeps |

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing (`go test -p 2 -race ./internal/memory/working/`)
- [ ] Contract C4 satisfied exactly (names, signatures, paths)
- [ ] Append does no I/O; Flush is the only I/O
- [ ] No import of internal/agent; no second Consolidator
- [ ] ASCII only; gofmt clean; no predictable IDs (pkg/id or provided Turn.ID)
- [ ] No deviations (or deviations documented in the report)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [last 20 lines of go test output]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; tests passing incl. -race
- [ ] Contract C4 matches exactly; Append no-I/O rule holds
- [ ] Queue bounded; Flush idempotent; Snapshot copy-safe
- [ ] No debug artifacts, no scope creep, no line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

Cite findings-memory-tiers.md R-Bx IDs for ring sizing and eviction order
choices in comments. The chat-path integration hook (who calls Append on
each turn) is branch A leaf 03 + daemon wiring - NOT this leaf; this leaf
ships the store + adapter + tests only. Keep Store free of config package
imports (Config is a plain struct; config plumbing lands in leaf 02).
