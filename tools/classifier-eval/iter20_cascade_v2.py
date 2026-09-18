#!/usr/bin/env python3
"""iter-20: retrain + validate with orchestration-cue features.

Probe features = [ModernBERT mean-pool (768)] ++ [cue one-hot vector]:
  cue 1: orchestration markers (subagent, task(s) N, wave, leaf, plan.md,
         handoff, tree, checklist, "in order", "one at a time", "then")
  cue 2: explicit-autonomy markers ("no check-ins", "just do", "make it
         happen", "without (asking|stopping)", "as you find", "as you go")
Both stages retrained; validated on the adjudicated gold replay.

This file is import-safe: model loading, corpus reads, training, and the
embedding-server round trips happen only inside ``main()``. Result
construction helpers (``miss_record``/``build_result``/``emit_result``)
replace private replay text with stable case identifiers (privacy
contract, routing-repair leaf 01): public outputs carry
``input_case_key`` fields, never raw input excerpts, on disk or stdout.
"""
import sys
sys.path.insert(0, "/Users/caimlas/git/meept/tools/classifier-eval")
import numpy as np
import torch
import json
import re
import eval_harness as H
from pathlib import Path
from transformers import AutoModel, AutoTokenizer

MODEL = "/Volumes/LLMs/answerdotai/ModernBERT-base"
ORCH = re.compile(r"(?i)\b(subagents?|tasks? \d|task list|waves?|leaves?|leaf \d|plan\.md|handoff|"
                  r"checklist|in order|one at a time|tree|sealed plan|tracking table)\b|,? then\b")
AUTO = re.compile(r"(?i)\b(no check-?ins?|just (do|make|apply)|make it happen|without (asking|stopping)|"
                  r"as you (find|go)|don'?t (ask|stop)|to completion|autonomously)\b")


def cues(text):
    return np.array([1.0 if ORCH.search(text) else 0.0,
                     1.0 if AUTO.search(text) else 0.0], dtype=np.float32)


def featurize(texts, vecs):
    """concat embeddings [n,768] with cue features [n,2] -> [n,770]"""
    cf = np.stack([cues(t) for t in texts])
    return np.concatenate([vecs, cf], axis=1)


def mb_embed(texts, batch=16):
    out = []
    with torch.no_grad():
        for i in range(0, len(texts), batch):
            enc = tok(texts[i:i + batch], padding=True, truncation=True,
                      max_length=256, return_tensors="pt").to(dev)
            o = base(**enc).last_hidden_state
            m = enc["attention_mask"].unsqueeze(-1).float()
            v = (o * m).sum(1) / m.sum(1).clamp(min=1e-9)
            out.extend(x.cpu().numpy().astype(np.float32)
                       for x in torch.nn.functional.normalize(v, dim=-1))
    return out


def miss_record(stage, input_text, expected, predicted):
    """Miss entry with the private excerpt replaced by its case key.

    ``input_text`` is the raw private replay input; it is used only to
    derive the stable identifier and is never stored or returned."""
    return {"stage": stage,
            "input_case_key": H.case_key(input_text),
            "expected_intent": expected,
            "predicted_intent": predicted}


def build_result(replay_n, a_n, a_ok, b_n, b_ok, c_n, tau, misses):
    """Result dict: historical schema, misses redacted via miss_record()."""
    sys_acc = (a_ok + b_ok + c_n * H.CHAIN_BASELINE) / replay_n
    return {
        "replay_n": replay_n,
        "stageA": {"routes": a_n, "correct": a_ok,
                   "precision": round(a_ok / max(a_n, 1), 3)},
        "stageB": {"routes": b_n, "correct": b_ok,
                   "precision": round(b_ok / max(b_n, 1), 3)},
        "stageC_chain": c_n,
        "expected_system_accuracy": round(sys_acc, 4),
        "tau": round(tau, 3),
        "misses": list(misses),
    }


def emit_result(result, outdir, filename):
    """Write the result JSON into outdir; returns the serialized payload."""
    outdir = Path(outdir)
    outdir.mkdir(parents=True, exist_ok=True)
    payload = json.dumps(result, indent=1)
    (outdir / filename).write_text(payload)
    return payload


def format_misses_report(misses):
    """Human-readable miss list for stdout: identifiers only, no excerpts."""
    lines = [f"misses ({len(misses)}):"]
    for m in misses:
        lines.append(f"  [{m['stage']}] true={m['expected_intent']} "
                     f"pred={m['predicted_intent']} :: case={m['input_case_key']}")
    return "\n".join(lines)


