#!/usr/bin/env python3
"""Detect Dart `Enum.name` (and sibling) extension shadowing.

In Dart 2.15+, every `enum` declaration synthesizes a `String get name`
property returning the enum value's identifier. In Dart 3.x, the same
synthesis applies to `int get index`. If an extension method declares
a getter with the same name on an enum type, the synthesized property
wins at every call site, and the compiler does NOT warn. This is a
language-level footgun that silently breaks code.

Origin: this script was written after Round 6 of the meept codebase
review found that `SearchScopeX` declared `String get name`, which was
silently shadowed by `Enum.name` at every call site. Search requests
were sent with the wrong scope identifier. The fix was to rename the
getter to `apiValue` (see `ui/flutter_ui/lib/models/api_models.dart`).

This script walks `ui/flutter_ui/lib/**/*.dart`, parses extensions, and
flags any extension getter whose name collides with a Dart-synthesized
enum property:

  - `name`        (highest value — the SearchScopeX bug)
  - `index`
  - `hashCode`    (inherited from Object, rarely a real collision)
  - `runtimeType` (inherited from Object)

For each match, it attempts to determine whether the `on` type is an
enum declared in the same file. If the type cannot be resolved
(imported from another file, generic type parameter, etc.), the finding
is reported as "POTENTIAL" requiring manual verification; otherwise it
is reported as "ERROR" (definite enum collision).

Exit codes:
  0 = no findings
  1 = at least one finding
  2 = script error (e.g., flutter_ui directory not found)

Usage:
    scripts/audit-dart-enum-name-shadow.py
    scripts/audit-dart-enum-name-shadow.py --root path/to/flutter_ui
    scripts/audit-dart-enum-name-shadow.py --strict   # treat POTENTIAL as ERROR
    scripts/audit-dart-enum-name-shadow.py file.dart [file2.dart ...]  # one-off

References:
  - Round 6 finding: SearchScopeX declared `String get name` which was
    silently shadowed by `Enum.name` at every call site.
  - Fix commit: renamed to `displayName` and `apiValue`.
"""

from __future__ import annotations

import argparse
import re
import sys
from dataclasses import dataclass
from pathlib import Path


# Names Dart synthesizes or inherits on enums. The synthesized members
# (name, index) are the high-value targets because they are commonly
# overridden in extensions and there is no compiler warning.
SHADOWED_NAMES: dict[str, str] = {
    "name": "Enum.name",
    "index": "Enum.index",
    "hashCode": "Object.hashCode",
    "runtimeType": "Object.runtimeType",
}

# Base severity per name. `name` and `index` are the dangerous ones
# because they are synthesized directly on Enum. `hashCode` and
# `runtimeType` are inherited from Object and rarely collide in
# practice (extensions on enum cannot actually override these — Dart
# forbids it — but we flag for completeness in case of generic typing).
SEVERITY_BY_NAME: dict[str, str] = {
    "name": "ERROR",
    "index": "ERROR",
    "hashCode": "WARNING",
    "runtimeType": "WARNING",
}

# Built-in / common types that are definitely NOT enums. Used to silence
# false positives when an extension is on a known non-enum type.
NON_ENUM_TYPES: frozenset[str] = frozenset(
    {
        "String",
        "int",
        "double",
        "bool",
        "num",
        "List",
        "Map",
        "Set",
        "Iterable",
        "Object",
        "dynamic",
        "BuildContext",
        "Subject",
        "Widget",
        "BuildContext",
        "Stream",
        "Future",
        "Duration",
    }
)


# ---------------------------------------------------------------------------
# Source cleaning (so pattern matching ignores comments and strings)
# ---------------------------------------------------------------------------


def _strip_block_comments(source: str) -> str:
    """Strip /* ... */ block comments from Dart source.

    Block comments in Dart do not nest. We replace them with an equal
    number of newlines so that line numbers in the cleaned source still
    match the original file.
    """
    out: list[str] = []
    i = 0
    n = len(source)
    while i < n:
        if source[i] == "/" and i + 1 < n and source[i + 1] == "*":
            j = source.find("*/", i + 2)
            if j == -1:
                j = n
            else:
                j += 2
            chunk = source[i:j]
            out.append("\n" * chunk.count("\n"))
            i = j
            continue
        out.append(source[i])
        i += 1
    return "".join(out)


