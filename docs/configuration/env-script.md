# The Env Resolver Script (`env`)

How the daemon resolves `${VARIABLE}` references in its configuration — chiefly
`apiKey` values in `~/.meept/models.json5` — when the variable is not set in
the daemon's own process environment.

## Resolution chain

For every dollar-brace reference (`${VAR}` or `$VAR`) found at config
expansion time, in order:

1. **Process environment.** Whoever started the daemon wins. This is the
   existing behavior and is unchanged: when the variable is present in the
   daemon's environment, the script below is never invoked.
2. **The env script.** The executable file named `env` directly under the
   meept home (`$MEEPT_HOME`, else `~/.meept`). The daemon invokes it with
   exactly one argument — the variable name — and reads the value from
   stdout. Exit code 0 = found (stdout, one trailing newline trimmed, is the
   value); any nonzero exit = not found.
3. **Not found.** The value is empty and the existing loud diagnostics fire,
   naming the variable (`has_api_key=false`, "config references undefined
   environment variable"). Diagnostics name VARIABLES only; values are never
   logged.

Each invocation is bounded by a **5 second timeout**. A script that hangs (or
whose child hangs) is killed and counts as not-found, with a warning naming
the variable. Results are memoized per boot: each variable name runs the
script at most once per daemon process.

Only variables the configuration actually references are ever looked up.

## The script contract

```
<meepthome>/env <VARIABLE_NAME>     # value on stdout, exit 0; exit nonzero = not found
```

- Mode **0700**. The daemon refuses a non-executable script (warning +
  behaves as if absent) and warns when the mode is wider than 0700.
- POSIX sh. The script is user-owned, user-editable, and opaque to the
  daemon.
- The daemon never syncs it, never rewrites it, and never logs its output.

## Writing pins

The shipped default script (copied into place by `make config-bootstrap`,
mode 0700, never overwritten once it exists) has two sections:

**1. Explicit pins (top of the file, take precedence over everything below).**
Pin a value directly when you want it decoupled from your shell startup:

```sh
case "$1" in
  GALA_API_KEY) echo "your-key-here"; exit 0 ;;
esac
```

**2. Shell-profile sourcing.** If no pin matches, the script asks your shell
profiles where the key actually lives: `~/.zprofile` + `~/.zshrc` via a real
`zsh` child (zsh syntax must be interpreted by zsh, never dot-sourced by
sh), then `~/.bash_profile` + `~/.bashrc` via a `bash` child, then a POSIX
fallback that sources only POSIX-compatible profile fragments. The first
source that yields a non-empty value for the requested variable wins; the
script exits 1 (not found) when nothing provides it.

Add a pin by inserting one `case` arm. Keep the argument handling intact:
the script refuses argument values that are not plain `[A-Za-z0-9_]` names.

## Why config sync must never ship it

`<meept home>/env` is **executable code the daemon runs at every boot**. If
config sync (or any repo-driven sync) could place it, anyone who can push to
the synced repository could execute arbitrary code on every node at daemon
start: remote code execution by config. The sync path therefore refuses it
unconditionally:

- `internal/config/merger.go` refuses to apply any file named `env` to the
  meept home root (logged as a loud WARN, recorded in `files_skipped`).
- `scripts/install-sync.py` never ships it (it is not under
  `config/{skills,agents,prompts}`) and prints a warning if an executable
  `env` appears in the target home so a manual mistake or a rogue writer is
  visible.

Edit the script locally, by hand. There is no supported remote path for
changing it.
