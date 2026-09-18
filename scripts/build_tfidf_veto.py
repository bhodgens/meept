#!/usr/bin/env python3
"""Build the Door-1 tfidf-veto model for the Go prefilter.

Trains a char n-gram TF-IDF + logistic classifier on the gold corpus
and serializes weights to JSON the Go prefilter loads alongside the
centroid store:

  internal/agent/testdata/prefilter_tfidf_veto.json  (in-repo default)

Schema (written by this script, consumed by
internal/agent/tfidf_veto.go):

{
  "built_at": ISO8601,
  "corpus": path,
  "ngram_range": [2, 4],
  "vocab": {"<char 2-4gram>": feature_index, ...},
  "idf": [float per feature],
  "classes": ["intent", ...],
  "coef": [[float per feature] per class],
  "intercept": [float per class],
  "threshold": float   // agreed-vote confidence floor (default 0.0:
                       // any agreement passes; logistic is well-calibrated
                       // enough that argmax agreement is the signal)
}

Usage:
  python3 scripts/build_tfidf_veto.py \
      --corpus testdata/eval/classifier-adversarial-corpus.json5 \
      --corpus testdata/eval/classifier-test-corpus.json5 \
      --out internal/agent/testdata/prefilter_tfidf_veto.json
"""
import argparse
import hashlib
import json
import math
import re
import sys
from collections import Counter
from datetime import datetime, timezone
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "tools" / "classifier-eval"))
import eval_harness as H  # reuse the JSON5 + case loading

NGRAM_LO, NGRAM_HI = 2, 4
CHAR_WB = True  # word-boundary padding like sklearn char_wb


def _file_sha256(path: str, chunk: int = 1 << 20) -> str:
    """Streaming sha256 of a source corpus file (content, not just the
    path label)."""
    h = hashlib.sha256()
    with open(path, "rb") as f:
        while True:
            b = f.read(chunk)
            if not b:
                break
            h.update(b)
    return h.hexdigest()


def _harness_mod():
    """Return the already-imported eval_harness module (imported at
    module load below for corpus loading reuse)."""
    return H


def char_ngrams(text):
    text = text.lower()
    if CHAR_WB:
        text = " " + text + " "
    out = []
    for n in range(NGRAM_LO, NGRAM_HI + 1):
        for i in range(len(text) - n + 1):
            out.append(text[i:i + n])
    return out


def build_vocab(docs, min_df=1):
    df = Counter()
    for d in docs:
        df.update(set(char_ngrams(d)))
    vocab = {}
    for gram, count in sorted(df.items()):
        if count >= min_df:
            vocab[gram] = len(vocab)
    return vocab


def tfidf_matrix(docs, vocab, idf):
    rows = []
    for d in docs:
        counts = Counter(g for g in char_ngrams(d) if g in vocab)
        vec = [0.0] * len(vocab)
        norm = 0.0
        for g, c in counts.items():
            j = vocab[g]
            tf = 1.0 + math.log(c)  # sublinear tf (sklearn sublinear_tf=True)
            w = tf * idf[j]
            vec[j] = w
            norm += w * w
        if norm > 0:
            norm = math.sqrt(norm)
            vec = [x / norm for x in vec]
        rows.append(vec)
    return rows


