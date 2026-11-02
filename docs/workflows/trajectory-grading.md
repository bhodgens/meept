# Trajectory Grading (Golden Sequences)

Trajectory grading is meept's **sequence** gate: instead of judging the
outcome of a run (does the final answer look right?), it compares the
exact ordered sequence of tool calls an agent made against a committed
"golden" reference sequence. Outcome grading lives in
[eval.md](eval.md) (`Judge()` / `TrajectoryJudgment`); this page covers
the sequence half. The two divide cleanly:

- **Outcome grading** - was the result correct? (see `docs/workflows/eval.md`)
- **Sequence grading** - were the steps correct? (this page)

## What gets graded

The comparator (`internal/eval/trajectorydiff`) grades **normalized
tool-call sequences**. Each recorded tool call is normalized to:

- the tool **name**, and
- an **argument shape**: JSON keys sorted, variable values (generated
  IDs, timestamps, temp paths, tokens - keys matching
  `id|uuid|timestamp|time|path|token|session`, case-insensitive)
  replaced with the `<var>` placeholder, per-tool argument rules
  applied.

So `read_file({"path":"/tmp/abc123/input.txt"})` and
`read_file({"path":"/tmp/xyz789/input.txt"})` both normalize to the
same step: `read_file {"path": "<var>"}`. The comparison is
deterministic; two replays of the same scripted scenario always match.

## Match modes

A fixture carries its own match mode and pass threshold in its `match`
block. The modes, from strictest to loosest:

| Mode         | Meaning                                             | Score |
|--------------|-----------------------------------------------------|-------|
| `exact`      | same length, position-wise equal                    | 1.0 iff equal, else length-ratio penalty |
| `in_order`   | golden is an ordered subsequence of actual (default) | LCS(golden, actual) / len(golden) |
| `any_order`  | golden multiset contained in actual multiset        | matched multiset / len(golden) |

`Passed = score >= threshold` (default threshold `1.0`). Both `mode`
and `threshold` are **per fixture**, so one scenario can demand exact
step-for-step fidelity while another tolerates extra interleaved calls.
On failure the result includes a one-line `Summary`
(`mode: score ... : N/M golden steps matched`), per-step `StepDiff`s,
and the 1-based index of the first mismatch.

Per-tool tuning also lives in the `match` block: `arg_rules` overrides
the argument rule per tool name (`exact` / `subset` / `ignore`), and
`ignore_tools` excludes brittle tools from comparison entirely.

## Capture policy

Goldens are captured **only** from FakeLLM-scripted hermetic runs:

- The capture harness (`e2e/harness/golden.go`) requires the exact
  `FakeLLM` script; a nil script is a refusal, and it refuses to write
  any `captured_from` value other than the literal
  `"fakellm-scripted"`. There is no code path that can record a live
  capture.
- A replay that records zero tool calls is also refused.
- The **live tier (`make e2e-chat`) is never sequence-gated.** Live
  model runs stay outcome-graded only; no golden comparison ever runs
  against a live-model session.

This keeps goldens deterministic: a fixture is the output of a script,
not of a model's mood.

## How the CI gate works

The `trajectory-golden` e2e suite (`e2e/suites/trajectory-golden/`,
build tag `e2e`) is the gate:

1. A scripted `FakeLLM` scenario replays through a real scratch daemon.
2. The actual tool-call sequence is captured from the session store
   (ordered by seq) and normalized.
3. `trajectorydiff` grades it against the committed fixture, using the
   fixture's own `match` block.

The gate is **blocking inside the suite**: a sequence regression fails
the test, with no retry loops - a hermetic scripted run that flakes is
a harness bug, not noise. The suite includes an in-test negative drill
that deliberately corrupts a fixture entry in memory and proves the
gate fails with a step diff (nothing on disk is touched).

CI membership: the suite runs in the hermetic fast tier
(`make e2e-fast`), which is a required member of the `ci-success`
aggregate gate in `ci.yml`. See `docs/workflows/e2e-testing.md` for
tiers and the manifest rule; the suite and its path mappings are
registered in `e2e/manifest.json` (`e2e/fixtures/golden/` and
`internal/eval/trajectorydiff/` map to `trajectory-golden`).

## Regenerating a golden (investigate, then regenerate)

**The rule: a golden diff is investigated first, never
blind-regenerated.** A fixture failure means the agent's behavior
changed - either a regression you must fix, or an intentional behavior
change that legitimately moves the golden. Find out which before
touching the fixture.

When you have confirmed the new sequence is correct (the change was
intentional), refresh the fixture:

```sh
# one scenario
make golden-regenerate SCENARIO=trajectory-golden-basic

# every fixture
make golden-regenerate-all
```

`SCENARIO` is the fixture stem under `e2e/fixtures/golden/`
(`<SCENARIO>.golden.json`). The regeneration helper
(`e2e/harness/golden_regen.go`) is **suite-first**: it runs the owning
`trajectory-golden` suite before writing anything, and refuses to
overwrite the fixture if the suite cannot run or fails. It replays the
**identical** script the suite replays (suites register their capture
options), so a regenerated fixture and a suite replay can never drift
apart. `FORCE=1` skips only the investigation diff print - never the
suite gate.

To add a new fixture, write the scripted scenario in the owning suite
and register it with the regen registry (`RegenerateScenarioScript`);
capture runs through the same harness path with the same refusals.

## Fixture format

Fixtures live at `e2e/fixtures/golden/<scenario>.golden.json`:

```json
{
  "schema_version": 1,
  "scenario": "trajectory-golden-basic",
  "captured_from": "fakellm-scripted",
  "match": {
    "mode": "in_order",
    "threshold": 1,
    "arg_rules": {},
    "ignore_tools": []
  },
  "golden": [
    {"name": "read_file", "args_shape": {"path": "<var>"}},
    {"name": "file_write", "args_shape": {"content": "golden", "direct": true, "path": "<var>"}},
    {"name": "read_file", "args_shape": {"path": "<var>"}}
  ]
}
```

- `schema_version` - format version, currently `1`.
- `captured_from` - must be the literal `"fakellm-scripted"`; the
  capture harness refuses to write anything else.
- `match` - the comparator options the suite replays: `mode`,
  `threshold`, per-tool `arg_rules`, `ignore_tools`. Empty
  mode/threshold normalize to the defaults (`in_order`, `1.0`) at
  write time.
- `golden` - the reference sequence: tool `name` plus scrubbed
  `args_shape` (variable values as `<var>`).

Existing fixtures: `trajectory-golden-basic`,
`demo-read-write`.

## See also

- `docs/workflows/eval.md` - outcome grading (`Judge()`,
  `TrajectoryJudgment`): the other half of evaluation.
- `docs/workflows/e2e-testing.md` - test tiers, manifest format, and
  the no-new-unit-tests policy this feature follows.
