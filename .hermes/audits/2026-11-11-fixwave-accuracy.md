# Fix-wave report + initial-report accuracy audit — 2026-11-11

Question answered: **"How accurate was the initial read-only bughunt report?"**

Fix commits: `fd91f000` (docs/e2e), `b378aadc` (stamp set, via sibling tracking commit),
`fcaa2bab` (telemetry contracts), `0504cd53` (barge-in set), `bc4925c4` (C-8 wiring
loose end, parent patch). The full suite ran at final HEAD (result in the session's
gate matrix below; log at `/tmp/final-suite.log`).

---

## ACCURACY VERDICT: 19 of 20 findings CONFIRMED at fix time (95%)

| # | Initial claim | Fixer verdict | Accuracy note |
|---|---|---|---|
| C-1 | HIGH — HandleInterrupt unreachable | **CONFIRMED** | Exact. The one biggest catch of the campaign. |
| C-2 | HIGH — stale residue written post-window | **CONFIRMED** | Exact. Fixer's first fix attempt was itself wrong and caught by the existing pin — corrected to read-time tagging. |
| C-3 | HIGH — stamp poisons `^`-anchored classifier | **CONFIRMED** | Exact; probe numbers matched. |
| C-4 | MED — model-history vs store divergence | **CONFIRMED** | Documented + pinned (conservative branch, as instructed). |
| C-5 | MED — stamp in trajectory + capture log | **CONFIRMED** | Fixed with C-3 via the shared helper. |
| C-6 | MED — forgeable bracket inside user boundary | **CONFIRMED (report-only)** | Proposed patch returned as instructed; not applied (security package unowned). |
| C-7 | MED — layout does not round-trip | **CONFIRMED** | Fixed by dropping the zone abbreviation from the layout. |
| C-8 | MED — daemon zone vs employee zone | **CONFIRMED** | Fixed via fallback chain; production wiring I landed at `bc4925c4`. |
| C-9 | MED — Cursor does not echo | **CONFIRMED** | Fixed as documented (echo semantics). |
| C-10 | MED — `truncated` conflates 3 conditions | **CONFIRMED** | Fixed: eviction-only + new `has_more`. |
| C-11 | MED — CLI stop() hang | **CONFIRMED** | Fixed: 5 s bounded round-trips. |
| C-12 | MED — GUI parity gap | **CONFIRMED (report-only)** | Follow-up proposal returned; Flutter out of scope. |
| C-13 | MED — `heard` lies about delivery | **CONFIRMED** | Fixed: append only on successful enqueue. |
| C-14 | MED — link floor events missing | **CONFIRMED** | Fixed via `SetEventSink` + `runCcid` keying. |
| C-15 | MED — e2e policy violated | **PARTIALLY WRONG** | The deferral was **already closed** by sibling commit `37fdf1f3` **before** the fixers dispatched — the initial report's grep predates that landing. See below. |
| C-16 | MED — AGENTS.md drift | **CONFIRMED** | All four sub-items fixed; the post-fix contract is now documented. |
| C-17 | LOW — Deepgram/LiveKit misattribution | **CONFIRMED** | All four sites fixed; FreeSWITCH provenance corrected. |
| C-18 | LOW — dead/stale code | **CONFIRMED** | `playbackRestarted` removed (one comment mention remains, accurate). |
| C-19 | LOW — wall-clock arithmetic | **CONFIRMED** | Fixed: monotonic wall↔mono anchor pair under a dedicated `anchorMu` (fixer caught a self-deadlock the naive fix would have introduced). |
| C-20 | MED — resurrection + `found` ignored | **CONFIRMED** | Fixed via `everSeen` set — the fixer **strengthened** my proposed bare `HasRing`, which would have had the same eviction flaw. |

**Severity calibration:** all 3 HIGHs were real and all 3 fixed. Of the 12 MEDs, 10
confirmed exactly, 1 was report-only by design, 1 (C-15) was stale at fix time. Both
LOWs confirmed. One initial LOW (V-6 in the pre-consolidation report) had already been
downgraded by probe before dispatch; it is not counted here.

---

## THE ONE MISS: C-15 was stale on arrival

The initial report (written ~16:45) verified `grep "voice.local.events"
e2e/manifest.json` → 0 and concluded the deferral was recorded nowhere durable.

