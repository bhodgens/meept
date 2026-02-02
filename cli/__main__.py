"""Entry point for the Meept TUI client (``python -m cli`` / ``meept-cli``)."""

from __future__ import annotations

import argparse
import os
import sys


def _build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="meept-cli",
        description="Meept interactive TUI client",
    )
    default_socket = os.path.join(os.path.expanduser("~"), ".meept", "meept.sock")
    parser.add_argument(
        "--socket",
        type=str,
        default=default_socket,
        help=f"Path to daemon Unix socket (default: {default_socket})",
    )
    return parser


def main() -> None:
    """Parse arguments and launch the Textual TUI."""
    parser = _build_parser()
    args = parser.parse_args()

    socket_path: str = args.socket

    # Verify the socket file exists before launching the TUI.
    if not os.path.exists(socket_path):
        print(
            f"Error: daemon socket not found at {socket_path}\n"
            "Is the meept daemon running?  Start it with: meept --daemon",
            file=sys.stderr,
        )
        sys.exit(1)

    try:
        from cli.app import MeeptApp
    except ImportError as exc:
        print(
            f"Error: could not import TUI dependencies: {exc}\n"
            "Install the cli extras:  pip install meept[cli]",
            file=sys.stderr,
        )
        sys.exit(1)

    app = MeeptApp(socket_path=socket_path)
    app.run()


if __name__ == "__main__":
    main()
