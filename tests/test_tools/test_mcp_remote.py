"""Tests for remote MCP server support in McpManager.

All tests use mocks — no real network calls are made.
"""

from __future__ import annotations

import asyncio
import json
from pathlib import Path
from typing import Any
from unittest.mock import AsyncMock, MagicMock, patch

import httpx
import pytest

from meept.tools.mcp_manager import (
    _RECONNECT_MAX_RETRIES,
    McpManager,
    McpServerConfig,
    _RunningServer,
)

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _write_config(path: Path, mcp_entries: dict[str, Any]) -> Path:
    """Write a minimal mcp_servers.json and return its path."""
    path.write_text(json.dumps({"mcp": mcp_entries}), encoding="utf-8")
    return path


def _make_running(
    name: str = "test-srv",
    server_type: str = "remote",
    url: str = "https://example.com/mcp",
    **kw: Any,
) -> _RunningServer:
    cfg = McpServerConfig(name=name, type=server_type, url=url, **kw)
    return _RunningServer(config=cfg)


# ---------------------------------------------------------------------------
# _RunningServer.running property
# ---------------------------------------------------------------------------


class TestRunningServerProperty:
    """Test the .running property for different server modes."""

    def test_remote_sdk_running(self):
        rs = _make_running()
        rs.session = MagicMock()
        assert rs.running is True

    def test_remote_sdk_not_running(self):
        rs = _make_running()
        assert rs.running is False

    def test_remote_raw_running(self):
        rs = _make_running()
        rs._remote_connected = True
        assert rs.running is True

    def test_remote_raw_not_running(self):
        rs = _make_running()
        rs._remote_connected = False
        assert rs.running is False

    def test_local_process_running(self):
        rs = _make_running(server_type="local", url=None)
        rs.process = MagicMock()
        rs.process.returncode = None
        assert rs.running is True

    def test_local_process_exited(self):
        rs = _make_running(server_type="local", url=None)
        rs.process = MagicMock()
        rs.process.returncode = 0
        assert rs.running is False

    def test_is_remote_property(self):
        rs = _make_running()
        assert rs.is_remote is True

    def test_is_remote_false_for_local(self):
        rs = _make_running(server_type="local", url=None)
        assert rs.is_remote is False


# ---------------------------------------------------------------------------
# Config parsing
# ---------------------------------------------------------------------------


class TestConfigParsing:
    """Test _load_config handling of remote/websocket/oauth entries."""

    def test_remote_config_loaded(self, tmp_path):
        config_path = _write_config(tmp_path / "mcp.json", {
            "my-remote": {
                "type": "remote",
                "url": "https://example.com/mcp",
                "headers": {"Authorization": "Bearer tok"},
                "timeout": 15000,
            }
        })
        mgr = McpManager(config_path=config_path)
        assert "my-remote" in mgr._configs
        cfg = mgr._configs["my-remote"]
        assert cfg.type == "remote"
        assert cfg.url == "https://example.com/mcp"
        assert cfg.headers == {"Authorization": "Bearer tok"}
        assert cfg.timeout == 15000

    def test_remote_config_with_oauth_dict(self, tmp_path):
        config_path = _write_config(tmp_path / "mcp.json", {
            "oauth-srv": {
                "type": "remote",
                "url": "https://example.com/mcp",
                "oauth": {"client_name": "test", "scope": "read"},
            }
        })
        mgr = McpManager(config_path=config_path)
        cfg = mgr._configs["oauth-srv"]
        assert isinstance(cfg.oauth, dict)
        assert cfg.oauth["client_name"] == "test"

    def test_remote_config_with_oauth_false(self, tmp_path):
        config_path = _write_config(tmp_path / "mcp.json", {
            "no-auth": {
                "type": "remote",
                "url": "https://example.com/mcp",
                "oauth": False,
            }
        })
        mgr = McpManager(config_path=config_path)
        cfg = mgr._configs["no-auth"]
        assert cfg.oauth is False

    def test_remote_config_with_oauth_absent(self, tmp_path):
        config_path = _write_config(tmp_path / "mcp.json", {
            "default-auth": {
                "type": "remote",
                "url": "https://example.com/mcp",
            }
        })
        mgr = McpManager(config_path=config_path)
        cfg = mgr._configs["default-auth"]
        assert cfg.oauth is None

    def test_websocket_config_loaded(self, tmp_path):
        config_path = _write_config(tmp_path / "mcp.json", {
            "ws-srv": {
                "type": "remote",
                "url": "wss://example.com/ws",
            }
        })
        mgr = McpManager(config_path=config_path)
        cfg = mgr._configs["ws-srv"]
        assert cfg.url.startswith("wss://")

    def test_remote_without_url_skipped(self, tmp_path):
        config_path = _write_config(tmp_path / "mcp.json", {
            "bad-remote": {"type": "remote"},
        })
        mgr = McpManager(config_path=config_path)
        assert "bad-remote" not in mgr._configs

    def test_disabled_examples_not_started(self, tmp_path):
        """Servers with enabled=false load but don't start."""
        config_path = _write_config(tmp_path / "mcp.json", {
            "disabled": {
                "type": "remote",
                "url": "https://example.com/mcp",
                "enabled": False,
            }
        })
        mgr = McpManager(config_path=config_path)
        assert "disabled" in mgr._configs
        assert mgr._configs["disabled"].enabled is False


