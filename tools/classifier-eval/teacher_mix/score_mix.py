"""Score the teacher-mix raw artifacts against the replay gold labels.

Contract: docs/plans/teacher-gate-mixture/master.md (C3, C4) and leaf 03.
Denominator discipline: accuracy = correct / ALL non-OOD cases; errors
count as WRONG.
Deterministic: byte-identical summary.json across runs.
"""
from __future__ import annotations

import argparse
import json
import sys
from collections import Counter
from datetime import datetime, timezone
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
from run_mix import LABEL_MAP, load_cases  # noqa: E402


def main() -> None:
    ap = argparse.ArgumentParser(description="score teacher-mix raw artifacts")
    ap.add_argument("--replay", required=True, type=Path)
    ap.add_argument("--rawdir", required=True, type=Path)
    ap.add_argument("--outdir", required=True, type=Path)
    args = ap.parse_args()

    cases, _ood = load_cases(args.replay)
    gold = {c["case_id"]: c["lane"] for c in cases}

    records = []
    for raw_path in sorted(args.rawdir.glob("*.json")):
        records.append(json.loads(raw_path.read_text()))
    by_id = {r["case_id"]: r for r in records}

    per_intent: dict[str, dict[str, int]] = {}
    wrong: list[dict] = []
    errors: list[str] = []
    source_counts = Counter()
    correct = 0
    n = len(cases)

    for case_id in sorted(gold):
        expected = gold[case_id]
        rec = by_id.get(case_id)
        bucket = per_intent.setdefault(expected, {"n": 0, "correct": 0})
        bucket["n"] += 1
        if rec is None:
            errors.append(case_id)
            wrong.append({"case_id": case_id, "expected": expected,
                          "predicted": "<missing>", "source": "error",
                          "workers_agreed_wrong": False})
            continue
        final = rec["final"]
        source_counts[final["source"]] += 1
        if final["source"] == "error" or final["intent"] != expected:
            errors_here = final["source"] == "error"
            if errors_here:
                errors.append(case_id)
            a_intent = (rec["a"] or {}).get("intent", "")
            b_intent = (rec["b"] or {}).get("intent", "")
            wrong.append({
                "case_id": case_id,
                "expected": expected,
                "predicted": final["intent"] or "<error>",
                "source": final["source"],
                "workers_agreed_wrong": (not errors_here and a_intent == b_intent
                                         and a_intent == final["intent"]),
            })
        else:
            correct += 1
            bucket["correct"] += 1

    accuracy = correct / n if n else 0.0
    summary = {
        "n": n,
        "n_final_nonerror": n - len(errors),
        "correct": correct,
        "accuracy": round(accuracy, 4),
        "per_intent": {k: per_intent[k] for k in sorted(per_intent)},
        "source_distribution": dict(source_counts),
        "errors": errors,
        "mix_cost_usd_est": None,
        "timestamp": datetime.now(timezone.utc).isoformat(timespec="seconds"),
    }
    out_path = args.outdir / "summary.json"
    out_path.write_text(json.dumps(summary, indent=2, sort_keys=True) + "\n")
    print(f"correct {correct}/{n} = {accuracy:.2%}")
    print(f"non-error: {summary['n_final_nonerror']}")
    print(f"sources: {dict(source_counts)}")
    for w in wrong:
        print(f"WRONG {w['case_id']}: expected {w['expected']}, "
              f"got {w['predicted']} ({w['source']}"
              f"{', workers agreed wrong' if w['workers_agreed_wrong'] else ''})")


if __name__ == "__main__":
    main()
