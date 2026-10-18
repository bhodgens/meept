# Leaf 01 — HermesConfig + internal/hermes HTTP client

## Goal

Config section + typed HTTP client for the Hermes API Server, exactly per
master.md Contracts A and B. The upstream endpoint contract is frozen in
master.md "Verified upstream contract" — read it first; do not invent
routes.

## Tasks

1. `internal/config/schema.go` — `HermesConfig` per Contract A (Enabled
   false, BaseURL http://127.0.0.1:8642, APIKeyEnv "HERMES_API_SERVER_KEY",
   TimeoutSeconds 300) + DefaultConfig entries + root Config field.
2. `config/meept.json5` — commented `[hermes]` section. Key by env var
   only.
3. `internal/hermes/client.go` — Client per Contract B: Health,
   Capabilities (60s TTL cache), ChatSync, CreateRun (202→run_id),
   RunStatus, SteerRun, StopRun, ListSessions, SessionMessages.
   Typed errors: ErrHermesBusy (429), ErrHermesSessionForeign (409,
   never-retry), ErrHermesAuth (401). Bearer header; never log the key.
   No SSE. Request/response structs mirror the documented JSON shapes
   (chat returns object hermes.session.chat.completion with
   message.content; runs return run_id + status).
4. Tests `internal/hermes/client_test.go` — httptest server: happy paths,
   each typed error, capabilities TTL (second call no HTTP), key never in
   error strings.
5. Config round-trip test in internal/config.

## Constraints

- OWN: internal/config/schema.go (HermesConfig region only), config
  template, internal/hermes/** (new). Nothing else.
- Do NOT register tools (leaf 02) or write docs (leaf 03).
- `go test -p 2 ./internal/config/... ./internal/hermes/...` green;
  both binaries build.
- Commit explicit paths: 'feat(hermes): HermesConfig + API server client (hermes-interop leaf 01)'.

## Report back

RED/GREEN evidence, files, typed-error test names, client method list.