# ---------------------------------------------------------------------------
# start_server routing
# ---------------------------------------------------------------------------


class TestStartServerRouting:
    """Test that start_server routes to the correct transport handler."""

    @pytest.fixture()
    def mgr(self, tmp_path):
        config_path = _write_config(tmp_path / "mcp.json", {})
        return McpManager(config_path=config_path)

    async def test_routes_websocket_url(self, mgr):
        """ws:// or wss:// URLs route to _start_websocket_server."""
        cfg = McpServerConfig(name="ws", type="remote", url="wss://example.com/ws")
        mgr._configs["ws"] = cfg

        with patch.object(
            mgr, "_start_websocket_server",
            new_callable=AsyncMock, return_value=True,
        ) as mock:
            result = await mgr.start_server("ws")
            mock.assert_awaited_once_with(cfg)
            assert result is True

    async def test_routes_http_sdk(self, mgr):
        """HTTP URL with SDK available routes to _start_remote_server_sdk."""
        cfg = McpServerConfig(name="http-sdk", type="remote", url="https://example.com/mcp")
        mgr._configs["http-sdk"] = cfg

        with (
            patch("meept.tools.mcp_manager._MCP_SDK_AVAILABLE", True),
            patch.object(
                mgr, "_start_remote_server_sdk",
                new_callable=AsyncMock, return_value=True,
            ) as mock,
        ):
            result = await mgr.start_server("http-sdk")
            mock.assert_awaited_once_with(cfg)
            assert result is True

    async def test_routes_http_raw(self, mgr):
        """HTTP URL without SDK routes to _start_remote_server_raw."""
        cfg = McpServerConfig(name="http-raw", type="remote", url="https://example.com/mcp")
        mgr._configs["http-raw"] = cfg

        with (
            patch("meept.tools.mcp_manager._MCP_SDK_AVAILABLE", False),
            patch.object(
                mgr, "_start_remote_server_raw",
                new_callable=AsyncMock, return_value=True,
            ) as mock,
        ):
            result = await mgr.start_server("http-raw")
            mock.assert_awaited_once_with(cfg)
            assert result is True

    async def test_remote_no_url_returns_false(self, mgr):
        cfg = McpServerConfig(name="no-url", type="remote", url=None)
        mgr._configs["no-url"] = cfg
        result = await mgr.start_server("no-url")
        assert result is False

    async def test_disabled_server_returns_false(self, mgr):
        cfg = McpServerConfig(name="off", type="remote", url="https://x.com", enabled=False)
        mgr._configs["off"] = cfg
        result = await mgr.start_server("off")
        assert result is False


