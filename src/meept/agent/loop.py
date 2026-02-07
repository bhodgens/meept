"""Main agent reasoning/action loop.

The :class:`AgentLoop` orchestrates the core cycle of:

1. Building a system prompt from the constitution, restrictions, purpose,
   and personality.
2. Sending the conversation to the LLM.
3. Interpreting tool-call requests from the LLM response.
4. Executing tool calls through the :class:`ActionExecutor`.
5. Feeding results back to the LLM until it produces a final text reply.

An iteration cap prevents infinite loops.
"""

from __future__ import annotations

import json
import logging
import uuid
from collections import OrderedDict
from typing import Any

from meept.llm.models import ChatMessage, LLMResponse, Role, ToolCall
from meept.models.messages import BusMessage, MessageType
from meept.tools.interface import ToolRegistry

log = logging.getLogger(__name__)

# Maximum number of concurrent conversations before LRU eviction.
_MAX_CONVERSATIONS = 100

# Maximum messages per conversation before pruning (keeps system + last N).
_MAX_MESSAGES_PER_CONVERSATION = 200

# Default system prompt sections (loaded from config files at runtime,
# but these serve as fallbacks).

_DEFAULT_CONSTITUTION = (
    "You are Meept, an autonomous assistant. Serve your creator honestly "
    "and transparently. Respect boundaries, minimise harm, and learn from "
    "past interactions."
)

_DEFAULT_RESTRICTIONS = (
    "Never execute financial transactions. Never exfiltrate credentials. "
    "Never attempt self-replication. Only connect to explicitly configured "
    "endpoints."
)

_DEFAULT_PURPOSE = (
    "Break complex tasks into steps. Verify results after every action. "
    "Use the right tool for each job. Communicate status proactively."
)


# ---------------------------------------------------------------------------
# AgentLoop
# ---------------------------------------------------------------------------