def _strip_string_literals(source: str) -> str:
    """Replace string literal contents with spaces (preserving newlines).

    Handles: '...', "...', '''...''' (triple), \"\"\"...\"\"\" (triple).
    This prevents accidental matches of `extension`/`on`/`enum`/`get`
    keywords inside string literals.
    """
    out: list[str] = []
    i = 0
    n = len(source)
    while i < n:
        ch = source[i]
        if ch not in ("'", '"'):
            out.append(ch)
            i += 1
            continue
        # Detect triple-quoted strings.
        if i + 2 < n and source[i + 1] == ch and source[i + 2] == ch:
            quote = ch * 3
            j = source.find(quote, i + 3)
            if j == -1:
                j = n
            else:
                j += 3
            chunk = source[i:j]
            out.append('"""')
            for k in range(3, len(chunk) - 3):
                out.append("\n" if chunk[k] == "\n" else " ")
            out.append('"""')
            i = j
            continue
        # single-line string
        j = i + 1
        while j < n:
            if source[j] == "\\" and j + 1 < n:
                j += 2
                continue
            if source[j] == source[i]:
                j += 1
                break
            if source[j] == "\n":
                break
            j += 1
        chunk = source[i:j]
        out.append(chunk[0])
        for k in range(1, len(chunk) - 1):
            out.append("\n" if chunk[k] == "\n" else " ")
        if len(chunk) > 1:
            out.append(chunk[-1])
        i = j
    return "".join(out)


def _strip_line_comment(line: str) -> str:
    """Strip a trailing `//` line comment, respecting quoted strings."""
    quote: str | None = None
    i = 0
    n = len(line)
    while i < n:
        ch = line[i]
        if quote is not None:
            if ch == "\\" and i + 1 < n:
                i += 2
                continue
            if ch == quote:
                quote = None
            i += 1
            continue
        if ch in ("'", '"'):
            quote = ch
            i += 1
            continue
        if ch == "/" and i + 1 < n and line[i + 1] == "/":
            return line[:i]
        i += 1
    return line


def _strip_source(source: str) -> str:
    """Clean a Dart source file for pattern matching.

    Steps:
      1. Strip block comments (preserving newlines for line numbers).
      2. Strip string literal contents (preserving newlines).
    Line comments are stripped per-line during extension body scanning.
    """
    source = _strip_block_comments(source)
    source = _strip_string_literals(source)
    return source


# ---------------------------------------------------------------------------
# Brace matching
# ---------------------------------------------------------------------------


def _match_brace(source: str, open_pos: int) -> int:
    """Given the position of an opening `{`, return the position of the
    matching closing `}`. Returns -1 if unmatched.
    """
    assert source[open_pos] == "{"
    depth = 0
    i = open_pos
    n = len(source)
    while i < n:
        ch = source[i]
        if ch == "{":
            depth += 1
        elif ch == "}":
            depth -= 1
            if depth == 0:
                return i
        i += 1
    return -1


def _line_of_pos(source: str, pos: int) -> int:
    """Return the 1-based line number for a character offset."""
    return source.count("\n", 0, pos) + 1


# ---------------------------------------------------------------------------
# Patterns
# ---------------------------------------------------------------------------

# Match an extension header. Supports:
#   extension Name on Type {
#   extension Name<T> on Type<T> {
#   extension _Private on Type {
#   extension on Type {                       (unnamed)
# Group 1 is the `on` type name (bare identifier).
_EXT_HEADER_RE = re.compile(
    r"""
    ^                           # start of line (multiline mode)
    [\t ]*                      # optional leading whitespace
    extension\b
    \s+
    (?:                         # optional name (may be unnamed)
        [A-Za-z_]\w*            # bare name
        (?:<[^>]*>)?            # optional type params
    )?
    \s+
    on\b
    \s+
    ([A-Za-z_]\w*)              # group(1): the `on` type name (bare)
    """,
    re.MULTILINE | re.VERBOSE,
)

# Match an enum declaration header: `enum Name {`.
# Group 1 is the enum name.
_ENUM_HEADER_RE = re.compile(
    r"""
    ^                           # start of line
    [\t ]*                      # optional leading whitespace
    enum\b
    \s+
    ([A-Za-z_]\w*)              # group(1): enum name
    \s*
    (?:<[^>]*>)?                # optional mix-in / type params
    \s*
    (?:implements\s+[^\{]+)?    # optional implements clause
    \s*
    \{                          # opening brace
    """,
    re.MULTILINE | re.VERBOSE,
)


