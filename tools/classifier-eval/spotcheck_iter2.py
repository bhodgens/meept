#!/usr/bin/env python3
"""iter-2 spot-check: identify the wrong route(s) and independently re-score."""
import sys
sys.path.insert(0, "/Users/caimlas/git/meept/tools/classifier-eval")
import numpy as np
import eval_harness as H

cases_b, cases_a = H.load_cases()
cases = cases_b + cases_a
folds = H.assign_folds(cases)
spec = H.json.loads(open("/Users/caimlas/git/meept/tools/classifier-eval/specs/iter1-baseline.json").read())
emb = H.Embedder(spec["embedder"]["url"], spec["embedder"]["model"], "")
m = H.run_permutation(spec, cases, folds, emb)
print("wrong:", m["wrong"], "direct:", m["direct"], "OOD_R:", round(m["OOD_R"], 3))
for (c, pred, score) in m["confusion"]:
    if pred != c.intent:
        print(f"WRONG: [{c.case_id}] {c.text[:70]!r}")
        print(f"   true={c.intent} pred={pred} score={score:.3f}")

# independent re-score with a fresh embedder instance
texts = [c.text for c in cases]
keys = [H.case_key(t) for t in texts]
emb2 = H.Embedder(spec["embedder"]["url"], spec["embedder"]["model"], "")
emb2.embed_keys(texts, keys)
V = emb2.vectors(keys)
intents = [c.intent for c in cases]
key_arr = np.array(keys)
is_ood = np.array([c.ood for c in cases])
fold_of = np.array([folds[k] for k in keys])
wrong_ids = [c.case_id for (c, p, s) in m["confusion"] if p != c.intent]
for cid in wrong_ids:
    ci = next(i for i, c in enumerate(cases) if c.case_id == cid)
    f = fold_of[ci]
    train_m = (fold_of != f) & ~is_ood
    sims = V[ci] @ V[train_m].T
    top = np.argsort(-sims)[:5]
    print(f"\nre-score {cid} top-5:")
    for i in top:
        gi = np.where(train_m)[0][i]
        print(f"   {sims[i]:.3f} {intents[gi]:8s} {cases[gi].text[:45]}")
