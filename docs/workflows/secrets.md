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

## Egress Proxy

The egress proxy is the network boundary where `MEEPT_SECRET:<name>`
placeholders become real credentials. It is a loopback-only reverse proxy:
requests routed through it are scanned for placeholders in headers and in
`text/*` / `application/json` bodies (first 1 MiB). When the destination host
matches the secret's declared `hosts` suffix list, every placeholder
occurrence is replaced with the `format`-applied real value **in place**.
Requests toward non-allowlisted hosts pass through completely unmodified and
increment the `secrets.leak_attempt` counter; unknown secret names behave the
same way.

Safety rules:

- **Loopback enforced.** The proxy refuses to bind any non-loopback address —
  misconfiguring `listen` to a routable interface is a hard startup error,
  not a warning. Resolved credentials must never leave the machine unencrypted.
- **Chunked requests rejected.** Requests using `Transfer-Encoding: chunked`
  get `400`; body scanning requires content framing.
- **Fail closed on mixed placeholders.** If one placeholder in a value targets
  a non-allowed host, the whole value passes through untouched (no partial
  injection).
- **Real values never logged.** Log lines carry secret *names* and destination
  hosts only.
- CONNECT/TLS interception and SOCKS are out of scope; the proxy forwards
  plain HTTP only.

### Configuration

```json5
[secrets.proxy]
enabled = true            // default false
listen  = "127.0.0.1:0"   // default: loopback, ephemeral port; MUST be loopback
```

When enabled, the daemon logs the bound address at startup (`secrets egress
proxy started`) and reports it over RPC status as `secrets_proxy.addr`,
alongside `secrets_proxy.leak_attempts`. Wire shell profiles or tools with:

```bash
export MEEPT_SECRETS_PROXY=$(meept status | jq -r .secrets_proxy.addr)
```

### Example

With a source declared as in [Configuration](#configuration) above
(`gh_token`, hosts `["github.com"]`, header `Authorization`, format
`Bearer {}`), send a request whose header carries the placeholder through the
proxy:

```bash
# Placeholder goes in...
curl -x "http://$MEEPT_SECRETS_PROXY" \
     -H "Authorization: MEEPT_SECRET:gh_token" \
     https://api.github.com/user

# ...and github.com receives:
#   Authorization: Bearer ghp_realtokenvalue
#
# The same request aimed at any other host would arrive with the
# placeholder untouched and bump secrets.leak_attempt.
```

A JSON body variant works identically: POSTing
`{"note": "hello MEEPT_SECRET:gh_tok"}` with `Content-Type: application/json`
through the proxy delivers `{"note": "hello ghp_realtokenvalue"}` to an
allowlisted host.
