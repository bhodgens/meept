"""Tests for the SQLite-backed SecurityEngine."""

from __future__ import annotations

import asyncio
from pathlib import Path

import pytest

from meept.security.engine import (
    DecisionRecord,
    PermissionDecision,
    SecurityEngine,
    SecurityStats,
)
from meept.security.permissions import RiskLevel


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


@pytest.fixture()
async def engine(tmp_path: Path) -> SecurityEngine:
    """Create and initialise a SecurityEngine backed by a temp database."""
    db_path = tmp_path / "test_security.db"
    eng = SecurityEngine(db_path=db_path)
    await eng.initialize()
    yield eng
    await eng.close()


# ---------------------------------------------------------------------------
# Initialization & Seeding
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_initialize_creates_database(tmp_path: Path) -> None:
    """Initialization should create the SQLite database file."""
    db_path = tmp_path / "security.db"
    eng = SecurityEngine(db_path=db_path)
    await eng.initialize()

    assert db_path.exists()
    await eng.close()


@pytest.mark.asyncio
async def test_seed_defaults_idempotent(tmp_path: Path) -> None:
    """Calling initialize() twice should not duplicate seed data."""
    db_path = tmp_path / "security.db"
    eng = SecurityEngine(db_path=db_path)
    await eng.initialize()
    await eng.close()

    # Re-open and re-initialize.
    eng2 = SecurityEngine(db_path=db_path)
    await eng2.initialize()

    # Should still work without errors.
    decision = await eng2.check("file_read")
    assert decision.allowed is True
    await eng2.close()


# ---------------------------------------------------------------------------
# Base rule lookups
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_safe_action_allowed(engine: SecurityEngine) -> None:
    """SAFE actions like file_read should be permitted."""
    decision = await engine.check("file_read", tool_name="file_read")
    assert decision.allowed is True
    assert decision.risk_level == RiskLevel.SAFE


@pytest.mark.asyncio
async def test_low_risk_allowed(engine: SecurityEngine) -> None:
    """LOW risk actions like network_request should be permitted."""
    decision = await engine.check("network_request", tool_name="network")
    assert decision.allowed is True


@pytest.mark.asyncio
async def test_medium_risk_allowed(engine: SecurityEngine) -> None:
    """MEDIUM risk actions should be allowed (no confirmation required)."""
    decision = await engine.check("file_write", tool_name="file_write")
    assert decision.allowed is True
    assert decision.risk_level == RiskLevel.MEDIUM


@pytest.mark.asyncio
async def test_high_risk_needs_confirmation(engine: SecurityEngine) -> None:
    """HIGH risk actions should require confirmation (denied by default)."""
    decision = await engine.check("file_delete", tool_name="file_delete")
    assert decision.allowed is False
    assert decision.requires_confirmation is True
    assert "confirmation" in decision.reason.lower()


@pytest.mark.asyncio
async def test_critical_risk_needs_confirmation(engine: SecurityEngine) -> None:
    """CRITICAL risk actions should require confirmation."""
    decision = await engine.check("system_modify", tool_name="system_modify")
    assert decision.allowed is False
    assert decision.requires_confirmation is True


# ---------------------------------------------------------------------------
# Command pattern classification
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_critical_command_blocked(engine: SecurityEngine) -> None:
    """Immutable CRITICAL commands (rm -rf /) should be blocked."""
    decision = await engine.check(
        "shell_execute",
        tool_name="shell",
        details={"command": "rm -rf /"},
    )
    assert decision.allowed is False
    assert decision.rule_source == "immutable"


@pytest.mark.asyncio
async def test_fork_bomb_blocked(engine: SecurityEngine) -> None:
    """Fork bomb pattern should be blocked."""
    decision = await engine.check(
        "shell_execute",
        tool_name="shell",
        details={"command": ":(){ :|:& };:"},
    )
    assert decision.allowed is False


@pytest.mark.asyncio
async def test_mkfs_blocked(engine: SecurityEngine) -> None:
    """mkfs should be blocked as CRITICAL."""
    decision = await engine.check(
        "shell_execute",
        tool_name="shell",
        details={"command": "mkfs -t ext4 /dev/sda1"},
    )
    assert decision.allowed is False


