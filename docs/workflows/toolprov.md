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

`[tool_providers]` in meept.json5 (lands with leaf 04-config.md):
per-provider `enabled` flag; absent = enabled. `meept doctor` lists each
registered provider's state (report-only).

## Edge Cases

- Unknown provider names in config pass through to RegisterAll, which
  errors listing the known names.
- A provider that declares no posture fails closed: its tools are treated
  as mutating and privilege-scanned (G4).
- Duplicate provider or tool names error at registration; nothing
  silently overwrites.
