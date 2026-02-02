"""Tests for CollaborativePlanner."""

from __future__ import annotations

from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

import pytest

from meept.agent.collaborative_planner import CollaborativePlanner, PlanReview
from meept.agent.planner import Planner
from meept.agent.workspace import WorkspaceManager
from meept.models.tasks import TaskPlan, TaskStatus, TaskStep


# ---------------------------------------------------------------------------
# Mock objects
# ---------------------------------------------------------------------------


@dataclass
class MockLLMResponse:
    content: str
    tool_calls: list = field(default_factory=list)
    usage: Any = None
    model: str = "test"
    finish_reason: str = "stop"


class MockLLMClient:
    """LLM client that returns canned responses."""

    def __init__(self, responses: list[str] | None = None) -> None:
        self._responses = list(responses or ["Mock analysis: plan looks reasonable."])
        self._call_count = 0

    async def chat(self, messages: Any, **kwargs: Any) -> MockLLMResponse:
        idx = min(self._call_count, len(self._responses) - 1)
        self._call_count += 1
        return MockLLMResponse(content=self._responses[idx])

    @property
    def call_count(self) -> int:
        return self._call_count


class MockPlanner:
    """Planner that returns a fixed set of steps."""

    def __init__(self, steps: list[TaskStep] | None = None) -> None:
        self._steps = steps or [
            TaskStep(id="s1", description="Research the problem"),
            TaskStep(id="s2", description="Implement solution", depends_on=["s1"]),
            TaskStep(id="s3", description="Test the solution", depends_on=["s2"]),
        ]
        self.decompose_count = 0

    async def decompose(self, task: str) -> list[TaskStep]:
        self.decompose_count += 1
        return list(self._steps)

    async def should_plan(self, message: str) -> bool:
        return True


# ---------------------------------------------------------------------------
# Keyword detection tests
# ---------------------------------------------------------------------------


class TestIsProgrammingTask:
    """Tests for is_programming_task heuristics."""

    @pytest.fixture
    def planner(self, tmp_path: Path) -> CollaborativePlanner:
        llm = MockLLMClient()
        mock_planner = MockPlanner()
        ws = WorkspaceManager(base_dir=tmp_path, auto_commit=False)
        return CollaborativePlanner(planner=mock_planner, llm_client=llm, workspace=ws)

    @pytest.mark.asyncio
    async def test_detects_code_task(self, planner: CollaborativePlanner) -> None:
        assert await planner.is_programming_task(
            "Write a Python script to scrape weather data"
        )

    @pytest.mark.asyncio
    async def test_detects_deploy_task(self, planner: CollaborativePlanner) -> None:
        assert await planner.is_programming_task(
            "Build and deploy a Docker container for the API"
        )

    @pytest.mark.asyncio
    async def test_detects_automation_task(self, planner: CollaborativePlanner) -> None:
        assert await planner.is_programming_task(
            "Automate the database migration with a cron job"
        )

    @pytest.mark.asyncio
    async def test_rejects_simple_chat(self, planner: CollaborativePlanner) -> None:
        assert not await planner.is_programming_task("What's the weather today?")

    @pytest.mark.asyncio
    async def test_rejects_single_keyword(self, planner: CollaborativePlanner) -> None:
        # Only 1 keyword hit ("test") -- needs >= 2.
        assert not await planner.is_programming_task("Can you test this idea?")

    @pytest.mark.asyncio
    async def test_detects_debugging_task(self, planner: CollaborativePlanner) -> None:
        assert await planner.is_programming_task(
            "Debug and fix bug in the authentication code"
        )


# ---------------------------------------------------------------------------
# Response classification tests
# ---------------------------------------------------------------------------


