# Leaf 04 — User management RPC + CLI surface

DISPATCH INSTRUCTION: Implement this leaf end-to-end. Do NOT commit.
Parent: master.md. Depends on: 01-auth-store (REVIEWED). Est. context ~50K.

## Goal

Make the store user-manageable: `meept` CLI subcommands for user/key
lifecycle, backed by daemon RPC (or direct file access when the CLI runs on
the same host as the users file — prefer direct file access for v1 to avoid
new bus topics).

## Files

Modify:
- cmd/meept/ (new `meept users` command tree: list/add/remove, key
  add/revoke/list with expiry flags)
- internal/auth/store.go: add `RemoveUser(id string) error` and
  `ListUsers() []User` IF absent from leaf-01 surface; otherwise extend in
  this leaf and note the deviation in your report.
- tests: CLI-level where practical, store-level otherwise

## Contract

```
meept users list
meept users add <name>
meept users remove <id>          # confirm prompt unless --yes
meept keys add <user-id> [--label L] [--expires RFC3339|never]
meept keys revoke <key-id>
meept keys list [user-id]        # shows id/label/expiry, never raw keys
```

Raw key printed EXACTLY ONCE at creation (same UX as `meept token
generate`). All output lowercase per UI conventions.

## Tasks (TDD)

1. Store additions (if needed) with tests.
2. Command tree using the existing cobra/command pattern in cmd/meept/
   (inspect a sibling like `agents_cmd.go` for structure).
3. Expiry parsing (--expires accepts RFC3339 or "never"); invalid → usage
   error.
4. Tests for arg parsing + store round-trip through the command layer.

## Self-Verification Checklist

- [ ] go build ./... ; vet clean
- [ ] Commands appear in gendoc output (run make build or the gendoc step)
- [ ] Raw key printed once; never persisted in logs
- [ ] Output strings lowercase

## Review Checklist (orchestrator)

- [ ] Command help text matches contract verbs exactly
- [ ] No raw-key persistence anywhere (grep)
- [ ] AGENTS.md-worthy notes reported back (do not edit it)
