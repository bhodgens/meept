# Change Journal (Revert)

Every accepted staged file change is recorded in a SQLite change journal so it
can be reverted later with `meept changes`. This closes the "no undo short of
git checkpoints" gap in the staged-change workflow (see
[adversarial-input-defense.md](adversarial-input-defense.md) for the staging
and fence model).

## How it works

1. **Stage** — `file_edit` / `write_file` / `stage_write` register a pending
   change holding the original content (pre-image) and its SHA256.
2. **Accept** — when the agent's `resolve` tool accepts the change, the daemon
   journals the entry *after* writing the modified bytes:
   - `pre_image` — the original bytes (capped at 1 MiB; larger pre-images are
     dropped and the entry becomes non-revertible)
   - `post_sha` — SHA256 of the applied content
   - `change_ids` — the pending-change IDs that produced this entry
3. **Revert** — `meept changes revert <id>` restores the pre-image, guarded by
   a three-way checksum check (below).

Storage: `<state-dir>/changes.db` (default `~/.meept/changes.db`), WAL mode,
same SQLite conventions as the session store.

## Drift guard (three-way checksums)

Revert refuses to clobber files that changed after apply:

| Current file hash          | Behavior                                        |
|----------------------------|-------------------------------------------------|
| == `sha256(applied)`       | clean revert — write pre-image atomically        |
| == `sha256(pre-image)`     | already reverted — idempotent success, no rewrite|
| anything else              | **refused**: "file changed since apply"          |
| pre-image not journaled    | refused: "pre-image not journaled (size cap)"    |

Writes go through temp-file + rename, so readers never see partial content.
The path fence is consulted before any revert write when configured.

## CLI

### List applied changes

```console
$ meept changes list --session sess-abc123 --limit 10
id                     file                                     applied               size revertable
change-9f2c11a4b8d0e5  /home/me/project/main.go                 2026-08-25 14:02:11   4.2 KiB yes
change-3b7d90cc21e64f  /home/me/project/README.md               2026-08-25 13:58:40   812 B   yes
change-77a01f6e5d3c98  /home/me/project/vendor/huge.min.js      2026-08-25 13:51:02   0 B     no (size cap)
```

Columns are lowercase (`id`, `file`, `applied`, `size`, `revertable`). Entries
marked `no (size cap)` had a pre-image larger than `max_entry_bytes` (default
1 MiB) and cannot be reverted.

Machine-readable form:

```console
$ meept changes list --json | jq '.[0]'
{
  "id": "change-9f2c11a4b8d0e5",
  "session_id": "sess-abc123",
  "file": "/home/me/project/main.go",
  "applied_at": "2026-08-25T14:02:11Z",
  "size_bytes": 4300,
  "revertable": true,
  "change_ids": ["stage-5c88aa01bb33ee22"]
}
```

### Revert one change

```console
$ meept changes revert change-9f2c11a4b8d0e5
reverted change-9f2c11a4b8d0e5 -> /home/me/project/main.go

$ meept changes revert change-77a01f6e5d3c98
change journal: /home/me/project/vendor/huge.min.js: pre-image not journaled (size cap)

$ meept changes revert change-3b7d90cc21e64f   # after manual edits to the file
change journal: /home/me/project/README.md: file changed since apply (applied b1946ac9… != current 4a7d1ed4…) — refusing to overwrite
```

`--json` on revert emits `{"id": ..., "file": ..., "reverted": true}` or an
`error` field, exiting non-zero on refusal.

Session-wide rollback is a loop of single reverts over `changes list --session`
(leaf 07 will surface this as a command); there is intentionally no
transactional batch revert in this tree.

## Configuration

Journal behavior follows defaults; no config keys are required:

| Key             | Default               | Meaning                              |
|-----------------|-----------------------|--------------------------------------|
| `db_path`       | `<state-dir>/changes.db` | SQLite database location           |
| `max_entry_bytes` | `1048576` (1 MiB)   | Pre-images larger than this are not stored |

## Tooling API

`internal/tools/builtin` exposes:

```go
journal, err := builtin.NewJournal(builtin.JournalConfig{DBPath: "~/.meept/changes.db"}, logger)
journal.Record(&builtin.JournalEntry{...})            // called by ResolveTool on accept
entries, err := journal.List(sessionID, limit)         // newest first, no pre-image bytes
path, err := journal.Revert(id, fenceChecker)          // checksum-guarded restore
```

`ResolveTool.SetJournal(*Journal)` is typed-nil guarded; without a journal the
accept path behaves exactly as before (journaling is best-effort and never
fails an accepted write).
