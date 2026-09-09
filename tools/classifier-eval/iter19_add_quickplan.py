#!/usr/bin/env python3
"""iter-19: add quickplan gold anchors to the tracked corpus + measure.

The adjudicated replay showed quickplan is 42% of real traffic with zero
gold anchors. This wave adds quickplan anchors to the tracked corpus
(synthetic, phrasing inspired by the adjudicated rules — the verbatim
adjudicated cases stay in the untracked replay-gold file).

Quickplan decision rules (adjudication record):
1. review-and-correct (autonomous) = quickplan
2. execute-existing-plan (any phrasing) = quickplan
3. multi-step sequences (commit, push, then run) = quickplan
4. explicit 'plan it and do it' phrasing = quickplan
"""
import json
from pathlib import Path

CASES = [
    # rule 1: review-and-correct
    ("scan the scheduler package for races and fix whatever you find", "quickplan"),
    ("go through the API layer looking for missing error handling and patch it as you go", "quickplan"),
    ("audit the docs for stale examples and update them where they're wrong", "quickplan"),
    ("hunt down all the TODOs in the parser and resolve them", "quickplan"),
    ("check the config loader against the schema and repair any drift", "quickplan"),
    # rule 2: execute-existing-plan
    ("implement the remaining phases of the plan", "quickplan"),
    ("execute the plan tree start to finish", "quickplan"),
    ("carry out the migration plan without stopping for approvals", "quickplan"),
    ("finish what's left on the roadmap", "quickplan"),
    ("run through the implementation checklist", "quickplan"),
    # rule 3: multi-step sequences
    ("commit the work, update the changelog, then push", "quickplan"),
    ("rebuild the index, run the test suite, and report the results", "quickplan"),
    ("pull latest, rebase, and rerun the failing test", "quickplan"),
    ("archive the old logs, compact the database, and verify integrity", "quickplan"),
    ("bump the version, tag the release, and publish the artifacts", "quickplan"),
    # rule 4: explicit plan-and-do
    ("figure out how to speed up ingestion and just make it happen", "quickplan"),
    ("work out the safest rollout for this change and execute it", "quickplan"),
    ("come up with a fix for the flaky ordering and apply it", "quickplan"),
    ("plan how to split this module and go ahead and split it", "quickplan"),
    ("decide the best structure for the new agents and build it out", "quickplan"),
    # quickplan vs review boundary negatives (verdict-only stays review)
    ("review the design doc and tell me if the approach holds up", "review"),
    ("assess whether the caching layer is worth keeping", "analyze"),
    # quickplan vs plan boundary negatives (design-first stays plan)
    ("design the rollout plan for the multi-region deploy", "plan"),
    ("plan the phases of the rewrite before we touch anything", "plan"),
]

corpus = Path("testdata/eval/classifier-adversarial-corpus.json5")
text = corpus.read_text()
assert "iteration-19" not in text, "already applied"

lines = ["    // ---- iteration 19: quickplan gold anchors (adjudication-derived rules) ----"]
for i, (inp, intent) in enumerate(CASES, 1):
    agent = "orchestrator" if intent == "quickplan" else (
        "reviewer" if intent == "review" else "analyst" if intent == "analyze" else "planner")
    lines.append(
        f'    {{ id: "h19-qp-{i:03d}", input: "{inp}", expected_intent: "{intent}", '
        f'expected_agent: "{agent}", added_in: "iteration-19", source: "hermes-inspired" }},')
block = "\n".join(lines) + "\n\n"

marker = "    // ---- failure class 8: long inputs, intent appears late ----"
assert marker in text
text = text.replace(marker, block + marker)
corpus.write_text(text)
print(f"added {len(CASES)} quickplan-wave cases ({sum(1 for _, i in CASES if i == 'quickplan')} quickplan)")
