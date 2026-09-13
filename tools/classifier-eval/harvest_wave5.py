#!/usr/bin/env python3
"""Harvest wave 5: novel prompt shapes from Hermes logs.

Adds cases for 6 shapes the current corpus does not cover (found by
pattern audit over 323 unique Hermes user messages):
  1. iteration-continuation ("continue", "did you finish?")
  2. correction-pushback ("you made changes to X, should've been Y")
  3. multi-numbered-list ("1) fix ... 2) fix ... 3) ...")
  4. choice-selection ("for problem 1 use A, for 2 use combination B")
  5. external-url-task ("create resume for this job: https://...")
  6. data-conversion ("convert data under X to csv, for each file...")
  7. style-preference ("make the font smaller", "use german flag colors")

Labels follow the adjudication rules (outcome-loop record):
  continuation -> quickplan (resume work, no re-plan)
  correction-pushback -> debug (fix the mistake)
  numbered-list -> quickplan (multi-step sequence)
  choice-selection -> quickplan (execute selected options)
  external-url-task -> code (artifact-producing work, source is a URL)
  data-conversion -> code
  style-preference -> code (tracked artifact adjustment)

Kept rows are stamped source "live-session" (they are harvested verbatim
from the user's Hermes transcripts). Two disjointness guards reject a
candidate: cosine > 0.95 vs the existing gold corpus, and exact-match or
cosine > 0.95 vs the adjudicated replay ruler (so the ruler stays
disjoint from the corpus that fits the models).
"""
import hashlib
import json
import re
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import eval_harness as H  # dedup guard helpers

CASES = [
    # 1. iteration-continuation -> quickplan
    ("continue", "quickplan", "orchestrator"),
    ("continue where you left off", "quickplan", "orchestrator"),
    ("did you finish the last task?", "quickplan", "orchestrator"),
    ("continue implementing with subagents", "quickplan", "orchestrator"),
    ("keep going with the remaining items", "quickplan", "orchestrator"),
    # 2. correction-pushback -> debug
    ("looks like there's still a problem - you made changes to config.json and it should've been done to config.jsonc",
     "debug", "debugger"),
    ("this used to work, but recent changes broke it", "debug", "debugger"),
    ("the output report.md is not properly formatted markdown and is unreadable. fix it.",
     "debug", "debugger"),
    ("you missed several entries - all of the information should have been extracted for each file",
     "debug", "debugger"),
    # 3. multi-numbered-list -> quickplan
    ("1) fix the layout 2) fix the header 3) ssl should be default 4) check the certs",
     "quickplan", "orchestrator"),
    ("1) yes we'll need additional accounts 2) what's the benefit of one vs the other? 3) unclear on the dashboard",
     "quickplan", "orchestrator"),
    # 4. choice-selection -> quickplan
    ("for problem 1, use the recommended fix. For 2, use a combination of options A and B. Also explain the test.",
     "quickplan", "orchestrator"),
    ("use more subagents", "quickplan", "orchestrator"),
    ("use a default global fallback for all agents", "quickplan", "orchestrator"),
    # 5. external-url-task -> code
    ("create a resume tailored to this job posting and save it to 2026/: https://careers.example.com/jobs/5813",
     "code", "coder"),
    ("install and configure the tool by following the instructions here: https://raw.githubusercontent.com/example/readme.md",
     "code", "coder"),
    # 6. data-conversion -> code
    ("there is data under original-data, this data needs to be converted to csv format. for each file with multiple sheets, convert each sheet to a separate csv",
     "code", "coder"),
    ("the master data json is now too difficult to edit manually. we need an additional abstraction layer",
     "code", "coder"),
    # 7. style-preference -> code
    ("use the colors of the german flag for the site theme", "code", "coder"),
    ("can we make the font used for technical skills smaller?", "code", "coder"),
    ("the format doesn't match the others - check whether it uses the schema fully and correctly, including fonts",
     "review", "coder"),
]

CASES = [
    (f"h20-{i:03d}", text, intent, agent)
    for i, (text, intent, agent) in enumerate(CASES, 1)
]