@pytest.mark.asyncio
async def test_high_command_needs_confirmation(engine: SecurityEngine) -> None:
    """HIGH-risk commands like rm -rf should require confirmation."""
    decision = await engine.check(
        "shell_execute",
        tool_name="shell",
        details={"command": "rm -rf /tmp/test_dir"},
    )
    assert decision.allowed is False
    assert decision.requires_confirmation is True


@pytest.mark.asyncio
async def test_sudo_needs_confirmation(engine: SecurityEngine) -> None:
    """sudo commands should require confirmation."""
    decision = await engine.check(
        "shell_execute",
        tool_name="shell",
        details={"command": "sudo apt update"},
    )
    assert decision.allowed is False
    assert decision.requires_confirmation is True


@pytest.mark.asyncio
async def test_safe_shell_command_allowed(engine: SecurityEngine) -> None:
    """Read-only commands like ls should be allowed."""
    decision = await engine.check(
        "shell_execute",
        tool_name="shell",
        details={"command": "ls -la /tmp"},
    )
    assert decision.allowed is True


@pytest.mark.asyncio
async def test_git_status_allowed(engine: SecurityEngine) -> None:
    """Read-only git commands should be allowed."""
    decision = await engine.check(
        "shell_execute",
        tool_name="shell",
        details={"command": "git status"},
    )
    assert decision.allowed is True


# ---------------------------------------------------------------------------
# Path rules
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_ssh_path_blocked(engine: SecurityEngine) -> None:
    """Paths matching ~/.ssh/* should be blocked."""
    decision = await engine.check(
        "file_read",
        tool_name="file_read",
        details={"path": "~/.ssh/id_rsa"},
    )
    assert decision.allowed is False
    assert "ssh" in decision.reason.lower() or "block" in decision.reason.lower()


@pytest.mark.asyncio
async def test_aws_path_blocked(engine: SecurityEngine) -> None:
    """Paths matching ~/.aws/* should be blocked."""
    decision = await engine.check(
        "file_read",
        tool_name="file_read",
        details={"path": "~/.aws/credentials"},
    )
    assert decision.allowed is False


@pytest.mark.asyncio
async def test_home_path_allowed(engine: SecurityEngine) -> None:
    """Paths within ~/ (that aren't blocked) should be allowed."""
    decision = await engine.check(
        "file_read",
        tool_name="file_read",
        details={"path": "~/documents/notes.txt"},
    )
    assert decision.allowed is True


# ---------------------------------------------------------------------------
# Financial detection
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_financial_transfer_blocked(engine: SecurityEngine) -> None:
    """Financial operations should be blocked."""
    decision = await engine.check(
        "send_message",
        tool_name="send_message",
        details={"content": "Please transfer funds to account 12345"},
    )
    assert decision.allowed is False
    assert "financial" in decision.reason.lower()


@pytest.mark.asyncio
async def test_cryptocurrency_blocked(engine: SecurityEngine) -> None:
    """Cryptocurrency references should be blocked."""
    decision = await engine.check(
        "send_message",
        tool_name="send_message",
        details={"content": "Send bitcoin to wallet address xyz"},
    )
    assert decision.allowed is False


@pytest.mark.asyncio
async def test_non_financial_allowed(engine: SecurityEngine) -> None:
    """Non-financial messages should be permitted."""
    decision = await engine.check(
        "send_message",
        tool_name="send_message",
        details={"content": "Hello, how are you today?"},
    )
    assert decision.allowed is True


# ---------------------------------------------------------------------------
# Permission overrides (permissionary model)
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_record_and_apply_allow_override(engine: SecurityEngine) -> None:
    """A creator 'allow' override should permit an otherwise gated action."""
    # pip install normally needs confirmation (HIGH risk).
    decision_before = await engine.check(
        "shell_execute",
        tool_name="shell",
        details={"command": "pip install requests"},
    )
    assert decision_before.allowed is False

    # Record an override.
    override_id = await engine.record_override(
        action="shell_execute",
        pattern="pip install*",
        decision="allow",
        reason="Creator approved pip install",
        max_uses=10,
        expires_days=7,
    )
    assert override_id > 0

    # Now the action should be allowed.
    decision_after = await engine.check(
        "shell_execute",
        tool_name="shell",
        details={"command": "pip install requests"},
    )
    assert decision_after.allowed is True
    assert decision_after.override_applied is True


