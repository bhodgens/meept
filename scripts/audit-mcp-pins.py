#!/usr/bin/env python3
"""Audit the shipped MCP catalog for unpinned npx/uvx commands.

Read-only, stdlib-only. Mirrors internal/tools/mcp/version_pin.go's
ValidateVersionPin semantics exactly, so the static gate and the runtime
refusal agree:

  * only argv[0] containing "npx" or "uvx" is checked (native binaries and
    HTTP servers need no pin);
  * leading flags (e.g. npx's -y) are skipped before the package name;
  * npx needs @scope/pkg@version or pkg@version (a scoped package has two
    '@', so the version separator is the LAST one);
  * uvx needs pkg==version.

JSON5 comments are blanked outside string literals before the command
arrays are read; no file is ever written.
"""
from __future__ import annotations

import argparse
import json
import os
import re
import sys

DEFAULT_CATALOG = os.path.join(
    os.path.dirname(os.path.dirname(os.path.abspath(__file__))),
    "config",
    "mcp_servers.json5",
)

_COMMAND_RE = re.compile(r'"command"\s*:\s*\[')
_STRING_RE = re.compile(r'"(?:[^"\\]|\\.)*"')


def strip_json5_comments(text: str) -> str:
    """Blank // and # comments outside strings, preserving all offsets."""
    out = list(text)
    i, n = 0, len(text)
    in_string = escaped = False
    while i < n:
        ch = text[i]
        if in_string:
            if escaped:
                escaped = False
            elif ch == "\\":
                escaped = True
            elif ch == '"':
                in_string = False
            i += 1
            continue
        if ch == '"':
            in_string = True
            i += 1
            continue
        if (ch == "/" and i + 1 < n and text[i + 1] == "/") or ch == "#":
            while i < n and text[i] != "\n":
                out[i] = " "
                i += 1
            continue
        i += 1
    return "".join(out)


def line_of(text: str, offset: int) -> int:
    """1-based source line for a character offset (newlines are preserved)."""
    return text.count("\n", 0, offset) + 1


def command_arrays(text: str):
    """Yield (line_number, argv) for each "command": [...] in the catalog."""
    for match in _COMMAND_RE.finditer(text):
        open_bracket = text.find("[", match.start())
        if open_bracket < 0:
            continue
        depth = 0
        in_string = escaped = False
        end = None
        for i in range(open_bracket, len(text)):
            ch = text[i]
            if in_string:
                if escaped:
                    escaped = False
                elif ch == "\\":
                    escaped = True
                elif ch == '"':
                    in_string = False
                continue
            if ch == '"':
                in_string = True
            elif ch == "[":
                depth += 1
            elif ch == "]":
                depth -= 1
                if depth == 0:
                    end = i
                    break
        if end is None:
            continue
        # Flat string arrays by contract; token-based decode tolerates a
        # trailing comma inside the array (valid JSON5, invalid JSON).
        argv = []
        for token in _STRING_RE.findall(text[open_bracket + 1 : end]):
            try:
                argv.append(json.loads(token))
            except (ValueError, TypeError):
                pass
        yield line_of(text, match.start()), argv


def is_pinned(argv: list[str]) -> bool:
    """Return True when argv satisfies ValidateVersionPin's rules."""
    if not argv:
        return True
    bin_name = argv[0]
    if "npx" not in bin_name and "uvx" not in bin_name:
        return True
    if len(argv) < 2:
        return False
    idx = 1
    while idx < len(argv) and argv[idx].startswith("-"):
        idx += 1
    if idx >= len(argv):
        return False
    pkg = argv[idx]
    if "uvx" in bin_name:
        return "==" in pkg
    at = pkg.rfind("@")
    return at > 0


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--catalog",
        default=DEFAULT_CATALOG,
        help="path to mcp_servers.json5 (default: the shipped catalog)",
    )
    args = parser.parse_args(argv)
    try:
        with open(args.catalog, "r", encoding="utf-8") as handle:
            raw = handle.read()
    except OSError as exc:
        print(f"{args.catalog}: cannot read catalog: {exc}", file=sys.stderr)
        return 1

    text = strip_json5_comments(raw)
    violations = []
    checked = 0
    for line, command in command_arrays(text):
        if not command or ("npx" not in command[0] and "uvx" not in command[0]):
            continue
        checked += 1
        if not is_pinned(command):
            violations.append(
                f"{args.catalog}:{line}: unpinned MCP command: {' '.join(command)}"
            )
    for violation in violations:
        print(violation)
    if violations:
        return 1
    print(f"OK: {checked} npx/uvx MCP command(s) pinned in {args.catalog}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
