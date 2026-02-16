"""Tester daemon controller.

Controls the tester daemon instance that performs self-improvement
analysis and generates fixes for the subject daemon.
"""

from __future__ import annotations

import asyncio
import logging
import os
import signal
from pathlib import Path
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from meept.selfimprove.config import SelfImproveConfig

log = logging.getLogger(__name__)


class TesterDaemon:
    """Controls the tester daemon instance.

    The tester daemon runs with ai-infra LLM access and performs
    self-improvement analysis on the subject daemon.
    """

    def __init__(
        self,
        config_path: Path | str,
        project_root: Path | str | None = None,
    ) -> None:
        self._config_path = Path(config_path)
        self._project_root = Path(project_root) if project_root else Path.cwd()
        self._process: asyncio.subprocess.Process | None = None
        self._pid: int | None = None

    async def start(self) -> None:
        """Start the tester daemon."""
        if self._process is not None:
            log.warning("tester: already running")
            return

        log.info("tester: starting daemon with config %s", self._config_path)

        self._process = await asyncio.create_subprocess_exec(
            "python",
            "-m",
            "meept",
            "--config",
            str(self._config_path),
            cwd=str(self._project_root),
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.STDOUT,
        )

        self._pid = self._process.pid
        log.info("tester: daemon started (pid=%d)", self._pid)

    async def stop(self) -> None:
        """Stop the tester daemon."""
        if self._process is None:
            return

        log.info("tester: stopping daemon (pid=%d)", self._pid)

        # Try graceful shutdown first
        self._process.terminate()

        try:
            await asyncio.wait_for(self._process.wait(), timeout=5.0)
        except asyncio.TimeoutError:
            log.warning("tester: daemon did not stop gracefully, killing")
            self._process.kill()
            await self._process.wait()

        self._process = None
        self._pid = None
        log.info("tester: daemon stopped")

    async def wait(self) -> int:
        """Wait for the daemon to exit."""
        if self._process is None:
            return -1
        return await self._process.wait()

    @property
    def is_running(self) -> bool:
        """Check if the daemon is running."""
        if self._process is None:
            return False
        return self._process.returncode is None

    @property
    def pid(self) -> int | None:
        """Get the daemon's PID."""
        return self._pid
