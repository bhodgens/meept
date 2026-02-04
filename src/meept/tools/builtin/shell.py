"""Sandboxed shell command execution tool."""

from __future__ import annotations

import asyncio
import logging
import shlex
from pathlib import Path
from typing import Any

from meept.security.permissions import RiskLevel
from meept.tools.interface import Tool, ToolDefinition, ToolParameter

log = logging.getLogger(__name__)

# Commands considered read-only (risk MEDIUM instead of HIGH).
_READ_ONLY_COMMANDS: frozenset[str] = frozenset({
    "ls", "cat", "head", "tail", "grep", "find", "wc", "du", "df",
    "file", "stat", "which", "whereis", "whoami", "hostname", "uname",
    "date", "uptime", "env", "printenv", "echo", "pwd", "id", "tree",
    "diff", "md5sum", "sha256sum", "shasum", "sort", "uniq", "tr",
    "cut", "awk", "sed",  # sed is borderline but useful for read pipelines
    "rg", "fd", "bat", "less", "more", "realpath", "basename", "dirname",
    "ps", "top", "htop", "free", "lsof", "netstat", "ss",
    "git", "python3", "python", "pip", "npm", "node", "cargo", "rustc",
    "go", "java", "javac", "make", "cmake",
})

# Commands that are always blocked (risk CRITICAL / hard deny).
_BLOCKED_COMMANDS: frozenset[str] = frozenset({
    "rm", "rmdir", "mkfs", "dd", "fdisk", "parted",
    "shutdown", "reboot", "halt", "poweroff", "init",
    "iptables", "ip6tables", "nft",
    "passwd", "useradd", "userdel", "usermod", "groupadd",
    "chown", "chmod",  # these may be needed -- but default-block for safety
    "mount", "umount",
    "kill", "killall", "pkill",
})