class TestClassifyResponse:
    @pytest.fixture
    def planner(self, tmp_path: Path) -> CollaborativePlanner:
        llm = MockLLMClient()
        mock_planner = MockPlanner()
        ws = WorkspaceManager(base_dir=tmp_path, auto_commit=False)
        return CollaborativePlanner(planner=mock_planner, llm_client=llm, workspace=ws)

    def test_approve(self, planner: CollaborativePlanner) -> None:
        assert planner.classify_response("Looks good, go ahead") == "approve"
        assert planner.classify_response("approved") == "approve"
        assert planner.classify_response("lgtm") == "approve"
        assert planner.classify_response("Yes") == "approve"

    def test_reject(self, planner: CollaborativePlanner) -> None:
        assert planner.classify_response("reject") == "reject"
        assert planner.classify_response("no") == "reject"
        assert planner.classify_response("cancel") == "reject"

    def test_revise_with_mixed_signals(self, planner: CollaborativePlanner) -> None:
        # "looks good" (approval) + "but" (revision indicator) -> revise
        assert planner.classify_response("Looks good but add error handling") == "revise"

    def test_revise_plain_feedback(self, planner: CollaborativePlanner) -> None:
        assert planner.classify_response("Add more error handling for edge cases") == "revise"


# ---------------------------------------------------------------------------
# Plan and review lifecycle tests
# ---------------------------------------------------------------------------


class TestPlanAndReview:
    @pytest.fixture
    def components(self, tmp_path: Path) -> dict[str, Any]:
        llm = MockLLMClient(["Analysis: missing error handling step."])
        mock_planner = MockPlanner()
        ws = WorkspaceManager(base_dir=tmp_path, auto_commit=False)
        cp = CollaborativePlanner(planner=mock_planner, llm_client=llm, workspace=ws)
        return {
            "planner": cp,
            "mock_planner": mock_planner,
            "llm": llm,
            "workspace": ws,
            "tmp_path": tmp_path,
        }

    @pytest.mark.asyncio
    async def test_plan_and_review_creates_workspace(self, components: dict) -> None:
        cp = components["planner"]
        review = await cp.plan_and_review("Write a scraper script", "conv1")

        assert isinstance(review, PlanReview)
        assert review.status == "pending_approval"
        assert review.workspace_path.is_dir()
        assert (review.workspace_path / "PLAN.md").exists()
        assert (review.workspace_path / "REVIEW.md").exists()

    @pytest.mark.asyncio
    async def test_plan_and_review_calls_decompose(self, components: dict) -> None:
        cp = components["planner"]
        mock_planner = components["mock_planner"]

        await cp.plan_and_review("Implement an API endpoint", "conv1")
        assert mock_planner.decompose_count == 1

    @pytest.mark.asyncio
    async def test_plan_and_review_calls_llm_analysis(self, components: dict) -> None:
        cp = components["planner"]
        llm = components["llm"]

        review = await cp.plan_and_review("Build a database migration", "conv1")
        # LLM should be called once for analysis.
        assert llm.call_count == 1
        assert "missing error handling" in review.analysis

    @pytest.mark.asyncio
    async def test_plan_and_review_formatted_summary(self, components: dict) -> None:
        cp = components["planner"]
        review = await cp.plan_and_review("Write a test suite", "conv1")

        assert "Steps" in review.formatted_summary
        assert "Review" in review.formatted_summary
        assert "approve" in review.formatted_summary.lower()

    @pytest.mark.asyncio
    async def test_has_pending_review(self, components: dict) -> None:
        cp = components["planner"]

        assert not cp.has_pending_review("conv1")
        await cp.plan_and_review("Write code", "conv1")
        assert cp.has_pending_review("conv1")

    @pytest.mark.asyncio
    async def test_plan_sets_pending_approval_status(self, components: dict) -> None:
        cp = components["planner"]
        review = await cp.plan_and_review("Write code", "conv1")
        assert review.plan.status == TaskStatus.PENDING_APPROVAL


# ---------------------------------------------------------------------------
# Approval / rejection / revision tests
# ---------------------------------------------------------------------------


