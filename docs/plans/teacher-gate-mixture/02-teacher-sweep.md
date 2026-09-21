# Leaf 02 — Teacher Sweep: runner + prompts + full 48-case run

DISPATCH INSTRUCTION: Any agent may implement this leaf. Do NOT commit.
Do NOT run `git add`. Write files, run verification commands, and report
results. The orchestrator handles all git operations.

**Parent:** `docs/plans/teacher-gate-mixture/master.md`
**Scope:** Implement `prompt.py` and `run_mix.py`, then execute the full
48-case mixture sweep producing raw per-case artifacts.
**Dependencies:** leaf 01 COMPLETE (route table in
`tools/classifier-eval/results/teacher-mix/PROBE.md`).
**Estimated context:** ~60K.

## Frozen contracts (from master.md — binding)

- C1 invocation: `python3.12 tools/classifier-eval/teacher_mix/run_mix.py
  --replay tools/classifier-eval/replay-gold.local.json5 --outdir
  tools/classifier-eval/results/teacher-mix/ [--limit N] [--only CSV]`
- C2 per-case artifact `outdir/raw/<case_id>.json`:
  `{"case_id": str, "a": verdict|null, "b": verdict|null,
  "judge": verdict|null, "final": {"intent": str, "confidence": float,
  "source": "a|b|judge|fallback|error"}}`; verdict =
  `{"intent": str, "confidence": float, "reason": str}`.
- C5 scoring lanes: lowercase; runner maps corpus label spellings to lane
  spellings with a frozen table and asserts 0 unmapped at load.
- C6: code lives in `tools/classifier-eval/teacher_mix/`; raw/ is
  GITIGNORED (add a .gitignore inside teacher_mix results dir if needed —
  actually: ensure `tools/classifier-eval/results/teacher-mix/raw/` is
  ignored by checking `git check-ignore`; if not covered by existing
  rules, append one path line to `tools/classifier-eval/.gitignore` if
  that file exists, else `tools/classifier-eval/.gitignore` may be created
  with the single line `results/teacher-mix/raw/`).
- C7: `/opt/homebrew/opt/python@3.12/bin/python3.12`, stdlib only.

## The 13 lanes (exact spellings — frozen)

From `tools/classifier-eval/intent_descriptions.json5` (12) + quickplan:

| lane | one-line description (embed in prompt) |
|---|---|
| coding | writing, modifying, or generating source code: implement functions, create APIs, add features, refactor, write tests or migrations |
| debugging | diagnosing or fixing broken behavior: crashes, errors, failing tests, leaks, hangs, exceptions, investigate why something fails |
| analysis | research, compare, evaluate options, best practices, study a topic, explain tradeoffs, summarize findings |
| search | find external information: search the web or docs, look up references, locate libraries or articles |
| chat | small talk, greetings, thanks, acknowledgments, or brief social replies with no task content |
| platform | questions about the assistant itself: capabilities, available tools, agents, or how to use the system |
| git | version control operations: commit, push, pull request, branch, merge, rebase, tag, diff, history |
| scheduling | reminders, timers, calendar events, meetings, recurring tasks, time-based actions |
| planning | design an approach or architecture, roadmap, migration strategy, break work into steps before building |
| review | critique or assess provided work: code review, check an implementation for issues, quality feedback |
| reporting | summarize what happened: progress reports, digests of activity, status overviews |
| recall | remember or retrieve past conversation context: what we discussed, prior decisions, earlier work |
| quickplan | the user asks to plan AND execute autonomously now: run subagents, implement an approved plan, review-and-fix end to end without further check-ins |

CORPUS LABEL MAP (replay labels -> lanes; asserted complete): code->coding,
debug->debugging, analyze->analysis, search->search, chat->chat,
platform->platform, git->git, schedule->scheduling, plan->planning,
review->review, report->reporting, recall->recall,
quickplan->quickplan. OOD-labelled cases: skip (do not score, do not send).

## Tasks

### Task 1 — prompt.py

Two functions, pure string builders:

- `worker_prompt(message: str) -> str` — renders the 13-lane list with the
  descriptions above + the FULL verbatim message + the mandatory output
  contract. Output contract text (exact):
  `Reply with ONLY a single-line JSON object: {"intent": "<lane>",
  "confidence": <0.0-1.0>, "reason": "<=15 words"}`.
- `judge_prompt(message: str, a: dict, b: dict) -> str` — same lane list +
  the verbatim message + BOTH worker verdicts rendered verbatim + this
  arbitration rule text (exact): `The two classifiers disagreed on intent.
  Decide which intent is correct from the message text alone. You may pick
  either classifier's intent or a third intent from the list. Reply with
  ONLY a single-line JSON object: {"intent": "<lane>", "confidence":
  <0.0-1.0>, "reason": "<=15 words"}`.

