"""Tests for the PatchGenerator."""

from pathlib import Path
from unittest.mock import AsyncMock, MagicMock

import pytest

from meept.selfimprove.config import AIInfraConfig, SafetyConfig
from meept.selfimprove.generator import PatchGenerator
from meept.selfimprove.models import (
    ProposedFix,
    RiskLevel,
    RootCauseAnalysis,
)


class TestPatchGenerator:
    """Tests for PatchGenerator."""

    @pytest.fixture
    def generator(
        self,
        ai_infra_config: AIInfraConfig,
        safety_config: SafetyConfig,
        mock_llm_client: MagicMock,
        temp_project: Path,
    ) -> PatchGenerator:
        """Create a PatchGenerator instance."""
        return PatchGenerator(
            ai_infra_config,
            safety_config,
            llm_client=mock_llm_client,
            project_root=temp_project,
        )

    def test_filter_blocked_files(
        self,
        generator: PatchGenerator,
    ) -> None:
        """Test filtering blocked files."""
        files = [
            "src/example.py",
            "src/meept/selfimprove/controller.py",
            ".env",
        ]
        blocked = generator._filter_blocked_files(files)
        assert "src/meept/selfimprove/controller.py" in blocked

    @pytest.mark.asyncio
    async def test_generate_low_confidence_skip(
        self,
        generator: PatchGenerator,
    ) -> None:
        """Test skipping low confidence analysis."""
        analysis = RootCauseAnalysis(
            issue_id="test-001",
            root_cause="Unknown",
            affected_files=[],
            affected_functions=[],
            confidence=0.1,  # Below threshold
            reasoning="Low confidence",
            suggested_approach="",
        )
        fix = await generator.generate(analysis)
        assert fix is None

    def test_parse_response_valid(
        self,
        generator: PatchGenerator,
    ) -> None:
        """Test parsing valid LLM response."""
        response = '''
```json
{
    "title": "Fix null check",
    "description": "Add null check",
    "risk_level": "low",
    "confidence": 0.9,
    "reasoning": "Simple fix",
    "patches": [{
        "file_path": "src/example.py",
        "original_content": "old",
        "new_content": "new",
        "start_line": 1,
        "end_line": 1,
        "description": "Fix"
    }],
    "tests_to_run": [],
    "rollback_instructions": ""
}
```
'''
        fix = generator._parse_response("test-001", ["test-001"], response)
        assert fix is not None
        assert fix.title == "Fix null check"
        assert fix.risk_level == RiskLevel.LOW
        assert len(fix.patches) == 1

    def test_parse_response_invalid_json(
        self,
        generator: PatchGenerator,
    ) -> None:
        """Test parsing invalid JSON response."""
        response = "This is not valid JSON"
        fix = generator._parse_response("test-001", ["test-001"], response)
        assert fix is None