# ---------------------------------------------------------------------------
# HTTP JSON-RPC helpers
# ---------------------------------------------------------------------------


class TestHttpJsonrpcRequest:
    """Test _http_jsonrpc_request."""

    def _make_manager(self, tmp_path):
        config_path = _write_config(tmp_path / "mcp.json", {})
        return McpManager(config_path=config_path)

    async def test_json_response(self, tmp_path):
        mgr = self._make_manager(tmp_path)
        running = _make_running()

        mock_resp = MagicMock()
        mock_resp.status_code = 200
        mock_resp.headers = {"content-type": "application/json"}
        mock_resp.json.return_value = {
            "jsonrpc": "2.0",
            "id": 1,
            "result": {"tools": []}
        }
        mock_resp.raise_for_status = MagicMock()

        mock_client = AsyncMock()
        mock_client.post = AsyncMock(return_value=mock_resp)
        running._http_client = mock_client

        result = await mgr._http_jsonrpc_request(running, "tools/list", {})
        assert result == {"tools": []}

    async def test_session_id_tracking(self, tmp_path):
        mgr = self._make_manager(tmp_path)
        running = _make_running()

        mock_resp = MagicMock()
        mock_resp.status_code = 200
        mock_resp.headers = {
            "content-type": "application/json",
            "mcp-session-id": "session-abc",
        }
        mock_resp.json.return_value = {"jsonrpc": "2.0", "id": 1, "result": {}}
        mock_resp.raise_for_status = MagicMock()

        mock_client = AsyncMock()
        mock_client.post = AsyncMock(return_value=mock_resp)
        running._http_client = mock_client

        await mgr._http_jsonrpc_request(running, "initialize", {})
        assert running._remote_session_id == "session-abc"

    async def test_401_returns_none(self, tmp_path):
        mgr = self._make_manager(tmp_path)
        running = _make_running()

        mock_resp = MagicMock()
        mock_resp.status_code = 401
        mock_resp.headers = {}

        mock_client = AsyncMock()
        mock_client.post = AsyncMock(return_value=mock_resp)
        running._http_client = mock_client

        result = await mgr._http_jsonrpc_request(running, "initialize", {})
        assert result is None

    async def test_403_returns_none(self, tmp_path):
        mgr = self._make_manager(tmp_path)
        running = _make_running()

        mock_resp = MagicMock()
        mock_resp.status_code = 403
        mock_resp.headers = {}

        mock_client = AsyncMock()
        mock_client.post = AsyncMock(return_value=mock_resp)
        running._http_client = mock_client

        result = await mgr._http_jsonrpc_request(running, "initialize", {})
        assert result is None

    async def test_timeout_returns_none(self, tmp_path):
        mgr = self._make_manager(tmp_path)
        running = _make_running()

        mock_client = AsyncMock()
        mock_client.post = AsyncMock(side_effect=httpx.TimeoutException("timeout"))
        running._http_client = mock_client

        result = await mgr._http_jsonrpc_request(running, "tools/call", {})
        assert result is None

    async def test_sse_response(self, tmp_path):
        """SSE content-type delegates to _parse_sse_response."""
        mgr = self._make_manager(tmp_path)
        running = _make_running()

        mock_resp = MagicMock()
        mock_resp.status_code = 200
        mock_resp.headers = {
            "content-type": "text/event-stream",
        }
        mock_resp.raise_for_status = MagicMock()

        mock_client = AsyncMock()
        mock_client.post = AsyncMock(return_value=mock_resp)
        running._http_client = mock_client

        with patch.object(
            mgr, "_parse_sse_response",
            new_callable=AsyncMock,
            return_value={"tools": []},
        ) as mock_parse:
            result = await mgr._http_jsonrpc_request(running, "tools/list", {})
            mock_parse.assert_awaited_once()
            assert result == {"tools": []}

    async def test_no_client_returns_none(self, tmp_path):
        mgr = self._make_manager(tmp_path)
        running = _make_running()
        running._http_client = None

        result = await mgr._http_jsonrpc_request(running, "initialize", {})
        assert result is None


