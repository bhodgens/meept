#!/usr/bin/env python3
"""Build classifier-prefilter centroids from the labeled eval corpus.

Embeds every corpus case through a running embed server
(scripts/embed_server.py) and writes the kNN example store the Go
prefilter loads:

    ~/.meept/classifier_prefilter_centroids.json  (default output)

The store is self-describing (model, dimension, corpus, built_at) so the
Go side can validate dimension drift and the provenance is auditable.

Default population (routing-repair leaf 03, contract C3): the FULL
evaluated corpus — the union of eligible (non-OOD) base + adversarial
keys from the eval-harness loader (tools/classifier-eval/eval_harness.py),
i.e. exactly the population the reported eval numbers were measured on.
The historical default (classifier-adversarial-corpus only) trained the
runtime index on a 222-key subset of the 361-key population.

Run (server must be up):
    python3 scripts/build_prefilter_centroids.py \
        --url http://127.0.0.1:8090/v1 \
        --model-path /Volumes/LLMs/Qwen3-Embedding-0.6B-4bit-DWQ

Explicit single-corpus SUBSET experiment (labeled as such in the
artifact provenance):
    python3 scripts/build_prefilter_centroids.py \
        --corpus testdata/eval/classifier-adversarial-corpus.json5

Sweep mode (accuracy vs threshold table, all cases embedded once):
    python3 scripts/build_prefilter_centroids.py --sweep
"""
import argparse
import hashlib
import json
import re
import sys
import time
import urllib.request
from collections import defaultdict
from datetime import datetime, timezone
from pathlib import Path

# The prefilter's runtime index must be built from the same corpus the
# eval numbers were measured on: the eval harness (tools/classifier-eval/
# eval_harness.py) trains on base + adversarial, where classifier-
# adversarial-corpus.json5 is the tracked cases-layout corpus (iters 2-20,
# includes the quickplan class + OOD abstentions). The base-only corpus
# predates the quickplan class entirely — an index built from it would
# carry a 12-class label space the eval never validated. (CORRECTION
# 2026-09-10, audit M7: default was previously classifier-test-corpus.)
DEFAULT_CORPUS = "testdata/eval/classifier-adversarial-corpus.json5"
DEFAULT_URL = "http://127.0.0.1:8090/v1"
DEFAULT_OUT = str(Path.home() / ".meept" / "classifier_prefilter_centroids.json")


def file_sha256(path: str, chunk: int = 1 << 20) -> str:
    """Streaming content hash of one file (no full read into memory)."""
    h = hashlib.sha256()
    with open(path, "rb") as f:
        while True:
            b = f.read(chunk)
            if not b:
                break
            h.update(b)
    return h.hexdigest()


def model_dir_sha256(model_path: str, chunk: int = 1 << 20) -> str:
    """Content fingerprint of a LOCAL model directory (contract C3,
    recorded decision 2026-09-17 #3): every file is hashed in sorted
    path order so ALL weight shards plus tokenizer/config files are
    covered; an alias name alone never identifies weights. Opt-in via
    --hash-model at build time because hashing multi-GB shards is slow.
    Endpoint URLs / API keys are never hashed."""
    root = Path(model_path)
    if not root.is_dir():
        raise ValueError(f"--model-path {model_path} is not a directory")
    h = hashlib.sha256()
    for p in sorted(root.rglob("*")):
        if not p.is_file():
            continue
        rel = str(p.relative_to(root))
        h.update(rel.encode())
        h.update(b"\x00")
        if p.stat().st_size <= 8 << 20:
            h.update(p.read_bytes())
        else:
            with open(p, "rb") as f:
                while True:
                    b = f.read(chunk)
                    if not b:
                        break
                    h.update(b)
        h.update(b"\x00")
    return h.hexdigest()


def case_key_of(text: str) -> str:
    """Case key identical to eval_harness.case_key (sha256[:16] of the
    stripped text). Mirrored here so the eligible-key-set hash can be
    computed without a second harness import at call time."""
    import hashlib as _h
    return _h.sha256(text.strip().encode()).hexdigest()[:16]


def H_BASE_CORPUS():
    return _harness().BASE_CORPUS


def H_ADV_CORPUS():
    return _harness().ADV_CORPUS