def build(argv=None):
    """Entry point used by the provenance tests: parses argv (defaults to
    sys.argv), trains, writes the model, returns an exit code. `main`
    delegates here so the provenance shape is exercised by the exact
    production code path (offline; synthetic corpora only in tests)."""
    ap = argparse.ArgumentParser()
    ap.add_argument("--corpus", action="append", required=True)
    ap.add_argument("--out", default="internal/agent/testdata/prefilter_tfidf_veto.json")
    ap.add_argument("--min-df", type=int, default=1)
    ap.add_argument("-v", "--verbose", action="store_true")
    args = ap.parse_args(argv)

    h = _harness_mod()
    selected: list = []   # (text, intent, corpus_path)
    for cpath in args.corpus:
        cb, ca = h.load_cases(h.BASE_CORPUS, Path(cpath))
        selected.extend((c.text, c.intent, cpath) for c in ca)

    # dedup identical texts (multi-corpus overlap)
    seen = set()
    docs, labels = [], []
    for text, intent, _ in selected:
        if not intent or intent == "OOD":
            continue
        k = hashlib.sha256(text.encode()).hexdigest()
        if k in seen:
            continue
        seen.add(k)
        docs.append(text)
        labels.append(intent)

    classes = sorted(set(labels))
    y = [classes.index(l) for l in labels]

    vocab = build_vocab(docs, args.min_df)
    N = len(docs)
    idf = [0.0] * len(vocab)
    df = Counter()
    for d in docs:
        df.update(set(g for g in char_ngrams(d) if g in vocab))
    for g, j in vocab.items():
        idf[j] = math.log((1 + N) / (1 + df[g])) + 1.0  # sklearn smooth idf

    X = tfidf_matrix(docs, vocab, idf)

    # One-vs-rest logistic via simple gradient descent (stdlib only).
    # Converges fine at this scale (few hundred docs, ~50-100k features
    # after min_df; L2 + low lr keeps it stable).
    nF = len(vocab)
    nC = len(classes)
    coef = [[0.0] * nF for _ in range(nC)]
    intercept = [0.0] * nC
    lr = 0.5
    epochs = 12
    for ep in range(epochs):
        for xi, yi in zip(X, y):
            # softmax
            scores = [intercept[c] + sum(a * b for a, b in zip(coef[c], xi) if b) for c in range(nC)]
            m = max(scores)
            exps = [math.exp(s - m) for s in scores]
            ssum = sum(exps)
            for c in range(nC):
                p = exps[c] / ssum
                g = p - (1.0 if c == yi else 0.0)
                if g == 0:
                    continue
                ic = intercept[c] - lr * g * 0.01
                intercept[c] = ic
                for j, xv in enumerate(xi):
                    if xv:
                        coef[c][j] -= lr * g * xv
        lr *= 0.85

    # train accuracy sanity
    correct = 0
    for xi, yi in zip(X, y):
        scores = [intercept[c] + sum(a * b for a, b in zip(coef[c], xi) if b) for c in range(nC)]
        if scores.index(max(scores)) == yi:
            correct += 1

    # PROVENANCE (leaf 03, MEAS-03): the metadata describes the ACTUAL
    # selected population (docs/labels above after OOD exclusion and
    # dedup), never a constant. The eligible key set is hashed in the
    # same form the centroid builder uses, so both artifacts from one
    # build are comparable by hash. The population label is derived from
    # REAL SET PARITY against the harness default eligible keys — an
    # explicit single-corpus run is recorded as the subset experiment it
    # is, never silently presented as the evaluated population. Go
    # decoder (internal/agent/tfidf_veto.go loadTfidfVeto) unmarshals a
    # fixed struct without DisallowUnknownFields: the extra
    # ``provenance`` key is tolerated.
    key_list = sorted(hashlib.sha256(t.strip().encode()).hexdigest()[:16]
                      for t in docs)
    eligible_sha = hashlib.sha256("\n".join(key_list).encode()).hexdigest()
    base_cases, adv_cases = h.load_cases()
    default_keys = {hashlib.sha256(c.text.strip().encode()).hexdigest()[:16]
                    for c in base_cases + adv_cases
                    if not c.ood and c.intent}
    is_default = set(key_list) == default_keys
    model = {
        "built_at": datetime.now(timezone.utc).isoformat(),
        "corpus": [c for c in args.corpus],
        "ngram_range": [NGRAM_LO, NGRAM_HI],
        "char_wb": CHAR_WB,
        "train_docs": N,
        "train_accuracy": round(correct / N, 4),
        "vocab": vocab,
        "idf": idf,
        "classes": classes,
        "coef": coef,
        "intercept": intercept,
        "threshold": 0.0,
        # ---- additive provenance block (Go-tolerated) ----
        "provenance": {
            "schema": 1,
            "population": ("default-eval-population" if is_default
                           else "subset-explicit"),
            "document_count": N,
            "eligible_key_set_sha256": eligible_sha,
            "source_files": [str(Path(c).resolve()) for c in args.corpus],
            "source_sha256": {str(Path(c).resolve()): _file_sha256(c)
                              for c in args.corpus},
            "preprocessing": {
                "ngram_lo": NGRAM_LO, "ngram_hi": NGRAM_HI,
                "char_wb": CHAR_WB, "sublinear_tf": True,
                "smooth_idf": True, "min_df": args.min_df,
                "lowercase": True,
            },
        },
    }
    out = Path(args.out)
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(model))
    print(f"docs: {N}  features: {len(vocab)}  classes: {nC}")
    print(f"train accuracy: {correct/N:.2%}")
    print(f"wrote {out} ({out.stat().st_size/1024:.0f} KB)")
    return 0


def main():
    sys.exit(build())


if __name__ == "__main__":
    main()
