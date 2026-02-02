"""Tests for the action permission manager."""

from __future__ import annotations

import pytest

from meept.models.config_schema import SecurityConfig
from meept.security.permissions import PermissionManager, RiskLevel


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _default_config(**overrides) -> SecurityConfig:
    """Build a SecurityConfig with test defaults, overriding as needed."""
    defaults = {
        "sanitize_inputs": True,
        "require_confirmation_high": True,
        "require_confirmation_critical": True,
        "block_financial": True,
        "allowed_paths": ["~/*"],
        "blocked_paths": ["~/.ssh/*", "~/.gnupg/*"],
    }
    defaults.update(overrides)
    return SecurityConfig(**defaults)


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------


def test_safe_action_allowed() -> None:
    """A SAFE-level action (file_read) should be allowed."""
    pm = PermissionManager(_default_config())
    allowed, reason = pm.check_permission("file_read", {"path": "~/documents/notes.txt"})

    assert allowed is True
    assert reason == "Permitted"


def test_blocked_path() -> None:
    """Access to ~/.ssh/* should be blocked by the path blocklist."""
    pm = PermissionManager(_default_config())
    allowed, reason = pm.check_permission("file_read", {"path": "~/.ssh/id_rsa"})

    assert allowed is False
    assert "blocked" in reason.lower() or "denied" in reason.lower() or "Blocked" in reason


def test_financial_blocked() -> None:
    """Financial operations should always be blocked when block_financial is True."""
    pm = PermissionManager(_default_config(block_financial=True))
    allowed, reason = pm.check_permission(
        "send_message",
        {"content": "Please transfer funds to account 12345"},
    )

    assert allowed is False
    assert "financial" in reason.lower() or "Financial" in reason


def test_high_risk_needs_confirmation() -> None:
    """HIGH-risk actions should return needs_confirmation when require_confirmation_high is set."""
    pm = PermissionManager(_default_config(require_confirmation_high=True))

    # file_delete is HIGH risk.
    allowed, reason = pm.check_permission("file_delete", {"path": "~/tmp/junk.txt"})

    assert allowed is False
    assert "confirmation" in reason.lower()
