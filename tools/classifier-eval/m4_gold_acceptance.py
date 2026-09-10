#!/usr/bin/env python3
"""M4 gold-replay acceptance run (campaign gate: system acc > 86.8%).

Runs the adjudicated 48-case gold replay through the full cascade with
the ADOPTED double-confidence policy:
  Door 1: centroid sim >= 0.60 AND margin >= 0.030 (cue guard on qp)
  Door 2 (probe): only when BOTH sim >= 0.70 AND same-class agreement
                  (the adopted double-confidence rule)
  Door C: everything else = chain floor 86.8%

This is the FINAL-REPORT acceptance number.
"""
import sys, json, re
sys.path.insert(0, "/Users/caimlas/git/meept/tools/classifier-eval")
import numpy as np
import torch
import eval_harness as H
from pathlib import Path
from transformers import AutoModel, AutoTokenizer

MODEL = "/Volumes/LLMs/answerdotai/ModernBERT-base"
torch.manual_seed(42)
tok = AutoTokenizer.from_pretrained(MODEL)
base = AutoModel.from_pretrained(MODEL).to("cpu").eval()
hidden = base.config.hidden_size

def mb_embed(texts, batch=16):
    out = []
    with torch.no_grad():
        for i in range(0, len(texts), batch):
            enc = tok(texts[i:i+batch], padding=True, truncation=True,
                      max_length=256, return_tensors="pt")
            o = base(**enc).last_hidden_state
            m = enc["attention_mask"].unsqueeze(-1).float()
            v = (o * m).sum(1) / m.sum(1).clamp(min=1e-9)
            out.extend(x.cpu().numpy().astype(np.float32)
                       for x in torch.nn.functional.normalize(v, dim=-1))
    return out

# gold corpus -> train both stages
cases_b, cases_a = H.load_cases()
gold = cases_b + cases_a
gkeys = [H.case_key(c.text) for c in gold]
intents = [c.intent for c in gold]
labs = sorted({c.intent for c in gold if not c.ood})

emb = H.Embedder("http://127.0.0.1:8090/v1", "qwen3-embedding", "")
emb.embed_keys([c.text for c in gold], gkeys)
V = emb.vectors(gkeys)
C = np.stack([V[[i for i in range(len(gold)) if intents[i] == l]].mean(axis=0)
              for l in labs])
C /= (np.linalg.norm(C, axis=1, keepdims=True) + 1e-12)
yg = np.array([labs.index(c.intent) if not c.ood else -1 for c in gold])

MB_G = np.stack(mb_embed([c.text for c in gold]))
mask = yg >= 0
counts = np.bincount(yg[mask], minlength=len(labs))
w = torch.tensor(len(yg[mask]) / (len(labs) * np.maximum(counts, 1)), dtype=torch.float32)
head = torch.nn.Linear(hidden, len(labs))
opt = torch.optim.AdamW(head.parameters(), lr=1e-3, weight_decay=1e-2)
lossf = torch.nn.CrossEntropyLoss(weight=w)
Xt = torch.tensor(MB_G[mask]); yt = torch.tensor(yg[mask], dtype=torch.long)
head.train()
for ep in range(30):
    perm = torch.randperm(len(Xt))
    for i in range(0, len(perm), 16):
        idx = perm[i:i+16]
        opt.zero_grad(); lossf(head(Xt[idx]), yt[idx]).backward(); opt.step()
head.eval()

# adjudicated gold replay (untracked)
rp = Path("/Users/caimlas/git/meept/tools/classifier-eval/replay-gold.local.json5")
silver = json.loads("[" + ",".join(re.findall(r"\{\s*\"input\".*?\}",
                       rp.read_text(), re.S)) + "]")
s_texts = [c["input"] for c in silver]
s_true = [c["expected_intent"] for c in silver]
skeys = [H.case_key(t) for t in s_texts]
emb.embed_keys(s_texts, skeys)
SV = emb.vectors(skeys)
MB_S = np.stack(mb_embed(s_texts))

ORCH = re.compile(r"(?i)\b(subagents?|tasks? \d|task list|waves?|leaves?|leaf \d|plan\.md|handoff|checklist|in order|one at a time|sealed plan|tracking table|as you (find|go)|, then\b|and correct them|and fix them|without (asking|stopping)|no check-?ins?|just (do|make|apply)|to completion|implement tasks?|work (through|items))\b")
CHAIN = 0.868

a = aok = b = bok = nchain = 0
rows = []
for text, true in zip(s_texts, s_true):
    qv = SV[len(rows)]
    sims = C @ qv
    order = np.argsort(-sims)
    top, second = float(sims[order[0]]), float(sims[order[1]])
    lab = labs[int(order[0])]
    routed1 = top >= 0.60 and (top - second) >= 0.030
    if routed1 and not (lab == "quickplan" and not ORCH.search(text)):
        a += 1; aok += lab == true
        rows.append((true, "A", lab, top - second)); continue
    # door 2 double-confidence: prob>=0.7 AND same-class agreement with A
    with torch.no_grad():
        p = torch.softmax(head(torch.tensor(MB_S[len(rows)][None])), dim=-1)[0]
    li = int(np.argmax(p))
    d2 = float(p.max()) >= 0.70 and labs[li] == lab and lab is not None
    if d2:
        b += 1; bok += labs[li] == true
        rows.append((true, "B", labs[li], float(p.max()))); continue
    nchain += 1
    rows.append((true, "C", "chain", CHAIN))

acc = (aok + bok + nchain * CHAIN) / len(s_texts)
print(f"gold replay n={len(s_texts)} (double-confidence policy)")
print(f"  Door A: {a} routes, {aok} correct")
print(f"  Door B: {b} routes, {bok} correct")
print(f"  Chain C: {nchain} (expected {CHAIN:.1%} each)")
print(f"  SYSTEM ACCURACY: {acc:.2%}  (acceptance: > 86.8%)")
print(f"  VERDICT: {'PASS' if acc > 0.868 else 'FAIL'}")
Path("/Users/caimlas/git/meept/tools/classifier-eval/results/m4-gold-acceptance.json").write_text(json.dumps({
    "n": len(s_texts), "policy": "double-confidence",
    "a_routes": a, "a_correct": aok, "b_routes": b, "b_correct": bok,
    "chain": nchain, "system_accuracy": round(acc, 4),
    "acceptance_threshold": 0.868, "verdict": "PASS" if acc > 0.868 else "FAIL",
}, indent=2))
