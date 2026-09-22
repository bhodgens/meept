#!/usr/bin/env python3
"""Scan tracked files for replay-corpus (category 3) text leakage.

Needles are derived at run time from the untracked private replay corpus
(tools/classifier-eval/replay-gold.local.json5): for every case input we
probe with (a) the full raw text, (b) its first-40-char prefix, each in
raw and JSON-escaped form. The synthetic 389-case corpus in
testdata/eval/ is DESIGNED content and is excluded, as are the harness
fixtures listed in EXCLUDE (decision B keeps those).

Modes:
  scan    -> print per-file needle hit counts and exit 1 if any hits
  details -> print per-needle details (which needle, how many times)
"""
import json
import re
import subprocess
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
CORPUS = REPO / "tools/classifier-eval/replay-gold.local.json5"

# Decision-B exclusions: designed fixtures / harness code, kept as-is.
EXCLUDE_PREFIXES = (
    "testdata/eval/",
    "internal/eval/testdata/",
)
EXCLUDE_EXACT = {
    "internal/agent/llm_classifier.go",
    # Routing fixtures pinning the shipped QuickPlanCuePattern surface
    # forms (documented design, decision B): dispatcher.go carries the
    # production cue literals, the qpupgrade regression test replays the
    # designed corpus rows (h18-planexec-001 + anchor paraphrases), the
    # fewshot/session tests pin DESIGNED phrasings that merely contain
    # the generic feature phrase.
    "internal/agent/dispatcher.go",
    "internal/agent/dispatcher_routing_repair_qpupgrade_test.go",
    "internal/agent/llm_classifier_fewshot_pin_test.go",
    "internal/agent/llm_classifier_quickplan_desc_test.go",
    "internal/agent/session_state_gate_test.go",
    "tools/classifier-eval/iter18_cases.json5",
    "tools/classifier-eval/m4_gold_acceptance.py",
    "tools/classifier-eval/test_result_privacy.py",  # holds placeholders
    # session-upgrade A/B: shipped quickplan-cue line ships the generic
    # feature phrase "implement the plan" inside any(...); designed probe
    # wire, never echoes a corpus message.
    "tools/classifier-eval/session_upgrade_ab.py",
}
EXCLUDE_PATTERNS = (
    re.compile(r"^tools/classifier-eval/(iter(7|10|19|20)[a-z0-9_]*_sweep\.py)$"),
    re.compile(r"^tools/classifier-eval/iter(7|10|19|20)_.*\.py$"),
    re.compile(r"^tools/classifier-eval/harvest_.*\.py$"),
)


def corpus_inputs():
    """Parse the untracked JSON5 corpus without external deps."""
    raw = CORPUS.read_text()
    # strip // comments (no // appears inside string literals in this file's
    # inputs; guard by only stripping lines whose // is outside quotes)
    lines = []
    for line in raw.splitlines():
        out, in_str, esc = [], False, False
        for ch in line:
            if esc:
                esc = False
            elif ch == "\\" and in_str:
                esc = True
            elif ch == '"':
                in_str = not in_str
            elif ch == "/" and not in_str and line.endswith("//"[:1]):
                pass
            if not in_str and ch == "/" and out and out[-1] == "/":
                out.pop()
                break
            out.append(ch)
        lines.append("".join(out))
    text = "\n".join(lines)
    inputs = []
    for m in re.finditer(r'"input"\s*:\s*"((?:[^"\\]|\\.)*)"', text):
        inputs.append(json.loads('"' + m.group(1) + '"'))
    return inputs


def needles_for(text):
    """Full text + 40-char prefix, each raw and JSON-escaped."""
    ns = []
    for t in (text, text[:40]):
        if not t.strip():
            continue
        ns.append(t)
        ns.append(json.dumps(t)[1:-1])  # JSON-escaped body
    # de-dup, longest first so replacements use the most specific needle
    seen, out = set(), []
    for n in sorted(set(ns), key=len, reverse=True):
        if n not in seen:
            seen.add(n)
            out.append(n)
    return out


def tracked_files():
    out = subprocess.run(["git", "ls-files"], cwd=REPO,
                         capture_output=True, text=True, check=True).stdout
    files = out.splitlines()
    keep = []
    for f in files:
        if f in EXCLUDE_EXACT or f.endswith("test_result_privacy.py"):
            continue
        if any(f.startswith(p) for p in EXCLUDE_PREFIXES):
            continue
        if any(p.match(f) for p in EXCLUDE_PATTERNS):
            continue
        keep.append(f)
    return keep


def scan(details=False):
    inputs = corpus_inputs()
    needle_map = {}  # needle -> case key
    for t in inputs:
        key = _case_key(t)
        for n in needles_for(t):
            needle_map[n] = key
    hits = {}  # file -> list of (needle, key, count)
    for f in tracked_files():
        p = REPO / f
        try:
            blob = p.read_text()
        except (UnicodeDecodeError, OSError):
            continue
        for n, key in needle_map.items():
            c = blob.count(n)
            if c:
                hits.setdefault(f, []).append((n, key, c))
    if details:
        for f in sorted(hits):
            print(f"== {f}")
            for n, key, c in sorted(hits[f], key=lambda x: -x[2]):
                shown = n if len(n) <= 60 else n[:57] + "..."
                print(f"   key={key} count={c} needle={shown!r}")
    else:
        for f in sorted(hits):
            total = sum(c for _, _, c in hits[f])
            print(f"{f}: {len(hits[f])} needles, {total} occurrences")
    print(f"\ncorpus cases: {len(inputs)}, needles: {len(needle_map)}, "
          f"files with hits: {len(hits)}")
    return hits


def _case_key(text):
    import hashlib
    return hashlib.sha256(text.strip().encode()).hexdigest()[:16]


if __name__ == "__main__":
    hits = scan(details="--details" in sys.argv or "-d" in sys.argv)
    sys.exit(1 if hits else 0)
