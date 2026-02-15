"""Tests for self-improvement data models."""

import pytest

from meept.selfimprove.models import (
    AppliedFix,
    FilePatch,
    Issue,
    IssueSeverity,
    IssueSource,
    ProposedFix,
    RiskLevel,
    RootCauseAnalysis,
    ValidationResult,
)


class TestIssue:
    """Tests for the Issue dataclass."""

    def test_create_issue(self, mock_issue: Issue) -> None:
        """Test creating an issue."""
        assert mock_issue.id == "test-issue-001"
        assert mock_issue.source == IssueSource.PYTEST
        assert mock_issue.severity == IssueSeverity.MEDIUM

    def test_to_dict(self, mock_issue: Issue) -> None:
        """Test serializing issue to dict."""
        data = mock_issue.to_dict()
        assert data["id"] == "test-issue-001"
        assert data["source"] == "pytest"
        assert data["severity"] == "medium"
        assert "timestamp" in data


class TestRootCauseAnalysis:
    """Tests for the RootCauseAnalysis dataclass."""

    def test_create_analysis(self, mock_analysis: RootCauseAnalysis) -> None:
        """Test creating an analysis."""
        assert mock_analysis.issue_id == "test-issue-001"
        assert mock_analysis.confidence == 0.85
        assert len(mock_analysis.affected_files) == 1

    def test_to_dict(self, mock_analysis: RootCauseAnalysis) -> None:
        """Test serializing analysis to dict."""
        data = mock_analysis.to_dict()
        assert data["issue_id"] == "test-issue-001"
        assert data["confidence"] == 0.85


class TestFilePatch:
    """Tests for the FilePatch dataclass."""

    def test_create_patch(self, mock_patch: FilePatch) -> None:
        """Test creating a patch."""
        assert mock_patch.file_path == "src/example.py"
        assert mock_patch.start_line == 42
        assert mock_patch.end_line == 42

    def test_to_dict(self, mock_patch: FilePatch) -> None:
        """Test serializing patch to dict."""
        data = mock_patch.to_dict()
        assert data["file_path"] == "src/example.py"
        assert "original_content" in data
        assert "new_content" in data


class TestProposedFix:
    """Tests for the ProposedFix dataclass."""

    def test_create_fix(self, mock_fix: ProposedFix) -> None:
        """Test creating a fix."""
        assert mock_fix.id == "fix-001"
        assert mock_fix.risk_level == RiskLevel.LOW
        assert len(mock_fix.patches) == 1

    def test_to_dict(self, mock_fix: ProposedFix) -> None:
        """Test serializing fix to dict."""
        data = mock_fix.to_dict()
        assert data["id"] == "fix-001"
        assert data["risk_level"] == "low"
        assert len(data["patches"]) == 1


class TestValidationResult:
    """Tests for the ValidationResult dataclass."""

    def test_create_validation(self, mock_validation: ValidationResult) -> None:
        """Test creating a validation result."""
        assert mock_validation.fix_id == "fix-001"
        assert mock_validation.success is True
        assert mock_validation.tests_passed == 10

    def test_to_dict(self, mock_validation: ValidationResult) -> None:
        """Test serializing validation to dict."""
        data = mock_validation.to_dict()
        assert data["fix_id"] == "fix-001"
        assert data["success"] is True
