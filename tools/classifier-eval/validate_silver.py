#!/usr/bin/env python3
"""M4 silver-replay validation of the 3-stage cascade (standalone script).

Reproducible: trains stage-B probe on ALL gold cases, validates on the
untracked Hermes-transcript silver corpus. Writes results under
results/m4-silver/.
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
gold_keys = [H.case_key(c.text) for c in gold]
gold_texts = [c.text for c in gold]
print(f"gold {len(gold)}; embedding (CPU ~2 min)...", file=sys.stderr)
MB_GOLD = mb_embed(gold_texts)
intents = [c.intent for c in gold]
is_ood = np.array([c.ood for c in gold])

QS = {"url": "http://127.0.0.1:8090/v1", "model": "qwen3-embedding", "instruction": ""}
qemb = H.Embedder(QS["url"], QS["model"], "")
qemb.embed_keys(gold_texts, gold_keys)
QV_gold = qemb.vectors(gold_keys)

labs = sorted({c.intent for c in gold if not c.ood})
Xg = np.stack(MB_GOLD)
yg = np.array([labs.index(c.intent) if not c.ood else -1 for c in gold])
mask = yg >= 0
counts = np.bincount(yg[mask], minlength=len(labs))
w = torch.tensor(len(yg[mask]) / (len(labs) * np.maximum(counts, 1)), dtype=torch.float32)
lossf = torch.nn.CrossEntropyLoss(weight=w)
head = torch.nn.Linear(hidden, len(labs))
opt = torch.optim.AdamW(head.parameters(), lr=1e-3, weight_decay=1e-2)
Xt = torch.tensor(Xg[mask])
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
print(f"probe trained; tau={tau:.3f}", file=sys.stderr)

C = np.stack([QV_gold[[i for i in range(len(gold)) if intents[i] == l]].mean(axis=0)
              for l in labs])
C /= (np.linalg.norm(C, axis=1, keepdims=True) + 1e-12)

text = Path("/Users/caimlas/git/meept/tools/classifier-eval/replay-corpus.local.json5").read_text()
silver = json.loads("[" + ",".join(re.findall(r'\{\s*"input".*?\}', text, re.S)) + "]")
print(f"silver {len(silver)}", file=sys.stderr)
silver_texts = [c["input"] for c in silver]
MB_S = mb_embed(silver_texts)
skeys = [H.case_key(t) for t in silver_texts]
qemb.embed_keys(silver_texts, skeys)
QV_S = qemb.vectors(skeys)

tot = a_n = a_ok = b_n = b_ok = c_n = 0
misses = []
for c, mv, qv in zip(silver, MB_S, QV_S):
    tot += 1
    true = c["expected_intent"]
    sims = C @ qv
    order = np.argsort(-sims)
    if sims[order[0]] >= 0.60 and (sims[order[0]] - sims[order[1]]) >= 0.030:
        a_n += 1
        if labs[order[0]] == true:
            a_ok += 1
        else:
            misses.append(("A", c["input"][:60], true, labs[order[0]]))
        continue
    with torch.no_grad():
        p = torch.softmax(head(torch.tensor(mv[None])), dim=-1)[0]
    if float(p.max()) >= tau:
        b_n += 1
        if labs[int(p.argmax())] == true:
            b_ok += 1
        else:
            misses.append(("B", c["input"][:60], true, labs[int(p.argmax())]))
        continue
    c_n += 1

sys_acc = (a_ok + b_ok + c_n * H.CHAIN_BASELINE) / tot
out = {
    "silver_n": tot, "stageA": {"routes": a_n, "correct": a_ok,
                                 "precision": round(a_ok / max(a_n, 1), 3)},
    "stageB": {"routes": b_n, "correct": b_ok,
                "precision": round(b_ok / max(b_n, 1), 3)},
    "stageC_chain": c_n, "expected_system_accuracy": round(sys_acc, 4),
    "tau": round(tau, 3), "misses": misses,
}
outdir = H.RESULTS / "m4-silver"
outdir.mkdir(parents=True, exist_ok=True)
(outdir / "cascade-validation.json").write_text(json.dumps(out, indent=1))
print(json.dumps({k: v for k, v in out.items() if k != "misses"}, indent=1))
print(f"misses ({len(misses)}):")
for m in misses:
    print(f"  [{m[0]}] true={m[2]} pred={m[3]} :: {m[1]}")
