"""Entry point for ``meept-daemon`` / ``python -m meept``."""

from __future__ import annotations

import argparse
import os
import signal
import sys
import time


def _build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="meept-daemon",
        description="Meept autonomous bot daemon",
    )

    action = parser.add_mutually_exclusive_group()
    action.add_argument(
        "--start",
        action="store_true",
        default=False,
        help="Start the daemon (default action)",
    )
    action.add_argument(
        "--stop",
        action="store_true",
        default=False,
        help="Stop a running daemon via SIGTERM",
    )

    parser.add_argument(
        "--foreground",
        action="store_true",
        default=False,
        help="Run in foreground (skip daemonize); useful for service managers and debugging",
    )
    parser.add_argument(
        "--config",
        type=str,
        default=None,
        help="Path to meept.toml config file (default: ~/.meept/meept.toml)",
    )
    parser.add_argument(
        "--version",
        action="store_true",
        default=False,
        help="Print version and exit",
    )
    return parser


def _stop_daemon(*, config_path: str | None = None) -> None:
    """Stop a running meept daemon by sending SIGTERM via the PID file.

    Reads the PID file, verifies the process exists, sends SIGTERM,
    and polls briefly to confirm the process has exited.
    """
    from pathlib import Path

    from meept.core.config import MeeptConfig

    config = MeeptConfig(config_path)
    pid_path = Path(config.settings.daemon.pid_file).expanduser()

    if not pid_path.exists():
        print("No PID file found -- is the daemon running?", file=sys.stderr)
        sys.exit(1)

    try:
        pid = int(pid_path.read_text(encoding="utf-8").strip())
    except (ValueError, OSError) as exc:
        print(f"Failed to read PID file {pid_path}: {exc}", file=sys.stderr)
        sys.exit(1)

    # Verify process exists.
    try:
        os.kill(pid, 0)
    except ProcessLookupError:
        print(f"Process {pid} not found -- cleaning up stale PID file", file=sys.stderr)
        pid_path.unlink(missing_ok=True)
        _cleanup_socket(config)
        sys.exit(0)
    except PermissionError:
        print(f"No permission to signal process {pid}", file=sys.stderr)
        sys.exit(1)

    # Send SIGTERM.
    print(f"Sending SIGTERM to meept daemon (pid {pid})...", file=sys.stderr)
    os.kill(pid, signal.SIGTERM)

    # Poll for up to 5 seconds for the process to exit.
    for _ in range(50):
        time.sleep(0.1)
        try:
            os.kill(pid, 0)
        except ProcessLookupError:
            print("Daemon stopped", file=sys.stderr)
            # Clean up stale files if the daemon didn't get to it.
            pid_path.unlink(missing_ok=True)
            _cleanup_socket(config)
            return
        except PermissionError:
            break

    print(f"Daemon (pid {pid}) still running after SIGTERM -- check manually", file=sys.stderr)
    sys.exit(1)


def _cleanup_socket(config) -> None:  # noqa: ANN001
    """Remove the Unix socket file if it still exists."""
    from pathlib import Path

    sock_path = Path(config.settings.daemon.socket_path).expanduser()
    try:
        sock_path.unlink(missing_ok=True)
    except OSError:
        pass


def main() -> None:
    parser = _build_parser()
    args = parser.parse_args()

    if args.version:
        from meept import __version__

        print(f"meept {__version__}")
        sys.exit(0)

    if args.stop:
        _stop_daemon(config_path=args.config)
        sys.exit(0)

    # Default action: start the daemon.
    from meept.core.daemon import run

    run(daemon=not args.foreground, config_path=args.config)


if __name__ == "__main__":
    main()
