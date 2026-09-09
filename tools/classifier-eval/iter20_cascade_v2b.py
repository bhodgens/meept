#!/usr/bin/env python3
"""iter-20b: fix the quickplan over-capture.

iter-20 v2's cue features made B over-fire quickplan (the cue vector
itself dominates: ANY 'review...then' hits cue-1). The cue must gate
the B-route DECISION, not the features. Correct architecture:
  - probe sees plain embeddings (768)
  - B-route rule: prob >= tau AND (pred == quickplan -> require cue
    support; pred != quickplan -> require NOT strongly-quickplan text)
Actually simplest correct form per the adjudication: the tau stays for
non-quickplan predictions; for a quickplan prediction require the
orchestration cue to be present.
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

ORCH = re.compile(r"(?i)\b(subagents?|tasks? \d|task list|waves?|leaves?|leaf \d|plan\.md|handoff|"
                  r"checklist|in order|one at a time|sealed plan|tracking table|as you (find|go)|"
                  r"then (run|push|deploy)|, then\b|and correct them|and fix them|without (asking|stopping)|"
                  r"no check-?ins?|just (do|make|apply)|make it happen|to completion|finish the remaining|"
                  r"carry on with the plan|execute (the|what)|implement (the|all) plan|implement tasks?|"
                  r"work (through|items)|knock out|carry out|complete the outstanding)\b")


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
print(f"gold {len(gold)}; embedding...", file=sys.stderr)
MB_G = np.stack(mb_embed([c.text for c in gold]))
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
print(f"probe trained (plain 768); tau={tau:.3f}", file=sys.stderr)

rp = Path("/Users/caimlas/git/meept/tools/classifier-eval/replay-gold.local.json5")
silver = json.loads("[" + ",".join(re.findall(r'\{\s*"input".*?\}', rp.read_text(), re.S)) + "]")
s_texts = [c["input"] for c in silver]
MB_S = np.stack(mb_embed(s_texts))
skeys = [H.case_key(t) for t in s_texts]
qemb.embed_keys(s_texts, skeys)
QV_S = qemb.vectors(skeys)

tot = a_n = a_ok = b_n = b_ok = c_n = 0
misses = []
for c, mv, qv in zip(silver, MB_S, QV_S):
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
            misses.append(("A", c["input"][:60], true, labs[int(order[0])]))
        continue
    with torch.no_grad():
        p = torch.softmax(head(torch.tensor(mv[None])), dim=-1)[0]
    lab = labs[int(p.argmax())]
    conf = float(p.max())
    # quickplan guard: quickplan prediction requires orchestration cue
    if lab == "quickplan" and not ORCH.search(c["input"]):
        c_n += 1
        continue
    if conf >= tau:
        b_n += 1
        ok = lab == true
        b_ok += ok
        if not ok:
            misses.append(("B", c["input"][:60], true, lab))
        continue
    c_n += 1

sys_acc = (a_ok + b_ok + c_n * H.CHAIN_BASELINE) / tot
res = {"replay_n": tot,
       "stageA": {"routes": a_n, "correct": a_ok,
                  "precision": round(a_ok / max(a_n, 1), 3)},
       "stageB": {"routes": b_n, "correct": b_ok,
                  "precision": round(b_ok / max(b_n, 1), 3)},
       "stageC_chain": c_n,
       "expected_system_accuracy": round(sys_acc, 4),
       "tau": round(tau, 3), "misses": misses}
outdir = H.RESULTS / "iter-20"
outdir.mkdir(parents=True, exist_ok=True)
(outdir / "quickplan-v2b-guarded.json").write_text(json.dumps(res, indent=1))
print(json.dumps(res, indent=1))
