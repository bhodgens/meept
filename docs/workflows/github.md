# GitHub PR/CI Tools

`internal/githubsvc` is the GitHub service layer for the github-pr-ci tool
tree: token resolution, a per-token client pool, PR/CI status
summarization, and the schedule seam (`ReArmSink`) a parked CI wait hands
back to the daemon. The eight `github_*` agent tools themselves live in
`internal/tools/builtin` (github_pr.go, github_ci.go, github_issue.go).

## Problem

An agent that opens a pull request needs the rest of the loop: read CI
health, wait for green (or fast-fail on a red check), comment/review/merge,
and answer issues — without a human pasting `gh` output. Two hazards make
naive tooling wrong:

- **Irreversibility.** Opening a PR, posting a comment, submitting a
  review, and merging are external effects GitHub does NOT dedupe for
  you. A crash or parked-turn resume between "decided to merge" and
  "merge confirmed" must not merge twice.
- **Waiting.** CI takes minutes; an agent turn that blocks the whole time
  stalls the platform. The wait needs to be able to PARK and re-check on
  its own.

## Config

`[github]` in `meept.json5` (schema: `config.GitHubConfig` in
`internal/config/schema.go`). Disabled by default:

```json5
"github": {
  "enabled": true,
  "token_secret": "~/.meept/github-token", // token FILE (owner-only), or a
                                           // named secret in the [secrets] broker
  "default_owner": "octo",                 // optional; tools accept explicit args too
  "default_repo":  "cat",
  "base_url": "",                          // GitHub Enterprise override (https URL)
}
```

Token-source precedence (daemon: `internal/daemon/github_wiring.go`):

1. `token_secret` names an existing **file** (after `~` / `MEEPT_HOME`
   expansion) with owner-only perms → `githubsvc.FileTokenSource` (read at
   request time, whitespace-trimmed, missing/empty fails closed with
   `githubsvc.ErrNoToken`). A file with group/other access bits is
   **refused**, not used.
2. otherwise `token_secret` is a **named secret** in the `[secrets]` broker
   → `githubsvc.SecretsTokenSource`. No broker wired → the tools still
   register (membership) but fail closed at request time with an error
   naming the missing source.

The token itself never lives in the config file, and no code path prints
it: logs and `meept github status` report presence/length only.

## Tools

| Tool | Effects-wrapped | Notes |
|------|-----------------|-------|
| `github_pr_create` | yes | claim → POST /pulls → receipt (created PR number/URL) |
| `github_pr_comment` | yes | claim keyed on body SHA256 |
| `github_pr_review` | yes | event allow-list APPROVE/REQUEST_CHANGES/COMMENT |
| `github_pr_merge` | yes | key includes the head SHA fetched pre-claim; provider 4xx/409 records a failure receipt (no auto-retry) |
| `github_issue_comment` | yes | claim keyed on body SHA256 |
| `github_pr_status` | no | point-in-time PR + CI health snapshot (GET only) |
| `github_ci_wait` | no | polls until all green, a terminal check failure (fast-fail), or `timeout_seconds` (default 600, cap 3600) |
| `github_issue_get` | no | single-issue fetch (GET only) |

Effects-wrapped tools run through the `effects.Run` claim/execute/receipt
protocol (docs/workflows/effects.md); GitHub is provider-NOT-idempotent, so
a reconciled stuck record is surfaced for human reconciliation, never
auto-retried. When the tools register without an effects ledger (the daemon
degrades the ledger to memory-only on open failure), they still register —
membership only — and `Execute` fails closed with an error naming the
missing effects ledger instead of running the irreversible call ungarded.

## Parking and re-arming CI waits

`github_ci_wait` gives up at its timeout with `githubsvc.ErrCIWaitParked`:
the result carries a `wait_id` and the tool hands the re-check to its
`ReArmSink`:

- **Daemon sink** — `githubsvc.InstructionReArmSink` persists a
  user-instruction row in the preferences store:
  `Trigger: "cron:<M H * * *"`, `Action: "agent_trigger"`, and
  `ActionArgs` with a `prompt` naming the waitID and owner/repo/number.
  `InstructionScheduler.SyncCronInstructions` turns `cron:` rows into
  agent jobs; the daemon additionally re-syncs on a 30 s ticker
  (`Components.startGitHubReArmTicker`) so a park written mid-life is
  picked up without a restart.
- **No sink** (instruction subsystem absent) — parking still returns the
  parked result and its waitID; the `ScheduleReArm` call is skipped and the
  skip is logged. The operator resumes manually with the waitID.

## Registration — the factory seam

`builtin` imports `githubsvc` (pool, `OwnerRepo`, `ReArmSink`), so
`githubsvc` cannot construct the builtin tools directly. Registration is an
inversion of control:

- `githubsvc.ToolFactories` is a package-level factory variable, populated
  by `builtin`'s `init()`
  (`internal/tools/builtin/github_register.go`) with a closure building all
  eight tools.
- `githubsvc.RegisterGitHubTools(reg, opts)` (the entry point the daemon
  calls, gated on `[github].enabled` + `token_secret`) consults the factory:
  enabled case with an unlinked `builtin` → descriptive error; disabled
  case (nil `Pool`) → nil error, no registrations; never a panic.
- `RegistrationOptions` carries `Pool`, `DefaultOwner/Repo`, `WorkingDir`,
  `Ledger` (effects), and `Sink` (re-arm). The frozen gate test
  (`internal/tools/builtin/github_register_test.go`, F8) pins the enabled /
  disabled membership contract; `TestInstructionReArmSink` pins the sink's
  row shape.

New github tools are added to the builtin factory closure — never to a
githubsvc-side constructor list (that would recreate the import cycle).

## CLI

`meept github status` (report-only, doctor convention): whether `[github]`
is enabled, the token source's kind/presence (a token FILE is checked for
presence, length, and unsafe perms; a named secret is reported by name —
its value is never read or shown by the CLI), the default owner/repo and
base_url, and the static tool count. Exit 0 in every state: disabled and
missing-token are normal states, not failures.
