#!/usr/bin/env python3
"""iter-10 diagnosis: where does the champion (centroid m0.030) ABSTAIN?
Harvest the abstained non-OOD cases with their top-2 centroid sims to find
coverage opportunities where top-1 is likely RIGHT but margin is thin."""
import sys
sys.path.insert(0, "/Users/caimlas/git/meept/tools/classifier-eval")
import numpy as np
import eval_harness as H

cases_b, cases_a = H.load_cases()
cases = cases_b + cases_a
folds = H.assign_folds(cases)
EMB = {"url": "http://127.0.0.1:8090/v1", "model": "qwen3-embedding", "instruction": ""}
emb = H.Embedder(EMB["url"], EMB["model"], "")
texts = [c.text for c in cases]
keys = [H.case_key(t) for t in texts]
emb.embed_keys(texts, keys)
V = emb.vectors(keys)
intents = [c.intent for c in cases]
is_ood = np.array([c.ood for c in cases])
fold_of = np.array([folds[k] for k in keys])

abstains = []
for f in range(5):
    test_m = fold_of == f
    train_m = ~test_m & ~is_ood
    train_idx = np.where(train_m)[0]
    labs = sorted({intents[i] for i in train_idx})
    C = np.stack([V[[i for i in train_idx if intents[i] == l]].mean(axis=0) for l in labs])
    C /= (np.linalg.norm(C, axis=1, keepdims=True) + 1e-12)
    for qi in np.where(test_m)[0]:
        if is_ood[qi]:
            continue
        sims = C @ V[qi]
        order = np.argsort(-sims)
        top1, top2 = float(sims[order[0]]), float(sims[order[1]])
        margin = top1 - top2
        routed = sims[order[0]] >= 0.60 and margin >= 0.030
        if not routed:
            abstains.append((cases[qi].case_id, intents[qi], labs[order[0]],
                             round(top1, 3), round(margin, 3), cases[qi].text[:55]))

print(f"abstained non-OOD: {len(abstains)} of 165")
near = [a for a in abstains if a[3] >= 0.60]
near.sort(key=lambda a: -a[4])
print(f"\nNEAR MISSES (top1 >= 0.60), sorted by margin — margin < 0.030 = harvest targets:")
for a in near[:25]:
    correct = "OK" if a[2] == a[1] else "TOP1-WRONG"
    print(f"  {a[0]:12} true={a[1]:8} top1={a[2]:8} sim={a[3]} margin={a[4]} [{correct}] {a[5]}")