@pytest.mark.asyncio
async def test_record_deny_override(engine: SecurityEngine) -> None:
    """A creator 'deny' override should block an otherwise allowed action."""
    # file_read is normally SAFE.
    decision_before = await engine.check(
        "file_read",
        tool_name="file_read",
        details={"path": "~/documents/notes.txt"},
    )
    assert decision_before.allowed is True

    # Record a deny override.
    await engine.record_override(
        action="file_read",
        pattern="*notes*",
        decision="deny",
        reason="Creator doesn't want notes files accessed",
    )

    # Now the action should be denied.
    decision_after = await engine.check(
        "file_read",
        tool_name="file_read",
        details={"path": "~/documents/notes.txt"},
    )
    assert decision_after.allowed is False
    assert decision_after.override_applied is True


@pytest.mark.asyncio
async def test_override_max_uses_expiry(engine: SecurityEngine) -> None:
    """Overrides should expire after max_uses is reached."""
    await engine.record_override(
        action="shell_execute",
        pattern="pip install*",
        decision="allow",
        reason="Temporary approval",
        max_uses=2,
        expires_days=0,
    )

    # Use 1
    d1 = await engine.check("shell_execute", details={"command": "pip install foo"})
    assert d1.allowed is True

    # Use 2
    d2 = await engine.check("shell_execute", details={"command": "pip install bar"})
    assert d2.allowed is True

    # Use 3 -- override exhausted, should fall back to base rule (HIGH, needs confirmation).
    d3 = await engine.check("shell_execute", details={"command": "pip install baz"})
    assert d3.allowed is False
    assert d3.requires_confirmation is True


# ---------------------------------------------------------------------------
# Immutable rules cannot be overridden
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_immutable_critical_not_overridable(engine: SecurityEngine) -> None:
    """Even with an allow override, immutable CRITICAL commands stay blocked."""
    await engine.record_override(
        action="shell_execute",
        pattern="*",
        decision="allow",
        reason="Override everything",
    )

    # rm -rf / is immutable CRITICAL -- override should not help.
    decision = await engine.check(
        "shell_execute",
        tool_name="shell",
        details={"command": "rm -rf /"},
    )
    assert decision.allowed is False
    assert decision.rule_source == "immutable"


# ---------------------------------------------------------------------------
# Backward-compatible check_permission
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_check_permission_compat(engine: SecurityEngine) -> None:
    """check_permission() should return (bool, str) tuple."""
    allowed, reason = await engine.check_permission("file_read")
    assert isinstance(allowed, bool)
    assert isinstance(reason, str)
    assert allowed is True


# ---------------------------------------------------------------------------
# LLM context generation
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_get_context_for_llm_allowed(engine: SecurityEngine) -> None:
    """Context for allowed actions should show 'Permitted'."""
    decision = await engine.check("file_read", tool_name="file_read")
    ctx = engine.get_context_for_llm(decision, "file_read", {})
    assert "Permitted" in ctx
    assert "SAFE" in ctx


@pytest.mark.asyncio
async def test_get_context_for_llm_denied(engine: SecurityEngine) -> None:
    """Context for denied actions should include restriction warning."""
    decision = await engine.check(
        "shell_execute", tool_name="shell",
        details={"command": "rm -rf /"},
    )
    ctx = engine.get_context_for_llm(decision, "shell_execute", {"command": "rm -rf /"})
    assert "Denied" in ctx
    assert "Do not attempt" in ctx


# ---------------------------------------------------------------------------
# Audit log & stats
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_decision_logging(engine: SecurityEngine) -> None:
    """Every permission check should be logged in the decision log."""
    await engine.check("file_read", tool_name="file_read")
    await engine.check("file_read", tool_name="file_read")
    await engine.check(
        "shell_execute", tool_name="shell",
        details={"command": "rm -rf /"},
    )

    records = await engine.query_log()
    assert len(records) >= 3
    assert all(isinstance(r, DecisionRecord) for r in records)


@pytest.mark.asyncio
async def test_query_log_filters(engine: SecurityEngine) -> None:
    """query_log should support filtering by action and decision."""
    await engine.check("file_read", tool_name="file_read")
    await engine.check(
        "shell_execute", tool_name="shell",
        details={"command": "rm -rf /"},
    )

    allow_records = await engine.query_log(decision="allow")
    deny_records = await engine.query_log(decision="deny")

    assert len(allow_records) >= 1
    assert len(deny_records) >= 1

    shell_records = await engine.query_log(action="shell_execute")
    assert len(shell_records) >= 1


