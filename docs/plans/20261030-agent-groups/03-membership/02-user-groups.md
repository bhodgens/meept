# 02: User Groups (users.json5) - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks via TDD. Do NOT commit.
> No read_file on source; explore with search_files/cat. Write once, stop.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** `groups:` on User in the auth store (C4 human half) + the
  resolution join (agent groups ∪ session-owner user groups).
- **Dependencies:** 01-frontmatter.md (REVIEWED)
- **Estimated Context:** 45K
- **Concurrency Group:** B (serialized)

## Goal

Human keys carry group membership the same additive way keys carry roles, and
a multiuser session's owner contributes their groups to the session's agent
resolution - without deriving identity from key strings (issue #76 class).

## Interface Contracts (From Parent)

C4: `User.Groups []string json:"groups,omitempty"` (internal/auth/users.go).
Join rule: agent effective groups = frontmatter ∪ owner user groups WHEN the
request carries an auth-store Identity; single-user install (nil identity) =
frontmatter only. NEVER synthesize identity from the key string - if no store
identity is available under multiuser, user-group contribution is EMPTY and
a Warn notes it (fail-safe; issue #76 limitation documented by leaf 09).

## Verified Anchors

- internal/auth/users.go:81 User struct (Groups field follows Key fields'
  additive JSON pattern); store.go load/saveStore (JSON5 round-trip).
- Identity flow: internal/comm/http/identity.go (middleware attaches Identity
  to context) - the join consumes the context Identity, not the raw key.
- CLI: cmd/meept/users_cmd.go gains `--groups` on user add/update (comma list).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state |
|---------|---------|------------|----------------|------------|------------|
| T1 | User.Groups field + store round-trip (save/load preserves) | internal/auth/users.go, store.go, +test | `go test -p 2 ./internal/auth/ -run TestUserGroups` | field absent | round-trip preserves groups; empty = absent key |
| T2 | CLI --groups flag on users add/update | cmd/meept/users_cmd.go, +test | `go build -o bin/meept ./cmd/meept` | flag absent | set/clear groups via CLI; file reflects |
| T3 | Join helper: EffectiveGroups(agentGroups, identity *auth.Identity, storeLookup) []string - nil identity => agentGroups only + caller-decided Warn; identity present => union (dedup, stable order) | internal/groups/ (memberjoin.go), +test | `go test -p 2 ./internal/groups/ -run TestEffectiveGroups` | absent | matrix: nil identity, identity w/ groups, dedup, no key-string synthesis anywhere |

## Self-Verification Checklist

- [ ] No code path derives groups from the key STRING (grep for the pattern)
- [ ] Store file permissions unchanged (0600 contract preserved)
- [ ] Gate tests GREEN and unmodified; gofmt clean

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched: [paths]
2. Verify output: [last 20 lines each]
3. Issue #76 status note: [identity availability under multiuser]

## Review Checklist (For Review Agent)

- [ ] Join is union-with-dedup, stable order (deterministic resolution)
- [ ] Fail-safe on missing identity (empty contribution, not error, not synthesis)
- [ ] users.json5 schema drift check clean (config-drift annotations if needed)
