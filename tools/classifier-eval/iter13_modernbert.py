#!/usr/bin/env python3
"""iter-13: ModernBERT-base raw embeddings as an ALTERNATE Stage-0 embedder.

Loads answerdotai/ModernBERT-base via transformers (CPU if MLX path
unavailable — master.md risk mitigation), mean-pools last hidden state,
L2-normalizes, embeds the full corpus into a SEPARATE cache tag, and
scores the champion head (centroid margin brackets).
"""
import sys
sys.path.insert(0, "/Users/caimlas/git/meept/tools/classifier-eval")
import numpy as np
import eval_harness as H

MODEL = "/Volumes/LLMs/answerdotai/ModernBERT-base"

from transformers import AutoModel, AutoTokenizer
import torch

print("loading", MODEL, file=sys.stderr)
tok = AutoTokenizer.from_pretrained(MODEL)
model = AutoModel.from_pretrained(MODEL)
model.eval()
print("loaded;", "cuda" if torch.cuda.is_available() else "cpu/mps", file=sys.stderr)


def embed_texts(texts, batch=16):
    out = []
    dev = "mps" if torch.backends.mps.is_available() else "cpu"
    model.to(dev)
    with torch.no_grad():
        for i in range(0, len(texts), batch):
            chunk = texts[i:i + batch]
            enc = tok(chunk, padding=True, truncation=True, max_length=256,
                      return_tensors="pt").to(dev)
            o = model(**enc)
            h = o.last_hidden_state  # (B, T, D)
            mask = enc["attention_mask"].unsqueeze(-1).float()
            vec = (h * mask).sum(1) / mask.sum(1).clamp(min=1e-9)  # mean pooling
            vec = torch.nn.functional.normalize(vec, dim=-1)
            out.extend(v.cpu().numpy().astype(np.float32) for v in vec)
            if (i // batch) % 10 == 0:
                print(f"  {i + len(chunk)}/{len(texts)}", file=sys.stderr)
    return out


cases_b, cases_a = H.load_cases()
cases = cases_b + cases_a
folds = H.assign_folds(cases)
print(f"corpus {len(cases)}; embedding with ModernBERT-base...", file=sys.stderr)

# Bypass the HTTP embedder: build vectors directly, then reuse the fold/eval path
# by injecting into a shim Embedder.
vectors = embed_texts([c.text for c in cases])
keys = [H.case_key(c.text) for c in cases]

class ShimEmbedder:
    def __init__(self, mem):
        self.mem = mem
        self.latency_ms = []
    def embed_keys(self, texts, keys):
        pass  # everything pre-embedded
    def vectors(self, keys_):
        return np.stack([self.mem[k] for k in keys_])

emb = ShimEmbedder(dict(zip(keys, vectors)))

MB = {"url": "modernbert-local", "model": "modernbert-base", "instruction": ""}
rows = []
for margin in (0.025, 0.030, 0.035):
    spec = {"name": f"iter13-modernbert-centroid-m{margin:.3f}", "embedder": MB,
            "head": {"type": "centroid", "floor": 0.60, "margin": margin}}
    m = H.run_permutation(spec, cases, folds, emb)
    rows.append(m)
    print(H.fmt_row(m))
    for (c, pred, score) in m["confusion"]:
        if pred != c.intent:
            print(f"   wrong [{c.case_id}] true={c.intent} pred={pred} "
                  f"score={score:.3f} :: {c.text[:55]}")

# comparator: qwen3 m0.030 champion on same corpus
QS = {"url": "http://127.0.0.1:8090/v1", "model": "qwen3-embedding", "instruction": ""}
qemb = H.Embedder(QS["url"], QS["model"], "")
spec = {"name": "iter13-qwen3-champion-m0.030", "embedder": QS,
        "head": {"type": "centroid", "floor": 0.60, "margin": 0.030}}
m = H.run_permutation(spec, cases, folds, qemb)
rows.append(m)
print(H.fmt_row(m))

stamp = H.time.strftime("%Y%m%d-%H%M%S")
out = H.RESULTS / "iter-13"
out.mkdir(parents=True, exist_ok=True)
(out / f"summary-{stamp}.json").write_text(H.json.dumps(
    [{k: v for k, v in m.items() if k not in ("confusion", "margin_cases")} for m in rows],
    indent=1))
print(f"summary -> {out}/summary-{stamp}.json", file=sys.stderr)
