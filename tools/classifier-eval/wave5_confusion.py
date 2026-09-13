#!/usr/bin/env python3
"""Wave-5 confusion analysis: what did the wrong routes at m=0.030 have in
common, and which fix (corpus anchor vs gate change) fits each?
Runs the same replay as iter7_sweep but prints the full confusion matrix
over the current 13-intent corpus.

IN-SAMPLE CAVEAT (F36): this analysis scores every case with class
centroids that include that case (no fold split), so each case's own-class
similarity is inflated and the wrong-route margins are optimistic. It is
NOT the campaign's per-fold protocol (eval_harness.py 5-fold with the
tracked fold-assignment.json) used for every other number, so the "the
gate is at/near its optimum" conclusion it supports is unvalidated for
the no-change decision until re-run per fold.
"""
import sys
sys.path.insert(0, ".")
import numpy as np
import eval_harness as H
from collections import Counter, defaultdict

cases_b, cases_a = H.load_cases()
gold = cases_b + cases_a
# Composition from the committed loader -- the campaign's only count
# source (F35: 389 = 250 adversarial + 139 base, 0 duplicate texts).
n_ood = sum(1 for c in cases_a if c.ood)
print(f"corpus: {len(gold)} "
      f"(base {len(cases_b)} + adversarial {len(cases_a)}; "
      f"non-OOD {len(gold) - n_ood}, OOD {n_ood})")

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
