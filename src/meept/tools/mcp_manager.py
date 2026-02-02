"""MCP (Model Context Protocol) server lifecycle management.

Manages MCP server subprocesses and remote HTTP/WebSocket connections,
communicates via stdio JSON-RPC or Streamable HTTP, and exposes the
tools each server provides in OpenAI function-calling format.

The ``mcp`` SDK is an optional dependency.  When absent, ``McpManager``
falls back to a raw JSON-RPC implementation so that basic tool discovery
and invocation still work without the SDK installed.  Remote servers
use Streamable HTTP (SDK) or raw ``httpx`` POST (no SDK).  WebSocket
transport requires the SDK.
"""

from __future__ import annotations

import asyncio
import json
import logging
import os
import random
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

import httpx

log = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Conditional MCP SDK import
# ---------------------------------------------------------------------------

try:
    from mcp import ClientSession, StdioServerParameters
    from mcp.client.stdio import stdio_client
    from mcp.client.streamable_http import streamable_http_client
    from mcp.client.websocket import websocket_client as mcp_websocket_client

    _MCP_SDK_AVAILABLE = True
except ImportError:
    _MCP_SDK_AVAILABLE = False
    log.debug(
        "mcp SDK not installed. Install with: pip install 'meept[mcp]' "
        "or pip install 'mcp>=1.25'  -- falling back to raw JSON-RPC."
    )

try:
    from httpx_sse import EventSource

    _HTTPX_SSE_AVAILABLE = True
except ImportError:
    _HTTPX_SSE_AVAILABLE = False

# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------

