#!/usr/bin/env python3
"""Derive file-level e2e path_map keys from scenario evidence for broad catch-all prefixes.

The problem: internal/daemon/ (and other hub prefixes) maps EVERY file in the
package to the same multi-suite list, so a one-line voice_wiring.go change runs
9 suites (~10 min pre-commit) even though only 2 of them touch that file. The
manifest's scenarios[] ALREADY carry the ground truth: each scenario lists the
exact source paths its suite exercises. This script turns that evidence into
exact-file path_map keys and shrinks the catch-all to whole-subject suites.

Rule per narrow-prefix P (see NARROW_PREFIXES):
  - For every scenario whose paths[] include a path under P, its suite is
    registered under an exact-file key for each such path (value = the union
    of suites citing that file, replacing any existing value for the key).
  - The catch-all P keeps ONLY suites listed in KEEP_ON_CATCHALL[P] — suites
    that genuinely test the whole subject (e.g. daemon boot/pid hygiene) and
    therefore must run for any change under P. Everything else leaves P and
    rides its exact-file keys (plus whatever other prefixes already list it).

Scenario paths that are DIRECTORY prefixes under P (e.g. internal/comm/slack/)
keep their directory key; files are detected by "/" absence in the tail.

Usage:
  scripts/e2e-pathmap-narrow.py            # dry-run: show the plan
  scripts/e2e-pathmap-narrow.py --write    # rewrite e2e/manifest.json

pinned by e2e/suites/manifest/manifest_completeness_test.go
(TestNarrowCatchallKeysAreScenarioDerived). Keep the two in sync.
"""

import json
import sys

MANIFEST = "e2e/manifest.json"

# Prefixes whose catch-all is over-selecting, and the suites that must STAY on
# the catch-all because they test the whole subject rather than named files.
# Adding a prefix here is a campaign: its scenario evidence must already name
# specific files (TestEveryScenarioPathIsCovered guarantees paths exist).
NARROW_PREFIXES = ["internal/daemon/"]
KEEP_ON_CATCHALL = {
    "internal/daemon/": [
        # Boots a scratch daemon and drives SIGTERM/restart/pid-file hygiene:
        # any daemon change can break boot, so it stays catch-all-selected.
        "daemon-lifecycle",
    ],
}


def main() -> int:
    write = "--write" in sys.argv
    m = json.load(open(MANIFEST))
    pm = m["path_map"]
    suite_names = {s["name"] for s in m["suites"]}

    for prefix in NARROW_PREFIXES:
        keep = KEEP_ON_CATCHALL.get(prefix, [])
        # suite -> exact files under prefix it cites
        cites: dict[str, set[str]] = {}
        # suite -> dir keys under prefix its scenarios cite (kept as-is)
        dir_cites: dict[str, set[str]] = {}
        for sc in m["scenarios"]:
            s = sc["suite"]
            if s not in suite_names:
                continue
            for p in sc.get("paths", []):
                if not p.startswith(prefix) or p == prefix:
                    continue
                tail = p[len(prefix):]
                if "/" in tail:
                    # deeper path (could be a dir); treat by trailing slash
                    if tail.endswith("/"):
                        dir_cites.setdefault(s, set()).add(p)
                    else:
                        # path is itself a dir-like prefix under P? use as key
                        dir_cites.setdefault(s, set()).add(p if tail.endswith("/") else p)
                else:
                    cites.setdefault(s, set()).add(p)
            # also catch dir-style scenario paths like internal/daemon/ with more segments
        # dir-style scenario paths under prefix: any path p under prefix whose
        # manifest counterpart is registered as a dir key (ends with /)
        for sc in m["scenarios"]:
            for p in sc.get("paths", []):
                if p.startswith(prefix) and p != prefix and not p[len(prefix):].rstrip("/"):
                    dir_cites.setdefault(sc["suite"], set()).add(p)

        # build derived file keys
        derived: dict[str, set[str]] = {}
        for s, files in cites.items():
            for f in files:
                derived.setdefault(f, set()).add(s)
        for s, dirs in dir_cites.items():
            for d in dirs:
                derived.setdefault(d, set()).add(s)

        print(f"== {prefix}")
        for k in sorted(derived):
            old = pm.get(k)
            note = "" if old is None else (" (was %s)" % sorted(old)) if sorted(old) != sorted(derived[k]) else " (unchanged)"
            print(f"   {k} -> {sorted(derived[k])}{note}")
        # suites currently on catch-all that leave it
        leaving = [s for s in pm.get(prefix, []) if s not in keep]
        print(f"   catch-all {prefix} keeps {keep}, drops {sorted(leaving)}")

        if write:
            for k in sorted(derived):
                pm[k] = sorted(derived[k])
            pm[prefix] = [s for s in keep]

    if write:
        from datetime import date
        m["generated"] = date.today().isoformat()
        with open(MANIFEST, "w") as f:
            json.dump(m, f, indent=2)
            f.write("\n")
        print("wrote", MANIFEST)
    else:
        print("dry-run (use --write)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
