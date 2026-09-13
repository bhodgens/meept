#!/usr/bin/env python3
"""M4 gold-replay acceptance run (campaign gate: system acc > 86.8%).

Runs the adjudicated 48-case gold replay through the full cascade under
BOTH candidate acceptance policies, on the same ruler and the same fold
rules:

  policy "double-confidence"
      Door A: centroid sim >= 0.60 AND margin >= 0.030 (quickplan cue guard)
      Door B: only when probe prob >= 0.70 AND same-class agreement with A
      Door C: everything else = chain floor 86.8%

  policy "tfidf-veto" (ALT-5)
      Door A routes only when the char n-gram TF-IDF logistic concurs with
      the centroid pick; disagreement falls to the chain. This is the
      policy results/m4-gold-acceptance.md claims at 87.35%.

Two integrity guards run BEFORE any case is scored:

  1. corpus <-> replay disjointness (eval_harness.replay_disjointness).
     The models are fit on the committed corpus; the ruler is the
     adjudicated replay. A replay case that also sits in the fitting
     corpus (exact case_key match, or cosine > 0.95 when the embed server
     is up) is a train-on-test leak. The run REFUSES to score when any
     leak is found (exit 2). It never silently drops the case, because
     dropping it would silently move the denominator.
  2. coverage floor MIN_ROUTED. With fewer than MIN_ROUTED routed cases
     the verdict is one or two Bernoulli draws against the hard-coded
     CHAIN constant, not a measurement, so the verdict is reported as
     INSUFFICIENT_COVERAGE -- never PASS/FAIL.

Each policy writes its OWN artifact:
      results/m4-gold-acceptance-double-confidence.json
      results/m4-gold-acceptance-tfidf-veto.json
The historical, committed results/m4-gold-acceptance.json (the
double-confidence FAIL record) is NEVER overwritten: result files are
write-once (F34).

Prerequisites the committed record cannot carry: the UNTRACKED adjudicated
replay (replay-gold.local.json5, gitignored) and a live embed server on
:8090. Without them the script reports each policy as UNVALIDATED and
exits non-zero; it never produces a PASS from missing inputs.

Usage:
  python3 m4_gold_acceptance.py                    # full run (needs :8090)
  python3 m4_gold_acceptance.py --policy all
  python3 m4_gold_acceptance.py --check-overlap    # guard only, no deps
"""
import argparse
import json
import re
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import eval_harness as H  # noqa: E402

RESULTS = HERE / "results"
REPLAY = HERE / "replay-gold.local.json5"
MODEL_DIR = "/Volumes/LLMs/answerdotai/ModernBERT-base"
EMBED_URL = "http://127.0.0.1:8090/v1"

CHAIN = 0.868          # hard-coded chain floor (eval_harness.CHAIN_BASELINE)
MIN_ROUTED = 20        # coverage floor (F19): below this a verdict is noise
SIM_THRESHOLD = 0.95   # corpus<->replay leak threshold (F20)

ORCH = re.compile(
    r"(?i)\b(subagents?|tasks? \d|task list|waves?|leaves?|leaf \d|plan\.md|"
    r"handoff|checklist|in order|one at a time|sealed plan|tracking table|"
    r"as you (find|go)|, then\b|and correct them|and fix them|"
    r"without (asking|stopping)|no check-?ins?|just (do|make|apply)|"
    r"to completion|implement tasks?|work (through|items))\b")


def load_replay():
    """Return the adjudicated replay records, or None when the untracked
    replay corpus is absent (gitignored by design)."""
    if not REPLAY.exists():
        return None
    txt = REPLAY.read_text()
    return json.loads("[" + ",".join(
        re.findall(r"\{\s*\"input\".*?\}", txt, re.S)) + "]")


def verdict_for(routed: int, routed_ok: int, nchain: int, n: int):
    """Score + verdict with the coverage floor. Never returns PASS/FAIL
    for a routed sample below MIN_ROUTED."""
    acc = (routed_ok + nchain * CHAIN) / n if n else 0.0
    if routed < MIN_ROUTED:
        return acc, "INSUFFICIENT_COVERAGE"
    return acc, ("PASS" if acc > CHAIN else "FAIL")


def write_artifact(policy: str, payload: dict) -> Path:
    out = RESULTS / f"m4-gold-acceptance-{policy}.json"
    if out.exists():
        # write-once: never clobber a committed result (F34). A re-run
        # writes an adjacent .rerun file so the historical record stands.
        out = RESULTS / f"m4-gold-acceptance-{policy}.rerun.json"
    out.write_text(json.dumps(payload, indent=2) + "\n")
    return out


