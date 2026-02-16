"""Input sanitization pipeline for LLM prompt injection defence.

Provides two-layer filtering:

1. **Pattern detection** -- regex-based scanning for known injection
   patterns (role-switching, instruction override, special token abuse).
2. **Structural sanitization** -- stripping/escaping of role markers and
   special tokens that could confuse the model.

The pipeline is configurable via three strictness levels:
``PERMISSIVE``, ``STANDARD``, and ``STRICT``.
"""

from __future__ import annotations

import enum
import logging
import re
from dataclasses import dataclass, field

logger = logging.getLogger(__name__)


# ---------------------------------------------------------------------------
# Public data structures
# ---------------------------------------------------------------------------


class StrictnessLevel(enum.Enum):
    """Controls how aggressively the sanitizer filters input."""

    PERMISSIVE = "permissive"
    STANDARD = "standard"
    STRICT = "strict"


@dataclass(slots=True)
class SanitizationResult:
    """Outcome of running a piece of text through the sanitization pipeline."""

    clean_text: str
    was_modified: bool
    threats_detected: list[str] = field(default_factory=list)


# ---------------------------------------------------------------------------
# Injection-pattern catalogue
# ---------------------------------------------------------------------------

# Each entry is (compiled_regex, human-readable threat label, minimum
# strictness level at which the pattern is enforced).

