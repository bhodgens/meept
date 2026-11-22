#!/usr/bin/env python3
"""Enforce the root AGENTS.md size budget, pointer freshness, and table rules.

Run: python3 scripts/agents-check.py [--rebaseline]

Checks:
  * size: AGENTS.md <= 16 KiB (16384 bytes).
  * pointers: every repo path token written as `internal/...`, `docs/...`,
    etc. in AGENTS.md exists on disk. Directory tokens may end in '/'.
  * routing: every doc path named in the routing table at the top of
    AGENTS.md exists; the voice pointer names docs/agents/voice-invariants.md.
  * coverage (ratchet): every top-level package directory in internal/, cmd/,
    and pkg/ appears in the Key Components table. The baseline tolerates the
    117 pre-existing directories that are not named there; a NEW directory
    must be added to the table. --rebaseline is a deliberate human step.
"""

from __future__ import annotations

import argparse
import os
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
AGENTS = ROOT / "AGENTS.md"
BASELINE = ROOT / "scripts" / "agents-baseline.txt"
MAX_BYTES = 16 * 1024

TABLE_START = "### Key Components"
TABLE_END = "## Critical Invariants"
ROUTE_START = "## Invariant doc routing table"
ROUTE_END = "## Build and Test Commands"

PATH_PREFIX = re.compile(
    r"^(?:\.?/)?(?:internal|cmd|pkg|docs|scripts|tools|ui|e2e|config|\.githooks)/"
)
BAD_CHARS = set(" *:<>$()[]|,;=")


def skip_token(candidate: str) -> bool:
    """True for placeholders or symbols that are not filesystem paths."""
    if any(ch in candidate for ch in BAD_CHARS):
        return True
    if candidate in SYMBOL_NAMES or candidate in NON_PATH_TOKENS:
        return True
    return candidate.startswith(("http://", "https://"))


# Backtick spans without newlines, so multi-line fenced blocks cannot yield
# giant false path tokens.
INLINE_CODE = re.compile(r"`([^`\n]*?)`")

# Path-like tokens anywhere in prose, including fenced routing blocks.
PATH_LIKE = re.compile(
    r"(?<![\w/])(?:internal|cmd|pkg|docs|scripts|tools|ui|e2e|config|\.githooks)"
    r"/[A-Za-z0-9_./-]*[A-Za-z0-9_/-]"
)
# Fully-qualified symbol names that are not filesystem paths. Check the
# leading segment as well so a broken docs/*.md pointer is never skipped.
SYMBOL_NAMES = {
    "pkg/id.Generate",
    "internal/tui/tableutil.Size",
    "internal/tui/tableutil.SetRows",
    "internal/auth.Store",
}
NON_PATH_TOKENS = {"e2e/bench", "./internal/..."}



def path_tokens(text: str) -> set[str]:
    out: set[str] = set()
    for match in INLINE_CODE.finditer(text):
        candidate = match.group(1).strip()
        if not PATH_PREFIX.match(candidate):
            continue
        if skip_token(candidate):
            continue
        out.add(candidate)
    return out


def table_universe() -> set[str]:
    dirs: set[str] = set()
    for top in ("internal", "cmd", "pkg"):
        base = ROOT / top
        for child in base.iterdir():
            if child.is_dir() and not child.name.startswith("."):
                if child.name in {"testdata", "vendor", "__pycache__"}:
                    continue
                dirs.add(f"{top}/{child.name}")
    return dirs


def baseline_universe() -> set[str]:
    if not BASELINE.exists():
        return set()
    return {
        line.strip()
        for line in BASELINE.read_text(encoding="utf-8").splitlines()
        if line.strip() and not line.startswith("#")
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--rebaseline", action="store_true")
    args = parser.parse_args()

    text = AGENTS.read_text(encoding="utf-8")
    raw = AGENTS.read_bytes()
    failures: list[str] = []

    # 1. Size budget.
    if len(raw) > MAX_BYTES:
        failures.append(
            f"size: {len(raw)} bytes exceeds {MAX_BYTES} byte cap by "
            f"{len(raw) - MAX_BYTES} bytes"
        )
    else:
        print(f"size: {len(raw)} / {MAX_BYTES} bytes OK")

    # 2. Pointer freshness across root and every routed doc.
    def collect_tokens(body: str) -> set[str]:
        found: set[str] = set(path_tokens(body))
        for match in PATH_LIKE.finditer(body):
            candidate = match.group(0)
            if not skip_token(candidate):
                found.add(candidate)
        return found

    all_tokens = collect_tokens(text)
    routed_docs = sorted((ROOT / "docs" / "agents").glob("*.md"))
    for doc in routed_docs:
        for token in collect_tokens(doc.read_text(encoding="utf-8")):
            all_tokens.add(token)
    missing = sorted(
        token for token in all_tokens
        if not (ROOT / token.rstrip("/")).exists()
    )
    if missing:
        failures.append("broken path pointers: " + ", ".join(missing))
    else:
        print(f"pointers: all path tokens exist ({len(routed_docs)} routed docs)")

    # 3. Routing table pointers.
    if ROUTE_START not in text or ROUTE_END not in text:
        failures.append("routing: marker not found; table was restructured")
    else:
        route = text[text.index(ROUTE_START):text.index(ROUTE_END)]
        required = [
            "internal/llm/AGENTS.md",
            "internal/agent/AGENTS.md",
            "internal/comm/AGENTS.md",
            "internal/voice/AGENTS.md",
            "docs/agents/voice-invariants.md",
            "invariants.md",
            "commands.md",
        ]
        missing = [p for p in required if p not in route]
        if missing:
            failures.append("routing pointers missing: " + ", ".join(missing))
        else:
            # Resolve the bare docs/agents names as well as full paths.
            resolved = [
                p if p.startswith(("docs/", "internal/", "cmd/")) else f"docs/agents/{p}"
                for p in required
            ]
            absent = [p for p in resolved if not (ROOT / p).exists()]
            if absent:
                failures.append("routing docs absent on disk: " + ", ".join(absent))
            else:
                print("routing: required pointers present")

    # 4. Coverage ratchet.
    if TABLE_START not in text or TABLE_END not in text:
        failures.append("coverage: Key Components table marker not found")
    else:
        table = text[text.index(TABLE_START):text.index(TABLE_END)]
        universe = table_universe()
        covered = {d for d in universe if d in table}
        tolerated = baseline_universe()
        new_uncovered = sorted(universe - covered - tolerated)
        if args.rebaseline:
            uncovered = sorted(universe - covered)
            BASELINE.write_text(
                "# Root AGENTS.md intentionally omitted package directories.\n"
                + "\n".join(uncovered) + "\n",
                encoding="utf-8",
            )
            print(f"rebaselined: {len(uncovered)} tolerated directories")
            return 0
        if new_uncovered:
            failures.append(
                "new package directories absent from Key Components table: "
                + ", ".join(new_uncovered)
            )
        else:
            print(f"coverage: {len(covered)} packages listed; no new omissions")

    if failures:
        for failure in failures:
            print(f"FAIL: {failure}", file=sys.stderr)
        return 1
    print("agents-check: OK")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
