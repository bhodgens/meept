#!/usr/bin/env python3
"""iter-18: does the wave-4 expansion move stage-A on the EXACT misses?
Replays the 3 known silver misses + the full silver set through the
centroid gate trained on gold (now including iter-18 cases)."""
import sys
sys.path.insert(0, "/Users/caimlas/git/meept/tools/classifier-eval")
import numpy as np
import eval_harness as H

cases_b, cases_a = H.load_cases()
cases = cases_b + cases_a
folds = H.assign_folds(cases)
emb = H.Embedder("http://127.0.0.1:8090/v1", "qwen3-embedding", "")
texts = [c.text for c in cases]
keys = [H.case_key(t) for t in texts]
emb.embed_keys(texts, keys)
V = emb.vectors(keys)
intents = [c.intent for c in cases]
is_ood = np.array([c.ood for c in cases])
fold_of = np.array([folds[k] for k in keys])

# silver misses from the M4 validation
MISSES = [
    ("implement the plan using subagents", "code"),
    ("implement the plan", "code"),
    ("create a skill-tailored resume for this posting (1-page) based on my resume and save the results under 2026", "code"),
]

# stage-A centroids trained on ALL non-OOD gold (deployment shape)
train_m = ~is_ood
train_idx = np.where(train_m)[0]
labs = sorted({intents[i] for i in train_idx})
C = np.stack([V[[i for i in train_idx if intents[i] == l]].mean(axis=0) for l in labs])
C /= (np.linalg.norm(C, axis=1, keepdims=True) + 1e-12)

print("stage-A replay of silver misses (post-wave-4):")
for t, true in MISSES:
    k = H.case_key(t)
    if k not in emb.mem:
        emb.embed_keys([t], [k])
    qv = emb.mem[k]
    sims = C @ qv
    order = np.argsort(-sims)
    margin = float(sims[order[0]] - sims[order[1]])
    routed = sims[order[0]] >= 0.60 and margin >= 0.030
    verdict = f"ROUTE {labs[order[0]]}" if routed else "abstain (falls to B/C)"
    ok = routed and labs[order[0]] == true
    print(f"  {'OK ' if ok else ('WRONG' if routed else 'PASS')} [{true}] -> {verdict} "
          f"(top1={labs[order[0]]} sim={sims[order[0]]:.3f} margin={margin:.3f}) :: {t[:45]}")
