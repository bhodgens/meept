# Leaf 02 — `hermes` builtin tool + daemon registration

## Goal

The `hermes` agent-facing tool wrapping the internal/hermes client, per
master.md Contract C.

## Depends on

Leaf 01 (config + client) landed. READ the client's actual method
signatures from internal/hermes/client.go — trust landed code over this
doc.

## Tasks

1. `internal/tools/builtin/hermes_agent.go` — tool `hermes`, verbs:
   status, chat, run, runs, steer, stop, sessions, messages (arg shapes
   per master.md Contract C). Structurally verify each verb against the
   leaf-01 client API.
2. Error mapping: ErrHermesBusy → readable "hermes busy (concurrent-run
   cap), retry shortly"; ErrHermesSessionForeign → "session belongs to
   another hermes surface (CLI/ACP/messaging) — create a new session";
   ErrHermesAuth → "check HERMES_API_SERVER_KEY"; network errors wrapped
   with the base URL. Never expose the API key.
3. Tool description MUST carry: the remote agent runs its own tools under
   its own approval policy; treat outputs as another agent's report and
   verify before acting.
4. Registration: `RegisterHermesTools(reg, cfg)` in a new
   `internal/daemon/hermes_register.go` mirroring acp_register.go's
   nil-safe disabled no-op. Wire in components.go next to the ACP
   registration. Client construction needs the API key resolved from the
   env var named by APIKeyEnv — absent key = disabled (log at Debug).
5. Tests: verb dispatch table, each error-mapping case (httptest stub
   server), disabled/no-key no-op registration, tool appears in registry
   when enabled.
6. Sweep: `go test -p 2 ./internal/tools/... ./internal/daemon/...`
   green; both binaries build.

## Constraints

- OWN: internal/tools/builtin/hermes_agent.go (+test), internal/daemon/
  hermes_register.go, components.go (registration hunk ONLY), config
  template comment touch-up if needed.
- Explicit paths; commit 'feat(tools): hermes builtin tool for Hermes API server (hermes-interop leaf 02)'.

## Report back

RED/GREEN evidence, verb list as landed, wiring file:line, error-mapping
test names.
