#!/usr/bin/env python3
"""
Audit codebase for hand-rolled ASCII case-conversion functions that corrupt
multi-byte UTF-8 characters.

The anti-pattern: code that does byte-level arithmetic like `c += 'a' - 'A'`
or `c |= 0x20` to convert uppercase ASCII to lowercase (or vice-versa).
When the input contains multi-byte UTF-8 characters (é, ñ, 漢, etc.), these
operations corrupt the byte sequence because they operate on individual bytes
rather than Unicode code points.

This script detects:
  1. Byte arithmetic on characters: `c + 'a' - 'A'`, `c - 'a' + 'A'`,
     `c | 0x20`, `c & ~0x20`, `c += 32`, `c -= 32`
  2. Manual toLower/toUpper loops checking `>= 'A' && <= 'Z'`
  3. Range checks against ASCII bounds followed by arithmetic

Supported languages: Go, Dart, JavaScript/TypeScript, Python, Java, C/C++

Usage:
    python3 scripts/audit-utf8-byte-arithmetic.py [path ...]

If no paths given, scans from the repository root.
"""

import os
import re
import sys
from pathlib import Path
from typing import List, Tuple

# ── Pattern definitions ────────────────────────────────────────────────

# Byte arithmetic patterns (language-agnostic, operate on source text)
BYTE_ARITH_PATTERNS = [
    # c + 'a' - 'A'  or  c - 'a' + 'A'  (and variants with spaces)
    (re.compile(r"""\+\s*['"]a['"]\s*-\s*['"]A['"]"""), "add a-A offset (ASCII lowercasing)"),
    (re.compile(r"""-\s*['"]a['"]\s*\+\s*['"]A['"]"""), "sub a-A offset (ASCII uppercasing)"),
    (re.compile(r"""\+\s*['"]A['"]\s*-\s*['"]a['"]"""), "add A-a offset (ASCII uppercasing)"),
    (re.compile(r"""-\s*['"]A['"]\s*\+\s*['"]a['"]"""), "sub A-a offset (ASCII lowercasing)"),
    # c | 0x20  (ASCII lowercase via bit flip)
    (re.compile(r'\|\s*0x20\b'), "bitwise OR 0x20 (ASCII lowercasing)"),
    # c & 0xDF  or  c & 0xdf  (ASCII uppercase via bit mask)
    (re.compile(r'&\s*0x[dD][fF]\b'), "bitwise AND 0xDF (ASCII uppercasing)"),
    (re.compile(r'&\s*~\s*0x20\b'), "bitwise AND ~0x20 (ASCII uppercasing)"),
    # c += 32  or  c -= 32  (ASCII case delta)
    (re.compile(r'[+\-]=\s*32\b'), "add/sub 32 (ASCII case delta)"),
    (re.compile(r'[+\-]=\s*0x20\b'), "add/sub 0x20 (ASCII case delta)"),
]

# Manual ASCII range check patterns: >= 'A' && <= 'Z' or similar
# These are only flagged when followed (within ~5 lines) by arithmetic
RANGE_CHECK_PATTERNS = [
    re.compile(r""">=\s*['"]A['"]\s*&&\s*<=\s*['"]Z['"]"""),
    re.compile(r""">=\s*['"]a['"]\s*&&\s*<=\s*['"]z['"]"""),
    re.compile(r""">=\s*65\b.*<=\s*90\b"""),  # ASCII 'A'=65, 'Z'=90
    re.compile(r""">=\s*97\b.*<=\s*122\b"""),  # ASCII 'a'=97, 'z'=122
    re.compile(r""">=\s*0x41\b.*<=\s*0x5[aA]\b"""),
    re.compile(r""">=\s*0x61\b.*<=\s*0x7[aA]\b"""),
]

# Function name patterns that suggest hand-rolled case conversion
FUNC_NAME_PATTERNS = [
    re.compile(r'func\s+toLower\w*', re.IGNORECASE),
    re.compile(r'func\s+toUpper\w*', re.IGNORECASE),
    re.compile(r'func\s+toLowerASCII\w*'),
    re.compile(r'func\s+toUpperASCII\w*'),
    re.compile(r'func\s+containsIgnoreCase\w*'),
    re.compile(r'func\s[contains]*IgnoreCase\w*', re.IGNORECASE),
    re.compile(r'def\s+to_lower\w*', re.IGNORECASE),
    re.compile(r'def\s+to_upper\w*', re.IGNORECASE),
]

