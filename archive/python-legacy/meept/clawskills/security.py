"""Security adapter for third-party ClawSkills.

All clawskills are treated as untrusted.  This adapter enforces:

1. **STRICT input sanitization** on SKILL.md instructions.
2. **Tool access filtering** -- blocks dangerous tools by name and pattern.
3. **Risk level enforcement** -- always HIGH, regardless of frontmatter.
4. **Iteration cap** -- capped at 10.
"""

from __future__ import annotations

import logging
import re

from meept.security.sanitizer import InputSanitizer, StrictnessLevel

log = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Blocked tool lists
# ---------------------------------------------------------------------------

# Explicitly blocked tool names.
BLOCKED_TOOLS: frozenset[str] = frozenset({
    "shell",
    "shell_execute",
    "file_write",
    "file_delete",
    "install_package",
    "system_modify",
    "send_message",
})

# Any tool whose name contains one of these substrings is also blocked.
BLOCKED_TOOL_PATTERNS: tuple[str, ...] = (
    "credential",
    "secret",
    "auth",
    "password",
    "token",
)

_BLOCKED_PATTERN_RE = re.compile(
    "|".join(re.escape(p) for p in BLOCKED_TOOL_PATTERNS),
    re.IGNORECASE,
)


# ---------------------------------------------------------------------------
# Adapter
# ---------------------------------------------------------------------------


class ClawSkillSecurityAdapter:
    """Applies strict security hardening to third-party clawskill definitions."""

    def __init__(self) -> None:
        self._sanitizer = InputSanitizer(strictness=StrictnessLevel.STRICT)

    def sanitize_instructions(self, instructions: str, slug: str = "") -> str:
        """Run SKILL.md instructions through STRICT sanitization.

        Returns the cleaned text.
        """
        result = self._sanitizer.sanitize(instructions, source=f"clawskill:{slug}")
        if result.threats_detected:
            log.warning(
                "ClawSkill %r instructions contained threats: %s",
                slug,
                ", ".join(result.threats_detected),
            )
        return result.clean_text

    def validate_allowed_tools(self, tools: list[str]) -> list[str]:
        """Filter a tool list, removing any blocked or pattern-matched tools.

        Returns the filtered list (may be empty).
        """
        safe: list[str] = []
        for tool in tools:
            if tool in BLOCKED_TOOLS:
                log.info("ClawSkill security: blocked tool %r (explicit block)", tool)
                continue
            if _BLOCKED_PATTERN_RE.search(tool):
                log.info("ClawSkill security: blocked tool %r (pattern match)", tool)
                continue
            safe.append(tool)
        return safe

    @staticmethod
    def enforce_risk_level(risk_level: str) -> str:
        """Ensure risk level is at least HIGH."""
        return "high"

    @staticmethod
    def enforce_max_iterations(max_iterations: int, cap: int = 10) -> int:
        """Cap max_iterations at *cap*."""
        return min(max_iterations, cap)