class ShellTool(Tool):
    """Execute shell commands in a sandboxed subprocess.

    Parameters
    ----------
    working_dir:
        The working directory for command execution.  Commands cannot
        escape this directory via ``cd`` in a meaningful way because each
        invocation starts fresh.
    default_timeout:
        Maximum wall-clock seconds before the process is terminated.
    """

    def __init__(
        self,
        working_dir: Path | None = None,
        default_timeout: float = 30.0,
        tirith_enabled: bool = False,
        tirith_binary: str = "tirith",
    ) -> None:
        self._working_dir = working_dir or Path.home()
        self._default_timeout = default_timeout
        self._tirith_enabled = tirith_enabled
        self._tirith_binary = tirith_binary

    def definition(self) -> ToolDefinition:
        return ToolDefinition(
            name="shell",
            description=(
                "Execute a shell command and return its stdout and stderr. "
                "Use for running system commands, scripts, and CLI tools. "
                "Commands run in a sandboxed subprocess with a timeout."
            ),
            parameters=[
                ToolParameter(
                    name="command",
                    type="string",
                    description="The shell command to execute.",
                ),
                ToolParameter(
                    name="timeout",
                    type="number",
                    description="Timeout in seconds (default 30).",
                    required=False,
                ),
                ToolParameter(
                    name="working_dir",
                    type="string",
                    description="Working directory for the command (optional).",
                    required=False,
                ),
            ],
        )

    async def execute(self, **kwargs: Any) -> dict[str, Any]:
        command: str = kwargs.get("command", "")
        timeout: float = kwargs.get("timeout", self._default_timeout)
        working_dir_str: str | None = kwargs.get("working_dir")

        if not command.strip():
            return {"success": False, "result": None, "error": "Empty command"}

        working_dir = Path(working_dir_str) if working_dir_str else self._working_dir
        if not working_dir.is_dir():
            return {
                "success": False,
                "result": None,
                "error": f"Working directory does not exist: {working_dir}",
            }

        risk = self._classify_risk(command)
        if risk == RiskLevel.CRITICAL:
            return {
                "success": False,
                "result": None,
                "error": f"Command blocked for safety: {command.split()[0]}",
            }

        # Tirith pre-execution security scan.
        if self._tirith_enabled:
            from meept.security.tirith import scan_command

            scan_result = await scan_command(command, self._tirith_binary)
            if scan_result is not None:
                if scan_result.blocked:
                    log.warning("Tirith blocked command: %s", scan_result.message)
                    return {
                        "success": False,
                        "result": None,
                        "error": f"Blocked by tirith: {scan_result.message}",
                    }
                elif scan_result.warning:
                    log.warning("Tirith warning (continuing): %s", scan_result.message)

        log.info("Executing shell command (risk=%s): %s", risk.value, command)

        try:
            process = await asyncio.create_subprocess_exec(
                "/bin/sh", "-c", command,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
                cwd=str(working_dir),
            )

            try:
                stdout_bytes, stderr_bytes = await asyncio.wait_for(
                    process.communicate(),
                    timeout=timeout,
                )
            except asyncio.TimeoutError:
                process.kill()
                await process.wait()
                return {
                    "success": False,
                    "result": None,
                    "error": f"Command timed out after {timeout}s",
                }

            stdout = stdout_bytes.decode("utf-8", errors="replace").strip()
            stderr = stderr_bytes.decode("utf-8", errors="replace").strip()
            return_code = process.returncode

            # Truncate very large outputs
            max_output = 50_000
            if len(stdout) > max_output:
                stdout = stdout[:max_output] + f"\n... (truncated, {len(stdout)} bytes total)"
            if len(stderr) > max_output:
                stderr = stderr[:max_output] + f"\n... (truncated, {len(stderr)} bytes total)"

            success = return_code == 0
            output_parts: list[str] = []
            if stdout:
                output_parts.append(stdout)
            if stderr:
                output_parts.append(f"[stderr]\n{stderr}")

            return {
                "success": success,
                "result": "\n".join(output_parts) if output_parts else "(no output)",
                "return_code": return_code,
                "error": None if success else f"Exit code {return_code}",
            }

        except OSError as exc:
            log.error("Failed to execute command: %s", exc)
            return {"success": False, "result": None, "error": str(exc)}

    # ------------------------------------------------------------------
    # Risk classification
    # ------------------------------------------------------------------

    @staticmethod
    def _classify_risk(command: str) -> RiskLevel:
        """Classify a command's risk level based on pattern matching.

        Returns
        -------
        RiskLevel
            MEDIUM for read-only commands, HIGH for write commands,
            CRITICAL for blocked commands.
        """
        command = command.strip()
        if not command:
            return RiskLevel.SAFE

        # Extract the base command (first token, ignoring env vars).
        try:
            tokens = shlex.split(command)
        except ValueError:
            # Malformed quoting -- treat as HIGH.
            return RiskLevel.HIGH

        # Skip leading environment variable assignments (FOO=bar cmd ...).
        base_cmd = ""
        for token in tokens:
            if "=" in token and not token.startswith("-"):
                continue
            base_cmd = token
            break

        if not base_cmd:
            return RiskLevel.MEDIUM

        # Resolve to basename (strip path).
        base_name = Path(base_cmd).name

        if base_name in _BLOCKED_COMMANDS:
            return RiskLevel.CRITICAL

        if base_name in _READ_ONLY_COMMANDS:
            return RiskLevel.MEDIUM

        # Check for pipe chains -- look at each command in the pipeline.
        if "|" in command:
            # Re-check each segment of the pipeline.
            segments = command.split("|")
            risk = RiskLevel.MEDIUM
            for segment in segments:
                segment_risk = ShellTool._classify_risk(segment.strip())
                if segment_risk > risk:
                    risk = segment_risk
            return risk

        # Check for sudo prefix.
        if base_name == "sudo":
            return RiskLevel.CRITICAL

        # Default: HIGH for unknown commands.
        return RiskLevel.HIGH

    def get_risk_level(self, command: str) -> RiskLevel:
        """Public accessor for risk classification (used by executor)."""
        return self._classify_risk(command)
