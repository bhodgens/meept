"""Data-driven security engine backed by SQLite.

Replaces the hardcoded permission rules with a database-driven approach
that supports:

- Pre-populated best-practice tool/command/path risk rules
- Creator permission overrides with expiry (permissionary model)
- Full audit logging of every permission decision
- Memory-system integration for persistent creator preferences
- Minimal contextual scope exposed to the LLM
"""

from __future__ import annotations

import asyncio
import fnmatch
import json
import logging
import re
from dataclasses import dataclass, field
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any

import aiosqlite

from meept.security.permissions import RiskLevel
from meept.security.seed_rules import (
    COMMAND_PATTERNS,
    FINANCIAL_PATTERNS,
    PATH_RULES,
    TOOL_RULES,
)

log = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Data types
# ---------------------------------------------------------------------------


@dataclass(slots=True)
class PermissionDecision:
    """Result of a permission check."""

    allowed: bool
    reason: str
    risk_level: RiskLevel
    rule_source: str  # 'base_rule', 'command_pattern', 'path_rule', 'override', 'immutable', 'confirmation_gate'
    requires_confirmation: bool = False
    override_applied: bool = False
    override_id: int | None = None


@dataclass(slots=True)
class DecisionRecord:
    """A single entry from the decision log."""

    id: int
    timestamp: str
    action: str
    tool_name: str
    details_json: str
    risk_level: int
    decision: str
    reason: str
    rule_source: str
    override_id: int | None
    conversation_id: str | None


@dataclass(slots=True)
class SecurityStats:
    """Aggregate security statistics."""

    total_decisions: int = 0
    total_allows: int = 0
    total_denies: int = 0
    total_escalations: int = 0
    active_overrides: int = 0
    top_denied_actions: list[tuple[str, int]] = field(default_factory=list)


# ---------------------------------------------------------------------------
# Schema SQL
# ---------------------------------------------------------------------------

_SCHEMA_SQL = """
CREATE TABLE IF NOT EXISTS tool_rules (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    tool_name   TEXT NOT NULL,
    action      TEXT NOT NULL,
    risk_level  INTEGER NOT NULL DEFAULT 2,
    description TEXT NOT NULL DEFAULT '',
    requires_confirmation INTEGER NOT NULL DEFAULT 0,
    immutable   INTEGER NOT NULL DEFAULT 0,
    enabled     INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE(tool_name, action)
);

CREATE TABLE IF NOT EXISTS command_patterns (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    pattern      TEXT NOT NULL,
    pattern_type TEXT NOT NULL DEFAULT 'regex',
    risk_level   INTEGER NOT NULL,
    category     TEXT NOT NULL DEFAULT 'general',
    description  TEXT NOT NULL DEFAULT '',
    immutable    INTEGER NOT NULL DEFAULT 0,
    enabled      INTEGER NOT NULL DEFAULT 1,
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE(pattern, pattern_type)
);

CREATE TABLE IF NOT EXISTS path_rules (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    pattern     TEXT NOT NULL,
    rule_type   TEXT NOT NULL,
    risk_level  INTEGER NOT NULL DEFAULT 2,
    description TEXT NOT NULL DEFAULT '',
    immutable   INTEGER NOT NULL DEFAULT 0,
    enabled     INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE(pattern, rule_type)
);

CREATE TABLE IF NOT EXISTS permission_overrides (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    action          TEXT NOT NULL,
    pattern         TEXT NOT NULL DEFAULT '*',
    decision        TEXT NOT NULL,
    reason          TEXT NOT NULL DEFAULT '',
    usage_count     INTEGER NOT NULL DEFAULT 0,
    max_uses        INTEGER NOT NULL DEFAULT 50,
    expires_at      TEXT,
    conversation_id TEXT,
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE TABLE IF NOT EXISTS decision_log (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    action          TEXT NOT NULL,
    tool_name       TEXT NOT NULL DEFAULT '',
    details_json    TEXT NOT NULL DEFAULT '{}',
    risk_level      INTEGER NOT NULL,
    decision        TEXT NOT NULL,
    reason          TEXT NOT NULL DEFAULT '',
    rule_source     TEXT NOT NULL DEFAULT '',
    override_id     INTEGER,
    conversation_id TEXT
);

CREATE TABLE IF NOT EXISTS financial_patterns (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    pattern      TEXT NOT NULL,
    pattern_type TEXT NOT NULL DEFAULT 'regex',
    description  TEXT NOT NULL DEFAULT '',
    immutable    INTEGER NOT NULL DEFAULT 1,
    enabled      INTEGER NOT NULL DEFAULT 1,
    UNIQUE(pattern, pattern_type)
);

CREATE INDEX IF NOT EXISTS idx_decision_log_action ON decision_log(action);
CREATE INDEX IF NOT EXISTS idx_decision_log_timestamp ON decision_log(timestamp);
CREATE INDEX IF NOT EXISTS idx_overrides_action ON permission_overrides(action);
"""


