"""Action permission gating for the autonomous agent.

Every action the agent attempts (file I/O, shell commands, network
requests, ...) is routed through the :class:`PermissionManager` which
decides whether to allow, deny, or request human confirmation based on:

* a risk-level classification for each action type,
* path allow/block lists from the security configuration, and
* special-case rules for financial operations.

HIGH and CRITICAL actions can optionally require explicit user
confirmation via an :class:`asyncio.Future` handshake.
"""

from __future__ import annotations

import asyncio
import enum
import fnmatch
import logging
import re
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from meept.models.config_schema import SecurityConfig

logger = logging.getLogger(__name__)


# ---------------------------------------------------------------------------
# Risk classification
# ---------------------------------------------------------------------------


class RiskLevel(enum.IntEnum):
    """Severity tiers for agent-initiated actions.

    Uses :class:`~enum.IntEnum` so that comparisons (``>=``, ``<``, etc.)
    work naturally without custom dunder methods.
    """

    SAFE = 0
    LOW = 1
    MEDIUM = 2
    HIGH = 3
    CRITICAL = 4


@dataclass(slots=True)
class ActionPermission:
    """Descriptor for a single permitted action category."""

    action: str
    risk_level: RiskLevel
    description: str
    requires_confirmation: bool


# ---------------------------------------------------------------------------
# Built-in action rules
# ---------------------------------------------------------------------------

# Base risk table.  The PermissionManager may dynamically elevate risk for
# specific invocations (e.g. ``shell_execute`` with ``rm -rf``).

_BUILTIN_RULES: dict[str, ActionPermission] = {
    "file_read": ActionPermission(
        action="file_read",
        risk_level=RiskLevel.SAFE,
        description="Read a file from the filesystem",
        requires_confirmation=False,
    ),
    "file_write": ActionPermission(
        action="file_write",
        risk_level=RiskLevel.MEDIUM,
        description="Write or overwrite a file on the filesystem",
        requires_confirmation=False,
    ),
    "file_delete": ActionPermission(
        action="file_delete",
        risk_level=RiskLevel.HIGH,
        description="Permanently delete a file from the filesystem",
        requires_confirmation=True,
    ),
    "shell_execute": ActionPermission(
        action="shell_execute",
        risk_level=RiskLevel.MEDIUM,
        description="Execute a shell command",
        requires_confirmation=False,
    ),
    "network_request": ActionPermission(
        action="network_request",
        risk_level=RiskLevel.LOW,
        description="Make an outbound HTTP/HTTPS request",
        requires_confirmation=False,
    ),
    "send_message": ActionPermission(
        action="send_message",
        risk_level=RiskLevel.MEDIUM,
        description="Send a message to a user or external service",
        requires_confirmation=False,
    ),
    "install_package": ActionPermission(
        action="install_package",
        risk_level=RiskLevel.HIGH,
        description="Install a software package on the system",
        requires_confirmation=True,
    ),
    "system_modify": ActionPermission(
        action="system_modify",
        risk_level=RiskLevel.CRITICAL,
        description="Modify system-level configuration or settings",
        requires_confirmation=True,
    ),
}

# Shell commands that bump ``shell_execute`` to HIGH.
_DANGEROUS_COMMANDS_RE = re.compile(
    r"\b(rm\s+-rf|mkfs|dd\s+if=|chmod\s+-R|chown\s+-R|shutdown|reboot"
    r"|init\s+[06]|systemctl\s+(stop|disable|mask)|kill\s+-9"
    r"|iptables|nft|deluser|userdel|groupdel)\b",
    re.IGNORECASE,
)

# Patterns indicating a financial operation (always blocked when
# ``block_financial`` is set).
_FINANCIAL_PATTERNS_RE = re.compile(
    r"\b(transfer\s+(funds?|money|payment)|send\s+(payment|money|funds?)"
    r"|wire\s+transfer|purchase|buy|sell|trade|withdraw"
    r"|credit\s*card|bank\s*account|routing\s*number"
    r"|cryptocurrency|bitcoin|ethereum|wallet\s*address)\b",
    re.IGNORECASE,
)


# ---------------------------------------------------------------------------
# PermissionManager
# ---------------------------------------------------------------------------


