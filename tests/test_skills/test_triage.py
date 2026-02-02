"""Tests for the TriageAgent."""

from __future__ import annotations

from dataclasses import dataclass

import pytest

from meept.skills.models import SkillDefinition, TriageResult
from meept.skills.triage import TriageAgent


# ---------------------------------------------------------------------------
# Mock LLM client
# ---------------------------------------------------------------------------


@dataclass
class MockLLMResponse:
    content: str
    tool_calls: list = None

    def __post_init__(self):
        if self.tool_calls is None:
            self.tool_calls = []


class MockLLM:
    """LLM client that returns a canned response."""

    def __init__(self, response_content: str) -> None:
        self._content = response_content
        self.call_count = 0

    async def chat(self, messages, **kwargs):
        self.call_count += 1
        return MockLLMResponse(content=self._content)


class FailingLLM:
    """LLM client that always raises."""

    async def chat(self, messages, **kwargs):
        raise RuntimeError("LLM down")


# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------


_TEST_SKILLS = [
    SkillDefinition(
        name="code_review",
        description="Reviews code for bugs and style",
        trigger_keywords=["review", "check my code"],
        examples=["Can you review src/main.py?"],
    ),
    SkillDefinition(
        name="web_search",
        description="Searches the web for information",
        trigger_keywords=["search", "find"],
        examples=["Search for Python best practices"],
    ),
]


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_classify_returns_skill() -> None:
    """Triage should return the correct skill when LLM responds properly."""
    llm = MockLLM('{"skill_name": "code_review", "confidence": 0.9, "reasoning": "User wants a review"}')
    agent = TriageAgent(llm, _TEST_SKILLS, confidence_threshold=0.5)

    result = await agent.classify("Please review my code in main.py")

    assert result.skill_name == "code_review"
    assert result.confidence == 0.9
    assert result.fallback_to_default is False
    assert llm.call_count == 1


@pytest.mark.asyncio
async def test_classify_low_confidence_fallback() -> None:
    """Below-threshold confidence should set fallback_to_default."""
    llm = MockLLM('{"skill_name": "code_review", "confidence": 0.3, "reasoning": "unsure"}')
    agent = TriageAgent(llm, _TEST_SKILLS, confidence_threshold=0.5)

    result = await agent.classify("Do something")

    assert result.skill_name == "code_review"
    assert result.confidence == 0.3
    assert result.fallback_to_default is True


@pytest.mark.asyncio
async def test_classify_default_skill() -> None:
    """LLM returning 'default' should be treated as fallback."""
    llm = MockLLM('{"skill_name": "default", "confidence": 0.8, "reasoning": "general request"}')
    agent = TriageAgent(llm, _TEST_SKILLS)

    result = await agent.classify("Hello!")

    assert result.skill_name == "default"
    assert result.fallback_to_default is True


@pytest.mark.asyncio
async def test_classify_unknown_skill() -> None:
    """Unknown skill names should fall back to default."""
    llm = MockLLM('{"skill_name": "nonexistent", "confidence": 0.9, "reasoning": "bad match"}')
    agent = TriageAgent(llm, _TEST_SKILLS)

    result = await agent.classify("Do something specific")

    assert result.skill_name == "default"
    assert result.fallback_to_default is True


@pytest.mark.asyncio
async def test_classify_llm_failure() -> None:
    """LLM failure should fall back gracefully."""
    agent = TriageAgent(FailingLLM(), _TEST_SKILLS)

    result = await agent.classify("Hello")

    assert result.fallback_to_default is True


@pytest.mark.asyncio
async def test_classify_invalid_json() -> None:
    """Invalid JSON response should fall back gracefully."""
    llm = MockLLM("This is not JSON at all")
    agent = TriageAgent(llm, _TEST_SKILLS)

    result = await agent.classify("Hello")

    assert result.fallback_to_default is True


@pytest.mark.asyncio
async def test_classify_with_markdown_fences() -> None:
    """JSON wrapped in markdown fences should be parsed correctly."""
    llm = MockLLM('```json\n{"skill_name": "web_search", "confidence": 0.85, "reasoning": "search query"}\n```')
    agent = TriageAgent(llm, _TEST_SKILLS)

    result = await agent.classify("Search for Python tutorials")

    assert result.skill_name == "web_search"
    assert result.confidence == 0.85


@pytest.mark.asyncio
async def test_classify_with_extracted_params() -> None:
    """Extracted params should be preserved."""
    llm = MockLLM(
        '{"skill_name": "code_review", "confidence": 0.95, '
        '"extracted_params": {"file": "main.py"}, "reasoning": "file review"}'
    )
    agent = TriageAgent(llm, _TEST_SKILLS)

    result = await agent.classify("Review main.py")

    assert result.extracted_params == {"file": "main.py"}


@pytest.mark.asyncio
async def test_classify_empty_skills() -> None:
    """Triage with no skills should work (always default)."""
    llm = MockLLM('{"skill_name": "default", "confidence": 0.5, "reasoning": "no skills"}')
    agent = TriageAgent(llm, [])

    result = await agent.classify("Hello")

    assert result.skill_name == "default"
