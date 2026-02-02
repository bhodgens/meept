"""CLI subcommand handlers for ``meept clawskills``.

Works without the daemon running -- uses ``asyncio.run()`` to execute
async :class:`ClawHubClient` methods directly.
"""

from __future__ import annotations

import argparse
import asyncio
import sys
from pathlib import Path
from typing import Sequence

from meept.clawskills.client import ClawHubClient, ClawHubAPIError
from meept.clawskills.installer import ClawSkillInstaller, ArchiveValidationError
from meept.clawskills.models import LockFile


# ---------------------------------------------------------------------------
# Config helpers
# ---------------------------------------------------------------------------

def _load_settings() -> tuple[str, Path]:
    """Return (registry_url, install_dir) from config, falling back to defaults."""
    try:
        from meept.core.config import MeeptConfig
        config = MeeptConfig()
        settings = config.settings.clawskills
        return settings.registry_url, settings.expanded_path(settings.install_dir) if hasattr(settings, "expanded_path") else Path(settings.install_dir).expanduser()
    except Exception:
        return "https://clawhub.ai", Path("~/.meept/clawskills").expanduser()


def _get_registry_url_and_dir(args: argparse.Namespace) -> tuple[str, Path]:
    """Extract registry URL and install dir, preferring CLI flags over config."""
    registry_url, install_dir = _load_settings()
    if hasattr(args, "registry") and args.registry:
        registry_url = args.registry
    return registry_url, install_dir


# ---------------------------------------------------------------------------
# Subcommand handlers
# ---------------------------------------------------------------------------

async def _cmd_search(args: argparse.Namespace) -> None:
    registry_url, _ = _get_registry_url_and_dir(args)
    client = ClawHubClient(base_url=registry_url)
    try:
        results = await client.search(args.query, limit=args.limit)
        if not results:
            print("No skills found.")
            return

        # Table header.
        print(f"{'Slug':<30} {'Description':<50}")
        print("-" * 80)
        for item in results:
            slug = item.get("slug", item.get("name", "?"))
            desc = item.get("description", "")[:50]
            print(f"{slug:<30} {desc:<50}")
    finally:
        await client.close()


async def _cmd_install(args: argparse.Namespace) -> None:
    registry_url, install_dir = _get_registry_url_and_dir(args)
    client = ClawHubClient(base_url=registry_url)
    installer = ClawSkillInstaller(base_dir=install_dir, client=client)
    try:
        origin = await installer.install(args.slug, version=args.version)
        print(f"Installed {origin.slug}@{origin.version}")
        print(f"  SHA-256: {origin.sha256}")
        print(f"  Files:   {len(origin.files)}")
    except ArchiveValidationError as exc:
        print(f"Security error: {exc}", file=sys.stderr)
        sys.exit(1)
    except ClawHubAPIError as exc:
        print(f"API error: {exc}", file=sys.stderr)
        sys.exit(1)
    finally:
        await client.close()


async def _cmd_update(args: argparse.Namespace) -> None:
    registry_url, install_dir = _get_registry_url_and_dir(args)
    client = ClawHubClient(base_url=registry_url)
    installer = ClawSkillInstaller(base_dir=install_dir, client=client)
    try:
        if args.all:
            updated = await installer.update_all()
            if updated:
                print(f"Updated: {', '.join(updated)}")
            else:
                print("All clawskills are up to date.")
        elif args.slug:
            result = await installer.update(args.slug)
            if result:
                print(f"Updated {result.slug}@{result.version}")
            else:
                print(f"{args.slug} is already up to date.")
        else:
            print("Specify a slug or --all", file=sys.stderr)
            sys.exit(1)
    except ClawHubAPIError as exc:
        print(f"API error: {exc}", file=sys.stderr)
        sys.exit(1)
    finally:
        await client.close()


async def _cmd_list(args: argparse.Namespace) -> None:
    _, install_dir = _get_registry_url_and_dir(args)
    lock_path = install_dir / ".lock.json"
    lock = LockFile.load(lock_path)

    if not lock.entries:
        print("No clawskills installed.")
        return

    print(f"{'Slug':<30} {'Version':<15} {'Installed':<25}")
    print("-" * 70)
    for entry in sorted(lock.entries.values(), key=lambda e: e.slug):
        print(f"{entry.slug:<30} {entry.version:<15} {entry.installed_at:<25}")


