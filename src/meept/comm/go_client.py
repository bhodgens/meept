"""Client adapter for connecting Python agents to the Go daemon.

This module provides an async client that speaks the same JSON-RPC
protocol as the Go daemon's CommServer.
"""

from __future__ import annotations

import asyncio
import json
import logging
from pathlib import Path
from typing import TYPE_CHECKING, Any

log = logging.getLogger(__name__)


class GoClientError(Exception):
    """Error communicating with Go daemon."""


class GoDaemonClient:
    """Async client for the Go meept-daemon.

    Uses the same length-prefixed JSON-RPC protocol as the Python CommServer
    to maintain compatibility.
    """

    def __init__(
        self,
        socket_path: str | Path | None = None,
        timeout: float = 30.0,
    ) -> None:
        if socket_path is None:
            home = Path.home()
            socket_path = home / ".meept" / "meept.sock"
        self._socket_path = Path(socket_path)
        self._timeout = timeout
        self._reader: asyncio.StreamReader | None = None
        self._writer: asyncio.StreamWriter | None = None
        self._request_id = 0
        self._lock = asyncio.Lock()

    async def connect(self) -> None:
        """Connect to the Go daemon."""
        if self._writer is not None:
            return

        if not self._socket_path.exists():
            raise GoClientError(f"Socket not found: {self._socket_path}")

        self._reader, self._writer = await asyncio.wait_for(
            asyncio.open_unix_connection(str(self._socket_path)),
            timeout=self._timeout,
        )
        log.debug("go_client: connected to %s", self._socket_path)

    async def close(self) -> None:
        """Close the connection."""
        if self._writer is not None:
            self._writer.close()
            await self._writer.wait_closed()
            self._writer = None
            self._reader = None
            log.debug("go_client: disconnected")

    async def call(self, method: str, params: dict[str, Any] | None = None) -> Any:
        """Call an RPC method on the Go daemon.

        Parameters
        ----------
        method:
            The RPC method name (e.g., "ping", "daemon.status").
        params:
            Optional parameters to pass to the method.

        Returns
        -------
        Any
            The result from the RPC call.

        Raises
        ------
        GoClientError
            If the call fails or returns an error.
        """
        async with self._lock:
            if self._writer is None:
                await self.connect()

            self._request_id += 1
            request = {
                "jsonrpc": "2.0",
                "id": self._request_id,
                "method": method,
                "params": params or {},
            }

            # Send request
            payload = json.dumps(request).encode("utf-8")
            frame = f"{len(payload)}\n".encode("utf-8") + payload
            self._writer.write(frame)
            await self._writer.drain()

            # Read response
            assert self._reader is not None
            length_line = await asyncio.wait_for(
                self._reader.readline(),
                timeout=self._timeout,
            )
            if not length_line:
                raise GoClientError("Connection closed by server")

            length = int(length_line.decode().strip())
            response_data = await asyncio.wait_for(
                self._reader.readexactly(length),
                timeout=self._timeout,
            )

            response = json.loads(response_data)

            if "error" in response and response["error"]:
                err = response["error"]
                raise GoClientError(f"RPC error {err.get('code')}: {err.get('message')}")

            return response.get("result")

    async def ping(self) -> str:
        """Ping the daemon."""
        return await self.call("ping")

    async def status(self) -> dict[str, Any]:
        """Get daemon status."""
        return await self.call("daemon.status")

    async def bus_publish(self, topic: str, payload: dict[str, Any]) -> dict[str, int]:
        """Publish a message to the bus."""
        return await self.call("bus.publish", {"topic": topic, "payload": payload})

    async def bus_stats(self) -> dict[str, int]:
        """Get bus statistics."""
        return await self.call("bus.stats")

    # Security methods (Go-native, high-performance)

    async def check_permission(
        self, action: str, details: dict[str, str]
    ) -> dict[str, Any]:
        """Check if an action is permitted.

        Parameters
        ----------
        action:
            Action type (e.g., "file_read", "shell_execute").
        details:
            Action-specific details (e.g., {"path": "/tmp/foo"}).

        Returns
        -------
        dict
            Result with keys: allowed, reason, effective_risk, needs_confirm.
        """
        return await self.call(
            "security.check_permission", {"action": action, "details": details}
        )

    async def check_path(self, path: str) -> bool:
        """Check if a path is allowed.

        Parameters
        ----------
        path:
            The file path to check.

        Returns
        -------
        bool
            True if the path is allowed.
        """
        result = await self.call("security.check_path", {"path": path})
        return result.get("allowed", False)

    async def evaluate_shell_risk(self, command: str) -> str:
        """Evaluate the risk level of a shell command.

        Parameters
        ----------
        command:
            The shell command to evaluate.

        Returns
        -------
        str
            Risk level: "SAFE", "LOW", "MEDIUM", "HIGH", or "CRITICAL".
        """
        result = await self.call("security.evaluate_shell_risk", {"command": command})
        return result.get("risk_level", "UNKNOWN")

    async def is_financial(self, text: str) -> bool:
        """Check if text contains financial operations.

        Parameters
        ----------
        text:
            The text to check.

        Returns
        -------
        bool
            True if financial operations are detected.
        """
        result = await self.call("security.is_financial", {"text": text})
        return result.get("is_financial", False)

    async def check_permission_batch(
        self, checks: list[dict[str, Any]]
    ) -> list[dict[str, Any]]:
        """Check multiple permissions in a single call.

        Parameters
        ----------
        checks:
            List of dicts with "action" and "details" keys.

        Returns
        -------
        list
            List of results, one per check.
        """
        result = await self.call("security.check_batch", {"checks": checks})
        return result.get("results", [])

    async def __aenter__(self) -> GoDaemonClient:
        await self.connect()
        return self

    async def __aexit__(self, *args: Any) -> None:
        await self.close()


async def check_go_daemon(socket_path: str | Path | None = None) -> bool:
    """Check if the Go daemon is running.

    Parameters
    ----------
    socket_path:
        Optional path to the Unix socket.

    Returns
    -------
    bool
        True if the daemon is running and responding.
    """
    try:
        async with GoDaemonClient(socket_path) as client:
            result = await client.ping()
            return result == "pong"
    except Exception:
        return False
