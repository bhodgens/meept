# Multiuser Access — Master Plan

Status: PENDING
Created: 2026-08-26
Parent: none (root)

## Goal

Add opt-in multi-user access to the meept daemon:

1. Users are defined by configuration. Each user may hold multiple API keys.
2. Keys can carry an expiry timestamp; expired keys fail authentication.
3. Users pool across a cluster: a user/key valid on one node is accepted on
   peer nodes while the nodes remain clustered. If a node leaves the cluster,
   only its LOCAL users remain authoritative.
4. Permission levels above "access": DESIGN ONLY (interfaces + docs), not
   implemented.
5. Per-user quotas: STUB ONLY. The unresolved sizing question (per-machine
   percentage vs other criteria) is documented in code as an open question.
6. Multi-user is DISABLED by default (`multiuser.enabled = false`); the
   existing single-key path is untouched behavior.
7. Configuration tooling updated in CLI, TUI, and Flutter GUI (parity rule).

## Architecture Overview

```
Client (key) ──HTTP──> AuthMiddleware ──> Identity{user, key} in ctx
                                            │
                     ┌──────────────────────┤
                     ▼                      ▼
              SessionStore            QuotaStub / PermStub
           (owner_id column)         (interfaces, no-op impls)
                     ▲
        users store (internal/auth/users.go, JSON5-backed,
        synced to peers over the existing backup_sync RPC channel)
```

- New package `internal/auth`: user/key model, validation, expiry.
- HTTP middleware extended: map presented key → `Identity`; legacy flat-key
  mode bypasses identity entirely (single-user).
- Sessions gain an optional nullable `owner_id`; NULL = unowned (legacy /
  single-user mode) visible to everyone. In multi-user mode, list/read
  filters by owner; admin-capable users see all (permission stub returns
  true for owner, false otherwise — single level enforced today).
- Cluster pooling: the users store is added to the existing peer sync
  payload (`internal/rpc/backup_sync.go`). Local users always win on merge;
  foreign ("other") users are marked `origin_node` and expire from the local
  cache if their source node leaves the cluster (peer set shrinks).

## Interface Contracts (frozen across children)

```go
// internal/auth/users.go
package auth

type Key struct {
    ID        string     `json:"id"`                  // stable key id
    Hash      string     `json:"hash"`                // sha256 hex of raw key
    Label     string     `json:"label,omitempty"`
    ExpiresAt *time.Time `json:"expires_at,omitempty"` // nil = never
}

type User struct {
    ID       string    `json:"id"`
    Name     string    `json:"name"`
    Keys     []Key     `json:"keys"`
    OriginNode string  `json:"origin_node,omitempty"` // empty = local
}

type Store struct { /* ... */ }

func NewStore(path string) (*Store, error)
func (s *Store) Validate(rawKey string, now time.Time) (*Identity, error)
func (s *Store) AddUser(name string) (*User, error)
func (s *Store) AddKey(userID string, label string, expiresAt *time.Time) (rawKey string, err error)
func (s *Store) RevokeKey(userID, keyID string) error
func (s *Store) MergeForeign(users []User, activePeers map[string]struct{}) error

type Identity struct {
    UserID   string
    UserName string
    KeyID    string
}
```

```go
// internal/comm/http — identity in request context
type ctxIdentityKey struct{}
func IdentityFromContext(ctx context.Context) (*auth.Identity, bool)
```

```go
// internal/config/schema.go additions
type MultiUserConfig struct {
    Enabled bool     `json:"enabled" toml:"enabled"` // default false
    UsersFile string `json:"users_file" toml:"users_file"` // default ~/.meept/users.json5
}
// embedded in Config as MultiUser MultiUserConfig
```

Session ownership (store contract): `sessions.owner_id TEXT NULL`.
Read/list APIs accept an optional `viewer *auth.Identity`; nil viewer =
unfiltered (legacy).

Quota stub (documented open question lives here):
```go
// internal/auth/quota.go
// OPEN QUESTION (do not resolve): quota sizing basis undecided —
// (a) percentage of machine-level config budget per user,
// (b) absolute per-user token budget, (c) per-node vs pooled-cluster.
// Implement a no-op Evaluator until decided.
type QuotaEvaluator interface {
    Allow(ctx context.Context, id *Identity, cost int) (bool, error)
}
```

Permission stub:
```go
// internal/auth/permissions.go
// DESIGN ONLY. Future shape candidates: role strings vs capability bits vs
// policy expressions. Only "can access own resources" is enforced today.
type PermissionChecker interface {
    CanAccess(ctx context.Context, actor *Identity, resourceOwner string) bool
}
```

Peer-sync wire addition (RPC `backup_sync`): new field
`users []auth.User` in the sync status/pull payload; merge rules as above.

## Child Index

