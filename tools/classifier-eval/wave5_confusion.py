#!/usr/bin/env python3
"""Wave-5 confusion analysis: what did the 10 wrong routes at m=0.030
have in common, and which fix (corpus anchor vs gate change) fits each?
Runs the same replay as iter7_sweep but prints the full confusion
matrix over the NEW 13-intent corpus (358 cases)."""
import sys
sys.path.insert(0, ".")
import numpy as np
import eval_harness as H
from collections import Counter, defaultdict

cases_b, cases_a = H.load_cases()
gold = cases_b + cases_a
print(f"corpus: {len(gold)}")

QS = {"url": "http://127.0.0.1:8090/v1", "model": "qwen3-embedding", "instruction": ""}
emb = H.Embedder(QS["url"], QS["model"], "")
gkeys = [H.case_key(c.text) for c in gold]
emb.embed_keys([c.text for c in gold], gkeys)
V = emb.vectors(gkeys)
intents = [c.intent for c in gold]
labs = sorted({c.intent for c in gold if not c.ood})

C = np.stack([V[[i for i in range(len(gold)) if intents[i] == l]].mean(axis=0) for l in labs])
C /= (np.linalg.norm(C, axis=1, keepdims=True) + 1e-12)

confusion = defaultdict(int)
margin_at_wrong = []
sim_at_wrong = []
for i, c in enumerate(gold):
    if c.ood:
        continue
    sims = C @ V[i]
    order = np.argsort(-sims)
    pred = labs[int(order[0])]
    margin = float(sims[order[0]] - sims[order[1]])
    if pred != c.intent:
        confusion[(c.intent, pred)] += 1
        margin_at_wrong.append(margin)
        sim_at_wrong.append(float(sims[order[0]]))

print("\nconfusion pairs (true -> predicted), top 12:")
for (t, p), n in sorted(confusion.items(), key=lambda kv: -kv[1])[:12]:
    print(f"  {t:12} -> {p:12} {n}")

import statistics as st
if margin_at_wrong:
    print(f"\nmargin at wrong routes: median={st.median(margin_at_wrong):.4f} "
          f"mean={st.mean(margin_at_wrong):.4f} max={max(margin_at_wrong):.4f}")
    print(f"sim at wrong routes: median={st.median(sim_at_wrong):.4f} max={max(sim_at_wrong):.4f}")
    below = sum(1 for m in margin_at_wrong if m < 0.030)
    print(f"wrong routes with margin < 0.030 (already gated): {below}/{len(margin_at_wrong)}")
