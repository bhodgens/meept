"""contract_io.py - minimal JSON5 reading + surgical editing for the jev
command-gate contract file (jev-command-gate leaf 03).

The contract (config/jev-command-gate.contract.json5) is HAND-AUTHORED JSON5:
comments are load-bearing documentation and the score_question string must
stay byte-identical with internal/llm/command_score.go. Freeze therefore does
NOT re-serialize the file; it performs a surgical textual edit of the
gate_threshold block and hashes the resulting bytes (sha256) into the
training manifest.

stdlib only (harness rule: no new third-party deps).
"""

import hashlib
import json
import re


class ContractError(Exception):
    """Raised when the contract file cannot be parsed or edited safely."""


def _strip_comments(text):
    """Remove // and /* */ comments outside string literals.

    Character-scanner, string-aware: a '//' inside a JSON string (e.g. a
    'https://...' URL or the escaped-two-char '\\\\n' rule text) is preserved.
    """
    out = []
    i = 0
    n = len(text)
    in_string = False
    while i < n:
        ch = text[i]
        if in_string:
            out.append(ch)
            if ch == "\\" and i + 1 < n:
                out.append(text[i + 1])
                i += 2
                continue
            if ch == '"':
                in_string = False
            i += 1
            continue
        if ch == '"':
            in_string = True
            out.append(ch)
            i += 1
            continue
        if ch == "/" and i + 1 < n and text[i + 1] == "/":
            while i < n and text[i] != "\n":
                i += 1
            continue
        if ch == "/" and i + 1 < n and text[i + 1] == "*":
            end = text.find("*/", i + 2)
            if end == -1:
                raise ContractError("unterminated block comment")
            i = end + 2
            continue
        out.append(ch)
        i += 1
    return "".join(out)


def load_contract(path):
    """Parse the contract file into a dict (minimal JSON5: comments + plain
    objects; the contract uses no other JSON5 extensions)."""
    text = open(path, "r", encoding="utf-8").read()
    cleaned = _strip_comments(text)
    # Tolerate trailing commas (defensive; the contract does not use them).
    cleaned = re.sub(r",(\s*[}\]])", r"\1", cleaned)
    try:
        return json.loads(cleaned)
    except json.JSONDecodeError as exc:
        raise ContractError("contract %s does not parse: %s" % (path, exc))


def gate_threshold_block_text(text):
    """Return the gate_threshold object's text span (for surgical edits)."""
    match = re.search(r'"gate_threshold"\s*:\s*\{', text)
    if not match:
        raise ContractError("gate_threshold block not found")
    start = match.start()
    depth = 0
    i = match.end() - 1
    in_string = False
    while i < len(text):
        ch = text[i]
        if in_string:
            if ch == "\\":
                i += 2
                continue
            if ch == '"':
                in_string = False
        elif ch == '"':
            in_string = True
        elif ch == "{":
            depth += 1
        elif ch == "}":
            depth -= 1
            if depth == 0:
                return start, i + 1
        i += 1
    raise ContractError("unterminated gate_threshold block")


def freeze_threshold(path, value, status="frozen"):
    """Surgically set gate_threshold.value + .status in the contract file.

    Returns the new sha256 hex digest of the file bytes. Refuses to run when
    the gate_threshold block does not carry the 03-eval frozen_by marker
    (the contract must designate this leaf as the freezer).
    """
    text = open(path, "r", encoding="utf-8").read()
    start, end = gate_threshold_block_text(text)
    block = text[start:end]
    if 'frozen_by: 03-eval' not in block.replace('"', "").replace(" ", "") \
            and "03-eval" not in block:
        raise ContractError(
            "gate_threshold block lacks the frozen_by: 03-eval marker; "
            "refusing to freeze")
    new_block, n_val = re.subn(
        r'("value"\s*:\s*)([0-9.]+)',
        lambda m: m.group(1) + repr(float(value)),
        block,
        count=1,
    )
    if n_val != 1:
        raise ContractError("gate_threshold.value not found")
    new_block, n_status = re.subn(
        r'("status"\s*:\s*")[^"]*(")',
        lambda m: m.group(1) + status + m.group(2),
        new_block,
        count=1,
    )
    if n_status != 1:
        raise ContractError("gate_threshold.status not found")
    new_text = text[:start] + new_block + text[end:]
    data = new_text.encode("utf-8")
    with open(path, "wb") as fh:
        fh.write(data)
    return hashlib.sha256(data).hexdigest()


def sha256_file(path):
    with open(path, "rb") as fh:
        return hashlib.sha256(fh.read()).hexdigest()