def unvalidated(policy: str, reason: str) -> None:
    print(f"[{policy}] UNVALIDATED: {reason}", file=sys.stderr)
    print(f"  artifact not written; see results/m4-gold-acceptance.md "
          f"for the UNVALIDATED record.", file=sys.stderr)


def check_overlap_only() -> int:
    """Disjointness check that needs no embed server: exact-match only."""
    cases_b, cases_a = H.load_cases()
    gold = cases_b + cases_a
    replay = load_replay()
    if replay is None:
        print(f"replay corpus absent ({REPLAY.name}); cannot check "
              f"disjointness", file=sys.stderr)
        return 3
    texts = [r["input"] for r in replay]
    leaks = H.replay_disjointness(texts, gold, sim_threshold=SIM_THRESHOLD)
    if leaks:
        print(H.format_leaks(leaks))
        print("REFUSING: the ruler is not disjoint from the fitting corpus "
              "(train-on-test). Remove the offending corpus row or replay "
              "case before scoring.", file=sys.stderr)
        return 2
    print(f"ruler disjoint: {len(texts)} replay cases vs "
          f"{len(gold)} corpus cases (exact-match + "
          f"sim>{SIM_THRESHOLD} when an embed server is supplied)")
    return 0


def run_full(policies: set) -> int:
    import numpy as np
    import torch
    from transformers import AutoModel, AutoTokenizer

    replay = load_replay()
    if replay is None:
        unvalidated("all", f"untracked replay corpus missing ({REPLAY})")
        return 3
    s_texts = [r["input"] for r in replay]
    s_true = [r["expected_intent"] for r in replay]

    torch.manual_seed(42)
    tok = AutoTokenizer.from_pretrained(MODEL_DIR)
    base = AutoModel.from_pretrained(MODEL_DIR).to("cpu").eval()
    hidden = base.config.hidden_size

    def mb_embed(texts, batch=16):
        out = []
        with torch.no_grad():
            for i in range(0, len(texts), batch):
                enc = tok(texts[i:i + batch], padding=True, truncation=True,
                          max_length=256, return_tensors="pt")
                o = base(**enc).last_hidden_state
                m = enc["attention_mask"].unsqueeze(-1).float()
                v = (o * m).sum(1) / m.sum(1).clamp(min=1e-9)
                out.extend(x.cpu().numpy().astype(np.float32)
                           for x in torch.nn.functional.normalize(v, dim=-1))
        return out

    # gold corpus -> fit both stages
    cases_b, cases_a = H.load_cases()
    gold = cases_b + cases_a
    gkeys = [H.case_key(c.text) for c in gold]
    intents = [c.intent for c in gold]
    labs = sorted({c.intent for c in gold if not c.ood})

    emb = H.Embedder(EMBED_URL, "qwen3-embedding", "")
    emb.embed_keys([c.text for c in gold], gkeys)
    V = emb.vectors(gkeys)
    C = np.stack([V[[i for i in range(len(gold)) if intents[i] == l]].mean(axis=0)
                  for l in labs])
    C /= (np.linalg.norm(C, axis=1, keepdims=True) + 1e-12)
    yg = np.array([labs.index(c.intent) if not c.ood else -1 for c in gold])

    MB_G = np.stack(mb_embed([c.text for c in gold]))
    mask = yg >= 0
    counts = np.bincount(yg[mask], minlength=len(labs))
    w = torch.tensor(len(yg[mask]) / (len(labs) * np.maximum(counts, 1)),
                     dtype=torch.float32)
    head = torch.nn.Linear(hidden, len(labs))
    opt = torch.optim.AdamW(head.parameters(), lr=1e-3, weight_decay=1e-2)
    lossf = torch.nn.CrossEntropyLoss(weight=w)
    Xt = torch.tensor(MB_G[mask])
    yt = torch.tensor(yg[mask], dtype=torch.long)
    head.train()
    for _ep in range(30):
        perm = torch.randperm(len(Xt))
        for i in range(0, len(perm), 16):
            idx = perm[i:i + 16]
            opt.zero_grad()
            lossf(head(Xt[idx]), yt[idx]).backward()
            opt.step()
    head.eval()

    skeys = [H.case_key(t) for t in s_texts]
    emb.embed_keys(s_texts, skeys)
    SV = emb.vectors(skeys)
    MB_S = np.stack(mb_embed(s_texts))

    # --- GUARD 1: corpus<->replay disjointness, BEFORE scoring ---------
    leaks = H.replay_disjointness(s_texts, gold,
                                  corpus_vectors=V, replay_vectors=SV,
                                  sim_threshold=SIM_THRESHOLD)
    if leaks:
        print(H.format_leaks(leaks), file=sys.stderr)
        print("REFUSING to score: the ruler is not disjoint from the "
              "fitting corpus (train-on-test). Fix the corpus/replay "
              "before re-running.", file=sys.stderr)
        return 2

    def centroid_routes(qv, text):
        sims = C @ qv
        order = np.argsort(-sims)
        top, second = float(sims[order[0]]), float(sims[order[1]])
        lab = labs[int(order[0])]
        routed = top >= 0.60 and (top - second) >= 0.030
        if routed and lab == "quickplan" and not ORCH.search(text):
            routed = False
        return routed, lab, top - second

    # --- policy: double-confidence -------------------------------------
    def run_double_confidence():
        a = aok = b = bok = nchain = 0
        for i, true in enumerate(s_true):
            routed, lab, margin = centroid_routes(SV[i], s_texts[i])
            if routed:
                a += 1
                aok += lab == true
                continue
            with torch.no_grad():
                p = torch.softmax(head(torch.tensor(MB_S[i][None])), dim=-1)[0]
            li = int(np.argmax(p))
            if float(p.max()) >= 0.70 and labs[li] == lab:
                b += 1
                bok += labs[li] == true
                continue
            nchain += 1
        acc, verdict = verdict_for(a + b, aok + bok, nchain, len(s_texts))
        return {"n": len(s_texts), "n_scored": len(s_texts),
                "a_routes": a, "a_correct": aok,
                "b_routes": b, "b_correct": bok, "chain": nchain,
                "routed": a + b, "min_routed": MIN_ROUTED,
                "system_accuracy": round(acc, 4),
                "acceptance_threshold": CHAIN, "verdict": verdict}

    # --- policy: tfidf-veto (ALT-5) ------------------------------------
    def run_tfidf_veto():
        try:
            from sklearn.feature_extraction.text import TfidfVectorizer
            from sklearn.linear_model import LogisticRegression
            from sklearn.pipeline import make_pipeline
        except ImportError as e:
            unvalidated("tfidf-veto", f"scikit-learn unavailable: {e}")
            return None
        gtexts = [gold[i].text for i in range(len(gold)) if mask[i]]
        glabels = [intents[i] for i in range(len(gold)) if mask[i]]
        clf = make_pipeline(
            TfidfVectorizer(analyzer="char_wb", ngram_range=(2, 4), min_df=1,
                            sublinear_tf=True),
            LogisticRegression(max_iter=2000, C=10.0),
        )
        clf.fit(gtexts, glabels)
        classes = list(clf.classes_)
        probs = clf.predict_proba(s_texts)
        a = aok = nchain = 0
        for i, true in enumerate(s_true):
            routed, lab, _margin = centroid_routes(SV[i], s_texts[i])
            if routed and classes[int(np.argmax(probs[i]))] == lab:
                a += 1
                aok += lab == true
                continue
            nchain += 1
        acc, verdict = verdict_for(a, aok, nchain, len(s_texts))
        return {"n": len(s_texts), "n_scored": len(s_texts),
                "a_routes": a, "a_correct": aok, "chain": nchain,
                "routed": a, "min_routed": MIN_ROUTED,
                "system_accuracy": round(acc, 4),
                "acceptance_threshold": CHAIN, "verdict": verdict}

    rc = 0
    if "double-confidence" in policies:
        payload = run_double_confidence()
        path = write_artifact("double-confidence", payload)
        print(f"double-confidence: acc={payload['system_accuracy']:.4f} "
              f"verdict={payload['verdict']} routed={payload['routed']} "
              f"(floor {MIN_ROUTED}) -> {path}")
    if "tfidf-veto" in policies:
        payload = run_tfidf_veto()
        if payload is None:
            rc = 3
        else:
            path = write_artifact("tfidf-veto", payload)
            print(f"tfidf-veto: acc={payload['system_accuracy']:.4f} "
                  f"verdict={payload['verdict']} routed={payload['routed']} "
                  f"(floor {MIN_ROUTED}) -> {path}")
    return rc


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--policy", default="all",
                    choices=["all", "double-confidence", "tfidf-veto"])
    ap.add_argument("--check-overlap", action="store_true",
                    help="run only the corpus<->replay disjointness guard "
                         "(no embed server needed)")
    args = ap.parse_args()

    if args.check_overlap:
        return check_overlap_only()

    policies = ({"double-confidence", "tfidf-veto"} if args.policy == "all"
                else {args.policy})
    return run_full(policies)


if __name__ == "__main__":
    sys.exit(main())
