#!/usr/bin/env python3
"""Build the embed-health reference set for meept (issue #42).

Reads the prefilter kNN store (~/.meept/classifier_prefilter_centroids.json,
written by scripts/build_prefilter_centroids.py) and writes the reference
JSON consumed by internal/agent/embed_health.go:
a bare 2-D float array [[float, ...], ...], one known-good embedding per row.

Go-side calibration (95th percentile of cross-fitted nearest-neighbour
cosine distances) is deterministic from these vectors, so the tool only
extracts and validates them.

Usage:
  python3 tools/build_embed_health_ref.py [--store PATH] [--out PATH]
                                          [--max-rows N] [--seed S]

Defaults: store = ~/.meept/classifier_prefilter_centroids.json,
out = ~/.meept/embed_health_reference.json. Deterministic subsample via
--seed when --max-rows is set (default: keep every row).
"""

from __future__ import annotations

import argparse
import json
import math
import random
import sys
from pathlib import Path

DEFAULT_STORE = Path.home() / ".meept" / "classifier_prefilter_centroids.json"
DEFAULT_OUT = Path.home() / ".meept" / "embed_health_reference.json"


def load_vectors(store_path: Path) -> list[list[float]]:
    data = json.loads(store_path.read_text())
    rows = data.get("examples") or data.get("centroids") or []
    vecs = [r["vector"] for r in rows if r.get("vector")]
    if not vecs:
        raise SystemExit(f"no vectors in {store_path} (need examples[] or centroids[])")
    dim = len(vecs[0])
    for i, v in enumerate(vecs):
        if len(v) != dim:
            raise SystemExit(f"ragged store: row {i} has {len(v)} dims, want {dim}")
        if not all(math.isfinite(x) for x in v):
            raise SystemExit(f"non-finite value in row {i}")
    return vecs


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--store", type=Path, default=DEFAULT_STORE)
    ap.add_argument("--out", type=Path, default=DEFAULT_OUT)
    ap.add_argument("--max-rows", type=int, default=None,
                    help="deterministically subsample to at most N rows")
    ap.add_argument("--seed", type=int, default=0)
    args = ap.parse_args()

    vecs = load_vectors(args.store)
    if args.max_rows is not None and len(vecs) > args.max_rows:
        rng = random.Random(args.seed)
        vecs = sorted(rng.sample(vecs, args.max_rows), key=lambda v: tuple(v))

    args.out.write_text(json.dumps(vecs))
    print(f"wrote {args.out}: {len(vecs)} vectors x {len(vecs[0])} dims "
          f"(from {args.store})")
    return 0


if __name__ == "__main__":
    sys.exit(main())