@pytest.mark.asyncio
async def test_get_stats(engine: SecurityEngine) -> None:
    """get_stats should return aggregate statistics."""
    await engine.check("file_read", tool_name="file_read")
    await engine.check(
        "shell_execute", tool_name="shell",
        details={"command": "rm -rf /"},
    )

    stats = await engine.get_stats()
    assert isinstance(stats, SecurityStats)
    assert stats.total_decisions >= 2
    assert stats.total_allows >= 1
    assert stats.total_denies >= 1


# ---------------------------------------------------------------------------
# /etc/passwd path rule
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_etc_passwd_blocked(engine: SecurityEngine) -> None:
    """/etc/passwd should be blocked."""
    decision = await engine.check(
        "file_read",
        tool_name="file_read",
        details={"path": "/etc/passwd"},
    )
    assert decision.allowed is False
    # Blocked either by the explicit path rule or because it's outside allowed paths.
    assert "block" in decision.reason.lower() or "allowed pattern" in decision.reason.lower()


# ---------------------------------------------------------------------------
# Self-replication pattern detection (Plan P6)
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_self_replication_git_clone_blocked(engine: SecurityEngine) -> None:
    """git clone of meept should be blocked as CRITICAL self-replication."""
    decision = await engine.check(
        "shell_execute",
        tool_name="shell",
        details={"command": "git clone https://github.com/user/meept.git"},
    )
    assert decision.allowed is False
    assert decision.rule_source == "immutable"


@pytest.mark.asyncio
async def test_self_replication_pip_install_meept_blocked(engine: SecurityEngine) -> None:
    """pip install meept should be blocked as CRITICAL self-replication."""
    decision = await engine.check(
        "shell_execute",
        tool_name="shell",
        details={"command": "pip install meept"},
    )
    assert decision.allowed is False
    assert decision.rule_source == "immutable"


@pytest.mark.asyncio
async def test_self_replication_cp_meept_blocked(engine: SecurityEngine) -> None:
    """cp meept to meept should be blocked."""
    decision = await engine.check(
        "shell_execute",
        tool_name="shell",
        details={"command": "cp -r /opt/meept /tmp/meept"},
    )
    assert decision.allowed is False
    assert decision.rule_source == "immutable"


@pytest.mark.asyncio
async def test_self_replication_docker_meept_blocked(engine: SecurityEngine) -> None:
    """docker commands involving meept should be blocked."""
    decision = await engine.check(
        "shell_execute",
        tool_name="shell",
        details={"command": "docker run meept:latest"},
    )
    assert decision.allowed is False
    assert decision.rule_source == "immutable"


# ---------------------------------------------------------------------------
# Memory query at decision time (Plan P5)
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_memory_query_at_decision_time(tmp_path: Path) -> None:
    """Engine should query memory for security_override entries without error."""

    class MockMemory:
        """Minimal mock memory manager with async search."""

        async def search(self, query: str, category: str = "") -> list[str]:
            return [f"Security override: Creator allowed '{query}'"]

    db_path = tmp_path / "security_mem.db"
    eng = SecurityEngine(db_path=db_path, memory_manager=MockMemory())
    await eng.initialize()

    # Should succeed without error; memory is consulted but doesn't change outcome.
    decision = await eng.check("file_read", tool_name="file_read")
    assert decision.allowed is True
    await eng.close()


@pytest.mark.asyncio
async def test_memory_query_none_memory_noop(engine: SecurityEngine) -> None:
    """Engine with no memory manager should work fine (graceful no-op)."""
    assert engine._memory is None
    decision = await engine.check("file_read", tool_name="file_read")
    assert decision.allowed is True


@pytest.mark.asyncio
async def test_memory_query_failure_handled(tmp_path: Path) -> None:
    """Engine should handle memory search failures gracefully."""

    class BrokenMemory:
        async def search(self, query: str, category: str = "") -> list[str]:
            raise RuntimeError("Memory unavailable")

    db_path = tmp_path / "security_broken_mem.db"
    eng = SecurityEngine(db_path=db_path, memory_manager=BrokenMemory())
    await eng.initialize()

    # Should still succeed despite memory failure.
    decision = await eng.check("file_read", tool_name="file_read")
    assert decision.allowed is True
    await eng.close()
