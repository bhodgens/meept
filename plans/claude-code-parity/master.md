# Claude Code Parity — Forest Root

## Goal

Implement 10 improvements to meept's agent harness, derived from a gap analysis against Claude Code v2.1.88's engineering report. Each improvement targets a specific LLM failure mode that degrades build quality (over-engineering, false completion claims, unverified changes, cache waste, context drift).

## Architecture Overview

The improvements span 6 internal packages: `agent` (verification, anti-patterns, hooks), `llm` (prompt cache, compaction), `tools` (safety metadata, file_unchanged, command semantics), `memory` (staleness), `security` (bash injection, secret scanning), and `selfimprove` (inline feedback). One new agent definition (explore) is added to `config/agents/`.

No changes to the request flow (`CommServer → MessageBus → AgentLoop → Dispatcher → Planner → Tools → Response`). All improvements are additive — existing behavior is preserved when new features are disabled.

## Interface Contracts

See SHARED-CONVENTIONS.md §6 for all cross-tree contracts (VerificationConfig, PromptCacheBoundary, CacheScope, per-tool safety, daemon verification config).

## Child Index

| # | Tree | Type | Est. Context | Dependencies | Concurrency |
|---|------|------|-------------|--------------|-------------|
| 01 | adversarial-verification | branch (3 leaves) | 90K/leaf | none | Wave 1 |
| 02 | anti-pattern-prompts | leaf | 45K | none | Wave 1 |
| 03 | prompt-cache | branch (2 leaves) | 70K/leaf | none | Wave 1 |
| 04 | compaction-user-messages | leaf | 35K | none | Wave 1 |
| 05 | combined-improvements | branch (4 leaves) | 50-70K/leaf | 01 (tool safety interface) | Wave 2 |

## Dependency Graph

```
01-adversarial-verification ──┐
02-anti-pattern-prompts ──────┤
03-prompt-cache ──────────────┼── Wave 1 (all independent)
04-compaction-user-messages ──┘
                               │
05-combined-improvements ──────┘── Wave 2 (05-01 depends on 01's Tool interface extension)
```

Tree 05 leaf 01 (per-tool safety) extends `tools.Tool` interface. Tree 01 does NOT touch this interface, so the dependency is soft — 05-01 can proceed if it defines the interface extension itself. The dependency is noted for integration review, not blocking.

## Dispatch Protocol

1. Dispatch Wave 1 trees (01, 02, 03, 04) concurrently — up to 4 parallel agents.
2. Review each tree's output in-session. Commit after review passes.
3. Dispatch Wave 2 (tree 05) after Wave 1 completes.
4. Run integration review: `go build ./...`, `go test ./... -race`, `go vet ./...`.
5. Run `gofmt -l .` and fix any formatting issues.
6. Final commit with integration summary.

## Review Checklist

- [ ] All new code compiles: `go build ./...`
- [ ] All tests pass: `go test ./... -race`
- [ ] No vet issues: `go vet ./...`
- [ ] No unused functions/imports (U1000)
- [ ] No debug artifacts (fmt.Println, TODO, FIXME, placeholder values)
- [ ] No line-number corruption: `grep -rcE '^\s+[0-9]+\|' --include='*.go' .` returns 0
- [ ] All new features have at least one user-facing interface (wiring requirement)
- [ ] Setter methods have nil guards
- [ ] No mutex held across I/O
- [ ] Prompt components follow §5 conventions

## Integration Test Plan

After all trees complete:
1. `go build ./...` — full compilation
2. `go test ./... -race -count=1` — full test suite with race detector
3. `go vet ./...` — static analysis
4. `go run ./tools/analyzers/predid/... ./...` — predictable ID check
5. `gofmt -l .` — formatting (should return empty)
6. Manual: verify verification mode triggers on a 3+ file edit scenario
7. Manual: verify anti-pattern prompts appear in agent system prompts
8. Manual: verify prompt cache boundary appears in Anthropic API requests

## Coding Conventions

See SHARED-CONVENTIONS.md §2-§3 for Go coding and test conventions.

## Completion Tracking Table

| Tree | Status | Notes |
|------|--------|-------|
| 01-adversarial-verification | PENDING | |
| 02-anti-pattern-prompts | PENDING | |
| 03-prompt-cache | PENDING | |
| 04-compaction-user-messages | PENDING | |
| 05-combined-improvements | PENDING | |

## Open Questions

None — all resolved in pre-planning Q&A (2026-07-24).
