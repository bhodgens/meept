#!/usr/bin/env python3
"""Test-efficacy tracking: aggregate recorded go test runs into a per-test table.

Each `make test-record` run appends one JSONL line per test result to
$MEEPT_HOME/test-history/results.jsonl (or test-history/ in the repo root when
MEEPT_HOME is unset). This script reads that file and reports, per test:

  - runs     : how many recorded runs contain this test
  - fails    : how many of those runs it failed in
  - flake%   : fails / runs (a test that sometimes passes and sometimes fails)
  - avg_ms   : mean elapsed time across runs (timing noise included)
  - last_fail: run ID (timestamp) of the most recent failure

Usage:
  python3 scripts/test-efficacy.py                     # summary table (worst first)
  python3 scripts/test-efficacy.py --json              # machine-readable
  python3 scripts/test-efficacy.py --hist <run-id>     # one run's failures
  python3 scripts/test-efficacy.py --file <path>       # alternate history file

The history file is gitignored; it lives on each machine that runs tests.
"""

from __future__ import annotations

import argparse
import json
import os
import sys
from collections import defaultdict
from pathlib import Path

DEFAULT_REL = Path("test-history") / "results.jsonl"


def history_path(explicit: str | None) -> Path:
    if explicit:
        return Path(explicit)
    home = os.environ.get("MEEPT_HOME")
    if home:
        return Path(home) / "test-history" / "results.jsonl"
    return DEFAULT_REL


def load(path: Path) -> list[dict]:
    if not path.exists():
        sys.exit(f"no history file at {path} — run `make test-record` first")
    records: list[dict] = []
    with path.open() as fh:
        for lineno, line in enumerate(fh, 1):
            line = line.strip()
            if not line:
                continue
            try:
                rec = json.loads(line)
            except json.JSONDecodeError as exc:
                print(f"warn: {path}:{lineno}: bad JSON skipped ({exc})", file=sys.stderr)
                continue
            if rec.get("type") != "test":
                continue
            records.append(rec)
    return records


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--json", action="store_true", help="emit JSON instead of a table")
    ap.add_argument("--hist", metavar="RUN_ID", help="show failures from one run")
    ap.add_argument("--file", metavar="PATH", help="alternate results.jsonl path")
    ap.add_argument("--min-runs", type=int, default=1, metavar="N",
                    help="only show tests with >= N recorded runs in table mode")
    args = ap.parse_args()

    path = history_path(args.file)
    records = load(path)
    runs = sorted({r["run"] for r in records})
    if args.hist:
        fails = [r for r in records if r["run"] == args.hist and r["result"] != "pass"]
        if not fails:
            print(f"run {args.hist}: no failures recorded")
            return
        for r in fails:
            print(f"{r['result']:8s} {r['test']}")
            if r.get("output"):
                print(f"         {r['output']}")
        return

    # One entry per (test) — go test package names already make test IDs unique
    # across the module, and duplicate names inside one run (e.g. -count>1 or
    # parallel packages) are folded by keeping the worst result and max time.
    stats: dict[str, dict] = {}
    per_run: dict[str, dict[str, dict]] = defaultdict(dict)
    for r in records:
        per_run[r["run"]].setdefault(r["test"], r)
    for run, tests in per_run.items():
        for name, r in tests.items():
            s = stats.setdefault(name, {"runs": 0, "fails": 0, "skips": 0, "ms": [], "last_fail": ""})
            s["runs"] += 1
            if r["result"] == "fail":
                s["fails"] += 1
                s["last_fail"] = run
            elif r["result"] == "skip":
                s["skips"] += 1
            if isinstance(r.get("ms"), (int, float)):
                s["ms"].append(r["ms"])

    rows = []
    for name, s in stats.items():
        flake = 100.0 * s["fails"] / s["runs"] if s["runs"] else 0.0
        avg = sum(s["ms"]) / len(s["ms"]) if s["ms"] else 0.0
        rows.append({
            "test": name, "runs": s["runs"], "fails": s["fails"], "skips": s["skips"],
            "flake_pct": round(flake, 1), "avg_ms": round(avg),
            "last_fail": s["last_fail"],
        })
    rows.sort(key=lambda r: (-r["fails"], -r["flake_pct"], r["test"]))

    if args.json:
        print(json.dumps({"history_file": str(path), "runs_recorded": len(runs), "tests": rows}, indent=2))
        return

    print(f"history: {path}  ({len(runs)} recorded runs)")
    print(f"{'flake%':>7} {'fails':>6} {'skips':>6} {'runs':>6} {'avg_ms':>8}  {'last_fail':<21} test")
    shown = 0
    for r in rows:
        if r["runs"] < args.min_runs:
            continue
        print(f"{r['flake_pct']:>7.1f} {r['fails']:>6} {r['skips']:>6} {r['runs']:>6} {r['avg_ms']:>8}  {r['last_fail']:<21} {r['test']}")
        shown += 1
    if not shown:
        print("(no tests with recorded failures or runs — nothing to report)")


if __name__ == "__main__":
    main()
