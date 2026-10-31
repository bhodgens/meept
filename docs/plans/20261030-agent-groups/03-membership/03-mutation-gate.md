# 03: Membership Mutation Gate - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks via TDD. Do NOT commit.
> No read_file on source; explore with search_files/cat. Write once, stop.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** The governance core (C5): groups.* RPC methods with the
  agent-update/admin-only write gate, plus the file-level backstop.
- **Dependencies:** 01 + 02 (REVIEWED)
- **Estimated Context:** 60K
- **Concurrency Group:** B (serialized, last in branch)

## Goal

Only the agent-update agent or an admin human can change membership - denied
at the RPC seam for RPC callers AND at the file-tool layer for bypass
attempts. Every mutation lands in the audit trail.

## Interface Contracts (From Parent)

C5 (verbatim): RPC groups.list / groups.explain(agent_id) / groups.assign /
groups.remove. Write gate: caller is (a) agent id in
security.groups.admin_agents (default ["agent-update"]) OR (b) a human key
whose STORE identity is in security.groups.admin_users. Everyone else:
denied at the seam with a deny reason. FILE BACKSTOP: when
groups.default_managed=true, BlockedPaths gains config/agents/**, the users
file, and the groups config section - for NON-ADMIN agents only.

## Verified Anchors

- RPC registration: internal/rpc/proxy.go + handlers pattern (rpc/groups.go
  NEW; service layer from 06-diagnosis if REVIEWED - else the RPC handlers
  call internal/groups + a thin writer this leaf adds, and 06 refactors onto
  it).
- Identity at RPC: internal/rpc/server.go PeerIdentity (unix socket) and the
  HTTP identity middleware path - the gate resolves caller identity the SAME
  way employee handler auth SHOULD (auth store; NOT key-string derivation -
  issue #76 class defect is a review-blocker here).
- Writer: membership lives in TWO stores (AGENT.md frontmatter - mutate the
  agent definition file via the existing agent-definition write path if one
  exists; users - via the auth store). groups.assign/remove MUST specify
  subject kind and route to the right writer; agent-file writes go through
  the same validation as manual edits (parse round-trip).
- BlockedPaths: pkg/security PermissionChecker config globs (components.go
  wiring); backstop = config-only addition when default_managed=true.
- Audit: internal/auditlog chain - emit membership-change events.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state |
|---------|-----------|-------|----------------|------------|------------|
| T1 | groups.list + groups.explain RPC (read methods, no gate beyond auth) | internal/rpc/groups.go, proxy.go, +tests | `go test -p 2 ./internal/rpc/ -run TestGroupsRPC` | methods absent | list returns policies+bindings; explain returns resolution w/ sources |
| T2 | groups.assign/remove with C5 write gate (agent-update id OR admin user; deny otherwise; audit event on success) | internal/rpc/groups.go (+writer), +tests | `go test -p 2 ./internal/rpc/ -run TestGroupsMutationGate` | absent | privileged => applied + audited; non-privileged agent + plain user => denied with reason; unknown subject => error |
| T3 | File backstop: when default_managed=true, non-admin agents' file_write/shell against config/agents/**, users file, groups config => denied | pkg/security config glob wiring or groups gate contribution, +test | `go test -p 2 ./pkg/security/ ./internal/agent/ -run TestGroupsBackstop` | absent | bypass attempt via file tool denied; admin agent unaffected |
| T4 | Validation integration: assign/remove runs C6 policy-tool validation before persisting | writer, +test | `go test -p 2 ./internal/rpc/` | n/a | policy with unregistered tool => rejected, nothing persisted |

## Self-Verification Checklist

- [ ] Gate resolves identity via auth store / PeerIdentity - ZERO key-string derivation
- [ ] Deny is at the SEAM (handler), not the client
- [ ] Mutations audited (auditlog events); file writes parse-roundtrip
- [ ] Backstop scoped to non-admin agents only (agent-update keeps its tools)
- [ ] Gate tests GREEN and unmodified; -race clean; gofmt clean

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched: [paths]
2. Verify output: [last 20 lines each]
3. Deviations: [none / list]

## Review Checklist (For Review Agent)

- [ ] All four RPC methods registered in proxy (missing registration = dead method)
- [ ] Deny paths tested for BOTH caller types (agent + human)
- [ ] Backstop does not lock OUT the admin (agent-update can still edit definitions)
- [ ] Audit events land for every successful mutation
