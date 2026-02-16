"""Fix validator that tests patches in a sandbox.

Applies proposed fixes to git worktrees and runs tests to verify
they actually work before applying to the main repository.
"""

from __future__ import annotations

import logging
import time
from pathlib import Path
from typing import TYPE_CHECKING

from meept.selfimprove.models import ProposedFix, ValidationResult
from meept.selfimprove.sandbox import SandboxError, SandboxManager

if TYPE_CHECKING:
    from meept.selfimprove.config import SandboxConfig, SafetyConfig

log = logging.getLogger(__name__)


class FixValidator:
    """Validates proposed fixes by testing them in a sandbox.

    Creates git worktrees, applies patches, runs tests, and reports
    whether the fix passes validation.
    """

    def __init__(
        self,
        sandbox_config: SandboxConfig,
        safety_config: SafetyConfig,
        project_root: Path | None = None,
    ) -> None:
        self._sandbox_config = sandbox_config
        self._safety_config = safety_config
        self._project_root = project_root or Path.cwd()
        self._sandbox = SandboxManager(sandbox_config, project_root)

    async def validate(self, fix: ProposedFix) -> ValidationResult:
        """Validate a single fix in a sandbox.

        Parameters
        ----------
        fix:
            The proposed fix to validate.

        Returns
        -------
        ValidationResult
            The validation result.
        """
        log.info("validator: validating fix %s", fix.id)
        start_time = time.monotonic()

        # Check if fix has patches
        if not fix.patches:
            return ValidationResult(
                fix_id=fix.id,
                success=False,
                tests_run=0,
                tests_passed=0,
                tests_failed=0,
                test_output="",
                error_message="Fix has no patches to apply",
                duration_seconds=0.0,
            )

        worktree_path: Path | None = None
        try:
            # Create sandbox
            worktree_path = await self._sandbox.create_worktree(fix.id)

            # Apply patches
            modified_files = await self._sandbox.apply_fix(worktree_path, fix)
            log.info(
                "validator: applied %d patches to %d files",
                len(fix.patches),
                len(modified_files),
            )

            if not modified_files:
                return ValidationResult(
                    fix_id=fix.id,
                    success=False,
                    tests_run=0,
                    tests_passed=0,
                    tests_failed=0,
                    test_output="",
                    error_message="No files were modified",
                    worktree_path=str(worktree_path),
                    duration_seconds=time.monotonic() - start_time,
                )

            # Run tests
            specific_tests = fix.tests_to_run if fix.tests_to_run else None
            success, passed, failed, output = await self._sandbox.run_tests(
                worktree_path,
                specific_tests=specific_tests,
            )

            duration = time.monotonic() - start_time

            # Cleanup on success if configured
            if success and self._sandbox_config.cleanup_on_success:
                await self._sandbox.cleanup_worktree(worktree_path, force=True)
                worktree_path = None

            result = ValidationResult(
                fix_id=fix.id,
                success=success,
                tests_run=passed + failed,
                tests_passed=passed,
                tests_failed=failed,
                test_output=output[:10000],  # Truncate large output
                error_message="" if success else f"{failed} test(s) failed",
                worktree_path=str(worktree_path) if worktree_path else "",
                duration_seconds=duration,
            )

            log.info(
                "validator: fix %s %s (passed=%d, failed=%d, %.1fs)",
                fix.id,
                "PASSED" if success else "FAILED",
                passed,
                failed,
                duration,
            )

            return result

        except SandboxError as exc:
            duration = time.monotonic() - start_time
            log.error("validator: sandbox error for fix %s: %s", fix.id, exc)

            # Cleanup on failure if configured
            if worktree_path and self._sandbox_config.cleanup_on_failure:
                await self._sandbox.cleanup_worktree(worktree_path, force=True)
                worktree_path = None

            return ValidationResult(
                fix_id=fix.id,
                success=False,
                tests_run=0,
                tests_passed=0,
                tests_failed=0,
                test_output="",
                error_message=str(exc),
                worktree_path=str(worktree_path) if worktree_path else "",
                duration_seconds=duration,
            )

        except Exception as exc:
            duration = time.monotonic() - start_time
            log.exception("validator: unexpected error for fix %s", fix.id)

            # Cleanup on failure if configured
            if worktree_path and self._sandbox_config.cleanup_on_failure:
                await self._sandbox.cleanup_worktree(worktree_path, force=True)
                worktree_path = None

            return ValidationResult(
                fix_id=fix.id,
                success=False,
                tests_run=0,
                tests_passed=0,
                tests_failed=0,
                test_output="",
                error_message=f"Unexpected error: {exc}",
                worktree_path=str(worktree_path) if worktree_path else "",
                duration_seconds=duration,
            )

    async def validate_batch(
        self,
        fixes: list[ProposedFix],
    ) -> list[ValidationResult]:
        """Validate multiple fixes.

        Parameters
        ----------
        fixes:
            List of proposed fixes to validate.

        Returns
        -------
        list[ValidationResult]
            Validation results for each fix.
        """
        results = []
        for fix in fixes:
            result = await self.validate(fix)
            results.append(result)

            # Stop if we've validated enough successful fixes
            successful = sum(1 for r in results if r.success)
            if successful >= 3:
                log.info("validator: stopping after %d successful validations", successful)
                break

        return results

    async def cleanup(self) -> int:
        """Clean up all sandbox worktrees.

        Returns
        -------
        int
            Number of worktrees cleaned up.
        """
        return await self._sandbox.cleanup_all(force=True)

    @property
    def active_worktrees(self) -> dict[str, Path]:
        """Return currently active worktrees."""
        return self._sandbox.active_worktrees
