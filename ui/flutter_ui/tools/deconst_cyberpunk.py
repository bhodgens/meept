#!/usr/bin/env python3
"""Remove `const` keywords from constant expressions that reference
CyberpunkColors.* (which became non-const runtime getters).

Strategy: mask out strings/comments, find every standalone `const` token,
match its following bracket group, and if `CyberpunkColors.` appears inside
that region, delete the const keyword. Edits applied right-to-left so
offsets stay valid.
"""
import os
import re
import sys

ROOT = sys.argv[1] if len(sys.argv) > 1 else 'lib'
OPEN = {'(': ')', '[': ']', '{': '}'}
CLOSE = {v: k for k, v in OPEN.items()}

CONST_RE = re.compile(r'\bconst\b')
CC_RE = re.compile(r'\bCyberpunkColors\s*\.')
# constructor path after `const`: Foo, Foo.bar, List<Foo> etc.
TYPE_RE = re.compile(r'[A-Za-z_$][\w$]*(?:\.[A-Za-z_$][\w$]*)*')


def mask(src: str) -> str:
    """Replace string contents and comments with spaces (keep length)."""
    out = list(src)
    i, n = 0, len(src)
    while i < n:
        c = src[i]
        if c == '/' and i + 1 < n and src[i + 1] == '/':
            j = src.find('\n', i)
            j = n if j == -1 else j
            for k in range(i, j):
                out[k] = ' '
            i = j
        elif c == '/' and i + 1 < n and src[i + 1] == '*':
            j = src.find('*/', i + 2)
            j = n if j == -1 else j + 2
            for k in range(i, j):
                if src[k] != '\n':
                    out[k] = ' '
            i = j
        elif c in ("'", '"') or (c == 'r' and i + 1 < n and src[i + 1] in ("'", '"')):
            q = src[i + 1] if c == 'r' else c
            start = i + 1 if c == 'r' else i
            triple = src.startswith(q * 3, start)
            if triple:
                endq = q * 3
                j = src.find(endq, start + 3)
                j = n if j == -1 else j + 3
                # keep quotes visible so bracket logic sees nothing weird;
                # mask interior only
                for k in range(start + 3, min(j, n) - 3 if j < n else n):
                    if src[k] != '\n':
                        out[k] = ' '
                i = j
            else:
                j = start + 1
                while j < n:
                    if src[j] == '\\':
                        j += 2
                        continue
                    if src[j] == q:
                        j += 1
                        break
                    if src[j] == '\n':  # unterminated single-line safety
                        break
                    j += 1
                for k in range(start + 1, min(j - 1 if j <= n and src[j-1:j] == q else j, n)):
                    if src[k] != '\n':
                        out[k] = ' '
                i = j
        elif c == '$' and i > 0 and out[i - 1] == '{':
            # interpolation braces inside already-masked strings: skip
            i += 1
        else:
            i += 1
    return ''.join(out)


def match_bracket(masked: str, open_idx: int) -> int:
    """Return index of closing bracket for the opener at open_idx, or -1."""
    stack = []
    i, n = open_idx, len(masked)
    while i < n:
        c = masked[i]
        if c in OPEN:
            stack.append(c)
        elif c in CLOSE:
            if not stack or stack[-1] != CLOSE[c]:
                return -1
            stack.pop()
            if not stack:
                return i
        i += 1
    return -1


def process(path: str) -> int:
    with open(path, encoding='utf-8') as f:
        src = f.read()
    masked = mask(src)

    edits = []  # (start, end) spans of `const` to delete
    for m in CONST_RE.finditer(masked):
        idx = m.end()
        n = len(masked)
        # skip whitespace
        while idx < n and masked[idx].isspace():
            idx += 1
        if idx >= n:
            continue
        c = masked[idx]
        if c not in OPEN:
            tm = TYPE_RE.match(masked, idx)
            if not tm:
                continue
            idx = tm.end()
            while idx < n and masked[idx] == ' ':
                idx += 1
            # optional generic args: List<Color>(...)
            if idx < n and masked[idx] == '<':
                gclose = match_bracket(masked, idx)
                if gclose == -1:
                    continue
                idx = gclose + 1
                while idx < n and masked[idx].isspace():
                    idx += 1
            if idx >= n or masked[idx] != '(':
                continue
            c = '('
        close_idx = match_bracket(masked, idx)
        if close_idx == -1:
            continue
        region = masked[idx:close_idx + 1]
        if CC_RE.search(region):
            edits.append((m.start(), m.end()))

    if not edits:
        return 0

    out = src
    for s, e in sorted(edits, reverse=True):
        out = out[:s] + out[e:]

    with open(path, 'w', encoding='utf-8') as f:
        f.write(out)
    return len(edits)


def main():
    total_files = total_edits = 0
    for dirpath, _dirnames, filenames in os.walk(ROOT):
        for fn in filenames:
            if fn.endswith('.dart'):
                p = os.path.join(dirpath, fn)
                count = process(p)
                if count:
                    total_files += 1
                    total_edits += count
                    print(f'{p}: removed {count} const')
    print(f'---\n{total_edits} const keywords removed across {total_files} files')


if __name__ == '__main__':
    main()
