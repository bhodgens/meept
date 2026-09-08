#!/usr/bin/env python3
"""iter-10 harvest wave 2: dedup check for near-miss-cluster cases.
Targets: code-vs-debug verb families, review anchors, search/analyze split,
more plan/report/recall/platform mass."""
import sys
sys.path.insert(0, "/Users/caimlas/git/meept/tools/classifier-eval")
import numpy as np
import eval_harness as H

NEW = [
    # code: more non-fix-verb anchors (pull the code centroid up)
    ("write a logging middleware for gin", "code"),
    ("implement pagination for the products list", "code"),
    ("create a config loader with env fallback", "code"),
    ("scaffold a REST controller for invoices", "code"),
    ("write a unit test for the parser module", "code"),
    ("add a decorator that retries on failure", "code"),
    ("build a CLI flag parser with subcommands", "code"),
    ("generate types from the OpenAPI spec", "code"),
    # debug: error-shaped anchors (distinct from code)
    ("the build fails with undefined symbols", "debug"),
    ("getting a null pointer when saving", "debug"),
    ("the worker crashes after ten minutes", "debug"),
    ("requests hang until timeout in prod", "debug"),
    ("the import raises ModuleNotFoundError", "debug"),
    ("why does the pagination return duplicates?", "debug"),
    # review: dedicated anchors (4 in base, top1-confused with debug)
    ("review my pull request before merge", "review"),
    ("critique this function for readability", "review"),
    ("check the migration script for pitfalls", "review"),
    ("give feedback on my architecture draft", "review"),
    ("audit the error handling in this module", "review"),
    # search: retrieval verbs (vs analyze)
    ("find the RFC for HTTP signatures", "search"),
    ("search for postmortems about cache stampedes", "search"),
    ("locate the docs for the kubelet config", "search"),
    ("look up the CVE for that openssl version", "search"),
    # analyze: judgment verbs (vs search)
    ("weigh the tradeoffs of event sourcing", "analyze"),
    ("compare monorepo and polyrepo strategies", "analyze"),
    ("interpret these latency percentiles for me", "analyze"),
    ("assess the security implications of the design", "analyze"),
    # plan: more anchors (5-6 in base)
    ("plan the zero-downtime migration", "plan"),
    ("outline the phases of the SDK rewrite", "plan"),
    ("devise a rollout strategy for the feature flag", "plan"),
    # report: more anchors
    ("write a weekly engineering update", "report"),
    ("compile the incident timeline into a summary", "report"),
    ("produce a changelog for the release", "report"),
    # recall: more anchors
    ("what did we decide about the queue library?", "recall"),
    ("dig up the reasoning from last sprint", "recall"),
    ("find where we discussed the schema change", "recall"),
    # platform: more anchors
    ("what tools do you have for database work?", "platform"),
    ("show your available skills", "platform"),
    ("how do I connect you to my repo?", "platform"),
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
    sims = V @ emb.mem[k]
    mx = float(sims.max())
    if mx > 0.95:
        rejected.append((t, intent, mx))
    else:
        accepted.append((t, intent, mx))
print(f"accepted {len(accepted)} rejected {len(rejected)}")
for t, i, mx in rejected:
    print(f"  REJECT [{i:8}] {mx:.3f} {t}")
for t, i, mx in accepted:
    print(f"  ok [{i:8}] {mx:.3f} {t}")
