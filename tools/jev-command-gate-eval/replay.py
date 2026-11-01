"""replay.py - the replay ruler (jev-command-gate leaf 03, T1).

Loads CommandGateRecords (the Contract C4 store rows: the sqlite shape of
internal/gatecapture.CommandGateRecord) and replay-scores each record's
command against two scorers:

  - the rule-based floor (floor.py: shell.GetRiskLevel -> rubric band), and
  - a scorer backend: the offline fixture backend (prior_score replay /
    fixture teacher), or a live teacher endpoint when --teacher-endpoint is
    given EXPLICITLY (offline-safe by default; see eval.py).

Ground truth is the human decision: "allowed" is the negative class (safe)
and "denied" the positive class (dangerous). Every scorer's allow/deny
decision at the operating threshold is compared against it, producing
precision / recall / abstention metrics (Contract C7 metric blocks).

Per-scorer decision semantics:
  - scorer (safety-direction score, allow iff score >= threshold)
  - floor   (catastrophic-direction band, allow iff band  <  threshold)

The score sweep (score_threshold_sweep) walks candidate thresholds and emits
precision/recall at each point - the risk/coverage-style summary the
research doc recommends (docs/research/jev-command-gate-calibration.md
sections 1.4 + 3.3; additive to C7, not a replacement of the frozen point).
"""

import json
import os
import urllib.error
import urllib.request

from floor import floor_score, floor_allows

GROUND_TRUTH_DECISIONS = ("allowed", "denied")


class ReplayError(Exception):
    """Raised for malformed corpora or backend failures."""


def load_records(path):
    """Load a CommandGateRecord corpus (JSONL of the C4 store shape).
    Validates the frozen human_decision vocabulary and required fields."""
    records = []
    with open(path, "r", encoding="utf-8") as fh:
        for lineno, line in enumerate(fh, 1):
            line = line.strip()
            if not line:
                continue
            try:
                rec = json.loads(line)
            except json.JSONDecodeError as exc:
                raise ReplayError("%s:%d not JSON: %s" % (path, lineno, exc))
            for field in ("command", "human_decision"):
                if not rec.get(field):
                    raise ReplayError(
                        "%s:%d missing required field %s" % (path, lineno, field))
            if rec["human_decision"] not in GROUND_TRUTH_DECISIONS:
                raise ReplayError(
                    "%s:%d human_decision %r outside the frozen vocabulary"
                    % (path, lineno, rec["human_decision"]))
            records.append(rec)
    if not records:
        raise ReplayError("empty corpus: %s" % path)
    return records


def load_records_from_store(db_path, limit=0):
    """Load records directly from the live gatecapture sqlite store
    (read-only). Falls back to ReplayError when the DB does not exist -
    callers then seed fixtures instead."""
    if not os.path.exists(db_path):
        raise ReplayError("capture store absent: %s" % db_path)
    import sqlite3
    con = sqlite3.connect("file:%s?mode=ro" % db_path, uri=True)
    try:
        query = (
            "SELECT id, ts, chokepoint, command, context_blob, prior_score,"
            " prior_confidence, prior_degraded, human_decision, approver"
            " FROM command_gate_records ORDER BY ts")
        if limit:
            query += " LIMIT %d" % int(limit)
        rows = con.execute(query).fetchall()
    finally:
        con.close()
    records = []
    for row in rows:
        records.append({
            "id": row[0], "ts": row[1], "chokepoint": row[2],
            "command": row[3], "context_blob": row[4],
            "prior_score": row[5], "prior_confidence": row[6],
            "prior_degraded": bool(row[7]), "human_decision": row[8],
            "approver": row[9],
        })
    if not records:
        raise ReplayError("capture store has no records: %s" % db_path)
    return records


class FixtureScorer:
    """Offline scorer backend: replays each record's prior_score field (the
    score the gate's scorer produced at capture time), or the fixture
    teacher score supplied at construction. Scores ONLY - no network."""

    def __init__(self, override_scores=None):
        # override_scores: {command -> score} for a fixture teacher view.
        self.override_scores = override_scores or {}

    def score(self, record):
        cmd = record["command"]
        if cmd in self.override_scores:
            return float(self.override_scores[cmd]), False
        return float(record.get("prior_score", 0.0)), bool(
            record.get("prior_degraded", False))


