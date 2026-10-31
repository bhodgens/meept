# Agent Groups: Policy Binding Layer - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Root
- **Parent:** none (root)
- **Children:** 14 (1 test-gate leaf, 2 branches, 10 standalone leaves)
- **Scope:** Agent grouping/isolation: a group-policy binding layer enforced at the three existing gates (dispatch-time tool filter, tool-call-time permission check, retrieval-time memory filter), membership on AGENT.md frontmatter + users.json5, agent-update-only mutation, diagnosis surfaces, and the absorption of the dead `internal/security.Engine`'s two good ideas (fence-as-stage, decision audit) before its deletion (issue #77 resolution).

## Goal

Meept enforces per-agent tool lists (FilteredToolRegistry, registry.go filterTools)
and per-employee constitution gates (PreExecChecker), but has no policy layer:
rules are per-agent files, deny is inexpressible, memory recall is global,
and "what can agent X do?" has no answer command. This tree adds ONE binding
model - (subject) -> (policy) -> (scope), additive grants + explicit
deny-overrides, one-level nesting - installed at gates that already exist and
are enforced. It also ports the two surviving ideas of the never-wired
internal/security.Engine into the live permission path, then deletes the
engine (resolving issue #77), and closes the cluster identity hole
(verified DispatchJob signatures).

## User decisions (frozen 2026-10-30, authorized via "create the hierarchical plan per your recommendations")

| # | Decision | Source |
|---|----------|--------|
| D1 | Membership: BOTH surfaces - agents via `groups:` AGENT.md frontmatter; humans via `groups:` on User in users.json5 | session recommendation, confirmed |
| D2 | Governance: group membership is mutated ONLY via daemon RPC (`groups.*`) gated to the agent-update agent id + human admin keys; BlockedPaths backstop on config/agents + users file + groups config for non-privileged agents | user requirement: "only the 'agent update' agent would be able to handle/touch" |
| D3 | Issue #77: PORT fence-stage + decision-audit concepts into pkg/security.PermissionChecker, THEN delete internal/security.Engine | user asked "can we add the benefits from the 'dead' version into our plan?" - answer yes via absorption |
| D4 | Tool assignment: deny-at-assignment validation (policy tool names must resolve in the live registry), intersection semantics (binding never widens the spec), deny beats allow (frozen order) | user requirement: "agents don't get assigned tools they don't have permission to, even if it's defined" |
| D5 | Diagnosis: `meept groups explain <agent>` (effective access + why), `meept groups check` (config validation), doctor permissions check; all report-only | user requirement: "surface for diagnosis should be easy, tooling complete, cover edge cases" |
| D6 | Docs: `docs/workflows/agent-groups.md` + configuration reference + `make docs-generate`; "how it works + how to configure" is a first-class deliverable | user requirement: "clear documentation on how this works, how to configure it" |
| D7 | Backward compat: ungrouped agents keep TODAY's behavior (spec allowlist only) while `security.groups.default_managed: false` (default); setting it true routes ungrouped agents through the fail-closed `default` policy | balances isolation posture against upgrade breakage |
| D8 | Empty-allowlist fix: when a group policy IS active, an empty effective allowed-tool set means DENY-ALL - never FilteredToolRegistry's legacy empty=all behavior | fail-closed; audit finding |

Options deliberately REJECTED (recorded so re-litigation has the baseline):
Zanzibar/ReBAC tuple store (no sharing chains yet; binding table migrates to
tuples losslessly later), Cedar/OPA/Casbin engines (behind the
auth.PermissionChecker seam if ever needed), full LDAP nesting (one level max),
external vector DBs, per-group OS containers (issue #75, out of scope here).

## Architecture

```
policy (named bundle: tools allowed/denied, memory_max_scope, fs_roots,
        network_class, allowed_models)
   ^
   | binding (subject: agent:<id> | group:<name>, policy, scope: daemon|project:<id>)
   |
subject = agent (frontmatter `groups:`) or group (one level of nesting)
          or human user (users.json5 `groups:`)

Enforcement gates (ALL pre-existing; this tree only feeds them):
  1. Dispatch-time:   filterTools intersection (internal/agent/registry.go:777)
  2. Tool-call-time:  Executor.checkPermission -> pkg/security.PermissionChecker
                      (executor.go:1675; generalized from employee-only to ALL agents)
  3. Retrieval-time:  WithScopeFilter memory ceiling (issue #70 tree branch 01
                      delivers the filter; this tree feeds it the group ceiling)
```

Absorbed from the dead engine (internal/security/engine.go - frozen since
April, zero production callers): (a) fence check as a first-class permission
stage (engine.go:427-461 shape) instead of per-tool opt-in; (b) every decision
logged with rule_source (engine logDecision shape) as structured slog lines;
(c) time-boxed overrides, re-invented as reviewable config artifacts.
After the ports land, the engine package is DELETED (leaf 01-permissions/03).

File-contention map (serialize these):
- pkg/security/permissions.go: leaves 01-permissions/01 and 02 touch it - the
  branch orchestrator sequences them.
- internal/agent/executor.go + registry.go: leaf 04 only (no sibling touches
  these files).
- internal/config/schema.go: leaves 02 (groups model), 07 (overrides), 08
  (cluster default) - sequence 02 -> {07, 08} or run 07/08 after 02 REVIEWED.
- internal/daemon/components.go: 01-permissions/01 and leaf 04 - branch 01
  completes before 04 dispatches.

## Interface Contracts

### C1: Fence stage in pkg/security (Owner: 01-permissions/01)

```
// pkg/security - narrow interface, NO internal/ import (daemon adapts):
type FenceStage interface {
    CheckPath(path, op string) error   // op: read|write|delete
    CheckCommand(command, workDir string) error
}
// PermissionChecker gains: SetFence(FenceStage) (typed-nil guarded).
// CheckPermissionForAgent runs the fence stage AFTER path globs, BEFORE
// confirmation: file actions -> CheckPath(details["path"], mapped op);
// shell_execute -> CheckCommand(details["command"], details["workdir"]).
// Fence error => Allowed:false, Reason: err.Error(). Nil fence = skip (legacy).
// Per-session injection: the daemon constructs ONE FenceStage adapter per
// session working-dir binding and injects at loop creation - NEVER a shared
// instance mutated at handler.go:2991. The per-tool SetFenceChecker wiring
// (components.go:6649-6766) is REMOVED in the same leaf once the stage is
// enforced centrally.
// Consumers: 01-permissions/02, 04-tool-gates, 00T-e2e
```

### C2: Decision audit line (Owner: 01-permissions/02)

```
// One slog line per checkPermission verdict at the executor seam:
// level=INFO msg="tool permission decision" decision=allow|deny|confirm
//   action=<action> tool=<tool> agent=<agent_id> groups=<comma,list|"-">
//   rule_source=<base_rule|group_deny|group_allow|fence|employee_pre_exec|
//                override|confirmation_gate|financial|unknown_action>
//   risk=<risk> reason="<short>"
// No DB write. No I/O under any lock. Denies log at WARN, allows at DEBUG
// (config: security.groups.decision_log_level, default "deny-warn").
// Consumers: 06-diagnosis (explain reads the same resolution, not the log),
//            operators (log-based audit)
```

### C3: Group model types + config (Owner: 02-groups-model)

```
// internal/groups (NEW package; imports internal/config only - no agent import)
type Policy struct {
    Name           string   `json:"name"`
    ToolsAllowed   []string `json:"tools_allowed,omitempty"`   // registered tool names (normalized)
    ToolsDenied    []string `json:"tools_denied,omitempty"`    // deny beats allow
    MemoryMaxScope string   `json:"memory_max_scope,omitempty"` // "", "project", "user", "global"
    FSRoots        []string `json:"fs_roots,omitempty"`         // additional fence roots
    NetworkClass   string   `json:"network_class,omitempty"`    // "", "unrestricted", "allowlist", "none"
    AllowedModels  []string `json:"allowed_models,omitempty"`   // alias names; empty = unrestricted
}
type Binding struct {
    Subject string `json:"subject"` // "agent:<id>" | "group:<name>" | "user:<id>"
    Policy  string `json:"policy"`  // policy name
    Scope   string `json:"scope"`   // "daemon" | "project:<id>"
}
type Resolved struct { // output of Resolve(spec-tools, bindings, policies)
    AllowedTools  []string // spec ∩ union(group allows) minus union(group denies)
    DeniedTools   []string
    MemoryMaxScope string  // narrowest across groups ("" = unrestricted)
    FSRoots       []string
    NetworkClass  string   // most restrictive across groups
    AllowedModels []string
}
// Precedence (FROZEN): evaluate denies first, then allows; layering merges
// most-restrictive-wins for scopes/classes. Nesting: group-in-group ONE level.
// Cycle detection at load: cyclic group membership = config error, fail boot.
// Config surface: top-level `groups` key in meept.json5:
//   groups: { policies: [...], bindings: [...],
//             default_managed: false,
//             decision_log_level: "deny-warn" }
// Template config/meept.json5 mirrors with comments. gendoc tags required.
// Consumers: 03-membership/*, 04-tool-gates, 05-memory-ceiling, 06-diagnosis, 07
```

### C4: Membership fields (Owner: 03-membership/01 + 02)

```
// AGENT.md frontmatter (5-step field pipeline - see meept-design skill):
AgentMetadata.Groups []string `yaml:"groups,omitempty"` -> AgentSpec.Groups
// users.json5:
User.Groups []string `json:"groups,omitempty"` (internal/auth/users.go)
// Resolution: agent effective groups = frontmatter groups ∪ user groups
// (multiuser session owner, when identity resolves; see issue #76 dependency).
// Unknown group name in frontmatter: Warn at load, member ignored (fail-safe,
// logged) - `groups check` reports it as an error.
// Consumers: 04-tool-gates, 06-diagnosis, 00T-e2e
```

### C5: Membership mutation gate (Owner: 03-membership/03)

```
// RPC: groups.list, groups.explain(agent_id), groups.assign, groups.remove
// (internal/rpc/groups.go + HTTP handlers + internal/services/groups_service.go)
// CLI: `meept groups list|explain|check|assign|remove` (cmd/meept/groups.go)
// WRITE GATE (assign/remove): caller must be (a) the agent-update agent id
// (config: security.groups.admin_agents, default ["agent-update"]) or
// (b) a human key whose store identity holds groups admin
// (config: security.groups.admin_users). Everyone else: denied at the seam.
// FILE BACKSTOP: when security.groups.default_managed=true, BlockedPaths gains
// config/agents/**, the users file, and the groups config section for
// non-admin agents - file_write/shell bypass attempts hit a hard deny.
// Consumers: 00T-e2e (gate asserts a non-privileged agent cannot mutate)
```

### C6: Assignment-time validation (Owner: 04-tool-gates)

```
// Policy tool names are validated against the LIVE tool registry at:
// (a) daemon boot (config load) and (b) groups.assign RPC time.
// A name that normalizes to no registered tool (use the EXISTING grant
// normalization - "shell_execute" resolves to registered "shell",
// AgentSpec GrantedToolNames precedent) => REJECT with the offending name.
// A policy that saves with a dangling tool name is a bug, not a state.
// Consumers: 02-groups-model (Config validation hook), 06-diagnosis (check)
```

### C7: Memory ceiling wiring (Owner: 05-memory-ceiling)

```
// DEPENDS ON: issue #70 tree (docs/plans/20261030-memory-second-brain/)
// branch 01-scoping COMPLETE (Scope, WithScopeFilter, MemoryMaxScope).
// This leaf ADDS: effective ceiling = narrower of (agent MemoryMaxScope,
// group Resolved.MemoryMaxScope). Fed into WithScopeFilter at buildMemoryContext
// + the four memory tools. Groups NEVER widen what the agent declares.
// Consumes C3. Consumers: 00T-e2e
```

### C8: Time-boxed overrides (Owner: 07-overrides)

```
// $MEEPT_HOME/group_overrides.json5 (gitignore-able, operator-managed):
// [{subject:"agent:x", allow:["git_push"], reason:"deploy window",
//   expires:"2026-11-05T17:00:00Z"}]
// Expired entries: skipped + one Warn at load. Overrides ADD allows only
// (never denies) and never widen memory/network ceilings. `groups check`
// reports active + expired overrides. CLI: `meept groups override add|list|prune`.
// Consumers: 04-tool-gates (resolver reads overrides), 06-diagnosis
```

### C9: Cluster identity (Owner: 08-cluster-identity)

```
// 1. ExecutorBridge verifies DispatchJob.Signature before local execution
//    (REMOVES the TODO at internal/cluster/executor_bridge.go:198-202).
// 2. cluster.security.require_node_signatures defaults TRUE when
//    cluster.enabled=true (config flip + template + docs same commit).
// 3. Unsigned/unverifiable dispatch => reject + Warn, never execute.
// Verification uses the existing ed25519 gossip signing keys (engine.go shape).
// Consumers: 00T-e2e (cluster suite member)
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | 00T-e2e-agent-groups.md | leaf (test gate) | none | 70K | T |
| 01 | 01-permissions/ (branch) | branch | 00T | - | A |
| 02 | 02-groups-model.md | leaf | 00T | 65K | B |
| 03 | 03-membership/ (branch) | branch | 02 | - | B |
| 04 | 04-tool-gates.md | leaf | 01, 02 | 70K | C |
| 05 | 05-memory-ceiling.md | leaf | 02, issue#70-branch01 | 40K | C |
| 06 | 06-diagnosis.md | leaf | 02, 04 | 55K | D |
| 07 | 07-overrides.md | leaf | 02 | 45K | D |
| 08 | 08-cluster-identity.md | leaf | 00T | 45K | A |
| 09 | 09-docs.md | leaf | all | 50K | E |

Dispatch order: 00T gate -> group A (01-permissions branch, 08) -> group B
(02-groups-model, then 03-membership branch) -> group C (04, 05) -> group D
(06, 07) -> group E (09 docs last, from the LANDED tree).

## Dispatch Protocol

For each concurrency group, in dependency order. Full recipe: skill
`hierarchical-plan-execution`.

### Phase 0: Test Gate (before ANY implementation dispatch)

Dispatch 00T-e2e-agent-groups.md first: tests only (hermetic e2e suites under
e2e/suites/ + manifest path_map entries), fail RED for missing-implementation
reasons. Validate in-session (never delegate review) against the Test Gate
matrix; apply the delta protocol; commit gate tests; set children to
TESTS_VALIDATED. See references/tests-first-gate.md.

Repo e2e rules (root AGENTS.md): suites in `e2e/suites/<name>/` with the `e2e`
build tag; manifest + path->suite mapping in `e2e/manifest.json` same commit;
scratch MEEPT_HOME harness pattern (harness.WithPreBootHook writes the REAL
on-disk store; assert through live RPC surface, not store internals).

### Phase 1: Dispatch Concurrency Groups

Dispatch same-letter children simultaneously via `delegate_task`: goal =
"Implement all tasks from <leaf path>"; context = full leaf text + the frozen
contracts it consumes + conventions below + relevant source INLINED; include
the no-commit, no-git-add, no-read_file-on-source, write-once-and-stop rules.

Package-contention serialization: within branch 01, sequence 01 -> 02 -> 03
(all touch pkg/security or internal/daemon). Leaf 04 waits for branch 01
COMPLETE (components.go + executor.go contention).

### Phase 2: Review and Commit Each Child

Review in-session (the main model - never a delegated reviewer). Check leaf
spec, contracts, Review Checklist; run the leaf's verify commands; gate-test
diff must be empty. Pass: commit (explicit pathspecs, single-line messages;
orchestrator is the only committer) + status REVIEWED. Gaps: re-dispatch with
findings, max 3 cycles, then escalate.

Commit hygiene (repo-proven): run affected gates directly (gofmt, vet scoped,
`go test -p 2 <touched packages>`); `git add` every NEW file BEFORE commit;
normalize new .md to ASCII (pre-commit-ascii gate); every commit reviews root
AGENTS.md; U1000 scans the whole staged package - delete dead code in touched
packages rather than bypassing.

### Phase 3: Integration Review

After ALL children REVIEWED: verify contracts C1-C9 across the combined tree,
`go build ./...`, `make test` (background, output to file), new e2e suites in
isolation, `make graphs` + `make graphs-check` (RPC/config surface changed),
`make docs-generate` if config gendoc drifted, `scripts/config-drift-check.py`.
Gaps: re-dispatch the failing child. Pass: set children COMPLETE.

## Review Checklist

The orchestrator (main model) verifies each child in-session:

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts satisfied (signatures, types, paths)
- [ ] All specified files created/modified at exact paths
- [ ] Gate tests pass GREEN; `git diff` shows no gate-test modification
- [ ] Tests written and passing (e2e for features; unit pins only for bug fixes)
- [ ] Code follows project conventions (no ignored errors, no mutex across
      I/O, typed-nil guards on wiring seams)
- [ ] No scope creep; no obvious bugs or security issues
- [ ] No debug artifacts (print debugging, TODOs, placeholders)
- [ ] No line-number corruption (`     N|` prefixes) in source files

Output: APPROVED or a list of specific gaps.

## Coding Conventions

- **Language:** Go (module github.com/caimlas/meept). GOTOOLCHAIN pinned
  in-repo - plain make targets need no env prefix.
- **Naming:** exported for cross-package seams (Policy, Binding, Resolved,
  Resolve); unexported internals. Config structs carry json+toml tags and
  `//gendoc:` comments - copy neighboring style.
- **Error handling:** wrap with %w; no ignored errors; no bare panic(err).
- **Testing:** new FEATURE tests are hermetic e2e suites; unit tests only for
  bug-fix pins. Table-driven. Test the RESOLUTION function exhaustively
  (table matrix), and the enforcement seams through live surfaces.
- **Defaults:** D7/D8 are product decisions AUTHORIZED here; record in
  docs/agents/opt-in-defaults.md (leaf 09).
- **Never:** time.Now().UnixNano()/math/rand for IDs (pkg/id.Generate);
  os.Getwd() fallbacks in internal/; heredoc writes inside package dirs.

## Requirement Traceability

| Req | Source (user words) | Plan element | Verified by |
|-----|---------------------|--------------|-------------|
| R1 | "clear documentation on how this works, how to configure it" | 09-docs.md | docs/workflows/agent-groups.md + configuration reference + gendoc fresh |
| R2 | "create a gh issue for option 10 and other uncovered issues" | DONE (issues #71-#77 filed); #75 stays external | n/a - recorded here |
| R3 | "agents don't get assigned tools they don't have permission to, even if it's defined" | C6 validation + 04-tool-gates intersection | 00T suite: policy with unregistered tool rejected; grouped agent's LLM-visible + callable tool set equals Resolved.AllowedTools |
| R4 | "surface for diagnosis should be easy" | 06-diagnosis; C2 | 00T suite: explain output names the binding/policy behind each grant |
| R5 | "tooling around permissions should be complete and cover edge cases" | 06-diagnosis + C3/C6/C8 edge cases (cycles, dangling names, deny+allow, empty allowlist D8, unknown group C4) | groups check table-driven unit pins + e2e |
| R6 | "only the 'agent update' agent would be able to handle/touch" membership | C5 gate + backstop | 00T suite: non-privileged agent mutate => denied at RPC AND at file tool |
| R7 | issue #77 absorption then deletion | 01-permissions branch | branch 03 diff deletes package; build+tests green |
| R8 | memory ceilings | C7 | 00T suite: grouped agent cannot recall above ceiling |
| R9 | cluster boundary signed | C9 | 00T cluster member: unsigned dispatch rejected |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-e2e-agent-groups.md | PENDING | 0 | |
| 01-permissions/orchestrator.md | PENDING | 0 | |
| 02-groups-model.md | PENDING | 0 | |
| 03-membership/orchestrator.md | PENDING | 0 | |
| 04-tool-gates.md | PENDING | 0 | |
| 05-memory-ceiling.md | PENDING | 0 | |
| 06-diagnosis.md | PENDING | 0 | |
| 07-overrides.md | PENDING | 0 | |
| 08-cluster-identity.md | PENDING | 0 | |
| 09-docs.md | PENDING | 0 | |

Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS |
IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

Gate rule: implementation children stay PENDING until 00T reaches
TESTS_VALIDATED. Branches govern their own children's statuses.

## Integration Test Plan

1. `go build ./...` - clean combined tree (engine deleted, no dangling refs).
2. `go test -p 2 ./internal/groups/... ./pkg/security/... ./internal/agent/... ./internal/config/... ./internal/daemon/...` - green.
3. New e2e suites in isolation (`make e2e-fast-area AREA=<name>`) - green.
4. Scratch-daemon live gate: boot scratch MEEPT_HOME with a groups config +
   one grouped agent; assert via live RPC: explain output correct, mutation by
   non-privileged agent denied, unsigned cluster dispatch rejected, grouped
   agent's tool calls obey deny rules with C2 log lines present.
5. `make graphs` + `make graphs-check` fresh; `make docs-generate` if gendoc
   drifted; `scripts/config-drift-check.py` classified.

## Test Gate

Test leaf: 00T-e2e-agent-groups.md - dispatched in Phase 0, validated by the
orchestrator in-session. Branches 01 and 03 carry their own gate leaves.

| # | Feature | Source | E2E test | Status |
|---|---------|--------|----------|--------|
| F1 | Grouped agent deny-beats-allow on tools | C3, 04 | agent-groups suite: denied tool absent from LLM defs AND uncallable | PENDING |
| F2 | Ungrouped agent backward compat (D7) | C3 | agent-groups suite: no-groups agent unchanged | PENDING |
| F3 | Assignment-time validation rejects dangling tool | C6 | agent-groups suite: RPC reject + boot reject | PENDING |
| F4 | Membership mutation gated to agent-update/admin | C5 | agent-groups suite: non-privileged deny at RPC + file backstop | PENDING |
| F5 | Memory ceiling enforced | C7 | agent-groups suite: recall above ceiling returns no rows | PENDING |
| F6 | Fence as central stage (fenced write by a tool with no opt-in) | C1 | agent-groups suite | PENDING |
| F7 | Decision log lines present with rule_source | C2 | agent-groups suite (log capture) | PENDING |
| F8 | explain/check surfaces | R4/R5 | groups-diagnosis suite | PENDING |
| F9 | Unsigned cluster dispatch rejected | C9 | cluster member | PENDING |
| F10 | Overrides expire | C8 | groups-diagnosis suite (clock-injected) | PENDING |

Statuses: PENDING | DELTA (resolution in progress) | VALIDATED
Delta log: (empty - no validations yet)

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as dated
addenda. Never edit a dispatched leaf document mid-execution.

## Notes

- Issue #70 tree interplay: that tree's branch 01 (memory scoping) and this
  tree's leaf 05 overlap ONLY at the seam C7 defines. If #70 branch 01 has not
  landed when group C dispatches, leaf 05 is BLOCKED on it - do not fork the
  scoping primitives. Conversely leaf 05 does NOT wait for #70's other leaves.
- Issue dependencies: #77 resolved by branch 01; #76 (employee auth identity)
  is a DEPENDENCY of C4 user-group resolution under multiuser - if #76 is not
  fixed, user groups resolve only for single-user installs and 03-membership/02
  must note the limitation in docs rather than derive identity from key strings.
- Known dead code adjacent (do NOT absorb silently): InjectContextBounded
  (internal/agent/conversation.go) - separate cleanup, flag if touched.
- The empty-allowlist legacy behavior (executor.go NewFilteredToolRegistry:
  empty allowed = allow-all) is load-bearing for UNGROUPED agents (D7) - the
  fix is at the RESOLUTION layer (a grouped agent always yields a non-empty
  decision record), never by changing FilteredToolRegistry semantics globally.
