#!/usr/bin/env python3
"""M4 gold-replay acceptance run (campaign gate: system acc > 86.8%).

Runs the adjudicated 48-case gold replay through the full cascade under
BOTH candidate acceptance policies, on the same ruler and the same fold
rules:

  policy "double-confidence"
      Door A: centroid sim >= 0.60 AND margin >= 0.030 (quickplan cue guard)
      Door B: only when probe prob >= 0.70 AND same-class agreement with A
      Door C: everything else = chain floor 86.8%

  policy "tfidf-veto" (ALT-5)
      Door A routes only when the char n-gram TF-IDF logistic concurs with
      the centroid pick; disagreement falls to the chain. This is the
      policy results/m4-gold-acceptance.md claims at 87.35%.

Two integrity guards run BEFORE any case is scored:

  1. corpus <-> replay disjointness (eval_harness.replay_disjointness).
     The models are fit on the committed corpus; the ruler is the
     adjudicated replay. A replay case that also sits in the fitting
     corpus (exact case_key match, or cosine > SIM_THRESHOLD when the
     embed server is up) is a train-on-test leak. The run REFUSES to score
     when any leak is found (exit 2). It never silently drops the case,
     because dropping it would silently move the denominator.
     An EMPTY ruler is refused too (exit 4): an empty sample makes no
     measurement and is never "disjoint". "Empty" means a genuinely empty
     LIST -- the ruler is parsed with the same JSON5 parser as the corpus,
     so unquoted keys or a reordered ("id" before "input") ruler still
     parses instead of faking empty. A supplied embedding row with a
     zero or non-finite norm is refused (exit 5): its cosine is 0.0 and
     would silently read as disjoint (F20 zero-norm hole). Exit 5 is a
     HARD refusal with no allowlist override -- a degenerate row must be
     re-embedded, unlike a near-duplicate similarity which
     NEAR_DUP_ALLOWLIST can exempt.
  2. coverage floor MIN_ROUTED (F19). With fewer than MIN_ROUTED routed
     cases the verdict is one or two Bernoulli draws against the hard-coded
     CHAIN constant, not a measurement, so the verdict is reported as
     INSUFFICIENT_COVERAGE -- never PASS/FAIL. For the committed 48-case
     ruler this means NO PASS/FAIL exists for EITHER policy (7 and 2 routed
     respectively, both < MIN_ROUTED).

Each policy writes its OWN artifact:
      results/m4-gold-acceptance-double-confidence.json
      results/m4-gold-acceptance-tfidf-veto.json
The historical, committed results/m4-gold-acceptance.json (the
double-confidence FAIL record) is NEVER overwritten: result files are
write-once (F34). A RE-run (canonical name already present) is written
OUTSIDE the repo under `<MEEPT_HOME>/classifier-eval-rerun/` -- or the
system temp dir when MEEPT_HOME itself points inside the repo -- because
the repo un-ignores `tools/classifier-eval/results/**` and would otherwise
leave an untracked artifact inside a tracked directory; the rerun name
carries a timestamp so a re-run cannot clobber the previous re-run either.
(If a tracked rerun location is wanted instead, the owner adds
`classifier-eval-rerun/` to `.gitignore`.)

Every payload THIS script writes carries its own guard evidence --
replay_sha256, replay_n, sim_threshold, leaks, min_routed/coverage_floor --
so the artifact can evidence the guard that produced it instead of
asserting it. The committed results/m4-gold-acceptance.json predates these
fields and does not carry them. A `guard` margin row stores the replay
case_key, NEVER the replay text: the ruler is private transcript text.

Prerequisites the committed record cannot carry: the UNTRACKED adjudicated
replay (replay-gold.local.json5, gitignored) and a live embed server on
:8090. Without them the script reports each policy as UNVALIDATED and
exits non-zero; it never produces a PASS from missing inputs.

Usage:
  python3 m4_gold_acceptance.py                    # full run (needs :8090)
  python3 m4_gold_acceptance.py --policy all
  python3 m4_gold_acceptance.py --check-overlap    # guard only, no deps
  python3 m4_gold_acceptance.py --self-test        # guard/floor self-test

Exit codes: 0 green | 2 leak | 3 missing inputs (UNVALIDATED) |
            4 empty ruler | 5 degenerate (zero/non-finite) embeddings |
            6 unparseable ruler (malformed JSON/JSON5, or not UTF-8).
"""
import argparse
import hashlib
import json
import os
import re
import sys
import tempfile
import time
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import eval_harness as H  # noqa: E402

RESULTS = HERE / "results"
REPLAY = HERE / "replay-gold.local.json5"
MODEL_DIR = "/Volumes/LLMs/answerdotai/ModernBERT-base"
EMBED_URL = "http://127.0.0.1:8090/v1"

CHAIN = 0.868          # hard-coded chain floor (eval_harness.CHAIN_BASELINE)
MIN_ROUTED = 20        # coverage floor (F19): below this a verdict is noise
SIM_THRESHOLD = 0.95   # corpus<->replay leak threshold (F20)
TOP_MARGINS = 5        # margins printed beside the leak list

# Reruns go OUTSIDE the repo: nothing under tools/classifier-eval/results/
# is gitignored, so a stray untracked artifact there is repo churn.
REPO = HERE.parent.parent
MEEPT_HOME = Path(os.environ.get("MEEPT_HOME") or (Path.home() / ".meept"))


def _rerun_dir() -> Path:
    """Rerun location, guaranteed OUTSIDE the repo tree.

    The default is ``<MEEPT_HOME>/classifier-eval-rerun``. But MEEPT_HOME
    can itself point INSIDE the repo (a repo-local dev home), which would
    drop the rerun back into the tracked tree; in that case fall back to
    the system temp dir. (A repo-local rerun is acceptable only if the
    owner adds ``classifier-eval-rerun/`` to ``.gitignore`` -- the owner's
    call, not this script's.)
    """
    cand = MEEPT_HOME / "classifier-eval-rerun"
    try:
        inside = REPO == cand.resolve() or REPO in cand.resolve().parents
    except OSError:
        inside = False
    if inside:
        return Path(tempfile.gettempdir()) / "classifier-eval-rerun"
    return cand


RERUN_DIR = _rerun_dir()

EXIT_LEAK = 2
EXIT_UNVALIDATED = 3
EXIT_EMPTY_RULER = 4
EXIT_DEGENERATE = 5
EXIT_UNPARSEABLE = 6

