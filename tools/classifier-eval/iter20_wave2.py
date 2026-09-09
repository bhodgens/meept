#!/usr/bin/env python3
"""iter-20 wave 2: quickplan anchors targeting the code-vocabulary collision.

The iter-19 misses were quickplan-labeled messages with heavy code vocab
("Implement Tasks 7 and 8: Add project fields...", "implement the plan
using subagents"). This wave adds anchors with EXACTLY those shapes:
execution verbs + code nouns + orchestration markers.
"""
from pathlib import Path

CASES = [
    # task-list execution (the exact collision family)
    ("Implement Tasks 3 and 4: add retry handling to the ingest path and wire the metrics counter.", "quickplan"),
    ("Implement Tasks 12 through 15 of the tracking table, in order.", "quickplan"),
    ("Knock out the next three tasks from the task list and mark them complete.", "quickplan"),
    ("Execute steps 1 through 5 of the migration checklist.", "quickplan"),
    ("Work items 6, 7 and 9 are yours: implement each and note completion.", "quickplan"),
    ("implement task 4 and task 5 from the sealed plan", "quickplan"),
    ("Tackle the pending tasks in the plan file one at a time until done.", "quickplan"),
    # orchestration-marked execution
    ("using subagents, implement the sealed plan and post progress to the thread", "quickplan"),
    ("dispatch subagents to finish the remaining leaves of the tree", "quickplan"),
    ("spin up subagents for the next wave and have them build the listed endpoints", "quickplan"),
    ("fan out the pending leaves to subagents and collect their reports", "quickplan"),
    # review-and-correct across new domains
    ("sweep the flutter client for null-safety issues and fix them as you go", "quickplan"),
    ("scan the RPC handlers for missing context propagation and correct them", "quickplan"),
    ("review the scheduler package for deadlocks and repair anything you find", "quickplan"),
    ("go over the storage layer looking for leaks and patch them in place", "quickplan"),
    ("inspect the auth flow for gaps and close them without asking", "quickplan"),
    # sequence-of-events (comma/then chains)
    ("format, lint, test, then push the release branch", "quickplan"),
    ("build the image, run the smoke suite, then deploy to staging", "quickplan"),
    ("vacuum the database, reindex, and verify row counts", "quickplan"),
    ("regenerate the graphs, check them in, and update the handoff doc", "quickplan"),
    # execute-existing-plan phrasing variants
    ("execute what's left of the plan", "quickplan"),
    ("carry on with the plan to completion", "quickplan"),
    ("finish the remaining waves of the implementation", "quickplan"),
    ("complete the outstanding tasks from the handoff notes", "quickplan"),
    ("resume the plan where it stopped and run it to the end", "quickplan"),
    # plan-and-do explicit
    ("work out why ingestion is slow and make it faster, no check-ins needed", "quickplan"),
    ("figure out the right fix for the flaky ordering test and just apply it", "quickplan"),
    ("decide how to structure the new toolkits and build them out", "quickplan"),
    # boundary negatives (must NOT become quickplan)
    ("review the migration script and tell me if it looks right", "review"),
    ("what are the tradeoffs between embedded and sidecar architectures?", "analyze"),
    ("design the rollout plan before we touch production", "plan"),
    ("why does the compiler reject the type cast?", "debug"),
]

corpus = Path("testdata/eval/classifier-adversarial-corpus.json5")
text = corpus.read_text()
assert "iteration-20" not in text, "already applied"
lines = ["    // ---- iteration 20: quickplan wave 2 - code-vocabulary collision shapes ----"]
for i, (inp, intent) in enumerate(CASES, 1):
    agent = {"quickplan": "orchestrator", "review": "reviewer", "analyze": "analyst",
             "plan": "planner", "debug": "debugger"}[intent]
    lines.append(
        f'    {{ id: "h20-qp-{i:03d}", input: "{inp}", expected_intent: "{intent}", '
        f'expected_agent: "{agent}", added_in: "iteration-20", source: "hermes-inspired" }},')
marker = "    // ---- failure class 8: long inputs, intent appears late ----"
text = text.replace(marker, "\n".join(lines) + "\n\n" + marker)
corpus.write_text(text)
print(f"added {len(CASES)} wave-2 cases "
      f"({sum(1 for _, i in CASES if i == 'quickplan')} quickplan, "
      f"{sum(1 for _, i in CASES if i != 'quickplan')} boundary negatives)")
