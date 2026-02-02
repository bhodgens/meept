"""Daemon lifecycle manager for meept.

Provides :class:`MeeptDaemon` (async start/stop) and the top-level
:func:`run` helper that ``__main__`` calls.
"""

from __future__ import annotations

import asyncio
import logging
import os
import signal
import sys
from pathlib import Path

from meept.core.bus import MessageBus
from meept.core.config import MeeptConfig
from meept.core.registry import Registry
from meept.models.messages import BusMessage, MessageType

log = logging.getLogger(__name__)


class MeeptDaemon:
    """Manages the full lifecycle of the meept daemon process.

    The daemon owns the asyncio event loop and orchestrates startup and
    teardown of every subsystem via the component :class:`Registry`.
    """

    def __init__(self, config: MeeptConfig) -> None:
        self._config = config
        self._registry = Registry()
        self._bus = MessageBus()
        self._running = False
        self._shutdown_event = asyncio.Event()

        # Seed the registry with the core singletons so that downstream
        # components can look them up by name.
        self._registry.register_instance("config", self._config)
        self._registry.register_instance("bus", self._bus)
        self._registry.register_instance("registry", self._registry)
        self._registry.register_instance("daemon", self)

    # ------------------------------------------------------------------
    # Public lifecycle
    # ------------------------------------------------------------------

    async def start(self) -> None:
        """Initialise all subsystems and enter the main loop."""
        if self._running:
            log.warning("daemon: start() called but already running")
            return

        self._running = True
        log.info("daemon: starting meept (pid=%d)", os.getpid())

        # --- core infrastructure ---
        await self._bus.start()

        # Subscribe to internal control messages.
        self._bus.subscribe("control.shutdown", self._on_shutdown)
        self._bus.subscribe("control.config_reload", self._on_config_reload)

        # --- optional subsystems (best-effort) ---
        await self._start_optional("comm_server")
        await self._start_optional("agent_loop")
        await self._start_optional("scheduler")

        # --- skills subsystem (conditional on skills.enabled) ---
        await self._start_skills()

        log.info("daemon: ready")

        # Block until something requests shutdown.
        await self._shutdown_event.wait()

    async def stop(self) -> None:
        """Gracefully shut down every subsystem in reverse order."""
        if not self._running:
            return
        self._running = False
        log.info("daemon: shutting down")

        # Broadcast shutdown so any listener can do last-second work.
        await self._bus.publish(
            "control.shutdown",
            BusMessage(
                type=MessageType.SHUTDOWN,
                payload={},
                source="daemon",
            ),
        )

        # Tear down optional subsystems (ignore missing).
        for name in ("skill_dispatcher", "scheduler", "agent_loop", "comm_server"):
            component = self._registry.get(name)
            if component is not None and hasattr(component, "stop"):
                try:
                    result = component.stop()
                    if asyncio.iscoroutine(result):
                        await result
                except Exception:
                    log.exception("daemon: error stopping %s", name)

        await self._bus.stop()
        self._cleanup_pid_file()
        log.info("daemon: stopped")

    # ------------------------------------------------------------------
    # Signal / bus handlers
    # ------------------------------------------------------------------

    async def _on_shutdown(self, _topic: str, _msg: BusMessage) -> None:
        self._shutdown_event.set()

    async def _on_config_reload(self, _topic: str, _msg: BusMessage) -> None:
        log.info("daemon: reloading configuration")
        self._config.reload()

    def _handle_signal(self, signum: int) -> None:
        name = signal.Signals(signum).name
        log.info("daemon: received %s", name)
        self._shutdown_event.set()

    # ------------------------------------------------------------------
    # Helpers
    # ------------------------------------------------------------------

    async def _start_skills(self) -> None:
        """Initialise the skills subsystem if enabled in config."""
        try:
            settings = self._config.settings
            if not getattr(settings, "skills", None):
                return
            if not settings.skills.enabled:
                log.debug("daemon: skills disabled -- skipping")
                return

            from meept.skills.loader import SkillLoader
            from meept.skills.registry import SkillRegistry

            loader = SkillLoader(settings.skills.directory)
            skill_defs = loader.load_all()

            skill_registry = SkillRegistry()
            for skill in skill_defs:
                skill_registry.register(skill)

            self._registry.register_instance("skill_registry", skill_registry)

            # Build triage agent if triage is enabled.
            triage_agent = None
            if settings.skills.triage.enabled:
                from meept.skills.triage import TriageAgent

                # Use the triage model from config (falls back to default).
                triage_llm = self._registry.get("llm_client")
                if triage_llm is not None:
                    triage_agent = TriageAgent(
                        llm_client=triage_llm,
                        skills=skill_defs,
                        confidence_threshold=settings.skills.triage.confidence_threshold,
                    )
                    log.info("daemon: triage agent initialized")

            # Build task executor.
            from meept.skills.executor import TaskExecutor

            executor = TaskExecutor(
                tool_registry=self._registry.get("tool_registry") or __import__("meept.tools.interface", fromlist=["ToolRegistry"]).ToolRegistry(),
                security=self._registry.get("security"),
                memory_manager=self._registry.get("memory"),
                bus=self._bus,
                llm_factory=self._registry.get("llm_factory"),
                budget=self._registry.get("token_budget"),
            )

            # Build dispatcher.
            from meept.skills.dispatcher import SkillDispatcher

            default_loop = self._registry.get("agent_loop")
            planner = self._registry.get("planner")

            dispatcher = SkillDispatcher(
                skill_registry=skill_registry,
                triage_agent=triage_agent,
                task_executor=executor,
                default_loop=default_loop,
                planner=planner,
                bus=self._bus,
            )

            self._registry.register_instance("skill_dispatcher", dispatcher)

            # Subscribe the dispatcher to chat requests (replaces direct
            # agent_loop subscription when skills are active).
            self._bus.subscribe("chat.request", dispatcher.handle_chat_request)

            log.info(
                "daemon: skills subsystem started (%d skill(s), triage=%s)",
                len(skill_registry),
                "on" if triage_agent else "off",
            )

        except Exception:
            log.exception("daemon: failed to start skills subsystem")

    async def _start_optional(self, name: str) -> None:
        """Start a registered subsystem if its factory exists."""
        if not self._registry.has(name):
            log.debug("daemon: subsystem %r not registered -- skipping", name)
            return
        try:
            component = await self._registry.get_or_create(name)
            if hasattr(component, "start"):
                result = component.start()
                if asyncio.iscoroutine(result):
                    await result
            log.info("daemon: subsystem %r started", name)
        except Exception:
            log.exception("daemon: failed to start subsystem %r", name)

    def _write_pid_file(self) -> None:
        pid_path = Path(self._config.settings.daemon.pid_file).expanduser()
        pid_path.parent.mkdir(parents=True, exist_ok=True)
        pid_path.write_text(str(os.getpid()), encoding="utf-8")
        log.debug("daemon: wrote PID %d to %s", os.getpid(), pid_path)

    def _cleanup_pid_file(self) -> None:
        pid_path = Path(self._config.settings.daemon.pid_file).expanduser()
        try:
            pid_path.unlink(missing_ok=True)
        except OSError:
            pass

    @property
    def is_running(self) -> bool:
        return self._running


