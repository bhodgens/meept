# Leaf 03 — Replay acceptance: scratch-daemon A/B + verdict report

DISPATCH INSTRUCTION: Any agent may implement this leaf. Do NOT commit.
Do NOT run `git add`. Write scripts, run the A/B, write the report, and
report results. The orchestrator handles all git operations.

**Parent:** `docs/plans/quickplan-session-upgrade/master.md`
**Scope:** Measure the production cascade on the 48-case adjudicated
replay with the session-state upgrade OFF vs ON, in a scratch daemon, and
write the verdict report.
**Dependencies:** leaf 02 COMPLETE.
**Estimated context:** ~60K.

## Measurement protocol (frozen)

- Ruler: `tools/classifier-eval/replay-gold.local.json5` (48 cases,
  UNTRACKED — verbatim text never enters git or tracked artifacts).
- Acceptance bar (master.md): aggregate replay accuracy >= 86.0% with the
  gate ON (>= 2 cases better than OFF), NO regression on non-quickplan
  lanes, and every upgrade visible as `quickplan_session_upgrade` in
  dispatch logs.
- IMPORTANT honesty rule: the replay was adjudicated for PER-MESSAGE
  classification. The session gate needs session state to exist. The A/B
  must therefore PRE-SEED session state per case: for every gold
  quickplan case, create an approved plan (or an executing task) in the
  scratch session BEFORE submitting the turn; for every non-quickplan
  case, seed NOTHING. This models the production premise (quickplan
  requests arrive in sessions where plans/tasks exist) and makes the
  one-way upgrade measurable. Record the seeding map in the report
  (case_id -> seeded evidence kind).

## Environment traps (from campaign scratch-rig record — read first)

- hujson: quoted keys at ALL depths.
- `transport.http.rest` is a BOOL — omission 404s every /api/v1 route.
- dev_key resolves the REAL `~/.meept/dev_key` (os.UserHomeDir), not
  MEEPT_HOME — copy it into the scratch home or use the real key.
- Remap EVERY fixed runtime port in the scratch config (not just HTTP):
  check `lsof -nP -iTCP:<port> -sTCP:LISTEN` before boot; :8080-:8084
  are commonly held by live runtimes; :8082 is the prompt-router sidecar.
- Rebuild scratch binaries AFTER the leaf-02 commit (a stale binary
  silently tests the wrong code): `go build -o <scratch>/bin/meept-daemon
  ./cmd/meept-daemon` etc.
- The coder execution budget needs `max_conversation_tokens` >= 400k in
  the coder AGENT.md frontmatter for review-shaped quickplan turns.

## Tasks

### Task 1 — scratch rig + OFF leg

Stand up the scratch daemon per the traps above (reuse the working
pattern from `scripts/` e2e harnesses if present — check
`ls scripts/ | grep -i e2e` and `docs/plans/classifier-iteration/` for
the last working scratch config). With
`orchestrator.classifier.session_state_upgrade` ABSENT (default off),
submit all 48 cases, harvest per-case verdicts (classification_method +
intent) from the dispatch log / audit rows. Artifact:
`results/session-upgrade/leg-off.json` — `[{case_id, intent, method}]`.

### Task 2 — ON leg

Same rig, config now `"classifier": {"session_state_upgrade": true}` and
per-case session seeding per the map. Artifact:
`results/session-upgrade/leg-on.json`, same shape plus
`session_reason` when present in logs.

### Task 3 — scoring + report

Score both legs against the gold labels (reuse
`teacher_mix/score_mix.py` conventions: denominator = all 48; label map
code->coding etc.). Write `results/session-upgrade/REPORT.md`:

1. Verdict line: `VERDICT: PASS/FAIL — OFF X/48 vs ON Y/48 (bar: ON >=
   42/48, no non-quickplan regression)`.
2. Per-lane table OFF vs ON with case-id-level flips (which cases
   upgraded, which of those flipped to correct).
3. Upgrade census: count of `quickplan_session_upgrade` rows (must be
   >= 1 for the gate to be proven live; 0 upgrades = the gate never
   fired = leaf 02 wiring failure, report as BLOCKED with log excerpts).
4. Regressions: any case correct-OFF wrong-ON, with its log excerpt.
5. Honest caveats: seeding models the production premise; the replay
   ruler stays per-message adjudicated; n=48, one case = 2.1%.
6. NO verbatim message text anywhere — case ids only.

### Task 3b — teardown

Kill scratch runtimes (`pgrep -fl` for the scratch spawn commands) and
confirm ports freed (`lsof`); leave no orphaned llama/MLX processes on
the user's machine. Report the teardown in the leaf output.

## Interface Contract (what this leaf exposes)

- `results/session-upgrade/leg-off.json`, `leg-on.json`, `REPORT.md`.
- REPORT.md's verdict line is THE result for the tracking table.
- Gitignore: any file that would contain verbatim text (the seeding map
  contains case ids + evidence kinds only — trackable).

## Self-Verification Checklist

- [ ] Both legs ran the FULL 48 cases (no silent truncation)
- [ ] Binary rebuilt post-leaf-02 (mtime check recorded)
- [ ] >= 1 `quickplan_session_upgrade` in the ON leg logs
- [ ] Score denominators = 48 both legs
- [ ] Scratch runtimes torn down; ports freed
- [ ] REPORT.md carries the verdict line + regressions + caveats

## Review Checklist (orchestrator)

- [ ] Recompute both legs' accuracy from the JSONs independently
- [ ] Verdict rule applied exactly; flips enumerated case-by-case
- [ ] Seeding map consistent with gold labels (quickplan cases seeded,
      non-quickplan not)
- [ ] No verbatim text in tracked artifacts

Suggested commit (orchestrator):
`git commit -m "feat(classifier-eval): session-state upgrade replay A/B — <verdict>" -- tools/classifier-eval/results/session-upgrade/`