# File extensions to scan
SCAN_EXTENSIONS = {
    '.go', '.dart', '.js', '.ts', '.jsx', '.tsx',
    '.py', '.java', '.c', '.cpp', '.h', '.hpp', '.rs',
}

# Directories to skip
SKIP_DIRS = {
    '.git', 'node_modules', 'vendor', '.dart_tool', 'build',
    '.gradle', '__pycache__', '.idea', '.vscode', 'dist',
    'bin', '.syft', '.next', 'coverage', 'site',
}

SKIP_FILE_PREFIXES = ('.min.',)

# File basenames to skip — the audit scripts themselves contain the regex
# patterns they search for (in comments/docstrings), which produce false
# positives on themselves.
SKIP_FILE_BASENAMES = {
    'audit-utf8-byte-arithmetic.py',
    'audit-dart-enum-name-shadow.py',
}

# ── Scanner ────────────────────────────────────────────────────────────


def should_scan(path: Path) -> bool:
    """Check if a file should be scanned based on extension and path."""
    # Skip by extension
    if path.suffix not in SCAN_EXTENSIONS:
        return False
    # Skip minified files
    name = path.name
    for prefix in SKIP_FILE_PREFIXES:
        if prefix in name:
            return False
    # Skip audit scripts themselves (they describe the patterns they look for)
    if name in SKIP_FILE_BASENAMES:
        return False
    # Skip files in skip directories
    parts = path.parts
    for skip in SKIP_DIRS:
        if skip in parts:
            return False
    return True


# Patterns that indicate TRUE byte-level case conversion (vs. stdlib delegation).
_UNSAFE_CASE_BODY_PATTERNS = [
    re.compile(r"'\s*[A-Za-z]\s*'\s*-\s*'\s*[A-Za-z]\s*'"),  # 'a' - 'A'
    re.compile(r"\+\s*32\b"),                                   # += 32
    re.compile(r"-\s*32\b"),                                   # -= 32
    re.compile(r"\|\s*0x20"),                                   # |= 0x20
    re.compile(r"&\s*~0x20"),                                   # &= ~0x20
    re.compile(r">=\s*'A'\s*&&\s*<=\s*'Z'"),                    # >= 'A' && <= 'Z'
    re.compile(r">=\s*97\s*&&\s*<=\s*122"),                     # ASCII numeric form
]


def _extract_function_body(lines: List[str], start_idx: int) -> str:
    """Return the body of a Go/C/Java/JS function starting at start_idx.

    Looks for the opening `{` after the func signature, then returns all
    lines up to (but not including) the matching closing `}` at column 0
    (top-level Go funcs are column-0 closed). Best-effort — never raises.
    """
    if start_idx >= len(lines):
        return ""
    out: List[str] = []
    brace_depth = 0
    seen_open = False
    for j in range(start_idx, min(start_idx + 100, len(lines))):
        line = lines[j]
        out.append(line)
        # Track brace depth, ignoring braces inside string/char literals
        # (simple state machine — good enough for the typical case).
        in_str = False
        str_ch = ''
        for ch in line:
            if in_str:
                if ch == str_ch and (len(out) == 0 or out[-1][-1:] != '\\'):
                    in_str = False
            else:
                if ch in '"\'`':
                    in_str = True
                    str_ch = ch
                elif ch == '{':
                    brace_depth += 1
                    seen_open = True
                elif ch == '}':
                    brace_depth -= 1
                    if seen_open and brace_depth == 0:
                        return '\n'.join(out)
        # Bounded so we don't scan a 1000-line function.
        if j - start_idx > 80:
            return '\n'.join(out)
    return '\n'.join(out)


def _body_has_unsafe_caseconv(body: str) -> bool:
    """Return True if function body contains byte-level ASCII case conversion.

    Functions whose body uses strings.ToLower / strings.ToUpper / etc. are
    safe — only flag bodies that contain the actual unsafe patterns.
    """
    if not body:
        # Could not extract body; be conservative and flag.
        return True
    for pat in _UNSAFE_CASE_BODY_PATTERNS:
        if pat.search(body):
            return True
    return False