class TestApproval:
    @pytest.fixture
    async def setup(self, tmp_path: Path) -> dict[str, Any]:
        llm = MockLLMClient(["Analysis looks good.", "Revised analysis."])
        mock_planner = MockPlanner()
        ws = WorkspaceManager(base_dir=tmp_path, auto_commit=False)
        cp = CollaborativePlanner(planner=mock_planner, llm_client=llm, workspace=ws)

        review = await cp.plan_and_review("Build a deployment pipeline", "conv1")
        return {"planner": cp, "review": review}

    @pytest.mark.asyncio
    async def test_approve(self, setup: dict) -> None:
        cp = setup["planner"]
        plan = await cp.approve("conv1")

        assert plan.approved is True
        assert plan.status == TaskStatus.PENDING
        # After approval, still tracked but status changed.

    @pytest.mark.asyncio
    async def test_approve_no_pending(self, tmp_path: Path) -> None:
        llm = MockLLMClient()
        mock_planner = MockPlanner()
        ws = WorkspaceManager(base_dir=tmp_path, auto_commit=False)
        cp = CollaborativePlanner(planner=mock_planner, llm_client=llm, workspace=ws)

        with pytest.raises(ValueError, match="No pending plan"):
            await cp.approve("nonexistent")

    @pytest.mark.asyncio
    async def test_reject(self, setup: dict) -> None:
        cp = setup["planner"]
        await cp.reject("conv1", reason="Not what I wanted")

        assert not cp.has_pending_review("conv1")

    @pytest.mark.asyncio
    async def test_reject_unknown_is_noop(self, tmp_path: Path) -> None:
        llm = MockLLMClient()
        mock_planner = MockPlanner()
        ws = WorkspaceManager(base_dir=tmp_path, auto_commit=False)
        cp = CollaborativePlanner(planner=mock_planner, llm_client=llm, workspace=ws)

        # Should not raise.
        await cp.reject("nonexistent", reason="test")

    @pytest.mark.asyncio
    async def test_revise(self, setup: dict) -> None:
        cp = setup["planner"]
        review = await cp.revise("conv1", feedback="Add error handling for rate limits")

        assert isinstance(review, PlanReview)
        assert review.status == "pending_approval"
        assert cp.has_pending_review("conv1")

    @pytest.mark.asyncio
    async def test_revise_no_pending(self, tmp_path: Path) -> None:
        llm = MockLLMClient()
        mock_planner = MockPlanner()
        ws = WorkspaceManager(base_dir=tmp_path, auto_commit=False)
        cp = CollaborativePlanner(planner=mock_planner, llm_client=llm, workspace=ws)

        with pytest.raises(ValueError, match="No pending plan"):
            await cp.revise("nonexistent", "feedback")


# ---------------------------------------------------------------------------
# Config tests
# ---------------------------------------------------------------------------


def test_workspace_config_defaults() -> None:
    """WorkspaceConfig should have sensible defaults."""
    from meept.models.config_schema import WorkspaceConfig

    cfg = WorkspaceConfig()
    assert cfg.enabled is True
    assert cfg.base_dir == "~/.meept/workspaces"
    assert cfg.auto_commit is True
    assert cfg.commit_on_plan is True
    assert cfg.commit_on_step is True
    assert cfg.cleanup_completed is False


def test_workspace_config_in_settings() -> None:
    """MeeptSettings should include the workspace section."""
    from meept.models.config_schema import MeeptSettings

    settings = MeeptSettings()
    assert hasattr(settings, "workspace")
    assert settings.workspace.enabled is True


def test_task_plan_new_fields() -> None:
    """TaskPlan should have workspace_path, analysis, and approved fields."""
    plan = TaskPlan(description="test")
    assert plan.workspace_path is None
    assert plan.analysis is None
    assert plan.approved is False


def test_task_status_pending_approval() -> None:
    """TaskStatus should include PENDING_APPROVAL."""
    assert TaskStatus.PENDING_APPROVAL.value == "pending_approval"


def test_message_types() -> None:
    """New message types should be present."""
    from meept.models.messages import MessageType

    assert MessageType.PLAN_REVIEW.value == "plan_review"
    assert MessageType.PLAN_APPROVED.value == "plan_approved"
    assert MessageType.PLAN_REJECTED.value == "plan_rejected"
    assert MessageType.WORKSPACE_CREATED.value == "workspace_created"
