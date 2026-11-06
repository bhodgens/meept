# Agent Groups: Policy Binding Layer

`internal/groups` implements a (subject) -> (policy) -> (scope) binding model
on top of the enforcement gates that already exist: the dispatch-time tool
filter, the tool-call-time permission check, and (declared, not yet enforced)
the retrieval-time memory filter. Plan tree:
`docs/plans/20261030-agent-groups/`.

One mental model: a **policy** is a named bundle of access rules, a
**binding** attaches a policy to a subject, and resolution merges every
policy bound to a subject's memberships into a single `Resolved` record that
all gates read. Resolution is deterministic and pure (`groups.Resolve`); the
gates never re-implement precedence.

```
policy (named bundle: tools_allowed / tools_denied, memory_max_scope,
        fs_roots, network_class, allowed_models)
   ^
   | binding (subject: agent:<id> | group:<name> | user:<id>,
   |          policy, scope: daemon | project:<id>)
   |
subject = agent (AGENT.md frontmatter `groups:`)
          or group (ONE level of nesting)
          or human user (users.json5 `groups:`)
```

## Concepts

### Policy

A named bundle (config type `GroupsPolicyConfig`,
`internal/config/schema.go`):

| Field | Meaning |
|-------|---------|
| `name` | Unique identifier; bindings reference it. |
| `tools_allowed` | Registered tool names the policy grants (grant spellings normalize to registered names: `shell_execute` resolves to `shell`). |
| `tools_denied` | Registered tool names the policy revokes. Deny beats allow. |
| `memory_max_scope` | Memory retrieval ceiling: `""` (unrestricted), `project`, `user`, or `global`. Narrowest wins across groups. |
| `fs_roots` | Additional path-fence roots for members (additive: write inside any listed root). |
| `network_class` | `""`/`unrestricted`, `allowlist`, or `none`. Most restrictive wins. |
| `allowed_models` | Model aliases the subject may use; empty = unrestricted. |

### Binding

One row: `subject` (`agent:<id>`, `group:<name>`, or `user:<id>`), `policy`
(must exist), `scope` (`daemon` or `project:<id>`; an empty scope is
diagnosed as `daemon`).

### Subjects and membership

Membership lives on two surfaces (decision D1):

- **Agents**: `groups:` in the `AGENT.md` frontmatter. Scalar-replacement
  semantics: discovery shadowing takes the higher tier's list outright
  (`model_alias:` precedent), never a union with the shadowed base.
- **Humans**: `groups:` on the `User` record in the users store
  (`internal/auth/users.go`). Manage with `meept users add --groups` and
  `meept users update --groups`.

An agent's **effective groups** = frontmatter groups UNION the session
owner's stored user groups, joined only when the request carries a
store-validated auth identity (`groups.EffectiveGroups`); a nil identity
(single-user install) contributes nothing. The result is sorted and
de-duplicated, so resolution is reproducible across calls.

### Nesting: exactly one level

A binding subject `group:X` selects its policy for every member of X. X is
never itself re-expanded as a subject: group-of-group is not supported. The
derived group-containment graph is checked at boot; a cyclic membership is a
named config defect and fails boot.

### The `default` policy

`default` is a reserved policy name. When `security.groups.default_managed`
is true, every agent with NO matching binding routes through it, fail-closed
(decision D7/D8). A missing `default` policy under default management
resolves to a deny-everything record with `network_class: "none"`.

## How enforcement works: the three gates

### Gate 1: dispatch-time intersection

`AgentRegistry.filterTools` (`internal/agent/registry.go`) intersects the
spec's grant set with the resolution record:

```
dispatch-visible = spec grants INTERSECT union(group allows) MINUS union(group denies)
```

- A binding **never widens** the spec: a tool the agent spec does not grant
  is never added by a group.
- A managed record with an EMPTY effective allowlist means DENY-ALL (D8): an
  explicit deny-all marker keeps `FilteredToolRegistry`'s legacy
  empty-means-all behavior from firing.
- Ungrouped/unmanaged agents (D7, default) are byte-identical to the legacy
  spec-allowlist posture.

### Gate 2: tool-call-time check

`Executor.checkPermission` runs the base permission pipeline first, then
`applyGroupGate` (`internal/agent/executor.go`). Precedence at this gate:

1. **Base pipeline verdict wins when it denies.** A deny from the employee
   constitution gate (`employee_pre_exec`), the fence (`fence`), the
   financial guard (`financial`), the groups file backstop
   (`groups_backstop`), or the confirmation gate (`confirmation_gate`) keeps
   its own `rule_source`; the group gate can only ever narrow.
