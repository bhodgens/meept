#!/usr/bin/env python3
"""Happy-path sweep runner: one tailored task per agent.

Usage:
  python3 tools/e2e-sweep/run_sweep.py [--base-url URL] [--home DIR] [agent ...]
"""
import sys

import client
import async_wait
from evidence import require_tool

# agent_id -> (task, must-contain substrings, never-contain substrings)
TASKS = {
    "chat": ["What is 17 * 23? Reply with the number and a one-line explanation.",
             ["391"], ["platform capabilities"]],
    "researcher": ["Use the json_extract tool with schema={\"type\":\"object\",\"properties\":{\"title\":{\"type\":\"string\"},\"year\":{\"type\":\"integer\"}},\"required\":[\"title\"]} and text='The transformer paper by Vaswani appeared in 2017.' Reply with the record only.",
                   ["2017"], ["platform capabilities", "not configured", "I stopped after"]],
    "coder": ["Write a Go function Min(a, b int) int returning the smaller value. Function only.",
              ["func Min"], ["platform capabilities", "I stopped after"]],
    "analyst": ["Analyze this claim: 'Caching always improves performance.' Give two counterexamples where caching hurts.",
                ["cache"], ["platform capabilities", "I stopped after"]],
    "writer": ["Write a two-sentence summary of what version control is for.",
               [""], ["platform capabilities", "I stopped after"]],
    "architect": ["Propose a 3-component architecture for a URL shortener. Under 120 words.",
                  ["component"], ["platform capabilities", "I stopped after"]],
    "skeptic": ["Critique: 'Tests are unnecessary because the code compiled.' Two strongest objections.",
                ["test"], ["platform capabilities", "I stopped after"]],
    "debugger": ["Debug: function add(a,b) returns a-b; caller expects a+b. Root cause and one-line fix.",
                 ["a + b", "a+b"], ["platform capabilities", "I stopped after"]],
    "planner": ["Plan (do not execute) adding a /health endpoint to a Go HTTP server. 3 numbered steps.",
                ["1"], ["platform capabilities", "I stopped after"]],
    "verifier": ["Verify by reasoning only: 'sorting n elements requires at least n-1 comparisons.' Does it hold?",
                 ["n"], ["platform capabilities", "I stopped after"]],
    "committer": ["Conventional-commit subject for a one-line off-by-one fix. One line.",
                  ["fix"], ["platform capabilities", "I stopped after"]],
    "code-reviewer": ["Review: '- return a - b' '+ return a + b' in function add. Correct?",
                      ["correct"], ["platform capabilities", "I stopped after"]],
}


def classify(reply, must, never):
    if not reply.strip():
        return "FAIL(empty)"
    low = reply.lower()
    for n in never:
        if n.lower() in low:
            return f"FAIL({n[:40]})"
    if must and not any(m.lower() in low for m in must):
        return "WEAK(no-expected-content)"
    return "PASS"


def main():
    args = client.parse_args()
    wanted = set(args.categories)
    unknown = wanted - set(TASKS)
    if unknown:
        print("unknown filter: " + ", ".join(sorted(unknown)), file=sys.stderr)
        return 2
    c = client.Client(args)
    counts = {"PASS": 0, "UNVERIFIED": 0, "FAIL": 0, "WEAK": 0, "ERROR": 0, "TIMEOUT": 0}
    for agent, (task, must, never) in TASKS.items():
        if wanted and agent not in wanted:
            continue
        try:
            grade = lambda r, ctx=None: (classify(r, must, never), "")
            if agent == "researcher":
                grade = require_tool(grade, count=1)
            v, d, dt = async_wait.run_async_aware(
                c, args.home, "SWEEP", agent, agent, task, grade)
        except Exception as e:
            v, d, dt = ("ERROR", str(e)[:90], 0)
        counts[v.split("(")[0]] = counts.get(v.split("(")[0], 0) + 1
        print(f"[{agent:20s}] {v:26s} {dt:5.1f}s  {d[:60]}")
    total = sum(counts.values())
    print(f"\n=== PASS {counts['PASS']} / WEAK {counts['WEAK']} / FAIL {counts['FAIL']} "
          f"/ UNVERIFIED {counts['UNVERIFIED']} / TIMEOUT {counts['TIMEOUT']} / ERROR {counts['ERROR']} of {total} ===")
    return 0 if total and counts["PASS"] == total else 1


if __name__ == "__main__":
    sys.exit(main())