def main():
    torch.manual_seed(42)
    global tok, base, dev, hidden
    dev = "cpu"
    tok = AutoTokenizer.from_pretrained(MODEL)
    base = AutoModel.from_pretrained(MODEL).to(dev).eval()
    hidden = base.config.hidden_size
    cases_b, cases_a = H.load_cases()
    gold = cases_b + cases_a
    gkeys = [H.case_key(c.text) for c in gold]
    print(f"gold {len(gold)}; embedding (CPU)...", file=sys.stderr)
    MB_G = np.stack(mb_embed([c.text for c in gold]))
    FG = featurize([c.text for c in gold], MB_G)
    intents = [c.intent for c in gold]
    is_ood = np.array([c.ood for c in gold])

    QS = {"url": "http://127.0.0.1:8090/v1", "model": "qwen3-embedding", "instruction": ""}
    qemb = H.Embedder(QS["url"], QS["model"], "")
    qemb.embed_keys([c.text for c in gold], gkeys)
    QV_gold = qemb.vectors(gkeys)

    labs = sorted({c.intent for c in gold if not c.ood})
    C = np.stack([QV_gold[[i for i in range(len(gold)) if intents[i] == l]].mean(axis=0)
                  for l in labs])
    C /= (np.linalg.norm(C, axis=1, keepdims=True) + 1e-12)

    yg = np.array([labs.index(c.intent) if not c.ood else -1 for c in gold])
    mask = yg >= 0
    counts = np.bincount(yg[mask], minlength=len(labs))
    # class weights + 2x oversample quickplan to counter code-class mass
    w = torch.tensor(len(yg[mask]) / (len(labs) * np.maximum(counts, 1)), dtype=torch.float32)
    lossf = torch.nn.CrossEntropyLoss(weight=w)
    head = torch.nn.Linear(hidden + 2, len(labs))
    opt = torch.optim.AdamW(head.parameters(), lr=1e-3, weight_decay=1e-2)
    Xall = FG[mask]
    yall = yg[mask]
    # oversample quickplan x2
    qp = np.where(yall == labs.index("quickplan"))[0]
    Xt = torch.tensor(np.concatenate([Xall, Xall[qp]]))
    yt = torch.tensor(np.concatenate([yall, yall[qp]]), dtype=torch.long)
    head.train()
    for ep in range(30):
        perm = torch.randperm(len(Xt))
        for i in range(0, len(perm), 16):
            idx = perm[i:i + 16]
            opt.zero_grad()
            lossf(head(Xt[idx]), yt[idx]).backward()
            opt.step()
    head.eval()
    Ptr = torch.softmax(head(Xt), dim=-1).detach().numpy()
    tau = float(np.quantile(Ptr.max(axis=1), 0.5))
    print(f"probe trained ({hidden + 2}-dim features, quickplan x2 oversample); tau={tau:.3f}",
          file=sys.stderr)

    rp = Path("/Users/caimlas/git/meept/tools/classifier-eval/replay-gold.local.json5")
    rtext = rp.read_text()
    silver = json.loads("[" + ",".join(re.findall(r'\{\s*"input".*?\}', rtext, re.S)) + "]")
    s_texts = [c["input"] for c in silver]
    MB_S = np.stack(mb_embed(s_texts))
    FS = featurize(s_texts, MB_S)
    skeys = [H.case_key(t) for t in s_texts]
    qemb.embed_keys(s_texts, skeys)
    QV_S = qemb.vectors(skeys)
    print(f"replay-gold {len(silver)}", file=sys.stderr)

    tot = a_n = a_ok = b_n = b_ok = c_n = 0
    misses = []
    for c, fv, qv in zip(silver, FS, QV_S):
        tot += 1
        true = c["expected_intent"]
        sims = C @ qv
        order = np.argsort(-sims)
        margin = float(sims[order[0]] - sims[order[1]])
        if sims[order[0]] >= 0.60 and margin >= 0.030:
            a_n += 1
            ok = labs[int(order[0])] == true
            a_ok += ok
            if not ok:
                misses.append(miss_record("A", c["input"], true, labs[int(order[0])]))
            continue
        with torch.no_grad():
            p = torch.softmax(head(torch.tensor(fv[None])), dim=-1)[0]
        if float(p.max()) >= tau:
            b_n += 1
            lab = labs[int(p.argmax())]
            ok = lab == true
            b_ok += ok
            if not ok:
                misses.append(miss_record("B", c["input"], true, lab))
            continue
        c_n += 1

    res = build_result(replay_n=tot, a_n=a_n, a_ok=a_ok, b_n=b_n, b_ok=b_ok,
                       c_n=c_n, tau=tau, misses=misses)
    outdir = H.RESULTS / "iter-20"
    emit_result(res, outdir, "quickplan-v2.json")
    print(json.dumps(res, indent=1))


if __name__ == "__main__":
    main()