# ---------------------------------------------------------------------------
# HTTP JSON-RPC notify
# ---------------------------------------------------------------------------


class TestHttpJsonrpcNotify:
    async def test_posts_notification(self, tmp_path):
        config_path = _write_config(tmp_path / "mcp.json", {})
        mgr = McpManager(config_path=config_path)
        running = _make_running()

        mock_resp = MagicMock()
        mock_resp.status_code = 202
        mock_resp.headers = {"mcp-session-id": "sess-1"}

        mock_client = AsyncMock()
        mock_client.post = AsyncMock(return_value=mock_resp)
        running._http_client = mock_client

        await mgr._http_jsonrpc_notify(running, "notifications/initialized", {})

        mock_client.post.assert_awaited_once()
        call_kwargs = mock_client.post.call_args
        body = call_kwargs.kwargs.get("json") or call_kwargs[1].get("json")
        assert body["method"] == "notifications/initialized"
        assert "id" not in body  # Notifications have no id.
        assert running._remote_session_id == "sess-1"


# ---------------------------------------------------------------------------
# invoke_tool routing
# ---------------------------------------------------------------------------


class TestInvokeToolRouting:
    """Test that invoke_tool routes correctly for remote servers."""

    @pytest.fixture()
    def mgr(self, tmp_path):
        config_path = _write_config(tmp_path / "mcp.json", {})
        return McpManager(config_path=config_path)

    async def test_routes_to_sdk_for_remote_with_session(self, mgr):
        running = _make_running()
        running.session = MagicMock()
        running.tools = [{"type": "function", "function": {"name": "test-srv.do_thing"}}]
        mgr._servers["test-srv"] = running

        sdk_result = {"text": "ok", "is_error": False}
        with (
            patch("meept.tools.mcp_manager._MCP_SDK_AVAILABLE", True),
            patch.object(
                mgr, "_invoke_tool_sdk",
                new_callable=AsyncMock, return_value=sdk_result,
            ),
        ):
            result = await mgr.invoke_tool("test-srv", "do_thing", {})
            assert result["text"] == "ok"

    async def test_routes_to_http_raw_for_remote_without_session(self, mgr):
        running = _make_running()
        running._remote_connected = True
        running.tools = [{"type": "function", "function": {"name": "test-srv.do_thing"}}]
        mgr._servers["test-srv"] = running

        raw_result = {"text": "raw-ok", "is_error": False}
        with (
            patch("meept.tools.mcp_manager._MCP_SDK_AVAILABLE", False),
            patch.object(
                mgr, "_invoke_tool_http_raw",
                new_callable=AsyncMock, return_value=raw_result,
            ),
        ):
            result = await mgr.invoke_tool("test-srv", "do_thing", {})
            assert result["text"] == "raw-ok"


# ---------------------------------------------------------------------------
# _invoke_tool_http_raw
# ---------------------------------------------------------------------------


class TestInvokeToolHttpRaw:
    async def test_parses_content_blocks(self, tmp_path):
        config_path = _write_config(tmp_path / "mcp.json", {})
        mgr = McpManager(config_path=config_path)
        running = _make_running()

        with patch.object(
            mgr, "_http_jsonrpc_request",
            new_callable=AsyncMock,
            return_value={
                "content": [
                    {"type": "text", "text": "Hello"},
                    {"type": "text", "text": "World"},
                ],
                "isError": False,
            },
        ):
            result = await mgr._invoke_tool_http_raw(
                running, "greet", {}, timeout=10.0
            )
            assert result["text"] == "Hello\nWorld"
            assert result["is_error"] is False

    async def test_returns_error_on_none_response(self, tmp_path):
        config_path = _write_config(tmp_path / "mcp.json", {})
        mgr = McpManager(config_path=config_path)
        running = _make_running()

        with patch.object(
            mgr, "_http_jsonrpc_request",
            new_callable=AsyncMock,
            return_value=None,
        ):
            result = await mgr._invoke_tool_http_raw(
                running, "fail", {}, timeout=10.0
            )
            assert result["is_error"] is True