2. **Group deny**: the tool is in the record's deny set -> denied
   (`group_deny`). This also catches the parent-registry bypass: a tool call
   that never went through the filtered dispatch registry still hits the
   deny set here.
3. **Managed allowlist is the ceiling**: a tool outside the record's
   allowlist -> denied (`group_deny`). Under an active policy this is where
   D8 (empty allowlist = deny-all) bites at call time.
4. **Inside the ceiling** -> allowed, stamped `group_allow`.
5. **Unmanaged record** -> the gate skips entirely (D7 legacy posture).

Baseline platform/task tools (the spec's `BaselineTools`: memory store/
search, task tools, platform status, handoff, `project_info`,
`delegate_task`) stay reachable because the record was intersected against
the full grant set that includes them.

### Gate 3: retrieval-time memory ceiling (enforced)

`memory_max_scope` resolves (narrowest wins) into the `Resolved` record and
IS enforced at recall time (leaf 05, C7): the loop folds the ceiling into
its scope resolution (`EffectiveMemoryScope` — the narrower of the agent's
own declaration and the group ceiling, order project < user < global), so
`buildMemoryContext` recall AND the four memory tools
(`memory_store`/`memory_search`/`memory_recall`/`memory_get_context`,
which filter and stamp from the loop-injected identity) all operate at the
effective ceiling. The dispatcher chat lane applies the same narrowed
resolution. Groups NEVER widen: a ceiling wider than the agent's own
`memory_max_scope` is inert, and an ungrouped agent resolves exactly as
the memory-scoping baseline. The gate is pinned by the agent-groups-07 e2e
scenario and the `internal/agent` ceiling pins.

The same caution applies to the remaining policy fields: `network_class`
and `allowed_models` resolve and merge (most-restrictive / intersection
respectively) and surface through `meept groups explain`, but no runtime
seam enforces them yet.

### Where the fence and the decision log fit

- **Fence as a stage**: `pkg/security.PermissionChecker` runs a
  `FenceStage` check (file paths + shell command/workdir) after path globs
  and before confirmation gating. Every session-scoped loop gets a fresh,
  session-owned fence rooted at the session working directory. On top of
  that, a managed resolution with `fs_roots` arms a `GroupFenceStage`
  (additive: the subject may act inside ANY group root, nothing outside),
  composed with the session fence via `UnionFence` (first denial wins).
- **Groups file backstop** (`groups_backstop`): when
  `default_managed=true`, non-admin agents are denied file/shell access to
  the membership surfaces: the users store, `<home>/agents/**`, and the
  daemon main config. Runs after the fence, before confirmation gating
  (a deny never degrades into a confirmation prompt). Admin agents
  (`security.groups.admin_agents`) are exempt. The file-tool lane is the
  hard guarantee; the shell lane is a best-effort probe.
- **Decision log** (C2): one structured line per permission verdict at the
  executor seam. See "Decision log anatomy" below.

## Precedence rules (frozen) with worked examples

Resolution order inside `resolveLayered`:

1. **Denies first** (deny beats allow, D4).
2. **Allows merge by union, then intersect with the spec** (a binding never
   widens the spec, D4).
3. **Ceilings merge most-restrictive-wins** (narrowest `memory_max_scope`,
   most restrictive `network_class`).
4. **`fs_roots` union** (additive fence roots); **`allowed_models`
   intersection** when more than one group constrains models.

Overrides (below) add allows only; deny still beats an override and the
spec stays the ceiling.

### Example: deny beats allow

```json5
{ "name": "coders",
  "tools_allowed": ["file_read", "file_write", "shell"],
  "tools_denied": ["shell"] }
```

Spec grants `file_read`, `file_write`, `shell`. Result: `shell` is denied
even though `tools_allowed` lists it. `groups explain` attributes it:
`shell  deny  coders`.

### Example: intersection never widens

Spec grants `file_read`, `file_write`. The agent's group allows
`file_read, web_search`. Result: allowed = `file_read` only.
`web_search` is NOT added: the group allow is an upper bound on the spec,
never a source of new grants.

### Example: most-restrictive-wins ceilings

An agent belongs to groups A and B:

- Policy A: `memory_max_scope: "user"`, `network_class: "unrestricted"`
- Policy B: `memory_max_scope: "project"`, `network_class: "none"`

Resolution: `memory_max_scope: "project"` (narrowest),
`network_class: "none"` (most restrictive). Groups never widen each other.

### Example: D8 empty allowlist is deny-all

```json5
{ "name": "quarantine", "tools_allowed": [] }
```

