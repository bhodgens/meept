#!/usr/bin/env python3
"""
Audit codebase for secret-material escape paths out of package `secrets`.

The security contract (internal/secrets/broker.go): a Broker holds real
secret values in memory; the EXPORTED surface splits into two lanes:

  - ChildValue / ChildValues return the PLACEHOLDER string
    "MEEPT_SECRET:<name>" — the ONLY values that may reach child process
    environments, command lines, logs, or bus payloads.
  - Value (and the unexported resolve) return the REAL value — for
    in-process consumers only (an outbound API token source compiled into
    the daemon, the egress proxy).

Rules this script mechanically pins:

  1. Any call of Broker.Value / .resolve( on a *secrets.Broker outside
     internal/secrets must belong to a file in ALLOWED_VALUE_CALLERS
     (below). That map is the human-review surface: a new caller is
     added only with a written justification.
  2. In an allowlisted caller, the variable that received the Value
     result must not appear on a log/print call line, in an env-entry
     string concatenation, or in an os.Setenv argument — the three
     escape surfaces detectable from source text.

Text-level analysis cannot do full dataflow; this audit is the tripwire,
not a proof. It catches the realistic accident shapes (a val variable
drifting into an slog line or an env entry over time).

Exit code 1 on any finding; prints file:line per finding.

Usage:
    python3 scripts/audit-secrets-value-callers.py [path ...]

With no paths, scans Go files under internal/ and cmd/ from the repo root.
"""

import re
import sys
from pathlib import Path

# File-level allowlist: Go files allowed to call Broker.Value / resolve.
# Each entry: repo-relative path -> justification. A new caller lands
# here only after this audit's escape-surface checks pass AND a human
# reviews the justification.
ALLOWED_VALUE_CALLERS = {
    # The token source for the [github] block: in-process outbound API
    # auth (githubsvc.SecretsTokenSource). Hands the token to the
    # go-github HTTP client only; never logs it; errors wrap ErrNoToken
    # without the value.
    "internal/githubsvc/token.go": (
        "SecretsTokenSource returns the real token to the in-process "
        "HTTP client (go-github) only; never logged, never placed in "
        "child envs or command args."
    ),
}

# A call whose result may carry real secret material. The receiver name is
# part of the anchor: only `.Broker.Value(` / `.broker.resolve(` shapes
# match (plus the bare in-package `resolve(` inside internal/secrets,
# which this script skips for rule 1 but still tracks for rule 2 via its
# own call site). A bare `.Value(` would false-positive on bubbles
# textarea/input .Value(), context.Value, store.Value, and similar —
# the "checker matches the CORRECT spelling" trap.
VALUE_CALL = re.compile(r"\.[Bb]roker\.(?:Value|resolve)\(")

# Assignment shape: <names> := or =  ... .Broker.Value(/.broker.resolve(
ASSIGN = re.compile(
    r"^\s*([A-Za-z_][A-Za-z0-9_,\s]*?)\s*(?::)?=\s*.*\.[Bb]roker\.(?:Value|resolve)\("
)

# Log/print call openers (slog + fmt printing).
LOG_CALL = re.compile(
    r"\.(?:Debug|Info|Warn|Error)\(|\bfmt\.Print(?:f|ln)?\(|\bslog\.(?:Debug|Info|Warn|Error)\("
)

# Env-entry string concatenation: "NAME=" + val  |  val + "=..."
ENV_CONCAT = re.compile(r"\"\s*\+\s*[A-Za-z_]|[A-Za-z_][A-Za-z0-9_]*\s*\+\s*\"")

OS_SETENV = re.compile(r"\bos\.Setenv\(")

ERROR_VARS = {"err", "e", "err2", "_"}


def scan_file(path: Path, rel: str):
    """Return a list of (lineno, message) findings for one Go file."""
    try:
        text = path.read_text(encoding="utf-8")
    except (OSError, UnicodeDecodeError):
        return []

    findings = []
    in_secrets_pkg = rel.startswith("internal/secrets/")
    allowed = rel in ALLOWED_VALUE_CALLERS

    value_vars = []  # (decl_lineno, name)
    lines = text.splitlines()
    for i, line in enumerate(lines, 1):
        stripped = line.strip()
        if stripped.startswith("//"):
            continue
        if not VALUE_CALL.search(line):
            continue
        m = ASSIGN.match(line)
        if not m:
            continue
        names = [n.strip() for n in m.group(1).split(",") if n.strip()]
        value_names = [n for n in names if n not in ERROR_VARS]
        if not value_names:
            continue
        if not in_secrets_pkg and not allowed:
            findings.append(
                (i, f"{rel}:{i}: Broker.Value/resolve call outside the "
                    f"allowlist (ALLOWED_VALUE_CALLERS in "
                    f"scripts/audit-secrets-value-callers.py)")
            )
        for n in value_names:
            value_vars.append((i, n))

    if not value_vars:
        return findings

    for i, line in enumerate(lines, 1):
        stripped = line.strip()
        if stripped.startswith("//"):
            continue
        for decl_ln, name in value_vars:
            if i == decl_ln:
                continue
            pat = re.compile(r"\b%s\b" % re.escape(name))
            if not pat.search(line):
                continue
            if LOG_CALL.search(line):
                findings.append(
                    (i, f"{rel}:{i}: value-bearing variable '{name}' on a "
                        f"log/print call line")
                )
            if ENV_CONCAT.search(line) and "=" in line:
                findings.append(
                    (i, f"{rel}:{i}: value-bearing variable '{name}' in an "
                        f"env-entry string concatenation")
                )
            if OS_SETENV.search(line):
                findings.append(
                    (i, f"{rel}:{i}: value-bearing variable '{name}' passed "
                        f"to os.Setenv")
                )
    return findings


def main():
    repo_root = Path(__file__).resolve().parent.parent
    targets = sys.argv[1:]
    if targets:
        files = []
        for t in targets:
            p = Path(t)
            if p.is_dir():
                files.extend(sorted(p.rglob("*.go")))
            elif p.suffix == ".go":
                files.append(p)
    else:
        files = sorted(
            p
            for base in ("internal", "cmd")
            if (repo_root / base).exists()
            for p in (repo_root / base).rglob("*.go")
        )

    findings = []
    for f in files:
        try:
            rel = str(f.relative_to(repo_root))
        except ValueError:
            rel = str(f)
        findings.extend(scan_file(f, rel))

    if findings:
        print("SECRETS AUDIT FAILURES:")
        for _, msg in findings:
            print("  " + msg)
        print(
            "\nIf a finding is a legitimate in-process consumer, fix the "
            "escape surface OR (after human review) add the file to "
            "ALLOWED_VALUE_CALLERS with a justification."
        )
        return 1
    print(
        f"secrets-value-callers audit: OK ({len(files)} Go files scanned, "
        f"{len(ALLOWED_VALUE_CALLERS)} allowlisted caller(s))"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
