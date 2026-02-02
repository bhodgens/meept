"""Output validation and credential-redaction pipeline.

The :class:`OutputMonitor` scans outgoing text for sensitive data --
API keys, tokens, passwords, private key material, sensitive file paths,
and potential data-exfiltration indicators -- before it reaches the user
or an external service.
"""

from __future__ import annotations

import logging
import re
from dataclasses import dataclass, field

logger = logging.getLogger(__name__)

_REDACTED = "[REDACTED]"


# ---------------------------------------------------------------------------
# Detection pattern catalogue
# ---------------------------------------------------------------------------
# Each entry: (compiled_regex, human-readable issue label).

_CREDENTIAL_PATTERNS: list[tuple[re.Pattern[str], str]] = [
    # --- API keys ---
    (
        re.compile(r"\b(sk-[A-Za-z0-9]{20,})\b"),
        "openai_api_key",
    ),
    (
        re.compile(r"\b(sk-ant-[A-Za-z0-9\-]{20,})\b"),
        "anthropic_api_key",
    ),
    (
        re.compile(r"\bAIza[0-9A-Za-z_\-]{35}\b"),
        "google_api_key",
    ),
    (
        re.compile(r"\b(ghp_[A-Za-z0-9]{36,})\b"),
        "github_pat",
    ),
    (
        re.compile(r"\b(gho_[A-Za-z0-9]{36,})\b"),
        "github_oauth_token",
    ),
    (
        re.compile(r"\b(glpat-[A-Za-z0-9\-_]{20,})\b"),
        "gitlab_pat",
    ),
    (
        re.compile(r"\bAKIA[0-9A-Z]{16}\b"),
        "aws_access_key_id",
    ),
    (
        re.compile(r"(?<![A-Za-z0-9/+])[A-Za-z0-9/+]{40}(?![A-Za-z0-9/+=])"),
        "possible_aws_secret_key",
    ),
    (
        re.compile(r"\b(xox[bpas]-[A-Za-z0-9\-]{10,})\b"),
        "slack_token",
    ),
    (
        re.compile(r"\b(sq0[a-z]{3}-[A-Za-z0-9\-_]{22,})\b"),
        "square_token",
    ),
    # --- Generic secret patterns ---
    (
        re.compile(
            r"(?i)(api[_\-]?key|api[_\-]?secret|secret[_\-]?key|access[_\-]?token"
            r"|auth[_\-]?token|bearer)\s*[:=]\s*['\"]?([A-Za-z0-9\-_.~+/]{16,})['\"]?"
        ),
        "generic_api_credential",
    ),
    (
        re.compile(
            r"(?i)(password|passwd|pwd)\s*[:=]\s*['\"]?(\S{6,})['\"]?"
        ),
        "password_in_output",
    ),
    # --- Private key material ---
    (
        re.compile(r"-----BEGIN\s+(RSA\s+|EC\s+|DSA\s+|OPENSSH\s+)?PRIVATE\s+KEY-----"),
        "private_key_material",
    ),
    # --- JWT tokens ---
    (
        re.compile(r"\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b"),
        "jwt_token",
    ),
]

# Sensitive filesystem paths.
_SENSITIVE_PATH_PATTERNS: list[tuple[re.Pattern[str], str]] = [
    (re.compile(r"(/etc/shadow|/etc/master\.passwd)"), "shadow_file_path"),
    (re.compile(r"~?/\.ssh/(id_[a-z]+|authorized_keys|known_hosts)"), "ssh_key_path"),
    (re.compile(r"~?/\.gnupg/"), "gpg_directory"),
    (re.compile(r"~?/\.aws/credentials"), "aws_credentials_file"),
    (re.compile(r"~?/\.netrc"), "netrc_file"),
    (re.compile(r"~?/\.env\b"), "dotenv_file"),
    (re.compile(r"/etc/passwd"), "passwd_file"),
]

# Data-exfiltration indicators.
_EXFILTRATION_PATTERNS: list[tuple[re.Pattern[str], str]] = [
    (
        re.compile(
            r"(?i)(curl|wget|httpx?\.post|requests\.post|fetch)\s*\(?\s*['\"]"
            r"https?://[^'\"]+['\"].*?(password|secret|token|key|credential)",
            re.DOTALL,
        ),
        "credential_exfiltration_attempt",
    ),
    (
        re.compile(
            r"(?i)base64[.\s]*(encode|b64encode)\s*\(.*?(password|secret|token|key)",
            re.DOTALL,
        ),
        "base64_encoding_credential",
    ),
    (
        re.compile(
            r"(?i)(subprocess|os\.system|os\.popen)\s*\(.*?"
            r"(curl|wget|nc\s|ncat|netcat)\b",
            re.DOTALL,
        ),
        "subprocess_network_exfiltration",
    ),
]


# ---------------------------------------------------------------------------
# OutputMonitor
# ---------------------------------------------------------------------------


class OutputMonitor:
    """Scans agent output for sensitive content and can redact it.

    Designed to be called on every piece of text the agent produces
    before it is sent to a user or external service.
    """

    def check_output(self, content: str) -> tuple[bool, list[str]]:
        """Analyse *content* for sensitive data.

        Returns
        -------
        tuple[bool, list[str]]
            ``(safe, issues)`` where *safe* is ``True`` if no issues were
            found and *issues* is a list of human-readable problem labels.
        """
        issues: list[str] = []

        for pattern, label in _CREDENTIAL_PATTERNS:
            if pattern.search(content):
                issues.append(label)

        for pattern, label in _SENSITIVE_PATH_PATTERNS:
            if pattern.search(content):
                issues.append(label)

        for pattern, label in _EXFILTRATION_PATTERNS:
            if pattern.search(content):
                issues.append(label)

        if issues:
            logger.warning(
                "Output monitor detected %d issue(s): %s",
                len(issues),
                ", ".join(issues),
            )

        return (len(issues) == 0), issues

    def redact_sensitive(self, content: str) -> str:
        """Return a copy of *content* with detected credentials replaced.

        Only credential and private-key patterns are redacted; sensitive
        paths and exfiltration indicators are flagged but left intact so
        the caller can decide how to handle them.
        """
        result = content

        for pattern, label in _CREDENTIAL_PATTERNS:
            if pattern.search(result):
                result = pattern.sub(_REDACTED, result)
                logger.info("Redacted %s from output", label)

        return result
