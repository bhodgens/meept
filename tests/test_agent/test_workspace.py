"""Tests for WorkspaceManager."""

from __future__ import annotations

import asyncio
from pathlib import Path

import pytest

from meept.agent.workspace import WorkspaceManager
from meept.models.tasks import TaskPlan, TaskStep


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _make_plan(task_id: str = "test_plan") -> TaskPlan:
    return TaskPlan(
        id=task_id,
        description="Test task plan",
        steps=[
            TaskStep(id="s1", description="First step"),
            TaskStep(id="s2", description="Second step", depends_on=["s1"]),
        ],
    )


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_create_workspace(tmp_path: Path) -> None:
    """create() should make a directory with a git repo and README."""
    mgr = WorkspaceManager(base_dir=tmp_path, auto_commit=True)

    ws = await mgr.create("task1", "Test description")

    assert ws.is_dir()
    assert (ws / ".git").is_dir()
    assert (ws / "README.md").exists()
    content = (ws / "README.md").read_text()
    assert "task1" in content
    assert "Test description" in content


@pytest.mark.asyncio
async def test_get_path(tmp_path: Path) -> None:
    """get_path() should return the workspace path after creation."""
    mgr = WorkspaceManager(base_dir=tmp_path, auto_commit=False)

    assert await mgr.get_path("nonexistent") is None

    await mgr.create("t1", "Desc")
    path = await mgr.get_path("t1")
    assert path is not None
    assert path.name == "t1"


@pytest.mark.asyncio
async def test_commit(tmp_path: Path) -> None:
    """commit() should create a git commit."""
    mgr = WorkspaceManager(base_dir=tmp_path, auto_commit=False)
    ws = await mgr.create("t1", "Desc")

    # git init was done, add the README manually.
    ok = await mgr.commit("t1", "Initial commit")
    assert ok is True

    # Check git log.
    log_output = await mgr.log("t1")
    assert "Initial commit" in log_output


@pytest.mark.asyncio
async def test_commit_unknown_task(tmp_path: Path) -> None:
    """commit() should return False for unknown task_id."""
    mgr = WorkspaceManager(base_dir=tmp_path)
    result = await mgr.commit("unknown", "msg")
    assert result is False


@pytest.mark.asyncio
async def test_write_plan(tmp_path: Path) -> None:
    """write_plan() should create PLAN.md."""
    mgr = WorkspaceManager(base_dir=tmp_path, auto_commit=False)
    await mgr.create("t1", "Desc")

    plan = _make_plan("t1")
    plan_path = await mgr.write_plan("t1", plan)

    assert plan_path.name == "PLAN.md"
    assert plan_path.exists()
    content = plan_path.read_text()
    assert "First step" in content
    assert "Second step" in content
    assert "depends on" in content


@pytest.mark.asyncio
async def test_write_review(tmp_path: Path) -> None:
    """write_review() should create REVIEW.md."""
    mgr = WorkspaceManager(base_dir=tmp_path, auto_commit=False)
    await mgr.create("t1", "Desc")

    review_path = await mgr.write_review("t1", "Looks good overall, minor issues.")

    assert review_path.name == "REVIEW.md"
    assert review_path.exists()
    content = review_path.read_text()
    assert "Looks good overall" in content


@pytest.mark.asyncio
async def test_append_log(tmp_path: Path) -> None:
    """append_log() should append entries to LOG.md."""
    mgr = WorkspaceManager(base_dir=tmp_path, auto_commit=False)
    await mgr.create("t1", "Desc")

    await mgr.append_log("t1", "Step 1 started")
    await mgr.append_log("t1", "Step 1 completed")

    log_path = tmp_path / "t1" / "LOG.md"
    assert log_path.exists()
    content = log_path.read_text()
    assert "Step 1 started" in content
    assert "Step 1 completed" in content


@pytest.mark.asyncio
async def test_status(tmp_path: Path) -> None:
    """status() should return git status output."""
    mgr = WorkspaceManager(base_dir=tmp_path, auto_commit=False)
    await mgr.create("t1", "Desc")

    # There should be untracked files (README.md).
    status = await mgr.status("t1")
    assert status  # Non-empty since README isn't committed yet.


@pytest.mark.asyncio
async def test_cleanup(tmp_path: Path) -> None:
    """cleanup() should remove the workspace directory."""
    mgr = WorkspaceManager(base_dir=tmp_path, auto_commit=False)
    ws = await mgr.create("t1", "Desc")
    assert ws.is_dir()

    await mgr.cleanup("t1")
    assert not ws.exists()
    assert await mgr.get_path("t1") is None


@pytest.mark.asyncio
async def test_cleanup_nonexistent(tmp_path: Path) -> None:
    """cleanup() should be a no-op for unknown task_id."""
    mgr = WorkspaceManager(base_dir=tmp_path)
    # Should not raise.
    await mgr.cleanup("nonexistent")


@pytest.mark.asyncio
async def test_write_plan_no_workspace(tmp_path: Path) -> None:
    """write_plan() should raise ValueError for unknown task_id."""
    mgr = WorkspaceManager(base_dir=tmp_path)
    plan = _make_plan()
    with pytest.raises(ValueError, match="No workspace"):
        await mgr.write_plan("unknown", plan)


@pytest.mark.asyncio
async def test_write_review_no_workspace(tmp_path: Path) -> None:
    """write_review() should raise ValueError for unknown task_id."""
    mgr = WorkspaceManager(base_dir=tmp_path)
    with pytest.raises(ValueError, match="No workspace"):
        await mgr.write_review("unknown", "analysis")


@pytest.mark.asyncio
async def test_auto_commit_on_create(tmp_path: Path) -> None:
    """With auto_commit=True, create() should produce a commit."""
    mgr = WorkspaceManager(base_dir=tmp_path, auto_commit=True)
    await mgr.create("t1", "Auto-commit test")

    log_output = await mgr.log("t1")
    assert "Initial workspace setup" in log_output


@pytest.mark.asyncio
async def test_auto_commit_on_plan(tmp_path: Path) -> None:
    """With auto_commit=True, write_plan() should produce a commit."""
    mgr = WorkspaceManager(base_dir=tmp_path, auto_commit=True)
    await mgr.create("t1", "Desc")

    plan = _make_plan("t1")
    await mgr.write_plan("t1", plan)

    log_output = await mgr.log("t1")
    assert "Add task plan" in log_output


@pytest.mark.asyncio
async def test_commit_specific_paths(tmp_path: Path) -> None:
    """commit() with specific paths should only stage those files."""
    mgr = WorkspaceManager(base_dir=tmp_path, auto_commit=False)
    ws = await mgr.create("t1", "Desc")

    # Create two files.
    (ws / "a.txt").write_text("file a")
    (ws / "b.txt").write_text("file b")

    # Commit only a.txt.
    ok = await mgr.commit("t1", "Add a.txt", paths=["a.txt"])
    assert ok is True

    # b.txt should still show as untracked.
    status = await mgr.status("t1")
    assert "b.txt" in status