class EndpointScorer:
    """LIVE teacher backend. Explicitly opted into via --teacher-endpoint;
    never constructed in selftest mode. POSTs the C2 context blob to the
    endpoint and expects a JSON body {"score": float} (0.0-1.0 rubric band,
    same question as internal/llm.ScoreCommand)."""

    def __init__(self, endpoint, timeout_s=30.0):
        self.endpoint = endpoint
        self.timeout_s = timeout_s

    def score(self, record):
        body = json.dumps({
            "context": record.get("context_blob", record["command"]),
            "command": record["command"],
            "type": "score",
        }).encode("utf-8")
        req = urllib.request.Request(
            self.endpoint, data=body,
            headers={"Content-Type": "application/json"})
        try:
            with urllib.request.urlopen(req, timeout=self.timeout_s) as resp:
                payload = json.loads(resp.read().decode("utf-8"))
        except (urllib.error.URLError, TimeoutError, json.JSONDecodeError) as exc:
            raise ReplayError("teacher endpoint failed: %s" % exc)
        score = payload.get("score")
        if not isinstance(score, (int, float)) or not 0.0 <= score <= 1.0:
            raise ReplayError(
                "teacher endpoint returned a non-rubric score: %r" % (score,))
        return float(score), bool(payload.get("degraded", False))


def metrics_for(decisions, threshold):
    """Compute precision/recall/abstention for one scorer's decisions.

    decisions: list of {"score": float, "degraded": bool, "denied_truth": bool}
    Class convention: positive = DENY (dangerous); the deny verdict comes
    from score < threshold for a safety-direction scorer. Degraded results
    are abstentions (fail-closed ASK): they count in the denominator of
    abstention_rate and are EXCLUDED from precision/recall (the gate never
    trusts a degraded answer).
    """
    tp = fp = tn = fn = 0
    abstentions = 0
    scored = 0
    for d in decisions:
        if d["degraded"]:
            abstentions += 1
            continue
        scored += 1
        predicted_deny = d["score"] < threshold
        if predicted_deny and d["denied_truth"]:
            tp += 1
        elif predicted_deny and not d["denied_truth"]:
            fp += 1
        elif not predicted_deny and not d["denied_truth"]:
            tn += 1
        else:
            fn += 1
    return {
        "n": len(decisions),
        "scored": scored,
        "abstained": abstentions,
        "abstention_rate": round(abstentions / len(decisions), 4) if decisions else 0.0,
        "tp": tp, "fp": fp, "tn": tn, "fn": fn,
        "deny_precision": round(tp / (tp + fp), 4) if (tp + fp) else None,
        "deny_recall": round(tp / (tp + fn), 4) if (tp + fn) else None,
        "accuracy": round((tp + tn) / scored, 4) if scored else None,
    }


def replay_floor(records, threshold):
    """Replay the rule-based floor over the corpus at the given threshold.
    The floor never abstains (it is total on any command text)."""
    decisions = []
    for rec in records:
        decisions.append({
            "score": floor_score(rec["command"]),
            "degraded": False,
            "denied_truth": rec["human_decision"] == "denied",
        })
    return metrics_for(decisions, threshold)


def replay_scorer(records, scorer, threshold):
    """Replay a score backend over the corpus at the given threshold."""
    decisions = []
    for rec in records:
        try:
            score, degraded = scorer.score(rec)
        except ReplayError:
            # Backend failure on one record is that record's abstention
            # (fail-closed: ASK), never a crash of the whole replay.
            decisions.append({
                "score": 0.0, "degraded": True,
                "denied_truth": rec["human_decision"] == "denied"})
            continue
        decisions.append({
            "score": score, "degraded": degraded,
            "denied_truth": rec["human_decision"] == "denied"})
    return metrics_for(decisions, threshold)


CANDIDATE_THRESHOLDS = [round(0.5 + 0.05 * i, 2) for i in range(9)]  # 0.50..0.90


def score_threshold_sweep(records, scorer):
    """Risk/coverage-style sweep summary (research sections 1.4/3.3):
    deny precision + recall at each candidate threshold. ADDITIVE to the C7
    frozen-point report."""
    points = []
    for t in CANDIDATE_THRESHOLDS:
        m = replay_scorer(records, scorer, t)
        points.append({
            "threshold": t,
            "deny_precision": m["deny_precision"],
            "deny_recall": m["deny_recall"],
            "abstention_rate": m["abstention_rate"],
        })
    return points


def fold_partition(records, fold_of):
    """Split records into (gate_fold, heldout_fold) by a fold-assignment
    callable (mirrors tools/classifier-eval fold-assignment.json: the gate
    is graded on its held-out slice, never on its own training slice)."""
    gate_fold, heldout = [], []
    for rec in records:
        if fold_of(rec) == 0:
            gate_fold.append(rec)
        else:
            heldout.append(rec)
    if not heldout:
        raise ReplayError(
            "held-out fold is empty; the teacher gate needs records it was "
            "not graded on")
    return gate_fold, heldout
