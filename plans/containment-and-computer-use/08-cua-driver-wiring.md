# cua-driver MCP Wiring - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD where testable.
> Do NOT commit. Do NOT use read_file on existing source — search_files/cat.

## Meta

- **Parent:** ../master.md
- **Scope:** Ship cua-driver as a cataloged (disabled-by-default) MCP server + SecurityEngine risk rules for its tools.
- **Dependencies:** none
- **Estimated Context:** 40K
- **Concurrency Group:** A
- **Audit references:** parity-audit gap #5; issue bhodgens/meept#26

## Goal

Add `cua-driver` to the shipped MCP catalog (enabled: false) so enabling is one toggle, and gate its actions through the existing SecurityEngine: capture/screenshot = low risk; all input-injection actions = high risk (confirmation-gated per existing require_confirmation_high).

## Context

Catalog template lives at repo config/mcp_servers.json5 (~/.meept copy created on install). Entry format: id, description, category, transport stdio, command array, env map, enabled bool — read 30 lines of the file to match field names EXACTLY. Security engine base rules: internal/security/engine.go lookupBaseRule ~512 maps action+toolName->RiskLevel; add prefix-based rules for tool names starting "computer." cua-driver exposes tools named like capture/click/type_text etc via MCP — we gate by OUR registered name prefix: when meept registers external MCP tools, they are namespaced (verify actual namespace format in internal/tools/mcp registration code and use it in rules; contract below assumes "computer." prefix — ADJUST to real prefix found, note deviation if different).

Key files:
- config/mcp_servers.json5 - catalog entry format
- internal/security/engine.go - base rule tables
- internal/tools/mcp/ - how external tools get namespaced on registration

## Interface Contracts (From Parent)

### Exposes

```
Catalog entry (config/mcp_servers.json5):
id: "cua-driver"
description: "background desktop computer-use (macOS/windows/linux) — capture screens with element overlays, click by index, type, scroll. requires cua-driver binary installed."
category: "automation"
command: ["cua-driver", "mcp"]
env: {}
enabled: false

Security rules (engine.go base table additions, prefix-matched on final registered tool name):
  <ns>capture / <ns>screenshot / <ns>list* / <ns>get_*   -> RiskLow
  <ns>click / <ns>type* / <ns>hotkey / <ns>key / <ns>scroll /
  <ns>drag / <ns>move* / <ns>wait / <ns>set_value        -> RiskHigh
where <ns> is the actual mcp-namespaced prefix discovered in code.

Docs: new section in docs/workflows/external-integrations.md (or nearest page):
install commands per OS (curl script from trycua docs), enable steps,
risk behavior explanation.
```

### Consumes

Existing catalog loader + security rule evaluation only.

## Tasks

### Task 1: Catalog entry

**Files:** Modify config/mcp_servers.json5; Test: extend any catalog-parsing test (search_files for existing json5 catalog test) asserting entry parses, defaults disabled, command correct.
Standard cycle.

### Task 2: Risk rules

**Files:** Modify internal/security/engine.go base tables; Test engine_test.go extension.
Failing tests table-driven: evaluate("computer.click") -> RiskHigh requiring confirmation under default config; ("computer.capture") -> RiskLow no confirmation; unknown computer.X -> default high (fail-closed); non-computer tools unchanged. Use REAL prefix after Task-0 discovery step (read mcp registration naming first; record finding).
Standard cycle.

### Task 3: Docs

**Files:** docs page section per contract. No failing test — verify anchors/paths exist; cross-link from mcp doc page listing servers.

## Self-Verification Checklist

- [ ] Entry disabled by default; parse test green
- [ ] Rules fail-closed for unknown computer.* actions
- [ ] Real namespace documented in leaf-deviation notes if differs from computer.
- [ ] Docs cross-linked both directions

**DO NOT COMMIT.**
**Deviations:** [none / list]

## Review Checklist (For Review Agent)

- [ ] Field names match sibling catalog entries byte-style
- [ ] No auto-enable path introduced
- [ ] Rule placement consistent with existing table style

Output: APPROVED or gaps.

## Notes

- Skill content is leaf 09 — not here. This leaf makes the server AVAILABLE and GATED only.
