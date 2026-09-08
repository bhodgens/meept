#!/usr/bin/env python3
"""iter-4 mechanical sweep: centroid + logistic + prototype-hybrid + two-stage
heads over the cached embedding space (no instruction prefix — iter-3 showed
it hurts this embedder)."""
import sys
sys.path.insert(0, "/Users/caimlas/git/meept/tools/classifier-eval")
import eval_harness as H

cases_b, cases_a = H.load_cases()
cases = cases_b + cases_a
folds = H.assign_folds(cases)
EMB = {"url": "http://127.0.0.1:8090/v1", "model": "qwen3-embedding", "instruction": ""}
DESC = str(H.REPO.parent / "meept/tools/classifier-eval/intent_descriptions.json5")
DESC = "/Users/caimlas/git/meept/tools/classifier-eval/intent_descriptions.json5"

specs = []
# centroid: margin is the real gate; floors low (membership)
for margin in (0.01, 0.02, 0.03, 0.05, 0.08):
    specs.append({"name": f"iter4-centroid-m{margin:.2f}-f0.60",
                  "head": {"type": "centroid", "floor": 0.60, "margin": margin}})
# logistic: probability tau + gap
for tau in (0.60, 0.70, 0.80, 0.90):
    specs.append({"name": f"iter4-logistic-tau{tau:.2f}",
                  "head": {"type": "logistic", "tau": tau, "gap_tau": 0.0}})
for gap in (0.10, 0.20, 0.30):
    specs.append({"name": f"iter4-logistic-tau0.80-gap{gap:.2f}",
                  "head": {"type": "logistic", "tau": 0.80, "gap_tau": gap}})
# prototype hybrid: description lines as virtual neighbors
for pw in (0.9, 1.0, 1.05):
    specs.append({"name": f"iter4-protohyb-knn5-pw{pw:.2f}-f0.70",
                  "head": {"type": "prototype_hybrid", "k": 5, "floor": 0.70,
                           "desc_file": DESC, "proto_weight": pw}})
# two-stage: kNN abstain -> logistic rescue
for tau in (0.75, 0.85, 0.92):
    specs.append({"name": f"iter4-twostage-knn5-rescue{tau:.2f}",
                  "head": {"type": "two_stage", "k": 5, "floor": 0.70,
                           "rescue": {"type": "logistic", "tau": tau, "gap_tau": 0.10},
                           "tau": tau}})

rows = []
for spec in specs:
    m = H.run_permutation({"name": spec["name"], "embedder": EMB, "head": spec["head"]},
                          cases, folds, H.Embedder(EMB["url"], EMB["model"], ""))
    rows.append(m)
    print(H.fmt_row(m))

stamp = H.time.strftime("%Y%m%d-%H%M%S")
out = H.RESULTS / "iter-4"
out.mkdir(parents=True, exist_ok=True)
(out / f"summary-{stamp}.json").write_text(H.json.dumps(
    [{k: v for k, v in m.items() if k not in ("confusion", "margin_cases")} for m in rows],
    indent=1))
print(f"summary -> {out}/summary-{stamp}.json")
