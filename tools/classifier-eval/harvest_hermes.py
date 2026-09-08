#!/usr/bin/env python3
"""Harvest user messages from Hermes session JSON transcripts
(~/.hermes/sessions/session_*.json), produce
tools/classifier-eval/replay-corpus.local.json5 (UNTRACKED).

Silver labels via pattern heuristics over the meept intent taxonomy.
"""
import json
import re
import sys
from pathlib import Path
from collections import Counter

SESS_DIR = Path.home() / ".hermes" / "sessions"
OUT = Path("/Users/caimlas/git/meept/tools/classifier-eval/replay-corpus.local.json5")

PATTERNS = [
    (r"\b(git\s+)?(commit|push\b|pull request|\bPR\b|rebase|merge .*(branch|conflict)|cherry-?pick|stash)\b", "git", "committer"),
    (r"\b(debug|segfault|traceback|nil pointer|deadlock|race condition|crashes?|stack trace|build (fails?|broken)|stacked? (bugs?|failures?)|isn'?t working|not working|broken)\b.*\b(fix|debug|figure out)?", "debug", "debugger"),
    (r"\b(figure out why .* (isn'?t|is not|not) working|why .*(fail|crash|broken|error)|what.?s? (broken|failing))\b", "debug", "debugger"),
    # document writing (plans/reports/sections) is NOT meept code intent
    (r"\b(write|draft)\b.*\b(sections? \d|report|changelog|docs?/|\.md\b|readme|summary of)\b", None, None),
    (r"\b(write|create|implement|add|build|refactor|generate|scaffold|code|patch|port|set up|wire)\b.*\b(function|script|endpoint|api|tests?|class|module|component|handler|wrapper|parser|hook|middleware|migration|feature|skill|pipeline|tool|daemon|cli|toolkit|checker|validator|probe|infra|config)\b", "code", "coder"),
    (r"\b(implement (the|this|my) (plan|tree|feature)|implement .* using subagents)\b", "code", "coder"),
    (r"\b(compare|analy[sz]e|re-?analy[sz]e|evaluate|assess|triage|audit|investigate|look into|best practices|tradeoffs?|pros and cons)\b", "analyze", "analyst"),
    (r"\b(search( and)?|look up|find (me )?(a |an |the )?(link|doc|reference|paper|article|jobs?|listings?))\b", "search", "analyst"),
    (r"\b(plan\b|roadmap|design .*(architecture|system|approach)|strategy|break .* (down|into) (steps|phases|tasks)|plan file)\b", "plan", "planner"),
    (r"\b(review\b|critique|feedback on|code review|look over|go over)\b", "review", "reviewer"),
    (r"\b(remind|schedule|calendar|timer|cron)\b", "schedule", "scheduler"),
    (r"\b(summari[sz]e|digest|recap|changelog|status report|state of|where (are|do) we (on|with))\b", "report", "chat"),
    (r"\b(remember (when|that|we)|last (week|time) (we|you)|what did we (do|say|decide)|earlier (today|this week))\b", "recall", "chat"),
    (r"^(what can you do|what (tools|skills|agents|models) (do you|are)|how do (I|i) (use|enable|connect|configure|set))", "platform", "chat"),
]

SKIP = re.compile(
    r"^\s*(/|\[|<|```|#|OK\b|okay\b|yes\b|no\b|nope\b|continue|continue\.|thanks|thank you|ok\b|go ahead|proceed|done\b|approved|deny\b|cool\b|great\b|sounds good|"
    r"test\b|sty\b|sty|y\b|n\b|\d+\s*$|good\b|perfect\b|nice\b|excellent|ack\b|agreed|uh|hmm|wait\b|stop\b|abort|cancel\b|skip\b|next\b|both\b|neither\b|either\b|"
    r"keep going|go on|why\b|how come|exactly|seriously|really\?|true\b|false\b|correct\b|wrong\b|right\b|1\b|2\b|3\b|a\b|b\b|c\b)",
    re.I)


def classify(text: str):
    for pat, intent, agent in PATTERNS:
        if re.search(pat, text, re.I):
            if intent is None:
                return None, None  # explicit OOD filter (doc-writing etc.)
            return intent, agent
    return None, None


def main():
    seen = set()
    out = []
    n_sess = 0
    for p in sorted(SESS_DIR.glob("session_*.json")):
        try:
            d = json.loads(p.read_text())
        except Exception:
            continue
        msgs = d.get("messages") or []
        n_sess += 1
        for m in msgs:
            if m.get("role") != "user":
                continue
            c = m.get("content")
            if not isinstance(c, str):
                # content blocks (list) — join text parts
                if isinstance(c, list):
                    c = " ".join(b.get("text", "") for b in c if isinstance(b, dict))
                else:
                    continue
            t = c.strip()
            if not (15 <= len(t) <= 280):
                continue
            if SKIP.match(t):
                continue
            if t.startswith(("http", "MEDIA:", "```", "[Pasted")):
                continue
            # skip hermes control sentences
            if re.match(r"^(run|do|use|try|check|read|look|also|then|and|but|or)\b.{0,3}$", t, re.I):
                continue
            intent, agent = classify(t)
            if not intent:
                continue
            k = t.lower()
            if k in seen:
                continue
            seen.add(k)
            out.append({
                "input": t,
                "expected_intent": intent,
                "expected_agent": agent,
                "silver": True,
                "source": f"hermes:{d.get('session_id', p.stem)}",
            })

    print(f"scanned {n_sess} sessions; harvested {len(out)} silver cases",
          file=sys.stderr)
    print(Counter(o["expected_intent"] for o in out), file=sys.stderr)

    header = ("// Hermes-transcript replay corpus — SILVER LABELS (pattern-heuristic).\n"
              "// UNTRACKED per user directive (verbatim, private). Never mix into\n"
              "// headline P/C without explicit promotion (master.md silver rule).\n"
              "{\n  cases: [\n")
    body = "".join("    " + json.dumps(o, ensure_ascii=False) + ",\n" for o in out)
    footer = "  ],\n}\n"
    OUT.write_text(header + body + footer)
    print(f"wrote {OUT}", file=sys.stderr)


if __name__ == "__main__":
    main()