# Recorded near-duplicate allowlist: replay case_key -> reason. An entry
# exempts that replay row from the SIMILARITY check only (byte-identity is
# always a leak). The 0.95 threshold has thin headroom -- the reviewer
# measured the known leak at 1.000 with the next rows at 0.943/0.938/0.923
# and six rows inside 0.90-0.95 -- so one more corpus anchor or an embedder
# upgrade could turn a non-leak into a hard refusal with no escape hatch.
# Any exemption added here MUST be argued in results/m4-gold-acceptance.md,
# never granted silently. Empty by default.
NEAR_DUP_ALLOWLIST: dict[str, str] = {}

ORCH = re.compile(
    r"(?i)\b(subagents?|tasks? \d|task list|waves?|leaves?|leaf \d|plan\.md|"
    r"handoff|checklist|in order|one at a time|sealed plan|tracking table|"
    r"as you (find|go)|, then\b|and correct them|and fix them|"
    r"without (asking|stopping)|no check-?ins?|just (do|make|apply)|"
    r"to completion|implement tasks?|work (through|items))\b")


class RulerUnparseableError(ValueError):
    """The ruler file EXISTS but is not parseable (zero-byte, truncated, or
    not UTF-8). Exit code 6: a broken FILE, deliberately distinct from exit 4
    (a valid file holding an empty list). Raised by ``load_replay`` so both
    entry points refuse with a documented code instead of dumping an
    uncaught ``json.JSONDecodeError`` traceback (exit 1)."""


def load_replay():
    """Return the adjudicated replay records, or None when the untracked
    replay corpus is absent (gitignored by design).

    Parsed with the SAME JSON5 parser as the corpus (H.parse_json5), not a
    key-order-sensitive regex. The old ``\\{\\s*"input"`` regex demanded
    ``input`` be the FIRST key and every key quoted, so a ruler written in
    the repo's own JSON5 style -- unquoted keys, or ``id`` before ``input``
    -- parsed to ZERO cases and tripped the empty-ruler refusal (exit 4)
    forever. Accepts the ``{ cases: [...] }`` layout the committed ruler
    uses, a ``{ categories: {...} }`` layout, or a bare list.

    Raises ``RulerUnparseableError`` when the file EXISTS but cannot be
    decoded or parsed -- a ZERO-BYTE ruler, a truncated document, or a
    non-UTF-8 file. That is exit 6, deliberately distinct from the empty
    ruler (exit 4): a broken file is not a ruler with nothing in it, and
    neither may surface as an uncaught JSONDecodeError traceback.
    """
    if not REPLAY.exists():
        return None
    try:
        raw = REPLAY.read_text()
    except (UnicodeDecodeError, OSError) as e:
        raise RulerUnparseableError(
            f"cannot read ruler {REPLAY}: {type(e).__name__}: {e}") from e
    try:
        data = H.parse_json5(raw)
    except (json.JSONDecodeError, ValueError) as e:
        raise RulerUnparseableError(
            f"ruler {REPLAY} is not parseable JSON5 "
            f"({type(e).__name__}: {e})") from e
    if isinstance(data, dict):
        if "cases" in data:
            return list(data["cases"] or [])
        if "input" in data:  # a single bare case object (one-line ruler)
            return [data]
        cats = data.get("categories") or {}
        return [e for entries in cats.values() for e in entries]
    return list(data)


def replay_sha256() -> str | None:
    """sha256 of the replay file BYTES: the artifact must pin the exact
    ruler revision it was measured against, not just cite a filename."""
    if not REPLAY.exists():
        return None
    return hashlib.sha256(REPLAY.read_bytes()).hexdigest()


def verdict_for(routed: int, routed_ok: int, nchain: int, n: int):
    """Score + verdict with the coverage floor. Never returns PASS/FAIL
    for a routed sample below MIN_ROUTED."""
    acc = (routed_ok + nchain * CHAIN) / n if n else 0.0
    if routed < MIN_ROUTED:
        return acc, "INSUFFICIENT_COVERAGE"
    return acc, ("PASS" if acc > CHAIN else "FAIL")


def write_artifact(policy: str, payload: dict) -> Path:
    """Write the per-policy acceptance artifact. Canonical name is
    write-once (F34); a re-run goes to RERUN_DIR (outside the repo) under a
    unique timestamped name, so it neither pollutes the tracked results
    dir nor clobbers the previous re-run."""
    RESULTS.mkdir(parents=True, exist_ok=True)
    out = RESULTS / f"m4-gold-acceptance-{policy}.json"
    if out.exists():
        RERUN_DIR.mkdir(parents=True, exist_ok=True)
        stamp = time.strftime("%Y%m%d-%H%M%S")
        out = RERUN_DIR / f"m4-gold-acceptance-{policy}.rerun-{stamp}.json"
    out.write_text(json.dumps(payload, indent=2) + "\n")
    return out


def unvalidated(policy: str, reason: str) -> None:
    print(f"[{policy}] UNVALIDATED: {reason}", file=sys.stderr)
    print(f"  artifact not written; see results/m4-gold-acceptance.md "
          f"for the UNVALIDATED record.", file=sys.stderr)


def check_overlap_only() -> int:
    """Disjointness check that needs no embed server: exact-match only."""
    cases_b, cases_a = H.load_cases()
    gold = cases_b + cases_a
    try:
        replay = load_replay()
    except RulerUnparseableError as e:
        print(f"REFUSING: {e}", file=sys.stderr)
        print("  a malformed ruler is exit 6, never an uncaught traceback; "
              "fix the ruler file (or remove it) before re-running.",
              file=sys.stderr)
        return EXIT_UNPARSEABLE
    if replay is None:
        print(f"replay corpus absent ({REPLAY.name}); cannot check "
              f"disjointness", file=sys.stderr)
        return EXIT_UNVALIDATED
    texts = [r["input"] for r in replay]
    if not texts:
        print(f"REFUSING: replay corpus {REPLAY} parsed to 0 cases -- an "
              f"empty ruler measures nothing and is never 'disjoint'. "
              f"Populate the replay or fix the parser.", file=sys.stderr)
        return EXIT_EMPTY_RULER
    guard: dict = {}
    try:
        leaks = H.replay_disjointness(texts, gold, sim_threshold=SIM_THRESHOLD,
                                      allowlist=NEAR_DUP_ALLOWLIST, stats=guard,
                                      top_n=TOP_MARGINS)
    except H.EmptyRulerError as e:
        print(f"REFUSING: {e}", file=sys.stderr)
        return EXIT_EMPTY_RULER
    except H.DegenerateVectorError as e:
        print(f"REFUSING: {e}", file=sys.stderr)
        return EXIT_DEGENERATE
    if leaks:
        print(H.format_leaks(leaks))
        print(H.format_margins(guard))
        print("REFUSING: the ruler is not disjoint from the fitting corpus "
              "(train-on-test). Remove the offending corpus row or replay "
              "case before scoring.", file=sys.stderr)
        return EXIT_LEAK
    print(f"ruler disjoint: {len(texts)} replay cases vs "
          f"{len(gold)} corpus cases (exact-match + "
          f"sim>{SIM_THRESHOLD} when an embed server is supplied)")
    print(H.format_margins(guard))
    return 0


