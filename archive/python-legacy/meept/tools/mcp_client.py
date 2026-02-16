"""MCP tool-call routing client.

Provides a high-level interface for invoking tools hosted by MCP servers.
Tool calls are routed through :class:`~meept.tools.mcp_manager.McpManager`
and optionally sanitized before being returned to the caller.
"""

from __future__ import annotations

import asyncio
import logging
from typing import Any

log = logging.getLogger(__name__)

# Re-export for type-checking convenience.
from meept.tools.mcp_manager import McpManager  # noqa: E402


class McpClient:
    """Route tool calls to the appropriate MCP server.

    Parameters
    ----------
    mcp_manager:
        The :class:`McpManager` that owns the running server subprocesses.
    sanitizer:
        Optional :class:`~meept.security.sanitizer.InputSanitizer`.  When
        provided, every textual value in the tool result is passed through
        :meth:`~InputSanitizer.sanitize` before being returned.
    timeout:
        Default per-call timeout in seconds.
    """

    DEFAULT_TIMEOUT: float = 30.0

    def __init__(
        self,
        mcp_manager: McpManager,
        sanitizer: Any | None = None,
        *,
        timeout: float = DEFAULT_TIMEOUT,
    ) -> None:
        self._manager = mcp_manager
        self._sanitizer = sanitizer
        self._timeout = timeout

    # ------------------------------------------------------------------
    # Public API
    # ------------------------------------------------------------------

    async def call_tool(
        self,
        server_name: str,
        tool_name: str,
        arguments: dict[str, Any],
        *,
        timeout: float | None = None,
    ) -> dict[str, Any]:
        """Invoke *tool_name* on *server_name* with the given *arguments*.

        Returns a standardised envelope::

            {
                "success": True,
                "result": <tool output>,
                "error": None
            }

        On failure the ``success`` field is ``False`` and ``error`` contains
        a human-readable message.
        """
        effective_timeout = timeout if timeout is not None else self._timeout

        try:
            raw = await self._manager.invoke_tool(
                server_name,
                tool_name,
                arguments,
                timeout=effective_timeout,
            )

            is_error = raw.get("is_error", False)
            text = raw.get("text", "")
            sanitized_text = self._sanitize_text(text)

            if is_error:
                return self._error_response(
                    f"Tool {server_name}.{tool_name} returned an error: {sanitized_text}"
                )

            return self._success_response(sanitized_text)

        except asyncio.TimeoutError:
            msg = (
                f"Tool call {server_name}.{tool_name} timed out "
                f"after {effective_timeout}s"
            )
            log.warning(msg)
            return self._error_response(msg)

        except ValueError as exc:
            # Raised by McpManager when the server is not running.
            log.warning("Tool call routing error: %s", exc)
            return self._error_response(str(exc))

        except Exception as exc:
            log.exception(
                "Unexpected error calling tool %s.%s", server_name, tool_name
            )
            return self._error_response(f"Internal error: {type(exc).__name__}: {exc}")

    async def call_tool_by_name(
        self,
        full_name: str,
        arguments: dict[str, Any],
        *,
        timeout: float | None = None,
    ) -> dict[str, Any]:
        """Invoke a tool using its fully-qualified ``"server.tool_name"`` form.

        The *full_name* is split on the **first** dot to separate the server
        name from the tool name.  This allows tool names that themselves
        contain dots (e.g. ``"myserver.namespace.action"`` maps to server
        ``"myserver"`` and tool ``"namespace.action"``).
        """
        server_name, tool_name = self._parse_full_name(full_name)
        return await self.call_tool(
            server_name, tool_name, arguments, timeout=timeout
        )

    # ------------------------------------------------------------------
    # Helpers
    # ------------------------------------------------------------------

    @staticmethod
    def _parse_full_name(full_name: str) -> tuple[str, str]:
        """Split ``"server.tool_name"`` into ``(server, tool_name)``.

        Raises
        ------
        ValueError
            If *full_name* does not contain a dot separator.
        """
        if "." not in full_name:
            raise ValueError(
                f"Invalid tool name {full_name!r} -- expected "
                f"'server_name.tool_name' format"
            )
        server_name, _, tool_name = full_name.partition(".")
        if not server_name or not tool_name:
            raise ValueError(
                f"Invalid tool name {full_name!r} -- both server name and "
                f"tool name must be non-empty"
            )
        return server_name, tool_name

    def _sanitize_text(self, text: str) -> str:
        """Pass *text* through the sanitizer if one is configured."""
        if self._sanitizer is None or not text:
            return text
        result = self._sanitizer.sanitize(text, source="mcp_tool_output")
        return result.clean_text

    @staticmethod
    def _success_response(result: Any) -> dict[str, Any]:
        """Build a standardised success envelope."""
        return {
            "success": True,
            "result": result,
            "error": None,
        }

    @staticmethod
    def _error_response(message: str) -> dict[str, Any]:
        """Build a standardised error envelope."""
        return {
            "success": False,
            "result": None,
            "error": message,
        }
