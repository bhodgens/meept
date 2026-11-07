#!/usr/bin/env python3
"""Audit: modernc.org/sqlite DSNs must use the `_pragma=name(value)` form.

The registered pure-Go "sqlite" driver (modernc.org/sqlite) honors ONLY
`_pragma=` (and `_time_format`) query keys — applyQueryParams in the
driver. mattn-style keys (`_journal_mode=WAL`, `_busy_timeout=5000`,
`_fk=1`, `_foreign_keys=ON`) are parsed out of the DSN and then silently
DROPPED: WAL never activates, busy_timeout stays 0, FK enforcement stays
off, and no error is ever returned. That is bughunt 2026-11-07
infra-fixes: 22 production sites shipped with pragmas that never reached
SQLite (gatecapture F-1 was the confirmed data-exposure instance).

The cgo driver (github.com/mattn/go-sqlite3, driver name "sqlite3") DOES
honor those keys, so files importing mattn are exempt.

Detection: any Go string literal containing '?' followed by a mattn key,
in a non-test file that does not import mattn/go-sqlite3.

Usage: python3 scripts/audit-sqlite-dsn-pragmas.py  (exit 1 on violations)
"""
import re
import subprocess
import sys

BAD = re.compile(r'\?[^"`]*[&_](_journal_mode|_busy_timeout|_foreign_keys|_fk)\b')

def main() -> int:
    files = subprocess.run(
        ["git", "ls-files", "*.go"],
        capture_output=True, text=True, check=True).stdout.splitlines()
    violations = []
    for path in files:
        if path.endswith("_test.go") or path.startswith("e2e/"):
            continue
        try:
            src = open(path, encoding="utf-8", errors="replace").read()
        except OSError:
            continue
        if "mattn/go-sqlite3" in src:
            continue  # cgo driver: its DSN keys are honored
        if "modernc.org/sqlite" not in src and "database/sql" not in src:
            continue
        for i, line in enumerate(src.splitlines(), 1):
            stripped = line.strip()
            if stripped.startswith("//") or stripped.startswith("*"):
                continue
            if BAD.search(line):
                violations.append((path, i, stripped[:140]))
    if violations:
        print("sqlite-DSN audit: modernc.org/sqlite ignores mattn-style DSN")
        print("keys (_journal_mode/_busy_timeout/_fk/_foreign_keys). Use")
        print("_pragma=name(value) instead. See bughunt 2026-11-07 infra-fixes.\n")
        for p, l, s in violations:
            print(f"  {p}:{l}: {s}")
        print(f"\n{len(violations)} violation(s).")
        return 1
    print("sqlite-DSN audit: all modernc DSNs use the honored _pragma= form.")
    return 0

if __name__ == "__main__":
    sys.exit(main())
