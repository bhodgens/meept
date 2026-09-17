#!/usr/bin/env python3
"""Adversarial e2e sweep runner for meept.

Usage:
  python3 tools/e2e-sweep/run_adversarial.py [--base-url URL] [--home DIR] [Category ...]

Categories: MISROUTE INJECTION IMPOSSIBLE COMPOUND STATEFUL GARBAGE CROSS
(no filter = all).

Verdicts: PASS / FAIL(reason) / WEAK(reason) / TIMEOUT / ERROR.
"""
import sys
import uuid

import client
import async_wait
import scenarios


def main():
    args = client.parse_args()
    wanted = set(args.categories)
    unknown = wanted - {row[0] for row in scenarios.SCENARIOS + scenarios.STATEFUL}
    if unknown:
        print("unknown filter: " + ", ".join(sorted(unknown)), file=sys.stderr)
        return 2
    c = client.Client(args)
    counts = {"PASS": 0, "UNVERIFIED": 0, "FAIL": 0, "WEAK": 0, "TIMEOUT": 0, "ERROR": 0}

    for category, name, agent, message, grade in scenarios.SCENARIOS:
        if wanted and category not in wanted:
            continue
        try:
            v, d, dt = async_wait.run_async_aware(
                c, args.home, category, name, agent, message, grade)
        except Exception as e:
            v, d, dt = ("ERROR", str(e)[:90], 0)
        counts[v.split("(")[0]] = counts.get(v.split("(")[0], 0) + 1
        print(f"[{category:10s}] {name:32s} [{agent:10s}] {v:26s} {dt:5.1f}s  {d[:60]}")

    for category, name, agent, msg1, msg2, grade2 in scenarios.STATEFUL:
        if wanted and category not in wanted:
            continue
        session = f"e2e-{category}-{name}-{uuid.uuid4().hex}"
        try:
            v, d, dt = async_wait.run_async_aware(
                c, args.home, category, name, agent, msg1,
                lambda reply, ctx: scenarios.grade_turn1(reply), session=session)
            if v == "PASS":
                v, d, dt = async_wait.run_async_aware(
                    c, args.home, category, name, agent, msg2, grade2,
                    session=session)
            else:
                d = "turn 1: " + d
        except Exception as e:
            v, d = "ERROR", str(e)[:90]
        counts[v.split("(")[0]] = counts.get(v.split("(")[0], 0) + 1
        print(f"[{category:10s}] {name:32s} [{agent:10s}] {v:26s}  {d[:60]}")

    total = sum(counts.values())
    print(f"\n=== PASS {counts['PASS']} / WEAK {counts['WEAK']} / FAIL {counts['FAIL']} "
          f"/ UNVERIFIED {counts['UNVERIFIED']} / TIMEOUT {counts['TIMEOUT']} / ERROR {counts['ERROR']} of {total} ===")
    return 0 if total and counts["PASS"] == total else 1


if __name__ == "__main__":
    sys.exit(main())
