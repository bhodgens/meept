---
title: Toolprov
---

# Toolprov

## Overview

`internal/toolprov` is the compiled-in tool provider seam. It lets native
capabilities register as providers instead of direct daemon wiring calls.
Contract C1 of the tool-provider-seam plan
(docs/plans/20261031-tool-provider-seam/master.md, GitHub issue #78).

Status: stub landed (types only). Implementation lands with plan leaf
01-seam.md; this page expands at 04-config.md.

## Problem

Meept's native tools register through hundreds of direct
`c.ToolRegistry.Register(...)` calls in internal/daemon/components.go.
Adding a native capability means editing daemon wiring, and nothing
structurally forces a new capability through the same gates as native
tools. The provider seam groups capabilities into named providers with a
declared permission ceiling, and the registry executes every tool through
one central gate.

## Behavior

- `Provider` interface: Name, Discover (tools contributed),
  PermissionClass (risk ceiling: read_only / mutating / privileged),
  Broker (secrets binding; placeholders toward child lanes).
- Provider packages register constructor factories via `init()`
  (pattern proven by githubsvc.ToolFactories). The daemon walks
  `RegisterAll(registry, enabledNames)`.
- Every execution passes the centralized gate in `Registry.Execute`:
  security scan per tool category, audit-chain emission, effects-ledger
  reservation for mutating tools, fail-closed posture inheritance
  (contract C2, leaf 02-gate.md).

## Configuration

`[tool_providers]` in meept.json5 configures the compiled-in providers:

```json5
{
  "tool_providers": {
    "providers": {
      "github":    { "enabled": false },
      "scheduler": { "enabled": false },
    },
  },
}
```

Semantics (default-true):

- Absent `tool_providers` block, absent provider entry, or an entry
  without `enabled` = the provider is enabled. Existing installs keep
  working with no config change.
- Explicit `enabled: false` disables the provider: its tools never
  register (the wiring site skips it and logs the skip at debug).
- Unknown names are not config errors: they flow through to
  `toolprov.RegisterAll`, which fails boot registration naming the
  known names.
- `meept doctor` lists every registered provider with its state
  (`enabled`, `enabled (config)`, `disabled`) plus config names with no
  registered factory as `unknown`. Report-only: it never installs,
  prompts, or changes the exit status.

## Edge Cases

- Unknown provider names in config pass through to RegisterAll, which
  errors listing the known names.
- A provider that declares no posture fails closed: its tools are treated
  as mutating and privilege-scanned (G4).
- Duplicate provider or tool names error at registration; nothing
  silently overwrites.

## Live Verification

`make e2e-fast-area AREA=tool-provider-seam` runs the hermetic live-gate
suite (e2e/suites/tool-provider-seam/): a scratch daemon boots with the
providers enabled, disabled, and doctor-surfaced, and the suite asserts
through live surfaces only - the platform_tools catalog and the executor
request definitions (both resolved from the live registry), a real
provider tool execution observed through scheduler.list_jobs, the
block_financial denial (job never created), the `tool gate wired` boot
line plus the on-disk tool_audit_chain.db store, and `meept doctor`'s
provider report lines.

