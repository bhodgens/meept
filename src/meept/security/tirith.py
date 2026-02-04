"""Async tirith scanner -- pre-execution security gate for shell commands.

Tirith detects homograph attacks, ANSI injection, pipe-to-shell patterns,
dotfile attacks, insecure transport, and credential exposure.

Fail mode: open -- BLOCKED findings block execution, WARNING findings log
and continue.  If tirith is not installed, commands pass through (graceful
degradation).
"""

from __future__ import annotations

import asyncio
import logging
import re
from dataclasses import dataclass

log = logging.getLogger(__name__)

_TIRITH_TIMEOUT = 2.0  # seconds

# Cached availability -- ``None`` means not yet checked.
_tirith_available: bool | None = None


@dataclass(frozen=True, slots=True)
class TirithResult:
    """Parsed result from a ``tirith check`` invocation."""

    blocked: bool  # True only for BLOCKED findings
    warning: bool  # True for WARNING findings (logged, not blocked)
    severity: str | None  # e.g. "CRITICAL", "MEDIUM"
    rule_id: str | None  # e.g. "non_ascii_hostname"
    message: str | None  # Full tirith output


# Pattern to extract ``[SEVERITY] rule_id`` from tirith output.
_DETAIL_RE = re.compile(r"\[(\w+)]\s+(\S+)")


async def check_tirith_available(binary: str = "tirith") -> bool:
    """Check whether the *binary* is reachable on ``$PATH``.

    The result is cached in the module-level ``_tirith_available`` sentinel
    so that we only log / probe once per process.
    """
    global _tirith_available  # noqa: PLW0603

    if _tirith_available is not None:
        return _tirith_available

    try:
        proc = await asyncio.create_subprocess_exec(
            binary, "--version",
            stdout=asyncio.subprocess.DEVNULL,
            stderr=asyncio.subprocess.DEVNULL,
        )
        await asyncio.wait_for(proc.wait(), timeout=_TIRITH_TIMEOUT)
        _tirith_available = proc.returncode == 0
    except (FileNotFoundError, asyncio.TimeoutError, OSError):
        _tirith_available = False

    if not _tirith_available:
        log.info("tirith binary not found (%s) -- security scanning disabled", binary)

    return _tirith_available


async def scan_command(
    command: str,
    binary: str = "tirith",
) -> TirithResult | None:
    """Run ``tirith check -- <command>`` and parse the output.

    Returns
    -------
    TirithResult
        Parsed scan result.
    None
        If tirith is not installed or the subprocess timed out (graceful
        degradation -- commands are not blocked).
    """
    if not await check_tirith_available(binary):
        return None

    try:
        proc = await asyncio.create_subprocess_exec(
            binary, "check", "--", command,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
        )
        stdout_bytes, stderr_bytes = await asyncio.wait_for(
            proc.communicate(),
            timeout=_TIRITH_TIMEOUT,
        )
    except asyncio.TimeoutError:
        log.warning("tirith timed out scanning command -- allowing execution")
        return None
    except (FileNotFoundError, OSError) as exc:
        log.warning("tirith execution failed (%s) -- allowing execution", exc)
        return None

    output = stderr_bytes.decode("utf-8", errors="replace")
    if not output:
        output = stdout_bytes.decode("utf-8", errors="replace")

    severity: str | None = None
    rule_id: str | None = None

    # Extract severity and rule_id from the line following the action keyword.
    for line in output.splitlines():
        m = _DETAIL_RE.search(line)
        if m:
            severity, rule_id = m.group(1), m.group(2)
            break

    message = output.strip() or None

    if "BLOCKED" in output:
        return TirithResult(
            blocked=True,
            warning=False,
            severity=severity,
            rule_id=rule_id,
            message=message,
        )

    if "WARNING" in output:
        return TirithResult(
            blocked=False,
            warning=True,
            severity=severity,
            rule_id=rule_id,
            message=message,
        )

    # Clean scan.
    return TirithResult(
        blocked=False,
        warning=False,
        severity=None,
        rule_id=None,
        message=message,
    )