_INJECTION_PATTERNS: list[tuple[re.Pattern[str], str, StrictnessLevel]] = [
    # -- Instruction override attempts --
    (
        re.compile(
            r"ignore\s+(all\s+)?(previous|prior|above|earlier|preceding)\s+"
            r"(instructions?|prompts?|rules?|guidelines?|directions?)",
            re.IGNORECASE,
        ),
        "instruction_override",
        StrictnessLevel.PERMISSIVE,
    ),
    (
        re.compile(
            r"(disregard|forget|override|bypass|skip)\s+(all\s+)?"
            r"(previous|prior|above|earlier|preceding)?\s*"
            r"(instructions?|prompts?|rules?|guidelines?|directions?|constraints?)",
            re.IGNORECASE,
        ),
        "instruction_override",
        StrictnessLevel.PERMISSIVE,
    ),
    (
        re.compile(
            r"(you\s+are\s+now|act\s+as|pretend\s+(to\s+be|you\s+are)|"
            r"roleplay\s+as|switch\s+to\s+role|enter\s+.{0,20}\s+mode)",
            re.IGNORECASE,
        ),
        "role_switch_attempt",
        StrictnessLevel.PERMISSIVE,
    ),
    (
        re.compile(
            r"new\s+instructions?\s*:", re.IGNORECASE
        ),
        "instruction_injection",
        StrictnessLevel.PERMISSIVE,
    ),
    (
        re.compile(
            r"(do\s+not|don'?t)\s+(follow|obey|listen\s+to)\s+"
            r"(your|the|any)\s+(rules?|instructions?|guidelines?|system\s+prompt)",
            re.IGNORECASE,
        ),
        "instruction_override",
        StrictnessLevel.PERMISSIVE,
    ),
    # -- Role markers in user text --
    (
        re.compile(r"^\s*system\s*:", re.IGNORECASE | re.MULTILINE),
        "role_marker_system",
        StrictnessLevel.PERMISSIVE,
    ),
    (
        re.compile(r"^\s*assistant\s*:", re.IGNORECASE | re.MULTILINE),
        "role_marker_assistant",
        StrictnessLevel.PERMISSIVE,
    ),
    (
        re.compile(r"^\s*user\s*:", re.IGNORECASE | re.MULTILINE),
        "role_marker_user",
        StrictnessLevel.STANDARD,
    ),
    # -- Markdown code-fence role injection --
    (
        re.compile(r"```\s*system", re.IGNORECASE),
        "markdown_role_injection",
        StrictnessLevel.PERMISSIVE,
    ),
    (
        re.compile(r"```\s*assistant", re.IGNORECASE),
        "markdown_role_injection",
        StrictnessLevel.PERMISSIVE,
    ),
    # -- Special-token injection (ChatML / Llama / Mistral) --
    (
        re.compile(r"<\|im_start\|>", re.IGNORECASE),
        "special_token_chatml",
        StrictnessLevel.PERMISSIVE,
    ),
    (
        re.compile(r"<\|im_end\|>", re.IGNORECASE),
        "special_token_chatml",
        StrictnessLevel.PERMISSIVE,
    ),
    (
        re.compile(r"\[INST\]", re.IGNORECASE),
        "special_token_llama",
        StrictnessLevel.PERMISSIVE,
    ),
    (
        re.compile(r"\[/INST\]", re.IGNORECASE),
        "special_token_llama",
        StrictnessLevel.PERMISSIVE,
    ),
    (
        re.compile(r"<<SYS>>", re.IGNORECASE),
        "special_token_llama_sys",
        StrictnessLevel.PERMISSIVE,
    ),
    (
        re.compile(r"<</SYS>>", re.IGNORECASE),
        "special_token_llama_sys",
        StrictnessLevel.PERMISSIVE,
    ),
    (
        re.compile(r"<\|system\|>", re.IGNORECASE),
        "special_token_phi",
        StrictnessLevel.PERMISSIVE,
    ),
    (
        re.compile(r"<\|user\|>", re.IGNORECASE),
        "special_token_phi",
        StrictnessLevel.STANDARD,
    ),
    (
        re.compile(r"<\|assistant\|>", re.IGNORECASE),
        "special_token_phi",
        StrictnessLevel.PERMISSIVE,
    ),
    (
        re.compile(r"<\|endoftext\|>", re.IGNORECASE),
        "special_token_eos",
        StrictnessLevel.PERMISSIVE,
    ),
    # -- STRICT-only: aggressive heuristics --
    (
        re.compile(
            r"(reveal|show|print|output|display|tell\s+me)\s+"
            r"(your\s+)?(system\s+prompt|instructions?|hidden\s+prompt|rules?)",
            re.IGNORECASE,
        ),
        "prompt_extraction_attempt",
        StrictnessLevel.STRICT,
    ),
    (
        re.compile(
            r"(repeat|echo|recite)\s+(everything|all|the\s+text)\s+"
            r"(above|before|so\s+far)",
            re.IGNORECASE,
        ),
        "prompt_extraction_attempt",
        StrictnessLevel.STRICT,
    ),
]

# Tokens that must be escaped in Layer 2 regardless of strictness.
_STRUCTURAL_TOKENS: list[tuple[re.Pattern[str], str]] = [
    (re.compile(r"<\|im_start\|>", re.IGNORECASE), "<|im_start|>"),
    (re.compile(r"<\|im_end\|>", re.IGNORECASE), "<|im_end|>"),
    (re.compile(r"\[INST\]", re.IGNORECASE), "[INST]"),
    (re.compile(r"\[/INST\]", re.IGNORECASE), "[/INST]"),
    (re.compile(r"<<SYS>>", re.IGNORECASE), "<<SYS>>"),
    (re.compile(r"<</SYS>>", re.IGNORECASE), "<</SYS>>"),
    (re.compile(r"<\|system\|>", re.IGNORECASE), "<|system|>"),
    (re.compile(r"<\|user\|>", re.IGNORECASE), "<|user|>"),
    (re.compile(r"<\|assistant\|>", re.IGNORECASE), "<|assistant|>"),
    (re.compile(r"<\|endoftext\|>", re.IGNORECASE), "<|endoftext|>"),
]

# Role-marker patterns to strip (only used in STANDARD / STRICT).
_ROLE_MARKER_RE = re.compile(
    r"^\s*(system|assistant|user)\s*:\s*", re.IGNORECASE | re.MULTILINE
)


