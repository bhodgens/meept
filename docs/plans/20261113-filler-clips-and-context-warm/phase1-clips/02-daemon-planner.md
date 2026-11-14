# Leaf 02 — Daemon Clip Planner (sidecar-gated)

> **DISPATCH INSTRUCTION:** Implement this leaf end-to-end with TDD. Work in
> the isolation worktree (path given in your dispatch context). Do NOT run
> `git commit` or `git add` — the orchestrator commits after review. Do NOT commit. Run the
> exact verify commands at the bottom; report results + file list.

## Meta

- **Parent:** orchestrator.md (phase1-clips)
- **Depends on:** 01 (clipstore), 05 (lanes), 03 (C3 frame fields exist)
- **Estimated context:** ~60K
- **Parallel group:** Wave B (with 03; contract-pinned, see serialization note in orchestrator Architecture)
- **Requirements:** R3, R4 (the gate lives HERE)

## Goal

Daemon side (`internal/daemon`): compute the clip render set per employee.

## Scope

Daemon side (`internal/daemon`): compute the clip render set per employee
voice config, diff against the store, and ship `clip_render` C1 frames to
the voice child — background, non-blocking, ONLY when the sidecar is
actually running.

## Tasks

1. `internal/daemon/voice_clip_planner.go` (new file):
   ```go
   // ClipPlanner diffs the resolved lane pool against the clip store and
   // emits C1 clip_render frames for misses. Construction is cheap and
   // inert: nothing reads or writes the store until Run.
   type ClipPlanner struct { store *clipstore.Store; lanes func() []voice.LaneLine; ... }
   func NewClipPlanner(...) *ClipPlanner
   // Run renders the diff for the given resolved TTS identity (vendor/
   // model/voice/refHash from the keys lane inputs — NOT the key VALUES,
   // the identity strings; refHash comes from the configured clone
   // reference file content, hashed once at load).
   func (pl *ClipPlanner) Run(ctx context.Context, ccid string, id TTSIdentity, send func(voiceframe.Frame) error) error
   ```
   Cap 32 clips per frame (Contract C3), shard the rest; per-call planner
   run is fire-and-forget on the call's ctx.
2. `internal/daemon/voice_wiring.go` — mount the planner at the child
   spawn/setup site (near the existing keys-frame emit, ~voice_wiring.go:
   1008 where FillerLines wires the talk loop):
   - Gate (R4, pinned by test): planner runs ONLY when (a) the voice child
     process is up for this call, AND (b) TTS vendor+model+voice all
     resolved non-empty, AND (c) at least one voice employee is
     configured. No sidecar ⇒ zero planner calls, zero store writes,
     store dir never created.
   - Config-reload hook: on voice config change, recompute lane pools for
     live identities and schedule a planner pass (background goroutine,
     ctx = daemon lifetime). Never block the reload path.
3. `internal/daemon/voice_keys.go` — extend the keys wire (additive, same
   no-omitempty convention comments) so the planner and the child agree on
   TTS identity strings without re-reading config: `tts_ref_hash` (hex
   sha256 of the clone reference audio, empty hash for preset voices).
   Old children ignore it (pin in test).

## Interface Contract (exposes to siblings)

Consumes: `clipstore` (01), `voice.VoiceLanes` (05), `voiceframe` clip
event shapes (03). Exposes: nothing used by other leaves except the
behavioral guarantee "clip_render arrives at least once per call before
the first filler can fire, for pool lines missing from the store".

## Self-Verification Checklist

- [ ] `go build ./internal/daemon/...`
- [ ] `go test -p 2 ./internal/daemon/... -count=1` green
- [ ] R4 unit test: voice disabled / no child ⇒ planner never invoked,
      store dir untouched (assert os.Stat error).
- [ ] Reload test: change a lane line → exactly one clip_render frame
      containing only the new line's key.
- [ ] Sharding test: 40-line pool ⇒ frames of 32+8.
- [ ] `gofmt -l` empty.

## Review Checklist (for the orchestrator)

- [ ] No TTS key material in any planner log line or frame (identity
      strings + hashes only; audit-secrets-value-callers must stay green).
- [ ] Reload path never holds a mutex across file I/O.
- [ ] Boot never blocks on the planner (spawn path timing unchanged when
      store is fully warm — test asserts planner.Run off the critical
      path).