An agent bound to `quarantine` is managed with an empty allow union: every
tool is denied at dispatch AND at call time. The legacy
empty-allowlist-means-all behavior never fires for a managed subject.
`groups check` reports `managed_empty_allowlist` /
`agent_denied_everything` so the surprise is named.

### Example: D7 ungrouped compatibility

With `default_managed: false` (the default), an agent with no groups and no
direct binding resolves unmanaged: spec allowlist only, legacy behavior
byte-identical, and the call-time gate skips. Setting
`default_managed: true` flips those agents to the fail-closed `default`
policy route.

### Example: overrides add, never rescue

Policy denies `git_push`; an override grants `git_push` to the agent.
Result: still denied. An override can only add a tool the policies neither
allow nor deny (and that the spec already grants). Deny beats override;
the spec is the ceiling.

## How to configure

### 1. The `security.groups` config section

The schema home is `[security.groups]` in `~/.meept/meept.json5`. The
shipped template (`config/meept.json5`) carries the same block commented
out; empty tables with `default_managed=false` are identical to having no
block at all.

```json5
{
  "security": {
    "groups": {
      "default_managed": false,          // true = route unbound agents through "default"
      "decision_log_level": "deny-warn", // "deny-warn" | "all" | "off"
      "admin_agents": ["agent-update"],  // agent ids that may mutate membership
      "admin_users": [],                 // human user ids that may mutate membership
      "policies": [
        {
          "name": "coders",
          "tools_allowed": ["file_read", "file_write", "shell"],
          "tools_denied": [],             // deny beats allow
          "memory_max_scope": "project",  // "" | "project" | "user" | "global"
          "fs_roots": [],                 // extra path-fence roots
          "network_class": "",            // "" | "unrestricted" | "allowlist" | "none"
          "allowed_models": []            // model aliases; empty = unrestricted
        },
        // The special "default" policy applies to unbound agents when
        // default_managed=true (fail-closed).
        // { "name": "default", "tools_allowed": ["file_read"] },
      ],
      "bindings": [
        { "subject": "group:coders", "policy": "coders", "scope": "daemon" },
        { "subject": "agent:coder", "policy": "coders", "scope": "daemon" }
      ]
    }
  }
}
```

**Top-level alias**: a top-level `"groups": { ... }` key is promoted into
`security.groups` at load. An existing `security.groups` section wins
(explicit nesting is authoritative); the alias only fills an absent section.

Full key reference: [docs/configuration/agent-groups.md](../configuration/agent-groups.md).

### 2. Agent frontmatter (`AGENT.md`)

```yaml
---
id: coder
name: Code Specialist
role: executor
groups:
  - coders
additional_tools:
  - file_read
  - file_write
---
```

### 3. Human users (`users.json5`)

```bash
meept users add "Dee Ops" --groups coders,oncall
meept users update usr_01ABC --groups coders     # replaces wholesale
meept users update usr_01ABC --groups ""         # clears membership
```

### 4. Overrides file

`$MEEPT_HOME/group_overrides.json5` (see the Overrides section).

## Membership governance (who may mutate)

Group membership is mutated ONLY through the daemon's `groups.assign` /
`groups.remove` RPC. The write gate (C5) accepts exactly two caller kinds:

1. **Admin agent**: a kernel-verified daemon-descendant socket peer whose
   declared agent id is in `security.groups.admin_agents`
   (default `["agent-update"]`). A `caller_agent_id` from any other peer is
   self-declared identity and is REJECTED.
2. **Admin user**: a human API key whose STORE-validated identity resolves
   to a user id in `security.groups.admin_users`. Identity is never derived
   from raw key strings.

An unverifiable caller (no identity, no daemon-descendant peer) is denied.
Single-user installs with no human admin configured are deny-by-default.

A denied attempt fails with:

```
groups mutation denied: caller is neither a groups admin agent nor an
admin user (denied at the seam)
```

plus a non-credential caller attribution (peer pid/daemon-descendant flag,
store user id, declared agent id).

Every successful mutation: (a) runs assignment-time tool validation (C6)
against the live registry BEFORE persisting (a policy naming an unregistered
tool is rejected, nothing is persisted); (b) lands one hash-chained audit
record (`internal/auditlog`); (c) persists the binding to the config file
and the label membership to the subject's native store (agent frontmatter
via a parse round-trip writer, users via the auth store).

**File backstop**: with `default_managed=true`, the same surfaces
(users store, `agents/**`, main config) are denied to non-admin agents at
the tool-call gate (`groups_backstop` rule source), so file-write and shell
bypass attempts hit a hard deny.

