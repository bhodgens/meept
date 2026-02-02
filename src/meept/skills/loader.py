"""Skill TOML file discovery and parsing.

The :class:`SkillLoader` scans a directory for ``.toml`` files and
deserialises each one into a :class:`SkillDefinition`.
"""

from __future__ import annotations

import logging
from pathlib import Path
from typing import Any

from meept.skills.models import SkillDefinition

log = logging.getLogger(__name__)

# Python 3.11+ has tomllib in the stdlib.
try:
    import tomllib  # type: ignore[import-not-found]
except ModuleNotFoundError:
    import tomli as tomllib  # type: ignore[no-redef]


class SkillLoader:
    """Discovers and parses skill ``.toml`` files from a directory.

    Parameters
    ----------
    directory:
        Path to the skills directory (e.g. ``~/.meept/skills``).
    """

    def __init__(self, directory: str | Path) -> None:
        self._directory = Path(directory).expanduser().resolve()

    @property
    def directory(self) -> Path:
        return self._directory

    def load_all(self) -> list[SkillDefinition]:
        """Scan the directory and return all valid skill definitions.

        Invalid TOML files are logged and skipped.
        """
        if not self._directory.is_dir():
            log.warning("Skills directory does not exist: %s", self._directory)
            return []

        skills: list[SkillDefinition] = []
        for toml_path in sorted(self._directory.glob("*.toml")):
            try:
                skill = self.load_file(toml_path)
                if skill is not None:
                    skills.append(skill)
            except Exception:
                log.warning("Failed to load skill file: %s", toml_path, exc_info=True)

        log.info("Loaded %d skill(s) from %s", len(skills), self._directory)
        return skills

    def load_file(self, path: Path) -> SkillDefinition | None:
        """Parse a single ``.toml`` file into a :class:`SkillDefinition`.

        Returns ``None`` if the file lacks a ``name`` field.
        """
        data = _read_toml(path)
        if not data.get("name"):
            log.warning("Skill file %s has no 'name' field; skipping", path)
            return None
        return _dict_to_skill(data)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _read_toml(path: Path) -> dict[str, Any]:
    """Read and parse a TOML file."""
    with open(path, "rb") as fh:
        return tomllib.load(fh)


def _dict_to_skill(data: dict[str, Any]) -> SkillDefinition:
    """Convert a raw TOML dict into a SkillDefinition."""
    return SkillDefinition(
        name=str(data.get("name", "")),
        description=str(data.get("description", "")),
        model=str(data.get("model", "default")),
        system_prompt=str(data.get("system_prompt", "")),
        instructions=str(data.get("instructions", "")),
        allowed_tools=list(data.get("allowed_tools", [])),
        temperature=data.get("temperature"),
        max_tokens=data.get("max_tokens"),
        risk_level=str(data.get("risk_level", "medium")),
        trigger_keywords=list(data.get("trigger_keywords", [])),
        examples=list(data.get("examples", [])),
        max_iterations=int(data.get("max_iterations", 10)),
    )
