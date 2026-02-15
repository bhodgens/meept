"""RPC bridge for communication between tester and subject daemons."""

from __future__ import annotations

import asyncio
import json
import logging
from pathlib import Path
from typing import Any

log = logging.getLogger(__name__)


class RpcBridge:
    """Bridges RPC communication between tester and subject daemons.

    Provides methods for the tester to query and control the subject.
    """

    def __init__(
        self,
        tester_socket: Path | str,
        subject_socket: Path | str,
    ) -> None:
        self._tester_socket = Path(tester_socket).expanduser()
        self._subject_socket = Path(subject_socket).expanduser()

    async def call_tester(
        self,
        method: str,
        params: dict[str, Any] | None = None,
    ) -> dict[str, Any]:
        """Call an RPC method on the tester daemon."""
        return await self._call(self._tester_socket, method, params)

    async def call_subject(
        self,
        method: str,
        params: dict[str, Any] | None = None,
    ) -> dict[str, Any]:
        """Call an RPC method on the subject daemon."""
        return await self._call(self._subject_socket, method, params)

    async def get_subject_status(self) -> dict[str, Any]:
        """Get the status of the subject daemon."""
        return await self.call_subject("status", {})

    async def send_subject_message(self, message: str) -> dict[str, Any]:
        """Send a chat message to the subject daemon."""
        return await self.call_subject("chat", {"message": message})

    async def get_subject_logs(self, limit: int = 100) -> list[str]:
        """Get recent logs from the subject daemon."""
        # This would need a custom RPC method on the subject
        log_file = self._subject_socket.parent / "meept.log"
        if log_file.exists():
            lines = log_file.read_text().split("\n")
            return lines[-limit:]
        return []

    async def _call(
        self,
        socket_path: Path,
        method: str,
        params: dict[str, Any] | None,
    ) -> dict[str, Any]:
        """Make an RPC call to a daemon."""
        if not socket_path.exists():
            raise ConnectionError(f"Socket not found: {socket_path}")

        try:
            reader, writer = await asyncio.open_unix_connection(str(socket_path))

            # Build JSON-RPC request
            request = {
                "jsonrpc": "2.0",
                "method": method,
                "params": params or {},
                "id": 1,
            }
            payload = json.dumps(request).encode("utf-8")

            # Send length-prefixed frame
            header = f"{len(payload)}\n".encode("utf-8")
            writer.write(header + payload)
            await writer.drain()

            # Read response
            length_line = await reader.readline()
            if not length_line:
                raise ConnectionError("No response from daemon")

            length = int(length_line.strip())
            response_bytes = await reader.readexactly(length)
            response = json.loads(response_bytes.decode("utf-8"))

            writer.close()
            await writer.wait_closed()

            if "error" in response:
                raise RuntimeError(f"RPC error: {response['error']}")

            return response.get("result", {})

        except FileNotFoundError:
            raise ConnectionError(f"Socket not found: {socket_path}")
        except Exception as exc:
            log.exception("RPC call failed: %s %s", method, params)
            raise RuntimeError(f"RPC call failed: {exc}") from exc
