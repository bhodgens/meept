"""Data models for the ClawSkills subsystem.

Defines provenance metadata (written as ``.origin.json`` per skill) and a
lock file that tracks every installed clawskill.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

import json


# ---------------------------------------------------------------------------
# Per-skill provenance
# ---------------------------------------------------------------------------


@dataclass(slots=True)
class OriginMetadata:
    """Provenance stored as ``.origin.json`` inside each installed skill dir."""

    slug: str = ""
    version: str = ""
    sha256: str = ""
    installed_at: str = ""
    source_url: str = ""
    files: list[str] = field(default_factory=list)

    def to_dict(self) -> dict[str, Any]:
        return {
            "slug": self.slug,
            "version": self.version,
            "sha256": self.sha256,
            "installed_at": self.installed_at,
            "source_url": self.source_url,
            "files": self.files,
        }

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> OriginMetadata:
        return cls(
            slug=str(data.get("slug", "")),
            version=str(data.get("version", "")),
            sha256=str(data.get("sha256", "")),
            installed_at=str(data.get("installed_at", "")),
            source_url=str(data.get("source_url", "")),
            files=list(data.get("files", [])),
        )

    def save(self, path: Path) -> None:
        """Write to *path* as JSON."""
        path.write_text(json.dumps(self.to_dict(), indent=2), encoding="utf-8")

    @classmethod
    def load(cls, path: Path) -> OriginMetadata:
        """Read from *path*."""
        data = json.loads(path.read_text(encoding="utf-8"))
        return cls.from_dict(data)


# ---------------------------------------------------------------------------
# Lock file
# ---------------------------------------------------------------------------


@dataclass(slots=True)
class LockFileEntry:
    """Per-skill record inside the lock file."""

    slug: str = ""
    version: str = ""
    sha256: str = ""
    installed_at: str = ""
    files: list[str] = field(default_factory=list)

    def to_dict(self) -> dict[str, Any]:
        return {
            "slug": self.slug,
            "version": self.version,
            "sha256": self.sha256,
            "installed_at": self.installed_at,
            "files": self.files,
        }

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> LockFileEntry:
        return cls(
            slug=str(data.get("slug", "")),
            version=str(data.get("version", "")),
            sha256=str(data.get("sha256", "")),
            installed_at=str(data.get("installed_at", "")),
            files=list(data.get("files", [])),
        )


@dataclass(slots=True)
class LockFile:
    """``~/.meept/clawskills/.lock.json`` -- tracks all installed clawskills."""

    schema_version: int = 1
    entries: dict[str, LockFileEntry] = field(default_factory=dict)
    last_updated: str = ""

    def to_dict(self) -> dict[str, Any]:
        return {
            "schema_version": self.schema_version,
            "entries": {k: v.to_dict() for k, v in self.entries.items()},
            "last_updated": self.last_updated,
        }

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> LockFile:
        entries_raw = data.get("entries", {})
        entries = {
            k: LockFileEntry.from_dict(v) for k, v in entries_raw.items()
        }
        return cls(
            schema_version=int(data.get("schema_version", 1)),
            entries=entries,
            last_updated=str(data.get("last_updated", "")),
        )

    def save(self, path: Path) -> None:
        """Write lock file to *path*."""
        self.last_updated = datetime.now(timezone.utc).isoformat()
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps(self.to_dict(), indent=2), encoding="utf-8")

    @classmethod
    def load(cls, path: Path) -> LockFile:
        """Read lock file from *path*, returning empty lock if missing."""
        if not path.is_file():
            return cls()
        try:
            data = json.loads(path.read_text(encoding="utf-8"))
            return cls.from_dict(data)
        except (json.JSONDecodeError, OSError):
            return cls()

    def add(self, entry: LockFileEntry) -> None:
        """Add or replace an entry."""
        self.entries[entry.slug] = entry

    def remove(self, slug: str) -> None:
        """Remove an entry by slug."""
        self.entries.pop(slug, None)

    def get(self, slug: str) -> LockFileEntry | None:
        """Look up an entry by slug."""
        return self.entries.get(slug)
