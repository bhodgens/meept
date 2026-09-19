#!/usr/bin/env python3
"""iter-18: does the wave-4 expansion move stage-A on the EXACT misses?
Replays the known silver misses through the centroid gate trained on
gold (including the iter-18 wave-4 cases).

Privacy note (routing-repair leaf 01): the raw replay excerpts that this
script once hardcoded are private local-corpus text. They are identified
here by ``eval_harness.case_key`` only; the verbatim text lives solely in
the untracked ``replay-gold.local.json5`` corpus, and this module
re-reads the exact strings from that corpus by their case keys at run
time. The historical findings these replays produced are recorded in the
committed canonical evidence chain:

  - results/iter-18/report.md        (post-wave-4 miss analysis 1-3)
  - results/m4-silver/cascade-validation-correction.md
                                     (pre/post-edit miss table of record)
"""
import sys
sys.path.insert(0, "/Users/caimlas/git/meept/tools/classifier-eval")
import json
import re
import numpy as np
import eval_harness as H
from pathlib import Path

# Silver misses from the M4 validation, identified by case key.
# Resolved at run time against replay-gold.local.json5 (the verbatim
# strings are never embedded in this file).
MISS_KEYS = (
    ("2c0434047ecad05e", "code", "quickplan orchestration phrasing "
     "(adjudication rule 3: execution + subagents)"),
    ("338cb3b2e9ffa8c4", "code", "bare execution verb; silver label "
     "itself contestable plan-vs-code (iter-18 report, miss 1)"),
    ("5cc43bded7c46087", "code", "document/artifact production case "
     "(truncated excerpt appears in committed m4-silver JSON misses)"),
)

def load_miss_texts():
    """Resolve the miss case keys to corpus texts, without embedding the
    private excerpts in source. Returns {case_key: text}."""
    rp = Path("/Users/caimlas/git/meept/tools/classifier-eval/replay-gold.local.json5")
    silver = json.loads("[" + ",".join(
        re.findall(r'\{\s*"input".*?\}', rp.read_text(), re.S)) + "]")
    by_key = {H.case_key(c["input"]): c["input"] for c in silver}
    return {k: by_key[k] for k, _, _ in MISS_KEYS if k in by_key}


def main():
    cases_b, cases_a = H.load_cases()
    cases = cases_b + cases_a
    folds = H.assign_folds(cases)
    emb = H.Embedder("http://127.0.0.1:8090/v1", "qwen3-embedding", "")
    texts = [c.text for c in cases]
    keys = [H.case_key(t) for t in texts]
    emb.embed_keys(texts, keys)
    V = emb.vectors(keys)
    intents = [c.intent for c in cases]
    is_ood = np.array([c.ood for c in cases])
    fold_of = np.array([folds[k] for k in keys])

    miss_texts = load_miss_texts()

    # stage-A centroids trained on ALL non-OOD gold (deployment shape)
    train_m = ~is_ood
    train_idx = np.where(train_m)[0]
    labs = sorted({intents[i] for i in train_idx})
    C = np.stack([V[[i for i in train_idx if intents[i] == l]].mean(axis=0) for l in labs])
    C /= (np.linalg.norm(C, axis=1, keepdims=True) + 1e-12)

    print("stage-A replay of silver misses (post-wave-4):")
    for k, true, note in MISS_KEYS:
        if k not in miss_texts:
            print(f"  SKIP [{true}] case={k} not in local corpus ({note})")
            continue
        t = miss_texts[k]
        kk = H.case_key(t)
        if kk not in emb.mem:
            emb.embed_keys([t], [kk])
        qv = emb.mem[kk]
        sims = C @ qv
        order = np.argsort(-sims)
        margin = float(sims[order[0]] - sims[order[1]])
        routed = sims[order[0]] >= 0.60 and margin >= 0.030
        verdict = f"ROUTE {labs[order[0]]}" if routed else "abstain (falls to B/C)"
        ok = routed and labs[order[0]] == true
        print(f"  {'OK ' if ok else ('WRONG' if routed else 'PASS')} [{true}] -> {verdict} "
              f"(top1={labs[order[0]]} sim={sims[order[0]]:.3f} margin={margin:.3f}) "
              f"case={k} :: {note}")
    print("\nhistorical analysis of record: results/iter-18/report.md;")
    print("pre/post-edit miss table: results/m4-silver/cascade-validation-correction.md")


if __name__ == "__main__":
    main()
