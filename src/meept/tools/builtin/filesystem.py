"""File system operation tools with path-based security checks."""

from __future__ import annotations

import logging
import os
from pathlib import Path
from typing import Any

from meept.security.permissions import PermissionManager, RiskLevel
from meept.tools.interface import Tool, ToolDefinition, ToolParameter

log = logging.getLogger(__name__)

# Maximum file size we will read into memory (5 MB).
_MAX_READ_SIZE = 5 * 1024 * 1024

# Maximum file size we will write (10 MB).
_MAX_WRITE_SIZE = 10 * 1024 * 1024


# ---------------------------------------------------------------------------
# File Read
# ---------------------------------------------------------------------------


class FileReadTool(Tool):
    """Read the contents of a file.

    Parameters
    ----------
    permission_manager:
        Used to validate the target path against allowed/blocked patterns.
    """

    def __init__(self, permission_manager: PermissionManager) -> None:
        self._pm = permission_manager

    def definition(self) -> ToolDefinition:
        return ToolDefinition(
            name="file_read",
            description=(
                "Read the contents of a file at the given path. "
                "Returns the text content. Optionally read a specific line range."
            ),
            parameters=[
                ToolParameter(
                    name="path",
                    type="string",
                    description="Absolute or ~-prefixed path to the file.",
                ),
                ToolParameter(
                    name="offset",
                    type="integer",
                    description="Line number to start reading from (1-based, optional).",
                    required=False,
                ),
                ToolParameter(
                    name="limit",
                    type="integer",
                    description="Maximum number of lines to read (optional).",
                    required=False,
                ),
            ],
        )

    async def execute(self, **kwargs: Any) -> dict[str, Any]:
        raw_path: str = kwargs.get("path", "")
        offset: int | None = kwargs.get("offset")
        limit: int | None = kwargs.get("limit")

        if not raw_path:
            return {"success": False, "result": None, "error": "No path specified"}

        resolved = Path(raw_path).expanduser().resolve()

        if not self._pm.check_path(str(resolved)):
            return {
                "success": False,
                "result": None,
                "error": f"Access denied: {resolved}",
            }

        if not resolved.is_file():
            return {
                "success": False,
                "result": None,
                "error": f"File not found: {resolved}",
            }

        file_size = resolved.stat().st_size
        if file_size > _MAX_READ_SIZE:
            return {
                "success": False,
                "result": None,
                "error": f"File too large ({file_size} bytes, max {_MAX_READ_SIZE})",
            }

        try:
            text = resolved.read_text(encoding="utf-8", errors="replace")
        except OSError as exc:
            return {"success": False, "result": None, "error": str(exc)}

        # Apply line range if requested.
        if offset is not None or limit is not None:
            lines = text.splitlines(keepends=True)
            start = max(0, (offset or 1) - 1)
            end = start + limit if limit else len(lines)
            text = "".join(lines[start:end])

        return {"success": True, "result": text, "error": None}


# ---------------------------------------------------------------------------
# File Write
# ---------------------------------------------------------------------------


class FileWriteTool(Tool):
    """Write content to a file (create or overwrite).

    Parameters
    ----------
    permission_manager:
        Used to validate the target path.
    """

    def __init__(self, permission_manager: PermissionManager) -> None:
        self._pm = permission_manager

    def definition(self) -> ToolDefinition:
        return ToolDefinition(
            name="file_write",
            description=(
                "Write text content to a file. Creates the file if it does not "
                "exist, overwrites if it does. Parent directories are created "
                "automatically."
            ),
            parameters=[
                ToolParameter(
                    name="path",
                    type="string",
                    description="Absolute or ~-prefixed path to the file.",
                ),
                ToolParameter(
                    name="content",
                    type="string",
                    description="The text content to write.",
                ),
                ToolParameter(
                    name="append",
                    type="boolean",
                    description="If true, append instead of overwrite (default false).",
                    required=False,
                ),
            ],
        )

    async def execute(self, **kwargs: Any) -> dict[str, Any]:
        raw_path: str = kwargs.get("path", "")
        content: str = kwargs.get("content", "")
        append: bool = kwargs.get("append", False)

        if not raw_path:
            return {"success": False, "result": None, "error": "No path specified"}

        if len(content.encode("utf-8")) > _MAX_WRITE_SIZE:
            return {
                "success": False,
                "result": None,
                "error": f"Content too large (max {_MAX_WRITE_SIZE} bytes)",
            }

        resolved = Path(raw_path).expanduser().resolve()

        if not self._pm.check_path(str(resolved)):
            return {
                "success": False,
                "result": None,
                "error": f"Access denied: {resolved}",
            }

        try:
            resolved.parent.mkdir(parents=True, exist_ok=True)
            mode = "a" if append else "w"
            with resolved.open(mode, encoding="utf-8") as fh:
                fh.write(content)
        except OSError as exc:
            return {"success": False, "result": None, "error": str(exc)}

        action = "appended to" if append else "wrote"
        log.info("FileWrite: %s %s (%d bytes)", action, resolved, len(content))
        return {
            "success": True,
            "result": f"Successfully {action} {resolved}",
            "error": None,
        }


