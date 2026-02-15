"""Git worktree sandbox manager for validating fixes.

Creates isolated git worktrees where fixes can be applied and tested
without affecting the main repository.
"""

from __future__ import annotations

import asyncio
import logging
import shutil
import uuid
from pathlib import Path
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from meept.selfimprove.config import SandboxConfig
    from meept.selfimprove.models import FilePatch, ProposedFix

log = logging.getLogger(__name__)


class SandboxError(Exception):
    """Error during sandbox operations."""


class SandboxManager:
    """Manages git worktrees for testing fixes in isolation.

    Creates temporary worktrees from the current branch, applies patches,
    runs tests, and cleans up when done.
    """

    def __init__(
        self,
        config: SandboxConfig,
        repo_root: Path | None = None,
    ) -> None:
        self._config = config
        self._repo_root = repo_root or Path.cwd()
        self._worktree_dir = Path(config.worktree_dir).expanduser()
        self._active_worktrees: dict[str, Path] = {}

    async def create_worktree(self, fix_id: str | None = None) -> Path:
        """Create a new git worktree for testing.

        Parameters
        ----------
        fix_id:
            Optional identifier for this worktree. If not provided, a
            random ID is generated.

        Returns
        -------
        Path
            Path to the created worktree.
        """
        if len(self._active_worktrees) >= self._config.max_worktrees:
            raise SandboxError(
                f"Maximum worktrees ({self._config.max_worktrees}) reached. "
                "Clean up existing worktrees first."
            )

        worktree_id = fix_id or f"sandbox-{uuid.uuid4().hex[:8]}"
        worktree_path = self._worktree_dir / worktree_id

        # Ensure parent directory exists
        self._worktree_dir.mkdir(parents=True, exist_ok=True)

        # Get current branch
        ok, branch = await self._git("rev-parse", "--abbrev-ref", "HEAD")
        if not ok:
            raise SandboxError(f"Failed to get current branch: {branch}")

        # Create worktree from current HEAD
        ok, output = await self._git(
            "worktree", "add", "--detach", str(worktree_path), "HEAD"
        )
        if not ok:
            raise SandboxError(f"Failed to create worktree: {output}")

        self._active_worktrees[worktree_id] = worktree_path
        log.info("sandbox: created worktree %s at %s", worktree_id, worktree_path)
        return worktree_path

    async def apply_patches(
        self,
        worktree_path: Path,
        patches: list[FilePatch],
    ) -> list[str]:
        """Apply file patches to a worktree.

        Parameters
        ----------
        worktree_path:
            Path to the worktree.
        patches:
            List of file patches to apply.

        Returns
        -------
        list[str]
            List of modified file paths.
        """
        modified_files: list[str] = []

        for patch in patches:
            target_file = worktree_path / patch.file_path

            if not target_file.exists():
                log.warning(
                    "sandbox: target file does not exist: %s", patch.file_path
                )
                continue

            try:
                content = target_file.read_text(encoding="utf-8")
                lines = content.split("\n")

                # Verify original content matches
                original_lines = lines[patch.start_line - 1 : patch.end_line]
                original_content = "\n".join(original_lines)

                if original_content.strip() != patch.original_content.strip():
                    log.warning(
                        "sandbox: original content mismatch in %s (lines %d-%d)",
                        patch.file_path,
                        patch.start_line,
                        patch.end_line,
                    )
                    # Try to apply anyway with fuzzy matching
                    if patch.original_content.strip() not in content:
                        raise SandboxError(
                            f"Cannot apply patch: original content not found in {patch.file_path}"
                        )
                    content = content.replace(
                        patch.original_content.strip(), patch.new_content.strip()
                    )
                else:
                    # Apply the patch by replacing lines
                    new_lines = patch.new_content.split("\n")
                    lines[patch.start_line - 1 : patch.end_line] = new_lines
                    content = "\n".join(lines)

                target_file.write_text(content, encoding="utf-8")
                modified_files.append(patch.file_path)
                log.debug(
                    "sandbox: applied patch to %s (lines %d-%d)",
                    patch.file_path,
                    patch.start_line,
                    patch.end_line,
                )

            except SandboxError:
                raise
            except Exception as exc:
                raise SandboxError(
                    f"Failed to apply patch to {patch.file_path}: {exc}"
                ) from exc

        return modified_files

    async def apply_fix(
        self,
        worktree_path: Path,
        fix: ProposedFix,
    ) -> list[str]:
        """Apply a complete fix to a worktree.

        Parameters
        ----------
        worktree_path:
            Path to the worktree.
        fix:
            The proposed fix to apply.

        Returns
        -------
        list[str]
            List of modified file paths.
        """
        return await self.apply_patches(worktree_path, fix.patches)

    async def run_tests(
        self,
        worktree_path: Path,
        specific_tests: list[str] | None = None,
    ) -> tuple[bool, int, int, str]:
        """Run pytest in the worktree.

        Parameters
        ----------
        worktree_path:
            Path to the worktree.
        specific_tests:
            Optional list of specific test files/patterns to run.

        Returns
        -------
        tuple[bool, int, int, str]
            (success, tests_passed, tests_failed, output)
        """
        args = ["python", "-m", "pytest", "-v", "--tb=short"]
        if specific_tests:
            args.extend(specific_tests)
        else:
            args.append("tests/")

        try:
            proc = await asyncio.create_subprocess_exec(
                *args,
                cwd=str(worktree_path),
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.STDOUT,
            )

            try:
                stdout, _ = await asyncio.wait_for(
                    proc.communicate(),
                    timeout=self._config.test_timeout_seconds,
                )
            except asyncio.TimeoutError:
                proc.kill()
                await proc.wait()
                return False, 0, 0, "Test execution timed out"

            output = stdout.decode("utf-8", errors="replace")

            # Parse test results from pytest output
            passed = 0
            failed = 0

            # Look for summary line: "X passed, Y failed" or similar
            import re

            summary = re.search(r"(\d+)\s+passed", output)
            if summary:
                passed = int(summary.group(1))

            summary = re.search(r"(\d+)\s+failed", output)
            if summary:
                failed = int(summary.group(1))

            success = proc.returncode == 0
            return success, passed, failed, output

        except FileNotFoundError:
            return False, 0, 0, "pytest not found"
        except Exception as exc:
            log.exception("sandbox: test execution failed")
            return False, 0, 0, str(exc)

    async def cleanup_worktree(
        self,
        worktree_path: Path,
        force: bool = False,
    ) -> bool:
        """Remove a worktree and clean up.

        Parameters
        ----------
        worktree_path:
            Path to the worktree to remove.
        force:
            If True, force removal even if there are changes.

        Returns
        -------
        bool
            True if cleanup succeeded.
        """
        worktree_id = worktree_path.name

        try:
            # Remove from git worktree list
            args = ["worktree", "remove"]
            if force:
                args.append("--force")
            args.append(str(worktree_path))

            ok, output = await self._git(*args)
            if not ok:
                # Try manual removal if git fails
                if worktree_path.exists():
                    shutil.rmtree(worktree_path)
                # Prune stale worktree entries
                await self._git("worktree", "prune")

            self._active_worktrees.pop(worktree_id, None)
            log.info("sandbox: removed worktree %s", worktree_id)
            return True

        except Exception:
            log.exception("sandbox: failed to cleanup worktree %s", worktree_id)
            return False

    async def cleanup_all(self, force: bool = False) -> int:
        """Remove all active worktrees.

        Parameters
        ----------
        force:
            If True, force removal even if there are changes.

        Returns
        -------
        int
            Number of worktrees cleaned up.
        """
        count = 0
        for worktree_path in list(self._active_worktrees.values()):
            if await self.cleanup_worktree(worktree_path, force=force):
                count += 1
        return count

    async def list_worktrees(self) -> list[dict]:
        """List all active worktrees.

        Returns
        -------
        list[dict]
            List of worktree info dicts.
        """
        ok, output = await self._git("worktree", "list", "--porcelain")
        if not ok:
            return []

        worktrees = []
        current: dict = {}

        for line in output.strip().split("\n"):
            if not line:
                if current:
                    worktrees.append(current)
                    current = {}
                continue

            if line.startswith("worktree "):
                current["path"] = line[9:]
            elif line.startswith("HEAD "):
                current["head"] = line[5:]
            elif line.startswith("branch "):
                current["branch"] = line[7:]
            elif line == "detached":
                current["detached"] = True

        if current:
            worktrees.append(current)

        return worktrees

    async def _git(self, *args: str) -> tuple[bool, str]:
        """Run a git command in the repo root.

        Returns
        -------
        tuple[bool, str]
            (success, output)
        """
        try:
            proc = await asyncio.create_subprocess_exec(
                "git",
                *args,
                cwd=str(self._repo_root),
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
            )
            stdout, stderr = await proc.communicate()
            output = (stdout or b"").decode() + (stderr or b"").decode()
            return proc.returncode == 0, output.strip()
        except FileNotFoundError:
            log.error("sandbox: 'git' executable not found")
            return False, "git not found"
        except Exception as exc:
            log.exception("sandbox: git command failed: git %s", " ".join(args))
            return False, str(exc)

    @property
    def active_worktrees(self) -> dict[str, Path]:
        """Return currently active worktrees."""
        return dict(self._active_worktrees)
