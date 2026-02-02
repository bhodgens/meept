"""FrontAgent -- thin entry point that replaces SkillDispatcher.

The FrontAgent is the bus handler for ``chat.request`` when skills are
enabled.  It validates input, classifies intent via TriageAgent, decides
between simple and complex execution paths, and delegates to the
Orchestrator for pipeline execution.
"""

from __future__ import annotations

import logging
from typing import Any

from meept.models.messages import BusMessage, MessageType
from meept.models.tasks import TaskStep

log = logging.getLogger(__name__)


class FrontAgent:
    """Thin, fast entry point for chat requests.

    Parameters
    ----------
    orchestrator:
        The :class:`~meept.agent.orchestrator.Orchestrator` for pipeline execution.
    triage_agent:
        Optional :class:`~meept.skills.triage.TriageAgent` for intent classification.
    planner:
        Optional :class:`~meept.agent.planner.Planner` for multi-step decomposition.
    default_loop:
        Default :class:`~meept.agent.loop.AgentLoop` for fallback handling.
    skill_registry:
        Optional :class:`~meept.skills.registry.SkillRegistry`.
    bus:
        Internal message bus.
    """

    def __init__(
        self,
        orchestrator: Any,
        triage_agent: Any | None = None,
        planner: Any | None = None,
        default_loop: Any | None = None,
        skill_registry: Any | None = None,
        bus: Any | None = None,
        collaborative_planner: Any | None = None,
        workspace_manager: Any | None = None,
    ) -> None:
        self._orchestrator = orchestrator
        self._triage = triage_agent
        self._planner = planner
        self._default_loop = default_loop
        self._skill_registry = skill_registry
        self._bus = bus
        self._collaborative_planner = collaborative_planner
        self._workspace_manager = workspace_manager

    async def handle_chat_request(self, message: BusMessage) -> None:
        """Bus handler for incoming chat requests.

        Replaces the default AgentLoop.handle_chat_request when skills
        are enabled.
        """
        text = message.payload.get("text", "")
        conv_id = message.payload.get("conversation_id")

        if not text or not text.strip():
            log.warning("FrontAgent: empty chat request from %s", message.source)
            return

        # Sanitize: strip leading/trailing whitespace.
        text = text.strip()

        log.info("FrontAgent: handling request from %s (conv=%s)", message.source, conv_id)

        response_text = await self.dispatch(text, conversation_id=conv_id)

        await self._publish(
            MessageType.CHAT_RESPONSE,
            {
                "text": response_text,
                "conversation_id": conv_id,
            },
            reply_to=message.id,
        )

    async def dispatch(
        self,
        message: str,
        conversation_id: str | None = None,
    ) -> str:
        """Route a user message through the agent pipeline.

        Decision flow:
        1. Triage (if available): classify intent.
        2. High-confidence skill match -> 1-step pipeline with skill handler.
        3. Planner.should_plan() -> decompose -> multi-step pipeline.
        4. Fallback -> 1-step pipeline with default handler.

        Parameters
        ----------
        message:
            The user's text input.
        conversation_id:
            Optional conversation identifier.

        Returns
        -------
        str
            The agent's response.
        """
        triage_result = None

        # Step 1: Triage (if enabled).
        if self._triage is not None:
            triage_result = await self._triage.classify(message)

            await self._publish(
                MessageType.TRIAGE_RESULT,
                {
                    "skill_name": triage_result.skill_name,
                    "confidence": triage_result.confidence,
                    "reasoning": triage_result.reasoning,
                    "fallback": triage_result.fallback_to_default,
                    "conversation_id": conversation_id,
                },
            )

            # Step 2: Direct skill execution via 1-step pipeline.
            if not triage_result.fallback_to_default:
                skill = None
                if self._skill_registry is not None:
                    skill = self._skill_registry.get(triage_result.skill_name)

                if skill is not None:
                    await self._publish(
                        MessageType.SKILL_TASK_START,
                        {
                            "skill_name": skill.name,
                            "conversation_id": conversation_id,
                        },
                    )

                    step = TaskStep(
                        description=message,
                        skill_name=skill.name,
                    )
                    result = await self._orchestrator.execute_single(step)

                    await self._publish(
                        MessageType.SKILL_TASK_COMPLETE,
                        {
                            "skill_name": skill.name,
                            "conversation_id": conversation_id,
                        },
                    )

                    return result

        # Step 3a: Collaborative planning check (programming/automation tasks).
        if self._collaborative_planner is not None:
            collab = self._collaborative_planner

            # Handle follow-up messages for pending plan reviews.
            if conversation_id and collab.has_pending_review(conversation_id):
                return await self._handle_plan_followup(message, conversation_id)

            # Check if this is a new programming task.
            if await collab.is_programming_task(message):
                review = await collab.plan_and_review(
                    message, conversation_id or "default",
                )

                await self._publish(
                    MessageType.PLAN_REVIEW,
                    {
                        "task_id": review.task_id,
                        "status": review.status,
                        "conversation_id": conversation_id,
                    },
                )

                return review.formatted_summary

        # Step 3b: Multi-step planning (if planner available).
        if self._planner is not None:
            should_plan = getattr(self._planner, "should_plan", None)
            if should_plan is not None:
                import inspect

                if inspect.iscoroutinefunction(should_plan):
                    needs_plan = await should_plan(message)
                else:
                    needs_plan = should_plan(message)

                if needs_plan:
                    plan = await self._planner.decompose(message)

                    await self._publish(
                        MessageType.CHAT_PROGRESS,
                        {
                            "event": "planning_complete",
                            "steps": len(plan.steps),
                            "conversation_id": conversation_id,
                        },
                    )

                    orch_result = await self._orchestrator.execute(plan.steps)
                    return orch_result.synthesized

        # Step 4: Default fallback -- 1-step pipeline with default handler.
        if self._default_loop is not None:
            return await self._default_loop.run_once(message, conversation_id=conversation_id)

        # No handler available at all.
        step = TaskStep(description=message)
        return await self._orchestrator.execute_single(step)

    # ------------------------------------------------------------------
    # Collaborative planning follow-up
    # ------------------------------------------------------------------

    async def _handle_plan_followup(self, message: str, conversation_id: str) -> str:
        """Handle a follow-up message for a pending plan review.

        Classifies the message as approval, rejection, or revision and
        routes accordingly.
        """
        collab = self._collaborative_planner
        action = collab.classify_response(message)

        if action == "approve":
            plan = await collab.approve(conversation_id)

            await self._publish(
                MessageType.PLAN_APPROVED,
                {
                    "task_id": plan.id,
                    "conversation_id": conversation_id,
                },
            )

            # Execute the approved plan.
            await self._publish(
                MessageType.CHAT_PROGRESS,
                {
                    "event": "executing_approved_plan",
                    "steps": len(plan.steps),
                    "conversation_id": conversation_id,
                },
            )

            orch_result = await self._orchestrator.execute(plan.steps)

            # Commit final results to workspace if workspace manager available.
            if self._workspace_manager is not None and plan.workspace_path:
                await self._workspace_manager.append_log(
                    plan.id, f"Execution complete (success={orch_result.success})",
                )
                await self._workspace_manager.commit(plan.id, "Execution complete")

            return orch_result.synthesized

        if action == "reject":
            await collab.reject(conversation_id, reason=message)

            await self._publish(
                MessageType.PLAN_REJECTED,
                {
                    "conversation_id": conversation_id,
                },
            )

            return "Plan rejected. Let me know if you'd like to try a different approach."

        # Revision.
        review = await collab.revise(conversation_id, feedback=message)

        await self._publish(
            MessageType.PLAN_REVIEW,
            {
                "task_id": review.task_id,
                "status": review.status,
                "conversation_id": conversation_id,
            },
        )

        return review.formatted_summary

    # ------------------------------------------------------------------
    # Bus helpers
    # ------------------------------------------------------------------

    async def _publish(
        self,
        msg_type: MessageType,
        payload: dict[str, Any],
        reply_to: str | None = None,
    ) -> None:
        """Publish a message on the bus if available."""
        if self._bus is None:
            return

        import inspect

        msg = BusMessage(
            type=msg_type,
            payload=payload,
            source="front_agent",
            reply_to=reply_to,
        )

        topic_map = {
            MessageType.CHAT_RESPONSE: "chat.response",
            MessageType.TRIAGE_RESULT: "skills.triage_result",
            MessageType.SKILL_TASK_START: "skills.skill_task_start",
            MessageType.SKILL_TASK_COMPLETE: "skills.skill_task_complete",
            MessageType.CHAT_PROGRESS: "chat.progress",
            MessageType.PIPELINE_PROGRESS: "pipeline.progress",
            MessageType.PLAN_REVIEW: "planning.plan_review",
            MessageType.PLAN_APPROVED: "planning.plan_approved",
            MessageType.PLAN_REJECTED: "planning.plan_rejected",
            MessageType.WORKSPACE_CREATED: "workspace.created",
        }
        topic = topic_map.get(msg_type, f"front_agent.{msg_type.value}")

        publish = getattr(self._bus, "publish", None)
        if publish is not None:
            if inspect.iscoroutinefunction(publish):
                await publish(topic, msg)
            else:
                publish(topic, msg)
