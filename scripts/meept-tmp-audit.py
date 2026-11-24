#!/usr/bin/env python3
"""Classify (and optionally clean) /tmp/meept* scratch entries.

Read-only by default. ``--clean`` is interactive and guarded by age, liveness,
and a protective allowlist. Standard library only.
"""

from __future__ import annotations

import argparse
import glob
import json
import os
import platform
import shutil
import stat
import subprocess
import sys
import time

PATTERN = "/tmp/meept*"
MIN_AGE_DAYS = 7
PROTECTED_FRAGMENT = "meept-head-check"
PROTECTED_SUFFIXES = (".log", ".txt")
SECONDS_PER_DAY = 86400


def human(size: int) -> str:
    """Render a byte count as a compact human-readable string."""
    value = float(size)
    for unit in ("B", "KiB", "MiB", "GiB", "TiB"):
        if abs(value) < 1024.0 or unit == "TiB":
            return f"{int(value)} {unit}" if unit == "B" else f"{value:.1f} {unit}"
        value /= 1024.0
    return f"{value:.1f} TiB"


def running_command_lines() -> list[str]:
    """Return current process command lines; empty list if ps is unavailable."""
    args = ["ps", "-axo", "command="] if platform.system() == "Darwin" else ["ps", "-eo", "args="]
    try:
        proc = subprocess.run(
            args,
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            text=True,
            check=False,
            timeout=15,
        )
        return [line for line in proc.stdout.splitlines() if line.strip()]
    except (OSError, subprocess.SubprocessError) as exc:
        print(f"warning: cannot inspect processes: {exc}", file=sys.stderr)
        return []


def dir_size(path: str) -> int:
    """Sum file sizes under a directory without following symlinks."""
    total = 0

    def onerror(exc: OSError) -> None:
        print(f"warning: {exc}", file=sys.stderr)

    for root, dirs, files in os.walk(path, followlinks=False, onerror=onerror):
        # os.walk lists symlinks-to-directories in `dirs` but does not descend
        # when followlinks=False; count the link itself like any other file.
        for name in dirs:
            link = os.path.join(root, name)
            if os.path.islink(link):
                try:
                    total += os.lstat(link).st_size
                except OSError:
                    pass
        for name in files:
            entry = os.path.join(root, name)
            try:
                total += os.lstat(entry).st_size
            except OSError:
                pass
    return total


def collect() -> list[dict]:
    """Build the classified entry list, sorted by size descending."""
    now = time.time()
    commands = running_command_lines()
    entries: list[dict] = []
    for path in sorted(glob.glob(PATTERN)):
        try:
            st = os.lstat(path)
        except OSError as exc:
            print(f"warning: skipping {path}: {exc}", file=sys.stderr)
            continue
        is_link = stat.S_ISLNK(st.st_mode)
        is_dir = stat.S_ISDIR(st.st_mode) and not is_link
        try:
            size = dir_size(path) if is_dir else st.st_size
        except OSError as exc:
            print(f"warning: sizing {path}: {exc}", file=sys.stderr)
            size = st.st_size
        age_days = max(0.0, (now - st.st_mtime) / SECONDS_PER_DAY)
        absolute = os.path.abspath(path)
        live = any(absolute in line for line in commands)
        name = os.path.basename(path)
        protected = PROTECTED_FRAGMENT in name or name.endswith(PROTECTED_SUFFIXES)
        entries.append(
            {
                "path": path,
                "size_bytes": size,
                "size_human": human(size),
                "age_days": round(age_days, 1),
                "state": "LIVE" if live else "idle",
                "live": live,
                "is_dir": is_dir,
                "is_symlink": is_link,
                "protected": protected,
                "eligible": age_days >= MIN_AGE_DAYS and not live and not protected,
            }
        )
    entries.sort(key=lambda item: (-item["size_bytes"], item["path"]))
    return entries


def print_table(entries: list[dict]) -> None:
    for item in entries:
        print(f"{item['size_human']}\t{item['age_days']:.1f}\t{item['state']}\t{item['path']}")
    total = sum(item["size_bytes"] for item in entries)
    print(f"TOTAL\t{len(entries)} entries\t{total} bytes ({human(total)})")


def clean(entries: list[dict]) -> int:
    print_table(entries)
    targets = [item for item in entries if item["eligible"]]
    if not sys.stdin.isatty():
        print(f"DRY RUN: stdin is not a TTY; would remove {len(targets)} entries:")
        for item in targets:
            print(f"  {item['path']}")
        return 0
    if not targets:
        print("nothing to remove")
        return 0
    try:
        answer = input(f"Remove {len(targets)} entries? [y/N] ")
    except EOFError:
        print("no confirmation; nothing removed")
        return 0
    if answer.strip().lower() not in ("y", "yes"):
        print("aborted; nothing removed")
        return 0
    removed = 0
    for item in targets:
        path = item["path"]
        try:
            # Re-check at deletion time so a symlink is never traversed.
            if os.path.islink(path) or not os.path.isdir(path):
                os.unlink(path)
            else:
                shutil.rmtree(path)
            print(f"removed {path}")
            removed += 1
        except OSError as exc:
            print(f"error removing {path}: {exc}", file=sys.stderr)
    print(f"removed {removed} of {len(targets)} entries")
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--status", action="store_true", help="report only (default)")
    mode.add_argument("--clean", action="store_true", help="interactive guarded cleanup")
    mode.add_argument("--json", action="store_true", help="emit entry data as JSON")
    args = parser.parse_args()

    entries = collect()
    if args.json:
        json.dump(entries, sys.stdout, indent=2)
        sys.stdout.write("\n")
        return 0
    if args.clean:
        return clean(entries)
    print_table(entries)
    return 0


if __name__ == "__main__":
    sys.exit(main())
