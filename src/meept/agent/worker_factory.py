"""Worker factory -- creates ephemeral skill-specific AgentLoop instances.

Replaces the per-skill loop creation from :class:`TaskExecutor` and adds
``schedule_job`` tool injection.
"""

from __future__ import annotations

import logging
from typing import Any, Callable

from meept.skills.models import SkillDefinition
from meept.skills.tool_filter import FilteredToolRegistry
from meept.tools.interface import ToolRegistry

log = logging.getLogger(__name__)


class WorkerFactory:
    """Creates ephemeral :class:`AgentLoop` workers for pipeline steps.

    Each worker gets a skill-specific system prompt, filtered tools,
    an injected ``schedule_job`` tool, and the skill's LLM model.

    Parameters
    ----------
    tool_registry:
        The full tool registry.
    security:
        Security engine or permission manager.
    memory:
        Shared memory manager (optional).
    bus:
        Internal message bus.
    llm_factory:
        Callable ``(model_name: str) -> llm_client``.
    scheduler:
        The MeeptScheduler instance (for schedule_job injection).
    """

    def __init__(
        self,
        tool_registry: ToolRegistry,
        security: Any,
        memory: Any | None = None,
        bus: Any | None = None,
        llm_factory: Any | None = None,
        scheduler: Any | None = None,
    ) -> None:
        self._registry = tool_registry
        self._security = security
        self._memory = memory
        self._bus = bus
        self._llm_factory = llm_factory
        self._scheduler = scheduler

    def create(self, skill: SkillDefinition | None = None) -> Any:
        """Create an ephemeral AgentLoop for *skill*.

        Parameters
        ----------
        skill:
            Skill definition to configure the worker for.  When ``None``
            a default-configured loop is returned.

        Returns
        -------
        AgentLoop
            A freshly created, configured agent loop.
        """
        from meept.agent.loop import AgentLoop

        system_prompt: str | None = None
        max_iterations = 10
        model_name = "default"
        allowed_tools: list[str] | None = None

        if skill is not None:
            prompt_parts = []
            if skill.system_prompt:
                prompt_parts.append(skill.system_prompt)
            if skill.instructions:
                prompt_parts.append(skill.instructions)
            system_prompt = "\n\n".join(prompt_parts) if prompt_parts else None
            max_iterations = skill.max_iterations
            model_name = skill.model
            allowed_tools = skill.allowed_tools if skill.allowed_tools else None

        # Create LLM client.
        llm_client = None
        if self._llm_factory is not None:
            try:
                llm_client = self._llm_factory(model_name)
            except Exception:
                log.warning(
                    "Could not create LLM client for model %r; worker will need a default client",
                    model_name,
                )

        # Build filtered tool registry.
        filtered_registry = FilteredToolRegistry(
            parent=self._registry,
            allowed_tools=allowed_tools,
        )

        # Inject schedule_job tool if scheduler is available.
        if self._scheduler is not None:
            self._ensure_schedule_tool_registered()

        return AgentLoop(
            llm_client=llm_client,
            tool_registry=filtered_registry,
            security=self._security,
            memory_manager=self._memory,
            bus=self._bus,
            config={"max_iterations": max_iterations},
            system_prompt_override=system_prompt,
        )

    def create_handler(
        self,
        skill: SkillDefinition | None,
        step_description: str,
    ) -> Callable[[dict[str, Any]], Any]:
        """Return a callable suitable for :attr:`PipelineStep.handler`.

        The returned callable:
        1. Creates an AgentLoop via :meth:`create`.
        2. Calls ``loop.run_once(step_description)``.
        3. Returns the result string.

        Parameters
        ----------
        skill:
            Skill definition (or ``None`` for default).
        step_description:
            The task description for the step.
        """
        factory = self

        async def _handler(context: dict[str, Any]) -> str:
            loop = factory.create(skill)
            # Build the prompt with context if dependencies provided results.
            prompt = step_description
            if context:
                context_lines = [f"- {k}: {v}" for k, v in context.items()]
                prompt = (
                    f"{step_description}\n\n"
                    f"Context from previous steps:\n"
                    + "\n".join(context_lines)
                )
            return await loop.run_once(prompt)

        return _handler

    def _ensure_schedule_tool_registered(self) -> None:
        """Register the schedule_job tool in the parent registry if not already present."""
        if "schedule_job" in self._registry:
            return

        from meept.tools.builtin.schedule_tool import ScheduleTool

        tool = ScheduleTool(scheduler=self._scheduler, security=self._security)
        self._registry.register(tool)
        log.info("WorkerFactory: registered schedule_job tool")
