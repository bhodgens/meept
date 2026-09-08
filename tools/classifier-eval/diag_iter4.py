#!/usr/bin/env python3
"""Diagnose the logistic head's C=0: max-prob distribution at tau=0.10."""
import sys
sys.path.insert(0, "/Users/caimlas/git/meept/tools/classifier-eval")
import numpy as np
import eval_harness as H

cases_b, cases_a = H.load_cases()
cases = cases_b + cases_a
folds = H.assign_folds(cases)
spec = {"name": "diag-logistic",
        "head": {"type": "logistic", "tau": 0.10, "gap_tau": 0.0},
        "embedder": {"url": "http://127.0.0.1:8090/v1", "model": "qwen3-embedding",
                     "instruction": ""}}
emb = H.Embedder(spec["embedder"]["url"], spec["embedder"]["model"], "")
m = H.run_permutation(spec, cases, folds, emb)
print("tau=0.10 forced-route:", H.fmt_row(m))

texts = [c.text for c in cases]
keys = [H.case_key(t) for t in texts]
emb.embed_keys(texts, keys)
V = emb.vectors(keys)
intents = [c.intent for c in cases]
is_ood = np.array([c.ood for c in cases])
fold_of = np.array([folds[k] for k in keys])

probs = []
for f in range(5):
    test_m = fold_of == f
    train_m = ~test_m & ~is_ood
    train_idx = np.where(train_m)[0]
    head = H.LogisticHead(tau=0.10, gap_tau=0.0)
    head.fit(train_idx, [intents[i] for i in train_idx])
    for qi in np.where(test_m)[0]:
        if is_ood[qi]:
            continue
        z = V[qi] @ head.W + head.b
        z = z - z.max()
        p = np.exp(z)
        p = p / p.sum()
        probs.append((float(p.max()), intents[qi], head.labs[int(np.argmax(p))]))

probs.sort(reverse=True)
print("top-8 max-prob (true/pred):")
for pr, t, pl in probs[:8]:
    print(f"  {pr:.3f} true={t:8s} pred={pl}")
print("bottom-3:")
for pr, t, pl in probs[-3:]:
    print(f"  {pr:.3f} true={t:8s} pred={pl}")