# ---------------------------------------------------------------------------
# stop_server
# ---------------------------------------------------------------------------


class TestStopServer:
    @pytest.fixture()
    def mgr(self, tmp_path):
        config_path = _write_config(tmp_path / "mcp.json", {})
        return McpManager(config_path=config_path)

    async def test_stop_remote_raw_closes_client(self, mgr):
        running = _make_running()
        running._remote_connected = True
        running._http_client = AsyncMock()
        running._http_client.aclose = AsyncMock()
        mgr._servers["test-srv"] = running

        await mgr.stop_server("test-srv")

        running._http_client.aclose.assert_awaited_once()
        assert running._remote_connected is False
        assert "test-srv" not in mgr._servers

    async def test_stop_remote_sdk_closes_contexts(self, mgr):
        running = _make_running()
        running.session = MagicMock()
        running._session_context = AsyncMock()
        running._session_context.__aexit__ = AsyncMock()
        running._http_transport_context = AsyncMock()
        running._http_transport_context.__aexit__ = AsyncMock()
        running._http_client = AsyncMock()
        running._http_client.aclose = AsyncMock()
        mgr._servers["test-srv"] = running

        await mgr.stop_server("test-srv")

        running._session_context.__aexit__.assert_awaited_once()
        running._http_transport_context.__aexit__.assert_awaited_once()
        running._http_client.aclose.assert_awaited_once()

    async def test_stop_websocket_closes_ws_context(self, mgr):
        running = _make_running(url="wss://example.com/ws")
        running.session = MagicMock()
        running._session_context = AsyncMock()
        running._session_context.__aexit__ = AsyncMock()
        running._ws_context = AsyncMock()
        running._ws_context.__aexit__ = AsyncMock()
        mgr._servers["test-srv"] = running

        await mgr.stop_server("test-srv")

        running._ws_context.__aexit__.assert_awaited_once()

    async def test_stop_cancels_reconnect_task(self, mgr):
        running = _make_running()
        running._remote_connected = True
        running._http_client = AsyncMock()
        running._http_client.aclose = AsyncMock()

        # Create a task that sleeps forever.
        async def forever():
            await asyncio.sleep(9999)

        running._reconnect_task = asyncio.create_task(forever())
        mgr._servers["test-srv"] = running

        await mgr.stop_server("test-srv")

        assert running._reconnect_task.cancelled()

    async def test_stop_nonexistent_server(self, mgr):
        """Stopping a server that isn't running is a no-op."""
        await mgr.stop_server("nonexistent")  # Should not raise.


# ---------------------------------------------------------------------------
# list_servers
# ---------------------------------------------------------------------------


class TestListServers:
    async def test_includes_url_for_remote(self, tmp_path):
        config_path = _write_config(tmp_path / "mcp.json", {
            "remote-1": {
                "type": "remote",
                "url": "https://example.com/mcp",
            }
        })
        mgr = McpManager(config_path=config_path)
        servers = await mgr.list_servers()
        assert len(servers) == 1
        assert servers[0]["url"] == "https://example.com/mcp"
        assert servers[0]["type"] == "remote"

    async def test_no_url_for_local(self, tmp_path):
        config_path = _write_config(tmp_path / "mcp.json", {
            "local-1": {
                "type": "local",
                "command": ["echo", "hi"],
            }
        })
        mgr = McpManager(config_path=config_path)
        servers = await mgr.list_servers()
        assert len(servers) == 1
        assert "url" not in servers[0]


# ---------------------------------------------------------------------------
# _reconnect_with_backoff
# ---------------------------------------------------------------------------


