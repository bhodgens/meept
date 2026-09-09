#!/usr/bin/env python3
"""Fix build_prefilter_centroids.py for the iter-19/20 adversarial corpus.

Blockers found by the leaf-03 agent:
1. parse_json5 does not strip // line comments (iters 19/20 added them).
2. load_cases reads the OLD categories layout only; the adversarial corpus
   uses cases: with expected_intent/ood fields (and the categories loop
   then crashes on non-dict entries).

This patch ports the harness's parse (comment stripping) and the dual-
layout case loading into the centroid builder, so the tracked corpus and
the eval harness read identically.
"""
import re
from pathlib import Path

p = Path("scripts/build_prefilter_centroids.py")
text = p.read_text()

# 1) comment stripping in parse_json5 (same approach as eval_harness.py)
old = '''    # Double-quoted strings: no escapes in the corpus, so [^"] suffices.
    text = re.sub(r'"[^"]*"', stash, text)
    # Quote unquoted object keys.'''
new = '''    # Double-quoted strings: no escapes in the corpus, so [^"] suffices.
    text = re.sub(r'"[^"]*"', stash, text)
    # Strip // line comments (adversarial corpus uses them; iters 19+).
    text = re.sub(r"(?m)^\\s*//.*$", "", text)
    # Quote unquoted object keys.'''
assert old in text, "parse anchor not found"
text = text.replace(old, new)

# 2) dual-layout case loading
old_load = '''def load_cases(corpus_path: str) -> list[tuple[str, str, str]]:
    """Returns (input, intent, agent) triples."""
    raw = Path(corpus_path).read_text()
    data = parse_json5(raw)
    cases = []
    for intent, entries in data["categories"].items():
        for e in entries:
            agent = e.get("expected_agent", "")
            cases.append((e["input"], e["expected_intent"], agent))
    return cases'''
new_load = '''def load_cases(corpus_path: str) -> list[tuple[str, str, str]]:
    """Returns (input, intent, agent) triples.

    Two layouts supported:
    - legacy categories: {categories: {intent: [{input, expected_intent}]}}
    - adversarial cases: {cases: [{input, expected_intent | ood}]}
      (classifier-iteration corpus, iters 2-20). OOD cases are skipped:
      the prefilter index carries only real intents.
    """
    raw = Path(corpus_path).read_text()
    data = parse_json5(raw)
    cases = []
    for intent, entries in (data.get("categories") or {}).items():
        for e in entries:
            cases.append((e["input"], e.get("expected_intent", intent),
                          e.get("expected_agent", "")))
    for e in (data.get("cases") or []):
        if e.get("ood") or not e.get("expected_intent"):
            continue  # OOD / compound-abstain: not indexable
        cases.append((e["input"], e["expected_intent"],
                      e.get("expected_agent", "")))
    return cases'''
assert old_load in text, "load_cases anchor not found"
text = text.replace(old_load, new_load)

p.write_text(text)
print("patched build_prefilter_centroids.py")