# dedup guards. Two disjointness checks run before a candidate is kept:
#   1. vs the existing gold corpus (cosine > 0.95 => reject) -- the
#      iter7_harvest.py pattern;
#   2. vs the adjudicated replay ruler (exact match or cosine > 0.95 =>
#      reject). Without (2) a harvested row silently becomes a training
#      case for the very ruler that scores it (train-on-test, F20).
sys.path.insert(0, str(Path(__file__).resolve().parent))
import eval_harness as H

REPLAY = Path(__file__).resolve().parent / "replay-gold.local.json5"
SIM_THRESHOLD = 0.95

cases_b, cases_a = H.load_cases()
gold = cases_b + cases_a
emb = H.Embedder("http://127.0.0.1:8090/v1", "qwen3-embedding", "")
gkeys = [H.case_key(c.text) for c in gold]
emb.embed_keys([c.text for c in gold], gkeys)
V = emb.vectors(gkeys)
import numpy as np

# ruler guard: use the SHARED disjointness guard (eval_harness), not an
# inline reimplementation -- an inline copy silently drifts from the guard
# that actually gates the acceptance run (review flagged it as unchecked).
# The shared guard also raises EmptyRulerError / DegenerateVectorError on a
# broken ruler instead of skipping the check.
replay_texts, rcases, RV = [], [], None
if REPLAY.exists():
    # committed ruler layout is { cases: [...] } (H.parse_json5, the shared
    # parser -- the old key-order regex is gone). A bare list is tolerated.
    _parsed = H.parse_json5(REPLAY.read_text())
    _cases = _parsed.get("cases", []) if isinstance(_parsed, dict) else _parsed
    replay_texts = [r["input"] for r in _cases]
    rcases = [H.Case(t, "replay", "", False, "replay", "", f"replay#{i}")
              for i, t in enumerate(replay_texts)]
    rkeys = [H.case_key(t) for t in replay_texts]
    emb.embed_keys(replay_texts, rkeys)
    RV = emb.vectors(rkeys)

kept, rejected = [], []
for cid, text, intent, agent in CASES:
    k = H.case_key(text)
    emb.embed_keys([text], [k])
    v = emb.vectors([k])[0]
    max_sim = float(np.max(V @ v))
    if max_sim > SIM_THRESHOLD:
        rejected.append((cid, text[:50], round(max_sim, 3), "corpus-dedup"))
        continue
    if rcases:
        rleaks = H.replay_disjointness([text], rcases, corpus_vectors=RV,
                                       replay_vectors=v[None],
                                       sim_threshold=SIM_THRESHOLD)
        if rleaks:
            why = ("ruler-exact" if rleaks[0]["reason"] == "exact"
                   else "ruler-similarity")
            rejected.append((cid, text[:50], rleaks[0]["similarity"], why))
            continue
    kept.append({
        "id": cid, "input": text, "expected_intent": intent,
        "expected_agent": agent, "added_in": "harvest-20260910",
        # live-session: harvested verbatim from ~/.hermes/sessions (the
        # only truthful provenance -- "hermes-inspired" understated real
        # private traffic; see docs/workflows/classification-architecture.md).
        "source": "live-session",
    })

print(f"kept: {len(kept)}  rejected by dedup/ruler guard: {len(rejected)}"
      f"  ruler cases: {len(replay_texts)}")
for cid, t, s, why in rejected:
    print(f"  REJECT {cid} cos={s} [{why}] {t}")

if "--write" in sys.argv and kept:
    corpus = Path("/Users/caimlas/git/meept/testdata/eval/classifier-adversarial-corpus.json5")
    text = corpus.read_text()
    marker = "    // ---- failure class 8: long inputs, intent appears late ----"
    block = "    // ---- harvest wave 5: novel prompt shapes from Hermes logs (dedup-guarded) ----\n"
    block += ",\n".join(
        '    { id: "%s", input: "%s", expected_intent: "%s", expected_agent: "%s", '
        'added_in: "%s", source: "%s" }'
        % (c["id"], c["input"].replace('"', '\\"'), c["expected_intent"],
           c["expected_agent"], c["added_in"], c["source"])
        for c in kept
    )
    block += ",\n"
    assert marker in text, "splice marker not found"
    text = text.replace(marker, block + marker)
    corpus.write_text(text)
    print(f"spliced {len(kept)} cases into {corpus}")
else:
    print("(dry run: pass --write to splice)")
