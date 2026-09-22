#!/usr/bin/env python3
"""Scrub replay-corpus (category 3) text from tracked evidence files.

Decision B: the private set is the 48-case REPLAY corpus only
(tools/classifier-eval/replay-gold.local.json5, untracked). Raw or
truncated replay excerpts in tracked report/doc/audit files are replaced
with `eval_harness.case_key` identifiers (16-hex), preserving the
surrounding structure. Designed fixtures (testdata/eval, harness
scripts, dispatcher tests) are excluded per the campaign adjudication.

Reads the corpus at run time; no private text is embedded here.
"""
import hashlib
import json
import re
import sys
from pathlib import Path

REPO = Path("/Users/caimlas/git/meept")
CORPUS = REPO / "tools/classifier-eval/replay-gold.local.json5"


def case_key(text):
    return hashlib.sha256(text.strip().encode()).hexdigest()[:16]


def corpus_inputs():
    raw = CORPUS.read_text()
    return [json.loads('"' + m.group(1) + '"')
            for m in re.finditer(r'"input"\s*:\s*"((?:[^"\\]|\\.)*)"', raw)]


# ---------------------------------------------------------------- scrub ops
# Each op: file -> list of (needle, replacement). Longest needles first.

def build_ops():
    inputs = corpus_inputs()
    k = {case_key(t): t for t in inputs}

    full_15 = k["2c0434047ecad05e"]   # implement the plan using subagents
    full_45 = k["338cb3b2e9ffa8c4"]   # implement the plan
    full_1 = k["244c105b409b844e"]    # review the json files to make sure they're complete
    pre_17 = k["2a7a0eb076138ce4"][:40]   # Add the projects section to the default
    pre_18 = k["834aead92a2a8d7f"][:40]   # Implement Tasks 7 and 8: Add project fie
    pre_31 = k["effcf40fbc1160e3"][:40]   # commit, omit the .env but include an env
    pre_33 = k["6e0fc8a0c28f759a"][:40]   # create a skill-tailored resume for this
    pre_38 = k["d8ef399b90b87cfe"]        # commit, push, then run prodenv/prod (full, 35 chars)
    pre_5 = k["616d64a1c5cc6116"][:40]    # using subagents, review the meept clien

    ops = {
        "docs/workflows/intent-routing.md": [
            (full_1, "[replay case 244c105b409b844e]"),
        ],
        "docs/plans/quickplan-mode/master.md": [
            (full_15, "[replay case 2c0434047ecad05e]"),
        ],
        "docs/plans/teacher-gate-mixture/03-scoring-analysis.md": [
            (full_45, "[replay case 338cb3b2e9ffa8c4]"),
        ],
        "docs/generated/llms-readme-full.txt": [
            (full_1, "[replay case 244c105b409b844e]"),
        ],
        ".hermes/audits/2026-09-10-bughunt-wave.md": [
            (full_15, "[replay case 2c0434047ecad05e]"),
        ],
        ".hermes/audits/2026-09-12-bughunt-wave.md": [
            (full_15, "[replay case 2c0434047ecad05e]"),
            (pre_17, "[replay case 2a7a0eb076138ce4 prefix]"),
        ],
        "tools/classifier-eval/results/iter-18/report.md": [
            (full_45, "[replay case 338cb3b2e9ffa8c4]"),
            (full_15, "[replay case 2c0434047ecad05e]"),
            ("commit, omit the .env...",
             "commit-omit-env [replay case effcf40fbc1160e3]"),
        ],
        "tools/classifier-eval/results/iter-19/report.md": [
            (pre_18, "[replay case 834aead92a2a8d7f prefix]"),
        ],
        "tools/classifier-eval/results/iter-20/report.md": [
            (pre_18, "[replay case 834aead92a2a8d7f prefix]"),
            (pre_38, "[replay case d8ef399b90b87cfe]"),
        ],
        "tools/classifier-eval/results/m4-gold-acceptance.md": [
            ("implement the plan using\n"
             "subagents", "[replay case 2c0434047ecad05e (line-wrapped)]"),
        ],
        "tools/classifier-eval/results/m4-silver/cascade-validation.json": [
            # canonical results JSON: keep schema, excerpt -> case key
            ('"commit, omit the .env but include an env.sample with comment"',
             '"[replay case effcf40fbc1160e3]"'),
            ('"create a skill-tailored resume for this posting (1-page) bas"',
             '"[replay case 6e0fc8a0c28f759a prefix]"'),
            ('"implement the plan"',
             '"[replay case 338cb3b2e9ffa8c4]"'),
        ],
        "tools/classifier-eval/results/m4-silver/cascade-validation-correction.md": [
            (full_15, "[replay case 2c0434047ecad05e]"),
            ('"commit, omit the .env but include an env.sample with comment"',
             '"[replay case effcf40fbc1160e3]"'),
        ],
        "tools/classifier-eval/results/m4-silver/report.md": [
            (full_15, "[replay case 2c0434047ecad05e]"),
            (full_45, "[replay case 338cb3b2e9ffa8c4]"),
        ],
    }
    # order needles longest-first within each file
    return {f: sorted(v, key=lambda x: -len(x[0])) for f, v in ops.items()}


def main():
    ops = build_ops()
    total = 0
    for rel, subs in ops.items():
        p = REPO / rel
        blob = p.read_text()
        n = 0
        for needle, repl in subs:
            c = blob.count(needle)
            if c:
                blob = blob.replace(needle, repl)
                n += c
        if n:
            p.write_text(blob)
            print(f"{rel}: {n} replacement(s)")
            total += n
    print(f"TOTAL: {total} replacements across {len(ops)} files")
    return total


if __name__ == "__main__":
    main()
