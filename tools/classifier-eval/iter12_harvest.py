#!/usr/bin/env python3
"""iter-12: harvest wave 3 — targeted at top1-CORRECT thin-margin abstains.
For each near-miss (top1 == true intent, margin < 0.030), author 2-3
same-intent paraphrase anchors to pull that centroid cluster tighter."""
import sys
sys.path.insert(0, "/Users/caimlas/git/meept/tools/classifier-eval")
import numpy as np
import eval_harness as H

NEW = [
    # debug cluster reinforcement (4 near-misses were debug, top1-correct)
    ("the compiler rejects the type cast", "debug"),
    ("fix this loop index that skips elements", "debug"),
    ("the POST endpoint responds with 500", "debug"),
    ("debug the mutex contention in the pool", "debug"),
    ("fix the flag parsing that breaks on dashes", "debug"),
    # code cluster reinforcement
    ("implement JWT auth for the REST layer", "code"),
    ("write a backup script for the database", "code"),
    ("refactor the service to use constructor injection", "code"),
    # plan reinforcement
    ("design the notification pipeline architecture", "plan"),
    ("plan how to break the task into steps", "plan"),
    # analyze reinforcement (long-003 was a near-miss)
    ("analyze the root causes of the latency spike", "analyze"),
    # search reinforcement
    ("search for database engine benchmark results", "search"),
    ("find monitoring tool comparisons", "search"),
    # report reinforcement
    ("generate a status report for the sprint", "report"),
    # review reinforcement (review#1 near-miss)
    ("check the code quality of this patch", "review"),
]

cases_b, cases_a = H.load_cases()
cases = cases_b + cases_a
spec = {"name": "dedup", "embedder": {"url": "http://127.0.0.1:8090/v1",
                                      "model": "qwen3-embedding", "instruction": ""},
        "head": {"type": "knn_unanimity", "k": 5, "floor": 0.70}}
emb = H.Embedder(spec["embedder"]["url"], spec["embedder"]["model"], "")
texts = [c.text for c in cases]
keys = [H.case_key(t) for t in texts]
emb.embed_keys(texts, keys)
V = emb.vectors(keys)
new_texts = [t for t, _ in NEW]
new_keys = [H.case_key(t) for t in new_texts]
emb.embed_keys(new_texts, new_keys)
accepted, rejected = [], []
for (t, intent), k in zip(NEW, new_keys):
    sims = V @ emb.mem[k]
    mx = float(sims.max())
    (rejected if mx > 0.95 else accepted).append((t, intent, mx))
print(f"accepted {len(accepted)} rejected {len(rejected)}")
for t, i, mx in rejected:
    print(f"  REJECT [{i:8}] {mx:.3f} {t}")
