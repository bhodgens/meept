#!/usr/bin/env python3
"""Classifier-iteration eval harness (campaign: docs/plans/classifier-iteration/).

Offline, Go-free. Loads the base + adversarial corpora, embeds via a running
embed server (scripts/embed_server.py) with a persistent disk cache, and
scores a permutation spec (JSON) with 5-fold cross-evaluation.

Protocol per docs/plans/classifier-iteration/master.md:
  - 5-fold gate evaluation, fixed seed 42, fold assignment cached next to the
    corpus so the split is stable for the whole campaign.
  - Metrics: coverage C, precision P, forced accuracy A, macro-F1, OOD
    abstain rate, p50 latency, wrong-route count, composite SCORE, and
    headline E2E = (gate-correct + 0.868 * abstained) / total.
  - Adversarial cases: first-seen fold in TEST (holdout), recorded in the
    fold-assignment cache; they NEVER enter the train index before M4
    promotion decisions.
  - OOD cases: gold OOD must abstain; wrong-direct-routes are penalized.

Usage:
  eval_harness.py --spec spec.json                     # run one permutation
  eval_harness.py --sweep specs/*.json                 # batch, one table
  eval_harness.py --confusions spec.json --top 30      # confusion harvest
  eval_harness.py --dump-cache spec.json               # write vectors to npz

Spec format (JSON):
  {
    "name": "knn5-unanimity-0.70",
    "embedder": {"url": "http://127.0.0.1:8090/v1", "model": "qwen3-embedding",
                 "instruction": ""},
    "head": {"type": "knn_unanimity", "k": 5, "floor": 0.70}
  }
Head types: knn_unanimity | knn_majority | centroid | logistic |
            two_stage ({"rescue": {"type": "logistic", "tau": 0.85}}) |
            prototype_hybrid ({"desc_file": "intent_descriptions.json5"})

Exit code 0 always (measurement tool); results go to stdout + files under
results/. NEVER mutates daemon config; no network except the embed server.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import re
import sys
import time
import urllib.request
from pathlib import Path

import numpy as np

REPO = Path(__file__).resolve().parent.parent.parent
BASE_CORPUS = REPO / "testdata/eval/classifier-test-corpus.json5"
ADV_CORPUS = REPO / "testdata/eval/classifier-adversarial-corpus.json5"
RESULTS = Path(__file__).resolve().parent / "results"
FOLD_CACHE = Path(__file__).resolve().parent / "fold-assignment.json"
SEED = 42
NFOLDS = 5
CHAIN_BASELINE = 0.868  # measured LLM-chain accuracy for abstained cases


# ---------------------------------------------------------------- corpus

def parse_json5(text: str) -> dict:
    """Parser for the corpus JSON5 flavor (raw apostrophes in strings,
    unquoted keys, trailing commas). Same approach as
    scripts/build_prefilter_centroids.py: stash double-quoted strings
    verbatim, normalize the remainder, restore."""
    strings: list[str] = []

    def stash(m: re.Match) -> str:
        strings.append(m.group(0)[1:-1])
        return f"\x00{len(strings)-1}\x00"

    # Double-quoted strings: no escapes in the corpora, so [^"] suffices.
    text = re.sub(r'"[^"]*"', stash, text)
    # Strip // line comments (adversarial corpus uses them).
    text = re.sub(r"(?m)^\s*//.*$", "", text)
    # Quote unquoted object keys.
    text = re.sub(r"(?<![:\w\x00'\"])([A-Za-z_][A-Za-z0-9_]*)\s*:", r'"\1":', text)
    text = re.sub(r",(\s*[}\]])", r"\1", text)

    def unstash(m: re.Match) -> str:
        return json.dumps(strings[int(m.group(1))])

    text = re.sub(r"\x00(\d+)\x00", unstash, text)
    return json.loads(text)


class Case:
    __slots__ = ("text", "intent", "agent", "ood", "provenance", "added_in", "case_id")

    def __init__(self, text: str, intent: str, agent: str, ood: bool,
                 provenance: str, added_in: str, case_id: str):
        self.text = text
        self.intent = intent  # "OOD" for ood cases
        self.agent = agent
        self.ood = ood
        self.provenance = provenance
        self.added_in = added_in
        self.case_id = case_id


def _cases_from_categories(data: dict, source_default: str) -> list[Case]:
    cases = []
    for intent, entries in (data.get("categories") or {}).items():
        for i, e in enumerate(entries):
            cases.append(Case(
                text=e["input"], intent=e.get("expected_intent", intent),
                agent=e.get("expected_agent", ""),
                ood=False,
                provenance=e.get("source", source_default),
                added_in=e.get("added_in", ""),
                case_id=f"{intent}#{i}",
            ))
    return cases


def load_cases(base: Path = BASE_CORPUS, adv: Path = ADV_CORPUS) -> tuple[list[Case], list[Case]]:
    base_cases = _cases_from_categories(parse_json5(base.read_text()), "base")
    adv_cases: list[Case] = []
    if adv.exists():
        data = parse_json5(adv.read_text())
        # two layouts accepted: {"cases":[...]} or {"categories":{intent:[...]}}
        for i, e in enumerate(data.get("cases") or []):
            adv_cases.append(Case(
                text=e["input"],
                intent=e.get("expected_intent") or ("OOD" if e.get("ood") else ""),
                agent=e.get("expected_agent", ""),
                ood=bool(e.get("ood")) or e.get("expected_intent") == "OOD",
                provenance=e.get("source", "synthetic"),
                added_in=e.get("added_in", ""),
                case_id=e.get("id") or f"adv#{i}",
            ))
        adv_cases += _cases_from_categories(data, "synthetic")
    return base_cases, adv_cases


def case_key(text: str) -> str:
    return hashlib.sha256(text.strip().encode()).hexdigest()[:16]


# ---------------------------------------------------------------- folds

def assign_folds(cases: list[Case]) -> dict[str, int]:
    """Stable 5-fold assignment. Deterministic per case_id+text; the map is
    persisted so adversarial cases keep their first-seen TEST fold for the
    whole campaign (master.md: adversarial always in test when first added)."""
    if FOLD_CACHE.exists():
        saved = json.loads(FOLD_CACHE.read_text())
    else:
        saved = {}
    changed = False
    out: dict[str, int] = {}
    for c in cases:
        k = case_key(c.text)
        if k in saved:
            out[k] = saved[k]
            continue
        # deterministic hash-fold; round-robin within hash bucket keeps
        # strata balanced without needing labels (stratification by intent
        # happens at index build time, not fold time)
        h = int(hashlib.sha256(f"{SEED}:{k}".encode()).hexdigest(), 16)
        out[k] = h % NFOLDS
        saved[k] = out[k]
        changed = True
    if changed:
        FOLD_CACHE.write_text(json.dumps(saved, indent=0, sort_keys=True))
    return out


# ---------------------------------------------------------------- embedder

class Embedder:
    def __init__(self, url: str, model: str, instruction: str = "",
                 cache_dir: Path | None = None):
        self.url = url.rstrip("/")
        self.model = model
        self.instruction = instruction
        tag = hashlib.sha256(f"{model}|{instruction}".encode()).hexdigest()[:12]
        self.cache_dir = (cache_dir or (Path.home() / ".meept" / "classifier-eval-cache")) / tag
        self.cache_dir.mkdir(parents=True, exist_ok=True)
        self.mem: dict[str, np.ndarray] = {}
        self.embed_time = 0.0
        self.embed_calls = 0
        # per-text latency samples (ms) for cache-miss embeds
        self.latency_ms: list[float] = []

    def _cache_path(self, key: str) -> Path:
        return self.cache_dir / f"{key}.npy"

    def embed_keys(self, texts: list[str], keys: list[str]) -> None:
        missing = [(k, t) for k, t in zip(keys, texts)
                   if k not in self.mem and not self._cache_path(k).exists()]
        # load disk hits
        for k in set(keys):
            if k in self.mem:
                continue
            p = self._cache_path(k)
            if p.exists():
                self.mem[k] = np.load(p)
        if missing:
            t0 = time.perf_counter()
            payload = json.dumps({
                "input": [self.instruction + t for _, t in missing],
                "model": self.model,
            }).encode()
            req = urllib.request.Request(
                self.url + "/embeddings", data=payload,
                headers={"Content-Type": "application/json"}, method="POST")
            with urllib.request.urlopen(req, timeout=600) as resp:
                out = json.loads(resp.read())
            dt = (time.perf_counter() - t0) * 1000.0
            per = dt / max(len(missing), 1)
            self.latency_ms.extend([per] * len(missing))
            self.embed_calls += 1
            got: list[np.ndarray | None] = [None] * len(missing)
            for item in out["data"]:
                got[item["index"]] = np.asarray(item["embedding"], dtype=np.float32)
            if any(v is None for v in got):
                raise RuntimeError("embed server returned fewer vectors than inputs")
            for (k, _), item in zip(missing, out["data"]):
                v = np.asarray(item["embedding"], dtype=np.float32)
                n = float(np.linalg.norm(v))
                if n > 0:
                    v = v / n
                np.save(self._cache_path(k), v)
                self.mem[k] = v
        self.embed_time += time.perf_counter() - 0  # noop keeper for symmetry
        _ = self.embed_time

    def vectors(self, keys: list[str]) -> np.ndarray:
        return np.stack([self.mem[k] for k in keys])


# ---------------------------------------------------------------- heads

def cosmat(q: np.ndarray, idx: np.ndarray) -> np.ndarray:
    # vectors are pre-normalized
    return q @ idx.T


class Gate:
    """Decides: route(intent) / abstain, per query vector."""

    def decide(self, q: np.ndarray, intents: list[str]) -> tuple[str | None, float]:
        raise NotImplementedError


class KnnUnanimity(Gate):
    def __init__(self, k: int, floor: float):
        self.k, self.floor = k, floor

    def decide(self, q, intents):
        sims = cosmat(q[None], ALLVEC)[0].copy()
        sims[self_mask] = -2.0
        top = np.argsort(-sims)[:self.k]
        s = sims[top]
        if (s < self.floor).any():
            return None, float(s[0])
        labels = [intents[i] for i in top]
        if len(set(labels)) > 1:
            return None, float(s[0])
        return labels[0], float(s[0])


class KnnMajority(Gate):
    def __init__(self, k: int, floor: float, margin: float = 0.0):
        self.k, self.floor, self.margin = k, floor, margin

    def decide(self, q, intents):
        sims = cosmat(q[None], ALLVEC)[0].copy()
        sims[self_mask] = -2.0
        top = np.argsort(-sims)[:self.k]
        s = sims[top]
        if (s < self.floor).any():
            return None, float(s[0])
        votes: dict[str, int] = {}
        for i in top:
            votes[intents[i]] = votes.get(intents[i], 0) + 1
        best = max(votes.items(), key=lambda kv: kv[1])
        need = max(2, self.k - 1)
        if best[1] < need:
            return None, float(s[0])
        # top-1 must be among winners (sanity) and win by margin
        top_label = intents[top[0]]
        if votes[top_label] < best[1]:
            return None, float(s[0])
        if len(votes) > 1:
            second = sorted(votes.values(), reverse=True)[1]
            if best[1] - second < self.margin:
                return None, float(s[0])
        return top_label, float(s[0])


class CentroidGate(Gate):
    def __init__(self, floor: float, margin: float = 0.02):
        self.floor, self.margin = floor, margin

    def fit(self, train_idx: np.ndarray, train_intents: list[str]):
        self.centroids = {}
        # train_idx holds GLOBAL case indices. Use the module-level full
        # view (_FULL_V, set by run_permutation) for exact row selection.
        Vfull = globals().get("_FULL_V")
        src = Vfull if Vfull is not None else ALLVEC
        for lab in sorted(set(train_intents)):
            sel = [gi for gi, l in zip(train_idx, train_intents) if l == lab]
            c = src[sel].mean(axis=0)
            c /= (np.linalg.norm(c) + 1e-12)
            self.centroids[lab] = c

    def decide(self, q, intents):
        # NOTE: uses centroids fit on the CURRENT fold's train slice
        labs = sorted(self.centroids)
        C = np.stack([self.centroids[l] for l in labs])
        sims = cosmat(q[None], C)[0]
        order = np.argsort(-sims)
        if sims[order[0]] < self.floor:
            return None, float(sims[order[0]])
        if len(order) > 1 and sims[order[0]] - sims[order[1]] < self.margin:
            return None, float(sims[order[0]])
        return labs[order[0]], float(sims[order[0]])


class LogisticHead(Gate):
    """Plain-NumPy multinomial logistic regression over frozen embeddings.
    Deterministic (full-batch, fixed init zero). Abstains when max prob < tau
    or top-2 gap < gap_tau."""

    def __init__(self, tau: float, gap_tau: float = 0.0, lr: float = 0.5,
                 epochs: int = 400, l2: float = 1e-3):
        self.tau, self.gap_tau, self.lr, self.epochs, self.l2 = tau, gap_tau, lr, epochs, l2

    def fit(self, train_idx: np.ndarray, train_intents: list[str]):
        # train_idx holds GLOBAL case indices. Use the module-level full
        # view (_FULL_V, set by run_permutation) so the row set matches
        # train_idx exactly regardless of the current ALLVEC slice.
        Vfull = globals().get("_FULL_V")
        src = Vfull if Vfull is not None else ALLVEC
        X = src[train_idx]
        labs = sorted(set(train_intents))
        y = np.array([labs.index(l) for l in train_intents])
        n, d = X.shape
        K = len(labs)
        W = np.zeros((d, K))
        b = np.zeros(K)
        Y = np.zeros((n, K))
        Y[np.arange(n), y] = 1.0
        for _ in range(self.epochs):
            z = X @ W + b
            z -= z.max(axis=1, keepdims=True)
            p = np.exp(z)
            p /= p.sum(axis=1, keepdims=True)
            g = (p - Y) / n
            W -= self.lr * (X.T @ g + self.l2 * W)
            b -= self.lr * g.sum(axis=0)
        self.W, self.b, self.labs = W, b, labs

    def decide(self, q, intents):
        z = q @ self.W + self.b
        z -= z.max()
        p = np.exp(z)
        p /= p.sum()
        order = np.argsort(-p)
        top = float(p[order[0]])
        gap = top - float(p[order[1]]) if len(order) > 1 else top
        if top < self.tau or gap < self.gap_tau:
            return None, top
        return self.labs[order[0]], top


class TwoStage(Gate):
    """kNN abstain -> rescue head. Rescue routes only above its own tau."""

    def __init__(self, knn: KnnUnanimity, rescue: Gate, tau: float):
        self.knn, self.rescue, self.tau = knn, rescue, tau

    def fit(self, train_idx: np.ndarray, train_intents: list[str]) -> None:
        if hasattr(self.rescue, "fit"):
            self.rescue.fit(train_idx, train_intents)

    def decide(self, q, intents):
        lab, score = self.knn.decide(q, intents)
        if lab is not None:
            return lab, score
        lab2, s2 = self.rescue.decide(q, intents)
        if lab2 is not None and s2 >= self.tau:
            return lab2, s2
        return None, min(score, s2)


class PrototypeHybrid(Gate):
    """kNN over examples + one extra prototype vector per intent built from
    the intent-description line (buildIntentText pattern: description is an
    extra index entry per intent). Descriptions from desc_file JSON5:
    {"code": "write, implement, or refactor source code ...", ...}"""

    def __init__(self, knn: KnnUnanimity, descriptions: dict[str, str],
                 emb: Embedder, proto_weight: float = 1.0):
        self.knn = knn
        self.descriptions = descriptions
        self.emb = emb
        self.proto_weight = proto_weight

    def fit(self, train_idx, train_intents):
        # embed descriptions once, cache them under synthetic keys
        self.proto_idx: list[int] = []
        self.proto_labels: list[str] = []
        rows = []
        for lab, desc in sorted(self.descriptions.items()):
            if lab == "OOD":
                continue
            k = "desc:" + hashlib.sha256(desc.encode()).hexdigest()[:16]
            if k not in self.emb.mem:
                self.emb.embed_keys([desc], [k])
            rows.append(self.emb.mem[k] * self.proto_weight)
            self.proto_labels.append(lab)
        self.descs = np.stack(rows)
        self.train_intents = train_intents

    def decide(self, q, intents):
        sims = cosmat(q[None], ALLVEC)[0].copy()
        sims[self_mask] = -2.0
        dsims = cosmat(q[None], self.descs)[0]
        for lab, s in zip(self.proto_labels, dsims):
            rows = np.where(np.array(self.train_intents) == lab)[0]
            if len(rows) == 0:
                continue
            # inject prototype as a virtual neighbor with weight-scaled sim
            best_ex = sims[rows].max()
            sims[rows[np.argmax(sims[rows])]] = max(best_ex, float(s) * self.proto_weight)
        k = self.knn.k
        top = np.argsort(-sims)[:k]
        s = sims[top]
        if (s < self.knn.floor).any():
            return None, float(s[0])
        labels = [intents[i] for i in top]
        if len(set(labels)) > 1:
            return None, float(s[0])
        return labels[0], float(s[0])


def build_head(spec_head: dict, emb: Embedder) -> Gate:
    t = spec_head["type"]
    if t == "knn_unanimity":
        return KnnUnanimity(int(spec_head.get("k", 5)), float(spec_head.get("floor", 0.70)))
    if t == "knn_majority":
        return KnnMajority(int(spec_head.get("k", 5)), float(spec_head.get("floor", 0.70)),
                           float(spec_head.get("margin", 0.0)))
    if t == "centroid":
        return CentroidGate(float(spec_head.get("floor", 0.70)),
                            float(spec_head.get("margin", 0.02)))
    if t == "logistic":
        return LogisticHead(float(spec_head.get("tau", 0.85)),
                            float(spec_head.get("gap_tau", 0.0)))
    if t == "two_stage":
        rescue = build_head(spec_head["rescue"], emb)
        base = KnnUnanimity(int(spec_head.get("k", 5)), float(spec_head.get("floor", 0.70)))
        return TwoStage(base, rescue, float(spec_head.get("tau", 0.85)))
    if t == "prototype_hybrid":
        descs = parse_json5(Path(spec_head["desc_file"]).read_text())
        base = KnnUnanimity(int(spec_head.get("k", 5)), float(spec_head.get("floor", 0.70)))
        return PrototypeHybrid(base, {k: str(v) for k, v in descs.items()}, emb,
                               float(spec_head.get("proto_weight", 1.0)))
    raise ValueError(f"unknown head type {t}")


# ---------------------------------------------------------------- metrics

def macro_f1(tp: dict, fp: dict, fn: dict) -> float:
    f1s = []
    labs = set(list(tp) + list(fp) + list(fn))
    for lab in labs:
        t = tp.get(lab, 0)
        p = fp.get(lab, 0)
        f = fn.get(lab, 0)
        prec = t / (t + p) if t + p else 0.0
        rec = t / (t + f) if t + f else 0.0
        f1s.append(2 * prec * rec / (prec + rec) if prec + rec else 0.0)
    return sum(f1s) / len(f1s) if f1s else 0.0


# ---------------------------------------------------------------- main eval

ALLVEC: np.ndarray = np.zeros((1, 1))  # global index matrix, set per fold
self_mask: np.ndarray = np.array([], dtype=int)


def run_permutation(spec: dict, cases: list[Case], folds: dict[str, int],
                    emb: Embedder, collect_confusions: int = 0):
    global ALLVEC, self_mask
    texts = [c.text for c in cases]
    keys = [case_key(t) for t in texts]
    emb.embed_keys(texts, keys)
    V = emb.vectors(keys)
    intents = [c.intent for c in cases]
    n = len(cases)
    is_ood = np.array([c.ood for c in cases])
    key_arr = np.array(keys)
    # module-level full view for direct-fit use outside run_permutation
    globals()["_FULL_V"] = V

    fold_of = np.array([folds[k] for k in keys])
    confusion = []  # (query case, predicted, score)

    per_fold = []
    all_lat = list(emb.latency_ms)
    for f in range(NFOLDS):
        test_m = fold_of == f
        train_m = ~test_m & ~is_ood  # OOD cases NEVER enter the index: the
        # production gate has no OOD class to vote for; OOD must abstain via
        # mixed neighborhoods / low similarity, not by voting label "OOD".
        train_idx = np.where(train_m)[0]
        # NOTE for kNN heads the index is ALL cases in the train folds.
        # ALLVEC must be set BEFORE fit(): fitted heads (centroid, logistic,
        # prototype) index ALLVEC by train-mask positions.
        ALLVEC = V[train_m]
        # adversarial holdout: cases never leave their first-seen fold
        head = build_head(spec["head"], emb)
        fit = getattr(head, "fit", None)
        if fit is not None:
            fit(train_idx, [intents[i] for i in train_idx])
        results = []
        for qi in np.where(test_m)[0]:
            if is_ood[qi]:
                results.append(None)
                continue
            # self-exclusion: mask any train case with the same text key
            self_mask = np.where(key_arr[qi] == key_arr[train_m])[0]
            lab, score = head.decide(V[qi], [intents[i] for i in train_idx])
            results.append((lab, score))
            if lab is not None and lab != intents[qi] and len(confusion) < 100000:
                confusion.append((cases[qi], lab, float(score)))
            elif lab is not None and collect_confusions and len(confusion) < collect_confusions:
                confusion.append((cases[qi], lab, float(score)))
        per_fold.append(results)

    # pool folds
    direct = wrong = ood_total = ood_abstain = 0
    tp: dict = {}
    fp: dict = {}
    fn: dict = {}
    correct = 0
    total_in = sum(1 for c in cases if not c.ood)
    margin_cases = []
    for f in range(NFOLDS):
        test_m = np.where(fold_of == f)[0]
        for pos, qi in enumerate(test_m):
            c = cases[qi]
            if c.ood:
                ood_total += 1
                r = per_fold[f][pos]
                if r is None or r[0] is None:
                    ood_abstain += 1
                else:
                    wrong += 1  # OOD routed = wrong-direct
                    fp[r[0]] = fp.get(r[0], 0) + 1
                continue
            r = per_fold[f][pos]
            if r is None or r[0] is None:
                fn[c.intent] = fn.get(c.intent, 0) + 1  # missed coverage
                continue
            direct += 1
            if r[0] == c.intent:
                correct += 1
                tp[c.intent] = tp.get(c.intent, 0) + 1
            else:
                wrong += 1
                fp[r[0]] = fp.get(r[0], 0) + 1
                fn[c.intent] = fn.get(c.intent, 0) + 1
            if abs(r[1] - spec["head"].get("floor", spec["head"].get("tau", 0.7))) < 0.05:
                margin_cases.append((c, r[0], r[1]))

    C = direct / total_in if total_in else 0.0
    P = correct / direct if direct else 0.0
    A = (correct / (correct + wrong)) if (correct + wrong) else 0.0
    f1 = macro_f1(tp, fp, fn)
    ood_r = ood_abstain / ood_total if ood_total else 1.0
    e2e = (correct + CHAIN_BASELINE * (total_in - direct)) / total_in if total_in else 0.0
    score = C * P * P - 5 * (wrong / total_in if total_in else 0.0)
    # latency p50 from cache-miss embeds (per-text); fall back to note
    lat = float(np.percentile(all_lat, 50)) if all_lat else 0.0
    return {
        "name": spec["name"], "total": total_in, "direct": direct,
        "correct": correct, "wrong": wrong, "C": C, "P": P, "A": A,
        "F1": f1, "OOD_total": ood_total, "OOD_abstain": ood_abstain,
        "OOD_R": ood_r, "E2E": e2e, "SCORE": score, "p50_ms": lat,
        "confusion": confusion[:collect_confusions] if collect_confusions else confusion[:50],
        "margin_cases": margin_cases[:50],
    }


def fmt_row(m: dict) -> str:
    return (f"{m['name']:<38} C={m['C']:6.1%} P={m['P']:6.1%} A={m['A']:6.1%} "
            f"F1={m['F1']:.3f} OOD-R={m['OOD_R']:6.1%} wrong={m['wrong']:>2} "
            f"E2E={m['E2E']:6.2%} SCORE={m['SCORE']:+.3f} p50={m['p50_ms']:.0f}ms")


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--spec", help="permutation spec JSON")
    ap.add_argument("--sweep", nargs="*", help="spec files to batch")
    ap.add_argument("--confusions", help="spec; dump top confusion cases as JSON")
    ap.add_argument("--dump-cache", help="spec; write vectors npz to results/")
    ap.add_argument("--top", type=int, default=30)
    ap.add_argument("--corpus", default=str(BASE_CORPUS))
    ap.add_argument("--adversarial", default=str(ADV_CORPUS))
    ap.add_argument("--iter-tag", default="", help="results dir tag (iter-N)")
    args = ap.parse_args()

    base_cases, adv_cases = load_cases(Path(args.corpus), Path(args.adversarial))
    cases = base_cases + adv_cases
    folds = assign_folds(cases)
    print(f"corpus: {len(base_cases)} base + {len(adv_cases)} adversarial "
          f"= {len(cases)} cases; folds cached -> {FOLD_CACHE.name}", file=sys.stderr)

    specs: list[dict] = []
    if args.spec:
        specs = [json.loads(Path(args.spec).read_text())]
    elif args.sweep:
        specs = [json.loads(Path(p).read_text()) for p in args.sweep]
    elif not (args.confusions or args.dump_cache):
        ap.error("one of --spec/--sweep/--confusions/--dump-cache required")
        return 2

    if args.dump_cache:
        spec = specs[0]
        e = Embedder(spec["embedder"]["url"], spec["embedder"]["model"],
                     spec["embedder"].get("instruction", ""))
        texts = [c.text for c in cases]
        keys = [case_key(t) for t in texts]
        e.embed_keys(texts, keys)
        RESULTS.mkdir(exist_ok=True)
        out = RESULTS / f"cache-{spec['name'].replace('/', '_')}.npz"
        np.savez_compressed(out, keys=np.array(keys), vectors=np.stack([e.mem[k] for k in keys]),
                            intents=np.array([c.intent for c in cases]),
                            ood=np.array([c.ood for c in cases]))
        print(f"wrote {out} ({len(keys)} vectors)", file=sys.stderr)
        return 0

    outdir = RESULTS / args.iter_tag if args.iter_tag else RESULTS
    outdir.mkdir(parents=True, exist_ok=True)

    table = []
    for spec in specs:
        emb = Embedder(spec["embedder"]["url"], spec["embedder"]["model"],
                       spec["embedder"].get("instruction", ""))
        want_conf = args.top if args.confusions else 0
        m = run_permutation(spec, cases, folds, emb, collect_confusions=want_conf)
        table.append(m)
        print(fmt_row(m))
        if args.confusions:
            rows = [{
                "case_id": c.case_id, "text": c.text, "true": c.intent,
                "pred": pred, "score": score, "provenance": c.provenance,
                "added_in": c.added_in,
            } for (c, pred, score) in m["confusion"]]
            (outdir / "confusions.json").write_text(json.dumps(rows, indent=1))
            print(f"  wrote {len(rows)} confusion rows -> {outdir/'confusions.json'}",
                  file=sys.stderr)

    if specs and not args.confusions:
        stamp = time.strftime("%Y%m%d-%H%M%S")
        preds = []
        for m in table:
            preds.append({k: v for k, v in m.items() if k not in ("confusion", "margin_cases")})
        (outdir / f"summary-{stamp}.json").write_text(json.dumps(preds, indent=1))
        print(f"summary -> {outdir}/summary-{stamp}.json", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
