#!/usr/bin/env python3
"""iter-14b: diagnose the trained head's confidence distribution.
The tau sweep added NOTHING (stage0.5 added=0 at all taus) — either the
head is max-softmax near 1.0 always (overconfident tiny-data fine-tune)
or something is broken. Measure the distribution directly."""
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


cases_b, cases_a = H.load_cases()
cases = cases_b + cases_a
folds = H.assign_folds(cases)
keys = [H.case_key(c.text) for c in cases]
MB_MEM = dict(zip(keys, embed([c.text for c in cases])))
intents = [c.intent for c in cases]
is_ood = np.array([c.ood for c in cases])
fold_of = np.array([folds[k] for k in keys])
MBV = np.stack([MB_MEM[k] for k in keys])

f = 0
test_m = fold_of == f
train_m = ~test_m & ~is_ood
train_idx = np.where(train_m)[0]
labs = sorted({intents[i] for i in train_idx})

head = torch.nn.Linear(hidden, len(labs)).to(dev)
opt = torch.optim.AdamW(head.parameters(), lr=2e-5, weight_decay=0.01)
lossf = torch.nn.CrossEntropyLoss()
X = np.stack([MB_MEM[H.case_key(cases[i].text)] for i in train_idx])
y = np.array([labs.index(intents[i]) for i in train_idx])
Xt = torch.tensor(X, device=dev)
yt = torch.tensor(y, dtype=torch.long, device=dev)
print("training 3 epochs on", len(Xt), "examples,", len(labs), "classes", file=sys.stderr)
head.train()
base.train()
params = list(head.parameters()) + [p for n, p in base.named_parameters()
                                    if "layer.21." in n or "layer.20." in n]
for p in base.parameters():
    p.requires_grad_(False)
for n, p in base.named_parameters():
    if "layer.21." in n or "layer.20." in n:
        p.requires_grad_(True)
opt = torch.optim.AdamW(params, lr=2e-5, weight_decay=0.01)
for ep in range(3):
    perm = torch.randperm(len(Xt), generator=torch.Generator().manual_seed(42))
    tot = 0.0
    for i in range(0, len(perm), 16):
        idx = perm[i:i + 16]
        opt.zero_grad()
        loss = lossf(head(Xt[idx]), yt[idx])
        loss.backward()
        opt.step()
        tot += float(loss)
    print(f"  epoch {ep}: loss {tot / (len(perm) // 16 + 1):.4f}", file=sys.stderr)
base.eval()
head.eval()

# confidence distribution on TEST queries
confs = []
for qi in np.where(test_m)[0]:
    if is_ood[qi]:
        continue
    with torch.no_grad():
        p = torch.softmax(head(torch.tensor(MBV[qi][None], device=dev)), dim=-1)[0]
    confs.append((float(p.max()), labs[int(p.argmax())] == intents[qi]))
confs.sort(reverse=True)
print("top-10 conf:", [(round(c, 3), ok) for c, ok in confs[:10]])
print("bottom-5:", [(round(c, 3), ok) for c, ok in confs[-5:]])
import numpy as _np
acc = sum(ok for _, ok in confs) / len(confs)
print(f"forced accuracy fold0: {acc:.1%} over {len(confs)}")
for tau in (0.99, 0.95, 0.90):
    routed = [(c, ok) for c, ok in confs if c >= tau]
    if routed:
        prec = sum(ok for _, ok in routed) / len(routed)
        print(f"tau={tau}: adds {len(routed)} routes at P {prec:.1%}")
    else:
        print(f"tau={tau}: adds 0")
