"""Data models for the self-improvement system."""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import UTC, datetime
from enum import Enum
from typing import Any


class IssueSeverity(str, Enum):
    """Severity levels for detected issues."""

    LOW = "low"
    MEDIUM = "medium"
    HIGH = "high"
    CRITICAL = "critical"


class IssueSource(str, Enum):
    """Where the issue was detected."""

    PYTEST = "pytest"
    RUNTIME_LOG = "runtime_log"
    TYPE_CHECK = "type_check"
    LINT = "lint"
    MANUAL = "manual"


class RiskLevel(str, Enum):
    """Risk level for proposed fixes."""

    LOW = "low"
    MEDIUM = "medium"
    HIGH = "high"
    CRITICAL = "critical"


@dataclass(slots=True)
class Issue:
    """A detected problem that needs investigation.

    Attributes
    ----------
    id:
        Unique identifier for the issue.
    source:
        Where the issue was detected (pytest, logs, etc.).
    severity:
        How severe the issue is.
    title:
        Short description of the issue.
    description:
        Full details including error messages, stack traces, etc.
    file_path:
        Path to the affected file, if known.
    line_number:
        Line number in the file, if known.
    test_name:
        Name of the failing test, if applicable.
    error_type:
        Type of error (e.g., "AssertionError", "TypeError").
    stack_trace:
        Full stack trace, if available.
    timestamp:
        When the issue was detected.
    metadata:
        Additional context-specific information.
    """

    id: str
    source: IssueSource
    severity: IssueSeverity
    title: str
    description: str
    file_path: str | None = None
    line_number: int | None = None
    test_name: str | None = None
    error_type: str | None = None
    stack_trace: str | None = None
    timestamp: str = field(default_factory=lambda: datetime.now(UTC).isoformat())
    metadata: dict[str, Any] = field(default_factory=dict)

    def to_dict(self) -> dict[str, Any]:
        """Serialize to a JSON-compatible dict."""
        return {
            "id": self.id,
            "source": self.source.value,
            "severity": self.severity.value,
            "title": self.title,
            "description": self.description,
            "file_path": self.file_path,
            "line_number": self.line_number,
            "test_name": self.test_name,
            "error_type": self.error_type,
            "stack_trace": self.stack_trace,
            "timestamp": self.timestamp,
            "metadata": self.metadata,
        }


@dataclass(slots=True)
class RootCauseAnalysis:
    """LLM-generated analysis of an issue's root cause.

    Attributes
    ----------
    issue_id:
        ID of the issue being analyzed.
    root_cause:
        Description of the underlying cause.
    affected_files:
        List of files that need modification.
    affected_functions:
        List of functions/methods that need changes.
    confidence:
        Confidence score (0.0-1.0) in the analysis.
    reasoning:
        Step-by-step reasoning that led to this conclusion.
    suggested_approach:
        High-level description of how to fix the issue.
    related_issues:
        IDs of other issues that may share the same root cause.
    timestamp:
        When the analysis was performed.
    model_used:
        Which LLM model performed the analysis.
    """

    issue_id: str
    root_cause: str
    affected_files: list[str]
    affected_functions: list[str]
    confidence: float
    reasoning: str
    suggested_approach: str
    related_issues: list[str] = field(default_factory=list)
    timestamp: str = field(default_factory=lambda: datetime.now(UTC).isoformat())
    model_used: str = ""

    def to_dict(self) -> dict[str, Any]:
        """Serialize to a JSON-compatible dict."""
        return {
            "issue_id": self.issue_id,
            "root_cause": self.root_cause,
            "affected_files": self.affected_files,
            "affected_functions": self.affected_functions,
            "confidence": self.confidence,
            "reasoning": self.reasoning,
            "suggested_approach": self.suggested_approach,
            "related_issues": self.related_issues,
            "timestamp": self.timestamp,
            "model_used": self.model_used,
        }


@dataclass(slots=True)
class FilePatch:
    """A patch to apply to a single file.

    Attributes
    ----------
    file_path:
        Path to the file to modify.
    original_content:
        The original content being replaced (for verification).
    new_content:
        The new content to write.
    start_line:
        Starting line number of the patch (1-indexed).
    end_line:
        Ending line number of the patch (1-indexed, inclusive).
    description:
        What this specific change does.
    """

    file_path: str
    original_content: str
    new_content: str
    start_line: int
    end_line: int
    description: str

    def to_dict(self) -> dict[str, Any]:
        """Serialize to a JSON-compatible dict."""
        return {
            "file_path": self.file_path,
            "original_content": self.original_content,
            "new_content": self.new_content,
            "start_line": self.start_line,
            "end_line": self.end_line,
            "description": self.description,
        }


