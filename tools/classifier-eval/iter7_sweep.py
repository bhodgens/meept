#!/usr/bin/env python3
"""iter-7 sweep after confusion-harvest growth: margin brackets on the
enlarged corpus + kNN comparator."""
import sys
sys.path.insert(0, "/Users/caimlas/git/meept/tools/classifier-eval")
import eval_harness as H

cases_b, cases_a = H.load_cases()
cases = cases_b + cases_a
folds = H.assign_folds(cases)
EMB = {"url": "http://127.0.0.1:8090/v1", "model": "qwen3-embedding", "instruction": ""}
emb = H.Embedder(EMB["url"], EMB["model"], "")
print(f"corpus: {len(cases)} (base {len(cases_b)} + adv {len(cases_a)})", file=sys.stderr)

rows = []
for margin in (0.025, 0.030, 0.035):
    spec = {"name": f"iter7-centroid-m{margin:.3f}", "embedder": EMB,
            "head": {"type": "centroid", "floor": 0.60, "margin": margin}}
    m = H.run_permutation(spec, cases, folds, emb)
    rows.append(m)
    print(H.fmt_row(m))
    for (c, pred, score) in m["confusion"]:
        if pred != c.intent:
            print(f"   wrong [{c.case_id}] true={c.intent} pred={pred} "
                  f"score={score:.3f} :: {c.text[:60]}")

# kNN comparator on the enlarged corpus
spec = {"name": "iter7-knn5-unanimity-0.70", "embedder": EMB,
        "head": {"type": "knn_unanimity", "k": 5, "floor": 0.70}}
m = H.run_permutation(spec, cases, folds, emb)
rows.append(m)
print(H.fmt_row(m))

stamp = H.time.strftime("%Y%m%d-%H%M%S")
out = H.RESULTS / "iter-7"
out.mkdir(parents=True, exist_ok=True)
(out / f"summary-{stamp}.json").write_text(H.json.dumps(
    [{k: v for k, v in m.items() if k not in ("confusion", "margin_cases")} for m in rows],
    indent=1))
print(f"summary -> {out}/summary-{stamp}.json", file=sys.stderr)
