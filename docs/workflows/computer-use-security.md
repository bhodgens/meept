# Computer-Use Security Rules (cua-driver)

Risk classification for tools exposed by the `cua-driver` MCP server
(desktop computer-use: screen capture with numbered element overlays, click
by element index, typing, scrolling, hotkeys). The catalog entry ships in
`config/mcp_servers.json5`, disabled by default.

## How It Works

The security engine classifies every tool call before execution. For tools
registered under the `cua-driver.` name prefix, `pkg/security.ComputerUseRule`
applies a fixed mapping instead of the generic MEDIUM default:

| Action pattern | Risk | Confirmation |
|---|---|---|
| capture, screenshot, list*, get_* (observation) | LOW | not required |
| click, type, hotkey, key, scroll, drag, move, wait, set_value (input injection) | HIGH | required when `[security] require_confirmation_high = true` |
| any unrecognized `cua-driver.*` action | HIGH (fail-closed) | required |

Operator overrides in the `tool_rules` table keep precedence over these base
rules; this mapping is consulted only when no explicit rule matches.

## Configuration

```json5
// [security] relevant knobs
require_confirmation_high = true   // gates all input-injection actions
```

Enable the server itself via the TUI MCP menu (`ctrl-x o`), select
`cua-driver`, press `e`; or set `enabled: true` on its entry in
`~/.meept/mcp_servers.json5`.

## Edge Cases

- Unknown actions fail closed to HIGH rather than falling through to MEDIUM.
- Rule lookup happens after DB-backed operator rules, so per-agent overrides
  still win.
- The engine returns `confirm=false` at rule level for HIGH results;
  confirmation is enforced by Stage 5 (`needsConfirmation`) so the standard
  confirmation flow and audit trail apply unchanged.

## Install

Per-OS install commands live in
[external-integrations](external-integrations.md#cua-driver-desktop-computer-use).

The bundled `computer-use` skill (`config/skills/computer-use/SKILL.md`)
encodes the recommended capture → act → verify loop and the safety rules
agents should follow when driving these tools; see also
[external-integrations](external-integrations.md#cua-driver-desktop-computer-use)
for enablement steps.
