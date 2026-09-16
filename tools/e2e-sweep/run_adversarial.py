#!/usr/bin/env python3
"""Adversarial e2e sweep runner for meept.

Usage:
  python3 tools/e2e-sweep/run_adversarial.py [--base-url URL] [--home DIR] [Category ...]

Categories: MISROUTE INJECTION IMPOSSIBLE COMPOUND STATEFUL GARBAGE CROSS
(no filter = all).

Verdicts: PASS / FAIL(reason) / WEAK(reason) / TIMEOUT / ERROR.
"""
import sys

import client
import async_wait
import scenarios


def main():
    args = client.parse_args()
    c = client.Client(args)
    wanted = set(args.categories)
    counts = {"PASS": 0, "FAIL": 0, "WEAK": 0, "TIMEOUT": 0, "ERROR": 0}

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
        session = f"e2e-{category}-{name}-{int(__import__('time').time())}"
        try:
            r1 = c.chat(msg1, session, agent)
        except Exception as e:
            print(f"[{category:10s}] {name:32s} ERROR {str(e)[:70]}")
            counts["ERROR"] += 1
            continue
        v1, d1 = scenarios.grade_turn1(r1)
        if v1 != "PASS":
            print(f"[{category:10s}] {name:32s} FAIL(turn1) {d1[:60]}")
            counts["FAIL"] += 1
            continue
        import time as _t
        _t.sleep(2)
        r2 = c.chat(msg2, session, agent)
        v, d = grade2(r2, {})
        counts[v.split("(")[0]] = counts.get(v.split("(")[0], 0) + 1
        print(f"[{category:10s}] {name:32s} [{agent:10s}] {v:26s}  {d[:60]}")

    total = sum(counts.values())
    print(f"\n=== PASS {counts['PASS']} / WEAK {counts['WEAK']} / FAIL {counts['FAIL']} "
          f"/ TIMEOUT {counts['TIMEOUT']} / ERROR {counts['ERROR']} of {total} ===")


if __name__ == "__main__":
    main()