class PermissionManager:
    """Central authority for agent action gating.

    Parameters
    ----------
    config:
        The ``[security]`` section of the application config.
    """

    def __init__(self, config: SecurityConfig) -> None:
        self._config = config

        # Pre-expand glob patterns for path matching.
        self._allowed_globs: list[str] = [
            str(Path(p).expanduser()) for p in config.allowed_paths
        ]
        self._blocked_globs: list[str] = [
            str(Path(p).expanduser()) for p in config.blocked_paths
        ]

        # Pending confirmation futures keyed by a unique request id.
        self._pending: dict[str, asyncio.Future[bool]] = {}
        self._next_id = 0

    # -- Path checking ------------------------------------------------------

    def _is_path_allowed(self, path_str: str) -> tuple[bool, str]:
        """Check *path_str* against allow/block lists.

        Returns ``(allowed, reason)``.
        """
        resolved = str(Path(path_str).expanduser().resolve())

        # Block list takes precedence.
        for pattern in self._blocked_globs:
            if fnmatch.fnmatch(resolved, pattern):
                return False, f"Path matches blocked pattern: {pattern}"

        # If there is an allow list, the path must match at least one entry.
        if self._allowed_globs:
            for pattern in self._allowed_globs:
                if fnmatch.fnmatch(resolved, pattern):
                    return True, "Path is within allowed paths"
            return False, "Path does not match any allowed path pattern"

        return True, "No path restrictions configured"

    def check_path(self, path: str) -> bool:
        """Return ``True`` if *path* is within allowed paths and not blocked.

        Parameters
        ----------
        path:
            Absolute or ``~``-prefixed filesystem path to check.
        """
        allowed, _reason = self._is_path_allowed(path)
        return allowed

    # -- Shell command risk elevation ---------------------------------------

    @staticmethod
    def _evaluate_shell_risk(command: str) -> RiskLevel:
        """Return the effective risk level for a shell command."""
        if _DANGEROUS_COMMANDS_RE.search(command):
            return RiskLevel.HIGH
        return RiskLevel.MEDIUM

    # -- Financial detection ------------------------------------------------

    @staticmethod
    def _is_financial(details: dict[str, Any]) -> bool:
        """Return ``True`` if *details* describe a financial operation."""
        for value in details.values():
            if isinstance(value, str) and _FINANCIAL_PATTERNS_RE.search(value):
                return True
        return False

    # -- Public API ---------------------------------------------------------

    def check_permission(
        self, action: str, details: dict[str, Any] | None = None,
    ) -> tuple[bool, str]:
        """Decide whether *action* with *details* is permitted.

        Returns
        -------
        tuple[bool, str]
            ``(allowed, reason)``
        """
        details = details or {}

        # Look up the base rule.
        rule = _BUILTIN_RULES.get(action)
        if rule is None:
            logger.warning("Unknown action requested: %s", action)
            return False, f"Unknown action: {action}"

        effective_risk = rule.risk_level

        # --- Financial gate ---
        if self._config.block_financial and self._is_financial(details):
            logger.warning("Blocked financial action: %s %s", action, details)
            return False, "Financial operations are blocked by policy"

        # --- Path-based checks for file actions ---
        if action in ("file_read", "file_write", "file_delete"):
            path = details.get("path", "")
            if path:
                allowed, reason = self._is_path_allowed(path)
                if not allowed:
                    logger.warning(
                        "Path denied for %s: %s (%s)", action, path, reason,
                    )
                    return False, reason

        # --- Shell command risk elevation ---
        if action == "shell_execute":
            command = details.get("command", "")
            effective_risk = self._evaluate_shell_risk(command)

        # --- Confirmation gating ---
        needs_confirm = (
            (effective_risk >= RiskLevel.HIGH and self._config.require_confirmation_high)
            or (
                effective_risk >= RiskLevel.CRITICAL
                and self._config.require_confirmation_critical
            )
        )

        if needs_confirm:
            logger.info(
                "Action %s (risk=%s) requires user confirmation",
                action,
                effective_risk.name,
            )
            return False, (
                f"Action '{action}' has risk level {effective_risk.name} and requires "
                f"user confirmation before execution"
            )

        logger.debug("Action %s permitted (risk=%s)", action, effective_risk.name)
        return True, "Permitted"

    # -- Async confirmation handshake ---------------------------------------

    def request_confirmation(
        self, action: str, details: dict[str, Any] | None = None,
    ) -> tuple[str, asyncio.Future[bool]]:
        """Create a confirmation request for a high-risk action.

        Returns
        -------
        tuple[str, asyncio.Future[bool]]
            A unique request ID and a future that resolves to ``True``
            (confirmed) or ``False`` (denied) once the human responds.
        """
        loop = asyncio.get_running_loop()
        future: asyncio.Future[bool] = loop.create_future()

        request_id = f"confirm-{self._next_id}"
        self._next_id += 1
        self._pending[request_id] = future

        logger.info(
            "Confirmation requested for %s (id=%s, details=%s)",
            action,
            request_id,
            details,
        )
        return request_id, future

    def resolve_confirmation(self, request_id: str, *, approved: bool) -> None:
        """Resolve a pending confirmation.

        Parameters
        ----------
        request_id:
            The ID returned by :meth:`request_confirmation`.
        approved:
            ``True`` to allow the action, ``False`` to deny.
        """
        future = self._pending.pop(request_id, None)
        if future is None:
            logger.warning("No pending confirmation for id=%s", request_id)
            return
        if not future.done():
            future.set_result(approved)
            logger.info(
                "Confirmation %s resolved: %s",
                request_id,
                "approved" if approved else "denied",
            )
