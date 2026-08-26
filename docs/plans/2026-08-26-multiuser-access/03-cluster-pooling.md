# Leaf 03 — Cluster user pooling via peer sync

DISPATCH INSTRUCTION: Implement this leaf end-to-end. Do NOT commit.
Parent: master.md. Depends on: 01-auth-store (REVIEWED). Est. context ~60K.

## Goal

Pool users across cluster nodes using the existing backup/peer-sync channel:
a user valid on node A authenticates on peer B while B still sees A as an
active peer. Nodes leaving the cluster lose their foreign users.

## Files

Modify:
- internal/rpc/backup_sync.go (payload extension + handlers)
- internal/backup/* sync puller / config syncer (whichever carries payloads —
  inspect first; extend, do not restructure)
- internal/auth/store.go ONLY if a small helper is missing (no signature changes)
- tests in the touched packages

## Contract

- Sync status/pull payload gains `users []auth.User` where each User has
  `OriginNode` set to the SENDING node's id.
- Receiver calls `Store.MergeForeign(users, activePeers)` with activePeers =
  currently-known peer set (existing peer tracking supplies this).
- Local users always authoritative; merge is additive for foreign users.
- Multiuser disabled → no users exchanged (field empty/omitted).

## Tasks (TDD)

1. Inspect existing sync payload shape and peer-set source; report both in
   your summary before writing code (one search pass).
2. Extend payload with users field; sender populates from local store with
   OriginNode stamped; only when multiuser enabled.
3. Receiver-side merge call wired into the pull path; errors logged not fatal.
4. Tests: exchange round-trip between two stores; node-drop eviction;
   local-authority preservation; disabled-mode no-op.

## Self-Verification Checklist

- [ ] Package tests green with -race; vet + analyzers clean
- [ ] MergeForeign called with live peer set, not a stale snapshot
- [ ] Disabled mode exchanges nothing (test asserts empty)
- [ ] No restructuring of existing sync machinery beyond the extension
- [ ] No unused exports

## Review Checklist (orchestrator)

- [ ] Wire format documented in the payload struct comment
- [ ] Eviction test proves foreign users vanish when peer set shrinks
- [ ] No circular dependency internal/auth ↔ internal/rpc
