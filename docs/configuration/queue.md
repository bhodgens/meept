# Queue Configuration

The `[queue]` section controls the job queue: where jobs persist, how
failed jobs retry, and how interactive work is prioritized over
background work.

## Configuration File

Queue settings are configured in `~/.meept/meept.json5` under the `queue` section:

```json5
{
  queue: {
    db_path: "~/.meept/queue.db",
    max_retries: 3,
    interactive_window: "5m",
  },
}
```

## Configuration Options

### db_path
- **Type**: string
- **Default**: `~/.meept/queue.db`
- **Description**: Path to the SQLite database where queued jobs persist

### max_retries
- **Type**: int
- **Default**: `3`
- **Description**: How many times a failed job is retried before it is marked failed

### interactive_window
- **Type**: duration string (e.g. `5m`, `30s`, `1h`)
- **Default**: `5m`
- **Description**: Recency window for the session interactivity signal
  (DECISIONS.md D11). A session counts as *interactive* when it received a
  user message within this window, or when the client holds it in the
  foreground (`session.set_foreground`). Work enqueued from an interactive
  session is claimed ahead of background jobs by the queue's claim
  ordering. Unparseable or non-positive values fall back to the default.
  See `docs/workflows/job-scheduling.md` ("Claim Ordering") for the full
  ordering and stamping semantics.

## Notes

- Chat turns themselves bypass the queue (they dispatch directly), so this
  window affects only queued work (planner/analyst/project tasks).
- The interactive classification is stamped onto a job at enqueue time and
  does not expire mid-life if the session goes quiet.
- JSON5 note: quote the duration value (`"5m"`, or escape as `"\u0035m"`).
  An unquoted `5m` is rewritten to a nanosecond integer by the config
  loader and rejected for this string-typed field.
