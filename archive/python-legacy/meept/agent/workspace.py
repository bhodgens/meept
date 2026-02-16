"""Per-task git workspace manager.

Creates isolated directories under a configurable base path (default
``~/.meept/workspaces/``), initialises each as a git repository, and
provides async helpers for committing plans, reviews, and artifacts at
every lifecycle stage.
"""

from __future__ import annotations

import asyncio
import logging
import shutil
from datetime import UTC, datetime
from pathlib import Path
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from meept.models.tasks import TaskPlan

log = logging.getLogger(__name__)


class WorkspaceManager:
    """Manage per-task workspace directories with git tracking.

    Parameters
    ----------
    base_dir:
        Root directory under which individual task workspaces are created.
        Defaults to ``~/.meept/workspaces``.
    auto_commit:
        When *True* (default), lifecycle helpers like :meth:`write_plan`
        and :meth:`write_review` automatically commit after writing.
    """

    def __init__(
        self,
        base_dir: Path | None = None,
        auto_commit: bool = True,
    ) -> None:
        self._base_dir = (base_dir or Path("~/.meept/workspaces")).expanduser()
        self._auto_commit = auto_commit
        # task_id -> workspace path
        self._workspaces: dict[str, Path] = {}

    # ------------------------------------------------------------------
    # Public API
    # ------------------------------------------------------------------

    async def create(self, task_id: str, description: str) -> Path:
        """Create a new workspace directory and ``git init`` it.

        Parameters
        ----------
        task_id:
            Unique identifier for the task.
        description:
            Human-readable task description (written to ``README.md``).

        Returns
        -------
        Path
            The workspace directory path.
        """
        workspace = self._base_dir / task_id
        workspace.mkdir(parents=True, exist_ok=True)
        self._workspaces[task_id] = workspace

        # Initialise git repo.
        ok, _ = await self._git(workspace, "init")
        if not ok:
            log.warning("workspace: git init failed for %s", task_id)

        # Write a README with the task description.
        readme = workspace / "README.md"
        readme.write_text(
            f"# Task: {task_id}\n\n{description}\n\n"
            f"Created: {datetime.now(UTC).isoformat()}\n",
            encoding="utf-8",
        )

        if self._auto_commit:
            await self.commit(task_id, "Initial workspace setup")

        log.info("workspace: created %s at %s", task_id, workspace)
        return workspace

    async def commit(
        self,
        task_id: str,
        message: str,
        paths: list[str] | None = None,
    ) -> bool:
        """Stage and commit changes in the task workspace.

        Parameters
        ----------
        task_id:
            The task whose workspace to commit in.
        message:
            Git commit message.
        paths:
            Specific paths to ``git add``.  When *None*, stages everything
            (``git add -A``).

        Returns
        -------
        bool
            ``True`` if the commit succeeded.
        """
        workspace = self._workspaces.get(task_id)
        if workspace is None:
            log.warning("workspace: commit called for unknown task %s", task_id)
            return False

        if paths:
            for p in paths:
                await self._git(workspace, "add", p)
        else:
            await self._git(workspace, "add", "-A")

        ok, output = await self._git(workspace, "commit", "-m", message, "--allow-empty")
        if not ok:
            # "nothing to commit" is not a real failure.
            if "nothing to commit" in output:
                return True
            log.warning("workspace: commit failed for %s: %s", task_id, output)
            return False
        return True

    async def write_plan(self, task_id: str, plan: TaskPlan) -> Path:
        """Write ``PLAN.md`` into the workspace and optionally commit.

        Parameters
        ----------
        task_id:
            The task identifier.
        plan:
            The :class:`TaskPlan` to serialise.

        Returns
        -------
        Path
            Path to the written ``PLAN.md``.
        """
        workspace = self._workspaces.get(task_id)
        if workspace is None:
            raise ValueError(f"No workspace for task {task_id}")

        plan_path = workspace / "PLAN.md"
        lines = [f"# Plan: {plan.description}\n"]
        for i, step in enumerate(plan.steps, 1):
            deps = f" (depends on: {', '.join(step.depends_on)})" if step.depends_on else ""
            lines.append(f"{i}. **{step.id}**: {step.description}{deps}")
        lines.append("")

        plan_path.write_text("\n".join(lines), encoding="utf-8")

        if self._auto_commit:
            await self.commit(task_id, "Add task plan")

        return plan_path

    async def write_review(self, task_id: str, analysis: str) -> Path:
        """Write ``REVIEW.md`` with the LLM analysis and optionally commit.

        Parameters
        ----------
        task_id:
            The task identifier.
        analysis:
            The review/analysis text.

        Returns
        -------
        Path
            Path to the written ``REVIEW.md``.
        """
        workspace = self._workspaces.get(task_id)
        if workspace is None:
            raise ValueError(f"No workspace for task {task_id}")

        review_path = workspace / "REVIEW.md"
        review_path.write_text(
            f"# Plan Review\n\n{analysis}\n\n"
            f"Reviewed: {datetime.now(UTC).isoformat()}\n",
            encoding="utf-8",
        )

        if self._auto_commit:
            await self.commit(task_id, "Add plan review")

        return review_path

    async def append_log(self, task_id: str, entry: str) -> None:
        """Append an entry to the workspace ``LOG.md``.

        Parameters
        ----------
        task_id:
            The task identifier.
        entry:
            A log line to append.
        """
        workspace = self._workspaces.get(task_id)
        if workspace is None:
            return

        log_path = workspace / "LOG.md"
        timestamp = datetime.now(UTC).isoformat()
        with log_path.open("a", encoding="utf-8") as fh:
            fh.write(f"- [{timestamp}] {entry}\n")

    async def get_path(self, task_id: str) -> Path | None:
        """Return the workspace path for *task_id*, or ``None``."""
        return self._workspaces.get(task_id)

    async def status(self, task_id: str) -> str:
        """Return ``git status --short`` output for the workspace."""
        workspace = self._workspaces.get(task_id)
        if workspace is None:
            return ""
        _, output = await self._git(workspace, "status", "--short")
        return output

    async def log(self, task_id: str, max_entries: int = 20) -> str:
        """Return recent ``git log --oneline`` output for the workspace."""
        workspace = self._workspaces.get(task_id)
        if workspace is None:
            return ""
        _, output = await self._git(
            workspace, "log", "--oneline", f"-{max_entries}",
        )
        return output

    async def cleanup(self, task_id: str) -> None:
        """Remove the workspace directory entirely."""
        workspace = self._workspaces.pop(task_id, None)
        if workspace is None:
            return
        try:
            shutil.rmtree(workspace)
            log.info("workspace: cleaned up %s", task_id)
        except OSError:
            log.exception("workspace: failed to clean up %s", task_id)

    # ------------------------------------------------------------------
    # Internal helpers
    # ------------------------------------------------------------------

    async def _git(self, workspace: Path, *args: str) -> tuple[bool, str]:
        """Run a git command in *workspace* and return ``(success, output)``.

        Uses ``asyncio.create_subprocess_exec`` for non-blocking I/O.
        """
        try:
            proc = await asyncio.create_subprocess_exec(
                "git",
                *args,
                cwd=str(workspace),
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
            )
            stdout, stderr = await proc.communicate()
            output = (stdout or b"").decode() + (stderr or b"").decode()
            return proc.returncode == 0, output.strip()
        except FileNotFoundError:
            log.error("workspace: 'git' executable not found")
            return False, "git not found"
        except Exception as exc:
            log.exception("workspace: git command failed: git %s", " ".join(args))
            return False, str(exc)