# ---------------------------------------------------------------------------
# Strictness ordering helper
# ---------------------------------------------------------------------------

_STRICTNESS_ORDER: dict[StrictnessLevel, int] = {
    StrictnessLevel.PERMISSIVE: 0,
    StrictnessLevel.STANDARD: 1,
    StrictnessLevel.STRICT: 2,
}


def _level_active(pattern_level: StrictnessLevel, current: StrictnessLevel) -> bool:
    """Return ``True`` if a pattern at *pattern_level* should fire under *current*."""
    return _STRICTNESS_ORDER[current] >= _STRICTNESS_ORDER[pattern_level]


# ---------------------------------------------------------------------------
# InputSanitizer
# ---------------------------------------------------------------------------


class InputSanitizer:
    """Two-layer sanitization pipeline for untrusted text.

    Parameters
    ----------
    strictness:
        Filtering aggressiveness.  Defaults to ``STANDARD``.
    """

    def __init__(self, strictness: StrictnessLevel = StrictnessLevel.STANDARD) -> None:
        self.strictness = strictness

    # -- Layer 1: pattern detection -----------------------------------------

    def _detect_patterns(self, text: str) -> list[str]:
        """Return a deduplicated list of threat labels found in *text*."""
        seen: set[str] = set()
        threats: list[str] = []
        for pattern, label, min_level in _INJECTION_PATTERNS:
            if not _level_active(min_level, self.strictness):
                continue
            if pattern.search(text) and label not in seen:
                seen.add(label)
                threats.append(label)
        return threats

    # -- Layer 2: structural sanitization -----------------------------------

    def _sanitize_structure(self, text: str) -> tuple[str, bool]:
        """Escape special tokens and strip role markers.

        Returns the cleaned text and whether any modification was made.
        """
        modified = False

        # Escape special tokens by inserting a zero-width space in the middle
        # so the model never sees the raw token.
        for tok_re, display in _STRUCTURAL_TOKENS:
            if tok_re.search(text):
                # Insert a Unicode zero-width space (\u200b) after the first
                # character to break the token without altering readability.
                replacement = display[0] + "\u200b" + display[1:]
                text = tok_re.sub(replacement, text)
                modified = True

        # Strip role markers at line beginnings (STANDARD and above).
        if _level_active(StrictnessLevel.STANDARD, self.strictness):
            new_text = _ROLE_MARKER_RE.sub("", text)
            if new_text != text:
                modified = True
                text = new_text

        return text, modified

    # -- Public API ---------------------------------------------------------

    def sanitize(self, text: str, source: str = "user") -> SanitizationResult:
        """Run the full sanitization pipeline on *text*.

        Parameters
        ----------
        text:
            Raw input string.
        source:
            Label indicating where the text came from (for logging).

        Returns
        -------
        SanitizationResult
            Contains the cleaned text, a flag indicating whether it was
            altered, and the list of detected threat labels.
        """
        # Layer 1 -- detection
        threats = self._detect_patterns(text)
        if threats:
            logger.warning(
                "Injection patterns detected in %s input: %s",
                source,
                ", ".join(threats),
            )

        # Layer 2 -- structural cleanup
        clean_text, struct_modified = self._sanitize_structure(text)

        was_modified = struct_modified or bool(threats)

        if was_modified:
            logger.info(
                "Input from %s was sanitized (threats=%s, structural_changes=%s)",
                source,
                threats,
                struct_modified,
            )

        return SanitizationResult(
            clean_text=clean_text,
            was_modified=was_modified,
            threats_detected=threats,
        )

    def is_safe(self, text: str) -> bool:
        """Quick safety check -- returns ``True`` if no threats detected.

        Unlike :meth:`sanitize`, this does **not** modify the text and is
        therefore cheaper for pre-flight validation.
        """
        return len(self._detect_patterns(text)) == 0