## Diagnosing permissions

### `meept groups list`

Policy and binding tables plus the `default_managed` flag, from the live
daemon over RPC. `--json` prints the raw RPC result with stable keys:
`policies`, `bindings`, `default_managed`, `admin_agents`, `admin_users`.

### `meept groups explain <agent>`

Per grant/deny attribution: WHICH binding and policy produced each entry.

```
meept groups explain coder
meept groups explain coder --json
```

Human output shows: the agent's effective groups, the managed flag, the
allowed/denied sets, a per-tool attribution table
(TOOL / EFFECT / POLICY / BINDING / SCOPE / NOTE), and every selecting
binding (including the synthetic `default_managed routed` row when D7
routing applied). `--json` keys (stable, pinned by tests):
`agent_id`, `groups`, `managed`, `allowed_tools`, `denied_tools`,
`grants[]` (each: `tool`, `effect`, `policy`, `binding_subject`,
`binding_scope`, `reason`), `bindings[]` (`subject`, `policy`, `scope`,
`note`), `resolved` (the full resolution record).

Explain reads the same `groups.Resolve` core the gates enforce; it never
re-implements precedence.

### `meept groups check`

Config defect enumeration over the live daemon. Exit codes: 0 clean, 1
defects found (scriptable). `--json` keys: `ok`, `defect_count`,
`defects[]` (`code`, `name`, `detail`), `agents_checked`,
`overrides` (`active`, `expired`, `total`, `note`). Overrides are loaded
per request, so operator edits to the overrides file are visible without a
restart.

### `meept doctor`

Three report-only rows, powered by the same check engine:

| Row | Meaning |
|-----|---------|
| `groups-config` | One line per structural defect, or a "groups config valid (N policies, M bindings, K agents evaluated)" summary. |
| `groups-permissions` | Dispatch-divergence posture. The CLI process cannot see the live registry, so this reports the evaluated posture and points at `meept groups check` for the live answer. |
| `groups-overrides` | Active/expired override counts; warn-flagged when any expired row exists. |

### Decision log anatomy

Every permission verdict logs one structured line at the executor seam:

```
level=INFO msg="tool permission decision" decision=allow|deny
  action=<action> tool=<tool> agent=<agent_id> groups=<comma,list|->
  rule_source=<source> risk=<risk> reason="<short>"
```

- `groups` is `-` when the subject is ungrouped/unmanaged.
- Levels: `deny-warn` (default) denies at WARN and allows at DEBUG; `all`
  logs every decision at INFO; `off` suppresses entirely. Configure with
  `security.groups.decision_log_level`.
- Confirmation requests surface separately (`CheckResult.NeedsConfirm`);
  the logged decision value is `allow|deny` only.

`rule_source` vocabulary:

| Value | Stage | Live path |
|-------|-------|-----------|
| `base_rule` | Default allow after all stages passed | yes |
| `group_deny` | Group deny set or managed allowlist ceiling | yes |
| `group_allow` | Allowed inside the group ceiling | yes |
| `fence` | Path/command fence stage (session or group fence) | yes |
| `employee_pre_exec` | Employee constitution gate | yes |
| `groups_backstop` | C5 file backstop on membership surfaces | yes |
| `confirmation_gate` | Risk-based confirmation request | yes |
| `financial` | Financial-action guard | yes |
| `unknown_action` | Action category not recognized by the checker | yes |
| `override` | Override-table verdict (reserved by the C2 vocabulary; the live executor currently stamps `group_allow`/`group_deny` for override-fed allows) | vocabulary only |

### Known limitations (diagnosis)

1. **explain-over-RPC normalizer divergence.** The RPC explain path runs
   with a default (identity) tool-name normalizer while the daemon wiring
   comment reserves the registry-backed one and the check path falls back
   to the diagnosis vocabulary (`shell_execute` -> `shell`,
   `web_search_execute` -> `web_search`, `file_read_text` -> `file_read`,
   `file_write_text` -> `file_write`, `file_edit_text` -> `file_edit`).
   Aliased tool names may therefore attribute differently between `explain`
   and `check`/dispatch for policies written in grant spellings. Small fix
   candidate tracked in the plan tree follow-ups.
2. **Check operates on already-booted configs.** A config with a dangling
   policy reference fails BOOT (the C6/structural validation hook is
   fail-closed) before `groups check` can run against it. `check` is
   therefore the tool for a RUNNING daemon's config posture and for
   intent-checking edits, not for post-mortem on a config that kills boot;
   boot's own named-defect error is the diagnosis surface in that case.
   (This is also why the groups-diagnosis e2e scenario that injects a
   dangling policy carries a dated skip.)

