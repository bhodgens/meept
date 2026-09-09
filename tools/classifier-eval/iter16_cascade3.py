#!/usr/bin/env python3
"""iter-16: THREE-stage cascade (user direction).

Stage A (cheapest): qwen3 embedding + centroid margin gate — microseconds.
Stage B (middle): ModernBERT-base linear probe (SetFit-style: frozen
  embedding model + trained linear head, exactly the SetFit recipe with
  our probe as the head) — milliseconds on CPU.
Stage C (most expensive): the production LLM chain (lfm-8b-mlx, measured
  86.8% on this corpus) — seconds. NOT simulated: routed-to-chain cases
  score 0.868 per the campaign's E2E convention.

Aggregate objective: total system accuracy, not any single stage's score.
Cost accounting: each stage fires only on what cheaper stages abstained.

Also measured: pairwise stage-B precision by confidence band, to place
the B-exit tau where the CASCADE's marginal E2E is maximized.
"""
import sys
sys.path.insert(0, "/Users/caimlas/git/meept/tools/classifier-eval")
import numpy as np
import torch
import eval_harness as H

CHAIN_ACC = H.CHAIN_BASELINE  # 0.868 measured for lfm-8b on this corpus

MODEL = "/Volumes/LLMs/answerdotai/ModernBERT-base"
from transformers import AutoModel, AutoTokenizer

torch.manual_seed(42)
dev = "cpu"
tok = AutoTokenizer.from_pretrained(MODEL)
base = AutoModel.from_pretrained(MODEL)
base.to(dev).eval()
hidden = base.config.hidden_size


def mb_embed(texts, batch=16):
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
keys = [H.case_key(c.text) for c in cases]
print(f"corpus {len(cases)}; ModernBERT CPU embedding once...", file=sys.stderr)
MB_MEM = dict(zip(keys, mb_embed([c.text for c in cases])))
intents = [c.intent for c in cases]
is_ood = np.array([c.ood for c in cases])
fold_of = np.array([folds[k] for k in keys])

QS = {"url": "http://127.0.0.1:8090/v1", "model": "qwen3-embedding", "instruction": ""}
qemb = H.Embedder(QS["url"], QS["model"], "")
qkeys = [H.case_key(t) for t in (c.text for c in cases)]
qemb.embed_keys([c.text for c in cases], qkeys)
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
    Xt, yt = torch.tensor(X), torch.tensor(y, dtype=torch.long)
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
        Ptr = torch.softmax(head(Xt), dim=-1).numpy()
    return head, labs, Ptr, y


def cascade(q_b, margin_gate):
    """3-stage: A centroid; B probe (rescue only if A margin in gate band);
    C chain (scored at 0.868). Returns pooled metrics."""
    total = routed = correct = 0
    a_n = a_ok = b_n = b_ok = c_n = 0
    for f in range(5):
        test_m = fold_of == f
        train_idx = np.where((~test_m) & (~is_ood))[0]
        labs = sorted({intents[i] for i in train_idx})
        head, _, Ptr, ytr = train_probe(train_idx)
        tau = float(np.quantile(Ptr.max(axis=1), 1.0 - q_b))
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
            margin = float(sims[order[0]] - sims[order[1]])
            # Stage A
            if sims[order[0]] >= 0.60 and margin >= 0.030:
                routed += 1
                ok = labs[order[0]] == true
                correct += ok
                a_n += 1
                a_ok += ok
                continue
            # Stage B fires only when A was genuinely unsure (margin band)
            # or always (margin_gate=False)
            if margin_gate and margin >= 0.030:
                pass  # A was confident-but-below-floor? not possible here; fall to B
            # Stage B (probe)
            with torch.no_grad():
                p = torch.softmax(head(torch.tensor(
                    MB_MEM[H.case_key(cases[qi].text)][None])), dim=-1)[0]
            if float(p.max()) >= tau:
                lab = labs[int(p.argmax())]
                routed += 1
                ok = lab == true
                correct += ok
                b_n += 1
                b_ok += ok
                continue
            # Stage C: LLM chain — DETERMINISTIC expected credit at the
            # measured chain accuracy (campaign convention: eval_harness.py's
            # E2E formula credits every abstained case CHAIN_BASELINE).
            # CORRECTION 2026-09-08: the original run drew
            # ok = np.random.random() < CHAIN_ACC (seed 42) — 109 Bernoulli
            # draws realized 89.9% (~+1σ luck) instead of 86.8%, which alone
            # manufactured E2E 92.8% and a false "clears bar" verdict.
            # See results/iter-16-corrected/report.md.
            c_n += 1
            routed += 1  # chain always answers
            correct += CHAIN_ACC
    E2E_ = (correct + CHAIN_ACC * (total - routed)) / total
    return {"C": routed / total, "P": correct / routed if routed else 0,
            "wrong": routed - correct, "E2E": E2E_,
            "stageA": (a_n, a_ok), "stageB": (b_n, b_ok), "stageC": c_n}


print("=== 3-stage cascade (deterministic expected credit): A centroid / B probe / C lfm-8b chain (0.868) ===", file=sys.stderr)
rows = []
for q_b in (0.30, 0.40, 0.50):
    r = cascade(q_b, margin_gate=False)
    a_n, a_ok = r["stageA"]
    b_n, b_ok = r["stageB"]
    print(f"q={q_b:.2f} C={r['C']:6.1%} P={r['P']:6.1%} wrong={r['wrong']:2d} E2E={r['E2E']:6.2%} | "
          f"A:{a_n}({a_ok}) B:{b_n}({b_ok}) C(chain):{r['stageC']}")
    rows.append({"q": q_b, **{k: v for k, v in r.items()}})

stamp = H.time.strftime("%Y%m%d-%H%M%S")
out = H.RESULTS / "iter-16"
out.mkdir(parents=True, exist_ok=True)
(out / f"summary-{stamp}.json").write_text(H.json.dumps(rows, indent=1))
print(f"summary -> {out}/summary-{stamp}.json", file=sys.stderr)
