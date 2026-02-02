"""Task executor -- creates per-skill AgentLoop instances and manages lifecycle.

The :class:`TaskExecutor` is responsible for:

- Creating a skill-specific :class:`AgentLoop` with the right model, tools,
  and system prompt.
- Running single skill requests.
- Running multi-step :class:`TaskPlan` objects, routing each step to its
  assigned skill.
"""

from __future__ import annotations

import logging
from typing import Any

from meept.models.tasks import TaskPlan, TaskStatus
from meept.skills.models import SkillDefinition, TriageResult
from meept.skills.tool_filter import FilteredToolRegistry
from meept.tools.interface import ToolRegistry

log = logging.getLogger(__name__)


class TaskExecutor:
    """Executes tasks using skill-specific agent loops.

    Parameters
    ----------
    tool_registry:
        The full tool registry (will be filtered per-skill).
    security:
        The security engine passed to each skill's AgentLoop.
    memory_manager:
        Shared memory manager.
    bus:
        Internal message bus.
    llm_factory:
        Callable that creates an LLM client for a given model name.
        Signature: ``(model_name: str) -> llm_client``.
    budget:
        Shared token budget object passed to all LLM clients.
    """

    def __init__(
        self,
        tool_registry: ToolRegistry,
        security: Any,
        memory_manager: Any | None = None,
        bus: Any | None = None,
        llm_factory: Any | None = None,
        budget: Any | None = None,
    ) -> None:
        self._registry = tool_registry
        self._security = security
        self._memory = memory_manager
        self._bus = bus
        self._llm_factory = llm_factory
        self._budget = budget

    async def execute_with_skill(
        self,
        message: str,
        skill: SkillDefinition,
        triage_result: TriageResult | None = None,
        conversation_id: str | None = None,
    ) -> str:
        """Run a single user message through a skill-specific AgentLoop.

        Parameters
        ----------
        message:
            The user message to process.
        skill:
            The skill definition to use.
        triage_result:
            Optional triage result with extracted params.
        conversation_id:
            Optional conversation identifier.

        Returns
        -------
        str
            The agent's text response.
        """
        loop = self._create_skill_loop(skill)

        try:
            return await loop.run_once(message, conversation_id=conversation_id)
        except Exception:
            log.error("Skill %r execution failed", skill.name, exc_info=True)
            return f"Skill '{skill.name}' encountered an error processing your request."

    async def execute_plan(
        self,
        message: str,
        plan: TaskPlan,
        skills: dict[str, SkillDefinition] | None = None,
        default_loop: Any | None = None,
    ) -> str:
        """Execute a multi-step TaskPlan, routing each step to its skill.

        Parameters
        ----------
        message:
            The original user message (used as context).
        plan:
            The task plan to execute.
        skills:
            Mapping of skill name to definition for step routing.
        default_loop:
            Fallback AgentLoop for steps without a skill assignment.

        Returns
        -------
        str
            Synthesised final response.
        """
        skills = skills or {}
        results: list[str] = []

        for step in plan.steps:
            step.status = TaskStatus.IN_PROGRESS

            skill = skills.get(step.skill_name) if step.skill_name else None

            try:
                if skill is not None:
                    result = await self.execute_with_skill(
                        step.description, skill,
                    )
                elif default_loop is not None:
                    result = await default_loop.run_once(step.description)
                else:
                    result = f"Step skipped: no skill or default loop for '{step.description}'"

                step.result = result
                step.status = TaskStatus.COMPLETED
                results.append(f"**{step.id}**: {result}")

            except Exception as exc:
                log.error("Plan step %s failed: %s", step.id, exc, exc_info=True)
                step.status = TaskStatus.FAILED
                step.result = str(exc)
                results.append(f"**{step.id}**: Failed -- {exc}")

        plan.status = TaskStatus.COMPLETED
        return "\n\n".join(results) if results else "No results produced."

    def _create_skill_loop(self, skill: SkillDefinition) -> Any:
        """Create a skill-specific AgentLoop instance."""
        from meept.agent.loop import AgentLoop

        # Build skill-specific system prompt.
        prompt_parts = []
        if skill.system_prompt:
            prompt_parts.append(skill.system_prompt)
        if skill.instructions:
            prompt_parts.append(skill.instructions)
        system_prompt = "\n\n".join(prompt_parts) if prompt_parts else None

        # Create or reuse LLM client.
        llm_client = None
        if self._llm_factory is not None:
            try:
                llm_client = self._llm_factory(skill.model)
            except Exception:
                log.warning(
                    "Could not create LLM client for model %r; "
                    "skill %r will need a default client",
                    skill.model, skill.name,
                )

        # Filtered tool registry.
        filtered_registry = FilteredToolRegistry(
            parent=self._registry,
            allowed_tools=skill.allowed_tools if skill.allowed_tools else None,
        )

        return AgentLoop(
            llm_client=llm_client,
            tool_registry=filtered_registry,
            security=self._security,
            memory_manager=self._memory,
            bus=self._bus,
            config={"max_iterations": skill.max_iterations},
            system_prompt_override=system_prompt,
        )
