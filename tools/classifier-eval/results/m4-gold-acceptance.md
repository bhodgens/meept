# M4 Gold-Replay Acceptance Results

Date: 2026-09-10. Acceptance gate (master.md): system accuracy > 86.8%
(chain-only floor) on the 48-case adjudicated gold replay.

## Policy comparison (same replay, same fold rules)

| policy | A routes (correct) | chain | system acc | verdict |
|---|---|---|---|---|
| double-confidence (A margin; B prob>=0.7 + agreement) | 7 (5) | 41 | **84.56%** | FAIL → **WITHDRAWN — legacy, sub-floor (CORRECTIONS 1/4)** |
| **tfidf-veto (ALT-5): A routes only when tfidf concurs** | 2 (2) | 46 | **87.35%** | **UNVALIDATED** — sub-floor (CORRECTIONS 1/2/4) |

## Interpretation

- The adopted double-confidence policy (iter-17 measurement, 84.56%) is
  confirmed on the acceptance run: FAIL vs the 86.8% gate. Its Door A
  precision on replay is 71% (5/7) — the two wrong routes cost more
  than the coverage earns.
- The tfidf-veto row: 100% Door-A precision (2/2) by requiring
  character-ngram agreement, pushing the rest to the chain. Coverage
  drops to 2/48 (4%) — the cost of the gate — but the system floor
  (chain at 86.8%) dominates the arithmetic. **This row is NOT a
  measurement produced by the committed script** (see CORRECTIONS 1).

## Decision consequences (for FINAL-REPORT and wiring)

1. **The wiring candidate would be tfidf-veto centroid**, not bare
   centroid and not double-confidence — but see CORRECTIONS 1: the
   supporting number is unvalidated and the routed sample is 2 cases.
   Do not wire on it until it is re-measured (see CORRECTIONS 2).
2. Door B (ModernBERT probe) remains benched: its rescue precision is
   below the chain floor on this data. Revisit only after the outcome
   loop harvests enough corrected-row data to retrain on REAL
   misroutes.
3. Coverage honesty: 87.35% is chain-dominated. The prefilter's value
   at this corpus size is precision (never misroute), not coverage.
   Coverage grows as the harvest loop feeds corrected examples back
   into the centroids.

## Artifacts

- m4_gold_acceptance.py — the acceptance run (both policies)
- m4-gold-acceptance.json — the committed double-confidence run (FAIL)
- alt_methods_sweep.py — the 5-variant comparison (4cf71b07)
- alt-methods-correction.md — the 87.35% claim correction (2026-09-10)
- This file — the acceptance record

## CORRECTIONS (2026-09-12 audit wave)

Appended, not overwritten: the original table cells above stand as the
history of what was claimed; the entries below are the corrections of
record.

### 1. The tfidf-veto PASS verdict is UNVALIDATED (was: PASS)

The only committed producer of a results artifact on this file's date —
`m4_gold_acceptance.py` at 5fc03637 — implemented ONLY the
double-confidence policy and wrote
`results/m4-gold-acceptance.json = {"policy":"double-confidence","a_routes":7,
"a_correct":5,"chain":41,"system_accuracy":0.8456,"verdict":"FAIL"}`. No
committed script produced the 87.35% row: its only tool,
`alt_methods_sweep.py`, prints to stdout and writes no artifact, and it
needs the UNTRACKED replay corpus plus a live embed server on :8090.
A number no one can re-derive from the repository is not evidence, so the
veto verdict is recorded as **UNVALIDATED** everywhere it is cited (this
table; `docs/plans/classifier-iteration/FINAL-REPORT.md`;
`internal/agent/tfidf_veto.go`). `m4_gold_acceptance.py` has since been
extended to run BOTH policies and write one artifact per policy
(`results/m4-gold-acceptance-<policy>.json`, write-once — it never
overwrites the committed `.json`), but neither policy can be re-run here:
the replay corpus is gitignored and :8090 is not always up.

### 2. Acceptance sensitivity / coverage floor

