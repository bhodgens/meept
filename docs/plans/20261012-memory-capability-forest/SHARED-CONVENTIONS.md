# Memory Capability Forest — Shared Conventions

Forest: `docs/plans/20261012-memory-capability-forest/`
Source analysis: TencentDB-Agent-Memory @ b1f79fc, clone at `/tmp/tencent-memory-review`.

All four trees follow these conventions. Each master.md references this file
instead of restating it.

## 1. Project layout and package seams

- Memory core: `internal/memory` (single Go package, ~24.7k LOC). All four
  trees touch this package. New subsystems MUST land in new subpackages to
  minimize same-file contention:
  - Tree 01: `internal/memory/l0/` (new)
  - Tree 02: `internal/memory/conflict/` (new)
  - Tree 03: `internal/memory` existing files + `internal/memory/review/` (new)
  - Tree 04: `internal/memory/knowledge/` (new)
- Manager wiring (`manager.go`, `handler.go`, `epistemic_ambient.go`) is the
  shared file set. Only ONE leaf per wave may edit these files (see §4 waves).
- Queue: `internal/queue.PersistentQueue` is the async job substrate. No Redis.
- Prompt injection seam: `internal/agent/prompt.go` `AddSectionWithStability`
  + `internal/agent/loop.go` memory-context block (~line 7160-7195).
- CLI: `cmd/meept/memory.go`. RPC: `internal/rpc` handler map.
- Audit: `internal/auditlog` (canonical JSON hash chain) for governance verdicts.

## 2. Frozen contracts (cross-tree)

### 2.1 L0 record schema (Tree 01 → all consumers)

One JSON object per line in `<memory.data_dir>/l0/YYYY-MM-DD.jsonl`:

```json
{"id":"<pkg/id.Generate()>","ts":"RFC3339","session_id":"...","role":"user|assistant","content":"...","meta":{}}
```

Sanitization reuses `internal/memory` ambient sanitizer conventions; injected
`<memory-context>` tags are stripped before append (prevents feedback loops,
mirrors TAM `l0-recorder.ts`).

### 2.2 Async distill job types (Tree 01)

Enqueued on PersistentQueue:
- `memory.l1_extract` — payload `{"session_id","l0_path","since_msg_id"}`
- `memory.l2_scene` — payload `{"session_id","since_ts"}`
- `memory.l3_persona` — payload `{"session_id"}`

Idempotency: L1 upserts claims by record_id (existing supersede/rev semantics,
`internal/memory/epistemic.go:570`). LLM calls resolve through the existing
`memory_model` slot preference (`memory_model` > general provider > general
client).

### 2.3 Conflict decision type (Tree 02)

```go
package conflict
type Action string // "store" | "merge" | "supersede" | "skip"
type Decision struct {
    CandidateIdx int
    Action       Action
    TargetIDs    []string
    MergedContent string
    Reason       string
}
```

Fail-open default: any detector error → all actions become `store`
(preserves current behavior; matches TAM `l1-dedup.ts` fallback).

### 2.4 Review verdict surface (Tree 03)

- RPC: `memory.review.list` (filter: status, age, confidence band), 
  `memory.review.verdict` (claim_id, promote|reject, reason ≤512B).
- CLI: `meept memory review`, `meept memory verdict <id> promote|reject`.
- Every verdict logs to `claim_verdicts.jsonl` (existing calibration path)
  AND appends one auditlog entry.

### 2.5 Knowledge asset contract (Tree 04)

```go
package knowledge
type Page struct {
    ID    string; Title string; Body string
    Links []string // page IDs
    Source string; IngestedAt time.Time
}
```

- Tools: `knowledge_search` (FTS over pages), `knowledge_read` (page ID).
- Config `[memory.knowledge] enabled` — default **false** (opt-in-defaults
  rule; wiki→inference reachability is a product decision). When disabled,
  tools are not registered and nothing enters prompts.

## 3. Coding conventions

- Go, stdlib-first; no new external deps without license check
  (GPL/AGPL = STOP and report).
- Errors: wrap with `fmt.Errorf("...: %w", err)`; no ignored errors; no bare
  panic. Sentinel errors for dismissible conditions.
- No `os.Getwd()` in daemon code. No `time.Now().UnixNano()`/`math/rand` IDs —
  `pkg/id.Generate()` only.
- Every `Set*` nil-guarded. No mutex across I/O.
- All new config fields: JSON5 in `internal/config`, default documented,
  opt-in (false) unless the tree says otherwise.
- UI text lowercase (TUI/GUI surfaces).

## 4. Test policy (repo e2e rule)

AGENTS.md: **new feature tests live in the hermetic e2e tier.** Each tree's
verification leaf adds `e2e/suites/<name>/` with the `e2e` build tag and a
manifest entry (`e2e/manifest.json` path_map). Run: `make e2e-fast-area AREA=<name>`.
In-package `*_test.go` files are only permitted for bug fixes to existing
behavior. Deterministic fake LLM chatter (see `references/fake-chatter-mapreduce-testing-2026-09-06.md`
pattern in meept-development skill) replaces live models in e2e.

## 5. Waves (compile-contention control)

All leaves edit `internal/` Go code in overlapping packages; the forest runs
in waves so no two in-flight leaves share a package:

- Wave 1: T01-L0 recorder · T03-review CLI/RPC · T04-knowledge store
  (three disjoint packages: `l0/`, `review/`+cmd, `knowledge/`)
- Wave 2: T01-async worker (queue wiring) · T02-conflict detector
  (`internal/memory/conflict/`) · T04-retrieval tools
- Wave 3: integration leaves — manager/loop wiring, prompt-stability split,
  e2e suites, docs.

Wave 3 is strictly serial (single integration leaf per tree, then root
integration review).

## 6. Commit policy

Only orchestrators commit (skill rule). Dispatch briefs say: "Do NOT commit.
Do NOT run git add." Orchestrator stages explicit paths per leaf (memory
rule: commit explicit paths — siblings sweep). Pre-push gates: bench doctor
must be alive; stale lint blocks — run `go vet ./internal/memory/...` before
staging.

## 7. Known pitfalls carried into dispatch briefs

- Concurrent same-package TDD leaves transiently break `go build` — never
  re-dispatch a healthy leaf for a sibling's RED file.
- `findDaemonBinary` prefers `~/go/bin`: `go install ./cmd/meept-daemon`
  before attributing live behavior.
- Test sweeps at `-p 2` (port exhaustion).
- No leaf may cite line numbers from `read_file` output into source files.
