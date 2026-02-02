"""MCP (Model Context Protocol) server lifecycle management.

Manages MCP server subprocesses, communicates via stdio JSON-RPC, and
exposes the tools each server provides in OpenAI function-calling format.

The ``mcp`` SDK is an optional dependency.  When absent, ``McpManager``
falls back to a raw JSON-RPC-over-stdio implementation so that basic
tool discovery and invocation still work without the SDK installed.
"""

from __future__ import annotations

import asyncio
import json
import logging
import os
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

log = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Conditional MCP SDK import
# ---------------------------------------------------------------------------

try:
    from mcp import ClientSession, StdioServerParameters
    from mcp.client.stdio import stdio_client

    _MCP_SDK_AVAILABLE = True
except ImportError:
    _MCP_SDK_AVAILABLE = False
    log.debug(
        "mcp SDK not installed. Install with: pip install 'meept[mcp]' "
        "or pip install 'mcp>=1.25'  -- falling back to raw JSON-RPC."
    )


# ---------------------------------------------------------------------------
# Data models
# ---------------------------------------------------------------------------


@dataclass(slots=True)
class McpServerConfig:
    """Configuration for a single MCP server.

    The format follows the `opencode <https://opencode.ai/docs/mcp-servers/>`_
    convention.  Servers are either ``"local"`` (subprocess over stdio) or
    ``"remote"`` (HTTP/SSE endpoint).

    Parameters
    ----------
    name:
        Human-readable server identifier (must be unique).
    type:
        ``"local"`` for a subprocess-based server, ``"remote"`` for an
        HTTP/SSE endpoint.
    command:
        (local only) List whose first element is the executable and
        remaining elements are arguments, e.g. ``["npx", "-y", "pkg"]``.
    environment:
        Extra environment variables injected into the subprocess (local)
        or ignored (remote).
    enabled:
        Whether the server should be started when the manager boots.
    timeout:
        Per-request timeout in milliseconds.  ``None`` means use the
        default (5 000 ms for local, 30 000 ms for remote).
    url:
        (remote only) URL of the remote MCP server.
    headers:
        (remote only) Extra HTTP headers sent with every request.
    """

    name: str
    type: str = "local"
    command: list[str] = field(default_factory=list)
    environment: dict[str, str] = field(default_factory=dict)
    enabled: bool = True
    timeout: int | None = None
    url: str | None = None
    headers: dict[str, str] = field(default_factory=dict)


# ---------------------------------------------------------------------------
# Internal: state for a running server
# ---------------------------------------------------------------------------


@dataclass
class _RunningServer:
    """Bookkeeping for a single running MCP server subprocess."""

    config: McpServerConfig
    process: asyncio.subprocess.Process | None = None
    tools: list[dict[str, Any]] = field(default_factory=list)

    # SDK-based session objects (only populated when the SDK is available).
    session: Any = None  # mcp.ClientSession | None
    _stdio_context: Any = None  # context manager returned by stdio_client
    _session_context: Any = None  # context manager for the ClientSession

    # Raw JSON-RPC bookkeeping (used when the SDK is *not* available).
    _request_id: int = 0

    @property
    def running(self) -> bool:
        if self.process is None:
            return False
        return self.process.returncode is None

    @property
    def tool_count(self) -> int:
        return len(self.tools)


# ---------------------------------------------------------------------------
# McpManager
# ---------------------------------------------------------------------------


