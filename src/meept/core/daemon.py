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

        # --- agent subsystem (conditional on skills.enabled) ---
        await self._start_agents()

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
        for name in ("front_agent", "scheduler", "agent_loop", "comm_server"):
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

    async def _start_agents(self) -> None:
        """Initialise the FrontAgent + Orchestrator pipeline if skills enabled."""
        try:
            settings = self._config.settings
            if not getattr(settings, "skills", None):
                return
            if not settings.skills.enabled:
                log.debug("daemon: skills disabled -- skipping")
                return

            from meept.agent.front import FrontAgent
            from meept.agent.orchestrator import Orchestrator
            from meept.agent.worker_factory import WorkerFactory
            from meept.scheduler.pipelines import PipelineExecutor
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

                triage_llm = self._registry.get("llm_client")
                if triage_llm is not None:
                    triage_agent = TriageAgent(
                        llm_client=triage_llm,
                        skills=skill_defs,
                        confidence_threshold=settings.skills.triage.confidence_threshold,
                    )
                    log.info("daemon: triage agent initialized")

            # Build WorkerFactory.
            tool_registry = self._registry.get("tool_registry")
            if tool_registry is None:
                from meept.tools.interface import ToolRegistry
                tool_registry = ToolRegistry()

            worker_factory = WorkerFactory(
                tool_registry=tool_registry,
                security=self._registry.get("security"),
                memory=self._registry.get("memory"),
                bus=self._bus,
                llm_factory=self._registry.get("llm_factory"),
                scheduler=self._registry.get("scheduler"),
            )

            # Build PipelineExecutor and Orchestrator.
            pipeline_executor = PipelineExecutor(bus=self._bus)
            orchestrator = Orchestrator(
                pipeline_executor=pipeline_executor,
                worker_factory=worker_factory,
                bus=self._bus,
                skill_registry=skill_registry,
            )

            self._registry.register_instance("orchestrator", orchestrator)

            # Build FrontAgent.
            default_loop = self._registry.get("agent_loop")
            planner = self._registry.get("planner")

            # Build workspace manager and collaborative planner (if enabled).
            workspace_manager = None
            collaborative_planner = None
            ws_cfg = getattr(settings, "workspace", None)
            if ws_cfg is not None and ws_cfg.enabled:
                from meept.agent.workspace import WorkspaceManager

                workspace_manager = WorkspaceManager(
                    base_dir=Path(ws_cfg.base_dir).expanduser(),
                    auto_commit=ws_cfg.auto_commit,
                )
                self._registry.register_instance("workspace_manager", workspace_manager)
                log.info("daemon: workspace manager initialized (base_dir=%s)", ws_cfg.base_dir)

                if planner is not None:
                    from meept.agent.collaborative_planner import CollaborativePlanner

                    collab_llm = self._registry.get("llm_client")
                    if collab_llm is not None:
                        collaborative_planner = CollaborativePlanner(
                            planner=planner,
                            llm_client=collab_llm,
                            workspace=workspace_manager,
                        )
                        self._registry.register_instance(
                            "collaborative_planner", collaborative_planner,
                        )
                        log.info("daemon: collaborative planner initialized")

            front_agent = FrontAgent(
                orchestrator=orchestrator,
                triage_agent=triage_agent,
                planner=planner,
                default_loop=default_loop,
                skill_registry=skill_registry,
                bus=self._bus,
                collaborative_planner=collaborative_planner,
                workspace_manager=workspace_manager,
            )

            self._registry.register_instance("front_agent", front_agent)

            # Subscribe FrontAgent to chat requests (replaces direct
            # agent_loop subscription when skills are active).
            self._bus.subscribe("chat.request", front_agent.handle_chat_request)

            # Wire scheduler bus subscribers if scheduler is running.
            scheduler = self._registry.get("scheduler")
            if scheduler is not None and hasattr(scheduler, "subscribe_to_bus"):
                await scheduler.subscribe_to_bus()

            log.info(
                "daemon: agent subsystem started (%d skill(s), triage=%s)",
                len(skill_registry),
                "on" if triage_agent else "off",
            )

        except Exception:
            log.exception("daemon: failed to start agent subsystem")

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
