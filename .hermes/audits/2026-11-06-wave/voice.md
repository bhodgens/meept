# Voice sidecar audit — findings

- Scope: internal/voice, internal/voicechild(+providers), daemon voice wiring (voice_wiring.go, voice_employee.go), cmd/meept-voice, cmd/meept voice subcommands, telnyx integration, supervisor gate, inbound registry-acquire fix 18a1cdbe, swept/restored commits 9f8a974c + c265636d, restore-gap check vs 0e36ed76 (beyond the two permission rows fixed in 6b42213b).
- Range: 0a446a21..HEAD
- HEAD SHA: 6b42213b
- Timestamp: 2026-11-06T18:42Z
- Method: read-only static review; no daemons, no telnyx connections, no real calls.

## Findings

## F-1 [HIGH] e2e manifest voice entries lost in the restore — suites are orphaned and unroutable
- `e2e/manifest.json` (HEAD)
- Evidence: at 18a1cdbe (leaf 15, pre-sweep) the manifest carried 5 `path_map` entries
  (`internal/voice/`, `internal/voice/telnyx/`, `internal/voiceframe/`, `internal/voicechild/`,
  `cmd/meept-voice/` → `[voice-lifecycle, voice-bargein, voice-concurrency]`) and 3 `suites`
  entries. Sweep commit 9f8a974c deleted them; restore commit c265636d landed the suite
  *files* (`e2e/suites/voice-{lifecycle,bargein,concurrency}/...` all PRESENT at HEAD,
  verified via `git cat-file -e`) but NEVER re-added the manifest rows. `grep -i voice
  e2e/manifest.json` at HEAD → zero matches; suites count 86, no voice suite.
- Why it's a bug: (a) `make e2e-affected` (pre-commit gate [18/18] and CI) can never route a
  change under internal/voice* to the voice suites — the three restored suites never run in
  CI or pre-commit; (b) AGENTS.md's e2e policy requires a manifest `path_map` + suite entry in
  the same commit as a package, so the restore left the repo violating its own gate contract;
  (c) pre-commit [18] blocks NEW packages without manifest entries but does not catch
  files-restored-under-existing-dirs-with-entries-deleted, which is exactly this state.
- Fix shape: restore the 5 path_map entries + 3 suite objects from
  `git show 18a1cdbe:e2e/manifest.json` (the post-leaf-15 version with the inbound
  registry-acquire gap closed — note the SKIPPED scenario names there predate the fix;
  re-check scenario names against the actual test bodies before re-adding).

## F-2 [MEDIUM] Root AGENTS.md voice sections lost in the restore — violates the same-commit maintenance invariant
- `AGENTS.md` (HEAD)
- Evidence: pre-sweep (9f8a974c^) root AGENTS.md had 11 voice mentions: the directory-chain
  pointer `internal/voice/ -> internal/voice/AGENTS.md` in the header block, the build line
  `go build -o bin/meept-voice ./cmd/meept-voice`, the **Voice** row in the Key Components
  table, and the "### Voice sidecar (phone channel)" Critical Invariants section. At HEAD:
  zero. The per-package `internal/voice/AGENTS.md` WAS restored by c265636d, but the root
  file was not — so agents working in internal/voice/ no longer auto-load the pointer and
  the root's own rule ("a rule whose full text lives in a package AGENTS.md was updated in
  the same commit as its summary here") is violated. Makefile voice targets DID survive
  (build-voice present at HEAD) — the drop is specific to AGENTS.md + manifest.
- Why it's a bug: stale agent guidance is explicitly called out in AGENTS.md as causing more
  bugs than no guidance; the root summary rows (secret lanes, one-call-per-employee,
  never-quota-parked) are invisible to a fresh agent until it happens to open the subdir file.
- Fix shape: re-add the four AGENTS.md blocks from `git show 9f8a974c^:AGENTS.md`
  (lines 16, 38-39, 202, 324-341 region).

## F-3 [MEDIUM] max_concurrent_calls <= 0 means "unlimited" in the registry but "block everything" in the outbound dialer pre-gate
- `internal/voice/dial.go:368-370, 411-416` vs `internal/voice/dial.go:487-501`
- Evidence: `CallRegistry.Acquire` documents and implements the sentinel as unlimited:
  "// cap <= 0 means unlimited (… zero config value must not make every Acquire busy).
  `if r.cap > 0 && len(r.live) >= r.cap`". `internal/voice/AGENTS.md` (C8) agrees:
  "cap <= 0 means unlimited". But the dialer's pre-gate `registryGate` computes
  `if len(live) >= d.registryCap()` — with cap 0 (or negative) this is always true, so
  `DialOut` refuses EVERY outbound call with `ErrBusy: at capacity (0)`. Stale comments
  cement the wrong reading: `NewRegistry` doc says "A cap of 0 refuses every Acquire with
  ErrBusy" and `registryCap` says "0 means zero capacity … no 'unlimited' sentinel" — both
  contradict Acquire's own comment and the package contract.
- Why it's a bug: an operator setting `max_concurrent_calls: 0` intending unlimited gets
  inbound calls accepted without cap while outbound dials are permanently deadlocked —
  a silent half-broken voice deployment; and three sites in one file disagree on the
  sentinel semantics, which is where the next regression will come from.