Prompt order is frozen: lanes FIRST, then message, then instruction. This
order is part of the experiment record.

### Task 2 — run_mix.py

Behavior:

1. Parse args (C1). Load the replay corpus with a STRING-AWARE JSON5 read:
   strip `//` comments only OUTSIDE strings, then `json.loads(strict=False)`
   (campaign lesson: regex comment-stripping eats `//` inside URLs).
2. Map labels via the frozen table; assert 0 unmapped; skip OOD cases;
   print `loaded: N cases (M ood skipped)`.
3. Read PROBE.md's route table (parse the markdown table) for the three
   model ids + transports. If any route says UNAVAILABLE: exit 2 with a
   message naming the route.
4. For each case: call worker A, worker B (order fixed). Then:
   - both agree on intent -> final = that verdict, source "a" (confidence
     from A), judge NOT called (cost control; agreement recorded)
   - disagree -> call judge; judge returns -> final = judge verdict,
     source "judge"; judge errors -> final = higher-confidence worker,
     source "fallback"; both workers errored -> final = {"intent": "",
     "confidence": 0.0, "source": "error"}
   NEVER average confidences. NEVER let agreement raise confidence.
5. Transport: subprocess
   `/Applications/OpenCode.app/Contents/MacOS/opencode-cli run <prompt>
   --model <id>`, timeout 120s, 2 retries (5s backoff). JSON extraction:
   find the FIRST `{` and scan to its matching `}` (brace counter),
   `json.loads` it; validate `intent` is one of the 13 lanes else treat as
   error for that slot. On HTTP fallback (worker-b only, if PROBE says
   FALLBACK): urllib POST per master C2 with `$ZAI_API_KEY` from env —
   read env var, never print it.
6. Resume support: skip cases whose raw/<case_id>.json already exists with
   `final.source != "error"`. Write each raw file immediately after the
   case completes (crash-safe).
7. Print a one-line progress per case: `case_id a=lane(0.xx) b=lane(0.xx)
   judge=lane final=lane(src)` — lanes only, NO message text to stdout
   (stdout may be captured into logs).

### Task 3 — Execute the sweep

Run the full 48 cases. Watch for: rate limits (the CLI may 429 — the retry
loop handles; if a route accumulates > 5 hard errors, STOP the run and
report, do not limp through), and the known opencode `models` subcommand
crash (do not use `models`; `run` works).

Report: total cases, agreement count (judge calls avoided), judge calls,
final non-error count, and the raw accuracy printed by:

`python3.12 -c "import json,glob; d=[json.load(open(p)) for p in
glob.glob('tools/classifier-eval/results/teacher-mix/raw/*.json')];
print(sum(1 for x in d if x['final']['source']!='error'), 'non-error of',
len(d))"`

## Interface Contract (what this leaf exposes)

- `tools/classifier-eval/teacher_mix/prompt.py` (two functions, signatures
  above), `tools/classifier-eval/teacher_mix/run_mix.py` (C1 CLI).
- `outdir/raw/*.json` per C2 — the COMPLETE 48-case set (minus skipped
  OOD) is what leaf 03 consumes. Expected non-error >= 44; fewer than 44
  is a leaf failure to report, not to hide.
- gitignore coverage for raw/ per C6.

## Self-Verification Checklist

- [ ] `py_compile` clean on both files
- [ ] `--limit 3` smoke produces 3 raw JSONs with the C2 shape
- [ ] Full run: raw/ holds every non-OOD case id; no case crashed the loop
- [ ] `grep -c api_key run_mix.py` = 0; env var only read via os.environ
- [ ] No verbatim message text in any file run_mix.py writes (message joins
      happen in memory only)
- [ ] Resume works: re-running skips completed cases (prove with a 2-case
      re-run printing skip lines)

## Review Checklist (orchestrator)

- [ ] Lane list in prompt.py matches the frozen table exactly (spellings)
- [ ] Judge invoked ONLY on disagreement; no confidence arithmetic anywhere
- [ ] raw/ gitignored; spot-check 3 raw files against C2
- [ ] Aggregation file counts match `loaded` line
- [ ] Scripts contain no debug prints beyond the specified progress line

Suggested commit (orchestrator, after review):
`git add tools/classifier-eval/teacher_mix/prompt.py
tools/classifier-eval/teacher_mix/run_mix.py
tools/classifier-eval/results/teacher-mix/summary-raw-counts.md
[.gitignore if created] && git commit -m "feat(classifier-eval):
teacher-mix sweep runner + 48-case raw artifacts (leaf 02)"`
(raw/ itself is NOT committed.)
