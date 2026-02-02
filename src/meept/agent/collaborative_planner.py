"""Collaborative planner with interactive review.

Wraps the existing :class:`Planner` with workspace-backed planning,
LLM-driven analysis, and an approval workflow so that programming /
automation tasks are reviewed before execution.
"""

from __future__ import annotations

import logging
import re
import uuid
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from meept.agent.planner import Planner
from meept.agent.workspace import WorkspaceManager
from meept.models.tasks import TaskPlan, TaskStatus

log = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Detection heuristics
# ---------------------------------------------------------------------------

_PROGRAMMING_KEYWORDS: list[str] = [
    "write",
    "code",
    "script",
    "program",
    "implement",
    "build",
    "deploy",
    "automate",
    "cron",
    "service",
    "daemon",
    "api",
    "endpoint",
    "database",
    "server",
    "docker",
    "container",
    "pipeline",
    "ci/cd",
    "terraform",
    "ansible",
    "kubernetes",
    "config",
    "infrastructure",
    "migration",
    "refactor",
    "debug",
    "fix bug",
    "patch",
    "update code",
    "git",
    "compile",
    "test",
    "unit test",
    "integration",
]

_APPROVAL_PATTERNS: list[str] = [
    "approved",
    "approve",
    "go ahead",
    "execute",
    "proceed",
    "lgtm",
    "looks good",
    "do it",
    "yes",
]

_REJECTION_PATTERNS: list[str] = [
    "reject",
    "no",
    "stop",
    "cancel",
    "don't",
    "redo",
]

_REVISION_INDICATORS: list[str] = [
    "but",
    "change",
    "modify",
    "add",
    "remove",
    "instead",
    "also",
    "what about",
]

# ---------------------------------------------------------------------------
# Analysis prompt
# ---------------------------------------------------------------------------

_ANALYSIS_SYSTEM_PROMPT = """\
You are a senior technical reviewer. Given a task plan, analyse it for:
1. Missing steps or prerequisites
2. Design flaws or questionable approaches
3. Dependency conflicts
4. Security concerns
5. Edge cases that should be handled
6. Resource requirements or constraints

Be concise but thorough. List each finding as a bullet point. If the plan
looks solid, say so briefly and note any minor suggestions.
"""

# ---------------------------------------------------------------------------
# PlanReview dataclass
# ---------------------------------------------------------------------------


@dataclass
class PlanReview:
    """Result of a collaborative planning + review cycle."""

    task_id: str
    plan: TaskPlan
    analysis: str
    status: str = "pending_approval"  # pending_approval | approved | rejected | revised
    workspace_path: Path = field(default_factory=lambda: Path("."))
    formatted_summary: str = ""


# ---------------------------------------------------------------------------
# CollaborativePlanner
# ---------------------------------------------------------------------------


