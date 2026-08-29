# Eval HTTP - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks via TDD. Do NOT commit.
> Do NOT use read_file on existing source. After writing, do not read back.

## Meta

- **Parent:** ../master.md
- **Scope:** HTTP API for eval runs. Same JSON as CLI. Wave 1 surface.
- **Dependencies:** 01-eval-core.md
- **Estimated Context:** 50K
- **Concurrency Group:** B

## Goal

TUI/GUI/menubar (leaves 16-18) read this API. Do not invent a second shape.

## Context

Routes live under `/api/v1/`. Auth follows existing HTTP key middleware. Unix RPC is owner-trusted; also register `eval.run|show|list` RPC so CLI-over-daemon can use it later. Prefer HTTP handlers that share a small service type.

Key files: `internal/comm/http/server.go` route registration, `internal/rpc` handler pattern.

## Interface Contracts (From Parent)

C1 JSON. C9 clients consume these paths exactly:

```
POST /api/v1/eval/runs     body {task_id, model_id, k, command, workdir} -> RunRecord
GET  /api/v1/eval/runs     -> {runs: []RunRecord}
GET  /api/v1/eval/runs/{id} -> RunRecord
```

Do not emit `type: chat_message` for eval events. If you publish a bus topic, classify as generic event.

### What This Leaf Exposes

Those three routes plus RPC methods `eval.run`, `eval.show`, `eval.list`.

### What This Leaf Consumes

C1. Disk store from leaf 02 if already merged; else a tiny store in `internal/eval/store.go` that 02 can reuse. If 02 already wrote the store, use it — do not fork.

## Tasks

### Task 1: Store helper if missing

**Files:** Create `internal/eval/store.go` only if 02 did not. Idempotent path `~/.meept/eval`.

### Task 2: HTTP handlers + tests

**Files:** Create `internal/comm/http/eval_handlers.go`, `eval_handlers_test.go`

httptest. Invalid k, missing workdir = 400. Unknown id = 404.

### Task 3: Wire routes + RPC

**Files:** Modify `internal/comm/http/server.go` RegisterRoutes. Modify daemon RPC registration in `internal/daemon` or `internal/rpc` — grep existing `RegisterHandler` style. Direct RegisterHandler, not a bus proxy without a responder.

## Self-Verification Checklist

- [ ] JSON matches C1
- [ ] Tests do not need a live daemon
- [ ] No chat_message WS type
- [ ] Production RegisterRoutes caller exists
- [ ] Do NOT commit

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** none

## Review Checklist (For Review Agent)

- [ ] Auth middleware still wraps the new routes
- [ ] No makeProxy without responder
- [ ] Multiuser-off path unchanged
