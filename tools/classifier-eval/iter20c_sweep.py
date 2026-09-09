#!/usr/bin/env python3
"""iter-20c: sweep B-route policy over tau x quickplan-guard variants.

v2: 67.5% (cue-features poisoned the probe)
v2b: 83.7% (plain probe + quickplan-cue guard), misses: 2 qp->code
    (guard blocked the routes), 1 qp->platform, 1 qp->git.

This sweep tries: lowering tau (admit more B-routes) and relaxing the
quickplan guard (partial cue OR high confidence). Also tries guard on
confidence instead of cue.
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
tau50 = float(np.quantile(Ptr.max(axis=1), 0.5))

rp = Path("/Users/caimlas/git/meept/tools/classifier-eval/replay-gold.local.json5")
silver = json.loads("[" + ",".join(re.findall(r'\{\s*"input".*?\}', rp.read_text(), re.S)) + "]")
s_texts = [c["input"] for c in silver]
MB_S = np.stack(mb_embed(s_texts))
skeys = [H.case_key(t) for t in s_texts]
qemb.embed_keys(s_texts, skeys)
QV_S = qemb.vectors(skeys)

# precompute per-case: stage-A result + probe distribution
cases_cache = []
for c, mv, qv in zip(silver, MB_S, QV_S):
    sims = C @ qv
    order = np.argsort(-sims)
    margin = float(sims[order[0]] - sims[order[1]])
    a_lab = labs[int(order[0])] if (sims[order[0]] >= 0.60 and margin >= 0.030) else None
    with torch.no_grad():
        p = torch.softmax(head(torch.tensor(mv[None])), dim=-1)[0]
    has_cue = bool(ORCH.search(c["input"]))
    cases_cache.append({"true": c["expected_intent"], "a_lab": a_lab,
                        "p": p.detach().cpu().numpy(), "cue": has_cue,
                        "text": c["input"][:60]})


def run(tau, guard_mode, guard_conf):
    tot = a_n = a_ok = b_n = b_ok = c_n = 0
    for cc in cases_cache:
        tot += 1
        if cc["a_lab"] is not None:
            a_n += 1
            a_ok += cc["a_lab"] == cc["true"]
            continue
        z = cc["p"]
        lab = labs[int(np.argmax(z))]
        conf = float(z.max())
        # quickplan guard variants
        if lab == "quickplan":
            if guard_mode == "cue" and not cc["cue"]:
                c_n += 1
                continue
            if guard_mode == "conf" and conf < guard_conf:
                c_n += 1
                continue
            if guard_mode == "cue_or_conf" and not (cc["cue"] or conf >= guard_conf):
                c_n += 1
                continue
        if conf < tau:
            c_n += 1
            continue
        b_n += 1
        b_ok += lab == cc["true"]
    acc = (sum(1 for cc in cases_cache if cc["a_lab"] == cc["true"])
           + b_ok + c_n * H.CHAIN_BASELINE) / tot
    return {"tau": round(tau, 3), "guard": guard_mode,
            "guard_conf": guard_conf, "A": a_n, "B": b_n, "C": c_n,
            "sys_acc": round(acc, 4)}


rows = []
for tau, gm, gc in [
    (tau50, "cue", 0), (tau50 * 0.9, "cue", 0), (tau50 * 0.8, "cue", 0),
    (tau50 * 0.7, "cue", 0),
    (tau50, "conf", 0.25), (tau50, "conf", 0.20), (tau50, "conf", 0.15),
    (tau50 * 0.9, "conf", 0.20),
    (tau50 * 0.9, "cue_or_conf", 0.22), (tau50 * 0.8, "cue_or_conf", 0.22),
    (tau50 * 0.7, "cue_or_conf", 0.25), (tau50 * 0.7, "cue_or_conf", 0.20),
    (tau50 * 0.6, "cue_or_conf", 0.22),
]:
    r = run(tau, gm, gc)
    rows.append(r)
    print(r)

rows.sort(key=lambda r: -r["sys_acc"])
outdir = H.RESULTS / "iter-20"
outdir.mkdir(parents=True, exist_ok=True)
(outdir / "policy-sweep.json").write_text(json.dumps(rows, indent=1))
print(f"\nBEST: {rows[0]}")
