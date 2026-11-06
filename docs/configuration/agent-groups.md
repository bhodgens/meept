# Agent Groups Configuration Reference

Every key of the `[security.groups]` section of `~/.meept/meept.json5`,
verified against `internal/config/schema.go` (`SecurityGroupsConfig`,
`GroupsPolicyConfig`, `GroupsBindingConfig`). JSON5 primary; the legacy TOML
fallback uses the same keys. Related files:

- Agent membership: `groups:` key in `AGENT.md` frontmatter
  (`internal/agent/spec.go`, `AgentSpec.Groups`).
- User membership: `groups:` key on a user in the users store
  (`internal/auth/users.go`, `User.Groups`).
- Overrides: `$MEEPT_HOME/group_overrides.json5`
  (`internal/groups/overrides.go`).
- Feature guide: [docs/workflows/agent-groups.md](../workflows/agent-groups.md).

## security.groups

| Key | Type | Default | Since | Notes |
|-----|------|---------|-------|-------|
| `security.groups` | object | `{}` | 2026-10-30 agent-groups tree (leaf 02) | Schema home `SecurityGroupsConfig`. A top-level `groups` key is promoted here at load when no explicit `security.groups` section exists. |
| `decision_log_level` | string | `"deny-warn"` | leaf 02 (branch 01) | `"deny-warn"`: denies at WARN, allows at DEBUG. `"all"`: every decision at INFO. `"off"`: no decision lines. Invalid values fall back to `deny-warn` at load. |
| `policies` | array of policy objects | `[]` | leaf 02 | Named policy table. Unique names required. |
| `bindings` | array of binding objects | `[]` | leaf 02 | Attaches policies to subjects. Unknown policy reference fails boot validation. |
| `default_managed` | bool | `false` | leaf 02 | D7: when false, agents with no matching binding keep the legacy spec-allowlist posture. When true they route through the fail-closed `default` policy. Flipping is a product decision (see `docs/agents/opt-in-defaults.md`). Also arms the C5 file backstop. |
| `admin_agents` | array of string | `["agent-update"]` | leaf 02 (landed early for leaf 03) | Agent ids allowed to mutate membership via the `groups.*` RPC write gate. |
| `admin_users` | array of string | `[]` | leaf 02 (landed early for leaf 03) | Human user ids whose store-validated keys may mutate membership. |

## security.groups.policies[]

| Key | Type | Default | Notes |
|-----|------|---------|-------|
| `name` | string | required | Unique policy identifier; bindings reference it. |
| `tools_allowed` | array of string | `[]` | Registered tool names granted. Grant spellings normalize to registered names (`shell_execute` -> `shell`). |
| `tools_denied` | array of string | `[]` | Registered tool names revoked. Deny beats allow. |
| `memory_max_scope` | string | `""` | Memory retrieval ceiling: `""` (unrestricted), `project`, `user`, `global`. Narrowest wins across groups. DECLARED ONLY: retrieval-side enforcement is not wired yet (plan leaf 05 blocked); see the workflow doc. |
| `fs_roots` | array of string | `[]` | Additional path-fence roots for members (additive: allowed inside any listed root). |
| `network_class` | string | `""` | `""`/`unrestricted`, `allowlist`, or `none`. Most restrictive wins. Declared/resolved only today. |
| `allowed_models` | array of string | `[]` | Model aliases; empty = unrestricted. Intersection across constraining groups. Declared/resolved only today. |

## security.groups.bindings[]

| Key | Type | Default | Notes |
|-----|------|---------|-------|
| `subject` | string | required | `"agent:<id>"`, `"group:<name>"`, or `"user:<id>"`. |
| `policy` | string | required | Must name an existing policy. |
| `scope` | string | `"daemon"` | `"daemon"` or `"project:<id>"`. Empty renders as `daemon` in diagnosis surfaces. |

## Membership keys

| Key | Type | Default | Notes |
|-----|------|---------|-------|
| `groups` (AGENT.md frontmatter) | array of string | absent | Agent group memberships (C4). Scalar-replacement across discovery tiers. |
| `groups` (users store User record) | array of string | absent | Human group memberships (C4). Managed via `meept users add --groups` / `meept users update --groups` (wholesale replace; empty value clears). |

## Overrides file (`group_overrides.json5`)

Location: `$MEEPT_HOME/group_overrides.json5` (default
`~/.meept/group_overrides.json5`). Written 0600, atomic tmp+rename.

| Entry key | Type | Required | Notes |
|-----------|------|----------|-------|
| `subject` | string | yes | `"agent:<id>"`, `"group:<name>"`, or `"user:<id>"`. Only `agent:<id>` entries are consumed by gates today. |
| `allow` | array of string | yes | Non-empty; tool names granted ADDITIONALLY. Never a deny; never a ceiling. |
| `reason` | string | yes | Human justification; an unexplained grant is a malformed entry. |
| `expires` | string | yes | RFC3339 instant. Active while `now < expires`; expired at `now >= expires`. |

Environment: `MEEPT_GROUPS_NOW` (RFC3339) pins the decision clock for tests;
unset = wall-clock UTC; a malformed value fails loudly.

## CLI commands

| Command | Notes |
|---------|-------|
| `meept groups list [--json]` | Policy + binding tables + `default_managed` (over daemon RPC). |
| `meept groups explain <agent> [--json]` | Per grant/deny: binding + policy attribution. |
| `meept groups check [--json]` | Defect enumeration; exit 0 clean / 1 defects. |
| `meept groups override add <subject> --allow T1,T2 --reason R --expires RFC3339 [--file P]` | Add a time-boxed allow override. |
| `meept groups override list [--all] [--file P]` | Active overrides (`--all` includes expired). |
| `meept groups override prune [--file P]` | Remove expired entries. |
| `meept users add <name> [--groups g1,g2] [--store P]` | Create user with memberships. |
| `meept users update <id> [--groups g1,g2\|"" ] [--store P]` | Replace/clear memberships. |
| `meept doctor` | Report-only `groups-config` / `groups-permissions` / `groups-overrides` rows. |

RPC methods (for GUI/API consumers): `groups.list`, `groups.explain`
(`agent_id` param), `groups.check`, plus the gated write methods
`groups.assign` / `groups.remove`.
