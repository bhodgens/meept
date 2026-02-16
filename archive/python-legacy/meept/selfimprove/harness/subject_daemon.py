"""Subject daemon controller.

Controls the subject daemon instance that is being tested/debugged
by the tester daemon.
"""

from __future__ import annotations

import asyncio
import logging
from pathlib import Path
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    pass

log = logging.getLogger(__name__)


class SubjectDaemon:
    """Controls the subject daemon instance.

    The subject daemon is the meept instance being tested and improved
    by the tester daemon.
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
        """Start the subject daemon."""
        if self._process is not None:
            log.warning("subject: already running")
            return

        log.info("subject: starting daemon with config %s", self._config_path)

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
        log.info("subject: daemon started (pid=%d)", self._pid)

    async def stop(self) -> None:
        """Stop the subject daemon."""
        if self._process is None:
            return

        log.info("subject: stopping daemon (pid=%d)", self._pid)

        # Try graceful shutdown first
        self._process.terminate()

        try:
            await asyncio.wait_for(self._process.wait(), timeout=5.0)
        except asyncio.TimeoutError:
            log.warning("subject: daemon did not stop gracefully, killing")
            self._process.kill()
            await self._process.wait()

        self._process = None
        self._pid = None
        log.info("subject: daemon stopped")

    async def restart(self) -> None:
        """Restart the subject daemon."""
        await self.stop()
        await asyncio.sleep(1.0)  # Brief pause
        await self.start()

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

    async def read_output(self, timeout: float = 1.0) -> str:
        """Read recent output from the daemon."""
        if self._process is None or self._process.stdout is None:
            return ""

        try:
            output = await asyncio.wait_for(
                self._process.stdout.read(4096),
                timeout=timeout,
            )
            return output.decode("utf-8", errors="replace")
        except asyncio.TimeoutError:
            return ""
