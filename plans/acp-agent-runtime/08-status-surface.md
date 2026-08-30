# Status Surface (TUI + HTTP) + Docs — Implementation Leaf

> **For the implementing agent:** Implement ALL tasks below using TDD.
> Do NOT commit. Do NOT use read_file on existing source files.

## Meta

- **Parent:** ../master.md
- **Scope:** HTTP status endpoint + TUI statusbar token + docs-workflows section for ACP
- **Dependencies:** 07 (wiring must exist; this leaf reads Manager state)
- **Estimated Context:** 45K
- **Concurrency Group:** E

## Goal

Users can SEE the feature: `GET /api/v1/acp/agents` reports enabled state and live sessions; the TUI statusbar shows an `acp:N` token when enabled (absent when disabled). Docs land in docs/workflows/ per the repo's feature documentation requirement. UI text lowercase throughout (repo invariant).

## Context

- internal/comm/http/ — how /api/v1/mcp/servers is implemented (route registration, auth posture, response shape). Mirror it: same middleware chain, same JSON envelope conventions.
- internal/tui/ — how the statusbar composes tokens (grep statusbar/status bar composition; tokens like session/model indicators). ACP token follows the same pattern, sourced from whatever status snapshot the TUI already polls (do NOT add a new polling path — extend the existing status payload).
- docs/workflows/external-integrations.md — the MCP server section is the model for an ACP client section; docs/reference/http-api.md gets the new endpoint row.
- TUI/GUI parity invariant: AGENTS.md requires Flutter parity — leaf 09 (same tree) ships the Flutter surface consuming this endpoint; this leaf's docs cross-reference it instead of recording a deferral.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/comm/http/ (file per existing layout)
// GET /api/v1/acp/agents
// 200: {"enabled": bool, "agents": [{"id": "...", "enabled": bool, "running": bool, "state": "ready|busy|closed|", "uptime_s": 0}]}
// auth posture: same as /api/v1/mcp/servers (whatever middleware that route uses — copy the chain exactly)
// disabled: {"enabled": false, "agents": []} (still 200 — surfacing that acp exists-but-off is correct)
```

TUI: statusbar token `acp:N` where N = count of non-closed sessions; rendered ONLY when cfg acp.enabled. Lowercase.

### What This Leaf Consumes

From 07: the daemon holds *acp.Manager (or nil). The HTTP handler needs a manager accessor — follow how the MCP servers endpoint reaches its manager (grep the handler's dependency injection; same mechanism).

## Tasks

### Task 1: Failing tests — HTTP endpoint

**Files:** Create: internal/comm/http/acp_status_test.go (or the file layout sibling MCP tests use)

Table: disabled manager -> {"enabled":false,"agents":[]}; enabled with fake manager (constructor-injected live sessions map) -> correct agents array with states; uptime_s is int seconds. Auth: request without the same credentials MCP endpoints require -> same rejection code as mcp/servers returns (assert parity, not a specific code — read what mcp/servers enforces and mirror the assertion).

### Task 2: Endpoint implementation

**Files:** Modify: internal/comm/http/ route registration file (the one holding /api/v1/mcp/servers) + new handler (new file acp_status.go if layout allows)

Route string exactly "/api/v1/acp/agents". Handler nil-manager-safe (disabled daemon builds without manager — must return the disabled envelope, not 500).

### Task 3: Failing tests + implementation — TUI statusbar token

**Files:** Test: the statusbar test file the existing tokens use; Modify: the statusbar composition site

Token text "acp:N", lowercase, only when enabled. N from the status payload the TUI polls — extend that payload's builder (likely a daemon-side status RPC or HTTP poll — follow the existing data path; do not add a second one). If the TUI status snapshot is built from an RPC payload, the payload extension belongs in the same leaf: add the field with json+yaml tags BOTH (repo hazard: yaml-tagged structs over JSON wires silently emit Go field names). Test the token renders and absent-when-disabled.

### Task 4: Docs

**Files:** Modify: docs/workflows/external-integrations.md (new "ACP Client Agents" section: what it is, config keys, catalog example, security posture table (launch/send HIGH, read/stop LOW), disabled-by-default note); docs/reference/http-api.md (endpoint row + example); docs/reference/cli.md only if a CLI surface exists for status (check `meept status` output path — if it aggregates HTTP status, the acp block appears automatically; verify and record, do not build a new CLI command).

## Self-Verification Checklist

- [ ] go build ./internal/comm/http/ ./internal/tui/ green
- [ ] go test ./internal/comm/http/ -run ACP -race green; TUI package scoped test green
- [ ] Disabled envelope returns 200 {"enabled":false} — tested
- [ ] Auth parity with mcp/servers asserted
- [ ] All UI strings lowercase
- [ ] Status payload struct carries BOTH json and yaml tags
- [ ] Docs sections added; Flutter parity noted as shipping in leaf 09 (same tree, no deferral)
- [ ] No TODOs

**DO NOT COMMIT.**

**Deviations from spec:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Endpoint mirrors mcp/servers (route file, middleware, envelope)
- [ ] TUI token only when enabled
- [ ] Handler nil-manager-safe
- [ ] Docs: three files touched, accurate, lowercase UI text
- [ ] No new polling path; payload extension reuses existing data flow
- [ ] File scope respected; foreign hunks reported not staged

## Notes

- The docs-workflows mapping rule: internal/acp -> docs/workflows/<pkg>.md — external-integrations.md is the established home for cross-package integration surfaces (MCP lives there); keep ACP beside it rather than minting a new doc file unless the tree pattern has changed.
- After this leaf, the master's Integration Test Plan step 5 (wiring smoke) becomes executable end-to-end.
