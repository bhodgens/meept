#!/usr/bin/env python3
"""Orphan-ratchet gate for the connectivity graph.

Compares the CURRENT orphan set — derived from docs/generated/bus-topology.json
(the "orphans" key written by scripts/gen-connectivity-graph.py) — against a
committed baseline in scripts/orphan-baseline.txt, with the same policy as the
deadcode gate (.githooks/pre-commit-deadcode): baselined stock is tolerated,
NEW orphans block, re-baselining is deliberate and documented.

Why a baseline instead of "must be empty": the repo already carries 66 orphans
(55 topics published but never subscribed + 11 subscribed but never published,
the latter mostly RPC-proxy request topics whose client surface is external).
A zero-tolerance gate would block every commit; a new-only ratchet forces every
NEW unwired bus topic / RPC handler / WS emitter to either land with its
consumer side or be consciously accepted in the baseline.

Stable keys: the comparison key is "<kind> <topic>" — topic/handler NAME only,
never the line offsets the generator embeds elsewhere (audit F66) — so code
motion above a publish/subscribe site never produces false new/removed churn.
The orphan lists themselves already carry plain topic names; we additionally
strip any accidentally-appended ".go:LINE" suffix defensively so the key stays
name-based even if the generator's shape drifts.

Non-vacuity floor: if the topology JSON parses to ZERO topics, the checker
exits 1 with a loud "checker parsed nothing" error rather than reporting a
clean run — a broken generator must never masquerade as a green gate.

Usage:
  python3 scripts/orphan-check.py                # gate (exit 1 on new orphans)
  python3 scripts/orphan-check.py --rebaseline   # regenerate the baseline

Env / flag overrides (testing/CI parity with the deadcode gate):
  --topology PATH   or MEEPT_ORPHAN_TOPOLOGY   topology JSON location
  --baseline PATH   or MEEPT_ORPHAN_BASELINE   baseline file location

Stdlib only. Exit codes: 0 clean, 1 new orphans / vacuous parse / bad input.
"""

import argparse
import json
import os
import sys

REPO_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))

# orphan dict key -> baseline line kind (stable, human-greppable)
KINDS = {
    "published_not_subscribed": "published-not-subscribed",
    "subscribed_not_published": "subscribed-not-published",
}


def normalize_entry(raw):
    """Return a stable topic/handler NAME from an orphan list entry.

    Entries are plain topic strings today; tolerate dicts (generator shape
    drift) by reading 'topic', and strip any accidental file:line suffix or
    whitespace so code motion never churns the key.
    """
    if isinstance(raw, dict):
        raw = raw.get("topic") or raw.get("name") or ""
    if not isinstance(raw, str):
        return None
    t = raw.strip().strip('"').strip("'")
    # Defensive: drop embedded source-location suffixes (name.go[:LINE]).
    idx = t.find(".go:")
    if idx != -1:
        t = t[:idx]
    return t or None


def current_orphans(topology_path):
    """Parse the topology JSON, return (keys, topic_count) or raise."""
    with open(topology_path, "r", encoding="utf-8") as f:
        data = json.load(f)
    if not isinstance(data, dict):
        raise ValueError("topology JSON is not an object")

    pubs = data.get("publishers") or {}
    subs = data.get("subscribers") or {}
    topic_count = len(set(pubs) | set(subs))

    orphans = data.get("orphans")
    if orphans is None:
        raise ValueError("topology JSON has no 'orphans' key")
    if not isinstance(orphans, dict):
        raise ValueError("topology JSON 'orphans' is not an object")

    keys = set()
    for okey, kind in KINDS.items():
        lst = orphans.get(okey, [])
        if not isinstance(lst, list):
            raise ValueError("orphans.%s is not a list" % okey)
        for entry in lst:
            name = normalize_entry(entry)
            if name is None:
                print("WARNING: unparseable orphan entry in %s: %r" % (okey, entry),
                      file=sys.stderr)
                continue
            keys.add("%s %s" % (kind, name))
    return keys, topic_count


