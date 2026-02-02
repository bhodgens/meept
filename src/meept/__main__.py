"""Entry point for ``python -m meept``."""

from __future__ import annotations

import argparse
import sys


def _build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="meept",
        description="Meept autonomous bot daemon",
    )
    parser.add_argument(
        "--daemon",
        action="store_true",
        default=False,
        help="Fork into background (double-fork daemonize)",
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


def main() -> None:
    parser = _build_parser()
    args = parser.parse_args()

    if args.version:
        from meept import __version__

        print(f"meept {__version__}")
        sys.exit(0)

    from meept.core.daemon import run

    run(daemon=args.daemon, config_path=args.config)


if __name__ == "__main__":
    main()
