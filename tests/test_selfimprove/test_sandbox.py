"""Tests for the SandboxManager."""

from pathlib import Path

import pytest

from meept.selfimprove.config import SandboxConfig
from meept.selfimprove.sandbox import SandboxError, SandboxManager
from meept.selfimprove.models import FilePatch


class TestSandboxManager:
    """Tests for SandboxManager."""

    @pytest.fixture
    def sandbox(
        self,
        sandbox_config: SandboxConfig,
        temp_project: Path,
    ) -> SandboxManager:
        """Create a SandboxManager instance."""
        return SandboxManager(sandbox_config, repo_root=temp_project)

    @pytest.mark.asyncio
    async def test_create_worktree(
        self,
        sandbox: SandboxManager,
    ) -> None:
        """Test creating a worktree."""
        worktree = await sandbox.create_worktree("test-fix-001")
        assert worktree.exists()
        assert "test-fix-001" in sandbox.active_worktrees
        await sandbox.cleanup_worktree(worktree, force=True)

    @pytest.mark.asyncio
    async def test_max_worktrees(
        self,
        sandbox: SandboxManager,
    ) -> None:
        """Test worktree limit enforcement."""
        # Create max worktrees
        w1 = await sandbox.create_worktree("fix-001")
        w2 = await sandbox.create_worktree("fix-002")

        # Should raise on third
        with pytest.raises(SandboxError, match="Maximum worktrees"):
            await sandbox.create_worktree("fix-003")

        # Cleanup
        await sandbox.cleanup_all(force=True)

    @pytest.mark.asyncio
    async def test_cleanup_all(
        self,
        sandbox: SandboxManager,
    ) -> None:
        """Test cleaning up all worktrees."""
        await sandbox.create_worktree("fix-001")
        await sandbox.create_worktree("fix-002")
        assert len(sandbox.active_worktrees) == 2

        count = await sandbox.cleanup_all(force=True)
        assert count == 2
        assert len(sandbox.active_worktrees) == 0

    @pytest.mark.asyncio
    async def test_list_worktrees(
        self,
        sandbox: SandboxManager,
    ) -> None:
        """Test listing worktrees."""
        worktrees = await sandbox.list_worktrees()
        # Should include at least the main worktree
        assert isinstance(worktrees, list)
