"""SKILL.md parser -- YAML frontmatter + Markdown body.

Parses skill definition files in the OpenCode-style ``SKILL.md`` format:
a YAML frontmatter block (delimited by ``---``) followed by a Markdown body
containing instructions and examples.
"""

from __future__ import annotations

import logging
from dataclasses import dataclass, field
from pathlib import Path

import yaml

log = logging.getLogger(__name__)


@dataclass(slots=True)
class SkillMetadata:
    """Structured metadata extracted from the YAML frontmatter."""

    name: str = ""
    description: str = ""
    requires: list[str] = field(default_factory=list)
    allowed_tools: list[str] = field(default_factory=list)
    risk_level: str = "medium"
    max_iterations: int = 10
    temperature: float | None = None
    max_tokens: int | None = None


@dataclass(slots=True)
class ParsedSkill:
    """A fully parsed SKILL.md file: metadata + Markdown body."""

    metadata: SkillMetadata
    body: str
    source_path: Path | None = None


def parse_skill_file(path: Path) -> ParsedSkill | None:
    """Parse a ``SKILL.md`` file into a :class:`ParsedSkill`.

    Returns ``None`` if the file cannot be parsed or has no ``name``.
    """
    try:
        text = path.read_text(encoding="utf-8")
    except OSError:
        log.warning("Could not read skill file: %s", path)
        return None

    return parse_skill_text(text, source_path=path)


def parse_skill_text(text: str, source_path: Path | None = None) -> ParsedSkill | None:
    """Parse raw SKILL.md text into a :class:`ParsedSkill`.

    Returns ``None`` if the frontmatter is invalid or has no ``name``.
    """
    frontmatter, body = _split_frontmatter(text)
    if frontmatter is None:
        log.warning("No YAML frontmatter found in skill file: %s", source_path)
        return None

    try:
        data = yaml.safe_load(frontmatter)
    except yaml.YAMLError:
        log.warning("Invalid YAML frontmatter in skill file: %s", source_path, exc_info=True)
        return None

    if not isinstance(data, dict):
        log.warning("Frontmatter is not a mapping in skill file: %s", source_path)
        return None

    name = data.get("name", "")
    if not name:
        log.warning("Skill file has no 'name' in frontmatter: %s", source_path)
        return None

    requires_raw = data.get("requires", [])
    if isinstance(requires_raw, str):
        requires_raw = [r.strip() for r in requires_raw.split(",")]

    allowed_raw = data.get("allowed-tools", data.get("allowed_tools", []))
    if isinstance(allowed_raw, str):
        allowed_raw = [t.strip() for t in allowed_raw.split(",")]

    metadata = SkillMetadata(
        name=str(name),
        description=str(data.get("description", "")),
        requires=list(requires_raw),
        allowed_tools=list(allowed_raw),
        risk_level=str(data.get("risk-level", data.get("risk_level", "medium"))),
        max_iterations=int(data.get("max-iterations", data.get("max_iterations", 10))),
        temperature=data.get("temperature"),
        max_tokens=data.get("max-tokens", data.get("max_tokens")),
    )

    return ParsedSkill(
        metadata=metadata,
        body=body.strip(),
        source_path=source_path,
    )


def _split_frontmatter(text: str) -> tuple[str | None, str]:
    """Split ``---``-delimited YAML frontmatter from the Markdown body.

    Returns ``(frontmatter_text, body_text)``.  If no valid frontmatter
    delimiters are found, returns ``(None, original_text)``.
    """
    stripped = text.lstrip()
    if not stripped.startswith("---"):
        return None, text

    # Find the closing ---
    first_break = stripped.index("---")
    rest = stripped[first_break + 3:]
    newline_pos = rest.find("\n")
    if newline_pos == -1:
        return None, text

    rest = rest[newline_pos + 1:]

    close_pos = rest.find("\n---")
    if close_pos == -1:
        # Try end-of-string ---
        if rest.rstrip().endswith("---"):
            close_pos = rest.rstrip().rfind("---")
            frontmatter = rest[:close_pos]
            body = ""
        else:
            return None, text
    else:
        frontmatter = rest[:close_pos]
        body = rest[close_pos + 4:]

    return frontmatter, body
