"""Change applier that commits validated fixes to the repository.

Applies validated fixes to the main repository with git commits,
supporting human approval gates and rollback capabilities.
"""

from __future__ import annotations

import asyncio
import logging
from datetime import UTC, datetime
from pathlib import Path
from typing import TYPE_CHECKING, Any

from meept.selfimprove.models import AppliedFix, ProposedFix, RiskLevel, ValidationResult

if TYPE_CHECKING:
    from meept.core.bus import MessageBus
    from meept.selfimprove.config import SafetyConfig

log = logging.getLogger(__name__)


class ApprovalRequired(Exception):
    """Raised when human approval is required before applying."""

    def __init__(self, fix_id: str, reason: str) -> None:
        self.fix_id = fix_id
        self.reason = reason
        super().__init__(f"Approval required for {fix_id}: {reason}")


class ChangeApplier:
    """Applies validated fixes to the main repository.

    Handles the final step of applying fixes with git commits,
    human approval gates, and rollback support.
    """

    def __init__(
        self,
        safety_config: SafetyConfig,
        project_root: Path | None = None,
        bus: MessageBus | None = None,
    ) -> None:
        self._safety_config = safety_config
        self._project_root = project_root or Path.cwd()
        self._bus = bus
        self._pending_approvals: dict[str, tuple[ProposedFix, ValidationResult]] = {}

    async def apply(
        self,
        fix: ProposedFix,
        validation: ValidationResult,
        approved_by: str = "auto",
    ) -> AppliedFix:
        """Apply a validated fix to the repository.

        Parameters
        ----------
        fix:
            The proposed fix to apply.
        validation:
            The validation result proving the fix works.
        approved_by:
            Who approved this fix ("human", "auto", etc.).

        Returns
        -------
        AppliedFix
            Record of the applied fix.

        Raises
        ------
        ApprovalRequired
            If human approval is required.
        ValueError
            If the fix failed validation.
        """
        log.info("applier: applying fix %s", fix.id)

        # Check validation passed
        if not validation.success:
            raise ValueError(f"Cannot apply fix {fix.id}: validation failed")

        # Check if tests are required
        if self._safety_config.require_tests_pass and validation.tests_failed > 0:
            raise ValueError(
                f"Cannot apply fix {fix.id}: {validation.tests_failed} test(s) failed"
            )

        # Check approval requirements
        if self._safety_config.require_human_approval and approved_by != "human":
            self._pending_approvals[fix.id] = (fix, validation)
            await self._request_approval(fix, validation)
            raise ApprovalRequired(
                fix.id,
                f"Human approval required for {fix.risk_level.value} risk fix",
            )

        # Get current commit for rollback
        ok, rollback_hash = await self._git("rev-parse", "HEAD")
        if not ok:
            raise RuntimeError("Failed to get current HEAD")
        rollback_hash = rollback_hash.strip()

        # Get current branch
        ok, branch = await self._git("rev-parse", "--abbrev-ref", "HEAD")
        if not ok:
            raise RuntimeError("Failed to get current branch")
        branch = branch.strip()

        # Apply the patches
        modified_files = []
        for patch in fix.patches:
            target_file = self._project_root / patch.file_path

            if not target_file.exists():
                log.warning("applier: file does not exist: %s", patch.file_path)
                continue

            try:
                content = target_file.read_text(encoding="utf-8")
                lines = content.split("\n")
                original_stripped = patch.original_content.strip()
                new_stripped = patch.new_content.strip()

                applied = False

                # Try exact line match first
                if patch.start_line > 0 and patch.end_line <= len(lines):
                    original_lines = lines[patch.start_line - 1 : patch.end_line]
                    original_content = "\n".join(original_lines)

                    if original_content.strip() == original_stripped:
                        # Exact match - apply by replacing lines
                        new_lines = patch.new_content.split("\n")
                        lines[patch.start_line - 1 : patch.end_line] = new_lines
                        content = "\n".join(lines)
                        applied = True
                        log.debug("applier: exact match, replacing lines %d-%d in %s",
                                  patch.start_line, patch.end_line, patch.file_path)

                # Fall back to fuzzy content matching
                if not applied and original_stripped in content:
                    occurrences = content.count(original_stripped)
                    if occurrences > 1:
                        log.warning("applier: original content appears %d times in %s",
                                    occurrences, patch.file_path)
                    content = content.replace(original_stripped, new_stripped, 1)
                    applied = True
                    log.debug("applier: fuzzy match applied to %s", patch.file_path)

                if not applied:
                    log.warning("applier: original content not found in %s", patch.file_path)
                    continue

                # Verify the patch was applied
                if new_stripped not in content:
                    raise RuntimeError(f"Patch verification failed for {patch.file_path}")

                target_file.write_text(content, encoding="utf-8")
                modified_files.append(patch.file_path)

            except Exception as exc:
                log.error("applier: failed to modify %s: %s", patch.file_path, exc)
                # Rollback any changes made so far
                if modified_files:
                    await self._git("checkout", "--", *modified_files)
                raise RuntimeError(f"Failed to apply patch to {patch.file_path}") from exc

        if not modified_files:
            raise RuntimeError("No files were modified")

        # Stage changes
        for file_path in modified_files:
            ok, output = await self._git("add", file_path)
            if not ok:
                log.error("applier: failed to stage %s: %s", file_path, output)

        # Create commit
        commit_message = self._format_commit_message(fix, validation)
        ok, output = await self._git("commit", "-m", commit_message)
        if not ok:
            # Unstage and revert
            await self._git("reset", "HEAD")
            await self._git("checkout", "--", *modified_files)
            raise RuntimeError(f"Failed to commit: {output}")

        # Get the new commit hash
        ok, commit_hash = await self._git("rev-parse", "HEAD")
        if not ok:
            commit_hash = "unknown"
        commit_hash = commit_hash.strip()

        applied = AppliedFix(
            fix_id=fix.id,
            commit_hash=commit_hash,
            commit_message=commit_message,
            branch=branch,
            files_modified=modified_files,
            approved_by=approved_by,
            validation_result=validation,
            rollback_hash=rollback_hash,
        )

        log.info(
            "applier: committed fix %s as %s",
            fix.id,
            commit_hash[:8],
        )

        return applied

    async def approve(self, fix_id: str, approved_by: str = "human") -> AppliedFix:
        """Approve and apply a pending fix.

        Parameters
        ----------
        fix_id:
            ID of the fix to approve.
        approved_by:
            Who is approving ("human", etc.).

        Returns
        -------
        AppliedFix
            The applied fix.
        """
        if fix_id not in self._pending_approvals:
            raise ValueError(f"No pending approval for fix {fix_id}")

        fix, validation = self._pending_approvals.pop(fix_id)

        # Temporarily disable approval requirement
        old_require = self._safety_config.require_human_approval
        # Note: We need to create a modified config, can't mutate
        # So we pass approved_by="human" to bypass the check
        result = await self.apply(fix, validation, approved_by=approved_by)

        return result

    async def reject(self, fix_id: str, reason: str = "") -> None:
        """Reject a pending fix.

        Parameters
        ----------
        fix_id:
            ID of the fix to reject.
        reason:
            Optional reason for rejection.
        """
        if fix_id in self._pending_approvals:
            fix, _ = self._pending_approvals.pop(fix_id)
            log.info("applier: rejected fix %s: %s", fix_id, reason or "no reason given")

    async def rollback(self, applied: AppliedFix) -> bool:
        """Rollback an applied fix.

        Parameters
        ----------
        applied:
            The applied fix to rollback.

        Returns
        -------
        bool
            True if rollback succeeded.
        """
        log.info("applier: rolling back fix %s", applied.fix_id)

        if not applied.rollback_hash:
            log.error("applier: no rollback hash for fix %s", applied.fix_id)
            return False

        # Revert the specific commit
        ok, output = await self._git("revert", "--no-edit", applied.commit_hash)
        if not ok:
            log.error("applier: failed to revert %s: %s", applied.commit_hash, output)
            return False

        log.info("applier: rolled back fix %s", applied.fix_id)
        return True

    async def _request_approval(
        self,
        fix: ProposedFix,
        validation: ValidationResult,
    ) -> None:
        """Request human approval via the message bus."""
        if self._bus is None:
            return

        from meept.models.messages import BusMessage, MessageType

        msg = BusMessage(
            type=MessageType.STATUS_UPDATE,
            payload={
                "action": "selfimprove.approval_required",
                "fix_id": fix.id,
                "title": fix.title,
                "description": fix.description,
                "risk_level": fix.risk_level.value,
                "confidence": fix.confidence,
                "patches_count": len(fix.patches),
                "files": [p.file_path for p in fix.patches],
                "tests_passed": validation.tests_passed,
                "tests_failed": validation.tests_failed,
            },
            source="selfimprove.applier",
        )
        await self._bus.publish("selfimprove.approval_required", msg)

    def _format_commit_message(
        self,
        fix: ProposedFix,
        validation: ValidationResult,
    ) -> str:
        """Format a commit message for the fix."""
        lines = [
            f"fix: {fix.title}",
            "",
            fix.description,
            "",
            f"Issues: {', '.join(fix.issue_ids)}",
            f"Risk: {fix.risk_level.value}",
            f"Confidence: {fix.confidence:.0%}",
            f"Tests: {validation.tests_passed} passed, {validation.tests_failed} failed",
            "",
            "Generated by meept self-improvement system",
        ]
        return "\n".join(lines)

    async def _git(self, *args: str) -> tuple[bool, str]:
        """Run a git command in the project root."""
        try:
            proc = await asyncio.create_subprocess_exec(
                "git",
                *args,
                cwd=str(self._project_root),
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
            )
            stdout, stderr = await proc.communicate()
            output = (stdout or b"").decode() + (stderr or b"").decode()
            return proc.returncode == 0, output.strip()
        except FileNotFoundError:
            log.error("applier: 'git' executable not found")
            return False, "git not found"
        except Exception as exc:
            log.exception("applier: git command failed: git %s", " ".join(args))
            return False, str(exc)

    @property
    def pending_approvals(self) -> dict[str, tuple[ProposedFix, ValidationResult]]:
        """Return pending approvals."""
        return dict(self._pending_approvals)
