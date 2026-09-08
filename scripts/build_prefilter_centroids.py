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
    """Parser for the corpus format. The file is JSON5-flavored BUT also
    contains raw apostrophes inside double-quoted strings ("what's"),
    which is invalid in both JSON and JSON5. Regex normalization cannot
    handle that, so strings are extracted FIRST, placeholders inserted,
    and the remainder (unquoted keys, trailing commas) normalized before
    json.loads. Extracted strings are restored verbatim afterwards."""
    strings: list[str] = []

    def stash(m: re.Match) -> str:
        # m.group(0) is a double-quoted string possibly containing raw '.
        strings.append(m.group(0)[1:-1])
        return f"\x00{len(strings)-1}\x00"

    # Double-quoted strings: no escapes in the corpus, so [^"] suffices.
    text = re.sub(r'"[^"]*"', stash, text)
    # Quote unquoted object keys.
    text = re.sub(r"(?<![:\w\x00'\"])([A-Za-z_][A-Za-z0-9_]*)\s*:", r'"\1":', text)
    # Trailing commas.
    text = re.sub(r",(\s*[}\]])", r"\1", text)

    def unstash(m: re.Match) -> str:
        return json.dumps(strings[int(m.group(1))])

    text = re.sub(r"\x00(\d+)\x00", unstash, text)
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
    ap.add_argument("--instruction", default="",
                    help="Qwen3-Embedding task instruction prefixed to every "
                         "query for embedding (official instruction-aware "
                         "format; must be identical at serving time)")
    ap.add_argument("--sweep", action="store_true",
                    help="report kNN-unanimity accuracy vs threshold and "
                         "exit (no write)")
    args = ap.parse_args()

    cases = load_cases(args.corpus)
    print(f"corpus: {len(cases)} cases from {args.corpus}")

    # Embed in batches of 32 (keeps request sizes sane, still fast).
    vectors: list[list[float]] = []
    t0 = time.time()
    for i in range(0, len(cases), 32):
        chunk = [args.instruction + c[0] if args.instruction else c[0]
                 for c in cases[i:i + 32]]
        vectors.extend(embed_batch(args.url, chunk, args.model))
        print(f"  embedded {min(i + 32, len(cases))}/{len(cases)}")
    print(f"embedding took {time.time() - t0:.1f}s")

    dim = len(vectors[0])

    if args.sweep:
        # Leave-one-out kNN unanimity: each case's 5 nearest neighbors
        # EXCLUDING itself vote; unanimous agreement = direct route.
        # This is the honest proxy for the Go prefilter's behavior.
        import math
        norms = [math.sqrt(sum(x * x for x in v)) or 1.0 for v in vectors]
        unit = [[x / n for x in v] for v, n in zip(vectors, norms)]

        def cos(a, b):
            return sum(x * y for x, y in zip(a, b))

        intents_of = [c[1] for c in cases]
        K = 5
        print(f"\nkNN-unanimity sweep (k={K}, leave-one-out — held-out):")
        best = (0.0, 0.0, 0.0)
        for tau in [0.85, 0.80, 0.75, 0.70, 0.65, 0.60, 0.55, 0.50]:
            direct = correct = 0
            for j, ((text, intent, _), vec) in enumerate(zip(cases, unit)):
                sims = sorted(
                    ((cos(vec, unit[m]), intents_of[m]) for m in range(len(cases)) if m != j),
                    key=lambda t: -t[0],
                )
                top = sims[:K]
                if len(top) < K or any(s < tau for s, _ in top):
                    continue  # neighbor below floor: no vote
                winner = top[0][1]
                if any(i != winner for _, i in top):
                    continue  # dissenter: no vote
                direct += 1
                correct += winner == intent
            total = len(cases)
            acc = correct / direct if direct else 0.0
            print(f"  tau={tau:.2f}  direct={direct}/{total} ({direct / total:.0%})  "
                  f"accuracy_when_direct={correct}/{direct or 1} ({acc:.1%})")
            coverage = correct / total
            if direct and acc > best[0]:
                best = (acc, coverage, tau)
            elif direct and acc == best[0] and coverage > best[1]:
                best = (acc, coverage, tau)
        print(f"\nbest tau by precision: {best[2]:.2f} "
              f"(precision {best[0]:.1%}, correct-route coverage {best[1]:.1%})")
        return 0

    # kNN index: per-example vectors. The Go prefilter votes on the raw
    # examples (unanimous top-k), so no averaging happens anywhere —
    # multi-modal intents keep their distinct clusters.
    store = {
        "model": args.model,
        "dimension": dim,
        "built_at": datetime.now(timezone.utc).isoformat(),
        "corpus": str(Path(args.corpus).resolve()),
        "instruction": args.instruction,
        "k": 5,
        "examples": [
            {
                "intent": intent,
                "agent": agent,
                "text": text,
                "vector": vec,
            }
            for (text, intent, agent), vec in zip(cases, vectors)
        ],
    }
    out = Path(args.out)
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(store))
    print(f"\nwrote {len(store['examples'])} example vectors (dim {dim}) -> {out}")
    print("daemon picks it up on next dispatcher Match (lazy load, no restart needed).")
    return 0


if __name__ == "__main__":
    sys.exit(main())
