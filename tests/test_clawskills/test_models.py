"""Tests for clawskills data models."""

from __future__ import annotations

import json
from pathlib import Path

from meept.clawskills.models import LockFile, LockFileEntry, OriginMetadata


class TestOriginMetadata:
    def test_defaults(self) -> None:
        origin = OriginMetadata()
        assert origin.slug == ""
        assert origin.version == ""
        assert origin.sha256 == ""
        assert origin.files == []

    def test_round_trip_dict(self) -> None:
        origin = OriginMetadata(
            slug="gifgrep",
            version="1.0.0",
            sha256="abc123",
            installed_at="2025-01-01T00:00:00Z",
            source_url="https://clawhub.ai/download?slug=gifgrep",
            files=["gifgrep/SKILL.md", "gifgrep/README.md"],
        )
        d = origin.to_dict()
        restored = OriginMetadata.from_dict(d)
        assert restored.slug == origin.slug
        assert restored.version == origin.version
        assert restored.sha256 == origin.sha256
        assert restored.installed_at == origin.installed_at
        assert restored.source_url == origin.source_url
        assert restored.files == origin.files

    def test_save_and_load(self, tmp_path: Path) -> None:
        origin = OriginMetadata(
            slug="test", version="2.0.0", sha256="def456",
        )
        path = tmp_path / ".origin.json"
        origin.save(path)

        loaded = OriginMetadata.load(path)
        assert loaded.slug == "test"
        assert loaded.version == "2.0.0"
        assert loaded.sha256 == "def456"


class TestLockFileEntry:
    def test_round_trip_dict(self) -> None:
        entry = LockFileEntry(
            slug="gifgrep", version="1.0.0", sha256="aaa",
            installed_at="2025-01-01", files=["SKILL.md"],
        )
        restored = LockFileEntry.from_dict(entry.to_dict())
        assert restored.slug == entry.slug
        assert restored.version == entry.version
        assert restored.files == entry.files


class TestLockFile:
    def test_empty_defaults(self) -> None:
        lock = LockFile()
        assert lock.schema_version == 1
        assert lock.entries == {}

    def test_add_and_get(self) -> None:
        lock = LockFile()
        entry = LockFileEntry(slug="gifgrep", version="1.0.0")
        lock.add(entry)
        assert lock.get("gifgrep") is entry
        assert lock.get("missing") is None

    def test_remove(self) -> None:
        lock = LockFile()
        lock.add(LockFileEntry(slug="a"))
        lock.add(LockFileEntry(slug="b"))
        lock.remove("a")
        assert lock.get("a") is None
        assert lock.get("b") is not None

    def test_remove_nonexistent(self) -> None:
        lock = LockFile()
        lock.remove("missing")  # Should not raise.

    def test_save_and_load(self, tmp_path: Path) -> None:
        lock = LockFile()
        lock.add(LockFileEntry(slug="s1", version="1.0"))
        lock.add(LockFileEntry(slug="s2", version="2.0"))

        path = tmp_path / ".lock.json"
        lock.save(path)

        loaded = LockFile.load(path)
        assert loaded.schema_version == 1
        assert len(loaded.entries) == 2
        assert loaded.get("s1") is not None
        assert loaded.get("s2") is not None
        assert loaded.last_updated != ""

    def test_load_missing_file(self, tmp_path: Path) -> None:
        loaded = LockFile.load(tmp_path / "nonexistent.json")
        assert loaded.entries == {}

    def test_load_corrupt_file(self, tmp_path: Path) -> None:
        path = tmp_path / ".lock.json"
        path.write_text("not json", encoding="utf-8")
        loaded = LockFile.load(path)
        assert loaded.entries == {}

    def test_round_trip_dict(self) -> None:
        lock = LockFile()
        lock.add(LockFileEntry(slug="x", version="3.0", sha256="hhh"))
        d = lock.to_dict()
        restored = LockFile.from_dict(d)
        assert restored.get("x") is not None
        assert restored.get("x").sha256 == "hhh"