Sibling commit **`37fdf1f3`** ("test(e2e): voice-local event telemetry suite — closes
the d0657f4b deferred gap") had already landed the suite, the manifest entries, and the
scenario by the time the report was finalized — but my verification grep ran against the
pre-landing tree and I did not re-check `git log` between reading the deferral claim and
sealing the report. The bughunt skill's own rule ("re-verify every finding against HEAD
before the report; siblings fix things mid-audit") is exactly the rule I violated.

**Consequence of the miss:** minor — the fixer correctly detected the existing suite,
added only the `docs/bugs-and-gaps.md` record, and no duplicate suite was created. But
the initial report told the user a policy violation was open when it had been closed
~50 minutes earlier.

**Calibration lesson (matches the skill's stale-finding history):** auditor-sourced
findings verified once early in a session must be re-verified at seal time if the tree
is shared. Two of the campaign's own disciplines — "HEAD moved mid-wave" (recorded as a
finding list note) and "re-verify at seal" — conflict when the second is skipped.

---

## SECONDARY FINDINGS THE INITIAL REPORT GOT RIGHT BUT THE FIXERS SHARPENED

- **C-2's fix shape:** my initial report suggested either carrying the deadline in
  `localReadItem` OR pre-enqueue discard. The fixer tried the first, discovered it
  re-creates the defect from the other side (consume-time re-judgment), and landed
  read-time tagging + pre-enqueue carry-drop — strictly better than my suggestion.
- **C-20's fix shape:** my initial report suggested a bare `HasRing`. The fixer proved a
  live-map probe has the same eviction flaw and built the `everSeen` set instead —
  again strictly better.
- **C-19's fix shape:** the fixer's first naive monotonic fix self-deadlocked
  (`Bump` holds `mu` while calling `clockNano`, so the anchor must ride a dedicated
  `anchorMu`). Caught by the fixer's own gates pre-commit. My initial report would have
  walked into the same trap if I had fixed it directly.

---

## WHAT THE FIXERS DID THAT THE REPORT DID NOT ANTICIPATE

1. **Sibling absorbed one fixer's staged work** into a docs-tracking commit
   (`b378aadc` "tracking — leaf 01 COMPLETE" carries the entire stamp fixer's diff).
   Verified: the 5 owned files are byte-identical to the gated state (`git diff`
   empty). Disclosed in the fixer's report; noted here because the commit message does
   not describe the code it carries — the exact mislabeled-commit class the bughunt
   skill warns about.
2. **`0504cd53` carries a foreign baseline regeneration** (`.githooks/deadcode-baseline.txt`
   −49 lines) unrelated to the voice diff — the fixer disclosed it (repo-wide reachable
   set shifted by sibling park-backend work; hook's own `--rebaseline` path). No
   `internal/voice` entries changed. This is disclosed, verified, and acceptable, but it
   makes the commit message slightly over-scoped.
3. **Two fixers used `--no-verify`** — both disclosed, both with their own gates green
   and foreign-file hook failures (pre-existing U1000s), consistent with the ledger
   rule. My own C-8 wiring commit (`bc4925c4`) did the same.

---

## GATE MATRIX AT FINAL HEAD (`bc4925c4`)

| Gate | Result |
|---|---|
| `go build ./...` | rc=0 |
| `go vet ./...` | rc=0 |
| Full suite `go test -p 2 -count=1 ./...` | **152 pkgs ok, 0 FAIL** at final HEAD (log `/tmp/final-suite.log`; the shell's `RC=` echo was lost to background-termination, but the log shows all packages ran and none failed) |
| `go test -count=1` voice / rpc / daemon / cmd/meept / voicechild | all ok (per-fixer gates, `-count=1`) |
| `go test -race ./internal/voice/` | ok (73.8s → 76.3s across two runs) |
| e2e `voice-local` suite | ok 7/7 (35s), incl. the telemetry lifecycle scenario |
| `make graphs-check` | green per fixer in detached worktree; shared-tree run blocked on sibling WIP at the time; needs one re-run at final HEAD |
| gofmt | clean on all fixer-owned files |

---

## DISCLOSURE LEDGER

- Full-suite at final HEAD completed: 152 pkgs ok, 0 FAIL.
- Graphs re-check at final HEAD was **red** (loop.go offsets shifted, as predicted);
  fixed by `make graphs` and committed as `6f941623` (3 generated files, +33/-33 —
  offsets only, no topology change). `make graphs-check` now green.
- C-6 and C-12 are fixed-proposals, not code: C-6 needs the security owner; C-12 needs
  Flutter work.
- The deadcode baseline regeneration in `0504cd53` was verified by the fixer only ("no
  internal/voice entries changed"); I did not independently diff the baseline file.
- The `.gitignore` duplicate line remains modified in the tree — foreign (sibling),
  left untouched all session.
- C-18's minor leftovers (localBargeResidueQuiet alias, poll-comment name,
  previewPrompt duplication) were deliberately left: they sit in files whose fixers had
  already committed, and they are cosmetic.

---

## NEXT STEPS (ranked)

1. ~~Run `make graphs` + `make graphs-check` once at final HEAD~~ — DONE: `6f941623`,
   graphs-check green.
2. Hand C-6's proposed patch to the security owner (prompt_guard.go) — it changes the
   stamp's placement contract, so it should land with the voice owner present.
3. Hand C-12's proposal to the Flutter lane (voice_service.dart `events()` + poller).
4. Remaining C-18 cosmetics can ride any future internal/voice commit.