| # | Document | Type | Est. context | Depends on |
|---|----------|------|--------------|------------|
| 1 | 01-auth-store.md | leaf | ~55K | none |
| 2 | 02-http-identity-sessions.md | leaf | ~75K | 01 |
| 3 | 03-cluster-pooling.md | leaf | ~60K | 01 |
| 4 | 04-user-management-cli.md | leaf | ~50K | 01 |
| 5 | 05-client-tooling-tui-gui.md | leaf | ~70K | 01 |
| 6 | 06-rpc-peercred.md | leaf | ~40K | none (independent) |

Concurrency: after 01 completes REVIEWED, dispatch 02, 03, 04, 05 in one
batch (cap permitting). Leaf 06 has NO dependency on 01 and may dispatch
immediately, even before 01.

## Dispatch Protocol

For each child document:

1. **Dispatch implementation agent** via `delegate_task` with the child doc
   content plus the frozen Interface Contracts section above verbatim, plus:
   "Do NOT commit. Do NOT run git add. Write code, run tests, report results
   only. Use search_files/terminal cat to inspect code — never feed
   read_file line-numbered output back into write_file."
2. **Review in-session** (main model): read changed files, verify contracts,
   run `go build ./...`, targeted `go test`, `go vet`, predid/mutexio
   analyzers. Check for stray artifacts, TODOs, unused code (U1000).
3. **Re-dispatch** with specific feedback on gaps (max 3 iterations).
4. **Commit**: orchestrator stages exact leaf file paths, message
   `feat(multiuser): <leaf name>` — SEPARATE COMMIT per concern.
5. Update tracking table.

## Coding Conventions

- Go stdlib style; errors wrapped with `%w` and lowercase context prefix.
- No I/O under mutex (mutexio analyzer enforces). Collect-under-lock then
  operate.
- IDs via `pkg/id.Generate()` — never time/rand (predid analyzer).
- Nil guards in all Set* methods.
- Two-value type assertions on bus payloads.
- UI text lowercase everywhere (TUI + GUI).
- All new config fields need defaults in `internal/config` and appear in
  generated config docs (`make build` regenerates).
- AGENTS.md must be updated in the integration commit (new package table
  row for internal/auth, invariant note about multiuser default-off).

## Review Checklist (per leaf)

- [ ] Compiles: `go build ./...`
- [ ] Tests pass: package tests + `-race` on new packages
- [ ] Analyzers clean: mutexio, predid, `go vet`
- [ ] Contract types/signatures match master §Interface Contracts exactly
- [ ] No debug prints, TODOs (except sanctioned OPEN QUESTION markers),
      placeholder values, commented-out code
- [ ] No unused exported functions (U1000-safe)
- [ ] Multi-user OFF path behaves identically to current behavior
- [ ] grep production callers exist for every exported function (wiring rule)

## Integration Test Plan

After all leaves COMPLETE:

1. Full suite: `go test ./... -short -race` repo-wide.
2. `go test ./tests/integration/ -count=1`.
3. Manual smoke (scriptable): start daemon with `multiuser.enabled=true`,
   two users each one key (one expiring in past) → verify 401/418 for bad/
   expired key, chat works with good key, second user cannot list first
   user's sessions.
4. Legacy smoke: same daemon with `multiuser.enabled=false` → dev-key path
   works exactly as before.
5. Peer sync smoke (if two daemons available): user created on node A
   authenticates on node B; stop clustering → B rejects A's foreign users
   after cache expiry window.
6. `make graphs` fresh (bus topology changes from new topics, if any).
7. AGENTS.md updated; compliance scan re-run.

## Completion Tracking Table

| Child | Status | Notes |
|-------|--------|-------|
| 01-auth-store.md | COMPLETE | commit 756491ec |
| 02-http-identity-sessions.md | COMPLETE | commits 15e8d344 + b926a483 (daemon wiring) |
| 03-cluster-pooling.md | COMPLETE | commit 74d50167 — gossip channel, merge target nil-wired until leaf 02 integration |
| 04-user-management-cli.md | COMPLETE | commit bc269da2 (+ bbbb3ccb sentinel follow-through) |
| 05-client-tooling-tui-gui.md | COMPLETE | commit 99fc881e — awareness+CLI-guidance parity (no manage path exists on either surface) |
| 06-rpc-peercred.md | COMPLETE | commit cbda76c9 |
| live | COMPLETE | totem1/2/3 3-node verification 2026-08-28: pooling + cross-node key validation confirmed; found+fixed 3 more production wiring bugs (0837af0e members provider, 9af6d011 git-sync start, 55868872 TCP-heartbeat liveness, a6fde549 heartbeat merge+guard) |

## Open Questions (design-only, do NOT block implementation)

- Q1 Permission shape: roles vs capabilities vs policy DSL. Stubs accept
  any; revisit when first consumer needs it.
- Q2 Quota basis: per-machine % vs absolute vs cluster-pooled. Documented
  in `internal/auth/quota.go`; decide before implementing evaluator.
- Q3 RPC identity: RESOLVED as defense-in-depth in leaf 06 (peer UID
  logging + optional allowlist). RPC remains owner-trusted single-user by
  design; see docs/reference/http-api-security.md §Unix Socket RPC.