# ------------------------------------------------------------------
# Daemonize helper (double-fork)
# ------------------------------------------------------------------


def _daemonize() -> None:
    """Classic double-fork to detach from the controlling terminal.

    After this function returns the caller is running as a background
    daemon with stdin/stdout/stderr redirected to ``/dev/null``.
    """
    # First fork -- let the parent exit so the shell gets its prompt back.
    if os.fork() > 0:
        os._exit(0)

    # New session, detach from terminal.
    os.setsid()

    # Second fork -- prevent the daemon from ever re-acquiring a terminal.
    if os.fork() > 0:
        os._exit(0)

    # Redirect standard file descriptors.
    sys.stdout.flush()
    sys.stderr.flush()
    devnull = os.open(os.devnull, os.O_RDWR)
    os.dup2(devnull, sys.stdin.fileno())
    os.dup2(devnull, sys.stdout.fileno())
    os.dup2(devnull, sys.stderr.fileno())
    os.close(devnull)


# ------------------------------------------------------------------
# Top-level entry point
# ------------------------------------------------------------------


def run(*, daemon: bool = False, config_path: str | None = None) -> None:
    """Boot the meept daemon.

    This is the function called by ``__main__``.

    Parameters
    ----------
    daemon:
        When *True* the process double-forks into the background before
        starting the event loop.
    config_path:
        Optional explicit path to ``meept.toml``.
    """
    # Load config early so we can set up logging before forking.
    config = MeeptConfig(config_path)
    _setup_logging(config.settings.daemon.log_level, daemon=daemon)

    if daemon:
        log.info("daemon: forking into background")
        _daemonize()

    meeptd = MeeptDaemon(config)
    meeptd._write_pid_file()

    loop = asyncio.new_event_loop()
    asyncio.set_event_loop(loop)

    # Wire OS signals to the daemon.
    for sig in (signal.SIGTERM, signal.SIGINT):
        loop.add_signal_handler(sig, meeptd._handle_signal, sig)

    try:
        loop.run_until_complete(meeptd.start())
    except KeyboardInterrupt:
        pass
    finally:
        loop.run_until_complete(meeptd.stop())
        loop.close()


def _setup_logging(level_name: str, *, daemon: bool) -> None:
    """Configure the root logger for meept."""
    level = getattr(logging, level_name.upper(), logging.INFO)

    handlers: list[logging.Handler] = []

    if not daemon:
        # Interactive mode -- log to stderr.
        handler = logging.StreamHandler(sys.stderr)
        handler.setFormatter(
            logging.Formatter(
                fmt="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
                datefmt="%Y-%m-%d %H:%M:%S",
            )
        )
        handlers.append(handler)

    # Always log to file when the data directory exists.
    data_dir = Path("~/.meept").expanduser()
    if data_dir.is_dir():
        file_handler = logging.FileHandler(data_dir / "meept.log", encoding="utf-8")
        file_handler.setFormatter(
            logging.Formatter(
                fmt="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
                datefmt="%Y-%m-%d %H:%M:%S",
            )
        )
        handlers.append(file_handler)

    root = logging.getLogger()
    root.setLevel(level)
    for h in handlers:
        root.addHandler(h)
