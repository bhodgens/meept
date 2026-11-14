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

Daemon side (`internal/daemon`): compute the clip render set for THIS call's
resolved TTS identity (the same identity the keys frame just carried), diff
against the store, and ship `clip_render` C1 frames to the voice child —
background, non-blocking, ONLY when the sidecar is actually running for that
call.

The trigger is the per-call `setup` frame, immediately after `sendKeysFrame`
(`internal/daemon/voice_keys.go`, the `routeFrame` EventSetup branch), NOT
daemon start and NOT child spawn. Before the keys frame the child has no TTS
config or key (its provider is constructed from that frame), so a render
frame sent earlier can only fail.

## Tasks

1. `internal/daemon/voice_clip_planner.go` (new file):
   ```go
   // ClipPlanner diffs the resolved lane pool against the clip store and
   // emits C1 clip_render frames for misses. Construction is cheap and
   // inert: nothing reads or writes the store until Run.
   type ClipPlanner struct { store *clipstore.Store; lanes func(employeeID string) []voice.LaneLine; ... }
   func NewClipPlanner(...) *ClipPlanner
   // Run renders the diff for the given resolved TTS identity (vendor/
   // model/voice from voice.ResolveProfile for this call's employee + leg;
   // refHash is nil for every shipped vendor - see root C1).
   func (pl *ClipPlanner) Run(ctx context.Context, ccid string, id TTSIdentity, send func(voiceframe.Frame) error) error
   ```
   Cap 32 clips per frame (Contract C3), shard the rest; the run is
   fire-and-forget on the call's ctx.
2. `internal/daemon/voice_wiring.go` — mount the planner in the SAME
   EventSetup branch that emits the keys frame, after `sendKeysFrame`
   returns (that call site also has the employee id and the resolved
   profile in hand; `voice_wiring.go:1008` is where `FillerLines` wires the
   talk loop):
   - Gate (R4, pinned by test): the planner runs ONLY when (a) the call has a
     live voice child connection, AND (b) the resolved TTS vendor, model, and
     voice are all non-empty. No sidecar / no keys frame ⇒ zero planner
     calls, zero store writes, store dir never created.
   - Config-reload hook: on a voice config change, recompute the lane pools
     and schedule one planner pass per LIVE ccid (background goroutine,
     ctx = daemon lifetime). Never block the reload path; a reload with no
     live calls writes nothing.
3. `internal/daemon/voice_keys.go` — extend the keys wire (additive, same
   no-omitempty convention comments) so the child can compute the same clip
   keys without reading config: `tts_ref_hash` (hex sha256 of the reference
   audio). No vendor consumes a reference clip today, so the field carries
   the empty digest `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`.
   Old children ignore the field (pin in test).

## Interface Contract (exposes to siblings)

Consumes: `clipstore` (01), `voice.VoiceLanes` (05), `voiceframe` clip
event shapes (03). Exposes: nothing used by other leaves. Behavioral
guarantee: "a `clip_render` frame for this call's misses is sent after the
keys frame for the same ccid, on a background goroutine". It is NOT a
guarantee that the clips exist before the first filler: a filler that fires
before a render completes simply takes the live-synth path (today's
behavior), and the next one hits the store.

## Self-Verification Checklist

- [ ] `go build ./internal/daemon/...`
- [ ] `go test -p 2 ./internal/daemon/... -count=1` green
- [ ] R4 unit test: voice disabled / no child ⇒ planner never invoked,
      store dir untouched (assert os.Stat error).
- [ ] Ordering test: the `clip_render` frame for a ccid is written AFTER
      that ccid's keys line, never before and never with an empty call_id.
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
