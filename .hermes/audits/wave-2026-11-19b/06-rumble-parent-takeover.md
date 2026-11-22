# Native Rumble WS STT streaming — PARENT-VERIFIED takeover

Provenance: child sa-2-b18302f2 ran 70 min then hung in a loop — it re-issued the
same `grep -rn 'RUMBLE_STT_STREAMING' <repo root>` 8+ times, each timing out at
180 s (the repo root grep walks everything; the string exists only in its own
report file). Parent stopped it. Its report (03-rumble-native-ws.md, 754 bytes)
has zero findings. Parent re-ran the lane. PROVENANCE = parent-verified.

Scope: e335ea15 native adapter, f5e384a2 proxy bypass, 26790fa9 RED gates,
7a5da96e knob docs. HEAD 7a5da96e.

## Protocol trust (the fake-vs-vendor HIGH class — CLEARED, with evidence)
The in-repo fake (stt_rumble_test.go:51-103: binary-frame counter, empty-binary
flush, partial/final/end JSON replies) mirrors a protocol that was verified
against the REAL running server before coding:
docs/plans/20261119-rumble-native-streaming/master.md:27 "Live behavior verified
by the operator and by an in-session WS probe" and :74 "Server invariants (read
from live source, verified 2026-11-19)". The running server on socrates:8000 is
a custom server, NOT stock vLLM — which is why the older research doc's "no
streaming API exists" statement does not apply to it. Not a fake-pin defect.

## Findings

### R1 (LOW) docs/research/rumble2flash-review.md contradicts the shipped adapter
The review (964fd536) concludes Rumble-2-Flash is batch/non-streaming and cannot
emit partials (true of the HF model card + stock vLLM). The shipped adapter talks
to a custom live server that DOES stream partial/final/end — verified live. The
plan tree itself flags the stale statement ("that skip text is stale",
master.md:283-284) but the research doc was never amended with the live-server
finding. Fix: one dated addendum line in the review doc pointing at master.md.
Doc-rot only; no code defect.

## Parent-verified CLEAN list (file:line at HEAD)
- Read limit: dialRumble passes wsutil.DefaultReadLimit (stt_rumble.go:48) —
  the prior-wave L7 "no read cap" concern is CLOSED by this rewrite.
- Lifecycle: run() owns out's close (single owner, :132); closeQuiet + cancelRead
  on every exit path (:135-136); pump stops via readCtx.Done (:164-165); dial
  error returns to caller (:121-123); mid-stream error fail-soft (:174-176).
- FlushNow: non-blocking coalescing send under mu, no I/O under mutex (:89-98);
  per-stream chan registered/unregistered (:138-140); matches mutexio contract.
- Partial semantics: partial(text)->Final:false, final->Final:true appended to
  finals, end->dedup remainder via rumbleUncoveredRemainder (:188-219).
  Dedup ignores whitespace and only emits the uncovered suffix — the
  "Yesterday it was"+"30 degrees" case emits nothing extra (:223-244).
- end is terminal and stops the pump via cancelRead (:217-218).
- Constructor gates: empty BaseURL -> ErrMissingRumbleBaseURL (:74-76) — the
  prior-wave dead-seam question is RESOLVED: stt_base_url is wired end-to-end
  (keys.go:270 -> providers_wiring.go:227 -> ProviderConfig.BaseURL -> this gate).
- Proxy bypass: NewSTT returns native vendors unwrapped (stt_deepgram.go:192-216,
  nativeStreamingVendor("rumble","deepgram")); PartialIntervalMs<=0 stays
  byte-identical; only batch vendors get PseudoStreamSTT. Unset = legacy path.
- PseudoStreamSTT: max-window cap 320 KB/10 s with force-finalize (pseudostream.go
  :53-58, :246-248), flush-and-keep-streaming on kick (:263-272), recognize with
  FRESH base and no lock held (:112-152), snapshot copies under a mutex used only
  around append/copy (:168-192). H3/M2 shapes from the prior wave are here.
- Config: NormalizeVoiceRumbleDefaults clamps GPU fraction to 0.20..0.25 at the
  load boundary (schema.go:5669-5682) — prior H1/M18 clamp is in place.
- 7a5da96e is comment/doc-only (commit stat: schema comment + template comment +
  drift annotation, no behavior change) — verified from the commit message + stat.
- Gates: go test -p 2 -count=1 ./internal/voicechild/providers/ -> ok 10.989s
  (all 513-line rewritten rumble test file + pseudostream + deepgram gates green);
  RED gates from 26790fa9 are now green.
- e2e suite registered: manifest ids rumble-native-stream-01 / rumble-native-
  flush-01 map to internal/voicechild/providers/stt_rumble.go (manifest.json:1560-).

## Observation (not a finding — pre-existing pattern, uncertainty flagged)
When run() exits early (end event or read error) the pump stops draining `pcm`;
the upstream feeder blocks on its send until ITS context cancels. Same shape in
the deepgram/whisper adapters; the feeder's ctx posture was not traced this turn.
If the pipeline ever feeds a never-closing pcm chan under a long call ctx, an
early adapter exit idles one producer goroutine. Needs a feeder-side trace.

## gates_run (parent):
- go test -p 2 -count=1 ./internal/voicechild/providers/ — ok (10.989s).
- go build ./... — clean (full tree, this session).
- Full suite already logged at baseline (158 ok / 2 flakes).
