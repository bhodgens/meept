#!/usr/bin/env python3
"""iter-5 mechanical sweep: per-class margin floors on the centroid head.

Classes that confuse each other get wider margins; tiny discourse classes
can be pushed to abstain-by-design (margin 1.0 = never route).
"""
import sys
sys.path.insert(0, "/Users/caimlas/git/meept/tools/classifier-eval")
import eval_harness as H

cases_b, cases_a = H.load_cases()
cases = cases_b + cases_a
folds = H.assign_folds(cases)
EMB = {"url": "http://127.0.0.1:8090/v1", "model": "qwen3-embedding", "instruction": ""}

# iter-4 confusion-informed per-class margins. Default 0.03 (winner);
# confusable quartet tighter; discourse classes + tiny classes abstain.
CONFUSABLE = {"code": 0.08, "debug": 0.08, "analyze": 0.06, "plan": 0.06}
ABSTAIN = {"recall": 1.0, "report": 1.0, "review": 1.0, "chat": 0.05}
MIXED = {**CONFUSABLE, **ABSTAIN}

# extend the centroid head inline: per-class margin via subclass
class PerClassCentroid(H.CentroidGate):
    def __init__(self, floor: float, margins: dict[str, float], default: float = 0.03):
        super().__init__(floor=floor, margin=default)
        self.margins = margins
        self.default = default

    def decide(self, q, intents):
        labs = sorted(self.centroids)
        C = H.np.stack([self.centroids[l] for l in labs])
        sims = H.cosmat(q[None], C)[0]
        order = H.np.argsort(-sims)
        top = labs[order[0]]
        m = self.margins.get(top, self.default)
        if sims[order[0]] < self.floor:
            return None, float(sims[order[0]])
        if len(order) > 1 and sims[order[0]] - sims[order[1]] < m:
            return None, float(sims[order[0]])
        return top, float(sims[order[0]])

def build_per_class(spec_head, emb):
    head = PerClassCentroid(spec_head["floor"], spec_head["margins"],
                            spec_head.get("default", 0.03))
    return head

H.build_head_original = H.build_head
def patched_build(spec_head, emb):
    if spec_head["type"] == "per_class_centroid":
        return build_per_class(spec_head, emb)
    return H.build_head_original(spec_head, emb)
H.build_head = patched_build

variants = [
    ("iter5-pc-default0.03", {}),
    ("iter5-pc-confusable-only", CONFUSABLE),
    ("iter5-pc-confusable-abstain", MIXED),
    ("iter5-pc-all005", {k: 0.05 for k in CONFUSABLE}),
    ("iter5-pc-all006", {k: 0.06 for k in CONFUSABLE}),
]

rows = []
for name, margins in variants:
    spec = {"name": name, "embedder": EMB,
            "head": {"type": "per_class_centroid", "floor": 0.60,
                     "margins": margins, "default": 0.03}}
    m = H.run_permutation(spec, cases, folds, H.Embedder(EMB["url"], EMB["model"], ""))
    rows.append(m)
    print(H.fmt_row(m))

# comparator: uniform margins 0.04 and 0.045 (between the 0.03 and 0.05 brackets)
for margin in (0.04, 0.045):
    spec = {"name": f"iter5-centroid-m{margin:.3f}", "embedder": EMB,
            "head": {"type": "centroid", "floor": 0.60, "margin": margin}}
    m = H.run_permutation(spec, cases, folds, H.Embedder(EMB["url"], EMB["model"], ""))
    rows.append(m)
    print(H.fmt_row(m))

stamp = H.time.strftime("%Y%m%d-%H%M%S")
out = H.RESULTS / "iter-5"
out.mkdir(parents=True, exist_ok=True)
(out / f"summary-{stamp}.json").write_text(H.json.dumps(
    [{k: v for k, v in m.items() if k not in ("confusion", "margin_cases")} for m in rows],
    indent=1))
print(f"summary -> {out}/summary-{stamp}.json")

# dump confusion for the winner (first row with P >= 0.97 if any, else best SCORE)
best = max(rows, key=lambda m: m["SCORE"])
print(f"\nwinner by SCORE: {best['name']} (wrong routes below)")
for (c, pred, score) in best["confusion"]:
    if pred != c.intent:
        print(f"   [{c.case_id}] true={c.intent} pred={pred} score={score:.3f} :: {c.text[:60]}")
