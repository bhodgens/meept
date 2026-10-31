# 08: Cluster Dispatch Identity - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks via TDD. Do NOT commit.
> No read_file on source; explore with search_files/cat. Write once, stop.

## Meta

- **Parent:** ../master.md
- **Scope:** Verify DispatchJob signatures before local execution (remove the
  TODO), default require_node_signatures ON when cluster.enabled (C9).
- **Dependencies:** 00T (TESTS_VALIDATED)
- **Estimated Context:** 45K
- **Concurrency Group:** A

## Goal

Cross-node task injection by bare agent_id ends: an unsigned or unverifiable
dispatch is rejected with a Warn and never executed.

## Interface Contracts (From Parent)

C9 (verbatim): ExecutorBridge verifies DispatchJob.Signature (TODO at
internal/cluster/executor_bridge.go:198-202 REMOVED); cluster.security.
require_node_signatures defaults TRUE when cluster.enabled=true (config flip
+ template + docs same commit); unsigned => reject + Warn, never execute.
Verification uses the existing ed25519 gossip signing keys
(internal/cluster/engine.go node identity shape).

## Verified Anchors

- The TODO: internal/cluster/executor_bridge.go:198-202 ("we accept unsigned
  jobs").
- Signing keys: internal/cluster/engine.go:101-107 (node id from pubkey),
  gossip.go:491-492 (signature verification when enabled) - REUSE the same
  verifier, do not mint a second crypto path.
- Config: internal/config/cluster_config.go:88-91 (ClusterSecurityConfig).
- Payload: pkg/models/cluster.go:102-113 DispatchJob fields.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state |
|---------|-----------|-------|----------------|------------|------------|
| T1 | ExecutorBridge verifies job signature against the signing node's pubkey before invoking the local agent; mismatch/absent => reject + Warn | internal/cluster/executor_bridge.go, +test | `go test -p 2 ./internal/cluster/ -run TestDispatchSignature` | no verification | valid signature executes; tampered payload rejected; unsigned rejected |
| T2 | Default flip: require_node_signatures true when cluster enabled (opt-out stays possible - explicit config false) | internal/config/cluster_config.go, config template cluster section, +test | `go test -p 2 ./internal/config/ -run TestCluster` | default false | fresh config + cluster.enabled=true => signatures required; explicit false honored |
| T3 | Document the flip in the cluster security docs (docs/workflows/ cluster doc) same-leaf | docs/workflows/<cluster doc> | grep | absent | flip + opt-out documented |

## Self-Verification Checklist

- [ ] One crypto verifier reused (grep: no second ed25519 verify path added)
- [ ] Rejection path NEVER executes the job (fail closed), Warn carries node + job id
- [ ] Gate cluster member suite GREEN when it lands (orchestrator runs it)
- [ ] gofmt clean; no debug artifacts

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched: [paths]
2. Verify output: [last 20 lines each]
3. Deviations: [none / list]

## Review Checklist (For Review Agent)

- [ ] TODO removed; fail-closed proven by test (unsigned + tampered)
- [ ] Default flip is conditional on cluster.enabled (non-cluster boots untouched)
- [ ] Docs flipped in same leaf