# ---------------------------------------------------------------------------
# File Delete
# ---------------------------------------------------------------------------


class FileDeleteTool(Tool):
    """Delete a file from the filesystem.

    Parameters
    ----------
    permission_manager:
        Used to validate the target path.
    """

    def __init__(self, permission_manager: PermissionManager) -> None:
        self._pm = permission_manager

    def definition(self) -> ToolDefinition:
        return ToolDefinition(
            name="file_delete",
            description=(
                "Delete a file at the given path. This is a destructive operation "
                "and cannot be undone."
            ),
            parameters=[
                ToolParameter(
                    name="path",
                    type="string",
                    description="Absolute or ~-prefixed path to the file to delete.",
                ),
            ],
        )

    async def execute(self, **kwargs: Any) -> dict[str, Any]:
        raw_path: str = kwargs.get("path", "")

        if not raw_path:
            return {"success": False, "result": None, "error": "No path specified"}

        resolved = Path(raw_path).expanduser().resolve()

        if not self._pm.check_path(str(resolved)):
            return {
                "success": False,
                "result": None,
                "error": f"Access denied: {resolved}",
            }

        if not resolved.exists():
            return {
                "success": False,
                "result": None,
                "error": f"File not found: {resolved}",
            }

        if resolved.is_dir():
            return {
                "success": False,
                "result": None,
                "error": f"Path is a directory, not a file: {resolved}",
            }

        try:
            resolved.unlink()
        except OSError as exc:
            return {"success": False, "result": None, "error": str(exc)}

        log.info("FileDelete: removed %s", resolved)
        return {
            "success": True,
            "result": f"Successfully deleted {resolved}",
            "error": None,
        }


# ---------------------------------------------------------------------------
# List Directory
# ---------------------------------------------------------------------------


class ListDirectoryTool(Tool):
    """List the contents of a directory.

    Parameters
    ----------
    permission_manager:
        Used to validate the target path.
    """

    def __init__(self, permission_manager: PermissionManager) -> None:
        self._pm = permission_manager

    def definition(self) -> ToolDefinition:
        return ToolDefinition(
            name="list_directory",
            description=(
                "List files and directories at the given path. "
                "Returns names, types, and sizes."
            ),
            parameters=[
                ToolParameter(
                    name="path",
                    type="string",
                    description="Absolute or ~-prefixed path to the directory.",
                ),
                ToolParameter(
                    name="recursive",
                    type="boolean",
                    description="If true, list recursively (default false).",
                    required=False,
                ),
                ToolParameter(
                    name="max_entries",
                    type="integer",
                    description="Maximum number of entries to return (default 200).",
                    required=False,
                ),
            ],
        )

    async def execute(self, **kwargs: Any) -> dict[str, Any]:
        raw_path: str = kwargs.get("path", "")
        recursive: bool = kwargs.get("recursive", False)
        max_entries: int = kwargs.get("max_entries", 200)

        if not raw_path:
            return {"success": False, "result": None, "error": "No path specified"}

        resolved = Path(raw_path).expanduser().resolve()

        if not self._pm.check_path(str(resolved)):
            return {
                "success": False,
                "result": None,
                "error": f"Access denied: {resolved}",
            }

        if not resolved.is_dir():
            return {
                "success": False,
                "result": None,
                "error": f"Not a directory: {resolved}",
            }

        entries: list[dict[str, Any]] = []
        try:
            iterator = resolved.rglob("*") if recursive else resolved.iterdir()
            for item in iterator:
                if len(entries) >= max_entries:
                    break
                try:
                    stat = item.stat()
                    entry_type = "directory" if item.is_dir() else "file"
                    rel = item.relative_to(resolved)
                    entries.append({
                        "name": str(rel),
                        "type": entry_type,
                        "size": stat.st_size if item.is_file() else None,
                    })
                except OSError:
                    # Permission denied or broken symlink -- skip.
                    continue
        except OSError as exc:
            return {"success": False, "result": None, "error": str(exc)}

        truncated = len(entries) >= max_entries
        return {
            "success": True,
            "result": {
                "path": str(resolved),
                "entries": entries,
                "count": len(entries),
                "truncated": truncated,
            },
            "error": None,
        }