The verdict flips on one route decision at n=48. 41 of the 48 cases are
chain-destined and each is credited at the hard-coded `CHAIN = 0.868`
constant, so:

| decomposition | score |
|---|---|
| 2/2 routes correct + 46 chain | 87.35% |
| 1/2 routes correct + 46 chain | 85.27% — below the 86.8% gate |
| 0 routes + 48 chain | 86.80% — equals the gate; strict `>` means FAIL |

87.35% is therefore 2 correct routes plus 46/48 case credit at an
unverified constant, not a measured win over 84.56%. Effective routed
sample = 2 cases. `m4_gold_acceptance.py` enforces a coverage floor
`MIN_ROUTED = 20`: when fewer than 20 cases are routed it reports
`verdict: "INSUFFICIENT_COVERAGE"` instead of PASS/FAIL (verified:
`verdict_for(19,19,29,48) = INSUFFICIENT_COVERAGE`,
`verdict_for(20,20,28,48) = PASS`). Both rows above are below that floor
(7 and 2 routed respectively), so **on this 48-case ruler no PASS/FAIL
verdict exists for EITHER policy** — the 84.56% / 87.35% *values* are
unchanged; only their PASS/FAIL framing is withdrawn, for both rows. The
committed `m4-gold-acceptance.json` (`"verdict": "FAIL"`) predates the
floor and is retained only as the historical record: it is not a current
verdict for the double-confidence policy. The per-policy artifact the
NEXT scoring run writes WILL carry `min_routed` / `coverage_floor` so a
reader can see that from the payload alone — no such artifact is committed
yet (the only committed artifact, `m4-gold-acceptance.json`, has neither
field).

**Case share vs score share (corrected 2026-09-13).** An earlier revision
of this section said "95.8% chain credit". That conflated two shares:
46 of 48 CASES are chain-destined (95.83%), but the chain's credit is
46 × 0.868 = 39.93 of the 41.93 scored points = **95.23% of the score**
(`46×0.868 / (2 + 46×0.868)`; the two correct routes contribute the rest).
Both shares are high; they are not the same number and the verdict rests
on the case count (46 cases), credited at the constant.

### 3. Corpus <-> replay disjointness guard (train-on-test)

