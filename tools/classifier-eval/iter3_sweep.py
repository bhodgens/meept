#!/usr/bin/env python3
"""iter-3 mechanical sweep: instruction-prefixed embeddings + kNN variants.

Per master.md M1/mechanical-sweep mode: embeddings cached (new cache tag
for the instruction-prefixed embedder), then a batch of permutations is
scored against the cache in-process. No subagents.
"""
import sys
sys.path.insert(0, "/Users/caimlas/git/meept/tools/classifier-eval")
import numpy as np
import eval_harness as H
from pathlib import Path

INSTRUCTION = ("Given a user message to a coding-agent platform, classify "
               "the user's intent")

cases_b, cases_a = H.load_cases()
cases = cases_b + cases_a
folds = H.assign_folds(cases)

# one embedder per (model, instruction) — cache tag differs from iter 1/2
emb = H.Embedder("http://127.0.0.1:8090/v1", "qwen3-embedding", INSTRUCTION)

specs = []
for floor in (0.80, 0.75, 0.70, 0.65):
    specs.append({
        "name": f"iter3-instr-knn5-unanimity-{floor:.2f}",
        "head": {"type": "knn_unanimity", "k": 5, "floor": floor},
    })
for k in (3, 4):
    specs.append({
        "name": f"iter3-instr-knn{k}-unanimity-0.70",
        "head": {"type": "knn_unanimity", "k": k, "floor": 0.70},
    })
for k, need in ((5, 4), (4, 3), (3, 2)):
    specs.append({
        "name": f"iter3-instr-knn{k}-maj{need}-0.70",
        "head": {"type": "knn_majority", "k": k, "floor": 0.70, "margin": 0.0},
    })

rows = []
for spec in specs:
    full = {"name": spec["name"],
            "embedder": {"url": "http://127.0.0.1:8090/v1",
                          "model": "qwen3-embedding", "instruction": INSTRUCTION},
            "head": spec["head"]}
    m = H.run_permutation(full, cases, folds, emb)
    rows.append(m)
    print(H.fmt_row(m))

# baseline re-check with same corpus for delta comparison (no instruction)
base_spec = {"name": "iter3-noinstr-knn5-unanimity-0.70",
             "embedder": {"url": "http://127.0.0.1:8090/v1",
                           "model": "qwen3-embedding", "instruction": ""},
             "head": {"type": "knn_unanimity", "k": 5, "floor": 0.70}}
emb0 = H.Embedder("http://127.0.0.1:8090/v1", "qwen3-embedding", "")
m0 = H.run_permutation(base_spec, cases, folds, emb0)
print(H.fmt_row(m0))

stamp = H.time.strftime("%Y%m%d-%H%M%S")
out = H.RESULTS / "iter-3"
out.mkdir(parents=True, exist_ok=True)
(out / f"summary-{stamp}.json").write_text(H.json.dumps(
    [{k: v for k, v in m.items() if k not in ("confusion", "margin_cases")} for m in rows] +
    [{k: v for k, v in m0.items() if k not in ("confusion", "margin_cases")}],
    indent=1))
print(f"summary -> {out}/summary-{stamp}.json")