def scan_file(path: Path) -> List[Tuple[int, str, str]]:
    """
    Scan a single file for UTF-8 byte arithmetic hazards.

    Returns list of (line_number, pattern_type, line_content) tuples.
    """
    try:
        text = path.read_text(encoding='utf-8', errors='replace')
    except Exception:
        return []

    findings: List[Tuple[int, str, str]] = []
    lines = text.splitlines()

    for i, line in enumerate(lines, start=1):
        # Skip comment-only lines (rough heuristic)
        stripped = line.strip()
        if stripped.startswith('//') or stripped.startswith('#') or stripped.startswith('/*') or stripped.startswith('*'):
            # But still scan for function name patterns in comments? No.
            continue

        # Check byte arithmetic patterns
        for pattern, desc in BYTE_ARITH_PATTERNS:
            if pattern.search(line):
                findings.append((i, f"byte-arithmetic: {desc}", line.rstrip()))
                break  # one finding per line is enough

        # Check for function name patterns (hand-rolled case conversion).
        # Look-ahead: only flag if the function body actually does byte
        # arithmetic or range checks. A function named containsIgnoreCase
        # that delegates to strings.ToLower is safe; only flag if its body
        # contains `+ 32`, `- 32`, `| 0x20`, or `>= 'A' && <= 'Z'`.
        for pattern in FUNC_NAME_PATTERNS:
            if pattern.search(line):
                # Find function body (up to next closing brace at column 0).
                body = _extract_function_body(lines, i - 1)
                if _body_has_unsafe_caseconv(body):
                    findings.append((i, "hand-rolled case function", line.rstrip()))
                break

        # Check for ASCII range check (flag for manual case conversion)
        for pattern in RANGE_CHECK_PATTERNS:
            if pattern.search(line):
                findings.append((i, "ASCII range check (potential manual case conversion)", line.rstrip()))
                break

    return findings


def scan_directory(root: str) -> List[Tuple[str, int, str, str]]:
    """
    Scan a directory tree.

    Returns list of (file_path, line_number, pattern_type, line_content).
    """
    results: List[Tuple[str, int, str, str]] = []
    root_path = Path(root)

    if root_path.is_file():
        if should_scan(root_path):
            for line_no, ptype, content in scan_file(root_path):
                results.append((str(root_path), line_no, ptype, content))
        return results

    for dirpath, dirnames, filenames in os.walk(root):
        # Prune skip directories
        dirnames[:] = [d for d in dirnames if d not in SKIP_DIRS]

        for fname in filenames:
            fpath = Path(dirpath) / fname
            if not should_scan(fpath):
                continue
            for line_no, ptype, content in scan_file(fpath):
                results.append((str(fpath), line_no, ptype, content))

    return results


# ── Main ───────────────────────────────────────────────────────────────


def main() -> int:
    args = sys.argv[1:] or ['.']
    paths = args

    all_findings: List[Tuple[str, int, str, str]] = []

    for p in paths:
        if not os.path.exists(p):
            print(f"warning: path does not exist: {p}", file=sys.stderr)
            continue
        all_findings.extend(scan_directory(p))

    if not all_findings:
        print("[OK] No UTF-8 byte arithmetic hazards found.")
        return 0

    # Group by file
    by_file: dict[str, list] = {}
    for fpath, line_no, ptype, content in all_findings:
        by_file.setdefault(fpath, []).append((line_no, ptype, content))

    print(f"[FOUND] {len(all_findings)} potential UTF-8 byte arithmetic hazard(s) "
          f"in {len(by_file)} file(s):\n")

    for fpath, items in sorted(by_file.items()):
        # Make path relative if possible
        try:
            rel = str(Path(fpath).relative_to(Path.cwd()))
        except ValueError:
            rel = fpath
        print(f"  {rel}")
        for line_no, ptype, content in items:
            print(f"    :{line_no}  [{ptype}]")
            print(f"    |  {content}")
        print()

    # Summary by pattern type
    type_counts: dict[str, int] = {}
    for _, _, ptype, _ in all_findings:
        type_counts[ptype] = type_counts.get(ptype, 0) + 1

    print("Summary by pattern type:")
    for ptype, count in sorted(type_counts.items(), key=lambda x: -x[1]):
        print(f"  {count:3d}  {ptype}")

    return 1 if all_findings else 0


if __name__ == '__main__':
    sys.exit(main())
