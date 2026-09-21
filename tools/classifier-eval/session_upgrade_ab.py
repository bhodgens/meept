"""Leaf 03 A/B: session-state upgrade offline replay.

Reproduces the iter-20 cascade simulation (centroid Door-A -> ModernBERT
probe Door-B -> chain fall-through C) with per-case verdict capture, then
applies the session-state upgrade predicate ON/OFF and scores both legs.

Seeding model (plan leaf 03): every gold-quickplan case is treated as
arriving in a session WITH quickplan evidence; every non-quickplan case
arrives with NO evidence. Disclosed in the report.

Privacy: artifacts carry case INDEX, stage, verdict lane, expected lane,
and cue/evidence booleans only — never message text.
"""
import json
import re
import sys
from pathlib import Path

import numpy as np
import torch
from transformers import AutoTokenizer, AutoModel

sys.path.insert(0, "/Users/caimlas/git/meept/tools/classifier-eval")
import eval_harness as H

MODEL = "/Volumes/LLMs/answerdotai/ModernBERT-base"
OUT = Path("/Users/caimlas/git/meept/tools/classifier-eval/results/session-upgrade")
OUT.mkdir(parents=True, exist_ok=True)
CHAIN_ACC = 0.868
BOUNDARY = {"code", "debug", "review", "plan", "git", "analyze"}

# QuickPlanCuePattern (internal/agent/quickplan_cue.go:17-24), ported 1:1.
CUE = re.compile(r"(?i)\b(subagents?|tasks? \d|task list|waves?|leaves?|leaf \d|"
    r"plan\.md|handoff|checklist|in order|one at a time|sealed plan|tracking table|"
    r"as you (find|go)|, then\b|and correct them|and fix them|without (asking|stopping)|"
    r"no check-?ins?|just (do|make|apply)|make it happen|to completion|finish the remaining|"
    r"carry on with the plan|execute (the|what)|implement (the|all) plan|implement tasks?|"
    r"work (through|items)|knock out|carry out|complete the outstanding)\b")


def upgrade_applies(verdict, cue_hit, has_evidence, knob):
    if not knob or not has_evidence or cue_hit is False:
        return False
    if verdict == "quickplan" or verdict not in BOUNDARY:
        return False
    return True


def main():
    torch.manual_seed(42)
    tok = AutoTokenizer.from_pretrained(MODEL)
    base = AutoModel.from_pretrained(MODEL).eval()
    hidden = base.config.hidden_size

    def mb_embed(texts, batch=16):
        out = []
        for i in range(0, len(texts), batch):
            enc = tok(texts[i:i+batch], padding=True, truncation=True,
                      max_length=512, return_tensors="pt")
            with torch.no_grad():
                hs = base(**enc).last_hidden_state
            mask = enc["attention_mask"].unsqueeze(-1).float()
            v = (hs * mask).sum(1) / mask.sum(1).clamp(min=1)
            out.append(torch.nn.functional.normalize(v, dim=1))
        return torch.cat(out).numpy()

    cases_b, cases_a = H.load_cases()
    gold = cases_b + cases_a
    gkeys = [H.case_key(c.text) for c in gold]
    intents = [c.intent for c in gold]
    MB_G = np.stack(mb_embed([c.text for c in gold]))

    qemb = H.Embedder("http://127.0.0.1:8090/v1", "qwen3-embedding", "")
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
    for ep in range(30):
        perm = torch.randperm(len(Xt))
        for i in range(0, len(perm), 16):
            idx = perm[i:i+16]
            opt.zero_grad()
            lossf(head(Xt[idx]), yt[idx]).backward()
            opt.step()
    head.eval()
    Ptr = torch.softmax(head(Xt), dim=-1).detach().numpy()
    tau = float(np.quantile(Ptr.max(axis=1), 0.5))
    print(f"probe tau={tau:.3f}")

    rp = Path("/Users/caimlas/git/meept/tools/classifier-eval/replay-gold.local.json5")
    silver = json.loads("[" + ",".join(re.findall(r'\{\s*"input".*?\}', rp.read_text(), re.S)) + "]")
    s_texts = [c["input"] for c in silver]
    MB_S = np.stack(mb_embed(s_texts))
    skeys = [H.case_key(t) for t in s_texts]
    qemb.embed_keys(s_texts, skeys)
    QV_S = qemb.vectors(skeys)

    rows = []
    for ci, (c, mv, qv) in enumerate(zip(silver, MB_S, QV_S)):
        true = c["expected_intent"]
        cue = bool(CUE.search(c["input"]))
        strong = any(s in c["input"].lower() for s in ("subagent", "implement the plan", "implement tasks"))
        verdict, stage = None, None
        sims = C @ qv
        order = np.argsort(-sims)
        margin = float(sims[order[0]] - sims[order[1]])
        if sims[order[0]] >= 0.60 and margin >= 0.030:
            verdict, stage = labs[int(order[0])], "A"
        else:
            with torch.no_grad():
                p = torch.softmax(head(torch.tensor(mv[None])), dim=-1)[0]
            lab = labs[int(p.argmax())]
            conf = float(p.max())
            if lab == "quickplan" and not cue:
                verdict, stage = None, "C"
            elif conf >= tau:
                verdict, stage = lab, "B"
            else:
                verdict, stage = None, "C"
        rows.append({
            "case": ci,
            "stage": stage,
            "verdict": verdict,
            "expected": true,
            "cue": cue,
            "strong": strong,
            "evidence": true == "quickplan",  # seeding model
        })

    def score(knob, strong_only):
        correct = 0.0
        routes = 0
        upgrades = []
        flips = []
        for r in rows:
            v = r["verdict"]
            if v is None:
                correct += CHAIN_ACC
                continue
            cue_ok = r["strong"] if strong_only else r["cue"]
            applied = (knob and r["evidence"] and cue_ok
                       and v != "quickplan" and v in BOUNDARY)
            final = "quickplan" if applied else v
            if applied:
                upgrades.append(r["case"])
            routes += 1
            if final == r["expected"]:
                correct += 1
            else:
                flips.append({"case": r["case"], "stage": r["stage"],
                              "verdict": v, "final": final,
                              "expected": r["expected"]})
        n = len(rows)
        return {"correct": correct, "n": n,
                "sys_acc": round(correct / n, 4), "routes": routes,
                "upgrades": upgrades, "wrong": flips}

    result = {
        "replay_n": len(rows),
        "chain_acc": CHAIN_ACC,
        "tau": round(tau, 4),
        "leg_off": score(False, False),
        "leg_on_cue": score(True, False),
        "leg_on_strong": score(True, True),
        "note": "offline cascade simulation per iter-20 method; session evidence seeded = gold quickplan; artifacts carry no message text",
    }
    json.dump(result, open(OUT / "summary.json", "w"), indent=1)
    per_case = [{k: r[k] for k in ("case", "stage", "verdict", "expected", "cue", "strong", "evidence")} for r in rows]
    json.dump(per_case, open(OUT / "per-case.json", "w"), indent=1)
    print(json.dumps({k: v for k, v in result.items() if k != "note"}, indent=1)[:1200])

if __name__ == "__main__":
    main()
