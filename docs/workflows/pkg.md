# Pkg (Shared Security Package)

Shared, import-cycle-free security primitives used by both the daemon's
`internal/security` engine and external consumers.

## How It Works

`pkg/security/computer_use.go` holds the risk-classification table for
cua-driver computer-use tools: observation actions (capture/screenshot/list/
get) classify LOW; input-injection actions (click/type/hotkey/scroll/drag/
set_value/wait) and any unrecognized `cua-driver.*` name classify HIGH
(fail-closed). `internal/security/engine.go` consults this table via
`ComputerUseRule()` when no operator rule matches. Full behavior documented
in [computer-use-security](computer-use-security.md).

## Configuration

None at package level. Consumers apply confirmation gating through the
standard `[security] require_confirmation_high` knob.

## Edge Cases

- Unknown actions fail closed to HIGH, never MEDIUM.
- The table is pure and synchronous — no I/O, safe under the engine's locks.
