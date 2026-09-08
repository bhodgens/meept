#!/usr/bin/env python3
"""iter-15 full experiment: two-stage with a PROPERLY trained Stage-0.5.

Diagnosis from iter15_probe_diag (fold 0, lr 1e-3 head): forced acc 60.4%
(chance 8.3%) — features ARE informative, iter-14's failure was the 50x
head-lr underdose confirmed. But max conf only ~0.21: max-softmax on 12
classes with 202 examples is underconfident — tau calibration must be
quantile-based, not fixed.

Full protocol:
  - per fold: train linear probe (lr 1e-3, 30 epochs, CPU, class-balanced)
  - calibrate tau per fold: pick the probe's TRAIN-side quantile so that
    the top-q fraction of train examples would route; sweep q in {0.3,0.4,0.5}
  - two-stage: stage-0 (qwen3 centroid m0.030) first, probe rescues
"""
import sys
sys.path.insert(0, "/Users/caimlas/git/meept/tools/classifier-eval")
import numpy as np
import torch
import eval_harness as H

MODEL = "/Volumes/LLMs/answerdotai/ModernBERT-base"
from transformers import AutoModel, AutoTokenizer

torch.manual_seed(42)
dev = "cpu"
tok = AutoTokenizer.from_pretrained(MODEL)
base = AutoModel.from_pretrained(MODEL)
base.to(dev).eval()
hidden = base.config.hidden_size


def embed(texts, batch=16):
    out = []
    with torch.no_grad():
        for i in range(0, len(texts), batch):
            enc = tok(texts[i:i + batch], padding=True, truncation=True,
                      max_length=256, return_tensors="pt")
            o = base(**enc).last_hidden_state
            m = enc["attention_mask"].unsqueeze(-1).float()
            v = (o * m).sum(1) / m.sum(1).clamp(min=1e-9)
            out.extend(x.numpy().astype(np.float32)
                       for x in torch.nn.functional.normalize(v, dim=-1))
    return out


cases_b, cases_a = H.load_cases()
cases = cases_b + cases_a
folds = H.assign_folds(cases)
print(f"corpus {len(cases)}; CPU embedding once (~3-4 min)...", file=sys.stderr)
keys = [H.case_key(c.text) for c in cases]
MB_MEM = dict(zip(keys, embed([c.text for c in cases])))
print("embedding done", file=sys.stderr)

intents = [c.intent for c in cases]
is_ood = np.array([c.ood for c in cases])
fold_of = np.array([folds[k] for k in keys])

QS = {"url": "http://127.0.0.1:8090/v1", "model": "qwen3-embedding", "instruction": ""}
qemb = H.Embedder(QS["url"], QS["model"], "")
qtexts = [c.text for c in cases]
qkeys = [H.case_key(t) for t in qtexts]
qemb.embed_keys(qtexts, qkeys)
QV = qemb.vectors(qkeys)


def train_probe(train_idx, epochs=30, lr=1e-3):
    labs = sorted({intents[i] for i in train_idx})
    X = np.stack([MB_MEM[H.case_key(cases[i].text)] for i in train_idx])
    y = np.array([labs.index(intents[i]) for i in train_idx])
    counts = np.bincount(y, minlength=len(labs))
    w = torch.tensor(len(y) / (len(labs) * np.maximum(counts, 1)), dtype=torch.float32)
    lossf = torch.nn.CrossEntropyLoss(weight=w)
    head = torch.nn.Linear(hidden, len(labs))
    opt = torch.optim.AdamW(head.parameters(), lr=lr, weight_decay=1e-2)
    Xt = torch.tensor(X)
    yt = torch.tensor(y, dtype=torch.long)
    head.train()
    for ep in range(epochs):
        perm = torch.randperm(len(Xt))
        for i in range(0, len(perm), 16):
            idx = perm[i:i + 16]
            opt.zero_grad()
            lossf(head(Xt[idx]), yt[idx]).backward()
            opt.step()
    head.eval()
    with torch.no_grad():
        P = torch.softmax(head(Xt), dim=-1).numpy()
    return head, labs, P, y


results = []
for q in (0.30, 0.40, 0.50):
    total = routed = correct = 0
    s0w = s05_add = s05_ok = s05_w = 0
    for f in range(5):
        test_m = fold_of == f
        train_idx = np.where((~test_m) & (~is_ood))[0]
        labs = sorted({intents[i] for i in train_idx})
        head, _, Ptr, ytr = train_probe(train_idx)
        # quantile calibration on train-side max-softmax
        tau = float(np.quantile(Ptr.max(axis=1), 1.0 - q))
        # stage-0 centroids for this fold
        C = np.stack([QV[[i for i in train_idx if intents[i] == l]].mean(axis=0)
                      for l in labs])
        C /= (np.linalg.norm(C, axis=1, keepdims=True) + 1e-12)
        for qi in np.where(test_m)[0]:
            if is_ood[qi]:
                continue
            total += 1
            true = intents[qi]
            sims = C @ QV[qi]
            order = np.argsort(-sims)
            s0 = None
            if sims[order[0]] >= 0.60 and (sims[order[0]] - sims[order[1]]) >= 0.030:
                s0 = labs[order[0]]
            if s0 is not None:
                routed += 1
                ok = s0 == true
                correct += ok
                s0w += not ok
                continue
            with torch.no_grad():
                p = torch.softmax(head(torch.tensor(
                    MB_MEM[H.case_key(cases[qi].text)][None])), dim=-1)[0]
            if float(p.max()) >= tau:
                lab = labs[int(p.argmax())]
                routed += 1
                ok = lab == true
                correct += ok
                s05_add += 1
                s05_ok += ok
                s05_w += not ok
    C_ = routed / total
    P_ = correct / routed if routed else 0
    E2E_ = (correct + H.CHAIN_BASELINE * (total - routed)) / total
    print(f"q={q:.2f} tau~{tau:.3f} C={C_:6.1%} P={P_:6.1%} wrong={s0w + s05_w:2d} "
          f"E2E={E2E_:6.2%} | s05 added={s05_add} ok={s05_ok} bad={s05_w} | s0 wrong={s0w}")
    results.append({"q": q, "tau": round(tau, 3), "C": C_, "P": P_,
                    "wrong": s0w + s05_w, "E2E": E2E_,
                    "s05_added": s05_add, "s05_ok": s05_ok, "s05_wrong": s05_w,
                    "s0_wrong": s0w})

stamp = H.time.strftime("%Y%m%d-%H%M%S")
out = H.RESULTS / "iter-15"
out.mkdir(parents=True, exist_ok=True)
(out / f"summary-{stamp}.json").write_text(H.json.dumps(results, indent=1))
print(f"summary -> {out}/summary-{stamp}.json", file=sys.stderr)