def _build_getter_re(name: str) -> re.Pattern[str]:
    """Build a regex that matches a getter named `name` in an extension body.

    Matches:
      String get name => ...
      int get name { ... }
      get name => ...            (no return type)
      external String get name;

    Rejects:
      String get displayName
      String getName()
      String get nameX
      String get name<T>          (generic method, not a getter)
      String get name()           (a method, not a getter)
    """
    return re.compile(
        r"""
        \b
        get
        \s+
        (?P<member>{name})
        \b
        (?!\s*[(<])              # not followed by ( (method) or < (generic)
        """.format(name=re.escape(name)),
        re.MULTILINE | re.VERBOSE,
    )


# ---------------------------------------------------------------------------
# Data structures
# ---------------------------------------------------------------------------


@dataclass
class Finding:
    """One lint hit."""

    path: Path
    lineno: int                  # 1-based line of the getter declaration
    ext_name: str                # extension name or "<unnamed>"
    on_type: str                 # bare type name from `on` clause
    getter: str                  # the getter name (`name`, `index`, ...)
    shadowed_by: str             # e.g. "Enum.name"
    severity: str                # "ERROR", "WARNING", or "INFO"
    confirmed_enum: bool         # True if we resolved the on-type to an enum


@dataclass
class _ExtensionBlock:
    """A parsed extension header + body span."""

    header_pos: int               # offset of the `extension` keyword
    body_open_pos: int            # offset of the `{`
    body_close_pos: int           # offset of the matching `}`
    name: str                     # extension name or "<unnamed>"
    on_type: str                  # bare type name from `on` clause


# ---------------------------------------------------------------------------
# Scanner
# ---------------------------------------------------------------------------


def _find_extensions(cleaned: str) -> list[_ExtensionBlock]:
    """Locate all extension declarations in cleaned source."""
    out: list[_ExtensionBlock] = []
    for m in _EXT_HEADER_RE.finditer(cleaned):
        search_from = m.end()
        open_idx = cleaned.find("{", search_from)
        if open_idx == -1:
            continue
        close_idx = _match_brace(cleaned, open_idx)
        if close_idx == -1:
            continue
        header_text = cleaned[m.start():open_idx]
        name_match = re.search(
            r"extension\s+([A-Za-z_]\w*(?:<[^>]*>)?)", header_text
        )
        if name_match:
            raw_name = name_match.group(1)
            plain_name = re.sub(r"<.*", "", raw_name)
        else:
            plain_name = "<unnamed>"
        on_type = m.group(1)
        out.append(
            _ExtensionBlock(
                header_pos=m.start(),
                body_open_pos=open_idx,
                body_close_pos=close_idx,
                name=plain_name,
                on_type=on_type,
            )
        )
    return out


def _find_enum_names(cleaned: str) -> set[str]:
    """Return the set of enum type names declared in this file."""
    return {m.group(1) for m in _ENUM_HEADER_RE.finditer(cleaned)}


def _scan_extension_body(
    cleaned: str,
    ext: _ExtensionBlock,
    file_path: Path,
    enum_names: set[str],
) -> list[Finding]:
    """Scan one extension body for shadowed getter declarations."""
    findings: list[Finding] = []
    body = cleaned[ext.body_open_pos + 1:ext.body_close_pos]
    body_offset = ext.body_open_pos + 1

    # Determine whether the on-type is definitely an enum (in-file) or
    # definitely not an enum (built-in primitive / collection).
    on_type = ext.on_type
    is_confirmed_enum = on_type in enum_names
    is_known_non_enum = on_type in NON_ENUM_TYPES

    for shadowed_name, shadowed_by in SHADOWED_NAMES.items():
        getter_re = _build_getter_re(shadowed_name)
        for gm in getter_re.finditer(body):
            abs_pos = body_offset + gm.start()
            lineno = _line_of_pos(cleaned, abs_pos)

            # Strip trailing line comment for accurate context.
            line_start = cleaned.rfind("\n", 0, abs_pos) + 1
            line_end = cleaned.find("\n", abs_pos)
            if line_end == -1:
                line_end = len(cleaned)
            raw_line = cleaned[line_start:line_end]
            cleaned_line = _strip_line_comment(raw_line).rstrip()

            if is_confirmed_enum:
                severity = SEVERITY_BY_NAME[shadowed_name]
                confirmed = True
            elif is_known_non_enum:
                # Extension on String/int/List/... — `name` getter is fine.
                continue
            else:
                # Could not resolve the type. Treat as potential.
                base = SEVERITY_BY_NAME[shadowed_name]
                severity = (
                    "WARNING" if base == "ERROR" else "INFO"
                )
                confirmed = False

            findings.append(
                Finding(
                    path=file_path,
                    lineno=lineno,
                    ext_name=ext.name,
                    on_type=on_type,
                    getter=shadowed_name,
                    shadowed_by=shadowed_by,
                    severity=severity,
                    confirmed_enum=confirmed,
                )
            )
    return findings