def run_scoring_guard(s_texts, gold, *, corpus_vectors=None,
                      replay_vectors=None):
    """GUARD 1: corpus<->replay disjointness, BEFORE any scoring.

    Shared by ``run_full`` (as the exact-match pre-flight and again with
    vectors) and ``--self-test`` so the refusal path is exercised by the
    self-test and cannot be ripped out of the run without the self-test
    failing. The pin is BEHAVIOURAL: the self-test calls this through
    ``run_full`` with a leaked ruler and asserts the run refuses (exit 2) and
    writes no payload -- a neutered call site fails that assertion, where the
    old source grep could be satisfied by dead code. Returns ``(exit_code,
    guard_stats, leaks)``; ``exit_code`` is None when the ruler is disjoint.
    Also returns the leak RECORDS, which carry the replay ``case_key`` and
    never the ruler text (privacy).
    """
    guard: dict = {}
    try:
        leaks = H.replay_disjointness(
            s_texts, gold, corpus_vectors=corpus_vectors,
            replay_vectors=replay_vectors, sim_threshold=SIM_THRESHOLD,
            allowlist=NEAR_DUP_ALLOWLIST, stats=guard, top_n=TOP_MARGINS)
    except H.DegenerateVectorError as e:
        print(f"REFUSING to score: {e}", file=sys.stderr)
        return EXIT_DEGENERATE, guard, None
    except H.EmptyRulerError as e:
        print(f"REFUSING to score: {e}", file=sys.stderr)
        return EXIT_EMPTY_RULER, guard, None
    if leaks:
        print(H.format_leaks(leaks), file=sys.stderr)
        print(H.format_margins(guard), file=sys.stderr)
        print("REFUSING to score: the ruler is not disjoint from the "
              "fitting corpus (train-on-test). Fix the corpus/replay "
              "before re-running.", file=sys.stderr)
        return EXIT_LEAK, guard, leaks
    return None, guard, leaks


def build_centroids(V, intents, labs):
    """Per-label centroid rows of the fitting corpus, L2-normalised.

    Normalisation goes through ``H._as_unit``: ``C /= (norm + 1e-12)`` would
    turn a zero-norm centroid row (cancelling unit vectors) into an all-zero
    vector whose cosine is 0.0 and silently reads as "disjoint" -- the F20
    hole ``_as_unit`` closes. Extracted so the self-test can drive the EXACT
    call site ``run_full`` uses: reverting this to norm+1e-12 must fail the
    self-test, which a source grep could not see.
    """
    import numpy as np
    C = np.stack([V[[i for i in range(len(intents)) if intents[i] == lab]]
                  .mean(axis=0) for lab in labs])
    return H._as_unit(C)


