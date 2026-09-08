#!/usr/bin/env python3
"""iter-14: fine-tuned ModernBERT Stage-0.5 head.

Two-stage experiment per master.md: Stage-0 (qwen3 centroid margin)
abstains -> Stage-0.5 (ModernBERT-base FINE-TUNED with a classification
head) routes only when its calibrated confidence >= tau.

Trained per fold on the fold's train slice (no leakage), 3 epochs, MPS,
head = Linear(768 -> n_intents). Calibrated abstain via max-softmax tau.
"""
import sys
sys.path.insert(0, "/Users/caimlas/git/meept/tools/classifier-eval")
import numpy as np
import torch
import eval_harness as H

MODEL = "/Volumes/LLMs/answerdotai/ModernBERT-base"
from transformers import AutoModel, AutoTokenizer

tok = AutoTokenizer.from_pretrained(MODEL)
base = AutoModel.from_pretrained(MODEL)
dev = "mps" if torch.backends.mps.is_available() else "cpu"
base.to(dev)
hidden = base.config.hidden_size
print("device:", dev, file=sys.stderr)


def embed(texts, batch=16):
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


class FineTunedHead:
    """ModernBERT backbone + linear head, trained. Abstains below tau."""

    def __init__(self, tau: float, epochs: int = 3, lr: float = 2e-5):
        self.tau, self.epochs, self.lr = tau, epochs, lr
        self.torch = torch

    def fit(self, train_idx, train_intents):
        t = self.torch
        self.labs = sorted(set(train_intents))
        # build fresh head per fold
        self.head = t.nn.Linear(hidden, len(self.labs)).to(dev)
        opt = t.optim.AdamW(self.head.parameters(), lr=self.lr, weight_decay=0.01)
        lossf = t.nn.CrossEntropyLoss()
        X = np.stack([globals()["_MB_MEM"][H.case_key(cases[i].text)] for i in train_idx])
        y = np.array([self.labs.index(l) for l in train_intents])
        Xt = t.tensor(X, device=dev)
        yt = t.tensor(y, dtype=t.long, device=dev)
        # backbone fine-tune too: unfreeze last 2 layers only (memory-friendly)
        params = list(self.head.parameters()) + [
            p for n, p in base.named_parameters() if "layer.21." in n or "layer.20." in n]
        for p in base.parameters():
            p.requires_grad_(False)
        for n, p in base.named_parameters():
            if "layer.21." in n or "layer.20." in n:
                p.requires_grad_(True)
        opt = t.optim.AdamW(params, lr=self.lr, weight_decay=0.01)
        self.head.train()
        base.train()
        for ep in range(self.epochs):
            perm = t.randperm(len(Xt), generator=t.Generator().manual_seed(42))
            for i in range(0, len(perm), 16):
                idx = perm[i:i + 16]
                opt.zero_grad()
                logits = self.head(Xt[idx])
                loss = lossf(logits, yt[idx])
                loss.backward()
                opt.step()
        base.eval()
        self.head.eval()

    def decide_from_vec(self, vec):
        t = self.torch
        with t.no_grad():
            logits = self.head(t.tensor(vec[None], device=dev))
            p = t.softmax(logits, dim=-1)[0]
        top = float(p.max())
        if top < self.tau:
            return None, top
        return self.labs[int(p.argmax())], top


# ---- build corpus + stage-0 ----
cases_b, cases_a = H.load_cases()
cases = cases_b + cases_a
folds = H.assign_folds(cases)
print("embedding corpus with ModernBERT (for stage-0.5)...", file=sys.stderr)
keys = [H.case_key(c.text) for c in cases]
globals()["_MB_MEM"] = dict(zip(keys, embed([c.text for c in cases])))
intents = [c.intent for c in cases]
is_ood = np.array([c.ood for c in cases])
key_arr = np.array(keys)
fold_of = np.array([folds[k] for k in keys])
MBV = np.stack([globals()["_MB_MEM"][k] for k in keys])

# stage-0: qwen3 centroid m0.030
QS = {"url": "http://127.0.0.1:8090/v1", "model": "qwen3-embedding", "instruction": ""}
qemb = H.Embedder(QS["url"], QS["model"], "")
QV = None

rows = []
for tau in (0.90, 0.80, 0.70):
    stage0_wrong = stage05_added = stage05_wrong = stage05_correct = 0
    total = routed = correct = 0
    for f in range(5):
        test_m = fold_of == f
        train_m = ~test_m & ~is_ood
        train_idx = np.where(train_m)[0]
        labs = sorted({intents[i] for i in train_idx})
        # stage-0 centroids (qwen3)
        qtexts = [c.text for c in cases]
        qkeys = [H.case_key(t) for t in qtexts]
        qemb.embed_keys(qtexts, qkeys)
        QVf = qemb.vectors(qkeys)
        C = np.stack([QVf[[i for i in train_idx if intents[i] == l]].mean(axis=0)
                      for l in labs])
        C /= (np.linalg.norm(C, axis=1, keepdims=True) + 1e-12)
        # stage-0.5 fit (fresh per fold, per tau — fit once per fold actually)
        head = FineTunedHead(tau=tau)
        head.fit(train_idx, [intents[i] for i in train_idx])
        for qi in np.where(test_m)[0]:
            if is_ood[qi]:
                continue
            total += 1
            true = intents[qi]
            sims = C @ QVf[qi]
            order = np.argsort(-sims)
            s0_route = None
            if sims[order[0]] >= 0.60 and (sims[order[0]] - sims[order[1]]) >= 0.030:
                s0_route = labs[order[0]]
            if s0_route is not None:
                routed += 1
                ok = s0_route == true
                correct += ok
                if not ok:
                    stage0_wrong += 1
                continue
            # stage-0.5 rescue
            lab, conf = head.decide_from_vec(MBV[qi])
            if lab is not None:
                routed += 1
                ok = lab == true
                correct += ok
                stage05_added += 1
                stage05_correct += ok
                if not ok:
                    stage05_wrong += 1
    C_ = routed / total
    P_ = correct / routed if routed else 0
    E2E_ = (correct + H.CHAIN_BASELINE * (total - routed)) / total
    print(f"tau={tau:.2f} C={C_:6.1%} P={P_:6.1%} wrong={total-correct if False else (stage0_wrong + stage05_wrong):2d} "
          f"E2E={E2E_:6.2%} | stage0.5 added={stage05_added} correct={stage05_correct} wrong={stage05_wrong} "
          f"| stage0 wrong={stage0_wrong}")
    rows.append({"tau": tau, "C": C_, "P": P_, "wrong": stage0_wrong + stage05_wrong,
                 "E2E": E2E_, "s05_added": stage05_added, "s05_correct": stage05_correct,
                 "s05_wrong": stage05_wrong, "s0_wrong": stage0_wrong})

stamp = H.time.strftime("%Y%m%d-%H%M%S")
out = H.RESULTS / "iter-14"
out.mkdir(parents=True, exist_ok=True)
(out / f"summary-{stamp}.json").write_text(H.json.dumps(rows, indent=1))
print(f"summary -> {out}/summary-{stamp}.json", file=sys.stderr)
