#!/usr/bin/env python3
"""iter-19: full 3-stage cascade with the quickplan class.

Training set: gold corpus (base+adversarial incl. quickplan anchors).
Validation: adjudicated gold replay (replay-gold.local.json5) — the
clean real-traffic ruler. Reports per-stage routes + system accuracy.
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
torch.manual_seed(42)
dev = "cpu"
tok = AutoTokenizer.from_pretrained(MODEL)
base = AutoModel.from_pretrained(MODEL).to(dev).eval()
hidden = base.config.hidden_size


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


cases_b, cases_a = H.load_cases()
gold = cases_b + cases_a
gkeys = [H.case_key(c.text) for c in gold]
print(f"gold {len(gold)}; embedding (CPU ~2-3 min)...", file=sys.stderr)
MB_G = np.stack(mb_embed([c.text for c in gold]))
intents = [c.intent for c in gold]
is_ood = np.array([c.ood for c in gold])

QS = {"url": "http://127.0.0.1:8090/v1", "model": "qwen3-embedding", "instruction": ""}
qemb = H.Embedder(QS["url"], QS["model"], "")
qemb.embed_keys([c.text for c in gold], gkeys)
QV_gold = qemb.vectors(gkeys)

labs = sorted({c.intent for c in gold if not c.ood})
print("classes:", len(labs), labs, file=sys.stderr)
C = np.stack([QV_gold[[i for i in range(len(gold)) if intents[i] == l]].mean(axis=0)
              for l in labs])
C /= (np.linalg.norm(C, axis=1, keepdims=True) + 1e-12)

yg = np.array([labs.index(c.intent) if not c.ood else -1 for c in gold])
mask = yg >= 0
counts = np.bincount(yg[mask], minlength=len(labs))
w = torch.tensor(len(yg[mask]) / (len(labs) * np.maximum(counts, 1)), dtype=torch.float32)
lossf = torch.nn.CrossEntropyLoss(weight=w)
head = torch.nn.Linear(hidden, len(labs))
opt = torch.optim.AdamW(head.parameters(), lr=1e-3, weight_decay=1e-2)
Xt = torch.tensor(MB_G[mask])
yt = torch.tensor(yg[mask], dtype=torch.long)
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
print(f"probe trained (13-way, incl quickplan); tau={tau:.3f}", file=sys.stderr)

# adjudicated gold replay
rp = Path("/Users/caimlas/git/meept/tools/classifier-eval/replay-gold.local.json5")
rtext = rp.read_text()
silver = json.loads("[" + ",".join(re.findall(r'\{\s*"input".*?\}', rtext, re.S)) + "]")
print(f"replay-gold {len(silver)}", file=sys.stderr)
s_texts = [c["input"] for c in silver]
MB_S = np.stack(mb_embed(s_texts))
skeys = [H.case_key(t) for t in s_texts]
qemb.embed_keys(s_texts, skeys)
QV_S = qemb.vectors(skeys)

tot = a_n = a_ok = b_n = b_ok = c_n = 0
a_wrong = b_wrong = 0
misses = []
per_intent = {}
for c, mv, qv in zip(silver, MB_S, QV_S):
    tot += 1
    true = c["expected_intent"]
    pi = per_intent.setdefault(true, [0, 0])  # n, correct-or-expected
    sims = C @ qv
    order = np.argsort(-sims)
    margin = float(sims[order[0]] - sims[order[1]])
    if sims[order[0]] >= 0.60 and margin >= 0.030:
        a_n += 1
        ok = labs[int(order[0])] == true
        a_ok += ok
        if not ok:
            a_wrong += 1
            misses.append(("A", c["input"][:60], true, labs[int(order[0])]))
        pi[0] += 1
        pi[1] += ok
        continue
    with torch.no_grad():
        p = torch.softmax(head(torch.tensor(mv[None])), dim=-1)[0]
    if float(p.max()) >= tau:
        b_n += 1
        lab = labs[int(p.argmax())]
        ok = lab == true
        b_ok += ok
        if not ok:
            b_wrong += 1
            misses.append(("B", c["input"][:60], true, lab))
        pi[0] += 1
        pi[1] += ok
        continue
    c_n += 1
    pi[0] += 1
    pi[1] += H.CHAIN_BASELINE  # expected chain credit

sys_acc = (a_ok + b_ok + c_n * H.CHAIN_BASELINE) / tot
result = {
    "replay_n": tot,
    "stageA": {"routes": a_n, "correct": a_ok, "precision": round(a_ok / max(a_n, 1), 3)},
    "stageB": {"routes": b_n, "correct": b_ok, "precision": round(b_ok / max(b_n, 1), 3)},
    "stageC_chain": c_n,
    "expected_system_accuracy": round(sys_acc, 4),
    "chain_only_baseline": H.CHAIN_BASELINE,
    "per_intent_expected": {k: {"n": v[0], "expected_correct": round(v[1], 2)}
                            for k, v in sorted(per_intent.items())},
    "tau": round(tau, 3),
}
outdir = H.RESULTS / "iter-19"
outdir.mkdir(parents=True, exist_ok=True)
(outdir / "quickplan-cascade.json").write_text(json.dumps(result, indent=1))
print(json.dumps({k: v for k, v in result.items() if k != "per_intent_expected"}, indent=1))
print("\nper-intent expected correct (of n):")
for k, v in result["per_intent_expected"].items():
    print(f"  {k:10} {v['expected_correct']}/{v['n']}")
print(f"\nmisses ({len(misses)}):")
for m in misses:
    print(f"  [{m[0]}] true={m[2]} pred={m[3]} :: {m[1]}")