def run_full(policies: set) -> int:
    # Input guards run BEFORE the heavy ML imports: an absent, EMPTY or
    # MALFORMED ruler must be refused without requiring torch/transformers/:8090.
    try:
        replay = load_replay()
    except RulerUnparseableError as e:
        print(f"REFUSING: {e}", file=sys.stderr)
        print("  a malformed ruler is exit 6, never an uncaught traceback.",
              file=sys.stderr)
        return EXIT_UNPARSEABLE
    if replay is None:
        unvalidated("all", f"untracked replay corpus missing ({REPLAY})")
        return EXIT_UNVALIDATED
    if not replay:
        print(f"REFUSING: replay corpus {REPLAY} parsed to 0 cases -- an "
              f"empty ruler measures nothing; no policy can be scored.",
              file=sys.stderr)
        return EXIT_EMPTY_RULER

    s_texts = [r["input"] for r in replay]
    # GUARD 1 PRE-FLIGHT (exact-match only): needs no vectors and no ML
    # stack, so a leaked ruler refuses HERE -- before torch/:8090 are
    # required. This is also the seam the self-test drives to assert the
    # guard's EFFECT in run_full's own control flow (a leaked ruler refuses
    # to score and writes NO payload), rather than grep-ing for the call.
    _gb, _ga = H.load_cases()
    preflight_rc, _pguard, _pleaks = run_scoring_guard(s_texts, _gb + _ga)
    if preflight_rc is not None:
        return preflight_rc

    import numpy as np
    import torch
    from transformers import AutoModel, AutoTokenizer

    s_true = [r["expected_intent"] for r in replay]
    replay_sha = replay_sha256()

    torch.manual_seed(42)
    tok = AutoTokenizer.from_pretrained(MODEL_DIR)
    base = AutoModel.from_pretrained(MODEL_DIR).to("cpu").eval()
    hidden = base.config.hidden_size

    def mb_embed(texts, batch=16):
        out = []
        with torch.no_grad():
            for i in range(0, len(texts), batch):
                enc = tok(texts[i:i + batch], padding=True, truncation=True,
                          max_length=256, return_tensors="pt")
                o = base(**enc).last_hidden_state
                m = enc["attention_mask"].unsqueeze(-1).float()
                v = (o * m).sum(1) / m.sum(1).clamp(min=1e-9)
                out.extend(x.cpu().numpy().astype(np.float32)
                           for x in torch.nn.functional.normalize(v, dim=-1))
        return out

    # gold corpus -> fit both stages
    cases_b, cases_a = H.load_cases()
    gold = cases_b + cases_a
    gkeys = [H.case_key(c.text) for c in gold]
    intents = [c.intent for c in gold]
    labs = sorted({c.intent for c in gold if not c.ood})

    emb = H.Embedder(EMBED_URL, "qwen3-embedding", "")
    emb.embed_keys([c.text for c in gold], gkeys)
    V = emb.vectors(gkeys)
    # Guarded normalisation, in one driven seam (build_centroids): see its
    # docstring -- a zero-norm centroid row must fail loud, not read as
    # "disjoint" (F20).
    try:
        C = build_centroids(V, intents, labs)
    except H.DegenerateVectorError as e:
        print(f"REFUSING to score: degenerate centroid row: {e}",
              file=sys.stderr)
        return EXIT_DEGENERATE
    yg = np.array([labs.index(c.intent) if not c.ood else -1 for c in gold])

    MB_G = np.stack(mb_embed([c.text for c in gold]))
    mask = yg >= 0
    counts = np.bincount(yg[mask], minlength=len(labs))
    w = torch.tensor(len(yg[mask]) / (len(labs) * np.maximum(counts, 1)),
                     dtype=torch.float32)
    head = torch.nn.Linear(hidden, len(labs))
    opt = torch.optim.AdamW(head.parameters(), lr=1e-3, weight_decay=1e-2)
    lossf = torch.nn.CrossEntropyLoss(weight=w)
    Xt = torch.tensor(MB_G[mask])
    yt = torch.tensor(yg[mask], dtype=torch.long)
    head.train()
    for _ep in range(30):
        perm = torch.randperm(len(Xt))
        for i in range(0, len(perm), 16):
            idx = perm[i:i + 16]
            opt.zero_grad()
            lossf(head(Xt[idx]), yt[idx]).backward()
            opt.step()
    head.eval()

    skeys = [H.case_key(t) for t in s_texts]
    emb.embed_keys(s_texts, skeys)
    SV = emb.vectors(skeys)
    MB_S = np.stack(mb_embed(s_texts))

    # --- GUARD 1: corpus<->replay disjointness, BEFORE scoring ---------
    guard_rc, guard, leaks = run_scoring_guard(
        s_texts, gold, corpus_vectors=V, replay_vectors=SV)
    if guard_rc is not None:
        return guard_rc
    # margins beside the (empty) leak list: the threshold headroom is the
    # earliest-warning signal that a corpus anchor or embedder upgrade is
    # about to turn a non-leak into a refusal.
    print(H.format_margins(guard), file=sys.stderr)

    # every written payload carries the guard evidence that produced it.
    common = {
        "policy": None,                        # set per policy below
        "replay_n": len(s_texts),
        "replay_sha256": replay_sha,
        "sim_threshold": SIM_THRESHOLD,
        "min_routed": MIN_ROUTED,
        "coverage_floor": MIN_ROUTED,
        "leaks": leaks,                        # [] on a scored run (exit 2 otherwise)
        "guard": guard,
        "near_dup_allowlist": sorted(NEAR_DUP_ALLOWLIST),
        "chain_baseline": CHAIN,
    }

    def centroid_routes(qv, text):
        sims = C @ qv
        order = np.argsort(-sims)
        top, second = float(sims[order[0]]), float(sims[order[1]])
        lab = labs[int(order[0])]
        routed = top >= 0.60 and (top - second) >= 0.030
        if routed and lab == "quickplan" and not ORCH.search(text):
            routed = False
        return routed, lab, top - second

    # --- policy: double-confidence -------------------------------------
    def run_double_confidence():
        a = aok = b = bok = nchain = 0
        for i, true in enumerate(s_true):
            routed, lab, margin = centroid_routes(SV[i], s_texts[i])
            if routed:
                a += 1
                aok += lab == true
                continue
            with torch.no_grad():
                p = torch.softmax(head(torch.tensor(MB_S[i][None])), dim=-1)[0]
            li = int(np.argmax(p))
            if float(p.max()) >= 0.70 and labs[li] == lab:
                b += 1
                bok += labs[li] == true
                continue
            nchain += 1
        acc, verdict = verdict_for(a + b, aok + bok, nchain, len(s_texts))
        return {"n": len(s_texts), "n_scored": len(s_texts),
                "a_routes": a, "a_correct": aok,
                "b_routes": b, "b_correct": bok, "chain": nchain,
                "routed": a + b, "min_routed": MIN_ROUTED,
                "coverage_floor": MIN_ROUTED,
                "system_accuracy": round(acc, 4),
                "acceptance_threshold": CHAIN, "verdict": verdict}

    # --- policy: tfidf-veto (ALT-5) ------------------------------------
    def run_tfidf_veto():
        try:
            from sklearn.feature_extraction.text import TfidfVectorizer
            from sklearn.linear_model import LogisticRegression
            from sklearn.pipeline import make_pipeline
        except ImportError as e:
            unvalidated("tfidf-veto", f"scikit-learn unavailable: {e}")
            return None
        gtexts = [gold[i].text for i in range(len(gold)) if mask[i]]
        glabels = [intents[i] for i in range(len(gold)) if mask[i]]
        clf = make_pipeline(
            TfidfVectorizer(analyzer="char_wb", ngram_range=(2, 4), min_df=1,
                            sublinear_tf=True),
            LogisticRegression(max_iter=2000, C=10.0),
        )
        clf.fit(gtexts, glabels)
        classes = list(clf.classes_)
        probs = clf.predict_proba(s_texts)
        a = aok = nchain = 0
        for i, true in enumerate(s_true):
            routed, lab, _margin = centroid_routes(SV[i], s_texts[i])
            if routed and classes[int(np.argmax(probs[i]))] == lab:
                a += 1
                aok += lab == true
                continue
            nchain += 1
        acc, verdict = verdict_for(a, aok, nchain, len(s_texts))
        return {"n": len(s_texts), "n_scored": len(s_texts),
                "a_routes": a, "a_correct": aok, "chain": nchain,
                "routed": a, "min_routed": MIN_ROUTED,
                "coverage_floor": MIN_ROUTED,
                "system_accuracy": round(acc, 4),
                "acceptance_threshold": CHAIN, "verdict": verdict}

    rc = 0
    if "double-confidence" in policies:
        payload = run_double_confidence()
        payload.update(common, policy="double-confidence")
        path = write_artifact("double-confidence", payload)
        print(f"double-confidence: acc={payload['system_accuracy']:.4f} "
              f"verdict={payload['verdict']} routed={payload['routed']} "
              f"(floor {MIN_ROUTED}) -> {path}")
    if "tfidf-veto" in policies:
        payload = run_tfidf_veto()
        if payload is None:
            rc = EXIT_UNVALIDATED
        else:
            payload.update(common, policy="tfidf-veto")
            path = write_artifact("tfidf-veto", payload)
            print(f"tfidf-veto: acc={payload['system_accuracy']:.4f} "
                  f"verdict={payload['verdict']} routed={payload['routed']} "
                  f"(floor {MIN_ROUTED}) -> {path}")
    return rc


