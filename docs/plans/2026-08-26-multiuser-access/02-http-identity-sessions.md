# Leaf 02 — HTTP identity wiring + session ownership

DISPATCH INSTRUCTION: Implement this leaf end-to-end. Do NOT commit.
Parent: master.md. Depends on: 01-auth-store (REVIEWED). Est. context ~75K.

## Goal

Wire the auth store into the HTTP server: key → Identity in request context;
session ownership column + filtered list/read; multi-user OFF preserves
current behavior byte-for-byte.

## Files

Modify:
- internal/comm/http/auth.go (identity-aware middleware)
- internal/comm/http/server.go (ServerConfig field, middleware wiring)
- internal/config/schema.go (+ defaults) and internal/daemon/daemon.go (store construction/wiring)
- internal/session/session.go + store_sqlite.go + memory store (owner_id)
- tests: internal/comm/http/*_test.go additions, session store test

## Contract

- `IdentityFromContext(ctx) (*auth.Identity, bool)` in package http.
- ServerConfig gains `AuthStore *auth.Store` (nil = legacy single-key mode).
- Middleware order unchanged: rate limit → auth. In multiuser mode the
  existing APIKeyAuth is REPLACED by store-backed validation; health/OPTIONS
  exemptions preserved; 418 for invalid/expired keys.
- Config: `multiuser.enabled` default false; `multiuser.users_file`
  default `~/.meept/users.json5`.
- Sessions: nullable `owner_id TEXT`; migration via existing
  `migrationAddColumn` pattern (see store_sqlite.go:174). Session create in
  multiuser mode stamps Identity.UserID when available. List/read accept
  optional viewer filter; nil viewer unfiltered.

## Tasks (TDD)

1. Config schema + defaults + config test round-trip (multiuser off by
   default).
2. Identity-aware auth middleware: valid key → ctx identity; expired → 418
   with expiry message; unknown → 418. Legacy mode untouched (existing
   tests must pass unchanged).
3. Session owner_id migration (SQLite) + memory-store parity. Test:
   migration adds nullable column on old DB fixture without data loss.
4. Owner stamping on session create; list filtering by viewer. Tests:
   user A cannot list B's sessions; nil viewer sees all; legacy mode sees
   all.
5. Daemon wiring: construct Store only when enabled; pass into httpCfg;
   log line "multi-user authentication enabled (N users)". Nil-guard per
   typed-nil rules.
6. Preserve CRITICAL INVARIANT: session_id vs conversation_id handling in
   WS filters — run existing comm/http WS tests.
7. Verify: `go build ./...`, `go test ./internal/comm/http/
   ./internal/session/ ./internal/config/ -race -count=1`, vet, analyzers.

## Self-Verification Checklist

- [ ] All targeted packages green with -race
- [ ] Existing legacy-mode tests pass UNCHANGED (no edits to their assertions)
- [ ] Migration tested against pre-multiuser schema fixture
- [ ] No mutex-held I/O; two-value type assertions on payloads
- [ ] grep confirms IdentityFromContext has production callers
- [ ] No stray artifacts

## Review Checklist (orchestrator)

- [ ] Multiuser-off path identical behavior (diff of legacy tests = none)
- [ ] Expired-key rejection covered by HTTP-level test
- [ ] Session invariant (session_id/conversation_id) intact
- [ ] AGENTS.md-worthy notes reported back to orchestrator (do not edit it)
