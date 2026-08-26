# Leaf 01 — Auth Store (internal/auth)

DISPATCH INSTRUCTION: Implement this leaf end-to-end. Do NOT commit.
Parent: master.md. Depends on: none. Est. context ~55K.

## Goal

Create `internal/auth`: the user/key model, a JSON5-backed store with expiry
validation, merge semantics for cluster-pooled ("foreign") users, and no-op
quota/permission stubs.

## Files

Create:
- internal/auth/users.go
- internal/auth/store.go
- internal/auth/quota.go
- internal/auth/permissions.go
- tests for each (users_test.go, store_test.go, quota_test.go, permissions_test.go)

## Interface Contract (frozen — copy verbatim from master.md §Interface Contracts)

Types `Key`, `User`, `Identity`, `Store`, functions
`NewStore(path) (*Store, error)`, `(s *Store) Validate(rawKey string,
now time.Time) (*Identity, error)`, `AddUser(name)`, `AddKey(userID, label,
expiresAt *time.Time) (rawKey string, err error)`, `RevokeKey(userID, keyID)`,
`MergeForeign(users []User, activePeers map[string]struct{}) error`.
Interfaces `QuotaEvaluator` and `PermissionChecker` per master contract,
including the OPEN QUESTION comments.

## Tasks (TDD)

1. **Key/User types + raw-key generation.** Raw keys are 32 random bytes
   hex (`crypto/rand`). Only the sha256 hash is persisted; raw is returned
   once at creation. Test: two generated keys differ; hash matches manual
   sha256; predid-safe ID generation via pkg/id.
2. **Store persistence.** JSON5 file at configured path, 0600, written
   atomically (temp file + rename). Collect-under-lock then write-outside-
   lock (mutexio). Test: save → reload round-trip preserves users/keys/
   expiries; corrupt file yields error not panic.
3. **Validate().** Hash lookup across all users' keys; reject when
   `ExpiresAt != nil && now.After(*ExpiresAt)`; return Identity. Constant-
   time compare of hashes. Test: valid key passes; expired key fails;
   unknown key fails; nil-expiry never expires.
4. **AddUser/AddKey/RevokeKey.** AddUser errors on duplicate name. RevokeKey
   removes by id, errors if missing. Tests for each incl. error paths.
5. **MergeForeign(users, activePeers).** Foreign users get `OriginNode`
   set from caller-provided metadata (see note). Rules:
   - Local users (empty OriginNode) are authoritative; never overwritten.
   - Foreign user accepted only while its OriginNode ∈ activePeers.
   - Existing foreign users whose node dropped out of activePeers are
     removed.
   - Same-origin re-merge replaces previous foreign copy wholesale.
   Note: MergeForeign receives fully-attributed User values; setting
   OriginNode is the sync layer's job (leaf 03).
   Tests cover each rule plus idempotent re-merge.
6. **Quota stub** — QuotaEvaluator interface + NoopQuota allowing everything,
   with the verbatim OPEN QUESTION comment block from master.
7. **Permission stub** — PermissionChecker interface +
   OwnerOnlyPermissions: CanAccess true iff resourceOwner == actor.UserID
   or actor == nil (legacy single-user mode sees everything).
8. Run: `go build ./...`, `go test ./internal/auth/ -race -count=1`,
   `go vet ./internal/auth/...`, analyzers mutexio+predid on the package.

## Self-Verification Checklist

- [ ] All package tests pass with -race
- [ ] mutexio/predid clean
- [ ] No I/O under mutex; atomic file writes
- [ ] No unused exported functions (wiring comes in leaves 02/03 — keep the
      exported surface EXACTLY as contracted; do not add extras)
- [ ] OPEN QUESTION comments present in quota.go and permissions.go
- [ ] No debug prints / TODOs beyond sanctioned markers

## Review Checklist (orchestrator)

- [ ] Signatures match master contract exactly
- [ ] Expired-key and unknown-key paths covered by tests
- [ ] MergeForeign local-authority rule tested
- [ ] File perms 0600 verified in test
