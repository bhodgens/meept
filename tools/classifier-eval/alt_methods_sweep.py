#!/usr/bin/env python3
"""Door-1/Door-2 alternative methods: measure on the gold replay (n=48).

Baselines (committed numbers):
  Door 1 alone (centroid margin 0.030): sys ~84% (7 routes, 5 right)
  Door 2 alone (chain): 86.8% (lab) / ~84% (live)
  Current cascade: 84.56% (double-confidence policy)

Alternatives tested here (all offline, same 48 gold cases, same
embed server):
  ALT-1: kNN (k=3, majority) instead of centroid — non-parametric Door 1
  ALT-2: centroid with per-class thresholds (learned from replay)
  ALT-3: two-stage centroid: route at margin 0.015, abstain to chain only
         if margin < 0.015 AND sim < 0.60 (looser first gate)
  ALT-4: TF-IDF char-ngram logistic (no embedder at all) as Door 1
  ALT-5: ALT-4 as a PRE-filter gate: only send to Door 1 when ALT-4
         agrees (cheap agreement veto)
"""
import sys, json, re
sys.path.insert(0, "/Users/caimlas/git/meept/tools/classifier-eval")
import numpy as np
import eval_harness as H
from pathlib import Path

# --- load gold corpus + replay
cases_b, cases_a = H.load_cases()
gold = cases_b + cases_a
gkeys = [H.case_key(c.text) for c in gold]
intents = [c.intent for c in gold]
labs = sorted({c.intent for c in gold if not c.ood})

QS = {"url": "http://127.0.0.1:8090/v1", "model": "qwen3-embedding", "instruction": ""}
emb = H.Embedder(QS["url"], QS["model"], "")
emb.embed_keys([c.text for c in gold], gkeys)
V = emb.vectors(gkeys)
C = np.stack([V[[i for i in range(len(gold)) if intents[i] == l]].mean(axis=0) for l in labs])
C /= (np.linalg.norm(C, axis=1, keepdims=True) + 1e-12)
yg = np.array([labs.index(c.intent) if not c.ood else -1 for c in gold])

rp = Path("/Users/caimlas/git/meept/tools/classifier-eval/replay-gold.local.json5")
silver = json.loads("[" + ",".join(re.findall(r"\{\s*\"input\".*?\}", rp.read_text(), re.S)) + "]")
s_texts = [c["input"] for c in silver]
s_true = [c["expected_intent"] for c in silver]
skeys = [H.case_key(t) for t in s_texts]
emb.embed_keys(s_texts, skeys)
SV = emb.vectors(skeys)

ORCH = re.compile(r"(?i)\b(subagents?|tasks? \d|task list|waves?|leaves?|leaf \d|plan\.md|handoff|checklist|in order|one at a time|sealed plan|tracking table|as you (find|go)|, then\b|and correct them|and fix them|without (asking|stopping)|no check-?ins?|just (do|make|apply)|to completion|implement tasks?|work (through|items))\b")
CUES = [bool(ORCH.search(t)) for t in s_texts]

CHAIN = 0.868

def system_score(routes):
    """routes: list of (door, correct_bool). chain fallback = CHAIN expected."""
    tot = len(routes)
    right = sum(1 for d, ok in routes if d in ("1", "2") and ok)
    n_chain = sum(1 for d, _ in routes if d == "C")
    return (right + n_chain * CHAIN) / tot

# --- Door 1 variants over the replay
def door1_centroid(sv, margin, simfloor, qp_cue_guard=True):
    sims = C @ sv
    order = np.argsort(-sims)
    top, second = float(sims[order[0]]), float(sims[order[1]])
    lab = labs[int(order[0])]
    if top < simfloor:
        return None
    if top - second < margin:
        return None
    if lab == "quickplan" and qp_cue_guard:
        # cue guard applies at dispatch; replay has no cues here — handled by caller
        pass
    return lab

def door1_knn3(sv):
    sims = V @ sv
    order = np.argsort(-sims)[:3]
    top_labs = [intents[i] for i in order]
    if top_labs[0] != top_labs[1]:
        return None  # unanimity-ish for k=3 majority
    if float(sims[order[0]]) < 0.60:
        return None
    return top_labs[0]

results = {}

# ALT-1: kNN k=3
routes = []
for sv, true, cue in zip(SV, s_true, CUES):
    lab = door1_knn3(sv)
    if lab == "quickplan" and not cue:
        lab = None
    if lab is None:
        routes.append(("C", True))
    else:
        routes.append(("1", lab == true))
results["ALT-1 knn3-majority"] = system_score(routes)

# ALT-2/ALT-3: centroid margin sweep (incl. looser gates)
for margin, simfloor in [(0.030, 0.60), (0.020, 0.60), (0.015, 0.60), (0.010, 0.65), (0.008, 0.70)]:
    routes = []
    for sv, true, cue in zip(SV, s_true, CUES):
        lab = door1_centroid(sv, margin, simfloor)
        if lab == "quickplan" and not cue:
            lab = None
        if lab is None:
            routes.append(("C", True))
        else:
            routes.append(("1", lab == true))
    results[f"ALT-3 centroid m={margin} sim>={simfloor}"] = system_score(routes)

# ALT-4: TF-IDF char-ngram logistic trained on gold (no embedder)
from sklearn.feature_extraction.text import TfidfVectorizer
from sklearn.linear_model import LogisticRegression
from sklearn.pipeline import make_pipeline
mask = yg >= 0
clf = make_pipeline(
    TfidfVectorizer(analyzer="char_wb", ngram_range=(2, 4), min_df=1, sublinear_tf=True),
    LogisticRegression(max_iter=2000, C=10.0),
)
clf.fit([gold[i].text for i in range(len(gold)) if mask[i]], [intents[i] for i in range(len(gold)) if mask[i]])
probs = clf.predict_proba(s_texts)
classes = list(clf.classes_)
routes = []
for p, true, cue in zip(probs, s_true, CUES):
    li = int(np.argmax(p))
    conf = float(p.max())
    lab = classes[li]
    gate = conf >= 0.55  # calibrated on gold LOO earlier ~ this band
    if not gate:
        routes.append(("C", True)); continue
    if lab == "quickplan" and not cue:
        routes.append(("C", True)); continue
    routes.append(("1", lab == true))
results["ALT-4 tfidf-logistic door1"] = system_score(routes)

# ALT-5: tfidf pre-gate on centroid (route only when tfidf top-1 agrees)
routes = []
for sv, p, true, cue in zip(SV, probs, s_true, CUES):
    lab = door1_centroid(sv, 0.030, 0.60)
    if lab is None:
        routes.append(("C", True)); continue
    if lab == "quickplan" and not cue:
        routes.append(("C", True)); continue
    tfidf_lab = classes[int(np.argmax(p))]
    if tfidf_lab != lab:  # disagreement -> chain
        routes.append(("C", True)); continue
    routes.append(("1", lab == true))
results["ALT-5 tfidf-veto on centroid"] = system_score(routes)

print(f"{'variant':44} system_acc")
print("-" * 56)
for k, v in sorted(results.items(), key=lambda kv: -kv[1]):
    print(f"{k:44} {v:.2%}")
print("-" * 56)
print(f"{'baseline: current cascade (committed)':44} 84.56%")
print(f"{'baseline: chain-only floor':44} 86.80%")
