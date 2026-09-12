# meept soul

Inspect the SOUL.md persona file.

## Synopsis

```bash
meept soul show
meept soul path
```

## Description

`~/.meept/SOUL.md` is the user-authored persona: plain markdown that shapes
how the agent responds. The daemon reads it at startup and re-reads it
whenever the file changes (hot reload, no restart). The text lands in the
cacheable STABLE personality slot of every system prompt, so an edit costs
exactly one provider cache miss.

**Startup behavior:**

- File missing → the daemon seeds the shipped default and continues.
- File present but invalid (empty, non-UTF-8, over 64 KiB, unreadable) →
  the daemon refuses to start and names the file and reason in the error.
  Delete the file or fix it to restore startup.

**Runtime behavior (while the daemon runs):**

- Valid change → reloaded within ~1 second; one `soul.md reloaded` info log
  line with the new sha256.
- Invalid change → the last accepted copy keeps serving; one `soul:
  rejected invalid change` error log names the reason. The daemon never
  degrades on a bad edit.
- File deleted → last copy keeps serving; recreation is picked up
  automatically.

Prose quality is never judged — "invalid" is mechanical only. A
stylistically bad SOUL.md loads fine.

## Subcommands

### show

Print the file content with its resolved path, sha256, and size. Warns on
stderr when the file is invalid (such a file would be rejected by the
daemon).

```bash
meept soul show
```

### path

Print the resolved file path. Honors `MEEPT_HOME`.

```bash
meept soul path
```

## Examples

```bash
# Where does the file live under a test home?
MEEPT_HOME=~/meept-test meept soul path

# Verify what the daemon last accepted
meept soul show

# Watch reloads live
tail -f ~/.meept/logs/daemon.log | grep soul
```

## Files

- `~/.meept/SOUL.md` — the persona file (seeded from the shipped default on
  first daemon start; never overwritten by meept afterward).

## See also

- `meept instructions` — automation rules (a different system: machine-acted
  rules, not persona).
- Employee constitutions (`internal/employee`) — per-employee tiered
  authority/enforcement, separate from the persona file.