def self_test() -> int:
    """Self-test for the guards and the coverage floor. Pure-stdlib +
    numpy; needs no embed server and no replay corpus. Pins:
      - the known train-on-test leak IS reported (exact case_key);
      - an EMPTY ruler is NOT green (EmptyRulerError + exit 4);
      - a ZERO-NORM embedding row is NOT green (DegenerateVectorError);
      - the recorded allowlist exempts similarity only, never byte-identity;
      - the coverage floor boundary holds at 19 vs 20 routed cases.
    """
    import numpy as np

    failures: list[str] = []

    def check(name, cond, detail=""):
        if cond:
            print(f"  ok   {name}")
        else:
            failures.append(name)
            print(f"  FAIL {name} {detail}")

    cases_b, cases_a = H.load_cases()
    gold = cases_b + cases_a
    known = "implement the plan using subagents"

    print("self-test: guards + coverage floor")

    # 1. the known leak is reported.
    leaks = H.replay_disjointness([known], gold)
    check("known leak reported (exact)",
          len(leaks) == 1 and leaks[0]["reason"] == "exact"
          and leaks[0]["corpus_case_id"] == "h18-planexec-001",
          f"-> {leaks}")

    # 2. an empty ruler is not green.
    empty_raised = False
    try:
        H.replay_disjointness([], gold)
    except H.EmptyRulerError:
        empty_raised = True
    check("empty ruler raises EmptyRulerError", empty_raised)

    d = Path(tempfile.mkdtemp())
    p = d / "replay-gold.local.json5"
    p.write_text("{ cases: [] }\n")
    saved = REPLAY

    def quiet_check(path):
        """check_overlap_only prints; keep the self-test output readable."""
        import contextlib
        import io
        with contextlib.redirect_stdout(io.StringIO()), \
                contextlib.redirect_stderr(io.StringIO()):
            try:
                globals()["REPLAY"] = path
                return check_overlap_only()
            finally:
                globals()["REPLAY"] = saved

    empty_code = quiet_check(p)
    check("check_overlap_only refuses an empty sample",
          empty_code == EXIT_EMPTY_RULER, f"-> exit {empty_code}")

    # 2b. a non-empty clean ruler IS green (the guard is not trivially red).
    q = d / "clean.json5"
    q.write_text('{ "input": "xq7-unique-selftest-row-not-in-corpus", '
                 '"expected_intent": "chat" }\n')
    clean_code = quiet_check(q)
    check("check_overlap_only green on a clean sample",
          clean_code == 0, f"-> exit {clean_code}")

    # 2c. a MALFORMED ruler (zero-byte / truncated) refuses with the
    #     documented exit 6, never an uncaught JSONDecodeError traceback.
    z = d / "zero.json5"
    z.write_text("")
    tr = d / "trunc.json5"
    tr.write_text('{ "cases": [ { "input": "x"')
    for tag, path in (("zero-byte", z), ("truncated", tr)):
        code = quiet_check(path)
        check(f"malformed ruler ({tag}) -> exit {EXIT_UNPARSEABLE}",
              code == EXIT_UNPARSEABLE, f"-> exit {code}")

    # 3. a zero-norm embedding row is not green.
    V = np.ones((len(gold), 4), dtype=np.float32)
    Z = np.zeros((1, 4), dtype=np.float32)
    deg_raised = False
    try:
        H.replay_disjointness(["zzz selftest zero-norm"], gold,
                              corpus_vectors=V, replay_vectors=Z)
    except H.DegenerateVectorError:
        deg_raised = True
    check("zero-norm row raises DegenerateVectorError", deg_raised)

    # 3b. a NON-FINITE row is not green either.
    N = np.full((1, 4), np.inf, dtype=np.float32)
    nonfin_raised = False
    try:
        H.replay_disjointness(["zzz selftest inf-norm"], gold,
                              corpus_vectors=V, replay_vectors=N)
    except H.DegenerateVectorError:
        nonfin_raised = True
    check("non-finite row raises DegenerateVectorError", nonfin_raised)

    # 4. a cosine leak IS reported, and the allowlist exempts similarity
    #    only (byte-identity stays a leak). "a" vs "b" differ in text but
    #    share a vector, so the sim must fire and exact must not.
    corpus_v = np.stack([np.array([1.0, 0, 0, 0], dtype=np.float32),
                         np.array([0, 1.0, 0, 0], dtype=np.float32)])
    replay_v = np.array([[1.0, 0, 0, 0]], dtype=np.float32)
    mini = gold[:2]
    sim_leaks = H.replay_disjointness(["selftest near-dup text"], mini,
                                      corpus_vectors=corpus_v,
                                      replay_vectors=replay_v,
                                      sim_threshold=0.95)
    check("similarity leak reported",
          len(sim_leaks) == 1 and "similarity" in sim_leaks[0]["reason"],
          f"-> {sim_leaks}")
    allowed = H.replay_disjointness(
        ["selftest near-dup text"], mini, corpus_vectors=corpus_v,
        replay_vectors=replay_v, sim_threshold=0.95,
        allowlist={H.case_key("selftest near-dup text"): "self-test reason"})
    check("allowlist exempts similarity", allowed == [], f"-> {allowed}")
    exact_still = H.replay_disjointness(
        [mini[0].text], mini, corpus_vectors=corpus_v, replay_vectors=replay_v,
        sim_threshold=0.95,
        allowlist={H.case_key(mini[0].text): "self-test reason"})
    check("allowlist does NOT exempt byte-identity",
          any(lk["reason"] == "exact" for lk in exact_still), f"-> {exact_still}")

    # 5. coverage floor boundary: 19 routed is INSUFFICIENT, 20 is a verdict.
    acc19, v19 = verdict_for(19, 19, 29, 48)
    acc20, v20 = verdict_for(20, 20, 28, 48)
    check("19 routed -> INSUFFICIENT_COVERAGE",
          v19 == "INSUFFICIENT_COVERAGE", f"-> {v19}")
    check("20 routed -> PASS/FAIL (floor boundary)",
          v20 in ("PASS", "FAIL"), f"-> {v20}")
    # the committed 48-case numbers must be sub-floor for BOTH policies.
    check("committed double-confidence (7 routed) is sub-floor",
          verdict_for(7, 5, 41, 48)[1] == "INSUFFICIENT_COVERAGE")
    check("committed tfidf-veto (2 routed) is sub-floor",
          verdict_for(2, 2, 46, 48)[1] == "INSUFFICIENT_COVERAGE")

    # 6. GUARD-1 WIRING, BEHAVIOURAL. The guard's EFFECT must be pinned in
    #    run_full's own control flow: a leaked ruler refuses to score and NO
    #    payload is written. The old source grep
    #    (`"run_scoring_guard(" in body`) was satisfied by `if False:`
    #    wrapped around the call, so the guard could be neutered while every
    #    check stayed green.
    import contextlib
    import io
    with contextlib.redirect_stdout(io.StringIO()), \
            contextlib.redirect_stderr(io.StringIO()):
        guard_rc, _gstats, _gleaks = run_scoring_guard([known], gold)
    check("run GUARD-1 refuses a leaked ruler (exit 2)",
          guard_rc == EXIT_LEAK, f"-> exit {guard_rc}")

    # 6b. run_full itself refuses a leaking ruler BEFORE the ML stack and
    #     writes NOTHING. torch/transformers are BLOCKED (None in sys.modules
    #     makes `import torch` raise) so the check is load-bearing: if the
    #     pre-flight call site is neutered the run falls through to the
    #     import and raises, instead of being rescued by the vector guard
    #     after the embeddings.
    leak_dir = Path(tempfile.mkdtemp())
    leak_ruler = leak_dir / "replay-gold.local.json5"
    leak_ruler.write_text(
        '{ cases: [ { input: "%s", expected_intent: "quickplan" } ] }\n'
        % known)
    _art_before = sorted(p.name for p in RESULTS.glob("*"))
    _saved_replay = REPLAY
    _blocked = {m: sys.modules.get(m) for m in ("torch", "transformers")}
    for m in _blocked:
        sys.modules[m] = None  # type: ignore[assignment]
    globals()["REPLAY"] = leak_ruler
    try:
        with contextlib.redirect_stdout(io.StringIO()), \
                contextlib.redirect_stderr(io.StringIO()):
            run_rc = run_full({"double-confidence"})
    except Exception as e:  # a neutered pre-flight falls through to the import
        run_rc = f"raised {type(e).__name__}"
    finally:
        globals()["REPLAY"] = _saved_replay
        for m, mod in _blocked.items():
            if mod is None:
                sys.modules.pop(m, None)
            else:
                sys.modules[m] = mod
    check("run_full refuses a leaked ruler without the ML stack (no scoring)",
          run_rc == EXIT_LEAK, f"-> {run_rc}")
    check("run_full writes no payload on a leak refusal",
          sorted(p.name for p in RESULTS.glob("*")) == _art_before,
          "-> an artifact appeared on the refusal path")

    # 6c. a DEGENERATE row refuses too (the vector guard's effect at the
    #     same seam run_full uses).
    with contextlib.redirect_stdout(io.StringIO()), \
            contextlib.redirect_stderr(io.StringIO()):
        deg_rc, _s2, _l2 = run_scoring_guard(
            ["zzz selftest deg-run-full"], gold,
            corpus_vectors=np.ones((len(gold), 4), dtype=np.float32),
            replay_vectors=np.zeros((1, 4), dtype=np.float32))
    check("run GUARD-1 refuses a degenerate row (exit 5)",
          deg_rc == EXIT_DEGENERATE, f"-> exit {deg_rc}")

    # 6d. BOTH _as_unit call sites: a cancelling centroid row must RAISE
    #     rather than normalise to an all-zero vector via norm+1e-12 (the
    #     mutation the source grep could not see).
    _full = np.array([[1.0, 0, 0, 0], [-1.0, 0, 0, 0]], dtype=np.float32)
    cen_raised = False
    try:
        build_centroids(_full, ["a", "a"], ["a"])
    except H.DegenerateVectorError:
        cen_raised = True
    check("build_centroids refuses a cancelling centroid", cen_raised)
    _saved_full = H.__dict__.get("_FULL_V")
    H.__dict__["_FULL_V"] = _full
    gate_raised = False
    try:
        H.CentroidGate(0.70).fit(np.array([0, 1]), ["a", "a"])
    except H.DegenerateVectorError:
        gate_raised = True
    finally:
        H.__dict__["_FULL_V"] = _saved_full
    check("CentroidGate.fit refuses a cancelling centroid", gate_raised)

    # 7. PRIVACY, BOTH DIRECTIONS. (a) The stats run_full copies into the
    #    artifact AND the leak records themselves must not carry ruler text;
    #    (b) the filter must not over-redact -- a leak must still be reported
    #    and identified by case_key, and format_leaks must be printable
    #    without leaking (it reaches stdout AND stderr).
    with contextlib.redirect_stdout(io.StringIO()), \
            contextlib.redirect_stderr(io.StringIO()):
        _one = np.array([[1.0, 0, 0, 0]], dtype=np.float32)
        _stats: dict = {}
        _leaks = H.replay_disjointness(["zsecret-guard-text-z"], gold[:1],
                                       corpus_vectors=_one,
                                       replay_vectors=_one,
                                       sim_threshold=0.95, stats=_stats)
    _blob = json.dumps(_stats) + json.dumps(_leaks)
    check("guard stats + leak records never contain ruler text",
          "zsecret-guard-text-z" not in _blob, f"-> {_leaks}")
    check("guard margin row carries the replay case_key",
          isinstance(_stats.get("top_margins", [None])[0][3], str)
          and _stats["top_margins"][0][3] == H.case_key("zsecret-guard-text-z"),
          f"-> {_stats.get('top_margins')}")
    check("similarity leak record still reported and keyed (no over-redact)",
          len(_leaks) == 1
          and _leaks[0].get("replay_case_key") == H.case_key("zsecret-guard-text-z")
          and "replay_text" not in _leaks[0], f"-> {_leaks}")
    _exact_leaks = H.replay_disjointness([known], gold)
    check("exact leak record carries no ruler text either",
          len(_exact_leaks) == 1 and known not in json.dumps(_exact_leaks)
          and _exact_leaks[0].get("replay_case_key") == H.case_key(known),
          f"-> {_exact_leaks}")
    _fmt = H.format_leaks(_exact_leaks) + H.format_leaks(_leaks)
    check("format_leaks prints keys, never the ruler text",
          known not in _fmt and "zsecret-guard-text-z" not in _fmt
          and H.case_key(known) in _fmt, f"-> {_fmt}")

    # ------------------------------------------------------------------
    # 8. SCORING INTEGRITY (leaf 02 / MEAS-04 + MEAS-08): the REAL
    #    run_permutation, driven with an in-memory embedder and
    #    constant/scripted predictors over synthetic in-memory cases --
    #    no embed server, no disk cache, no files written. Pins:
    #      - every held-out case (in-domain AND OOD) reaches head.decide;
    #      - OOD gold labels never enter training (they only grade);
    #      - the C2 count definitions hold, including the recorded hand
    #        calculation (1 correct in-domain route + 1 wrong OOD route
    #        => total=1 direct=1 correct=1 wrong=1 OOD_R=0, modeled
    #        in-domain E2E stays 1, and the wrong-route penalty fires);
    #      - zero-denominator ratios are None (JSON null, rendered "n/a"),
    #        never a fabricated 0.0 or 100% (recorded decision 2026-09-17);
    #      - E2E keeps the 0.868 chain coefficient as MODELED in-domain
    #        accuracy and is unchanged by OOD errors when the in-domain
    #        predictions are identical.
    print("self-test: scoring integrity (real run_permutation, scripted heads)")
    _saved_env = {k: H.__dict__.get(k)
                  for k in ("ALLVEC", "self_mask", "_FULL_V", "build_head")}

    def _restore_env():
        for _k, _v in _saved_env.items():
            if _v is None:
                H.__dict__.pop(_k, None)
            else:
                H.__dict__[_k] = _v

    def close(a, b, tol=1e-9):
        return a is not None and b is not None and abs(a - b) <= tol

    def mk_case(i: int, ood: bool = False) -> H.Case:
        text = f"leaf02 selftest synthetic {'ood' if ood else 'in'}-{i}"
        return H.Case(text, "OOD" if ood else "code", "", ood,
                      "selftest", "", f"leaf02-{i}")

    class MemEmbedder(H.Embedder):
        """In-memory embedder: no disk cache, no network, no latency."""

        def __init__(self, vecs: dict):
            self.mem = dict(vecs)
            self.latency_ms = []

        def embed_keys(self, texts, keys):
            pass

        def vectors(self, keys):
            return np.stack([np.asarray(self.mem[k], dtype=np.float32)
                             for k in keys])

    def vecs_for(cases) -> dict:
        eye = np.eye(max(len(cases), 1), dtype=np.float32)
        return {H.case_key(c.text): eye[i] for i, c in enumerate(cases)}

    def scripted_run(spec_name, cases, folds, verdicts):
        """Drive the real run_permutation with a scripted constant head.
        Returns (metrics, decide-call count, labels seen by fit)."""
        calls = {"n": 0}
        train: list = []

        class ScriptedHead:
            def fit(self, idx, intents):
                train.extend(intents)

            def decide(self, q, intents):
                calls["n"] += 1
                v = verdicts[min(calls["n"] - 1, len(verdicts) - 1)]
                return (v, 1.0) if v is not None else (None, 1.0)

        H.build_head = lambda spec, emb: ScriptedHead()
        try:
            m = H.run_permutation(
                {"name": spec_name, "head": {"type": "scripted"}},
                cases, folds, MemEmbedder(vecs_for(cases)))
        finally:
            pass  # globals restored once by the outer finally
        return m, calls["n"], train

    try:
        # --- recorded hand calculation: one in-domain + one OOD case,
        #     always-code predictor. Two prediction calls, OOD_abstain=0,
        #     wrong=1; total/direct/correct=1; E2E=1; penalty fires.
        in1, ood1 = mk_case(101), mk_case(102, ood=True)
        folds2 = {H.case_key(in1.text): 0, H.case_key(ood1.text): 1}
        m, calls, train = scripted_run("leaf02-hand-calc", [in1, ood1],
                                       folds2, ["code"])
        check("hand-calc: BOTH held-out cases reach head.decide (2 calls)",
              calls == 2, f"-> {calls} calls")
        check("hand-calc: OOD gold label never entered training",
              "OOD" not in train, f"-> trained on {train}")
        check("hand-calc: total=1 direct=1 correct=1",
              (m["total"], m["direct"], m["correct"]) == (1, 1, 1),
              f"-> {m['total']}/{m['direct']}/{m['correct']}")
        check("hand-calc: wrong=1 (the routed OOD case)",
              m["wrong"] == 1, f"-> {m['wrong']}")
        check("hand-calc: OOD_total=1 OOD_abstain=0 OOD_R=0 (measured)",
              (m["OOD_total"], m["OOD_abstain"]) == (1, 0)
              and close(m["OOD_R"], 0.0),
              f"-> {m['OOD_total']}/{m['OOD_abstain']}/{m['OOD_R']}")
        check("hand-calc: modeled in-domain E2E stays 1",
              close(m["E2E"], 1.0), f"-> {m['E2E']}")
        check("hand-calc: wrong-route penalty fires (SCORE=-4)",
              close(m["SCORE"], -4.0), f"-> {m['SCORE']}")
        check("hand-calc: C=1 P=1 A=0.5",
              close(m["C"], 1.0) and close(m["P"], 1.0)
              and close(m["A"], 0.5),
              f"-> {m['C']}/{m['P']}/{m['A']}")
        check("hand-calc: the wrong OOD route lands in confusion evidence",
              any(c.case_id == ood1.case_id for c, _, _ in m["confusion"]),
              f"-> {[c.case_id for c, _, _ in m['confusion']]}")

        # --- controls on a 6-case fixture: 5 in-domain (one per fold, so
        #     every test fold keeps 4 training cases) + 1 OOD (fold 0).
        in5 = [mk_case(200 + i) for i in range(5)]
        ood6 = mk_case(299, ood=True)
        six = in5 + [ood6]
        folds6 = {**{H.case_key(c.text): i for i, c in enumerate(in5)},
                  H.case_key(ood6.text): 0}
        folds5 = {H.case_key(c.text): i for i, c in enumerate(in5)}

        m, calls, train = scripted_run("always-route", six, folds6, ["code"])
        check("always-route: all 6 held-out cases predicted", calls == 6,
              f"-> {calls}")
        check("always-route: OOD excluded from training", "OOD" not in train)
        check("always-route: routed OOD counts as wrong", m["wrong"] == 1,
              f"-> {m['wrong']}")
        check("always-route: OOD_R measured 0 (not assumed)", close(m["OOD_R"], 0.0),
              f"-> {m['OOD_R']}")
        check("always-route: E2E=1, SCORE=0 (penalty -5*1/5 fires)",
              close(m["E2E"], 1.0) and close(m["SCORE"], 0.0),
              f"-> {m['E2E']}/{m['SCORE']}")
        ref_indomain = (m["total"], m["direct"], m["correct"], m["E2E"])

        m, _c, _t = scripted_run("always-abstain", six, folds6, [None])
        check("always-abstain: direct=0 correct=0 wrong=0",
              (m["direct"], m["correct"], m["wrong"]) == (0, 0, 0),
              f"-> {m['direct']}/{m['correct']}/{m['wrong']}")
        check("always-abstain: OOD_abstain=1, OOD_R=1 MEASURED (head said no)",
              (m["OOD_abstain"],) == (1,) and close(m["OOD_R"], 1.0),
              f"-> {m['OOD_abstain']}/{m['OOD_R']}")
        check("always-abstain: undefined P and A are null, not 0.0",
              m["P"] is None and m["A"] is None,
              f"-> {m['P']}/{m['A']}")
        check("always-abstain: C=0 is defined; E2E=0.868 modeled credit",
              close(m["C"], 0.0) and close(m["E2E"], H.CHAIN_BASELINE),
              f"-> {m['C']}/{m['E2E']}")
        check("always-abstain: SCORE keeps the 0-contribution convention",
              close(m["SCORE"], 0.0), f"-> {m['SCORE']}")

        m, _c, _t = scripted_run("wrong-in-domain", six, folds6, ["chat"])
        check("wrong-in-domain: direct=5 correct=0 wrong=6 (5 + 1 OOD)",
              (m["direct"], m["correct"], m["wrong"]) == (5, 0, 6),
              f"-> {m['direct']}/{m['correct']}/{m['wrong']}")
        check("wrong-in-domain: P=0, A=0, E2E=0 (no abstention credit)",
              close(m["P"], 0.0) and close(m["A"], 0.0) and close(m["E2E"], 0.0),
              f"-> {m['P']}/{m['A']}/{m['E2E']}")
        check("wrong-in-domain: SCORE=-6 (penalty 5*6/5)",
              close(m["SCORE"], -6.0), f"-> {m['SCORE']}")

        # mixed script: call order follows case order per fold
        # (in0, ood, in1, in2, in3, in4); route calls 0/2/3, abstain 1/4/5.
        m, calls, _t = scripted_run(
            "mixed-domain", six, folds6,
            ["code", None, "code", "code", None, None])
        check("mixed-domain: 6 prediction calls", calls == 6, f"-> {calls}")
        check("mixed-domain: direct=3 correct=3 wrong=0",
              (m["direct"], m["correct"], m["wrong"]) == (3, 3, 0),
              f"-> {m['direct']}/{m['correct']}/{m['wrong']}")
        check("mixed-domain: OOD abstention measured (OOD_R=1)",
              (m["OOD_abstain"],) == (1,) and close(m["OOD_R"], 1.0),
              f"-> {m['OOD_abstain']}/{m['OOD_R']}")
        check("mixed-domain: C=0.6 P=1 A=1",
              close(m["C"], 0.6) and close(m["P"], 1.0) and close(m["A"], 1.0),
              f"-> {m['C']}/{m['P']}/{m['A']}")
        check("mixed-domain: E2E=(3 + 0.868*2)/5 (modeled, deterministic)",
              close(m["E2E"], (3 + 2 * H.CHAIN_BASELINE) / 5), f"-> {m['E2E']}")

        # --- zero-denominator representation (recorded decision
        #     2026-09-17): undefined ratios are null, never fabricated.
        m, _c, _t = scripted_run("no-ood", in5, folds5, ["code"])
        check("no-ood: OOD_total=0", m["OOD_total"] == 0, f"-> {m['OOD_total']}")
        check("no-ood: OOD_R is null (never a fabricated 100% abstention)",
              m["OOD_R"] is None, f"-> {m['OOD_R']!r}")
        check("no-ood: in-domain metrics identical to the mixed corpus "
              "(OOD errors do not corrupt in-domain-only ratios)",
              (m["total"], m["direct"], m["correct"]) == ref_indomain[:3]
              and close(m["E2E"], ref_indomain[3]),
              f"-> {m['total']}/{m['direct']}/{m['correct']}/{m['E2E']} "
              f"vs {ref_indomain}")
        _row = H.fmt_row(m)
        check("fmt_row renders a null OOD_R as n/a",
              _row.split("OOD-R=")[1].split()[0] == "n/a", f"-> {_row}")

        m, _c, _t = scripted_run("ood-only", [ood6],
                                 {H.case_key(ood6.text): 0}, ["code"])
        check("ood-only: total=0 -> C, E2E and SCORE are null "
              "(no unsupported claims on an empty denominator)",
              m["total"] == 0 and m["C"] is None and m["E2E"] is None
              and m["SCORE"] is None,
              f"-> {m['total']}/{m['C']}/{m['E2E']}/{m['SCORE']}")
        check("ood-only: wrong=1 and OOD_R=0 still measured",
              m["wrong"] == 1 and close(m["OOD_R"], 0.0),
              f"-> {m['wrong']}/{m['OOD_R']}")
        _row = H.fmt_row(m)
        check("fmt_row renders a total=0 row as n/a without raising",
              "n/a" in _row and "C=" in _row, f"-> {_row}")

        m, _c, _t = scripted_run("ood-only-abstain", [ood6],
                                 {H.case_key(ood6.text): 0}, [None])
        check("no-label fixture: vacuous macro-F1 is null, not 0.0",
              m["F1"] is None, f"-> {m['F1']!r}")
        _row = H.fmt_row(m)
        check("fmt_row renders null F1 as n/a",
              _row.split("F1=")[1].split()[0] == "n/a", f"-> {_row}")

        # --- Task 3 (MEAS-08): cascade expected-count formatting. The
        #     historical iter16 print used `:2d` on the WRONG count, which
        #     is an EXPECTED (fractional) number because stage-C credit is
        #     CHAIN_BASELINE per chain case; one chain case => ValueError.
        _frac = 106 - (100 + 6 * H.CHAIN_BASELINE)  # fractional by design
        try:
            f"wrong={_frac:2d}"
            _d2d_failed = False
        except ValueError:
            _d2d_failed = True
        check(":2d on a fractional wrong count raises ValueError (bug "
              "reproduced against the live interpreter)",
              _d2d_failed, f"-> wrong={_frac}")
        _crow = H.fmt_cascade_row(0.30, {
            "C": 1.0, "P": _frac and (106 - _frac) / 106, "wrong": _frac,
            "E2E": 0.9, "stageA": (100, 95), "stageB": (6, 5), "stageC": 3})
        check("fmt_cascade_row prints a fractional wrong count as a decimal "
              "estimate (est suffix), never crashing",
              f"wrong={_frac:.2f}est" in _crow, f"-> {_crow}")
        _crow_int = H.fmt_cascade_row(0.30, {
            "C": 1.0, "P": 106 / 106, "wrong": 0, "E2E": 0.9,
            "stageA": (106, 106), "stageB": (0, 0), "stageC": 0})
        check("fmt_cascade_row keeps integer format for integral wrong "
              "counts (an observation stays an observation)",
              "wrong= 0 " in _crow_int and "est" not in _crow_int,
              f"-> {_crow_int}")
    finally:
        _restore_env()

    if failures:
        print(f"self-test FAILED: {len(failures)} check(s): {failures}",
              file=sys.stderr)
        return 1
    print("self-test PASSED")
    return 0


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--policy", default="all",
                    choices=["all", "double-confidence", "tfidf-veto"])
    ap.add_argument("--check-overlap", action="store_true",
                    help="run only the corpus<->replay disjointness guard "
                         "(no embed server needed)")
    ap.add_argument("--self-test", action="store_true",
                    help="run the guard + coverage-floor self-test "
                         "(no embed server, no replay corpus needed)")
    args = ap.parse_args()

    if args.self_test:
        return self_test()

    if args.check_overlap:
        return check_overlap_only()

    policies = ({"double-confidence", "tfidf-veto"} if args.policy == "all"
                else {args.policy})
    return run_full(policies)


if __name__ == "__main__":
    sys.exit(main())
