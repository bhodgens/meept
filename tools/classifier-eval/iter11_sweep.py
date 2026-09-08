#!/usr/bin/env python3
"""iter-11: two experiments at once (mechanical sweep mode).

A) Micro-cluster length-floor simulation: skip gate for inputs < N words
   (emulates the daemon-side length floor). Purely harness-side.
B) Double-margin rule: require BOTH margin >= 0.030 AND top1 >= 0.70
   (floor on absolute sim, the kNN membership idea applied to centroid).
"""
import sys
sys.path.insert(0, "/Users/caimlas/git/meept/tools/classifier-eval")
import numpy as np
import eval_harness as H

cases_b, cases_a = H.load_cases()
cases = cases_b + cases_a
folds = H.assign_folds(cases)
EMB = {"url": "http://127.0.0.1:8090/v1", "model": "qwen3-embedding", "instruction": ""}


class MarginFloorCentroid(H.CentroidGate):
    """centroid + margin + absolute floor on top1 sim."""
    def decide(self, q, intents):
        labs = sorted(self.centroids)
        C = np.stack([self.centroids[l] for l in labs])
        sims = H.cosmat(q[None], C)[0]
        order = np.argsort(-sims)
        if sims[order[0]] < self.floor:
            return None, float(sims[order[0]])
        if len(order) > 1 and sims[order[0]] - sims[order[1]] < self.margin:
            return None, float(sims[order[0]])
        return labs[order[0]], float(sims[order[0]])


_ORIG_BUILD_HEAD = H.build_head


def build(spec_head, emb):
    if spec_head["type"] == "margin_floor_centroid":
        return MarginFloorCentroid(floor=spec_head["floor"], margin=spec_head["margin"])
    return _ORIG_BUILD_HEAD(spec_head, emb)


H.build_head = build

orig_run = H.run_permutation


def run_with_wordfloor(spec, cases, folds, emb, word_floor=0):
    """Wrap decide() to abstain when token count < word_floor."""
    global _FLOOR
    _FLOOR = word_floor
    m = orig_run(spec, cases, folds, emb)
    return m


# simplest correct approach: pre-filter cases by word count inside decide
# via closure over the CURRENT query text — but decide() only sees vectors.
# Instead, run permutations over case subsets directly:
rows = []
emb = H.Embedder(EMB["url"], EMB["model"], "")

def wc(text):
    return len(text.split())

for floor_words in (0, 2, 3):
    subset = [c for c in cases if c.ood or wc(c.text) >= floor_words]
    spec = {"name": f"iter11-wordfloor{floor_words}-m0.030", "embedder": EMB,
            "head": {"type": "centroid", "floor": 0.60, "margin": 0.030}}
    m = H.run_permutation(spec, subset, folds, emb)
    rows.append(m)
    print(H.fmt_row(m), f"[word-floor {floor_words}: n={len(subset)}]")

for floor_sim in (0.65, 0.70, 0.75):
    spec = {"name": f"iter11-mf-centroid-m0.030-f{floor_sim:.2f}", "embedder": EMB,
            "head": {"type": "margin_floor_centroid", "floor": floor_sim, "margin": 0.030}}
    m = H.run_permutation(spec, cases, folds, emb)
    rows.append(m)
    print(H.fmt_row(m))

stamp = H.time.strftime("%Y%m%d-%H%M%S")
out = H.RESULTS / "iter-11"
out.mkdir(parents=True, exist_ok=True)
(out / f"summary-{stamp}.json").write_text(H.json.dumps(
    [{k: v for k, v in m.items() if k not in ("confusion", "margin_cases")} for m in rows],
    indent=1))
print(f"summary -> {out}/summary-{stamp}.json", file=sys.stderr)
