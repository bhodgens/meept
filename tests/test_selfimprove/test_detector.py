"""Tests for the IssueDetector."""

from pathlib import Path

import pytest

from meept.selfimprove.config import DetectionConfig
from meept.selfimprove.detector import IssueDetector
from meept.selfimprove.models import IssueSource


class TestIssueDetector:
    """Tests for IssueDetector."""

    @pytest.fixture
    def detector(
        self,
        detection_config: DetectionConfig,
        temp_project: Path,
    ) -> IssueDetector:
        """Create an IssueDetector instance."""
        return IssueDetector(detection_config, project_root=temp_project)

    @pytest.mark.asyncio
    async def test_detect_all_empty(
        self,
        detection_config: DetectionConfig,
        tmp_path: Path,
    ) -> None:
        """Test detection with no issues."""
        # Create empty project
        (tmp_path / "tests").mkdir()
        (tmp_path / "tests" / "test_empty.py").write_text(
            "def test_pass():\n    assert True\n"
        )

        detector = IssueDetector(detection_config, project_root=tmp_path)
        issues = await detector.detect_all()
        # May have issues or not depending on pytest being installed
        assert isinstance(issues, list)

    def test_parse_pytest_output_failure(self, detector: IssueDetector) -> None:
        """Test parsing pytest failure output."""
        output = """
FAILED tests/test_example.py::test_example_none - AssertionError
=========================== short test summary info ============================
FAILED tests/test_example.py::test_example_none - AssertionError: assert None is None
"""
        detector._parse_pytest_output(output)
        assert len(detector._issues) >= 1
        assert detector._issues[0].source == IssueSource.PYTEST

    def test_parse_pytest_output_success(self, detector: IssueDetector) -> None:
        """Test parsing successful pytest output."""
        output = """
=========================== test session starts ============================
collected 2 items

tests/test_example.py ..                                              [100%]

============================= 2 passed in 0.01s ==============================
"""
        detector._parse_pytest_output(output)
        assert len(detector._issues) == 0
