# backfill-evolver-plans

One-shot migration tool: inserts plan rows into the evolver plan sink store
(`~/.meept/plans-evolver.db`) for the historical plan markdown files in
`~/.meept/plans/evolver/`. Those files predate the sink store (plan-sink leaf
01 of the skill-evolver-proposal-lifecycle tree), so without rows they are
invisible to `meept plans list/show/approve` even though the RPC handler now
falls back to the sink store.

## Usage

```bash
go run ./cmd/backfill-evolver-plans            # dry run
go run ./cmd/backfill-evolver-plans -apply     # insert rows
```

## Behavior

- Each `*.md` file becomes a `pending_approval` row; `plan_id` comes from the
  file's Meta block, FilePath points at the real file (the approval actuator
  reads provenance from the file, so approval works once the row exists).
- Idempotent: ids already present are skipped. Files without a `plan_id`
  (e.g. the decision-framework gap-fill plan) are skipped — file-only by
  design.
- Config knobs: none (uses `-db`/`-dir` flags; defaults match the daemon's
  `skills.evolver.plan_dir` default).

## Edge cases

- Unique-violation on insert is treated as already-present (skip), not an
  error — safe to re-run.
- Exited non-zero when any insert fails for a non-duplicate reason.