The models are fit on the committed corpus; the ruler is the adjudicated
replay. They were not disjoint: the replay case "implement the plan using
subagents" is byte-identical to committed corpus row `h18-planexec-001`
(case_key match; that row's own note records it was aligned to "the
adjudicated replay gold for this verbatim text"). The committed script now
runs `eval_harness.replay_disjointness()` before scoring (exact case_key
match, plus cosine > 0.95 when the embed server is up) and REFUSES the run
(exit 2) on any leak — it never silently drops the case, because dropping
it would move the denominator. Reproduce the guard:
`python3 tools/classifier-eval/m4_gold_acceptance.py --check-overlap`
(currently reports the 1 leak). Resolving the leak (remove the corpus row
or the replay case) is a measurement-protocol decision, not a code fix.
The harvest path (`harvest_wave5.py`) applies the same ruler guard when
splicing new corpus rows.

### 4. Guard hardening + payload evidence (2026-09-13 fix wave)

Verified at HEAD before fixing, with read-only runs of the committed
Python. At the time of that review a grep for `replay_disjointness` /
`MIN_ROUTED` / `INSUFFICIENT_COVERAGE` found no test and no make/CI target,
so reverting any of these guards broke nothing. That gap is now CLOSED:
`make classifier-eval-selftest` (Makefile) and the CI job in
`.github/workflows/code-quality.yml` run
`m4_gold_acceptance.py --self-test`, so reverting a guard turns the gate
red.

- **Self-test added (was: unpinned).**
  `python3 tools/classifier-eval/m4_gold_acceptance.py --self-test`
  (stdlib + numpy; no embed server, no replay corpus) now pins: the known
  leak is reported; an empty sample is not green; a zero-norm row and a
  non-finite row are not green; the allowlist exempts similarity only; the
  floor boundary at 19 vs 20 routed cases; that the run's GUARD-1 refuses a
  leaked ruler AND that `run_full` actually calls it (the call site is
  asserted in source, so deleting the guard block fails the self-test); and
  that the guard stats carry a replay `case_key`, never the ruler text.
- **Empty-sample hole (was: reported green).** `check_overlap_only`
  returned 0 on a replay file that parsed to zero cases, printing
  `ruler disjoint: 0 replay cases vs 389 corpus cases`. It now returns a
  distinct exit 4 with a refusal message, and `replay_disjointness` raises
  `EmptyRulerError` on an empty ruler — an empty sample is never
  "disjoint", and scoring it measures nothing. Exit 4 now means a
  GENUINELY empty list: the ruler is parsed with the corpus JSON5 parser
  (`eval_harness.parse_json5`, escape-aware), not a regex that required
  `input` to be the first quoted key, so a ruler written in the repo's own
  JSON5 style (unquoted keys, or `id` before `input`) parses instead of
  faking an empty sample and latching exit 4.
- **Zero-norm hole (was: silently disjoint).** `_as_unit` computed
  `0/(0+1e-12) = 0`, and the Embedder deliberately stored a zero vector
  when the server returned one, so such a row was never flagged.
  `_as_unit` now raises `DegenerateVectorError` on a zero or non-finite
  norm (naming the rows), the Embedder counts and warns on degenerate
  server rows, and the guard reports how many rows it actually compared.
  The remaining `+1e-12` normalisations in the acceptance run's centroid
  build (`m4_gold_acceptance.py`) and in `CentroidGate.fit`
  (`eval_harness.py`) now go through `_as_unit`, and the harvest path
  (`harvest_wave5.py`) calls the shared `replay_disjointness` guard instead
  of its own inline copy — so no embedding degeneracy path is left
  unchecked. Exit 5 is a HARD refusal with no allowlist override (a
  degenerate row must be re-embedded, unlike a near-duplicate similarity
  which `NEAR_DUP_ALLOWLIST` can exempt).
- **Threshold headroom (sim 0.95).** The guard now prints the top-5 cosine
  margins (max similarity per replay row) beside the leak list, so a
  threshold sitting inside the near-duplicate band becomes visible before
  it turns a non-leak into a hard exit-2. `NEAR_DUP_ALLOWLIST` (replay
  `case_key` -> recorded reason string) is the explicit escape hatch; it is
  EMPTY, an entry with no reason raises, and any entry added must be argued
  here — never granted silently.
- **Self-evidencing payload (not yet committed).** The claim that a
  COMMITTED artifact "now records its replay hash, replay count, threshold
  and leak count" is unsupported by the repository:
  `results/m4-gold-acceptance.json` carries none of those fields, and no
  committed JSON contains `replay_sha256`. What is true: the payload the
  NEXT scoring run writes WILL carry `replay_sha256`, `replay_n`,
  `sim_threshold`, `leaks`, `min_routed`, `coverage_floor` and the guard
  stats — so a run can evidence its own guard instead of asserting it. A
  `guard` margin row stores the replay `case_key`, never the ruler text
  (the ruler is untracked private transcript text).
- **Rerun artifacts.** A re-run no longer drops an unignored `.rerun.json`
  into this tracked directory: `git check-ignore` reports everything under
  `tools/classifier-eval/results/` as NOT ignored (`.gitignore:103`
  re-includes it), so a re-run is written to
  `<MEEPT_HOME>/classifier-eval-rerun/` under a timestamped name — or to
  the system temp dir when `MEEPT_HOME` itself points inside the repo, so
  the rerun can never land in the tracked tree. The canonical name stays
  write-once, and reruns no longer clobber each other. If a repo-local
  rerun is preferred instead, the owner adds `classifier-eval-rerun/` to
  `.gitignore` (the owner's call, not this script's).
- **Exit codes** (also in the module docstring): 0 green, 2 leak,
  3 missing inputs (UNVALIDATED), 4 empty ruler, 5 degenerate embeddings,
  6 unparseable ruler. Exit 4 fires only on a GENUINELY empty list (a
  JSON5-style ruler parses now). Exit 5 has no allowlist override: a
  degenerate row must be re-embedded, never waived. Exit 6 (2026-09-13)
  covers a ruler file that EXISTS but cannot be decoded/parsed — a
  zero-byte file or a truncated document used to escape as an uncaught
  `json.JSONDecodeError` traceback (process exit 1, an UNDOCUMENTED code)
  from `load_replay` and from the harvest path; both now print a clear
  `REFUSING:` line and return 6. Reproduce:
  `printf '{ "cases": [ { "input": "x"' > /tmp/bad.json5` then
  `python3 tools/classifier-eval/m4_gold_acceptance.py --check-overlap`
  with `REPLAY` pointed at it (the self-test pins both the zero-byte and
  the truncated case).

Reproduce:
`python3 tools/classifier-eval/m4_gold_acceptance.py --self-test`
(no embed server, no replay corpus needed) and
`python3 tools/classifier-eval/m4_gold_acceptance.py --check-overlap`
(needs the untracked replay corpus).

### 5. Ruler-text privacy, behavioural pins, and the remap/hook hardening test (2026-09-13, wave 4)

The wave-3 regression review of `bdac163e` (hooks + e2e) and `e3760c1e`
(eval guards) found that several guards were only partially closed. Fixed
and pinned here:

- **Ruler text reached the terminal and the leak records.** A leak record
  stored `replay_text`, `format_leaks` printed its first 60 chars (to
  STDOUT from `--check-overlap`, to stderr from `run_scoring_guard`), and
  those records are what a scoring run copies into the artifact's `leaks`
  field. Reproduced before the fix: `--check-overlap` on the real ruler
  printed a private row to stdout. Leak records now carry
  `replay_case_key` (never `replay_text`), `format_leaks` prints the key,
  and the self-test asserts the ruler text is absent from the records, the
  stats, and the printed string — while the leak is still REPORTED and
  identified (both directions; an over-redacting filter fails too).
- **A malformed ruler raised instead of a documented code.** Zero-byte and
  truncated rulers produced an uncaught `json.JSONDecodeError` (exit 1);
  now exit 6 (above), pinned for both shapes.
- **Behavioural guard pins (was: a source grep).** The self-test used
  `"run_scoring_guard(" in body`, satisfied by `if False:`. It now drives
  `run_full` with a leaking ruler AND with `torch`/`transformers` blocked
  in `sys.modules`: the run must refuse (exit 2) and write NO artifact. A
  neutered call site falls through to the import and raises. Both
  `_as_unit` call sites are pinned too — `build_centroids` (extracted from
  `run_full`) and `CentroidGate.fit` must RAISE on a cancelling centroid
  row rather than normalise it to a zero vector via `norm+1e-12`. The
  margin-row-carrying-text and deleted-`EmptyRulerError` mutations also
  keep failing.
- **The remap and hook hardening had no runner at all.**
  `tools/classifier-eval/test_hardening.py` extracts the e2e remapper
  heredoc and the hook's bash functions from source and drives them on
  synthetic inputs: the five false rewrites (`timeout_ms`/`seed`/
  `foo/:9000`/`EXPORT`/`TRANSPORT`) stay unrewritten while the real config
  diff is unchanged (only the 4 spawn-bearing providers); the escaped and
  unescaped variable-port forms agree (FATAL with no literal endpoint,
  allowed-with-a-warning when a literal pins the provider); a clobbered
  `./gendoc` moves the fingerprint and BLOCKS while a stray `coverage.out`
  only warns; the go-less derivation prints an explicit UNCHECKED warning.
  Wired as `make classifier-eval-hardening-test` plus a
  `generated-artifacts` CI step in `.github/workflows/code-quality.yml`,
  because CI's pre-commit job runs on a fresh checkout where
  `git diff --cached` is empty and never exercises the hook classifier.