# ---------------------------------------------------------------------------
# SecurityEngine
# ---------------------------------------------------------------------------


class SecurityEngine:
    """Data-driven permission engine backed by SQLite.

    Parameters
    ----------
    db_path:
        Path to the SQLite database file.
    config:
        The security section of the application config.  Used for
        confirmation thresholds and feature flags.
    memory_manager:
        Optional memory manager for the permissionary model integration.
    """

    def __init__(
        self,
        db_path: Path | str,
        config: Any = None,
        memory_manager: Any | None = None,
    ) -> None:
        self._db_path = str(Path(db_path).expanduser().resolve())
        self._config = config
        self._memory = memory_manager
        self._db: aiosqlite.Connection | None = None
        self._compiled_patterns: list[tuple[re.Pattern[str], int, str, str, bool]] | None = None
        self._compiled_financial: list[re.Pattern[str]] | None = None

    # ------------------------------------------------------------------
    # Lifecycle
    # ------------------------------------------------------------------

    async def initialize(self) -> None:
        """Open the database, create schema, and seed default rules."""
        self._db = await aiosqlite.connect(self._db_path)
        await self._db.executescript(_SCHEMA_SQL)
        await self._seed_defaults()
        await self._compile_patterns()
        log.info("SecurityEngine initialized: %s", self._db_path)

    async def close(self) -> None:
        """Close the database connection."""
        if self._db is not None:
            await self._db.close()
            self._db = None

    # ------------------------------------------------------------------
    # Seeding
    # ------------------------------------------------------------------

    async def _seed_defaults(self) -> None:
        """Insert default rules if not already present."""
        assert self._db is not None

        for tool_name, action, risk, desc, confirm, immut in TOOL_RULES:
            await self._db.execute(
                "INSERT OR IGNORE INTO tool_rules "
                "(tool_name, action, risk_level, description, requires_confirmation, immutable) "
                "VALUES (?, ?, ?, ?, ?, ?)",
                (tool_name, action, risk, desc, int(confirm), int(immut)),
            )

        for pattern, ptype, risk, cat, desc, immut in COMMAND_PATTERNS:
            await self._db.execute(
                "INSERT OR IGNORE INTO command_patterns "
                "(pattern, pattern_type, risk_level, category, description, immutable) "
                "VALUES (?, ?, ?, ?, ?, ?)",
                (pattern, ptype, risk, cat, desc, int(immut)),
            )

        for pattern, rtype, risk, desc, immut in PATH_RULES:
            await self._db.execute(
                "INSERT OR IGNORE INTO path_rules "
                "(pattern, rule_type, risk_level, description, immutable) "
                "VALUES (?, ?, ?, ?, ?)",
                (pattern, rtype, risk, desc, int(immut)),
            )

        for pattern, ptype, desc in FINANCIAL_PATTERNS:
            await self._db.execute(
                "INSERT OR IGNORE INTO financial_patterns "
                "(pattern, pattern_type, description) "
                "VALUES (?, ?, ?)",
                (pattern, ptype, desc),
            )

        await self._db.commit()

    async def _compile_patterns(self) -> None:
        """Pre-compile regex patterns from the database for performance."""
        assert self._db is not None

        # Command patterns
        self._compiled_patterns = []
        async with self._db.execute(
            "SELECT pattern, risk_level, category, description, immutable "
            "FROM command_patterns WHERE enabled = 1 ORDER BY risk_level DESC"
        ) as cursor:
            async for row in cursor:
                pattern_str, risk, cat, desc, immut = row
                try:
                    compiled = re.compile(pattern_str, re.IGNORECASE)
                    self._compiled_patterns.append((compiled, risk, cat, desc, bool(immut)))
                except re.error:
                    log.warning("Invalid command pattern regex: %s", pattern_str)

        # Financial patterns
        self._compiled_financial = []
        async with self._db.execute(
            "SELECT pattern FROM financial_patterns WHERE enabled = 1"
        ) as cursor:
            async for row in cursor:
                try:
                    self._compiled_financial.append(re.compile(row[0], re.IGNORECASE))
                except re.error:
                    log.warning("Invalid financial pattern regex: %s", row[0])

    # ------------------------------------------------------------------
    # Main permission check
    # ------------------------------------------------------------------

    async def check(
        self,
        action: str,
        tool_name: str = "",
        details: dict[str, Any] | None = None,
        conversation_id: str | None = None,
    ) -> PermissionDecision:
        """Run the full permission check pipeline.

        Pipeline stages:
        1. Immutable deny (financial, destructive)
        2. Base rule lookup (tool_rules)
        3. Context analysis (command_patterns, path_rules)
        4. Memory override check (permission_overrides)
        5. Confirmation gate
        6. Log decision

        Returns
        -------
        PermissionDecision
            The allow/deny decision with full context.
        """
        details = details or {}

        # Stage 1: Immutable financial check
        decision = await self._check_financial(action, details)
        if decision is not None:
            await self._log_decision(decision, action, tool_name, details, conversation_id)
            return decision

        # Stage 2: Base rule lookup
        base_risk, base_confirm, base_desc = await self._lookup_base_rule(action, tool_name)

        # Stage 3: Context analysis -- commands and paths
        effective_risk = base_risk
        rule_source = "base_rule"

        if action == "shell_execute":
            cmd = details.get("command", "")
            cmd_risk, cmd_source, cmd_immutable = self._evaluate_command(cmd)
            if cmd_risk > effective_risk:
                effective_risk = cmd_risk
                rule_source = f"command_pattern:{cmd_source}"
            if cmd_immutable and cmd_risk >= RiskLevel.CRITICAL:
                decision = PermissionDecision(
                    allowed=False,
                    reason=f"Command matches immutable CRITICAL rule: {cmd_source}",
                    risk_level=RiskLevel(cmd_risk),
                    rule_source="immutable",
                )
                await self._log_decision(decision, action, tool_name, details, conversation_id)
                return decision

        if action in ("file_read", "file_write", "file_delete"):
            path = details.get("path", "")
            if path:
                path_decision = await self._check_path(path, action)
                if path_decision is not None:
                    await self._log_decision(path_decision, action, tool_name, details, conversation_id)
                    return path_decision

        # Stage 4: Memory override check
        override_decision = await self._check_overrides(action, details)
        if override_decision is not None:
            await self._log_decision(override_decision, action, tool_name, details, conversation_id)
            return override_decision

        # Stage 5: Confirmation gate
        needs_confirm = self._needs_confirmation(effective_risk)
        if needs_confirm:
            decision = PermissionDecision(
                allowed=False,
                reason=(
                    f"Action '{action}' has risk level {RiskLevel(effective_risk).name} "
                    f"and requires user confirmation"
                ),
                risk_level=RiskLevel(effective_risk),
                rule_source="confirmation_gate",
                requires_confirmation=True,
            )
            await self._log_decision(decision, action, tool_name, details, conversation_id)
            return decision

        # Permitted
        decision = PermissionDecision(
            allowed=True,
            reason="Permitted",
            risk_level=RiskLevel(effective_risk),
            rule_source=rule_source,
        )
        await self._log_decision(decision, action, tool_name, details, conversation_id)
        return decision

    # ------------------------------------------------------------------
    # Backward-compatible interface
    # ------------------------------------------------------------------

    async def check_permission(
        self,
        action: str,
        details: dict[str, Any] | None = None,
    ) -> tuple[bool, str]:
        """Compatibility shim matching the old PermissionManager.check_permission signature."""
        decision = await self.check(action, details=details)
        return decision.allowed, decision.reason

    # ------------------------------------------------------------------
    # Stage 1: Financial detection
    # ------------------------------------------------------------------

    async def _check_financial(
        self, action: str, details: dict[str, Any],
    ) -> PermissionDecision | None:
        """Return a deny decision if details describe a financial operation."""
        if self._config is not None and not getattr(self._config, "block_financial", True):
            return None

        if self._compiled_financial is None:
            return None

        for value in details.values():
            if not isinstance(value, str):
                continue
            for pattern in self._compiled_financial:
                if pattern.search(value):
                    return PermissionDecision(
                        allowed=False,
                        reason="Financial operations are blocked by policy",
                        risk_level=RiskLevel.CRITICAL,
                        rule_source="immutable",
                    )
        return None

    # ------------------------------------------------------------------
    # Stage 2: Base rule lookup
    # ------------------------------------------------------------------

    async def _lookup_base_rule(
        self, action: str, tool_name: str,
    ) -> tuple[int, bool, str]:
        """Look up the base risk level for an action from the database."""
        if self._db is None:
            return RiskLevel.MEDIUM, False, "Unknown action (no database)"

        # Try exact action match first
        async with self._db.execute(
            "SELECT risk_level, requires_confirmation, description "
            "FROM tool_rules WHERE action = ? AND enabled = 1 LIMIT 1",
            (action,),
        ) as cursor:
            row = await cursor.fetchone()
            if row:
                return row[0], bool(row[1]), row[2]

        # Try tool_name match
        if tool_name:
            async with self._db.execute(
                "SELECT risk_level, requires_confirmation, description "
                "FROM tool_rules WHERE tool_name = ? AND enabled = 1 LIMIT 1",
                (tool_name,),
            ) as cursor:
                row = await cursor.fetchone()
                if row:
                    return row[0], bool(row[1]), row[2]

        log.warning("No rule found for action=%s tool=%s; defaulting to MEDIUM", action, tool_name)
        return RiskLevel.MEDIUM, False, "Unknown action"

    # ------------------------------------------------------------------
    # Stage 3a: Command pattern evaluation
    # ------------------------------------------------------------------

    def _evaluate_command(self, command: str) -> tuple[int, str, bool]:
        """Evaluate a shell command against compiled patterns.

        Returns (risk_level, description, immutable).
        Patterns are checked highest-risk first; first match wins.
        """
        if not command or self._compiled_patterns is None:
            return RiskLevel.MEDIUM, "shell_execute", False

        for compiled, risk, _cat, desc, immut in self._compiled_patterns:
            if compiled.search(command):
                return risk, desc, immut

        return RiskLevel.MEDIUM, "shell_execute", False

    # ------------------------------------------------------------------
    # Stage 3b: Path checking
    # ------------------------------------------------------------------

    async def _check_path(
        self, path_str: str, action: str,
    ) -> PermissionDecision | None:
        """Check a filesystem path against path rules.

        Returns a deny decision if blocked, or None if allowed.
        """
        if self._db is None:
            return None

        resolved = str(Path(path_str).expanduser().resolve())

        # Check block rules first (precedence)
        async with self._db.execute(
            "SELECT pattern, description, immutable, risk_level "
            "FROM path_rules WHERE rule_type = 'block' AND enabled = 1"
        ) as cursor:
            async for row in cursor:
                pattern, desc, immut, risk = row
                expanded_pattern = str(Path(pattern).expanduser())
                if fnmatch.fnmatch(resolved, expanded_pattern):
                    return PermissionDecision(
                        allowed=False,
                        reason=f"Path blocked: {desc} (pattern: {pattern})",
                        risk_level=RiskLevel(risk),
                        rule_source="path_rule" if not immut else "immutable",
                    )

        # Check allow rules
        has_allow_rules = False
        async with self._db.execute(
            "SELECT pattern FROM path_rules WHERE rule_type = 'allow' AND enabled = 1"
        ) as cursor:
            async for row in cursor:
                has_allow_rules = True
                pattern = row[0]
                expanded_pattern = str(Path(pattern).expanduser())
                if fnmatch.fnmatch(resolved, expanded_pattern):
                    return None  # Allowed

        if has_allow_rules:
            return PermissionDecision(
                allowed=False,
                reason="Path does not match any allowed pattern",
                risk_level=RiskLevel.MEDIUM,
                rule_source="path_rule",
            )

        return None

    # ------------------------------------------------------------------
    # Stage 4: Override check
    # ------------------------------------------------------------------

    async def _check_overrides(
        self, action: str, details: dict[str, Any],
    ) -> PermissionDecision | None:
        """Check for creator permission overrides.

        Also queries the memory system for security_override entries that
        may reflect creator preferences not yet recorded as DB overrides.
        """
        if self._db is None:
            return None

        # Query memory for security overrides (Plan P5) -- graceful no-op.
        if self._memory is not None:
            try:
                search_fn = getattr(self._memory, "search", None)
                if search_fn is not None:
                    import inspect

                    if inspect.iscoroutinefunction(search_fn):
                        memories = await search_fn(
                            f"security_override {action}",
                            category="security_override",
                        )
                    else:
                        memories = search_fn(
                            f"security_override {action}",
                            category="security_override",
                        )

                    if memories:
                        log.debug(
                            "Found %d memory-based security override(s) for action=%s; "
                            "consider recording them as DB overrides",
                            len(memories) if isinstance(memories, list) else 1,
                            action,
                        )
            except Exception:
                log.debug("Memory query for security overrides failed", exc_info=True)

        now = datetime.now(timezone.utc).isoformat()

        async with self._db.execute(
            "SELECT id, pattern, decision, reason, usage_count, max_uses, expires_at "
            "FROM permission_overrides "
            "WHERE action = ? AND (expires_at IS NULL OR expires_at > ?) "
            "ORDER BY created_at DESC",
            (action, now),
        ) as cursor:
            async for row in cursor:
                ov_id, pattern, ov_decision, ov_reason, usage_count, max_uses, expires_at = row

                # Check max_uses
                if max_uses > 0 and usage_count >= max_uses:
                    continue

                # Check pattern match
                if pattern != "*":
                    detail_str = json.dumps(details, default=str)
                    if not fnmatch.fnmatch(detail_str, pattern) and pattern not in detail_str:
                        # Try matching against specific detail values
                        matched = False
                        for v in details.values():
                            if isinstance(v, str) and fnmatch.fnmatch(v, pattern):
                                matched = True
                                break
                        if not matched:
                            continue

                # Increment usage count
                await self._db.execute(
                    "UPDATE permission_overrides SET usage_count = usage_count + 1, "
                    "updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?",
                    (ov_id,),
                )
                await self._db.commit()

                if ov_decision == "allow":
                    return PermissionDecision(
                        allowed=True,
                        reason=f"Creator override: {ov_reason}" if ov_reason else "Creator pre-approved",
                        risk_level=RiskLevel.MEDIUM,
                        rule_source="override",
                        override_applied=True,
                        override_id=ov_id,
                    )
                else:
                    return PermissionDecision(
                        allowed=False,
                        reason=f"Creator override (deny): {ov_reason}" if ov_reason else "Creator denied",
                        risk_level=RiskLevel.HIGH,
                        rule_source="override",
                        override_applied=True,
                        override_id=ov_id,
                    )

        return None

    # ------------------------------------------------------------------
    # Stage 5: Confirmation gate
    # ------------------------------------------------------------------

    def _needs_confirmation(self, risk_level: int) -> bool:
        """Determine if the risk level triggers the confirmation gate."""
        if self._config is None:
            return risk_level >= RiskLevel.HIGH

        require_high = getattr(self._config, "require_confirmation_high", True)
        require_critical = getattr(self._config, "require_confirmation_critical", True)

        if risk_level >= RiskLevel.CRITICAL and require_critical:
            return True
        if risk_level >= RiskLevel.HIGH and require_high:
            return True
        return False

    # ------------------------------------------------------------------
    # Decision logging
    # ------------------------------------------------------------------

    async def _log_decision(
        self,
        decision: PermissionDecision,
        action: str,
        tool_name: str,
        details: dict[str, Any],
        conversation_id: str | None,
    ) -> None:
        """Write a permission decision to the audit log."""
        if self._db is None:
            return

        decision_str = "allow" if decision.allowed else ("escalate" if decision.requires_confirmation else "deny")

        try:
            details_json = json.dumps(details, default=str)
        except (TypeError, ValueError):
            details_json = "{}"

        await self._db.execute(
            "INSERT INTO decision_log "
            "(action, tool_name, details_json, risk_level, decision, reason, "
            "rule_source, override_id, conversation_id) "
            "VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
            (
                action,
                tool_name,
                details_json,
                decision.risk_level,
                decision_str,
                decision.reason,
                decision.rule_source,
                decision.override_id,
                conversation_id,
            ),
        )
        await self._db.commit()

        if not decision.allowed:
            log.info(
                "Permission %s for %s/%s: %s [source=%s]",
                decision_str, action, tool_name, decision.reason, decision.rule_source,
            )
        else:
            log.debug(
                "Permission allow for %s/%s [source=%s]",
                action, tool_name, decision.rule_source,
            )

    # ------------------------------------------------------------------
    # Override management (permissionary model)
    # ------------------------------------------------------------------

    async def record_override(
        self,
        action: str,
        pattern: str = "*",
        decision: str = "allow",
        reason: str = "",
        conversation_id: str | None = None,
        max_uses: int = 50,
        expires_days: int = 30,
    ) -> int:
        """Record a creator permission override.

        Parameters
        ----------
        action:
            The action category (e.g. "shell_execute").
        pattern:
            Specific pattern to match (e.g. "python*"), or "*" for all.
        decision:
            "allow" or "deny".
        reason:
            Why the creator made this decision.
        conversation_id:
            Which conversation produced this override.
        max_uses:
            Override expires after this many uses (0=unlimited).
        expires_days:
            Override expires after this many days (0=no expiry).

        Returns
        -------
        int
            The ID of the created override.
        """
        assert self._db is not None

        expires_at = None
        if expires_days > 0:
            expires_at = (datetime.now(timezone.utc) + timedelta(days=expires_days)).isoformat()

        cursor = await self._db.execute(
            "INSERT INTO permission_overrides "
            "(action, pattern, decision, reason, max_uses, expires_at, conversation_id) "
            "VALUES (?, ?, ?, ?, ?, ?, ?)",
            (action, pattern, decision, reason, max_uses, expires_at, conversation_id),
        )
        await self._db.commit()
        override_id = cursor.lastrowid

        # Also store in memory system if available
        if self._memory is not None:
            try:
                store_fn = getattr(self._memory, "store", None)
                if store_fn is not None:
                    expires_msg = f" (expires in {expires_days} days, {max_uses} uses max)" if expires_days or max_uses else ""
                    memory_text = (
                        f"Security override: Creator {decision}ed '{action}' "
                        f"matching '{pattern}'. Reason: {reason}{expires_msg}"
                    )
                    if asyncio.iscoroutinefunction(store_fn):
                        await store_fn(memory_text, category="security_override")
                    else:
                        store_fn(memory_text, category="security_override")
            except Exception:
                log.debug("Failed to store override in memory", exc_info=True)

        log.info(
            "Override recorded: %s %s pattern=%s reason=%s (id=%d)",
            decision, action, pattern, reason, override_id,
        )
        return override_id

    # ------------------------------------------------------------------
    # LLM context generation
    # ------------------------------------------------------------------

    def get_context_for_llm(
        self, decision: PermissionDecision, action: str, details: dict[str, Any],
    ) -> str:
        """Generate a minimal context string for the LLM.

        The LLM only sees the verdict, never the full rule set.
        """
        lines = ["# Security Context (current action)"]
        lines.append(f"- Action: {action}")

        if action == "shell_execute":
            cmd = details.get("command", "")
            if cmd:
                lines.append(f"- Command: {cmd[:100]}")
        elif action in ("file_read", "file_write", "file_delete"):
            path = details.get("path", "")
            if path:
                lines.append(f"- Path: {path}")

        lines.append(f"- Risk: {decision.risk_level.name}")
        lines.append(f"- Status: {'Permitted' if decision.allowed else 'Denied - ' + decision.reason}")

        if decision.override_applied:
            lines.append("- Note: Creator override is active for this action")

        if not decision.allowed:
            lines.append("- Do not attempt to work around this restriction.")

        return "\n".join(lines)

    # ------------------------------------------------------------------
    # Audit & stats
    # ------------------------------------------------------------------

    async def query_log(
        self,
        action: str | None = None,
        decision: str | None = None,
        limit: int = 100,
        since: str | None = None,
    ) -> list[DecisionRecord]:
        """Query the decision log with optional filters."""
        if self._db is None:
            return []

        conditions = []
        params: list[Any] = []

        if action:
            conditions.append("action = ?")
            params.append(action)
        if decision:
            conditions.append("decision = ?")
            params.append(decision)
        if since:
            conditions.append("timestamp >= ?")
            params.append(since)

        where = " AND ".join(conditions)
        if where:
            where = "WHERE " + where

        sql = f"SELECT id, timestamp, action, tool_name, details_json, risk_level, decision, reason, rule_source, override_id, conversation_id FROM decision_log {where} ORDER BY timestamp DESC LIMIT ?"
        params.append(limit)

        records = []
        async with self._db.execute(sql, params) as cursor:
            async for row in cursor:
                records.append(DecisionRecord(*row))

        return records

    async def get_stats(self) -> SecurityStats:
        """Return aggregate security statistics."""
        if self._db is None:
            return SecurityStats()

        stats = SecurityStats()

        async with self._db.execute("SELECT COUNT(*) FROM decision_log") as c:
            row = await c.fetchone()
            stats.total_decisions = row[0] if row else 0

        async with self._db.execute("SELECT COUNT(*) FROM decision_log WHERE decision = 'allow'") as c:
            row = await c.fetchone()
            stats.total_allows = row[0] if row else 0

        async with self._db.execute("SELECT COUNT(*) FROM decision_log WHERE decision = 'deny'") as c:
            row = await c.fetchone()
            stats.total_denies = row[0] if row else 0

        async with self._db.execute("SELECT COUNT(*) FROM decision_log WHERE decision = 'escalate'") as c:
            row = await c.fetchone()
            stats.total_escalations = row[0] if row else 0

        now = datetime.now(timezone.utc).isoformat()
        async with self._db.execute(
            "SELECT COUNT(*) FROM permission_overrides "
            "WHERE (expires_at IS NULL OR expires_at > ?) "
            "AND (max_uses = 0 OR usage_count < max_uses)",
            (now,),
        ) as c:
            row = await c.fetchone()
            stats.active_overrides = row[0] if row else 0

        async with self._db.execute(
            "SELECT action, COUNT(*) as cnt FROM decision_log "
            "WHERE decision = 'deny' GROUP BY action ORDER BY cnt DESC LIMIT 10"
        ) as c:
            stats.top_denied_actions = [(row[0], row[1]) async for row in c]

        return stats
