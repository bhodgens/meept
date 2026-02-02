"""Tests for episodic memory (SQLite/FTS5 fallback)."""

from __future__ import annotations

import asyncio
from pathlib import Path

import pytest

from meept.memory.episodic import EpisodicMemory


# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------


@pytest.fixture()
async def episodic(tmp_path: Path) -> EpisodicMemory:
    """Create and initialise an EpisodicMemory backed by a temp directory."""
    mem = EpisodicMemory()
    data_dir = tmp_path / "memory"
    data_dir.mkdir()
    await mem.initialize(data_dir)
    yield mem
    await mem.close()


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------


async def test_store_and_search(episodic: EpisodicMemory) -> None:
    """Storing text and then searching for it should return a match."""
    await episodic.store("The quick brown fox jumps over the lazy dog", category="conversation")
    await episodic.store("Python asyncio event loop basics", category="code")

    results = await episodic.search("quick brown fox", limit=5)

    assert len(results) >= 1
    assert any("fox" in r.item.content for r in results)


async def test_get_recent(episodic: EpisodicMemory) -> None:
    """get_recent() should return memories ordered by creation time (newest first)."""
    await episodic.store("First memory", category="conversation")
    await episodic.store("Second memory", category="conversation")
    await episodic.store("Third memory", category="conversation")

    recent = await episodic.get_recent(limit=10)

    assert len(recent) == 3
    # Most recent should come first.
    assert recent[0].item.content == "Third memory"
    assert recent[2].item.content == "First memory"


async def test_category_filter(episodic: EpisodicMemory) -> None:
    """get_by_category() should return only memories in the specified category."""
    await episodic.store("A conversation about cats", category="conversation")
    await episodic.store("git rebase --interactive", category="code")
    await episodic.store("Another chat about dogs", category="conversation")

    conv_results = await episodic.get_by_category("conversation", limit=10)
    code_results = await episodic.get_by_category("code", limit=10)

    assert len(conv_results) == 2
    assert len(code_results) == 1
    assert all(r.item.category == "conversation" for r in conv_results)
    assert code_results[0].item.category == "code"


async def test_empty_search(episodic: EpisodicMemory) -> None:
    """Searching for an irrelevant query should return no FTS matches (falls back to recent)."""
    await episodic.store("Information about Python decorators", category="code")

    # A query with words not present in any stored memory.  FTS5 will not
    # match, and the implementation falls back to get_recent() when the
    # sanitised query is non-empty but yields no FTS results.
    results = await episodic.search("xylophone zeppelin", limit=5)

    # Either empty or a fallback to recent -- both are acceptable.
    # The important thing is no exception is raised.
    assert isinstance(results, list)