@dataclass(slots=True)
class ProposedFix:
    """A proposed fix for one or more issues.

    Attributes
    ----------
    id:
        Unique identifier for this fix proposal.
    issue_ids:
        IDs of issues this fix addresses.
    title:
        Short description of the fix.
    description:
        Detailed explanation of what the fix does.
    patches:
        List of file patches to apply.
    risk_level:
        Assessed risk of applying this fix.
    confidence:
        Confidence score (0.0-1.0) that this fix will work.
    reasoning:
        Why this fix was chosen.
    tests_to_run:
        Specific tests that should pass after applying.
    rollback_instructions:
        How to undo this fix if needed.
    timestamp:
        When this fix was generated.
    model_used:
        Which LLM model generated this fix.
    """

    id: str
    issue_ids: list[str]
    title: str
    description: str
    patches: list[FilePatch]
    risk_level: RiskLevel
    confidence: float
    reasoning: str
    tests_to_run: list[str] = field(default_factory=list)
    rollback_instructions: str = ""
    timestamp: str = field(default_factory=lambda: datetime.now(UTC).isoformat())
    model_used: str = ""

    def to_dict(self) -> dict[str, Any]:
        """Serialize to a JSON-compatible dict."""
        return {
            "id": self.id,
            "issue_ids": self.issue_ids,
            "title": self.title,
            "description": self.description,
            "patches": [p.to_dict() for p in self.patches],
            "risk_level": self.risk_level.value,
            "confidence": self.confidence,
            "reasoning": self.reasoning,
            "tests_to_run": self.tests_to_run,
            "rollback_instructions": self.rollback_instructions,
            "timestamp": self.timestamp,
            "model_used": self.model_used,
        }


@dataclass(slots=True)
class ValidationResult:
    """Result of validating a fix in a sandbox.

    Attributes
    ----------
    fix_id:
        ID of the fix being validated.
    success:
        Whether all tests passed.
    tests_run:
        Number of tests executed.
    tests_passed:
        Number of tests that passed.
    tests_failed:
        Number of tests that failed.
    test_output:
        Raw output from pytest.
    error_message:
        Error details if validation failed.
    worktree_path:
        Path to the git worktree used for validation.
    duration_seconds:
        How long validation took.
    timestamp:
        When validation was performed.
    """

    fix_id: str
    success: bool
    tests_run: int
    tests_passed: int
    tests_failed: int
    test_output: str
    error_message: str = ""
    worktree_path: str = ""
    duration_seconds: float = 0.0
    timestamp: str = field(default_factory=lambda: datetime.now(UTC).isoformat())

    def to_dict(self) -> dict[str, Any]:
        """Serialize to a JSON-compatible dict."""
        return {
            "fix_id": self.fix_id,
            "success": self.success,
            "tests_run": self.tests_run,
            "tests_passed": self.tests_passed,
            "tests_failed": self.tests_failed,
            "test_output": self.test_output,
            "error_message": self.error_message,
            "worktree_path": self.worktree_path,
            "duration_seconds": self.duration_seconds,
            "timestamp": self.timestamp,
        }


@dataclass(slots=True)
class AppliedFix:
    """Record of a fix that has been applied to the main repository.

    Attributes
    ----------
    fix_id:
        ID of the applied fix.
    commit_hash:
        Git commit hash of the fix.
    commit_message:
        The commit message used.
    branch:
        Branch the fix was applied to.
    files_modified:
        List of files that were changed.
    approved_by:
        Who approved the fix (e.g., "human", "auto").
    applied_at:
        When the fix was applied.
    validation_result:
        The validation result that led to approval.
    rollback_hash:
        Commit hash to revert to if needed.
    """

    fix_id: str
    commit_hash: str
    commit_message: str
    branch: str
    files_modified: list[str]
    approved_by: str
    applied_at: str = field(default_factory=lambda: datetime.now(UTC).isoformat())
    validation_result: ValidationResult | None = None
    rollback_hash: str = ""

    def to_dict(self) -> dict[str, Any]:
        """Serialize to a JSON-compatible dict."""
        return {
            "fix_id": self.fix_id,
            "commit_hash": self.commit_hash,
            "commit_message": self.commit_message,
            "branch": self.branch,
            "files_modified": self.files_modified,
            "approved_by": self.approved_by,
            "applied_at": self.applied_at,
            "validation_result": (
                self.validation_result.to_dict() if self.validation_result else None
            ),
            "rollback_hash": self.rollback_hash,
        }


@dataclass(slots=True)
class ImprovementCycle:
    """Tracks a full improvement cycle from detection to application.

    Attributes
    ----------
    id:
        Unique identifier for this cycle.
    started_at:
        When the cycle started.
    completed_at:
        When the cycle completed (or None if still running).
    issues_detected:
        Number of issues found.
    issues_analyzed:
        Number of issues with root cause analysis.
    fixes_generated:
        Number of fix proposals created.
    fixes_validated:
        Number of fixes that passed validation.
    fixes_applied:
        Number of fixes applied to the codebase.
    status:
        Current status (running, completed, failed).
    error:
        Error message if the cycle failed.
    """

    id: str
    started_at: str = field(default_factory=lambda: datetime.now(UTC).isoformat())
    completed_at: str | None = None
    issues_detected: int = 0
    issues_analyzed: int = 0
    fixes_generated: int = 0
    fixes_validated: int = 0
    fixes_applied: int = 0
    status: str = "running"
    error: str = ""

    def to_dict(self) -> dict[str, Any]:
        """Serialize to a JSON-compatible dict."""
        return {
            "id": self.id,
            "started_at": self.started_at,
            "completed_at": self.completed_at,
            "issues_detected": self.issues_detected,
            "issues_analyzed": self.issues_analyzed,
            "fixes_generated": self.fixes_generated,
            "fixes_validated": self.fixes_validated,
            "fixes_applied": self.fixes_applied,
            "status": self.status,
            "error": self.error,
        }