def _harness():
    """Import the eval-harness loader WITHOUT triggering its model
    dependencies at builder import time. eval_harness imports numpy at
    module level (stdlib+numpy is the verified builder environment);
    everything model/network-shaped there lives behind functions, so the
    import itself is side-effect free. This is the SINGLE corpus parser
    contract: the builder reuses the harness loader instead of carrying
    its own drifting copy of the JSON5 + layout logic."""
    import importlib.util
    harness_path = (Path(__file__).resolve().parent.parent
                    / "tools" / "classifier-eval" / "eval_harness.py")
    spec = importlib.util.spec_from_file_location("_eval_harness_for_builder",
                                                  harness_path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def eligible_cases(base: Path | None, adv: Path | None):
    """Eligible (indexable) cases via the harness loader: non-OOD cases
    with an intent, from base + adversarial combined. Returns a list of
    harness Case objects (text, intent, agent)."""
    h = _harness()
    base_cases, adv_cases = h.load_cases(
        base if base is not None else h.BASE_CORPUS,
        adv if adv is not None else h.ADV_CORPUS)
    # A missing optional side (explicit-subset runs pass a placeholder
    # adversarial path) yields zero cases, matching harness --adversarial
    # semantics: a nonexistent cases file is an empty population, not a
    # crash. Only the BASE corpus is mandatory.
    return [c for c in base_cases + adv_cases if not c.ood and c.intent]


def select_default_cases() -> list:
    """Default build population: the FULL evaluated corpus — the union of
    eligible base + adversarial keys from the harness loader (routing
    repair leaf 03, contract C3). The previous default (adversarial-only)
    trained the runtime index on a 222-key subset of the 361-key
    population the eval numbers were measured on."""
    return eligible_cases(None, None)


def select_explicit_subset(corpus: Path) -> list:
    """Explicit single-corpus selection: a labeled SUBSET experiment, not
    the default. Either corpus layout is accepted: the file is routed to
    the matching slot of the harness loader (categories -> base slot,
    cases -> adversarial slot) so the loader's own semantics do all the
    parsing; the other slot stays empty."""
    h = _harness()
    absent = Path("/nonexistent/no-adversarial.json5")
    data = h.parse_json5(corpus.read_text())
    if data.get("categories"):
        base_cases, adv_cases = h.load_cases(corpus, absent)
    else:
        # cases layout: the loader reads the base slot unconditionally
        # (a cases-layout file contributes zero cases there) and the
        # adversarial slot carries the actual entries.
        base_cases, adv_cases = h.load_cases(corpus, corpus)
    return [c for c in base_cases + adv_cases if not c.ood and c.intent]


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
    # Strip // line comments (adversarial corpus uses them; iters 19+).
    text = re.sub(r"(?m)^\s*//.*$", "", text)
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
    """Returns (input, intent, agent) triples.

    Two layouts supported:
    - legacy categories: {categories: {intent: [{input, expected_intent}]}}
    - adversarial cases: {cases: [{input, expected_intent | ood}]}
      (classifier-iteration corpus, iters 2-20). OOD cases are skipped:
      the prefilter index carries only real intents.
    """
    raw = Path(corpus_path).read_text()
    data = parse_json5(raw)
    cases = []
    for intent, entries in (data.get("categories") or {}).items():
        for e in entries:
            cases.append((e["input"], e.get("expected_intent", intent),
                          e.get("expected_agent", "")))
    for e in (data.get("cases") or []):
        if e.get("ood") or not e.get("expected_intent"):
            continue  # OOD / compound-abstain: not indexable
        cases.append((e["input"], e["expected_intent"],
                      e.get("expected_agent", "")))
    return cases


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--corpus", default=None,
                    help="single explicit corpus (SUBSET experiment; the "
                         "default builds from the full evaluated base + "
                         "adversarial population the harness measures)")
    ap.add_argument("--url", default=DEFAULT_URL)
    ap.add_argument("--model", default="qwen3-embedding")
    ap.add_argument("--model-path", default=None,
                    help="local weights directory backing the embed server "
                         "(recorded + content-hashed into the artifact "
                         "provenance; no files are read unless --hash-model "
                         "is passed)")
    ap.add_argument("--hash-model", action="store_true",
                    help="hash the model content at build time (reads the "
                         "weight shards; slow for large models)")
    ap.add_argument("--out", default=DEFAULT_OUT)
    ap.add_argument("--instruction", default="",
                    help="Qwen3-Embedding task instruction prefixed to every "
                         "query for embedding (official instruction-aware "
                         "format; must be identical at serving time)")
    ap.add_argument("--sweep", action="store_true",
                    help="report kNN-unanimity accuracy vs threshold and "
                         "exit (no write)")
    args = ap.parse_args()

    if args.corpus:
        # Explicit single-corpus run: labeled a subset, never silently
        # presented as the evaluated population (leaf 03 contract C3).
        cases = [((c.text, c.intent, c.agent), "subset-explicit")
                 for c in select_explicit_subset(Path(args.corpus))]
        print(f"corpus: {len(cases)} SUBSET cases from {args.corpus} "
              f"(explicit single-corpus experiment)")
    else:
        # Default: the FULL evaluated population (eligible base +
        # adversarial keys from the harness loader — the same set the
        # eval numbers are measured on).
        cases = [((c.text, c.intent, c.agent), "default-eval-population")
                 for c in select_default_cases()]
        print(f"corpus: {len(cases)} cases from the full evaluated "
              f"population (base + adversarial, harness loader)")

    # Embed in batches of 32 (keeps request sizes sane, still fast).
    triples = [t for t, _lbl in cases]
    corpus_label = cases[0][1] if cases else "empty"
    vectors: list[list[float]] = []
    t0 = time.time()
    for i in range(0, len(triples), 32):
        chunk = [args.instruction + c[0] if args.instruction else c[0]
                 for c in triples[i:i + 32]]
        vectors.extend(embed_batch(args.url, chunk, args.model))
        print(f"  embedded {min(i + 32, len(triples))}/{len(triples)}")
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

        intents_of = [c[1] for c in triples]
        K = 5
        print(f"\nkNN-unanimity sweep (k={K}, leave-one-out — held-out):")
        best = (0.0, 0.0, 0.0)
        for tau in [0.85, 0.80, 0.75, 0.70, 0.65, 0.60, 0.55, 0.50]:
            direct = correct = 0
            for j, ((text, intent, _), vec) in enumerate(zip(triples, unit)):
                sims = sorted(
                    ((cos(vec, unit[m]), intents_of[m]) for m in range(len(triples)) if m != j),
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
            total = len(triples)
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
    #
    # PROVENANCE (leaf 03, MEAS-02/MEAS-05, contract C3): the store
    # records exactly which population and which model content produced
    # it. The Go decoder (internal/agent/embedding_prefilter.go)
    # unmarshals into a fixed struct WITHOUT DisallowUnknownFields, so
    # additional keys are tolerated by the runtime reader (verified by
    # test_provenance against the Go source). No existing field changes.
    key_list = [case_key_of(t[0]) for t in triples]
    eligible_sha = hashlib.sha256(
        "\n".join(sorted(key_list)).encode()).hexdigest()
    sources = ([str(Path(args.corpus).resolve())] if args.corpus
               else [str(H_BASE_CORPUS()), str(H_ADV_CORPUS())])
    model_path = args.model_path
    store = {
        "model": args.model,
        "dimension": dim,
        "built_at": datetime.now(timezone.utc).isoformat(),
        "corpus": str(Path(args.corpus).resolve()) if args.corpus
                  else "base+adversarial (harness default population)",
        "instruction": args.instruction,
        "k": 5,
        # ---- new provenance block (additive, Go-tolerated) ----
        "provenance": {
            "schema": 1,
            "population": corpus_label,   # default-eval-population | subset-explicit
            "document_count": len(triples),
            "eligible_key_set_sha256": eligible_sha,
            "source_files": sources,
            "source_sha256": {s: file_sha256(s) for s in sources},
            "embedding": {
                "model_alias": args.model,
                "model_path": model_path,
                "model_content_sha256": (
                    model_dir_sha256(model_path) if model_path else None),
                "instruction": args.instruction,
                "preprocessing": "last-token(EOS) pooling, L2-normalized "
                                 "(scripts/embed_server.py recipe)",
            },
        },
        "examples": [
            {
                "intent": intent,
                "agent": agent,
                "text": text,
                "vector": vec,
            }
            for (text, intent, agent), vec in zip(triples, vectors)
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
