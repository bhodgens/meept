# AGENTS.md

Guidance for AI coding agents working in this repository.

## Connectivity Graph (Bus / RPC / HTTP / WS)

The daemon's components communicate via string-typed bus topics, RPC handlers,
and HTTP routes — all invisible to the compiler. A generated connectivity graph
maps every publish/subscribe edge, RPC handler, HTTP route, and WS event
classification so you can trace cross-boundary data flow without reading the
code:

- **`docs/generated/bus-topology.md`** — human-readable tables: every bus topic
  with its publishers, subscribers, and payload keys; orphan analysis (topics
  published but never subscribed, and vice versa); WS event classification;
  RPC handler map; HTTP route map.
- **`docs/generated/bus-topology.json`** — machine-readable version for tooling.
- **`docs/generated/rpc-handlers.json`**, **`http-routes.json`**,
  **`ws-event-map.json`** — individual layer exports.

Regenerate with `make graphs` (runs automatically on every `make build`).
Verify freshness in CI with `make graphs-check`.

**When debugging cross-boundary issues** (events not reaching clients, wrong
payload fields, orphaned listeners), start with `docs/generated/bus-topology.md`
before reading source — it shows the full publish/subscribe graph in one table.
