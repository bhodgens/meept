# Secrets Broker

Declarative secret management for agent-run child processes. Secrets are
declared once in configuration; children (shell commands, MCP server
subprocesses) receive only placeholder tokens — plaintext never enters a
child environment.

## How It Works

Declare sources under `[secrets.sources]` in `meept.json5`. Each source has:

- `kind` — `"env"` (read from the daemon's environment at startup) or `"file"`
  (read from a path; trailing newline trimmed)
- `hosts` — host suffixes the egress proxy may inject this secret toward
  (consumed by the proxy stage)
- `header` / `format` — how the value is formatted when injected, e.g.
  `header = "Authorization"`, `format = "Bearer {}"`

The broker eager-loads every source when the daemon starts. A missing env var
or unreadable file produces one aggregated startup error naming every failure.

Children see the placeholder token `MEEPT_SECRET:<name>` wherever the secret
is referenced. The environment allowlist (`[runtime] env_policy`) always
passes placeholder values through deny-globs. The egress proxy (separate
component) swaps placeholders for real values on matching outbound requests.

## Configuration

```json5
[secrets]
[secrets.sources.gh_token]
kind   = "env"
name   = "GITHUB_TOKEN"
hosts  = ["github.com"]
header = "Authorization"
format = "Bearer {}"

[secrets.sources.db_url]
kind  = "file"
name  = "~/.config/meept/db-url"
hosts = ["db.internal.example"]
```

MCP server entries can reference secrets without receiving them:
`"${secret:name}"` in an env value substitutes to the `MEEPT_SECRET:name`
placeholder at launch; other `${VAR}` forms pass through unchanged.

## Edge Cases

- Unknown names error on lookup; known names always resolve to the exact
  placeholder string.
- Plaintext is never logged — the broker logs names and kinds only.
- Values live in broker memory only; there is no persistence layer.