class AgentLoop:
    """Orchestrates LLM reasoning interleaved with tool execution.

    Parameters
    ----------
    llm_client:
        An object with ``async chat(messages, tools=..., **kw) -> LLMResponse``.
    tool_registry:
        Registry of available tools.
    security:
        The permission manager (used by the executor).
    memory_manager:
        Optional memory subsystem.  If provided, the loop will query it
        for relevant context before each turn.
    bus:
        Internal message bus for event publishing.
    config:
        Application configuration dict or object. Expected keys/attrs:
        ``constitution``, ``restrictions``, ``purpose``, ``personality``,
        ``max_iterations``.  All are optional.
    """

    def __init__(
        self,
        llm_client: Any,
        tool_registry: ToolRegistry,
        security: Any,
        memory_manager: Any | None = None,
        bus: Any | None = None,
        config: Any | None = None,
        system_prompt_override: str | None = None,
        prompt_guard: Any | None = None,
        output_monitor: Any | None = None,
        input_sanitizer: Any | None = None,
    ) -> None:
        self._llm = llm_client
        self._registry = tool_registry
        self._security = security
        self._memory = memory_manager
        self._bus = bus
        self._system_prompt_override = system_prompt_override
        self._prompt_guard = prompt_guard
        self._output_monitor = output_monitor
        self._input_sanitizer = input_sanitizer

        # Configuration.
        cfg = config or {}
        self._constitution: str = _get_cfg(cfg, "constitution", _DEFAULT_CONSTITUTION)
        self._restrictions: str = _get_cfg(cfg, "restrictions", _DEFAULT_RESTRICTIONS)
        self._purpose: str = _get_cfg(cfg, "purpose", _DEFAULT_PURPOSE)
        self._personality: str = _get_cfg(cfg, "personality", "")
        self._max_iterations: int = int(_get_cfg(cfg, "max_iterations", 10))

        # Conversation state (per-conversation) with LRU eviction.
        self._conversations: OrderedDict[str, list[ChatMessage]] = OrderedDict()

        # Security context to inject before the next LLM call (set by
        # process_tool_calls when a SecurityEngine provides context).
        self._pending_security_context: str | None = None

    # ------------------------------------------------------------------
    # Public API
    # ------------------------------------------------------------------

    async def run_once(
        self,
        user_message: str,
        conversation_id: str | None = None,
    ) -> str:
        """Process a single user turn through the full reasoning loop.

        Parameters
        ----------
        user_message:
            The user's text input.
        conversation_id:
            Optional conversation identifier for multi-turn sessions.
            A new id is generated if ``None``.

        Returns
        -------
        str
            The agent's final text response to the user.
        """
        conv_id = conversation_id or uuid.uuid4().hex[:16]
        history = self._get_or_create_history(conv_id)

        # Build system prompt (always position 0).
        system_msg = self._build_system_prompt()
        if history and history[0].role == Role.SYSTEM:
            history[0] = system_msg
        else:
            history.insert(0, system_msg)

        # --- Input sanitization pipeline ---
        processed_message = user_message

        # Layer 1: Sanitize input (injection pattern detection + structural cleanup).
        if self._input_sanitizer is not None:
            san_result = self._input_sanitizer.sanitize(processed_message, source="user")
            processed_message = san_result.clean_text
            if san_result.threats_detected:
                log.warning(
                    "Input sanitizer detected threats in conv=%s: %s",
                    conv_id, san_result.threats_detected,
                )

        # Layer 2: Wrap user input in boundary markers.
        if self._prompt_guard is not None:
            processed_message = self._prompt_guard.wrap_user_input(processed_message)

        # Append user message.
        history.append(ChatMessage(role=Role.USER, content=processed_message))

        # Optionally enrich with memory context.
        await self._inject_memory_context(history, user_message, conv_id)

        # Prune conversation if it's grown too large.
        self._prune_history(history)

        # Reasoning loop.
        tools_schema = self._registry.get_openai_tools()
        iteration = 0

        try:
            while iteration < self._max_iterations:
                iteration += 1
                log.debug("Agent loop iteration %d/%d (conv=%s)", iteration, self._max_iterations, conv_id)

                # Call the LLM.
                try:
                    response: LLMResponse = await self._llm.chat(
                        messages=history,
                        tools=tools_schema if tools_schema else None,
                    )
                except Exception:
                    log.error("LLM call failed on iteration %d", iteration, exc_info=True)
                    error_msg = "I encountered an error communicating with the language model. Please try again."
                    history.append(ChatMessage(role=Role.ASSISTANT, content=error_msg))
                    return error_msg

                # Case 1: LLM returned tool calls.
                if response.tool_calls:
                    # Append the assistant message with tool calls (content may be None).
                    assistant_msg = ChatMessage(
                        role=Role.ASSISTANT,
                        content=response.content or "",
                        tool_calls=response.tool_calls,
                    )
                    history.append(assistant_msg)

                    # Publish AGENT_ACTION.
                    await self._publish(
                        MessageType.AGENT_ACTION,
                        {
                            "conversation_id": conv_id,
                            "iteration": iteration,
                            "tool_calls": [
                                {"name": tc.function.name, "arguments": tc.function.arguments}
                                for tc in response.tool_calls
                            ],
                        },
                    )

                    # Execute tools.
                    tool_result_messages = await self.process_tool_calls(response.tool_calls)
                    history.extend(tool_result_messages)

                    # Publish AGENT_RESULT.
                    await self._publish(
                        MessageType.AGENT_RESULT,
                        {
                            "conversation_id": conv_id,
                            "iteration": iteration,
                            "results": [
                                {"tool_call_id": m.tool_call_id, "content": m.content}
                                for m in tool_result_messages
                            ],
                        },
                    )

                    # Inject security context if set by process_tool_calls.
                    if self._pending_security_context:
                        history.append(
                            ChatMessage(
                                role=Role.SYSTEM,
                                content=self._pending_security_context,
                            )
                        )
                        self._pending_security_context = None

                    # Continue the loop so the LLM can decide next action.
                    continue

                # Case 2: LLM returned a text response (no tool calls) -- done.
                final_text = response.content or ""

                # --- Output monitoring pipeline ---
                final_text = self._monitor_output(final_text)

                history.append(ChatMessage(role=Role.ASSISTANT, content=final_text))
                log.info("Agent loop complete after %d iteration(s) (conv=%s)", iteration, conv_id)
                return final_text

        finally:
            # Always clear pending security context to prevent leaking
            # to the next turn on error paths.
            self._pending_security_context = None

        # Exhausted iterations.
        exhaust_msg = (
            "I've reached the maximum number of reasoning steps for this turn. "
            "Here is what I have so far -- please let me know if you'd like "
            "me to continue."
        )
        history.append(ChatMessage(role=Role.ASSISTANT, content=exhaust_msg))
        return exhaust_msg

    async def process_tool_calls(
        self,
        tool_calls: list[ToolCall],
    ) -> list[ChatMessage]:
        """Execute a batch of tool calls sequentially.

        Each call is permission-checked and executed through the tool
        registry.  Results are returned as ``TOOL``-role messages
        suitable for appending to the conversation.

        Parameters
        ----------
        tool_calls:
            List of tool calls from the LLM response.

        Returns
        -------
        list[ChatMessage]
            One message per tool call containing the execution result.
        """
        results: list[ChatMessage] = []

        for tc in tool_calls:
            tool_name = tc.function.name
            raw_args = tc.function.arguments

            # Parse arguments.
            try:
                arguments: dict[str, Any] = json.loads(raw_args) if raw_args else {}
            except json.JSONDecodeError:
                log.warning("Failed to parse tool arguments: %s", raw_args)
                results.append(
                    ChatMessage(
                        role=Role.TOOL,
                        content=json.dumps({
                            "success": False,
                            "error": f"Invalid JSON in tool arguments: {raw_args}",
                        }),
                        tool_call_id=tc.id,
                    )
                )
                continue

            # Look up tool.
            tool = self._registry.get(tool_name)
            if tool is None:
                results.append(
                    ChatMessage(
                        role=Role.TOOL,
                        content=json.dumps({
                            "success": False,
                            "error": f"Unknown tool: {tool_name}",
                        }),
                        tool_call_id=tc.id,
                    )
                )
                continue

            # Permission check -- use full SecurityEngine pipeline when available.
            from meept.security.engine import SecurityEngine

            security_context: str | None = None
            if isinstance(self._security, SecurityEngine):
                from meept.agent.executor import _TOOL_ACTION_MAP

                action = _TOOL_ACTION_MAP.get(tool_name, tool_name)
                decision = await self._security.check(action, tool_name=tool_name, details=arguments)
                allowed = decision.allowed
                reason = decision.reason
                security_context = self._security.get_context_for_llm(decision, action, arguments)
            else:
                allowed, reason = await self._check_permission(tool_name, arguments)

            if not allowed:
                log.info("Tool %s blocked: %s", tool_name, reason)
                results.append(
                    ChatMessage(
                        role=Role.TOOL,
                        content=json.dumps({
                            "success": False,
                            "error": f"Permission denied: {reason}",
                        }),
                        tool_call_id=tc.id,
                    )
                )
                # Inject security context so the LLM understands the denial.
                if security_context:
                    self._pending_security_context = security_context
                continue

            # Execute.
            try:
                result = await tool.execute(**arguments)
            except Exception as exc:
                log.error("Tool %s raised: %s", tool_name, exc, exc_info=True)
                result = {"success": False, "error": str(exc)}

            # Serialise result to string for the message.
            try:
                content = json.dumps(result, default=str)
            except (TypeError, ValueError):
                content = str(result)

            # Monitor tool output for credential leaks.
            content = self._monitor_output(content)

            # Wrap tool output in boundary markers.
            if self._prompt_guard is not None:
                content = self._prompt_guard.wrap_tool_output(tool_name, content)

            results.append(
                ChatMessage(
                    role=Role.TOOL,
                    content=content,
                    tool_call_id=tc.id,
                )
            )

        return results

    async def handle_chat_request(self, message: BusMessage) -> None:
        """Bus handler for incoming chat requests.

        Expected payload keys:
            - ``text`` (str): the user message
            - ``conversation_id`` (str, optional): conversation identifier
            - ``reply_to`` (str, optional): message id to reply to

        Publishes a ``CHAT_RESPONSE`` message with the result.
        """
        text = message.payload.get("text", "")
        conv_id = message.payload.get("conversation_id")

        if not text:
            log.warning("Received empty chat request from %s", message.source)
            return

        log.info("Handling chat request from %s (conv=%s)", message.source, conv_id)

        response_text = await self.run_once(text, conversation_id=conv_id)

        await self._publish(
            MessageType.CHAT_RESPONSE,
            {
                "text": response_text,
                "conversation_id": conv_id,
            },
            reply_to=message.id,
        )

    # ------------------------------------------------------------------
    # Conversation management
    # ------------------------------------------------------------------

    def _get_or_create_history(self, conversation_id: str) -> list[ChatMessage]:
        """Return the message history for a conversation, creating if needed."""
        if conversation_id in self._conversations:
            # Move to end (most recently used).
            self._conversations.move_to_end(conversation_id)
        else:
            # Evict oldest conversation if at capacity.
            if len(self._conversations) >= _MAX_CONVERSATIONS:
                evicted_id, _ = self._conversations.popitem(last=False)
                log.debug("Evicted oldest conversation: %s", evicted_id)
            self._conversations[conversation_id] = []
        return self._conversations[conversation_id]

    @staticmethod
    def _prune_history(history: list[ChatMessage]) -> None:
        """Trim conversation history if it exceeds the maximum length.

        Preserves the system prompt (index 0) and the most recent messages.
        """
        if len(history) <= _MAX_MESSAGES_PER_CONVERSATION:
            return
        # Keep system prompt + last N messages.
        keep = _MAX_MESSAGES_PER_CONVERSATION - 1
        system_msg = history[0] if history and history[0].role == Role.SYSTEM else None
        recent = history[-keep:]
        history.clear()
        if system_msg is not None:
            history.append(system_msg)
        history.extend(recent)
        log.debug("Pruned conversation history to %d messages", len(history))

    def _monitor_output(self, content: str) -> str:
        """Run output through the OutputMonitor if configured.

        Returns the (possibly redacted) content.
        """
        if self._output_monitor is None:
            return content
        safe, issues = self._output_monitor.check_output(content)
        if not safe:
            log.warning("Output monitor flagged issues: %s", issues)
            content = self._output_monitor.redact_sensitive(content)
        return content

    def get_history(self, conversation_id: str) -> list[ChatMessage]:
        """Return a copy of the conversation history (public accessor)."""
        return list(self._conversations.get(conversation_id, []))

    def clear_history(self, conversation_id: str) -> None:
        """Discard conversation history for a given id."""
        self._conversations.pop(conversation_id, None)

    # ------------------------------------------------------------------
    # System prompt
    # ------------------------------------------------------------------

    def _build_system_prompt(self) -> ChatMessage:
        """Assemble the system prompt from configuration sections.

        If ``system_prompt_override`` was provided at construction time, it
        replaces the constitution/restrictions/purpose/personality sections
        while still appending the available-tools block.
        """
        sections: list[str] = []

        if self._system_prompt_override:
            sections.append(self._system_prompt_override)
        else:
            sections.append("# Constitution")
            sections.append(self._constitution)

            sections.append("\n# Safety Restrictions")
            sections.append(self._restrictions)

            sections.append("\n# Purpose & Task Principles")
            sections.append(self._purpose)

            if self._personality:
                sections.append("\n# Personality")
                sections.append(self._personality)

        # Advertise available tools.
        tool_defs = self._registry.list_tools()
        if tool_defs:
            sections.append("\n# Available Tools")
            for td in tool_defs:
                params = ", ".join(
                    f"{p.name}: {p.type}" + (" (optional)" if not p.required else "")
                    for p in td.parameters
                )
                sections.append(f"- **{td.name}**({params}): {td.description}")

        return ChatMessage(role=Role.SYSTEM, content="\n".join(sections))

    # ------------------------------------------------------------------
    # Permission bridge
    # ------------------------------------------------------------------

    async def _check_permission(
        self, tool_name: str, arguments: dict[str, Any],
    ) -> tuple[bool, str]:
        """Delegate permission check to the security manager."""
        if self._security is None:
            return True, "No security manager configured"

        # Map tool names to permission action categories.
        from meept.agent.executor import _TOOL_ACTION_MAP

        action = _TOOL_ACTION_MAP.get(tool_name, tool_name)

        check = getattr(self._security, "check_permission", None)
        if check is not None:
            import asyncio
            if asyncio.iscoroutinefunction(check):
                return await check(action, arguments)
            return check(action, arguments)

        # Fallback: if security object has a simpler check interface.
        check_simple = getattr(self._security, "check", None)
        if check_simple is not None:
            import asyncio
            if asyncio.iscoroutinefunction(check_simple):
                result = await check_simple(tool_name, arguments)
            else:
                result = check_simple(tool_name, arguments)
            if isinstance(result, tuple):
                return result
            return bool(result), "Permitted" if result else "Denied"

        return False, "Security manager has no check method -- denied by default"

    # ------------------------------------------------------------------
    # Memory integration
    # ------------------------------------------------------------------

    async def _inject_memory_context(
        self,
        history: list[ChatMessage],
        user_message: str,
        conversation_id: str,
    ) -> None:
        """Query the memory manager and inject relevant context.

        Inserts a system-level context message right after the main
        system prompt if relevant memories are found.
        """
        if self._memory is None:
            return

        try:
            query_fn = getattr(self._memory, "query", None)
            if query_fn is None:
                return

            import asyncio
            if asyncio.iscoroutinefunction(query_fn):
                memories = await query_fn(user_message, conversation_id=conversation_id)
            else:
                memories = query_fn(user_message, conversation_id=conversation_id)

            if not memories:
                return

            # Format memories as a context block.
            if isinstance(memories, list):
                context_lines = ["# Relevant Context from Memory"]
                for mem in memories:
                    if isinstance(mem, str):
                        context_lines.append(f"- {mem}")
                    elif isinstance(mem, dict):
                        context_lines.append(f"- {mem.get('content', mem)}")
                    else:
                        context_lines.append(f"- {mem}")
                context_text = "\n".join(context_lines)
            else:
                context_text = f"# Relevant Context from Memory\n{memories}"

            # Insert after the system prompt (position 1).
            context_msg = ChatMessage(role=Role.SYSTEM, content=context_text)

            # Remove any previous memory context message.
            history[:] = [
                m for m in history
                if not (m.role == Role.SYSTEM and m.content.startswith("# Relevant Context"))
            ]

            # Insert at position 1 (after main system prompt).
            if len(history) >= 1:
                history.insert(1, context_msg)
            else:
                history.append(context_msg)

        except Exception:
            log.debug("Memory query failed; proceeding without context", exc_info=True)

    # ------------------------------------------------------------------
    # Bus helpers
    # ------------------------------------------------------------------

    async def _publish(
        self,
        msg_type: MessageType,
        payload: dict[str, Any],
        reply_to: str | None = None,
    ) -> None:
        """Publish a message on the internal bus if available."""
        if self._bus is None:
            return

        import asyncio

        msg = BusMessage(
            type=msg_type,
            payload=payload,
            source="agent",
            reply_to=reply_to,
        )

        topic = f"agent.{msg_type.value}"

        publish = getattr(self._bus, "publish", None)
        if publish is not None:
            if asyncio.iscoroutinefunction(publish):
                await publish(topic, msg)
            else:
                publish(topic, msg)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _get_cfg(cfg: Any, key: str, default: str) -> str:
    """Extract a config value from a dict-like or object."""
    if isinstance(cfg, dict):
        return cfg.get(key, default)
    return getattr(cfg, key, default)