class TestReconnectWithBackoff:
    async def test_reconnects_on_success(self, tmp_path):
        config_path = _write_config(tmp_path / "mcp.json", {
            "reconn": {"type": "remote", "url": "https://example.com/mcp"},
        })
        mgr = McpManager(config_path=config_path)

        # Simulate a stale server entry.
        old_running = _make_running(name="reconn")
        old_running._http_client = AsyncMock()
        old_running._http_client.aclose = AsyncMock()
        mgr._servers["reconn"] = old_running

        with patch.object(mgr, "start_server", new_callable=AsyncMock, return_value=True), \
             patch("meept.tools.mcp_manager.asyncio.sleep", new_callable=AsyncMock):
            await mgr._reconnect_with_backoff("reconn")

        # Old client should have been closed.
        old_running._http_client.aclose.assert_awaited_once()

    async def test_gives_up_after_max_retries(self, tmp_path):
        config_path = _write_config(tmp_path / "mcp.json", {
            "fail-srv": {"type": "remote", "url": "https://example.com/mcp"},
        })
        mgr = McpManager(config_path=config_path)

        call_count = 0

        async def fail_start(name):
            nonlocal call_count
            call_count += 1
            return False

        with patch.object(mgr, "start_server", side_effect=fail_start), \
             patch("meept.tools.mcp_manager.asyncio.sleep", new_callable=AsyncMock):
            await mgr._reconnect_with_backoff("fail-srv")

        assert call_count == _RECONNECT_MAX_RETRIES

    async def test_noop_for_local_server(self, tmp_path):
        config_path = _write_config(tmp_path / "mcp.json", {
            "local": {"type": "local", "command": ["echo"]},
        })
        mgr = McpManager(config_path=config_path)

        # Should return immediately without attempting reconnect.
        with patch.object(mgr, "start_server", new_callable=AsyncMock) as mock:
            await mgr._reconnect_with_backoff("local")
            mock.assert_not_awaited()


# ---------------------------------------------------------------------------
# _parse_tool_result
# ---------------------------------------------------------------------------


class TestParseToolResult:
    def test_extracts_text_blocks(self):
        resp = {
            "content": [
                {"type": "text", "text": "line1"},
                {"type": "text", "text": "line2"},
                {"type": "image", "data": "..."},
            ],
            "isError": False,
        }
        result = McpManager._parse_tool_result(resp)
        assert result["text"] == "line1\nline2"
        assert result["is_error"] is False

    def test_falls_back_to_json_dump(self):
        resp = {"content": [], "isError": True}
        result = McpManager._parse_tool_result(resp)
        # With no text blocks, falls back to JSON dump.
        assert "content" in result["text"]
        assert result["is_error"] is True


# ---------------------------------------------------------------------------
# OAuth config gating
# ---------------------------------------------------------------------------


class TestOauthGating:
    async def test_raw_mode_warns_about_oauth(self, tmp_path):
        """When SDK is unavailable and oauth is configured, a warning is logged."""
        config_path = _write_config(tmp_path / "mcp.json", {
            "oauth-no-sdk": {
                "type": "remote",
                "url": "https://example.com/mcp",
                "oauth": {"client_name": "test"},
            }
        })
        mgr = McpManager(config_path=config_path)

        # Mock _http_jsonrpc_request to fail (we just want to check the warning).
        with (
            patch("meept.tools.mcp_manager._MCP_SDK_AVAILABLE", False),
            patch.object(
                mgr, "_http_jsonrpc_request",
                new_callable=AsyncMock, return_value=None,
            ),
            patch("meept.tools.mcp_manager.log") as mock_log,
        ):
            await mgr._start_remote_server_raw(mgr._configs["oauth-no-sdk"])
            # Check that a warning about OAuth was logged.
            warning_calls = [
                str(c) for c in mock_log.warning.call_args_list
            ]
            assert any("OAuth requires the MCP SDK" in w for w in warning_calls)
