# Effects (External-Effect Idempotency Ledger)

`internal/effects` is a durable ledger of irreversible external effects
(publish, delete, send, push). Before the platform performs such an effect,
it atomically claims a stable key; after the provider confirms the effect, it
records a receipt and writes a completion marker. On daemon startup and
parked-turn resume, every claimed-but-incomplete record is reconciled —
auto-retried only when the provider accepts the same idempotency key,
otherwise surfaced for human reconciliation with `meept effects`.

## Why

A daemon crash or parked-turn resume between "the agent decided to push/send"
and "the effect was confirmed" can silently drop the effect or execute it
twice. Meept's existing mechanisms solve different problems: park/resume
dedups waiting turns, scheduler claims dedup job ownership — neither says
anything about whether the effect itself already ran. The ledger closes that
gap: the claim happens before the irreversible call, so a restart can always
tell "may not have run" from "definitely done".

## The protocol

Every wired tool wraps its external call in the `effects.Run` guard:

1. **Claim** — record the effect key in the ledger (`claimed`). The INSERT
   primary-key conflict is the atomicity mechanism: a second claim of the
   same key gets `granted=false` plus the prior record, never a second row.
2. **Execute** — run the irreversible call exactly once, and only if the
   claim was granted.
3. **Verify** — confirm the provider actually accepted the effect before
   treating it as done.
4. **RecordReceipt** — store the provider's outcome (`claimed` →
   `receipted`, `executed_at` stamped). The receipt JSON is preserved
   byte-for-byte.
5. **Complete** — write the completion marker (`receipted` → `completed`,
   `completed_at` stamped). From then on, any claim of the same key is an
   idempotent no-op returning the stored receipt — never a re-execution.

On an execute error the record stays `claimed` on purpose (reconcile finds
it); the tool decides whether the effect is permanently dead and should be
`Abandon`ed explicitly.

## Effect keys

Keys are content-derived, never generated: `EffectKey(parts ...string)` is
the sha256 hex of the parts joined with the unit separator (`\x1f`), each
part trimmed. Callers pass parts in a fixed documented order per tool, so
the same logical effect always derives the same key across restarts. Keys
are identities — the CLI prints them in full.

Example: `EffectKey("backup.git_push", "/repo", "abc123")` identifies "the
backup push of commit `abc123` in `/repo`" — pushing the same commit again
claims the same key and no-ops.

## Wired tools

| Tool | EffectKey order | Provider-idempotent | Reconcile behavior |
|------|-----------------|---------------------|--------------------|
| push notification (`internal/services` PushService) | tool, session, request digest | no — re-publishing would double-render in TUI/Telegram | surfaced for `meept effects reconcile` |
| backup git push (`internal/backup` gitPushWithRetry) | tool, repo path, head SHA | yes — pushing the same commit again is `AlreadyUpToDate` | auto-retried on startup/resume |

## Reconcile behavior

On daemon startup and at the top of every parked-turn resume, the daemon
runs `ReconcilePending` over the stuck `claimed`/`receipted` records
(`internal/daemon/effects_wiring.go`):

- tool registered a re-executor **and** its `EffectMeta.ProviderIdempotent`
  is true → the effect is re-driven through `effects.Run` (success:
  receipted + completed; failure: stays pending, logged).
- everything else → `slog` error with key, tool, and age; the record waits
  for `meept effects reconcile`. The agent is never prompted.

There are NO new bus topics: effect state is observed via this CLI and logs
only. Park/resume events continue to ride the existing `agent.quota_wait`
topic family.

## CLI

```console
$ meept effects list
KEY                                                                STATE     TOOL             CLAIMED              AGE
3f9a…(full 64-char key printed)                                    claimed   push.notify      2026-09-05 12:00:11  3h12m

$ meept effects reconcile <key>
key:                 3f9a…
state:               claimed
tool:                push.notify
provider_idempotent: false
claimed_at:          2026-09-05T12:00:11Z

confirm the effect's real outcome, then re-run with --complete [--receipt '<json>'] or --abandon --reason '<text>'

$ meept effects reconcile <key> --complete --receipt '{"delivered":true}'
$ meept effects reconcile <key> --abandon --reason 'provider confirms the message never sent'
```

`--complete` is the human's confirmation that the effect verifiably landed;
an optional hand-verified `--receipt` is recorded first (only while the
record is still `claimed`). `--abandon` requires a non-empty `--reason`; the
record stays in the ledger for audit. `list --json` returns the raw RPC
result indented; `--state` filters `claimed`, `receipted`, `completed`, or
`abandoned` (terminal states resolve only together with a key, since the
ledger surface enumerates pending records only).

## States

| State       | Meaning                                                            | Moves to |
|-------------|--------------------------------------------------------------------|----------|
| `claimed`   | claimed before execution; effect believed not-yet-run              | `receipted`, `completed`, `abandoned` |
| `receipted` | executed and provider outcome stored; completion marker not yet written | `completed`, `abandoned` |
| `completed` | done; later claims of the key are idempotent no-ops                | — |
| `abandoned` | dead (tool error path or human reconciliation); kept for audit     | — |

## Storage

SQLite at `<data_dir>/effects.db` (WAL + 5s busy timeout, same DSN
convention as the park store). Single table `effects_ledger`, `key TEXT
PRIMARY KEY` (the atomicity mechanism), with `state` (CHECK-constrained to
the four states), `task_id`, `step_id`, `session_id`, `tool`,
`provider_idempotent`, RFC3339Nano timestamps `claimed_at`/`executed_at`/
`completed_at`, and `payload`/`receipt` raw-JSON TEXT columns preserved
byte-for-byte. Index on `state` serves `ReconcilePending`.

## Adding a new effect tool

1. Derive a stable key: pick the tool's fixed part order and call
   `effects.EffectKey(toolName, ...)` (same inputs → same key, forever).
2. Wrap the external call in `effects.Run(ctx, ledger, key, meta, execute)`;
   `execute` must verify the provider accepted the effect before returning
   a non-nil receipt.
3. Call `Complete` after a successful `Run`.
4. Register a reconcile re-executor **only if** the provider accepts the
   same idempotency key (`ProviderIdempotent: true`); otherwise leave the
   record for human reconciliation.
5. Update this doc's wired-tools table.
