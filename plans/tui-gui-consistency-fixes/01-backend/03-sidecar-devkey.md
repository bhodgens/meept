# Sidecar Dev-Key Token Bootstrap - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. If this branch runs a tests-first
> gate, the failing tests ALREADY EXIST and are committed - your job is to
> make them pass; do NOT delete, skip, weaken, or rewrite any gate test
> (report suspected-wrong gate tests to the orchestrator instead). Do NOT
> commit - the orchestrator handles all git operations after review. Do NOT
> use read_file on existing source files - explore with search_files or
> terminal cat. After writing a file, do NOT read it back - write once and
> stop. Report what you built, files touched, and any deviations.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Seed the kanban sidecar token store with the per-installation
  dev key so a default GUI install can open the board without a pairing code.
- **Dependencies:** 04-config-docs/00T-e2e-pin.md; C5, C7
- **Estimated Context:** 45K
- **Concurrency Group:** D1b
- **Audit references:** R7, R14

## Goal

With option B the daemon spawns the kanban sidecar by default. The GUI already
sends `$MEEPT_HOME/dev_key` as its bearer token to port 8790. The sidecar's
`TokenStore` starts empty and answers 401 to everything until a browser pairs.
This leaf grants the existing dev key `board.read`, `board.mutate`, and
`board.takeover` in `$MEEPT_HOME/kanban/tokens.json` before the child spawns.
No Dart change. No anonymous or default token is introduced.

## Context

The seed is the existing per-installation credential, not a new secret. It
carries the same trust as the loopback daemon HTTP path. The sidecar still
refuses a token file with group/other bits and still enforces all grant
checks. Remote GUI access still needs the documented TLS proxy and
`[kanban].allowed_origins`.

- `internal/kanbansvc/pairing.go:155-176` - NewTokenStore / DefaultTokenStorePath
- `internal/kanbansvc/pairing.go:262-274` - Grant(token, grants...) persists with 0600/0700
- `internal/kanbansvc/gateway.go:34-36` - GrantRead/Mutate/Takeover strings
- `pkg/constants/api_key.go:104` - DevAPIKey() reads $MEEPT_HOME/dev_key
- `internal/daemon/kanban_boot.go:231-306` - StartKanbanSidecar spawn gate
- `internal/daemon/kanban_boot.go:372-384` - child argv construction
- `cmd/meept-kanban/main.go:51` - -pair-store flag already accepted by child

## Verified Anchors

- internal/kanbansvc/pairing.go:155 - NewTokenStore(path string) (*TokenStore, error)
- internal/kanbansvc/pairing.go:262 - (s *TokenStore) Grant(token string, grants ...string) error
- internal/kanbansvc/gateway.go:34 - GrantRead = "board.read"
- pkg/constants/api_key.go:104 - DevAPIKey() string
- internal/daemon/kanban_boot.go:231 - func (d *Daemon) StartKanbanSidecar(ctx context.Context) error
- internal/daemon/kanban_boot.go:372 - child args construction

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// daemon-internal; no new RPC surface
func seedKanbanDevKeyToken(logger *slog.Logger) error
// Called from StartKanbanSidecar only when SidecarEnabled && Enabled.
// Never logs the key. Returns error; spawn proceeds only on nil.
```

### What This Leaf Consumes

- C5: `DefaultConfig().Kanban.Enabled == true`, `SidecarEnabled == true`,
  `WIPMode == "warn"` - owner: 01-kanban-default.md
- C7: sidecar stays config-blind; flags only - owner: existing contract

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Seed helper with RED unit test | Create: internal/daemon/kanban_devkey_test.go; Create: internal/daemon/kanban_devkey.go | `go test -p 2 ./internal/daemon -run TestKanbanDevKeySeed` | helper undefined | grants present, file 0600 | Given a temp MEEPT_HOME with dev_key; When seed; Then tokens.json holds 3 grants for that key and mode is 0600 |
| T2 | Gate seed into sidecar boot | Modify: internal/daemon/kanban_boot.go | `go test -p 2 ./internal/daemon -run TestKanbanSidecarSeed` | no seed call | seed runs before spawn | Given enabled+sidecar_enabled; When StartKanbanSidecar; Then Grant called before child argv |
| T3 | No secret logging + no-op when disabled | Modify: internal/daemon/kanban_boot.go | same + `grep -n 'dev_key\|token' internal/daemon/kanban_devkey.go` | key logged | no key in log lines | Given SidecarEnabled=false; When StartKanbanSidecar; Then store untouched |

TDD detail for T1:
1. Write a test pointing `MEEPT_HOME` at `t.TempDir()`, write a known dev_key
   file there, call `seedKanbanDevKeyToken`, assert `TokenStore.Grants(devKey)`
   returns all three grant strings and the file mode has no group/other bits.
   Never print the key.
2. Verify RED: `go test -p 2 ./internal/daemon -run TestKanbanDevKeySeed` fails
   because the helper is undefined.
3. Implement the helper: call `constants.DevAPIKey()`, refuse empty key,
   `kanbansvc.NewTokenStore("")`, then `Grant(key, GrantRead, GrantMutate,
   GrantTakeover)`. Wrap errors with `%w`. Log presence only, never value.
4. Verify GREEN.

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations (or deviations documented in the report)
- [ ] No scope creep - only what the tasks specify

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [verify commands; last 20 lines of output]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; tests passing, gate tests untouched
- [ ] Interface contracts match exactly (signatures, types, paths)
- [ ] Conventions followed; no bugs, no security issues
- [ ] No scope creep; no debug artifacts or line-number corruption
- [ ] The dev key value appears in NO log statement or test output
- [ ] Existing pairing flow still works; empty-store boot posture unchanged
      when the sidecar is started manually without the daemon

Output: APPROVED or specific gaps with file + line references.

## Notes

- `TokenStore.Grant` replaces any prior grant set for that token. Re-running
  the daemon is idempotent. Do not revoke other tokens.
- Do NOT add a `-seed-token` child flag. The daemon writes the shared store
  before spawn; the child reads it from the same `$MEEPT_HOME` path.
- Do NOT change `cmd/meept-kanban` boot: a directly launched sidecar keeps
  the documented 401-everything posture until an operator pairs.
