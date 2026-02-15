"""Fixtures for self-improvement tests."""

from __future__ import annotations

import tempfile
from pathlib import Path
from typing import TYPE_CHECKING
from unittest.mock import AsyncMock, MagicMock

import pytest

from meept.selfimprove.config import (
    AIInfraConfig,
    DetectionConfig,
    SafetyConfig,
    SandboxConfig,
    SelfImproveConfig,
)
from meept.selfimprove.models import (
    FilePatch,
    Issue,
    IssueSeverity,
    IssueSource,
    ProposedFix,
    RiskLevel,
    RootCauseAnalysis,
    ValidationResult,
)


@pytest.fixture
def mock_issue() -> Issue:
    """Create a mock issue for testing."""
    return Issue(
        id="test-issue-001",
        source=IssueSource.PYTEST,
        severity=IssueSeverity.MEDIUM,
        title="Test failure: test_example",
        description="AssertionError: expected True but got False",
        file_path="tests/test_example.py",
        line_number=42,
        test_name="test_example",
        error_type="AssertionError",
        stack_trace="Traceback...",
    )


@pytest.fixture
def mock_analysis() -> RootCauseAnalysis:
    """Create a mock root cause analysis."""
    return RootCauseAnalysis(
        issue_id="test-issue-001",
        root_cause="Missing null check before accessing attribute",
        affected_files=["src/example.py"],
        affected_functions=["example_function"],
        confidence=0.85,
        reasoning="The stack trace shows AttributeError at line 42...",
        suggested_approach="Add a null check before accessing the attribute",
    )


@pytest.fixture
def mock_patch() -> FilePatch:
    """Create a mock file patch."""
    return FilePatch(
        file_path="src/example.py",
        original_content="result = obj.value",
        new_content="result = obj.value if obj else None",
        start_line=42,
        end_line=42,
        description="Add null check",
    )


@pytest.fixture
def mock_fix(mock_patch: FilePatch) -> ProposedFix:
    """Create a mock proposed fix."""
    return ProposedFix(
        id="fix-001",
        issue_ids=["test-issue-001"],
        title="Add null check to prevent AttributeError",
        description="Adds a null check before accessing obj.value",
        patches=[mock_patch],
        risk_level=RiskLevel.LOW,
        confidence=0.85,
        reasoning="Simple null check addition",
        tests_to_run=["tests/test_example.py::test_example"],
    )


@pytest.fixture
def mock_validation() -> ValidationResult:
    """Create a mock validation result."""
    return ValidationResult(
        fix_id="fix-001",
        success=True,
        tests_run=10,
        tests_passed=10,
        tests_failed=0,
        test_output="10 passed",
        duration_seconds=5.2,
    )


@pytest.fixture
def detection_config() -> DetectionConfig:
    """Create a detection config for testing."""
    return DetectionConfig(
        scan_pytest=True,
        scan_runtime_logs=False,
        scan_type_check=False,
        scan_lint=False,
    )


@pytest.fixture
def ai_infra_config() -> AIInfraConfig:
    """Create an AI infra config for testing."""
    return AIInfraConfig(
        enabled=False,
        base_url="http://localhost:8100",
    )


@pytest.fixture
def sandbox_config() -> SandboxConfig:
    """Create a sandbox config for testing."""
    return SandboxConfig(
        worktree_dir="/tmp/meept-test-worktrees",
        cleanup_on_success=True,
        cleanup_on_failure=True,
        max_worktrees=2,
        test_timeout_seconds=30.0,
    )


@pytest.fixture
def safety_config() -> SafetyConfig:
    """Create a safety config for testing."""
    return SafetyConfig(
        require_human_approval=False,
        max_files_per_fix=5,
        max_lines_changed_per_fix=100,
        min_confidence_threshold=0.5,
    )


@pytest.fixture
def selfimprove_config(
    ai_infra_config: AIInfraConfig,
    sandbox_config: SandboxConfig,
    safety_config: SafetyConfig,
    detection_config: DetectionConfig,
) -> SelfImproveConfig:
    """Create a full self-improve config for testing."""
    return SelfImproveConfig(
        enabled=True,
        data_dir="/tmp/meept-test-selfimprove",
        ai_infra=ai_infra_config,
        sandbox=sandbox_config,
        safety=safety_config,
        detection=detection_config,
    )


@pytest.fixture
def temp_project(tmp_path: Path) -> Path:
    """Create a temporary project directory for testing."""
    # Create basic project structure
    (tmp_path / "src").mkdir()
    (tmp_path / "tests").mkdir()

    # Create a sample Python file
    (tmp_path / "src" / "example.py").write_text(
        '''def example_function(obj):
    """Example function with potential bug."""
    result = obj.value
    return result
''',
        encoding="utf-8",
    )

    # Create a sample test file
    (tmp_path / "tests" / "test_example.py").write_text(
        '''from src.example import example_function

def test_example():
    """Test that should pass."""
    class Obj:
        value = 42
    assert example_function(Obj()) == 42

def test_example_none():
    """Test that might fail with None."""
    assert example_function(None) is None
''',
        encoding="utf-8",
    )

    # Initialize as git repo
    import subprocess
    subprocess.run(["git", "init"], cwd=tmp_path, capture_output=True)
    subprocess.run(
        ["git", "config", "user.email", "test@test.com"],
        cwd=tmp_path,
        capture_output=True,
    )
    subprocess.run(
        ["git", "config", "user.name", "Test"],
        cwd=tmp_path,
        capture_output=True,
    )
    subprocess.run(["git", "add", "-A"], cwd=tmp_path, capture_output=True)
    subprocess.run(
        ["git", "commit", "-m", "Initial commit"],
        cwd=tmp_path,
        capture_output=True,
    )

    return tmp_path


@pytest.fixture
def mock_llm_client() -> MagicMock:
    """Create a mock LLM client."""
    client = MagicMock()
    client.chat = AsyncMock(
        return_value=MagicMock(
            content='{"root_cause": "test", "affected_files": [], "confidence": 0.9}'
        )
    )
    client.close = AsyncMock()
    return client
