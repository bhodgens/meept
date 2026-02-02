"""Chat screen and RPC client for communicating with the meept daemon."""

from __future__ import annotations

import asyncio
import json
import logging
import uuid
from typing import Any

from textual import work
from textual.app import ComposeResult
from textual.binding import Binding
from textual.containers import ScrollableContainer
from textual.screen import Screen
from textual.widgets import Footer, Header, Input, Static

log = logging.getLogger(__name__)


# ---------------------------------------------------------------------------
# RPC Client
# ---------------------------------------------------------------------------


class RpcClient:
    """Async JSON-RPC client that communicates over a Unix domain socket.

    Uses the same length-prefixed framing as :class:`meept.comm.server.CommServer`::

        {length}\\n{json_payload}
    """

    def __init__(self, socket_path: str) -> None:
        self._socket_path = socket_path
        self._reader: asyncio.StreamReader | None = None
        self._writer: asyncio.StreamWriter | None = None
        self._lock = asyncio.Lock()

    async def connect(self) -> None:
        """Open a connection to the daemon socket."""
        self._reader, self._writer = await asyncio.open_unix_connection(
            self._socket_path,
        )

    async def close(self) -> None:
        """Close the underlying socket connection."""
        if self._writer is not None:
            try:
                self._writer.close()
                await self._writer.wait_closed()
            except Exception:
                pass
            self._writer = None
            self._reader = None

    @property
    def connected(self) -> bool:
        """Return ``True`` if the underlying transport is open."""
        return self._writer is not None and not self._writer.is_closing()

    async def call(
        self,
        method: str,
        params: dict[str, Any] | None = None,
        *,
        timeout: float = 120.0,
    ) -> dict[str, Any]:
        """Send a JSON-RPC request and return the parsed result.

        Raises
        ------
        ConnectionError
            If not connected or the connection drops.
        RuntimeError
            If the server returns a JSON-RPC error response.
        asyncio.TimeoutError
            If the server does not respond within *timeout* seconds.
        """
        if not self.connected:
            raise ConnectionError("Not connected to daemon")

        assert self._reader is not None
        assert self._writer is not None

        request_id = uuid.uuid4().hex[:12]
        payload: dict[str, Any] = {
            "jsonrpc": "2.0",
            "method": method,
            "id": request_id,
        }
        if params is not None:
            payload["params"] = params

        encoded = json.dumps(payload, separators=(",", ":")).encode("utf-8")

        async with self._lock:
            # Write length-prefixed frame.
            header = f"{len(encoded)}\n".encode("utf-8")
            self._writer.write(header + encoded)
            await self._writer.drain()

            # Read response frame.
            response = await asyncio.wait_for(
                self._read_frame(),
                timeout=timeout,
            )

        return self._parse_response(response, request_id)

    async def _read_frame(self) -> bytes:
        """Read a single length-prefixed frame from the stream."""
        assert self._reader is not None

        length_line = await self._reader.readline()
        if not length_line:
            raise ConnectionError("Connection closed by server")

        try:
            payload_length = int(length_line.strip())
        except ValueError as exc:
            raise ConnectionError(f"Invalid frame header: {length_line!r}") from exc

        return await self._reader.readexactly(payload_length)

    @staticmethod
    def _parse_response(data: bytes, expected_id: str) -> dict[str, Any]:
        """Decode a JSON-RPC response and return the result dict."""
        try:
            obj = json.loads(data)
        except json.JSONDecodeError as exc:
            raise RuntimeError(f"Malformed response JSON: {exc}") from exc

        if "error" in obj and obj["error"] is not None:
            err = obj["error"]
            code = err.get("code", -1)
            message = err.get("message", "Unknown error")
            err_data = err.get("data")
            detail = f"[{code}] {message}"
            if err_data is not None:
                detail += f" -- {err_data}"
            raise RuntimeError(detail)

        return obj.get("result", {})


# ---------------------------------------------------------------------------
# Chat message widget
# ---------------------------------------------------------------------------


class ChatMessage(Static):
    """A single chat message rendered in the scrollable history."""

    DEFAULT_CSS = """
    ChatMessage {
        padding: 0 1;
        margin: 0 0 1 0;
    }
    ChatMessage.user {
        color: $accent;
    }
    ChatMessage.assistant {
        color: $text;
    }
    ChatMessage.system {
        color: $text-muted;
        text-style: italic;
    }
    """