def load_baseline(path):
    if not os.path.exists(path):
        return None
    with open(path, "r", encoding="utf-8") as f:
        return {line.strip() for line in f if line.strip()
                and not line.startswith("#")}


def main(argv=None):
    desc = (__doc__ or "Orphan-ratchet gate").splitlines()[0]
    ap = argparse.ArgumentParser(description=desc)
    ap.add_argument("--rebaseline", action="store_true",
                    help="write the current orphan set to the baseline file")
    ap.add_argument("--topology", default=os.environ.get("MEEPT_ORPHAN_TOPOLOGY"),
                    help="topology JSON (default docs/generated/bus-topology.json)")
    ap.add_argument("--baseline", default=os.environ.get("MEEPT_ORPHAN_BASELINE"),
                    help="baseline file (default scripts/orphan-baseline.txt)")
    args = ap.parse_args(argv)

    topology = args.topology or os.path.join(REPO_ROOT, "docs", "generated", "bus-topology.json")
    baseline_path = args.baseline or os.path.join(REPO_ROOT, "scripts", "orphan-baseline.txt")

    if not os.path.exists(topology):
        print("ERROR: topology JSON not found: %s (run `make graphs`)" % topology,
              file=sys.stderr)
        return 1

    try:
        current, topic_count = current_orphans(topology)
    except (ValueError, json.JSONDecodeError) as e:
        print("ERROR: checker parsed nothing (%s): %s" % (topology, e), file=sys.stderr)
        return 1

    # Non-vacuity floor: zero topics means the generator produced nothing
    # useful — a green gate here would be a silent no-op.
    if topic_count == 0:
        print("ERROR: checker parsed nothing — 0 bus topics in %s. "
              "The connectivity generator output is empty or malformed; "
              "NOT reporting clean." % topology, file=sys.stderr)
        return 1

    if args.rebaseline:
        header = (
            "# Orphan-ratchet baseline for scripts/orphan-check.py.\n"
            "# Stock of known unwired bus topics (name-only keys; line offsets\n"
            "# are never part of the key). Regenerate deliberately with\n"
            "# `python3 scripts/orphan-check.py --rebaseline` and commit the\n"
            "# diff WITH justification (same policy as the deadcode baseline).\n"
        )
        with open(baseline_path, "w", encoding="utf-8") as f:
            f.write(header)
            for key in sorted(current):
                f.write(key + "\n")
        print("Rebaselined: wrote %d orphan entries to %s" % (len(current), baseline_path))
        return 0

    baseline = load_baseline(baseline_path)
    if baseline is None:
        print("ERROR: baseline file not found: %s\n"
              "Create it with: python3 scripts/orphan-check.py --rebaseline"
              % baseline_path, file=sys.stderr)
        return 1

    new = current - baseline
    resolved = baseline - current

    if new:
        print("ORPHAN RATCHET: %d NEW orphan topic(s)/handler(s) with no "
              "consumer side (not in scripts/orphan-baseline.txt):" % len(new),
              file=sys.stderr)
        for key in sorted(new):
            print("  + %s" % key, file=sys.stderr)
        print("", file=sys.stderr)
        print("Fix by wiring the consuming side (subscriber for a published "
              "topic, publisher/RPC client for a subscribed topic), or accept "
              "the orphan deliberately:\n"
              "  python3 scripts/orphan-check.py --rebaseline   "
              "(then commit the baseline diff with justification)", file=sys.stderr)
        return 1

    print("orphan-check: clean (%d orphan(s) in current set, all baselined; "
          "%d topic(s) scanned)" % (len(current), topic_count))
    if resolved:
        print("  note: %d baselined orphan(s) no longer present — "
              "consider `--rebaseline` to ratchet the stock down" % len(resolved))
    return 0


if __name__ == "__main__":
    sys.exit(main())