class McpManager:
    """Lifecycle manager for MCP server subprocesses.

    Parameters
    ----------
    config_path:
        Path to ``mcp_servers.json``.
    sanitizer:
        Optional :class:`~meept.security.sanitizer.InputSanitizer` used to
        scrub server responses before they reach the LLM.
    """

    def __init__(self, config_path: Path, sanitizer: Any | None = None) -> None:
        self._config_path = Path(config_path)
        self._sanitizer = sanitizer
        self._configs: dict[str, McpServerConfig] = {}
        self._servers: dict[str, _RunningServer] = {}
        self._load_config()

    # ------------------------------------------------------------------
    # Configuration
    # ------------------------------------------------------------------

    def _load_config(self) -> None:
        """Parse ``mcp_servers.json`` into :class:`McpServerConfig` instances.

        The JSON file uses the opencode convention::

            {
              "mcp": {
                "my-server": {
                  "type": "local",
                  "command": ["npx", "-y", "my-mcp-command"],
                  "environment": {"KEY": "value"},
                  "enabled": true,
                  "timeout": 5000
                }
              }
            }
        """
        if not self._config_path.exists():
            log.warning("MCP config not found at %s -- no servers configured", self._config_path)
            return

        try:
            raw = json.loads(self._config_path.read_text(encoding="utf-8"))
        except (json.JSONDecodeError, OSError) as exc:
            log.error("Failed to read MCP config %s: %s", self._config_path, exc)
            return

        servers_raw: dict[str, Any] = raw.get("mcp", {})
        for name, entry in servers_raw.items():
            if not isinstance(entry, dict):
                log.warning("MCP config: skipping malformed entry %r", name)
                continue

            server_type = entry.get("type", "local")

            if server_type == "remote":
                url = entry.get("url")
                if not url:
                    log.warning("MCP config: remote server %r has no 'url' -- skipping", name)
                    continue
                self._configs[name] = McpServerConfig(
                    name=name,
                    type="remote",
                    url=url,
                    headers=entry.get("headers", {}),
                    enabled=entry.get("enabled", True),
                    timeout=entry.get("timeout"),
                )
            else:
                command = entry.get("command")
                if not command or not isinstance(command, list) or len(command) == 0:
                    log.warning(
                        "MCP config: local server %r has no 'command' array -- skipping",
                        name,
                    )
                    continue
                self._configs[name] = McpServerConfig(
                    name=name,
                    type="local",
                    command=command,
                    environment=entry.get("environment", {}),
                    enabled=entry.get("enabled", True),
                    timeout=entry.get("timeout"),
                )

        log.info(
            "MCP config: loaded %d server definition(s) from %s",
            len(self._configs),
            self._config_path,
        )

    # ------------------------------------------------------------------
    # Server lifecycle
    # ------------------------------------------------------------------

    async def start_server(self, name: str) -> bool:
        """Start the MCP server identified by *name*.

        Returns ``True`` if the server was started (or was already running),
        ``False`` on failure.
        """
        if name in self._servers and self._servers[name].running:
            log.info("MCP server %r is already running", name)
            return True

        cfg = self._configs.get(name)
        if cfg is None:
            log.error("MCP server %r not found in configuration", name)
            return False

        if not cfg.enabled:
            log.info("MCP server %r is disabled in configuration -- skipping", name)
            return False

        if cfg.type == "remote":
            log.warning(
                "MCP server %r is remote (url=%s) -- remote transport not yet implemented",
                name,
                cfg.url,
            )
            return False

        log.info("Starting MCP server %r: %s", name, " ".join(cfg.command))

        if _MCP_SDK_AVAILABLE:
            return await self._start_server_sdk(cfg)
        return await self._start_server_raw(cfg)

    async def _start_server_sdk(self, cfg: McpServerConfig) -> bool:
        """Start server using the official MCP SDK."""
        try:
            env = {**os.environ, **cfg.environment} if cfg.environment else None
            params = StdioServerParameters(
                command=cfg.command[0],
                args=cfg.command[1:],
                env=env,
            )

            running = _RunningServer(config=cfg)

            # Enter the stdio_client context manager.
            running._stdio_context = stdio_client(params)
            read_stream, write_stream = await running._stdio_context.__aenter__()

            # Enter the ClientSession context manager.
            running._session_context = ClientSession(read_stream, write_stream)
            session: ClientSession = await running._session_context.__aenter__()
            running.session = session

            # Initialise the session (MCP handshake).
            await session.initialize()

            # Discover tools.
            tools_result = await session.list_tools()
            running.tools = self._convert_sdk_tools(cfg.name, tools_result.tools)

            self._servers[cfg.name] = running
            log.info(
                "MCP server %r started (SDK mode) -- %d tool(s) available",
                cfg.name,
                running.tool_count,
            )
            return True

        except Exception:
            log.exception("Failed to start MCP server %r (SDK mode)", cfg.name)
            return False

    async def _start_server_raw(self, cfg: McpServerConfig) -> bool:
        """Start server via raw subprocess + JSON-RPC over stdio."""
        try:
            env = {**os.environ, **cfg.environment} if cfg.environment else None
            process = await asyncio.create_subprocess_exec(
                cfg.command[0],
                *cfg.command[1:],
                stdin=asyncio.subprocess.PIPE,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
                env=env,
            )

            running = _RunningServer(config=cfg, process=process)

            # Perform JSON-RPC initialize handshake.
            init_resp = await self._jsonrpc_request(running, "initialize", {
                "protocolVersion": "2024-11-05",
                "capabilities": {},
                "clientInfo": {"name": "meept", "version": "0.1.0"},
            })

            if init_resp is None:
                log.error("MCP server %r did not respond to initialize", cfg.name)
                await self._terminate_process(process)
                return False

            # Send initialized notification.
            await self._jsonrpc_notify(running, "notifications/initialized", {})

            # Discover tools.
            tools_resp = await self._jsonrpc_request(running, "tools/list", {})
            if tools_resp is not None:
                raw_tools = tools_resp.get("tools", [])
                running.tools = self._convert_raw_tools(cfg.name, raw_tools)

            self._servers[cfg.name] = running
            log.info(
                "MCP server %r started (raw JSON-RPC) -- %d tool(s) available",
                cfg.name,
                running.tool_count,
            )
            return True

        except Exception:
            log.exception("Failed to start MCP server %r (raw JSON-RPC)", cfg.name)
            return False

    async def stop_server(self, name: str) -> None:
        """Stop the MCP server identified by *name*."""
        running = self._servers.pop(name, None)
        if running is None:
            log.debug("MCP server %r is not running -- nothing to stop", name)
            return

        log.info("Stopping MCP server %r", name)

        if _MCP_SDK_AVAILABLE and running.session is not None:
            # Tear down SDK context managers in reverse order.
            try:
                if running._session_context is not None:
                    await running._session_context.__aexit__(None, None, None)
            except Exception:
                log.debug("Error closing MCP session for %r", name, exc_info=True)
            try:
                if running._stdio_context is not None:
                    await running._stdio_context.__aexit__(None, None, None)
            except Exception:
                log.debug("Error closing MCP stdio for %r", name, exc_info=True)
        elif running.process is not None:
            await self._terminate_process(running.process)

    async def stop_all(self) -> None:
        """Stop every running MCP server."""
        names = list(self._servers.keys())
        for name in names:
            await self.stop_server(name)
        log.info("All MCP servers stopped")

    # ------------------------------------------------------------------
    # Tool discovery
    # ------------------------------------------------------------------

    async def list_servers(self) -> list[dict[str, Any]]:
        """Return status information for every configured server.

        Each entry contains:

        * ``name`` -- server identifier
        * ``type`` -- ``"local"`` or ``"remote"``
        * ``running`` -- whether the subprocess is alive
        * ``tool_count`` -- number of tools exposed
        * ``enabled`` -- whether the config entry is enabled
        """
        result: list[dict[str, Any]] = []
        for name, cfg in self._configs.items():
            running = self._servers.get(name)
            result.append({
                "name": name,
                "type": cfg.type,
                "running": running.running if running else False,
                "tool_count": running.tool_count if running else 0,
                "enabled": cfg.enabled,
            })
        return result

    async def get_tools(self, server_name: str) -> list[dict[str, Any]]:
        """Return the tool list for *server_name* in OpenAI function-calling schema.

        Returns an empty list if the server is not running.
        """
        running = self._servers.get(server_name)
        if running is None or not running.running:
            return []
        return list(running.tools)

    async def get_all_tools(self) -> list[dict[str, Any]]:
        """Aggregate tools from every running server in OpenAI schema format."""
        tools: list[dict[str, Any]] = []
        for running in self._servers.values():
            if running.running:
                tools.extend(running.tools)
        return tools

    # ------------------------------------------------------------------
    # Tool invocation (used by McpClient)
    # ------------------------------------------------------------------

    async def invoke_tool(
        self,
        server_name: str,
        tool_name: str,
        arguments: dict[str, Any],
        *,
        timeout: float = 30.0,
    ) -> dict[str, Any]:
        """Invoke *tool_name* on the server *server_name*.

        Returns the raw result dictionary from the server.

        If the server has a ``timeout`` configured (in milliseconds), that
        value takes precedence over the *timeout* parameter.

        Raises
        ------
        ValueError
            If the server is not running.
        TimeoutError
            If the call exceeds *timeout* seconds.
        """
        running = self._servers.get(server_name)
        if running is None or not running.running:
            raise ValueError(f"MCP server {server_name!r} is not running")

        # Honour per-server timeout (stored in milliseconds, convert to seconds).
        cfg = running.config
        if cfg.timeout is not None:
            timeout = cfg.timeout / 1000.0

        if _MCP_SDK_AVAILABLE and running.session is not None:
            return await self._invoke_tool_sdk(running, tool_name, arguments, timeout=timeout)
        return await self._invoke_tool_raw(running, tool_name, arguments, timeout=timeout)

    async def _invoke_tool_sdk(
        self,
        running: _RunningServer,
        tool_name: str,
        arguments: dict[str, Any],
        *,
        timeout: float,
    ) -> dict[str, Any]:
        """Call a tool via the MCP SDK session."""
        result = await asyncio.wait_for(
            running.session.call_tool(tool_name, arguments=arguments),
            timeout=timeout,
        )
        # The SDK returns a CallToolResult with a .content list.
        # Flatten text content into a single string for simplicity.
        content_parts: list[str] = []
        for block in result.content:
            if hasattr(block, "text"):
                content_parts.append(block.text)
            elif hasattr(block, "data"):
                content_parts.append(str(block.data))

        text = "\n".join(content_parts) if content_parts else ""
        is_error = getattr(result, "isError", False)
        return {"text": text, "is_error": is_error}

    async def _invoke_tool_raw(
        self,
        running: _RunningServer,
        tool_name: str,
        arguments: dict[str, Any],
        *,
        timeout: float,
    ) -> dict[str, Any]:
        """Call a tool via raw JSON-RPC."""
        resp = await asyncio.wait_for(
            self._jsonrpc_request(running, "tools/call", {
                "name": tool_name,
                "arguments": arguments,
            }),
            timeout=timeout,
        )
        if resp is None:
            return {"text": "", "is_error": True}

        content = resp.get("content", [])
        parts: list[str] = []
        for block in content:
            if isinstance(block, dict) and block.get("type") == "text":
                parts.append(block.get("text", ""))
        text = "\n".join(parts) if parts else json.dumps(resp)
        is_error = resp.get("isError", False)
        return {"text": text, "is_error": is_error}

    # ------------------------------------------------------------------
    # JSON-RPC helpers (raw mode)
    # ------------------------------------------------------------------

    async def _jsonrpc_request(
        self,
        running: _RunningServer,
        method: str,
        params: dict[str, Any],
    ) -> dict[str, Any] | None:
        """Send a JSON-RPC request and wait for the response."""
        if running.process is None or running.process.stdin is None or running.process.stdout is None:
            return None

        running._request_id += 1
        request = {
            "jsonrpc": "2.0",
            "id": running._request_id,
            "method": method,
            "params": params,
        }
        payload = json.dumps(request) + "\n"

        try:
            running.process.stdin.write(payload.encode())
            await running.process.stdin.drain()

            line = await asyncio.wait_for(running.process.stdout.readline(), timeout=30.0)
            if not line:
                return None

            response = json.loads(line.decode())
            if "error" in response:
                log.warning(
                    "MCP JSON-RPC error from %r method=%s: %s",
                    running.config.name,
                    method,
                    response["error"],
                )
                return None
            return response.get("result")

        except (asyncio.TimeoutError, OSError, json.JSONDecodeError) as exc:
            log.warning(
                "MCP JSON-RPC communication error with %r: %s", running.config.name, exc
            )
            return None

    async def _jsonrpc_notify(
        self,
        running: _RunningServer,
        method: str,
        params: dict[str, Any],
    ) -> None:
        """Send a JSON-RPC notification (no response expected)."""
        if running.process is None or running.process.stdin is None:
            return

        notification = {
            "jsonrpc": "2.0",
            "method": method,
            "params": params,
        }
        payload = json.dumps(notification) + "\n"
        try:
            running.process.stdin.write(payload.encode())
            await running.process.stdin.drain()
        except OSError as exc:
            log.debug("Failed to send notification to %r: %s", running.config.name, exc)

    # ------------------------------------------------------------------
    # Process management helpers
    # ------------------------------------------------------------------

    @staticmethod
    async def _terminate_process(process: asyncio.subprocess.Process) -> None:
        """Gracefully terminate a subprocess, escalating to kill if needed."""
        if process.returncode is not None:
            return
        try:
            process.terminate()
            try:
                await asyncio.wait_for(process.wait(), timeout=5.0)
            except asyncio.TimeoutError:
                log.warning("MCP subprocess did not exit after SIGTERM -- sending SIGKILL")
                process.kill()
                await process.wait()
        except ProcessLookupError:
            pass

    # ------------------------------------------------------------------
    # Tool schema conversion
    # ------------------------------------------------------------------

    def _convert_sdk_tools(self, server_name: str, tools: Any) -> list[dict[str, Any]]:
        """Convert MCP SDK tool objects to OpenAI function-calling schema.

        Each tool name is prefixed with ``<server_name>.`` to avoid
        collisions across servers.
        """
        result: list[dict[str, Any]] = []
        for tool in tools:
            prefixed_name = f"{server_name}.{tool.name}"
            schema: dict[str, Any] = {
                "type": "function",
                "function": {
                    "name": prefixed_name,
                    "description": tool.description or "",
                    "parameters": tool.inputSchema if tool.inputSchema else {
                        "type": "object",
                        "properties": {},
                    },
                },
            }
            result.append(schema)
        return result

    def _convert_raw_tools(
        self, server_name: str, raw_tools: list[dict[str, Any]]
    ) -> list[dict[str, Any]]:
        """Convert raw JSON tool definitions to OpenAI function-calling schema."""
        result: list[dict[str, Any]] = []
        for tool in raw_tools:
            name = tool.get("name", "unknown")
            prefixed_name = f"{server_name}.{name}"
            input_schema = tool.get("inputSchema", {"type": "object", "properties": {}})
            schema: dict[str, Any] = {
                "type": "function",
                "function": {
                    "name": prefixed_name,
                    "description": tool.get("description", ""),
                    "parameters": input_schema,
                },
            }
            result.append(schema)
        return result
