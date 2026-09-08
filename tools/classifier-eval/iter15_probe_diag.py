#!/usr/bin/env python3
"""iter-15 diagnostic: CPU linear probe over frozen ModernBERT features.

Hypothesis under test: iter-14's flat loss was head-lr underdose (2e-5 on a
fresh head), not feature poverty. A linear probe at head-appropriate lr
should learn if features carry intent signal."""
import sys
sys.path.insert(0, "/Users/caimlas/git/meept/tools/classifier-eval")
import numpy as np
import torch
import eval_harness as H

MODEL = "/Volumes/LLMs/answerdotai/ModernBERT-base"
from transformers import AutoModel, AutoTokenizer

torch.manual_seed(42)
dev = "cpu"  # research-driven: avoid MPS training kernels entirely
tok = AutoTokenizer.from_pretrained(MODEL)
base = AutoModel.from_pretrained(MODEL)
base.to(dev).eval()
hidden = base.config.hidden_size
print("device:", dev, file=sys.stderr)


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
print(f"corpus {len(cases)}; CPU embedding once...", file=sys.stderr)
keys = [H.case_key(c.text) for c in cases]
MB_MEM = dict(zip(keys, embed([c.text for c in cases])))
intents = [c.intent for c in cases]
is_ood = np.array([c.ood for c in cases])
fold_of = np.array([folds[k] for k in keys])

f = 0
test_m = fold_of == f
train_idx = np.where((~test_m) & (~is_ood))[0]
test_idx = np.where(test_m & (~is_ood))[0]
labs = sorted({intents[i] for i in train_idx})
X = np.stack([MB_MEM[H.case_key(cases[i].text)] for i in train_idx])
y = np.array([labs.index(intents[i]) for i in train_idx])
Xt = torch.tensor(X)
yt = torch.tensor(y, dtype=torch.long)

# class-balanced weights (small classes)
counts = np.bincount(y, minlength=len(labs))
w = torch.tensor(len(y) / (len(labs) * np.maximum(counts, 1)), dtype=torch.float32)
lossf = torch.nn.CrossEntropyLoss(weight=w)

head = torch.nn.Linear(hidden, len(labs))
# THE FIX: head-appropriate lr (iter-14 used 2e-5 for everything = 50x underdose)
opt = torch.optim.AdamW(head.parameters(), lr=1e-3, weight_decay=1e-2)
print(f"training linear probe: {len(X)} examples, {len(labs)} classes, lr 1e-3", file=sys.stderr)
head.train()
for ep in range(20):
    perm = torch.randperm(len(Xt))
    tot = 0.0
    for i in range(0, len(perm), 16):
        idx = perm[i:i + 16]
        opt.zero_grad()
        loss = lossf(head(Xt[idx]), yt[idx])
        loss.backward()
        opt.step()
        tot += float(loss)
    if ep in (0, 4, 9, 19):
        print(f"  epoch {ep}: loss {tot / (len(perm) // 16 + 1):.4f}", file=sys.stderr)
head.eval()

# test-side confidence distribution
confs = []
for qi in test_idx:
    with torch.no_grad():
        p = torch.softmax(head(torch.tensor(MB_MEM[H.case_key(cases[qi].text)][None])), dim=-1)[0]
    confs.append((float(p.max()), labs[int(p.argmax())] == intents[qi]))
confs.sort(reverse=True)
acc = sum(ok for _, ok in confs) / len(confs)
print(f"\nforced accuracy fold0: {acc:.1%} over {len(confs)} (chance = {1/len(labs):.1%})")
print("top-8 conf:", [(round(c, 3), ok) for c, ok in confs[:8]])
print("bottom-3:", [(round(c, 3), ok) for c, ok in confs[-3:]])
for tau in (0.5, 0.4, 0.3, 0.25, 0.2):
    routed = [(c, ok) for c, ok in confs if c >= tau]
    if routed:
        prec = sum(ok for _, ok in routed) / len(routed)
        print(f"tau={tau}: {len(routed)} routes at P {prec:.1%}")
    else:
        print(f"tau={tau}: 0 routes")