# ---------------------------------------------------------------------------
# Driver
# ---------------------------------------------------------------------------


def audit_path(path: Path) -> list[Finding]:
    """Audit a single Dart file."""
    try:
        raw = path.read_text(encoding="utf-8")
    except (OSError, UnicodeDecodeError) as exc:
        print(f"warning: failed to read {path}: {exc}", file=sys.stderr)
        return []
    cleaned = _strip_source(raw)
    enum_names = _find_enum_names(cleaned)
    extensions = _find_extensions(cleaned)
    out: list[Finding] = []
    for ext in extensions:
        out.extend(_scan_extension_body(cleaned, ext, path, enum_names))
    return out


def audit(root: Path, strict: bool = False) -> list[Finding]:
    """Run the audit against `root/lib/**/*.dart` (or `root` if it's a file)."""
    if root.is_file():
        findings = audit_path(root)
    else:
        lib_dir = root / "lib"
        scan_root = lib_dir if lib_dir.is_dir() else root
        dart_files = sorted(scan_root.rglob("*.dart"))
        findings = []
        for path in dart_files:
            # Skip generated files.
            if path.name.endswith(".g.dart") or path.name.endswith(".freezed.dart"):
                continue
            findings.extend(audit_path(path))

    if strict:
        for f in findings:
            if f.severity in ("WARNING", "INFO"):
                f.severity = "ERROR"
                f.confirmed_enum = True  # treat as definite under strict
    return findings


def format_finding(f: Finding) -> str:
    """Format a finding as a single line for stdout."""
    status = "CONFIRMED" if f.confirmed_enum else "POTENTIAL"
    return (
        f"{f.path}:{f.lineno}: [{f.severity}] "
        f"Extension '{f.ext_name}' on '{f.on_type}' declares "
        f"'{f.getter}' which is shadowed by Dart 3.x {f.shadowed_by} "
        f"({status} enum collision — verify manually if POTENTIAL)"
    )


def main(argv: list[str] | None = None) -> int:
    repo_root = Path(__file__).resolve().parent.parent
    default_flutter = repo_root / "ui" / "flutter_ui"

    parser = argparse.ArgumentParser(
        description=(
            "Detect Dart Enum.name (and sibling) extension shadowing. "
            "See header docstring for background on the bug class."
        )
    )
    parser.add_argument(
        "paths",
        nargs="*",
        type=Path,
        help=(
            "Files or directories to scan. Defaults to "
            "ui/flutter_ui/lib/**/*.dart."
        )
    )
    parser.add_argument(
        "--root",
        type=Path,
        default=default_flutter,
        help=(
            "Flutter app root to scan when no positional paths given "
            "(default: %(default)s)."
        ),
    )
    parser.add_argument(
        "--strict",
        action="store_true",
        help="Treat POTENTIAL findings (unresolved on-type) as ERROR.",
    )
    parser.add_argument(
        "--quiet",
        action="store_true",
        help="Suppress the summary line on stderr.",
    )
    args = parser.parse_args(argv)

    all_findings: list[Finding] = []

    if args.paths:
        for p in args.paths:
            if not p.exists():
                print(f"error: {p} does not exist", file=sys.stderr)
                return 2
            all_findings.extend(audit(p, strict=args.strict))
    else:
        root: Path = args.root
        if not root.exists():
            print(f"error: {root} does not exist", file=sys.stderr)
            return 2
        all_findings.extend(audit(root, strict=args.strict))

    # Stable ordering: by path, then line number, then getter name.
    all_findings.sort(
        key=lambda f: (str(f.path), f.lineno, f.getter)
    )

    for f in all_findings:
        print(format_finding(f))

    if not args.quiet:
        if all_findings:
            print(
                f"\n{len(all_findings)} finding(s). "
                "Rename the getter (e.g. `apiValue`, `displayName`) or "
                "remove the extension.",
                file=sys.stderr,
            )
        else:
            print("OK: no Enum.name shadowing found.", file=sys.stderr)

    return 1 if all_findings else 0


if __name__ == "__main__":
    sys.exit(main())