async def _cmd_inspect(args: argparse.Namespace) -> None:
    registry_url, _ = _get_registry_url_and_dir(args)
    client = ClawHubClient(base_url=registry_url)
    try:
        detail = await client.skill_detail(args.slug)
        print(f"Slug:        {detail.get('slug', detail.get('name', '?'))}")
        print(f"Description: {detail.get('description', '')}")
        print(f"Author:      {detail.get('author', detail.get('owner', ''))}")
        print(f"Version:     {detail.get('version', detail.get('latest_version', ''))}")
        print(f"Downloads:   {detail.get('downloads', '?')}")

        versions = await client.skill_versions(args.slug)
        if versions:
            print(f"\nVersions ({len(versions)}):")
            for v in versions[:10]:
                ver = v.get("version", v.get("tag", "?"))
                date = v.get("created_at", v.get("date", ""))
                print(f"  {ver:<15} {date}")
    except ClawHubAPIError as exc:
        print(f"API error: {exc}", file=sys.stderr)
        sys.exit(1)
    finally:
        await client.close()


async def _cmd_info(args: argparse.Namespace) -> None:
    _, install_dir = _get_registry_url_and_dir(args)
    from meept.clawskills.models import OriginMetadata

    origin_path = install_dir / args.slug / ".origin.json"
    if not origin_path.is_file():
        print(f"Clawskill {args.slug!r} is not installed.", file=sys.stderr)
        sys.exit(1)

    origin = OriginMetadata.load(origin_path)
    print(f"Slug:        {origin.slug}")
    print(f"Version:     {origin.version}")
    print(f"SHA-256:     {origin.sha256}")
    print(f"Installed:   {origin.installed_at}")
    print(f"Source URL:  {origin.source_url}")
    print(f"Files:       {len(origin.files)}")
    for f in origin.files:
        print(f"  {f}")


async def _cmd_remove(args: argparse.Namespace) -> None:
    registry_url, install_dir = _get_registry_url_and_dir(args)
    client = ClawHubClient(base_url=registry_url)
    installer = ClawSkillInstaller(base_dir=install_dir, client=client)
    try:
        installer.remove(args.slug)
        print(f"Removed {args.slug}")
    finally:
        await client.close()


# ---------------------------------------------------------------------------
# Parser / dispatch
# ---------------------------------------------------------------------------


def _build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="meept clawskills",
        description="Manage third-party skills from ClawHub",
    )
    parser.add_argument(
        "--registry",
        type=str,
        default=None,
        help="Override ClawHub registry URL",
    )
    sub = parser.add_subparsers(dest="command")

    # search
    p_search = sub.add_parser("search", help="Search ClawHub for skills")
    p_search.add_argument("query", help="Search query")
    p_search.add_argument("--limit", type=int, default=20)

    # install
    p_install = sub.add_parser("install", help="Install a skill from ClawHub")
    p_install.add_argument("slug", help="Skill slug")
    p_install.add_argument("--version", default=None, help="Specific version")

    # update
    p_update = sub.add_parser("update", help="Update installed skill(s)")
    p_update.add_argument("slug", nargs="?", default=None, help="Skill slug")
    p_update.add_argument("--all", action="store_true", help="Update all")

    # list
    sub.add_parser("list", help="List installed clawskills")

    # inspect
    p_inspect = sub.add_parser("inspect", help="View remote skill detail")
    p_inspect.add_argument("slug", help="Skill slug")

    # info
    p_info = sub.add_parser("info", help="View installed skill detail")
    p_info.add_argument("slug", help="Skill slug")

    # remove
    p_remove = sub.add_parser("remove", help="Remove an installed skill")
    p_remove.add_argument("slug", help="Skill slug")

    return parser


_DISPATCH = {
    "search": _cmd_search,
    "install": _cmd_install,
    "update": _cmd_update,
    "list": _cmd_list,
    "inspect": _cmd_inspect,
    "info": _cmd_info,
    "remove": _cmd_remove,
}


def handle_clawskills(argv: Sequence[str] | None = None) -> None:
    """Entry point called from ``cli/__main__.py``.

    Parses arguments and dispatches to the appropriate async handler.
    """
    parser = _build_parser()
    args = parser.parse_args(argv)

    if not args.command:
        parser.print_help()
        sys.exit(0)

    handler = _DISPATCH.get(args.command)
    if handler is None:
        parser.print_help()
        sys.exit(1)

    asyncio.run(handler(args))
