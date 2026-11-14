# Leaf 01 — Clip Store Package

> **DISPATCH INSTRUCTION:** Implement this leaf end-to-end with TDD. Work in
> the isolation worktree (path given in your dispatch context). Do NOT run
> `git commit` or `git add` — the orchestrator commits after review. Do NOT commit. Run the
> exact verify commands at the bottom; report results + file list.

## Meta

- **Parent:** orchestrator.md (phase1-clips)
- **Depends on:** 00T gate tests committed (TESTS_VALIDATED)
- **Estimated context:** ~45K
- **Parallel group:** Wave A (with 05; file-disjoint)
- **Requirements:** R1, R3 (storage half)

## Goal

New package `internal/voice/clipstore`: content-addressed cache of.

## Scope

New package `internal/voice/clipstore`: content-addressed cache of
pre-rendered speech clips under `$MEEPT_HOME/voice_clips/` per root
Contracts C1 + C2 (quote them verbatim into the code as doc comments).

## Tasks

1. `internal/voice/clipstore/key.go` — `func ClipKey(text, vendor, model, voice string, refHash []byte) string`
   implementing C1 EXACTLY: `hex(sha256("v1" + \x1f + text + \x1f + vendor +
   \x1f + model + \x1f + voice + \x1f + hex(sha256(refHash))))`. Table test:
   any component change flips the key; stable across calls; empty refHash
   (non-clone voice) uses the hash of zero bytes.
2. `internal/voice/clipstore/store.go` — `type Store struct{ dir string }`
   with `NewStore() *Store` (uses `config.MeeptPath("voice_clips")`,
   honored MEEPT_HOME per internal/config/home.go), and methods:
   - `Has(key string) bool` — meta+pcm exist AND pcm length == meta.bytes.
   - `Get(key string) (io.ReadCloser, Meta, error)` — opens validated pcm;
     on length/parse mismatch deletes both files (treat as miss) and
     returns os.ErrNotExist.
   - `Put(key string, meta Meta, pcm io.Reader) error` — write to
     `<key>.pcm.tmp` via `os.CreateTemp(dir)`, sync, rename; then write
     meta JSON. Create dir with 0700 ONLY on first Put (Has/Get never
     create it — pins the R4 "no sidecar, no dir" test).
   - `ListKeys() ([]string, error)` — for the reload diff (leaf 02).
   - All file I/O outside any mutex; no global lock (mutexio analyzer).
3. `internal/voice/clipstore/meta.go` — `type Meta struct { Key, Text,
   Vendor, Model, Voice string; SampleRate int; Bytes int64; CreatedAt
   time.Time }` with json tags per root C2.
4. Unit tests live where the 00T suite put them; this leaf makes them pass
   (adjust only if a gate test contradicts Contract C1/C2 — then report to
   the orchestrator, do not silently rewrite the gate).

## Interface Contract (exposes to siblings)

`clipstore.ClipKey`, `clipstore.NewStore`, `Store.Has/Get/Put/ListKeys`,
`clipstore.Meta` — signatures above, frozen for leaves 02/03.

## Self-Verification Checklist

- [ ] `go build ./internal/voice/clipstore/...`
- [ ] `go test -p 2 ./internal/voice/clipstore/... -count=1` green
- [ ] `go run ./tools/analyzers/mutexio/... ./internal/voice/clipstore/...` clean (or `make mutexio` scope-equivalent)
- [ ] Dir NOT created by Has/Get (test asserts)
- [ ] Corrupt pcm file → Get deletes and misses (test asserts)
- [ ] `gofmt -l` empty on changed files

## Review Checklist (for the orchestrator)

- [ ] Key formula byte-exact vs root C1 (grep the concatenation order).
- [ ] MeeptPath used, never os.UserHomeDir / literal ~/.meept.
- [ ] 0700 on store dir, 0600 on clip files (audio of a personal clone).
- [ ] No mutex held across file I/O; no ignored errors.
