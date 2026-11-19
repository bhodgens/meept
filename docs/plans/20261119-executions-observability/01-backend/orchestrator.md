# 01 Backend - Execution Runs Projection + RPC

> **For the implementing agent:** You are the orchestrator for this branch.
> Dispatch test leaves first (00T-tests.md), then impl leaves. Do NOT commit.

## Meta

- **Parent:** ../master.md
- **Children:** 00T-tests.md, 01-store.md, 02-rpc-and-http.md
- **Scope:** Persist bot.result/employee.result to a bounded projection; expose it.

## Interface Contracts

### From master (C1)
execution_runs schema; agents.executionRuns RPC; GET /api/v1/executions.

## Ownership

- 01-store.md creates internal/execlog/store.go (SQLite) +
  internal/execlog/projector.go (bus subscriber) and wires via
  internal/daemon wireComponents (or an init-once in internal/daemon).
- 02-rpc-and-http.md exposes RPC "agents.executionRuns" and HTTP
  GET /api/v1/executions in internal/comm/http/server.go or
  internal/rpc (pattern of the existing activity endpoints).

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-tests.md | PENDING | 0 | |
| 01-store.md | PENDING | 0 | |
| 02-rpc-and-http.md | PENDING | 0 | |
