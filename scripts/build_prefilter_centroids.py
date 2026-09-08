#!/usr/bin/env python3
"""Build classifier-prefilter centroids from the labeled eval corpus.

Embeds every corpus case through a running embed server
(scripts/embed_server.py), averages vectors per intent category, and
writes the centroid store the Go prefilter loads:

    ~/.meept/classifier_prefilter_centroids.json  (default output)

The store is self-describing (model, dimension, corpus, built_at) so the
Go side can validate dimension drift and the provenance is auditable.

Run (server must be up):
    python3 scripts/build_prefilter_centroids.py \
        --corpus testdata/eval/classifier-test-corpus.json5 \
        --url http://127.0.0.1:8090/v1

Sweep mode (accuracy vs threshold table, all cases embedded once):
    python3 scripts/build_prefilter_centroids.py --sweep
"""
import argparse
import json
import re
import sys
import time
import urllib.request
from collections import defaultdict
from datetime import datetime, timezone
from pathlib import Path

DEFAULT_CORPUS = "testdata/eval/classifier-test-corpus.json5"
DEFAULT_URL = "http://127.0.0.1:8090/v1"
DEFAULT_OUT = str(Path.home() / ".meept" / "classifier_prefilter_centroids.json")


def parse_json5(text: str) -> dict:
    """Minimal JSON5 subset parser for the corpus format: unquoted keys,
    single-quoted strings, trailing commas. Uses hujson-free stdlib trick:
    quote keys, single->double quotes, strip trailing commas."""
    # Quote unquoted object keys.
    text = re.sub(r"(?<![:\w\"'])([A-Za-z_][A-Za-z0-9_]*)\s*:", r'"\1":', text)
    # Single-quoted -> double-quoted strings (corpus has no escapes/quotes inside).
    text = text.replace("'", '"')
    # Trailing commas.
    text = re.sub(r",(\s*[}\]])", r"\1", text)
    return json.loads(text)


def embed_batch(url: str, texts: list[str], model: str) -> list[list[float]]:
    payload = json.dumps({"input": texts, "model": model}).encode()
    req = urllib.request.Request(
        url.rstrip("/") + "/embeddings",
        data=payload,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=120) as resp:
        out = json.loads(resp.read())
    vecs = [None] * len(texts)
    for item in out["data"]:
        vecs[item["index"]] = item["embedding"]
    if any(v is None for v in vecs):
        raise RuntimeError("server returned fewer embeddings than inputs")
    return vecs


def load_cases(corpus_path: str) -> list[tuple[str, str, str]]:
    """Returns (input, intent, agent) triples."""
    raw = Path(corpus_path).read_text()
    data = parse_json5(raw)
    cases = []
    for intent, entries in data["categories"].items():
        for e in entries:
            agent = e.get("expected_agent", "")
            cases.append((e["input"], e["expected_intent"], agent))
    return cases


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--corpus", default=DEFAULT_CORPUS)
    ap.add_argument("--url", default=DEFAULT_URL)
    ap.add_argument("--model", default="qwen3-embedding")
    ap.add_argument("--out", default=DEFAULT_OUT)
    ap.add_argument("--sweep", action="store_true",
                    help="report accuracy vs threshold and exit (no write)")
    args = ap.parse_args()

    cases = load_cases(args.corpus)
    print(f"corpus: {len(cases)} cases from {args.corpus}")

    # Embed in batches of 32 (keeps request sizes sane, still fast).
    vectors: list[list[float]] = []
    t0 = time.time()
    for i in range(0, len(cases), 32):
        chunk = [c[0] for c in cases[i:i + 32]]
        vectors.extend(embed_batch(args.url, chunk, args.model))
        print(f"  embedded {min(i + 32, len(cases))}/{len(cases)}")
    print(f"embedding took {time.time() - t0:.1f}s")

    dim = len(vectors[0])

    # Per-intent centroid = mean of member vectors, re-normalized.
    sums: dict[str, list[float]] = defaultdict(lambda: [0.0] * dim)
    counts: dict[str, int] = defaultdict(int)
    for (_, intent, _), vec in zip(cases, vectors):
        acc = sums[intent]
        for j, x in enumerate(vec):
            acc[j] += x
        counts[intent] += 1
    centroids = {}
    for intent, acc in sums.items():
        norm = sum(x * x for x in acc) ** 0.5
        centroids[intent] = [x / norm for x in acc] if norm else acc

    if args.sweep:
        # Leave-one-out style eval: each case scored against centroids
        # built from ALL cases (optimistic) — the honest number arrives
        # with the in-daemon A/B; this sweep only picks tau.
        print("\nthreshold sweep (self-inclusive centroids — upper bound):")
        best = (0.0, 0.0)
        for tau in [0.95, 0.92, 0.90, 0.88, 0.85, 0.82, 0.80, 0.75, 0.70]:
            correct = direct = 0
            for (text, intent, _), vec in zip(cases, vectors):
                sims = {
                    name: sum(a * b for a, b in zip(vec, cent))
                    for name, cent in centroids.items()
                }
                top = max(sims, key=sims.get)
                score = sims[top]
                if score >= tau:
                    direct += 1
                    correct += top == intent
            total = len(cases)
            if direct:
                acc = correct / direct
                print(f"  tau={tau:.2f}  direct={direct}/{total} ({direct / total:.0%})  "
                      f"accuracy_when_direct={correct}/{direct} ({acc:.1%})")
            else:
                print(f"  tau={tau:.2f}  direct=0/{total}")
            coverage_acc = correct / total  # accuracy contribution over ALL traffic
            if coverage_acc > best[1]:
                best = (tau, coverage_acc)
        print(f"\nrecommended tau (max overall correct-route coverage): {best[0]:.2f} "
              f"({best[1]:.1%} of all traffic correctly direct-routed)")
        return 0

    store = {
        "model": args.model,
        "dimension": dim,
        "built_at": datetime.now(timezone.utc).isoformat(),
        "corpus": str(Path(args.corpus).resolve()),
        "centroids": [
            {
                "intent": intent,
                "agent": next(a for t, i, a in cases if i == intent),
                "count": counts[intent],
                "vector": centroids[intent],
            }
            for intent in sorted(counts)
        ],
    }
    out = Path(args.out)
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(store))
    print(f"\nwrote {len(store['centroids'])} centroids (dim {dim}) -> {out}")
    print("daemon picks it up on next dispatcher Match (lazy load, no restart needed).")
    return 0


if __name__ == "__main__":
    sys.exit(main())
