"""Textual TUI application for Meept."""

from __future__ import annotations

from textual.app import App, ComposeResult
from textual.binding import Binding

from cli.screens.chat import ChatScreen


class MeeptApp(App[None]):
    """Root application for the Meept interactive terminal UI.

    Parameters
    ----------
    socket_path:
        Filesystem path to the meept daemon's Unix domain socket.
    """

    TITLE = "Meept"
    CSS = """
    Screen {
        layout: vertical;
    }
    """
    BINDINGS = [
        Binding("q", "quit", "Quit", show=True, priority=True),
        Binding("ctrl+c", "quit", "Quit", show=False, priority=True),
    ]

    def __init__(self, socket_path: str, **kwargs: object) -> None:
        super().__init__(**kwargs)
        self.socket_path = socket_path

    def on_mount(self) -> None:
        """Push the chat screen once the app has mounted."""
        self.push_screen(ChatScreen(socket_path=self.socket_path))
