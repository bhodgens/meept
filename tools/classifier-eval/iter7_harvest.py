#!/usr/bin/env python3
"""iter-7 pre-check: dedup guard for new adversarial cases (cosine > 0.95
to any existing case = reject, per master.md)."""
import sys
sys.path.insert(0, "/Users/caimlas/git/meept/tools/classifier-eval")
import numpy as np
import eval_harness as H

NEW = [
    # boundary cluster: code vs debug (hotfix family)
    ("ship a patch for the out-of-bounds read", "code"),
    ("write a fix for the segfault in the parser", "code"),
    ("patch the buffer overflow in the reader", "code"),
    ("add a guard clause for empty payloads", "code"),
    ("code a workaround for the library bug", "code"),
    # boundary cluster: search vs analyze (comparison family)
    ("find me a comparison of CI providers", "search"),
    ("look up benchmarks for embedded databases", "search"),
    ("gather pricing pages for observability vendors", "search"),
    ("pull together links about WebAssembly adoption", "search"),
    ("hunt down docs on the framesync API", "search"),
    ("research the state of server-side rendering frameworks", "analyze"),
    ("evaluate graph databases for our telemetry pipeline", "analyze"),
    ("assess whether Rust rewrite of the ingest path is justified", "analyze"),
    # chat micro-cluster: distinct real chat, short but semantic
    ("sounds good, talk tomorrow", "chat"),
    ("perfect, thanks a lot", "chat"),
    ("I will try that and report back", "chat"),
    # git micro-class: more anchors (only 10 in base)
    ("revert the last commit", "git"),
    ("open a draft PR for this branch", "git"),
    ("fetch and rebase onto origin main", "git"),
    ("stash my local changes", "git"),
    # schedule anchors (5 in base)
    ("set up a daily standup reminder at 9am", "schedule"),
    ("cancel my 3pm meeting", "schedule"),
    ("reschedule the retro to Thursday", "schedule"),
    # plan anchors (5 in base)
    ("plan the rollout of the new billing flow", "plan"),
    ("break down the auth refactor into phases", "plan"),
    ("draft a testing strategy for the release", "plan"),
    # platform anchors (5 in base)
    ("which models can you route to?", "platform"),
    ("how do I enable the prefilter?", "platform"),
    ("list the skills you have installed", "platform"),
]

cases_b, cases_a = H.load_cases()
cases = cases_b + cases_a
spec = {"name": "dedup", "embedder": {"url": "http://127.0.0.1:8090/v1",
                                       "model": "qwen3-embedding", "instruction": ""},
        "head": {"type": "knn_unanimity", "k": 5, "floor": 0.70}}
emb = H.Embedder(spec["embedder"]["url"], spec["embedder"]["model"], "")
texts = [c.text for c in cases]
keys = [H.case_key(t) for t in texts]
emb.embed_keys(texts, keys)
V = emb.vectors(keys)

new_texts = [t for t, _ in NEW]
new_keys = [H.case_key(t) for t in new_texts]
emb.embed_keys(new_texts, new_keys)

accepted, rejected = [], []
for (t, intent), k in zip(NEW, new_keys):
    v = emb.mem[k]
    sims = V @ v
    mx = float(sims.max())
    if mx > 0.95:
        rejected.append((t, intent, mx, cases[int(sims.argmax())].text[:45]))
    else:
        accepted.append((t, intent, mx))

print(f"accepted {len(accepted)}, rejected {len(rejected)}")
for t, i, mx in accepted:
    print(f"  ACCEPT [{i:8}] max_cos={mx:.3f} {t}")
for t, i, mx, near in rejected:
    print(f"  REJECT [{i:8}] max_cos={mx:.3f} {t}  ~= {near}")