class CollaborativePlanner:
    """Wraps :class:`Planner` with workspace tracking and interactive review.

    Parameters
    ----------
    planner:
        The underlying :class:`Planner` for task decomposition.
    llm_client:
        LLM client with an async ``chat(messages, **kwargs)`` method,
        used for the analysis pass.
    workspace:
        :class:`WorkspaceManager` for per-task git workspaces.
    """

    def __init__(
        self,
        planner: Planner,
        llm_client: Any,
        workspace: WorkspaceManager,
    ) -> None:
        self._planner = planner
        self._llm = llm_client
        self._workspace = workspace
        # conversation_id -> PlanReview (tracks pending reviews)
        self._pending: dict[str, PlanReview] = {}

    # ------------------------------------------------------------------
    # Public API
    # ------------------------------------------------------------------

    async def is_programming_task(self, message: str) -> bool:
        """Heuristic check whether *message* describes a programming or
        automation task that should go through collaborative review.

        Parameters
        ----------
        message:
            The user's text.

        Returns
        -------
        bool
            ``True`` if the message matches programming-task heuristics.
        """
        lower = message.lower()
        hits = sum(1 for kw in _PROGRAMMING_KEYWORDS if kw in lower)
        return hits >= 2

    def has_pending_review(self, conversation_id: str) -> bool:
        """Return whether *conversation_id* has a pending plan review."""
        review = self._pending.get(conversation_id)
        return review is not None and review.status == "pending_approval"

    def classify_response(self, message: str) -> str:
        """Classify a follow-up message as approval, rejection, or revision.

        Returns
        -------
        str
            One of ``"approve"``, ``"reject"``, or ``"revise"``.
        """
        lower = message.lower().strip()

        # Check approval first (more specific phrases).
        if any(pat in lower for pat in _APPROVAL_PATTERNS):
            # But if revision indicators are also present, treat as revision.
            if any(ind in lower for ind in _REVISION_INDICATORS):
                return "revise"
            return "approve"

        if any(pat in lower for pat in _REJECTION_PATTERNS):
            return "reject"

        # Default: treat unrecognised follow-ups as revision feedback.
        return "revise"

    async def plan_and_review(
        self,
        message: str,
        conversation_id: str,
    ) -> PlanReview:
        """Decompose, analyse, and prepare a plan for user review.

        This method:
        1. Creates a workspace for the task.
        2. Decomposes the message into steps via the underlying planner.
        3. Runs an LLM analysis pass on the plan.
        4. Commits plan and review to the workspace.
        5. Returns a :class:`PlanReview` with ``status="pending_approval"``.

        Parameters
        ----------
        message:
            The user's task request.
        conversation_id:
            Conversation identifier for tracking the review state.

        Returns
        -------
        PlanReview
            The plan + review, ready for user approval.
        """
        task_id = uuid.uuid4().hex[:16]

        # 1. Create workspace.
        workspace_path = await self._workspace.create(task_id, message)

        # 2. Decompose.
        steps = await self._planner.decompose(message)
        plan = TaskPlan(
            id=task_id,
            description=message,
            steps=steps,
            status=TaskStatus.PENDING_APPROVAL,
            workspace_path=str(workspace_path),
        )

        # 3. Write plan to workspace.
        await self._workspace.write_plan(task_id, plan)

        # 4. LLM analysis pass.
        analysis = await self._analyse_plan(plan)
        plan.analysis = analysis

        # 5. Write review to workspace.
        await self._workspace.write_review(task_id, analysis)

        # 6. Build formatted summary.
        summary = self._format_summary(plan, analysis)

        review = PlanReview(
            task_id=task_id,
            plan=plan,
            analysis=analysis,
            status="pending_approval",
            workspace_path=workspace_path,
            formatted_summary=summary,
        )

        # Track for follow-up messages.
        self._pending[conversation_id] = review
        await self._workspace.append_log(task_id, "Plan created and pending approval")

        return review

    async def approve(self, conversation_id: str) -> TaskPlan:
        """Mark the pending plan as approved.

        Parameters
        ----------
        conversation_id:
            Conversation with the pending plan.

        Returns
        -------
        TaskPlan
            The approved plan.

        Raises
        ------
        ValueError
            If there is no pending plan for *conversation_id*.
        """
        review = self._pending.get(conversation_id)
        if review is None or review.status != "pending_approval":
            raise ValueError(f"No pending plan for conversation {conversation_id}")

        review.status = "approved"
        review.plan.approved = True
        review.plan.status = TaskStatus.PENDING

        await self._workspace.append_log(review.task_id, "Plan approved by user")
        await self._workspace.commit(review.task_id, "Plan approved")

        return review.plan

    async def reject(self, conversation_id: str, reason: str = "") -> None:
        """Reject the pending plan.

        Parameters
        ----------
        conversation_id:
            Conversation with the pending plan.
        reason:
            Optional rejection reason.
        """
        review = self._pending.get(conversation_id)
        if review is None:
            return

        review.status = "rejected"
        review.plan.status = TaskStatus.CANCELLED

        log_msg = f"Plan rejected: {reason}" if reason else "Plan rejected"
        await self._workspace.append_log(review.task_id, log_msg)
        await self._workspace.commit(review.task_id, "Plan rejected")

        del self._pending[conversation_id]

    async def revise(self, conversation_id: str, feedback: str) -> PlanReview:
        """Revise the pending plan based on user feedback.

        Re-runs decomposition with the original message plus user
        feedback, then re-analyses.

        Parameters
        ----------
        conversation_id:
            Conversation with the pending plan.
        feedback:
            The user's revision feedback.

        Returns
        -------
        PlanReview
            Updated plan review.

        Raises
        ------
        ValueError
            If there is no pending plan for *conversation_id*.
        """
        review = self._pending.get(conversation_id)
        if review is None:
            raise ValueError(f"No pending plan for conversation {conversation_id}")

        task_id = review.task_id
        original_desc = review.plan.description

        # Re-decompose with feedback incorporated.
        revised_prompt = (
            f"{original_desc}\n\nAdditional requirements/feedback:\n{feedback}"
        )
        steps = await self._planner.decompose(revised_prompt)

        plan = TaskPlan(
            id=task_id,
            description=original_desc,
            steps=steps,
            status=TaskStatus.PENDING_APPROVAL,
            workspace_path=str(review.workspace_path),
        )

        await self._workspace.write_plan(task_id, plan)

        analysis = await self._analyse_plan(plan)
        plan.analysis = analysis
        await self._workspace.write_review(task_id, analysis)

        summary = self._format_summary(plan, analysis)

        review.plan = plan
        review.analysis = analysis
        review.status = "pending_approval"
        review.formatted_summary = summary

        await self._workspace.append_log(task_id, f"Plan revised based on feedback: {feedback[:80]}")
        await self._workspace.commit(task_id, "Plan revised")

        return review

    # ------------------------------------------------------------------
    # Internal helpers
    # ------------------------------------------------------------------

    async def _analyse_plan(self, plan: TaskPlan) -> str:
        """Run the LLM analysis pass over the plan."""
        from meept.llm.models import ChatMessage, Role

        plan_text = self._format_plan_for_analysis(plan)

        messages = [
            ChatMessage(role=Role.SYSTEM, content=_ANALYSIS_SYSTEM_PROMPT),
            ChatMessage(
                role=Role.USER,
                content=(
                    f"Task: {plan.description}\n\n"
                    f"Proposed plan:\n{plan_text}\n\n"
                    "Please review this plan and identify any issues."
                ),
            ),
        ]

        try:
            response = await self._llm.chat(messages)
            return response.content or "No analysis generated."
        except Exception:
            log.warning("LLM analysis pass failed", exc_info=True)
            return "Analysis unavailable (LLM call failed)."

    @staticmethod
    def _format_plan_for_analysis(plan: TaskPlan) -> str:
        """Format a plan's steps as numbered text for the analysis prompt."""
        lines: list[str] = []
        for i, step in enumerate(plan.steps, 1):
            deps = f" [depends on: {', '.join(step.depends_on)}]" if step.depends_on else ""
            hint = f" (tool: {step.tool_hint})" if step.tool_hint else ""
            lines.append(f"{i}. {step.description}{hint}{deps}")
        return "\n".join(lines)

    @staticmethod
    def _format_summary(plan: TaskPlan, analysis: str) -> str:
        """Build a human-readable summary of the plan + review."""
        parts: list[str] = []

        parts.append(f"## Task Plan: {plan.description[:100]}")
        parts.append("")
        parts.append("### Steps")
        for i, step in enumerate(plan.steps, 1):
            deps = f" *(after: {', '.join(step.depends_on)})*" if step.depends_on else ""
            parts.append(f"{i}. {step.description}{deps}")

        parts.append("")
        parts.append("### Review")
        parts.append(analysis)
        parts.append("")
        parts.append(
            "---\n"
            "Reply **approve** / **go ahead** to execute, "
            "**reject** to cancel, or provide feedback to revise."
        )
        return "\n".join(parts)
