"""Action execution pipeline with permission checks and output monitoring.

The :class:`ActionExecutor` sits between the agent loop and the tool
registry.  Every tool invocation flows through this class so that:

1. The tool is validated as registered.
2. Permissions are checked (path checks, risk gating, financial blocks).
3. The tool is executed.
4. The output is optionally monitored / sanitised.
5. A standardised result dict is returned.
"""

from __future__ import annotations

import asyncio
import json
import logging
from typing import Any

from meept.models.messages import BusMessage, MessageType
from meept.security.permissions import PermissionManager, RiskLevel
from meept.security.engine import SecurityEngine
from meept.tools.interface import ToolRegistry

log = logging.getLogger(__name__)

# Mapping from tool names to the permission-system action categories.
_TOOL_ACTION_MAP: dict[str, str] = {
    "shell": "shell_execute",
    "file_read": "file_read",
    "file_write": "file_write",
    "file_delete": "file_delete",
    "list_directory": "file_read",
    "web_search": "network_request",
    "web_fetch": "network_request",
}


class ActionExecutor:
    """Full execution pipeline for tool actions.

    Parameters
    ----------
    tool_registry:
        The registry of available tools.
    permission_manager:
        Evaluates whether actions are allowed.
    output_monitor:
        Optional callable that inspects tool output for safety.  If
        provided it receives the result dict and may return a modified
        version or raise to block the output.  ``None`` means no
        monitoring.
    bus:
        The internal message bus for publishing events.  May be ``None``
        during testing.
    """

    def __init__(
        self,
        tool_registry: ToolRegistry,
        permission_manager: PermissionManager | SecurityEngine,
        output_monitor: Any | None = None,
        bus: Any | None = None,
    ) -> None:
        self._registry = tool_registry
        self._pm = permission_manager
        self._monitor = output_monitor
        self._bus = bus

    async def execute(
        self,
        tool_name: str,
        arguments: dict[str, Any],
    ) -> dict[str, Any]:
        """Execute a tool action through the full safety pipeline.

        Steps:
            1. Validate the tool exists in the registry.
            2. Check permissions via :class:`PermissionManager`.
            3. Execute the tool.
            4. Run output through the monitor (if configured).
            5. Return a standardised result dict.

        Parameters
        ----------
        tool_name:
            Name of the tool to invoke.
        arguments:
            Keyword arguments for the tool's ``execute`` method.

        Returns
        -------
        dict
            ``{"success": bool, "result": Any, "error": str | None}``
        """
        # 1. Validate tool exists.
        tool = self._registry.get(tool_name)
        if tool is None:
            return {
                "success": False,
                "result": None,
                "error": f"Unknown tool: {tool_name}",
            }

        # 2. Permission check.
        action_category = _TOOL_ACTION_MAP.get(tool_name, tool_name)
        if isinstance(self._pm, SecurityEngine):
            allowed, reason = await self._pm.check_permission(action_category, arguments)
        else:
            allowed, reason = self._pm.check_permission(action_category, arguments)

        if not allowed:
            log.warning(
                "Permission denied for tool %s: %s", tool_name, reason,
            )
            return {
                "success": False,
                "result": None,
                "error": f"Permission denied: {reason}",
            }

        # 3. Execute with argument validation.
        log.info("Executing tool: %s (args=%s)", tool_name, _summarise_args(arguments))
        try:
            result = await tool.execute(**arguments)
        except TypeError as exc:
            log.warning("Tool %s received invalid arguments: %s", tool_name, exc)
            return {
                "success": False,
                "result": None,
                "error": f"Invalid arguments for tool '{tool_name}': {exc}",
            }
        except Exception as exc:
            log.error("Tool %s raised an exception: %s", tool_name, exc, exc_info=True)
            return {
                "success": False,
                "result": None,
                "error": f"Tool execution failed: {exc}",
            }

        # 4. Monitor output.
        if self._monitor is not None:
            try:
                result = await self._monitor(result) if asyncio.iscoroutinefunction(
                    self._monitor
                ) else self._monitor(result)
            except Exception as exc:
                log.warning("Output monitor blocked result: %s", exc)
                return {
                    "success": False,
                    "result": None,
                    "error": f"Output blocked by safety monitor: {exc}",
                }

        # 5. Normalise and return.
        return _normalise_result(result)

    async def execute_with_confirmation(
        self,
        tool_name: str,
        arguments: dict[str, Any],
    ) -> dict[str, Any]:
        """Execute a tool that requires user confirmation.

        For HIGH / CRITICAL risk actions, this method publishes a
        confirmation request on the bus and waits for the user to
        approve or deny before proceeding.

        Parameters
        ----------
        tool_name:
            Name of the tool to invoke.
        arguments:
            Keyword arguments for the tool.

        Returns
        -------
        dict
            ``{"success": bool, "result": Any, "error": str | None}``
        """
        tool = self._registry.get(tool_name)
        if tool is None:
            return {
                "success": False,
                "result": None,
                "error": f"Unknown tool: {tool_name}",
            }

        # Create a confirmation request.
        action_category = _TOOL_ACTION_MAP.get(tool_name, tool_name)
        request_id, future = self._pm.request_confirmation(
            action_category, arguments,
        )

        # Publish the confirmation request on the bus so frontends can
        # present it to the user.
        if self._bus is not None:
            await self._publish(
                MessageType.AGENT_ACTION,
                {
                    "type": "confirmation_request",
                    "request_id": request_id,
                    "tool": tool_name,
                    "arguments": arguments,
                    "message": (
                        f"Tool '{tool_name}' requires confirmation. "
                        f"Approve or deny request {request_id}."
                    ),
                },
            )

        # Wait for user response (with timeout).
        try:
            approved = await asyncio.wait_for(future, timeout=300.0)
        except asyncio.TimeoutError:
            log.warning("Confirmation timed out for %s", request_id)
            return {
                "success": False,
                "result": None,
                "error": "Confirmation timed out after 5 minutes",
            }

        if not approved:
            log.info("User denied action %s (%s)", tool_name, request_id)
            return {
                "success": False,
                "result": None,
                "error": "Action denied by user",
            }

        # User approved -- proceed with execution (bypass normal
        # permission check since we already have explicit approval).
        log.info("User approved action %s (%s), executing", tool_name, request_id)
        try:
            result = await tool.execute(**arguments)
        except Exception as exc:
            log.error("Tool %s raised: %s", tool_name, exc, exc_info=True)
            return {
                "success": False,
                "result": None,
                "error": f"Tool execution failed: {exc}",
            }

        if self._monitor is not None:
            try:
                result = await self._monitor(result) if asyncio.iscoroutinefunction(
                    self._monitor
                ) else self._monitor(result)
            except Exception as exc:
                return {
                    "success": False,
                    "result": None,
                    "error": f"Output blocked by safety monitor: {exc}",
                }

        return _normalise_result(result)

    # ------------------------------------------------------------------
    # Bus helpers
    # ------------------------------------------------------------------

    async def _publish(self, msg_type: MessageType, payload: dict[str, Any]) -> None:
        """Publish a message on the bus if available."""
        if self._bus is None:
            return

        topic = f"executor.{msg_type.value}"

        msg = BusMessage(
            type=msg_type,
            payload=payload,
            source="executor",
        )

        publish = getattr(self._bus, "publish", None)
        if publish is not None:
            if asyncio.iscoroutinefunction(publish):
                await publish(topic, msg)
            else:
                publish(topic, msg)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _normalise_result(result: dict[str, Any]) -> dict[str, Any]:
    """Ensure the result dict has the standard shape."""
    if not isinstance(result, dict):
        return {"success": True, "result": result, "error": None}
    # Ensure required keys exist.
    result.setdefault("success", True)
    result.setdefault("result", None)
    result.setdefault("error", None)
    return result


def _summarise_args(args: dict[str, Any], max_len: int = 200) -> str:
    """Return a truncated string representation of arguments for logging."""
    try:
        text = json.dumps(args, default=str)
    except (TypeError, ValueError):
        text = str(args)
    if len(text) > max_len:
        text = text[:max_len] + "..."
    return text