## Overrides (time-boxed tool grants)

Overrides are operator-managed security exceptions in
`$MEEPT_HOME/group_overrides.json5` (owner-only 0600 on write). Entry shape
(C8):

```json5
[
  {
    "subject": "agent:coder",
    "allow": ["git_push"],
    "reason": "deploy window, ticket OPS-442",
    "expires": "2026-11-05T17:00:00Z"
  }
]
```

Semantics:

- **Add-only by type shape**: an entry has no deny field and no ceiling
  fields; it cannot express a denial or a memory/network widening.
- **Deny beats override; the spec is the ceiling.** An override adds a tool
  only when the policies neither allow nor deny it AND the spec grants it.
- **Expiry**: an entry is active while `now < expires`; expired once
  `now >= expires`. Expired entries are skipped at daemon load with ONE
  batch warning. Overrides are REQUIRED to be time-boxed: a missing or zero
  expiry is a malformed entry.
- **Malformed fails closed**: a malformed file or entry fails daemon boot
  with a named error (`malformed group override`); the CLI surfaces the
  same named error. No silent skips.
- **Clock discipline**: the reader never calls wall-clock itself. The
  daemon injects the clock, and `MEEPT_GROUPS_NOW` (RFC3339) pins it for
  tests and the e2e gate; a malformed pinned value fails loudly.
- **Subject coverage**: only `agent:<id>` subjects are consumed by the
  resolution seam today. `group:`/`user:` subject entries load, list, and
  count in check/doctor, but no gate acts on them yet (documented
  limitation).

CLI:

```bash
meept groups override add agent:coder --allow git_push \
    --reason "deploy window, ticket OPS-442" --expires 2026-11-05T17:00:00Z
meept groups override list            # active only
meept groups override list --all      # include expired
meept groups override prune           # remove expired rows from the file
```

All three accept `--file` to target a non-default overrides file. `add`
requires `--allow` and `--reason` (the file is a reviewable artifact; an
unexplained grant is rejected) and `--expires` (RFC3339).

## Edge cases

The eight R5 classes, each reported by `meept groups check` with a stable
defect code:

| Edge case | Defect code | Behavior |
|-----------|-------------|----------|
| Cyclic group membership (a->b->a) | `cyclic_group` | Fails boot with a named defect; check reports it. |
| Binding references a missing policy | `dangling_policy` | Fails boot; resolution degrades by skipping the unresolvable binding. |
| Policy names an unregistered tool | `unregistered_tool` | Fails boot (live-registry validation) and is rejected at `groups.assign` time (C6). |
| Unknown group in agent frontmatter | `unknown_group` | Membership with no policy effect; check reports it (fail-safe, logged). |
| Policy denies a tool it also allows | `deny_allow_conflict` | Legal: deny wins; reported so the author knows the allow is dead config. |
| Managed + empty allowlist | `managed_empty_allowlist`, `agent_denied_everything` | Deny-everything for the subject (D8), never legacy empty=all. |
| `default_managed=true` with unbound agents | `agent_default_managed` | Agent routes through `default` (fail-closed, reported not fatal); no `default` policy defined also yields `managed_empty_allowlist` plus a resolve-time deny-everything record with `network_class: "none"`. |
| Overrides active/expired | `active_override`, `expired_override`, `overrides_reader` | Counted through the reader seam; expired rows warn; a reader error surfaces as a named finding. |

Structural classes also reuse the frozen core vocabulary:
`duplicate_policy`, `empty_policy_name`, `bad_subject`, `bad_scope`,
`duplicate_binding`.

## Limitations summary

- `memory_max_scope` is declared and resolves but is NOT enforced (leaf 05
  blocked on the memory-scoping branch, issue #70 tree). Do not treat it as
  isolation.
- `network_class` and `allowed_models` resolve and merge into the record but
  have no enforcement seam yet.
- Overrides on `group:`/`user:` subjects are visible but not consumed by any
  gate.
- The file backstop's shell lane is a best-effort probe (variable expansion
  can bypass it); the file-tool lane is the hard guarantee.
- explain-over-RPC normalizer divergence on aliased tool names (above).
- No peer-key distribution path for cluster-wide group sync yet
  (`MergeForeign` group-sync is follow-up territory).

See also: [configuration reference](../configuration/agent-groups.md),
opt-in defaults [D7/D8](../agents/opt-in-defaults.md), plan tree
`docs/plans/20261030-agent-groups/master.md`.