# ---------------------------------------------------------------------------
# Chat screen
# ---------------------------------------------------------------------------


class ChatScreen(Screen[None]):
    """Interactive chat screen with message history and input box."""

    BINDINGS = [
        Binding("d", "switch_screen('dashboard')", "Dashboard", show=True),
        Binding("m", "switch_screen('memory')", "Memory", show=True),
        Binding("t", "switch_screen('tasks')", "Tasks", show=True),
        Binding("q", "quit_app", "Quit", show=True, priority=True),
    ]

    DEFAULT_CSS = """
    ChatScreen {
        layout: vertical;
    }

    #chat-history {
        height: 1fr;
        border: solid $primary;
        padding: 1;
    }

    #chat-input {
        dock: bottom;
        margin: 1 0 0 0;
    }
    """

    def __init__(self, socket_path: str, **kwargs: object) -> None:
        super().__init__(**kwargs)
        self._socket_path = socket_path
        self._rpc: RpcClient = RpcClient(socket_path)
        self._conversation_id: str = uuid.uuid4().hex

    def compose(self) -> ComposeResult:
        yield Header()
        yield ScrollableContainer(id="chat-history")
        yield Input(placeholder="Type a message...", id="chat-input")
        yield Footer()

    async def on_mount(self) -> None:
        """Connect to the daemon when the screen mounts."""
        try:
            await self._rpc.connect()
            self._append_message("Connected to meept daemon.", role="system")
        except Exception as exc:
            self._append_message(
                f"Failed to connect: {exc}",
                role="system",
            )

    async def on_unmount(self) -> None:
        """Clean up the RPC connection."""
        await self._rpc.close()

    async def on_input_submitted(self, event: Input.Submitted) -> None:
        """Handle user pressing Enter in the input box."""
        text = event.value.strip()
        if not text:
            return

        # Clear the input.
        event.input.value = ""

        # Display the user message.
        self._append_message(text, role="user")

        # Send to daemon asynchronously.
        self._send_chat(text)

    @work(exclusive=True, thread=False)
    async def _send_chat(self, text: str) -> None:
        """Send a chat message to the daemon and display the response."""
        # Show a thinking indicator.
        thinking = self._append_message("thinking...", role="system")

        try:
            result = await self._rpc.call(
                "chat",
                {"message": text, "conversation_id": self._conversation_id},
            )
            reply = result.get("reply", "(no response)")
        except ConnectionError as exc:
            reply = f"Connection error: {exc}"
        except RuntimeError as exc:
            reply = f"Error: {exc}"
        except asyncio.TimeoutError:
            reply = "Request timed out."
        except Exception as exc:
            reply = f"Unexpected error: {exc}"

        # Remove the thinking indicator.
        thinking.remove()

        # Display the reply.
        self._append_message(reply, role="assistant")

    def _append_message(self, text: str, *, role: str) -> ChatMessage:
        """Add a message to the chat history and scroll to the bottom.

        Returns the widget so callers can manipulate it (e.g. remove it).
        """
        if role == "user":
            label = "You"
        elif role == "assistant":
            label = "Meept"
        else:
            label = ""

        if label:
            display_text = f"{label}: {text}"
        else:
            display_text = text

        widget = ChatMessage(display_text, classes=role)
        container = self.query_one("#chat-history", ScrollableContainer)
        container.mount(widget)
        widget.scroll_visible(animate=False)
        return widget

    def action_switch_screen(self, screen_name: str) -> None:
        """Navigate to another screen by name."""
        from cli.screens.dashboard import DashboardScreen
        from cli.screens.memory_browser import MemoryBrowserScreen
        from cli.screens.tasks import TasksScreen

        screen_map: dict[str, type[Screen]] = {
            "dashboard": DashboardScreen,
            "memory": MemoryBrowserScreen,
            "tasks": TasksScreen,
        }
        cls = screen_map.get(screen_name)
        if cls is not None:
            self.app.switch_screen(cls(socket_path=self._socket_path))

    def action_quit_app(self) -> None:
        """Quit the application."""
        self.app.exit()