_DEFAULT_LOCAL_TIMEOUT_S = 5.0
_DEFAULT_REMOTE_TIMEOUT_S = 30.0
_DEFAULT_SSE_READ_TIMEOUT_S = 300.0
_RECONNECT_INITIAL_DELAY_S = 1.0
_RECONNECT_MAX_DELAY_S = 30.0
_RECONNECT_MAX_RETRIES = 5
_RECONNECT_JITTER_RATIO = 0.25

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
    oauth:
        (remote only) OAuth configuration dict, ``False`` to explicitly
        disable, or ``None`` for default (no auth unless headers present).
    """

    name: str
    type: str = "local"
    command: list[str] = field(default_factory=list)
    environment: dict[str, str] = field(default_factory=dict)
    enabled: bool = True
    timeout: int | None = None
    url: str | None = None
    headers: dict[str, str] = field(default_factory=dict)
    oauth: dict[str, Any] | bool | None = None


# ---------------------------------------------------------------------------
# Internal: state for a running server
# ---------------------------------------------------------------------------


@dataclass
class _RunningServer:
    """Bookkeeping for a single running MCP server."""

    config: McpServerConfig
    process: asyncio.subprocess.Process | None = None
    tools: list[dict[str, Any]] = field(default_factory=list)

    # SDK-based session objects (only populated when the SDK is available).
    session: Any = None  # mcp.ClientSession | None
    _stdio_context: Any = None  # context manager returned by stdio_client
    _session_context: Any = None  # context manager for the ClientSession

    # Remote transport contexts (SDK mode).
    _http_transport_context: Any = None  # streamable_http_client context
    _ws_context: Any = None  # websocket_client context

    # HTTP client (both SDK auth and raw mode).
    _http_client: Any = None  # httpx.AsyncClient | None

    # Remote raw mode bookkeeping.
    _remote_connected: bool = False
    _remote_session_id: str | None = None
    _last_event_id: str | None = None
    _get_session_id: Any = None  # callback from SDK streamable HTTP

    # Background tasks.
    _reconnect_task: asyncio.Task | None = None  # SSE listener / reconnect

    # Raw JSON-RPC bookkeeping (used when the SDK is *not* available).
    _request_id: int = 0

    @property
    def is_remote(self) -> bool:
        return self.config.type == "remote"

    @property
    def running(self) -> bool:
        # Remote + SDK: session is active.
        if self.is_remote and self.session is not None:
            return True
        # Remote + raw: connected flag.
        if self.is_remote and self._remote_connected:
            return True
        # Local: subprocess is alive.
        if self.process is not None:
            return self.process.returncode is None
        return False

    @property
    def tool_count(self) -> int:
        return len(self.tools)


# ---------------------------------------------------------------------------
# McpManager
# ---------------------------------------------------------------------------


class McpManager:
    """Lifecycle manager for MCP servers (local and remote).

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

                # Parse oauth: dict, False, or None (absent).
                oauth_raw = entry.get("oauth")
                if isinstance(oauth_raw, dict):
                    oauth: dict[str, Any] | bool | None = oauth_raw
                elif oauth_raw is False:
                    oauth = False
                else:
                    oauth = None

                self._configs[name] = McpServerConfig(
                    name=name,
                    type="remote",
                    url=url,
                    headers=entry.get("headers", {}),
                    enabled=entry.get("enabled", True),
                    timeout=entry.get("timeout"),
                    oauth=oauth,
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
            if not cfg.url:
                log.error("MCP server %r has no URL configured", name)
                return False
            # Auto-detect WebSocket from URL scheme.
            if cfg.url.startswith(("ws://", "wss://")):
                return await self._start_websocket_server(cfg)
            if _MCP_SDK_AVAILABLE:
                return await self._start_remote_server_sdk(cfg)
            return await self._start_remote_server_raw(cfg)

        log.info("Starting MCP server %r: %s", name, " ".join(cfg.command))

        if _MCP_SDK_AVAILABLE:
            return await self._start_server_sdk(cfg)
        return await self._start_server_raw(cfg)

    # -- Local server start methods ------------------------------------------

    async def _start_server_sdk(self, cfg: McpServerConfig) -> bool:
        """Start local server using the official MCP SDK."""
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
        """Start local server via raw subprocess + JSON-RPC over stdio."""
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

    # -- Remote server start methods -----------------------------------------

    async def _start_remote_server_sdk(self, cfg: McpServerConfig) -> bool:
        """Start a remote HTTP server using the MCP SDK's streamable HTTP transport."""
        http_client: httpx.AsyncClient | None = None
        try:
            timeout_s = (cfg.timeout / 1000.0) if cfg.timeout else _DEFAULT_REMOTE_TIMEOUT_S

            # Build OAuth auth if configured.
            auth = None
            if isinstance(cfg.oauth, dict):
                auth = self._build_oauth_auth(cfg)

            http_client = httpx.AsyncClient(
                headers=cfg.headers or {},
                timeout=httpx.Timeout(timeout_s, read=_DEFAULT_SSE_READ_TIMEOUT_S),
                follow_redirects=True,
                auth=auth,
            )

            running = _RunningServer(config=cfg, _http_client=http_client)

            log.info(
                "Connecting to remote MCP server %r at %s (SDK streamable HTTP)",
                cfg.name, cfg.url,
            )

            # Enter the streamable HTTP context.
            running._http_transport_context = streamable_http_client(
                url=cfg.url,
                http_client=http_client,
            )
            read_stream, write_stream, get_session_id = (
                await running._http_transport_context.__aenter__()
            )
            running._get_session_id = get_session_id

            # Create and initialise the MCP session.
            running._session_context = ClientSession(read_stream, write_stream)
            session = await running._session_context.__aenter__()
            running.session = session

            await session.initialize()

            # Discover tools.
            tools_result = await session.list_tools()
            running.tools = self._convert_sdk_tools(cfg.name, tools_result.tools)

            self._servers[cfg.name] = running
            log.info(
                "MCP server %r connected (SDK streamable HTTP) -- %d tool(s) available",
                cfg.name,
                running.tool_count,
            )
            return True

        except Exception:
            log.exception("Failed to connect to remote MCP server %r (SDK)", cfg.name)
            if http_client is not None:
                await http_client.aclose()
            return False

    async def _start_remote_server_raw(self, cfg: McpServerConfig) -> bool:
        """Start a remote HTTP server using raw httpx POST (no SDK)."""
        http_client: httpx.AsyncClient | None = None
        try:
            if isinstance(cfg.oauth, dict):
                log.warning(
                    "MCP server %r: OAuth requires the MCP SDK. "
                    "Install with: pip install 'meept[mcp]'  -- "
                    "falling back to header-based auth only",
                    cfg.name,
                )

            timeout_s = (cfg.timeout / 1000.0) if cfg.timeout else _DEFAULT_REMOTE_TIMEOUT_S
            http_client = httpx.AsyncClient(
                headers=cfg.headers or {},
                timeout=httpx.Timeout(timeout_s, read=_DEFAULT_SSE_READ_TIMEOUT_S),
                follow_redirects=True,
            )

            running = _RunningServer(config=cfg, _http_client=http_client)

            log.info("Connecting to remote MCP server %r at %s (raw HTTP)", cfg.name, cfg.url)

            # JSON-RPC initialize.
            init_resp = await self._http_jsonrpc_request(running, "initialize", {
                "protocolVersion": "2024-11-05",
                "capabilities": {},
                "clientInfo": {"name": "meept", "version": "0.1.0"},
            })

            if init_resp is None:
                log.error("Remote MCP server %r did not respond to initialize", cfg.name)
                await http_client.aclose()
                return False

            # Send initialized notification.
            await self._http_jsonrpc_notify(running, "notifications/initialized", {})

            # Discover tools.
            tools_resp = await self._http_jsonrpc_request(running, "tools/list", {})
            if tools_resp is not None:
                raw_tools = tools_resp.get("tools", [])
                running.tools = self._convert_raw_tools(cfg.name, raw_tools)

            running._remote_connected = True
            self._servers[cfg.name] = running

            # Start SSE listener for server-initiated requests.
            running._reconnect_task = asyncio.create_task(
                self._sse_listener(running),
                name=f"mcp-sse-{cfg.name}",
            )

            log.info(
                "MCP server %r connected (raw HTTP) -- %d tool(s) available",
                cfg.name,
                running.tool_count,
            )
            return True

        except Exception:
            log.exception("Failed to connect to remote MCP server %r (raw HTTP)", cfg.name)
            if http_client is not None:
                await http_client.aclose()
            return False

    async def _start_websocket_server(self, cfg: McpServerConfig) -> bool:
        """Start a remote WebSocket server (requires the MCP SDK)."""
        if not _MCP_SDK_AVAILABLE:
            log.error(
                "MCP server %r: WebSocket transport requires the MCP SDK. "
                "Install with: pip install 'meept[mcp]'",
                cfg.name,
            )
            return False

        try:
            log.info("Connecting to remote MCP server %r at %s (WebSocket)", cfg.name, cfg.url)

            running = _RunningServer(config=cfg)

            # Enter the WebSocket context.
            running._ws_context = mcp_websocket_client(cfg.url)
            read_stream, write_stream = await running._ws_context.__aenter__()

            # Create and initialise the MCP session.
            running._session_context = ClientSession(read_stream, write_stream)
            session = await running._session_context.__aenter__()
            running.session = session

            await session.initialize()

            # Discover tools.
            tools_result = await session.list_tools()
            running.tools = self._convert_sdk_tools(cfg.name, tools_result.tools)

            self._servers[cfg.name] = running
            log.info(
                "MCP server %r connected (WebSocket) -- %d tool(s) available",
                cfg.name,
                running.tool_count,
            )
            return True

        except Exception:
            log.exception("Failed to connect to remote MCP server %r (WebSocket)", cfg.name)
            return False

    # -- OAuth helper --------------------------------------------------------

    @staticmethod
    def _build_oauth_auth(cfg: McpServerConfig) -> Any | None:
        """Build an OAuth auth object from server config, or None."""
        if not isinstance(cfg.oauth, dict) or not cfg.url:
            return None

        try:
            from meept.tools.mcp_auth import (
                build_client_credentials_provider,
                build_oauth_provider,
            )
        except ImportError:
            log.warning("mcp_auth module not available -- skipping OAuth for %r", cfg.name)
            return None

        grant_type = cfg.oauth.get("grant_type")
        if grant_type == "client_credentials":
            client_id = cfg.oauth.get("client_id", "")
            client_secret = cfg.oauth.get("client_secret", "")
            if not client_id or not client_secret:
                log.error(
                    "MCP server %r: client_credentials requires client_id and client_secret",
                    cfg.name,
                )
                return None
            return build_client_credentials_provider(
                server_name=cfg.name,
                server_url=cfg.url,
                client_id=client_id,
                client_secret=client_secret,
                scope=cfg.oauth.get("scope"),
            )
        else:
            return build_oauth_provider(
                server_name=cfg.name,
                server_url=cfg.url,
                oauth_config=cfg.oauth,
            )

    # -- Stop methods --------------------------------------------------------

    async def stop_server(self, name: str) -> None:
        """Stop the MCP server identified by *name*."""
        running = self._servers.pop(name, None)
        if running is None:
            log.debug("MCP server %r is not running -- nothing to stop", name)
            return

        log.info("Stopping MCP server %r", name)

        # Cancel any background SSE listener / reconnection task.
        if running._reconnect_task is not None:
            running._reconnect_task.cancel()
            try:
                await running._reconnect_task
            except (asyncio.CancelledError, Exception):
                pass

        if running.is_remote:
            # Remote + SDK (HTTP or WebSocket).
            if running.session is not None:
                try:
                    if running._session_context is not None:
                        await running._session_context.__aexit__(None, None, None)
                except Exception:
                    log.debug("Error closing MCP session for %r", name, exc_info=True)

                # Close the transport context.
                if running._http_transport_context is not None:
                    try:
                        await running._http_transport_context.__aexit__(None, None, None)
                    except Exception:
                        log.debug("Error closing HTTP transport for %r", name, exc_info=True)

                if running._ws_context is not None:
                    try:
                        await running._ws_context.__aexit__(None, None, None)
                    except Exception:
                        log.debug("Error closing WebSocket for %r", name, exc_info=True)

            # Close httpx client.
            if running._http_client is not None:
                try:
                    await running._http_client.aclose()
                except Exception:
                    log.debug("Error closing HTTP client for %r", name, exc_info=True)

            running._remote_connected = False

        elif _MCP_SDK_AVAILABLE and running.session is not None:
            # Local + SDK.
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
            # Local + raw.
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
        * ``running`` -- whether the server is connected/alive
        * ``tool_count`` -- number of tools exposed
        * ``enabled`` -- whether the config entry is enabled
        * ``url`` -- (remote only) the server URL
        """
        result: list[dict[str, Any]] = []
        for name, cfg in self._configs.items():
            running = self._servers.get(name)
            entry: dict[str, Any] = {
                "name": name,
                "type": cfg.type,
                "running": running.running if running else False,
                "tool_count": running.tool_count if running else 0,
                "enabled": cfg.enabled,
            }
            if cfg.type == "remote" and cfg.url:
                entry["url"] = cfg.url
            result.append(entry)
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

        # SDK session covers local, remote HTTP, and WebSocket.
        if _MCP_SDK_AVAILABLE and running.session is not None:
            return await self._invoke_tool_sdk(running, tool_name, arguments, timeout=timeout)

        # Remote raw HTTP.
        if running.is_remote:
            return await self._invoke_tool_http_raw(running, tool_name, arguments, timeout=timeout)

        # Local raw stdio.
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
        """Call a tool via raw JSON-RPC (local stdio)."""
        resp = await asyncio.wait_for(
            self._jsonrpc_request(running, "tools/call", {
                "name": tool_name,
                "arguments": arguments,
            }),
            timeout=timeout,
        )
        if resp is None:
            return {"text": "", "is_error": True}

        return self._parse_tool_result(resp)

    async def _invoke_tool_http_raw(
        self,
        running: _RunningServer,
        tool_name: str,
        arguments: dict[str, Any],
        *,
        timeout: float,
    ) -> dict[str, Any]:
        """Call a tool via raw HTTP JSON-RPC (remote, no SDK)."""
        resp = await asyncio.wait_for(
            self._http_jsonrpc_request(running, "tools/call", {
                "name": tool_name,
                "arguments": arguments,
            }),
            timeout=timeout,
        )
        if resp is None:
            return {"text": "", "is_error": True}

        return self._parse_tool_result(resp)

    @staticmethod
    def _parse_tool_result(resp: dict[str, Any]) -> dict[str, Any]:
        """Parse a tools/call response into ``{text, is_error}``."""
        content = resp.get("content", [])
        parts: list[str] = []
        for block in content:
            if isinstance(block, dict) and block.get("type") == "text":
                parts.append(block.get("text", ""))
        text = "\n".join(parts) if parts else json.dumps(resp)
        is_error = resp.get("isError", False)
        return {"text": text, "is_error": is_error}

    # ------------------------------------------------------------------
    # JSON-RPC helpers (raw stdio mode — local servers)
    # ------------------------------------------------------------------

    async def _jsonrpc_request(
        self,
        running: _RunningServer,
        method: str,
        params: dict[str, Any],
    ) -> dict[str, Any] | None:
        """Send a JSON-RPC request over stdio and wait for the response."""
        if (
            running.process is None
            or running.process.stdin is None
            or running.process.stdout is None
        ):
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

        except (TimeoutError, OSError, json.JSONDecodeError) as exc:
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
        """Send a JSON-RPC notification over stdio (no response expected)."""
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
    # HTTP JSON-RPC helpers (raw mode — remote servers)
    # ------------------------------------------------------------------

    async def _http_jsonrpc_request(
        self,
        running: _RunningServer,
        method: str,
        params: dict[str, Any],
    ) -> dict[str, Any] | None:
        """POST a JSON-RPC request to a remote server via HTTP.

        Handles both ``application/json`` and ``text/event-stream`` responses.
        Tracks the ``Mcp-Session-Id`` header across requests.
        """
        if running._http_client is None or not running.config.url:
            return None

        running._request_id += 1
        body = {
            "jsonrpc": "2.0",
            "id": running._request_id,
            "method": method,
            "params": params,
        }

        headers: dict[str, str] = {
            "Content-Type": "application/json",
            "Accept": "application/json, text/event-stream",
        }
        if running._remote_session_id:
            headers["Mcp-Session-Id"] = running._remote_session_id

        try:
            resp = await running._http_client.post(
                running.config.url,
                json=body,
                headers=headers,
            )

            # Track session ID from response.
            session_id = resp.headers.get("mcp-session-id")
            if session_id:
                running._remote_session_id = session_id

            if resp.status_code == 401:
                log.warning(
                    "MCP server %r returned 401 Unauthorized -- check auth config",
                    running.config.name,
                )
                return None

            if resp.status_code == 403:
                log.warning(
                    "MCP server %r returned 403 Forbidden",
                    running.config.name,
                )
                return None

            resp.raise_for_status()

            content_type = resp.headers.get("content-type", "")

            if "text/event-stream" in content_type:
                return await self._parse_sse_response(resp, running)

            # Standard JSON response.
            data = resp.json()
            if "error" in data:
                log.warning(
                    "MCP HTTP JSON-RPC error from %r method=%s: %s",
                    running.config.name,
                    method,
                    data["error"],
                )
                return None
            return data.get("result")

        except httpx.TimeoutException as exc:
            log.warning(
                "MCP HTTP timeout with %r method=%s: %s",
                running.config.name,
                method,
                exc,
            )
            return None
        except (httpx.HTTPError, json.JSONDecodeError) as exc:
            log.warning(
                "MCP HTTP communication error with %r method=%s: %s",
                running.config.name,
                method,
                exc,
            )
            return None

    async def _http_jsonrpc_notify(
        self,
        running: _RunningServer,
        method: str,
        params: dict[str, Any],
    ) -> None:
        """POST a JSON-RPC notification to a remote server via HTTP."""
        if running._http_client is None or not running.config.url:
            return

        body = {
            "jsonrpc": "2.0",
            "method": method,
            "params": params,
        }

        headers: dict[str, str] = {"Content-Type": "application/json"}
        if running._remote_session_id:
            headers["Mcp-Session-Id"] = running._remote_session_id

        try:
            resp = await running._http_client.post(
                running.config.url,
                json=body,
                headers=headers,
            )
            # Track session ID.
            session_id = resp.headers.get("mcp-session-id")
            if session_id:
                running._remote_session_id = session_id

        except (httpx.HTTPError, OSError) as exc:
            log.debug(
                "Failed to send HTTP notification to %r: %s",
                running.config.name,
                exc,
            )

    async def _parse_sse_response(
        self,
        resp: httpx.Response,
        running: _RunningServer,
    ) -> dict[str, Any] | None:
        """Parse a ``text/event-stream`` response to extract the JSON-RPC result.

        Uses ``httpx_sse`` if available, otherwise falls back to manual parsing.
        """
        if _HTTPX_SSE_AVAILABLE:
            try:
                source = EventSource(resp)
                for sse in source.iter_sse():
                    if sse.id:
                        running._last_event_id = sse.id
                    if sse.event == "message" or sse.event == "":
                        try:
                            data = json.loads(sse.data)
                            if "result" in data:
                                return data["result"]
                            if "error" in data:
                                log.warning(
                                    "MCP SSE error from %r: %s",
                                    running.config.name,
                                    data["error"],
                                )
                                return None
                        except json.JSONDecodeError:
                            continue
            except Exception as exc:
                log.warning("Error parsing SSE from %r: %s", running.config.name, exc)
                return None
        else:
            # Manual SSE parsing fallback.
            for line in resp.text.splitlines():
                if line.startswith("data: "):
                    try:
                        data = json.loads(line[6:])
                        if "result" in data:
                            return data["result"]
                        if "error" in data:
                            return None
                    except json.JSONDecodeError:
                        continue
                elif line.startswith("id: "):
                    running._last_event_id = line[4:].strip()
        return None

    # ------------------------------------------------------------------
    # SSE listener (server-initiated requests, raw mode)
    # ------------------------------------------------------------------

    async def _sse_listener(self, running: _RunningServer) -> None:
        """Background task: listen for server-initiated messages via GET SSE.

        Only used in raw HTTP mode.  SDK mode handles this internally.
        """
        if running._http_client is None or not running.config.url:
            return

        while running._remote_connected:
            headers: dict[str, str] = {"Accept": "text/event-stream"}
            if running._remote_session_id:
                headers["Mcp-Session-Id"] = running._remote_session_id
            if running._last_event_id:
                headers["Last-Event-Id"] = running._last_event_id

            try:
                async with running._http_client.stream(
                    "GET",
                    running.config.url,
                    headers=headers,
                ) as resp:
                    if resp.status_code != 200:
                        log.debug(
                            "MCP SSE listener for %r got HTTP %d -- will retry",
                            running.config.name,
                            resp.status_code,
                        )
                        break

                    async for line in resp.aiter_lines():
                        if not running._remote_connected:
                            return
                        line = line.strip()
                        if line.startswith("id: "):
                            running._last_event_id = line[4:]
                        elif line.startswith("data: "):
                            try:
                                msg = json.loads(line[6:])
                                # Log notifications; ignore for now.
                                method = msg.get("method", "")
                                if method:
                                    log.debug(
                                        "MCP server %r sent notification: %s",
                                        running.config.name,
                                        method,
                                    )
                            except json.JSONDecodeError:
                                pass

            except asyncio.CancelledError:
                return
            except (httpx.HTTPError, OSError) as exc:
                if not running._remote_connected:
                    return
                log.warning(
                    "MCP SSE listener for %r disconnected: %s -- attempting reconnect",
                    running.config.name,
                    exc,
                )
                await self._reconnect_with_backoff(running.config.name)
                return

    # ------------------------------------------------------------------
    # Auto-reconnection
    # ------------------------------------------------------------------

    async def _reconnect_with_backoff(self, name: str) -> None:
        """Attempt to reconnect a remote server with exponential backoff."""
        cfg = self._configs.get(name)
        if cfg is None or cfg.type != "remote":
            return

        # Remove stale server entry.
        old_running = self._servers.pop(name, None)
        if old_running and old_running._http_client:
            try:
                await old_running._http_client.aclose()
            except Exception:
                pass

        for attempt in range(_RECONNECT_MAX_RETRIES):
            delay = min(
                _RECONNECT_INITIAL_DELAY_S * (2 ** attempt),
                _RECONNECT_MAX_DELAY_S,
            )
            jitter = random.uniform(0, _RECONNECT_JITTER_RATIO * delay)
            delay -= jitter

            log.info(
                "MCP server %r reconnect attempt %d/%d in %.1fs",
                name,
                attempt + 1,
                _RECONNECT_MAX_RETRIES,
                delay,
            )

            await asyncio.sleep(delay)

            try:
                success = await self.start_server(name)
                if success:
                    log.info("MCP server %r reconnected successfully", name)
                    return
            except Exception:
                log.debug("Reconnect attempt %d for %r failed", attempt + 1, name, exc_info=True)

        log.error(
            "MCP server %r: gave up after %d reconnection attempts",
            name,
            _RECONNECT_MAX_RETRIES,
        )

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
            except TimeoutError:
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
