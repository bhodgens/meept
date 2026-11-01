# Codemode Builtin Tool + Config Gate - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD. Gate e2e
> cases exist. Do NOT commit. Write once, never read back.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** `codemode` builtin tool wrapping internal/codemode; config gate `[agent.tools] codemode_enabled` (default false); docs
- **Dependencies:** 01-sandbox-core COMPLETE
- **Estimated Context:** 90K
- **Concurrency Group:** solo

## Goal

Register a `codemode` tool ONLY when the config gate is on; bridge the
agent's filtered registry into ToolBridge list; return Result JSON to the
model; document in docs/workflows/codemode.md; add AGENTS.md package-table row
for internal/codemode in the same commit as the tool (orchestrator stages both).

## Context

- Tool factory pattern: internal/tools/builtin/*.go (existing tools as templates;
  registration via components.go deterministic wiring - see
  applyDeterministicToolsFromConfig, internal/daemon/components.go:3053).
- FilteredToolRegistry: internal/agent/executor.go:1927 - bridge source is the
  agent's ALREADY-filtered registry (never widen access).
- Config schema: internal/config/schema.go [agent.tools] section (SchemaMode
  lives there, ~line 1915-1947 pattern to follow: string/bool field + Validate).
- Config template: config/meept.json5 (or the matching template) gains
  `codemode_enabled: false` with a comment.

## Verified Anchors

- internal/daemon/components.go:3053 - applyDeterministicToolsFromConfig
- internal/config/schema.go:1924-1947 - [agent.tools] validation pattern
- internal/agent/executor.go:1927 - NewFilteredToolRegistry

## Interface Contracts (From Parent)

### What This Leaf Exposes

Tool name `codemode`; input schema `{script: string (required), timeout_s?: int (default 60, max 600)}`;
output = Result JSON (C3). Description tells the model: nested tool calls do
not consume context; use for mechanical multi-call work.

### What This Leaf Consumes

- C3 Sandbox API (leaf 01)
- C1 clamp constant reuse where practical (50KB)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Config field + validation | Modify: internal/config/schema.go; config template | `go test -p 2 ./internal/config/ -run TestCodemode` | RED | unknown value rejected; default false | Given codemode_enabled absent; When loaded; Then false |
| T2 | Tool implementation | Create: internal/tools/builtin/codemode.go | `go build ./...` | n/a | Execute builds bridges from the provided registry, runs sandbox, returns Result JSON | Given echo bridge registry; When script calls tools.echo; Then result contains the echo output |
| T3 | Gated registration | Modify: internal/daemon/components.go | `go test -p 2 ./internal/daemon/ -run TestCodemodeGate` or e2e gate cases | gate RED | tool absent when flag off, present when on | Given default config; When definitions listed; Then no codemode entry |
| T4 | Docs + AGENTS.md | Create: docs/workflows/codemode.md; Modify: AGENTS.md (package table), config template comment | `make docs-check` if touched surfaces require | n/a | docs describe gate, security model, examples | Given a user reading docs; When they follow the example; Then it runs |
| T5 | Bridge-set safety | Modify: codemode.go | `go test -p 2 ./internal/tools/builtin/ -run TestCodemodeBridges` | RED | sandbox tool set == agent filtered set (no additions); shell/file_write included only if the agent has them | Given a restricted agent; When bridges listed; Then ALL_TOOLS equals its registry names |

## Self-Verification Checklist

- [ ] Gate default false end-to-end (config load -> registration)
- [ ] Tool never widens access; timeout_s clamped to 600
- [ ] AGENTS.md + docs in the same staged set as code

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched; 2. verify tails; 3. deviations.

## Review Checklist (For Review Agent)

- [ ] e2e gate cases TestCodemodeDisabledByDefault / RegisteredWhenEnabled pass
- [ ] Description text teaches context-savings usage (model-facing quality)
- [ ] No secrets/env passthrough into the sandbox