- Fix shape: one helper (e.g. `capBlocked := r.cap > 0 && len(live) >= r.cap`) shared by
  Acquire and registryGate, or normalize config so <=0 maps to an explicit sentinel;
  correct the two stale comments; pin `TestDialRefusals` with cap=0 expecting allow.

(none else yet)

## Audited-clean

- **Secret leakage lanes (clean).** Start token: `crypto/rand`, STDIN pipe only, never
  argv/env/log (`internal/voice/childproc.go:321-340`); child `SpawnCommand` argv carries
  only socket path + audio dir; child env is minimal (`VOICE_ONNX_LIB` path only).
  `KeysFrame` redacts via both `String()` and `LogValue()` (`internal/voicechild/keys.go:24-37`);
  provider keys go daemon->child over the C1 keys frame and live in child memory only.
  Telnyx token resolves in-daemon via `Broker.Value` (`voice_wiring.go:141`), never leaves
  the process; `telnyx.postCall` error paths wrap status + body snippet only, headers never
  included (`internal/voice/telnyx/client.go:65-112`). No `slog`/`log` call in scope prints
  an apiKey/token variable (grep sweep `internal/voice*`, `cmd/meept-voice`).
- **Webhook/media auth (clean).** Ed25519 verification is mandatory fail-closed: missing
  signature/timestamp header = error not pass, 300s skew window, length checks before
  `ed25519.Verify` (`telnyx/signature.go`); routes refuse to arm enabled without both
  verifiers (`internal/comm/http/voice.go:157-158`); media socket signature verified BEFORE
  WS upgrade (403, no socket). `loopback_plain_addr` binds only loopback, non-loopback /
  unparseable = named startup error (`voice_plain.go:27-40`).
- **Dialer lifecycle (clean).** `DialOut` ordering effect-claim -> gates -> POST ->
  TrackOutbound -> Acquire -> receipt/Complete; every pre-POST refusal abandons the effect;
  acquire-lost race hangs up + finishes the leg (dial.go:275-289). `finishCall` is
  once-per-ccid under mutex; no-answer timer stopped by `mgrDone`/`NotifyAnswered`;
  hangup-for-unknown-id idempotent; `call.answered` untracked refused (manager.go:191-196);
  outbound legs never AnswerCall'd. Redelivery on a transient 500 self-heals the
  answered-before-TrackOutbound window.
- **Supervisor gate (clean).** Fail-open per sentence with hard timeout; nil chatter = the
  only sanctioned allow-all degrade; quota never parks a call — matches
  `internal/voice/AGENTS.md` C6.
- **Inbound registry-acquire fix 18a1cdbe (clean).** Spawn seam Acquires
  `(ccid, default_employee, from)` with empty-default-employee a named error
  (`voice_wiring.go:198-204`); the e2e suite pins same-employee second call -> busy refusal.
- **Child confinement pins (clean).** jargo import pins present at HEAD
  (`internal/voice/import_test.go`, `internal/voiceframe/pin_test.go` restored by c265636d).
- **Restore-gap beyond permission rows (checked).** All 88 sweep-deleted voice files verified
  PRESENT at HEAD (`git cat-file -e` loop), including `internal/comm/http/voice*.go`,
  `configui/sections_voice.go`, `cmd/meept-voice/main.go`, e2e suites,
  `scripts/leaf14-live-gate.py`, and the `[voice]` template block in `config/meept.json5`
  (byte-comparable to pre-sweep). Makefile `build-voice`/install rows survived the sweep.
  Deltas 0e36ed76..HEAD on voice paths are only: voice_wiring.go +12 (leaf-15 inbound
  Acquire), dial.go ±4, AGENTS.md(subdir) — all intentional. The TWO gaps found are the
  non-file surfaces: manifest (F-1) and root AGENTS.md (F-2).

### Notes / observations (not scored as findings)
- `internal/voice/registry_test.go.staged` and the e2e `*.staged` files restored by c265636d
  are dead artifacts (`.staged` suffix is excluded from builds) — hygiene cleanup candidate,
  not a bug.
- Inbound calls produce a `voice call ended` log line but no auditlog row; only outbound
  dials write `voice.dial` audit records. C9's contract names the dial row only, so this is
  plausibly intentional — flag for product confirmation if call-detail auditing is expected.
- Pre-sweep manifest@18a1cdbe listed the inbound-gap SKIPPED scenarios as still skipped,
  yet 18a1cdbe IS the fix commit; scenario-name drift must be re-checked when restoring
  F-1 rows.


## Wave closeout (parent, 2026-11-06)
- F-1 CLOSED by bdbe43f5 (manifest voice entries restored; manifest suite ok).
- F-2 CLOSED by bdbe43f5 (AGENTS.md voice blocks restored).
- F-1/F-2 were captured against 6b42213b; the parent's sweep predated them.
- F-3 (max_concurrent_calls<=0 sentinel conflict) OPEN — parent fixing.
- F-3 CLOSED (parent): dialer registryGate now honors the cap<=0 unlimited
  sentinel (matches CallRegistry.Acquire); dial_test.go over-cap pin moved to
  cap-1 + new cap-0-must-succeed pin; full internal/voice suite ok.
