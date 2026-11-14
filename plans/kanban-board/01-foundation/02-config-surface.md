# 01/02-config-surface

## DISPATCH INSTRUCTION
Do NOT commit. Do NOT run `git add`.

## Parent and Dependencies
- Parent: `01-foundation` (orchestrator)

## Scope
Integrate Kanban configuration into the global configuration system.
1. Add `[kanban]` block to `internal/config` as per master.md C9.
2. Implement default values (all `false` per `docs/agents/opt-in-defaults.md`).
3. Provide a template stanza for `config/meept.json5`.
4. Note: This touches shared config; ensure `enabled=false` path is byte-identical to current state if `enabled` is false.

## Estimated Context
45K tokens

## Interface Contract
Updates `internal/config/config.go` to include:
```go
type KanbanConfig struct {
    Enabled         bool   `json:"enabled"`
    SidecarEnabled  bool   `json:"sidecar_enabled"`
    SidecarBind     string `json:"sidecar_bind"`
    WIPMode         string `json:"wip_mode"` // "warn" | "block"
    GitHubSync      struct {
        Enabled bool `json:"enabled"`
    } `json:"github_sync"`
}
```

## Tasks
1. **TDD: failing test for missing config field**
   - `go test -v ./internal/config/ -run TestConfig_KanbanDefaults`
2. **Implementation: config.go**
   - Add `Kanban` field to main config struct.
   - Implement default initialization logic.
3. **Template: meept.json5 snippet**
   - Add JSON5 template stanza for `[kanban]`.
4. **Verification: config_test.go**
   - Assert default values are correct and `enabled=false` is default.

## Self-Verification Checklist
- [ ] `[kanban]` block follows master.md C9 exactly.
- [ ] `enabled=false` path compiles and is byte-identical to current config.
- [ ] No new external dependencies.

## Review Checklist
- [ ] No debug prints or TODOs.
- [ ] Every new Set*/setter has a nil guard.
- [ ] No I/O under mutex.
