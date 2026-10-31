# Activity + Drift RPC Surface - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing a
> file, do NOT read it back - write once and stop. Report what you built,
> files touched, and any deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** New RPC methods (activity.query / activity.recent /
  activity.overview / activity.findings / agents.review) registered as
  direct daemon closures, wiring the aggregator + ring + employee Manager
  to clients.
- **Dependencies:** 01-aggregator.md (Aggregator + ActivityRecord), 05-ring.md (Ring)
- **Estimated Context:** 85K
- **Concurrency Group:** B (dispatch after 01 and 05 reach REVIEWED)

## Goal

Give TUI and GUI one query surface for activity, findings, and drift.
Register the four `activity.*` methods plus `agents.review` as direct
`RegisterHandler` closures in a new `internal/daemon/activity_rpc.go`, with
params/result shapes byte-identical to master contract C2/C4.

## Context

New daemon RPC methods follow the direct-closure pattern (not bus proxies):
construct an aggregator/ring/manager handle in daemon wiring, then register
closures. Precedent files to copy in style: internal/daemon/memory_rpc.go,
internal/daemon/changes_rpc.go, internal/daemon/verdict_audit.go (for
opening the chain store at a data-dir path). The employee Manager already
implements findings listing (ListAuditFindings) and drift (Review with
DriftScore + RecentFindings) - this leaf only EXPOSES them; it adds no
employee business logic.

## Verified Anchors

Anchors grep-confirmed 2026-10-30. Re-grep before editing.

- internal/rpc/server.go:230-231 - RegisterHandler(method, func(ctx, json.RawMessage) (any, error)) - the registration seam.
- internal/daemon/memory_rpc.go:29-92 - direct-closure RPC file pattern to copy.
- internal/daemon/daemon.go:1375-1380 - where a handlers map is registered in bulk (employee handlers) - wiring neighborhood.
- internal/employee/handler.go:161-163 - agents.audit.list/resolve/verify existing registrations (do not duplicate).
- internal/employee/manager.go:2192-2194 - Manager.ListAuditFindings(ctx, q AuditQuery).
- internal/employee/manager.go:2220-2235 + :170-177 - Manager.Review -> Review{DriftScore, RecentFindings} - NET-NEW exposure, zero RPC callers today.
- internal/daemon/verdict_audit.go:83 - auditlog.OpenStore filepath.Join(dataDir, "audit_chain.db") - chain store wiring neighborhood.
- internal/daemon/otlp_wiring.go:31-67 - example of a wiring helper file the activity wiring should mirror.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/daemon/activity_rpc.go
func registerActivityRPC(server *rpc.Server, agg *activity.Aggregator,
    ring *activity.Ring, mgr *employee.Manager, planRes activity.PlanResolver)
// Called from daemon wiring after components exist. Registers exactly:
//   activity.query    (C2 params/result, incl. sort chrono|task|plan,
//                      task/plan/agent/kinds filters, limit/offset)
//   activity.recent   (C2; ring.Tail; includes dropped counter)
//   activity.overview (C2; per-agent active_tasks, last_activity,
//                      drift_score from mgr.Review, open_findings count)
//   activity.findings (C2; delegates to mgr.ListAuditFindings with
//                      employee_id/since/severity/resolved/limit/offset)
//   agents.review     (C4; delegates to mgr.Review(employee_id))
// JSON result field names byte-identical to C1/C2 (activity.query result:
// {"records":[...],"total":N}).
```

### What This Leaf Consumes

- C1 ActivityRecord, C3 Ring (both from earlier leaves)
- internal/employee Manager.ListAuditFindings + Manager.Review (existing)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | activity.query closure: param decode, sort/filter passthrough, JSON result | Create: internal/daemon/activity_rpc.go; Test: internal/daemon/activity_rpc_test.go | `go test -p 2 ./internal/daemon/ -run TestActivityRPC` | method unregistered | RPC server with fake agg returns {"records":[...],"total":N}; unknown sort -> error | Given seeded fake aggregator; When call activity.query sort=task; Then grouped result JSON matches C2 |
| T2 | activity.recent + activity.overview closures | Test: internal/daemon/activity_rpc_test.go | `go test -p 2 ./internal/daemon/ -run TestActivityRPC` | methods unregistered | recent returns ring tail + dropped; overview aggregates per-agent drift via mgr.Review | Given ring with 5 events; When activity.recent limit=3; Then 3 newest + dropped count |
| T3 | activity.findings + agents.review closures over real Manager | Test: internal/daemon/activity_rpc_test.go | `go test -p 2 ./internal/daemon/ -run TestActivityRPC` | methods unregistered | findings pass filters to ListAuditFindings; agents.review returns DriftScore | Given employee store with findings; When agents.review; Then drift_score in [0,1] |
| T4 | Daemon wiring: construct Aggregator + Ring + resolver, call registerActivityRPC; RegisterHandler collision safety | Modify: internal/daemon/components.go (wiring), internal/daemon/daemon.go (call site); Test: internal/daemon/activity_wiring_test.go | `go test -p 2 ./internal/daemon/ -run TestActivityWiring` | wiring absent | boot wiring registers all 5 methods exactly once; double-registration attempt logs and skips (no panic) | Given components built; When daemon starts (test double); Then all methods registered with live handles |

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts satisfied exactly (C2/C4 shapes byte-identical)
- [ ] All files at exact specified paths
- [ ] No deviations (or deviations documented in the report)
- [ ] No scope creep - only what the tasks specify
- [ ] All handlers nil-safe: a nil ring or nil mgr degrades that method to a
      clean RPC error, never a panic

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [verify commands; last 20 lines of output]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; tests passing
- [ ] C2/C4 param/result JSON shapes byte-identical to master
- [ ] Method names exactly: activity.query, activity.recent,
      activity.overview, activity.findings, agents.review
- [ ] No duplication of agents.audit.* methods
- [ ] Wiring nil-guards every handle (typed-nil rule)
- [ ] No scope creep; no debug artifacts or line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

- HTTP surface for the GUI is LEAF 04's job - do not touch
  internal/comm/http here.
- Decode params with json.Unmarshal into small param structs in the same
  file; RFC3339 for timestamps (second precision - truncate when comparing
  against store stamps).
- The GUI polls over HTTP; keep these RPC closures free of streaming
  assumptions (single request/response).
