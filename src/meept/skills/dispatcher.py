"""Top-level skill dispatcher -- coordinates triage, execution, and fallback.

The :class:`SkillDispatcher` sits at the bus level and replaces direct
``AgentLoop`` subscription for CHAT_REQUEST when skills are enabled.

Flow:
    1. Receive CHAT_REQUEST from bus.
    2. If triage enabled: classify with TriageAgent.
    3. If confidence >= threshold and skill exists: route to TaskExecutor.
    4. If Planner.should_plan(): decompose and execute multi-step plan.
    5. Otherwise: delegate to default AgentLoop.
    6. Publish CHAT_RESPONSE.
"""

from __future__ import annotations

import logging
from typing import Any

from meept.models.messages import BusMessage, MessageType
from meept.skills.executor import TaskExecutor
from meept.skills.models import TriageResult
from meept.skills.registry import SkillRegistry
from meept.skills.triage import TriageAgent

log = logging.getLogger(__name__)


class SkillDispatcher:
    """Coordinator that routes messages to skill-specific or default agents.

    Parameters
    ----------
    skill_registry:
        Registry of loaded skill definitions.
    triage_agent:
        Triage classifier (may be ``None`` if triage is disabled).
    task_executor:
        Executor for skill-specific agent loops.
    default_loop:
        The standard AgentLoop used when no skill matches.
    planner:
        Optional planner for multi-step decomposition.
    bus:
        Internal message bus.
    """

    def __init__(
        self,
        skill_registry: SkillRegistry,
        triage_agent: TriageAgent | None,
        task_executor: TaskExecutor,
        default_loop: Any,
        planner: Any | None = None,
        bus: Any | None = None,
    ) -> None:
        self._skill_registry = skill_registry
        self._triage = triage_agent
        self._executor = task_executor
        self._default_loop = default_loop
        self._planner = planner
        self._bus = bus

    async def handle_chat_request(self, message: BusMessage) -> None:
        """Bus handler for incoming chat requests.

        Replaces the default AgentLoop.handle_chat_request when skills
        are enabled.
        """
        text = message.payload.get("text", "")
        conv_id = message.payload.get("conversation_id")

        if not text:
            log.warning("SkillDispatcher: empty chat request from %s", message.source)
            return

        log.info("SkillDispatcher: handling request from %s (conv=%s)", message.source, conv_id)

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
        """Route a user message through the skill pipeline.

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
        triage_result: TriageResult | None = None

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

            # Step 2: Direct skill execution.
            if not triage_result.fallback_to_default:
                skill = self._skill_registry.get(triage_result.skill_name)
                if skill is not None:
                    await self._publish(
                        MessageType.SKILL_TASK_START,
                        {
                            "skill_name": skill.name,
                            "conversation_id": conversation_id,
                        },
                    )

                    result = await self._executor.execute_with_skill(
                        message, skill, triage_result,
                        conversation_id=conversation_id,
                    )

                    await self._publish(
                        MessageType.SKILL_TASK_COMPLETE,
                        {
                            "skill_name": skill.name,
                            "conversation_id": conversation_id,
                        },
                    )

                    return result

        # Step 3: Multi-step planning (if planner available).
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
                    skills_map = {
                        s.name: s for s in self._skill_registry.list_skills()
                    }
                    return await self._executor.execute_plan(
                        message, plan,
                        skills=skills_map,
                        default_loop=self._default_loop,
                    )

        # Step 4: Default agent loop.
        return await self._default_loop.run_once(message, conversation_id=conversation_id)

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
            source="skill_dispatcher",
            reply_to=reply_to,
        )

        topic = f"skills.{msg_type.value}"

        publish = getattr(self._bus, "publish", None)
        if publish is not None:
            if inspect.iscoroutinefunction(publish):
                await publish(topic, msg)
            else:
                publish(topic, msg)
