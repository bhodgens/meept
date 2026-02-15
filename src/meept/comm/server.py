"""Unix domain socket server exposing JSON-RPC 2.0 over length-prefixed framing.

Each message on the wire is encoded as::

    {length}\\n{json_payload}

where *length* is the byte-length of *json_payload* expressed as ASCII
decimal digits.  This scheme avoids the need for escaping and makes it
straightforward to read an exact frame from a stream.
"""

from __future__ import annotations

import asyncio
import logging
import os
import time
from pathlib import Path
from typing import Any, Callable, Coroutine

from meept.comm.protocol import (
    INTERNAL_ERROR,
    INVALID_PARAMS,
    INVALID_REQUEST,
    METHOD_NOT_FOUND,
    PARSE_ERROR,
    JsonRpcRequest,
    format_error,
    format_response,
    parse_message,
)
from meept.core.bus import MessageBus
from meept.models.messages import BusMessage, MessageType

log = logging.getLogger(__name__)

# Type alias for RPC method handlers.
MethodHandler = Callable[..., Coroutine[Any, Any, dict[str, Any]]]

# How long to wait for a chat response from the bus before timing out.
_CHAT_TIMEOUT: float = 120.0


class CommServer:
    """Asynchronous Unix-socket JSON-RPC server.

    Parameters
    ----------
    socket_path:
        Filesystem path for the Unix domain socket.
    bus:
        The internal :class:`MessageBus` used to relay messages between
        daemon subsystems.
    """

    def __init__(self, socket_path: str, bus: MessageBus) -> None:
        self._socket_path = Path(socket_path)
        self._bus = bus
        self._server: asyncio.AbstractServer | None = None
        self._methods: dict[str, MethodHandler] = {}
        self._start_time: float = time.monotonic()

        # Register built-in handlers.
        self.register_method("chat", self._handle_chat)
        self.register_method("status", self._handle_status)
        self.register_method("memory.query", self._handle_memory_query)
        self.register_method("memory.export", self._handle_memory_export)
        self.register_method("scheduler.list_jobs", self._handle_scheduler_list_jobs)
        self.register_method("scheduler.add_job", self._handle_scheduler_add_job)
        self.register_method("config.reload", self._handle_config_reload)
        self.register_method("security.query_log", self._handle_security_query_log)
        self.register_method("security.get_stats", self._handle_security_get_stats)
        self.register_method("security.record_override", self._handle_security_record_override)
        self.register_method("skills.list", self._handle_skills_list)
        self.register_method("skills.triage", self._handle_skills_triage)
        self.register_method("pipeline.status", self._handle_pipeline_status)
        self.register_method("scheduler.schedule_agent_task", self._handle_scheduler_agent_task)
        self.register_method("security.approve_action", self._handle_security_approve_action)

        # Self-improvement RPC methods.
        self.register_method("selfimprove.detect", self._handle_selfimprove_detect)
        self.register_method("selfimprove.analyze", self._handle_selfimprove_analyze)
        self.register_method("selfimprove.generate", self._handle_selfimprove_generate)
        self.register_method("selfimprove.validate", self._handle_selfimprove_validate)
        self.register_method("selfimprove.apply", self._handle_selfimprove_apply)
        self.register_method("selfimprove.status", self._handle_selfimprove_status)
        self.register_method("selfimprove.cycle", self._handle_selfimprove_cycle)

    # ------------------------------------------------------------------
    # Lifecycle
    # ------------------------------------------------------------------

    async def start(self) -> None:
        """Create the Unix socket and begin accepting connections."""
        # Ensure parent directory exists.
        self._socket_path.parent.mkdir(parents=True, exist_ok=True)

        # Remove a stale socket file if present.
        if self._socket_path.exists():
            self._socket_path.unlink()

        self._server = await asyncio.start_unix_server(
            self.handle_client,
            path=str(self._socket_path),
        )

        # Restrict socket permissions to owner only.
        os.chmod(self._socket_path, 0o600)

        self._start_time = time.monotonic()
        log.info("comm: listening on %s", self._socket_path)

    async def stop(self) -> None:
        """Shut down the server and clean up the socket file."""
        if self._server is not None:
            self._server.close()
            await self._server.wait_closed()
            self._server = None
            log.info("comm: server closed")

        if self._socket_path.exists():
            try:
                self._socket_path.unlink()
            except OSError:
                log.debug("comm: could not remove socket file %s", self._socket_path)

    # ------------------------------------------------------------------
    # Method registration
    # ------------------------------------------------------------------

    def register_method(self, name: str, handler: MethodHandler) -> None:
        """Register an RPC method *name* backed by *handler*.

        *handler* must be an async callable that accepts a ``dict`` of
        params and returns a ``dict`` result.
        """
        self._methods[name] = handler
        log.debug("comm: registered method %r", name)

    # ------------------------------------------------------------------
    # Client handling
    # ------------------------------------------------------------------

    async def handle_client(
        self,
        reader: asyncio.StreamReader,
        writer: asyncio.StreamWriter,
    ) -> None:
        """Read length-prefixed JSON-RPC frames, dispatch, and respond."""
        peer = writer.get_extra_info("peername") or "unknown"
        log.debug("comm: client connected (%s)", peer)

        try:
            while True:
                # --- Read the length line --------------------------------
                length_line = await reader.readline()
                if not length_line:
                    # Client disconnected cleanly.
                    break

                try:
                    payload_length = int(length_line.strip())
                except ValueError:
                    await self._write_frame(
                        writer,
                        format_error(PARSE_ERROR, "Invalid length prefix"),
                    )
                    continue

                if payload_length <= 0 or payload_length > 10 * 1024 * 1024:
                    await self._write_frame(
                        writer,
                        format_error(INVALID_REQUEST, "Payload length out of range"),
                    )
                    continue

                # --- Read the JSON payload --------------------------------
                payload = await reader.readexactly(payload_length)

                # --- Parse & dispatch -------------------------------------
                response_bytes = await self._dispatch(payload)
                await self._write_frame(writer, response_bytes)

        except asyncio.IncompleteReadError:
            log.debug("comm: client disconnected mid-read (%s)", peer)
        except ConnectionResetError:
            log.debug("comm: client reset connection (%s)", peer)
        except Exception:
            log.exception("comm: unhandled error in client handler (%s)", peer)
        finally:
            try:
                writer.close()
                await writer.wait_closed()
            except Exception:
                pass
            log.debug("comm: client disconnected (%s)", peer)

    # ------------------------------------------------------------------
    # Internal helpers
    # ------------------------------------------------------------------

    async def _dispatch(self, raw: bytes) -> bytes:
        """Parse a raw JSON-RPC payload and route to the correct handler."""
        try:
            request = parse_message(raw)
        except ValueError as exc:
            return format_error(PARSE_ERROR, str(exc))

        request_id = request.id

        handler = self._methods.get(request.method)
        if handler is None:
            return format_error(
                METHOD_NOT_FOUND,
                f"Method {request.method!r} not found",
                request_id=request_id,
            )

        params = request.params if request.params is not None else {}
        if not isinstance(params, dict):
            return format_error(
                INVALID_PARAMS,
                "Params must be a JSON object (dict)",
                request_id=request_id,
            )

        try:
            result = await handler(params)
            return format_response(result, request_id)
        except TypeError as exc:
            return format_error(INVALID_PARAMS, str(exc), request_id=request_id)
        except Exception as exc:
            log.exception("comm: handler error for method %r", request.method)
            return format_error(INTERNAL_ERROR, str(exc), request_id=request_id)

    @staticmethod
    async def _write_frame(writer: asyncio.StreamWriter, data: bytes) -> None:
        """Write a length-prefixed frame to the stream."""
        header = f"{len(data)}\n".encode("utf-8")
        writer.write(header + data)
        await writer.drain()

    # ------------------------------------------------------------------
    # Built-in RPC handlers
    # ------------------------------------------------------------------

    async def _handle_chat(self, params: dict[str, Any]) -> dict[str, Any]:
        """Publish a chat message to the bus and wait for a response.

        Expected params::

            {"message": "Hello!", "conversation_id": "..."}

        Returns::

            {"reply": "...", "conversation_id": "..."}
        """
        message_text = params.get("message")
        if not message_text or not isinstance(message_text, str):
            raise TypeError("'message' param is required and must be a non-empty string")

        conversation_id = params.get("conversation_id", "")

        # Build a bus message.
        request_msg = BusMessage(
            type=MessageType.CHAT_REQUEST,
            payload={
                "text": message_text,
                "conversation_id": conversation_id,
            },
            source="comm.rpc",
        )

        # Set up a Future that will be resolved when the response arrives.
        loop = asyncio.get_running_loop()
        response_future: asyncio.Future[dict[str, Any]] = loop.create_future()

        async def _on_response(topic: str, msg: BusMessage) -> None:
            if msg.reply_to == request_msg.id and not response_future.done():
                response_future.set_result(msg.payload)

        self._bus.subscribe("chat.response", _on_response)
        try:
            await self._bus.publish("chat.request", request_msg)
            result = await asyncio.wait_for(response_future, timeout=_CHAT_TIMEOUT)
        except asyncio.TimeoutError:
            raise TimeoutError("Chat response timed out")
        finally:
            self._bus.unsubscribe("chat.response", _on_response)

        return {
            "reply": result.get("text", ""),
            "conversation_id": conversation_id,
        }

    async def _handle_status(self, params: dict[str, Any]) -> dict[str, Any]:
        """Return basic daemon status information."""
        uptime = time.monotonic() - self._start_time
        return {
            "status": "running",
            "uptime_seconds": round(uptime, 2),
            "registered_methods": sorted(self._methods.keys()),
            "bus_subscribers": self._bus.subscriber_count,
        }

    async def _handle_memory_query(self, params: dict[str, Any]) -> dict[str, Any]:
        """Forward a memory query through the bus."""
        query = params.get("query", "")
        if not query:
            raise TypeError("'query' param is required")

        msg = BusMessage(
            type=MessageType.MEMORY_QUERY,
            payload={"query": query, **{k: v for k, v in params.items() if k != "query"}},
            source="comm.rpc",
        )

        loop = asyncio.get_running_loop()
        result_future: asyncio.Future[dict[str, Any]] = loop.create_future()

        async def _on_result(topic: str, bus_msg: BusMessage) -> None:
            if bus_msg.reply_to == msg.id and not result_future.done():
                result_future.set_result(bus_msg.payload)

        self._bus.subscribe("memory.result", _on_result)
        try:
            await self._bus.publish("memory.query", msg)
            result = await asyncio.wait_for(result_future, timeout=30.0)
        except asyncio.TimeoutError:
            raise TimeoutError("Memory query timed out")
        finally:
            self._bus.unsubscribe("memory.result", _on_result)

        return result

    async def _handle_memory_export(self, params: dict[str, Any]) -> dict[str, Any]:
        """Trigger a memory export via the bus."""
        msg = BusMessage(
            type=MessageType.MEMORY_QUERY,
            payload={"action": "export", **params},
            source="comm.rpc",
        )
        await self._bus.publish("memory.export", msg)
        return {"status": "export_started"}

    async def _handle_scheduler_list_jobs(self, params: dict[str, Any]) -> dict[str, Any]:
        """Request the list of scheduled jobs via the bus."""
        msg = BusMessage(
            type=MessageType.STATUS_UPDATE,
            payload={"action": "list_jobs"},
            source="comm.rpc",
        )

        loop = asyncio.get_running_loop()
        result_future: asyncio.Future[dict[str, Any]] = loop.create_future()

        async def _on_result(topic: str, bus_msg: BusMessage) -> None:
            if bus_msg.reply_to == msg.id and not result_future.done():
                result_future.set_result(bus_msg.payload)

        self._bus.subscribe("scheduler.result", _on_result)
        try:
            await self._bus.publish("scheduler.list_jobs", msg)
            result = await asyncio.wait_for(result_future, timeout=10.0)
        except asyncio.TimeoutError:
            return {"jobs": [], "note": "Scheduler did not respond in time"}
        finally:
            self._bus.unsubscribe("scheduler.result", _on_result)

        return result

    async def _handle_scheduler_add_job(self, params: dict[str, Any]) -> dict[str, Any]:
        """Add a scheduled job via the bus."""
        if "name" not in params:
            raise TypeError("'name' param is required")

        msg = BusMessage(
            type=MessageType.STATUS_UPDATE,
            payload={"action": "add_job", **params},
            source="comm.rpc",
        )

        loop = asyncio.get_running_loop()
        result_future: asyncio.Future[dict[str, Any]] = loop.create_future()

        async def _on_result(topic: str, bus_msg: BusMessage) -> None:
            if bus_msg.reply_to == msg.id and not result_future.done():
                result_future.set_result(bus_msg.payload)

        self._bus.subscribe("scheduler.result", _on_result)
        try:
            await self._bus.publish("scheduler.add_job", msg)
            result = await asyncio.wait_for(result_future, timeout=10.0)
        except asyncio.TimeoutError:
            raise TimeoutError("Scheduler did not respond in time")
        finally:
            self._bus.unsubscribe("scheduler.result", _on_result)

        return result

    async def _handle_config_reload(self, params: dict[str, Any]) -> dict[str, Any]:
        """Publish a config-reload event on the bus."""
        msg = BusMessage(
            type=MessageType.CONFIG_RELOAD,
            payload=params,
            source="comm.rpc",
        )
        await self._bus.publish("config.reload", msg)
        return {"status": "reload_requested"}

    async def _handle_security_query_log(self, params: dict[str, Any]) -> dict[str, Any]:
        """Query the security decision log via the bus."""
        msg = BusMessage(
            type=MessageType.STATUS_UPDATE,
            payload={"action": "security.query_log", **params},
            source="comm.rpc",
        )

        loop = asyncio.get_running_loop()
        result_future: asyncio.Future[dict[str, Any]] = loop.create_future()

        async def _on_result(topic: str, bus_msg: BusMessage) -> None:
            if bus_msg.reply_to == msg.id and not result_future.done():
                result_future.set_result(bus_msg.payload)

        self._bus.subscribe("security.result", _on_result)
        try:
            await self._bus.publish("security.query_log", msg)
            result = await asyncio.wait_for(result_future, timeout=10.0)
        except asyncio.TimeoutError:
            return {"records": [], "note": "Security engine did not respond in time"}
        finally:
            self._bus.unsubscribe("security.result", _on_result)

        return result

    async def _handle_security_get_stats(self, params: dict[str, Any]) -> dict[str, Any]:
        """Get aggregate security statistics via the bus."""
        msg = BusMessage(
            type=MessageType.STATUS_UPDATE,
            payload={"action": "security.get_stats"},
            source="comm.rpc",
        )

        loop = asyncio.get_running_loop()
        result_future: asyncio.Future[dict[str, Any]] = loop.create_future()

        async def _on_result(topic: str, bus_msg: BusMessage) -> None:
            if bus_msg.reply_to == msg.id and not result_future.done():
                result_future.set_result(bus_msg.payload)

        self._bus.subscribe("security.result", _on_result)
        try:
            await self._bus.publish("security.get_stats", msg)
            result = await asyncio.wait_for(result_future, timeout=10.0)
        except asyncio.TimeoutError:
            return {"error": "Security engine did not respond in time"}
        finally:
            self._bus.unsubscribe("security.result", _on_result)

        return result

    async def _handle_security_record_override(self, params: dict[str, Any]) -> dict[str, Any]:
        """Record a creator permission override via the bus."""
        action = params.get("action")
        if not action:
            raise TypeError("'action' param is required")

        msg = BusMessage(
            type=MessageType.STATUS_UPDATE,
            payload={"action": "security.record_override", **params},
            source="comm.rpc",
        )

        loop = asyncio.get_running_loop()
        result_future: asyncio.Future[dict[str, Any]] = loop.create_future()

        async def _on_result(topic: str, bus_msg: BusMessage) -> None:
            if bus_msg.reply_to == msg.id and not result_future.done():
                result_future.set_result(bus_msg.payload)

        self._bus.subscribe("security.result", _on_result)
        try:
            await self._bus.publish("security.record_override", msg)
            result = await asyncio.wait_for(result_future, timeout=10.0)
        except asyncio.TimeoutError:
            raise TimeoutError("Security engine did not respond in time")
        finally:
            self._bus.unsubscribe("security.result", _on_result)

        return result

    async def _handle_skills_list(self, params: dict[str, Any]) -> dict[str, Any]:
        """List all loaded skill definitions via the bus."""
        msg = BusMessage(
            type=MessageType.STATUS_UPDATE,
            payload={"action": "skills.list"},
            source="comm.rpc",
        )

        loop = asyncio.get_running_loop()
        result_future: asyncio.Future[dict[str, Any]] = loop.create_future()

        async def _on_result(topic: str, bus_msg: BusMessage) -> None:
            if bus_msg.reply_to == msg.id and not result_future.done():
                result_future.set_result(bus_msg.payload)

        self._bus.subscribe("skills.result", _on_result)
        try:
            await self._bus.publish("skills.list", msg)
            result = await asyncio.wait_for(result_future, timeout=10.0)
        except asyncio.TimeoutError:
            return {"skills": [], "note": "Skills subsystem did not respond in time"}
        finally:
            self._bus.unsubscribe("skills.result", _on_result)

        return result

    async def _handle_skills_triage(self, params: dict[str, Any]) -> dict[str, Any]:
        """Triage a message to determine the best skill via the bus."""
        message = params.get("message")
        if not message or not isinstance(message, str):
            raise TypeError("'message' param is required and must be a non-empty string")

        msg = BusMessage(
            type=MessageType.STATUS_UPDATE,
            payload={"action": "skills.triage", "message": message},
            source="comm.rpc",
        )

        loop = asyncio.get_running_loop()
        result_future: asyncio.Future[dict[str, Any]] = loop.create_future()

        async def _on_result(topic: str, bus_msg: BusMessage) -> None:
            if bus_msg.reply_to == msg.id and not result_future.done():
                result_future.set_result(bus_msg.payload)

        self._bus.subscribe("skills.result", _on_result)
        try:
            await self._bus.publish("skills.triage", msg)
            result = await asyncio.wait_for(result_future, timeout=10.0)
        except asyncio.TimeoutError:
            return {"error": "Skills triage did not respond in time"}
        finally:
            self._bus.unsubscribe("skills.result", _on_result)

        return result

    async def _handle_pipeline_status(self, params: dict[str, Any]) -> dict[str, Any]:
        """Query the status of a running or completed pipeline.

        Expected params::

            {"pipeline_id": "..."}

        Returns pipeline progress information.
        """
        pipeline_id = params.get("pipeline_id")
        if not pipeline_id:
            raise TypeError("'pipeline_id' param is required")

        msg = BusMessage(
            type=MessageType.STATUS_UPDATE,
            payload={"action": "pipeline.status", "pipeline_id": pipeline_id},
            source="comm.rpc",
        )

        loop = asyncio.get_running_loop()
        result_future: asyncio.Future[dict[str, Any]] = loop.create_future()

        async def _on_result(topic: str, bus_msg: BusMessage) -> None:
            if bus_msg.reply_to == msg.id and not result_future.done():
                result_future.set_result(bus_msg.payload)

        self._bus.subscribe("pipeline.result", _on_result)
        try:
            await self._bus.publish("pipeline.status", msg)
            result = await asyncio.wait_for(result_future, timeout=10.0)
        except asyncio.TimeoutError:
            return {"error": "Pipeline status query timed out"}
        finally:
            self._bus.unsubscribe("pipeline.result", _on_result)

        return result

    async def _handle_security_approve_action(self, params: dict[str, Any]) -> dict[str, Any]:
        """Approve or deny a pending confirmation request.

        Expected params::

            {"request_id": "confirm-0", "approved": true}

        Publishes to the bus so the PermissionManager can resolve the future.
        """
        request_id = params.get("request_id")
        if not request_id or not isinstance(request_id, str):
            raise TypeError("'request_id' param is required and must be a non-empty string")

        approved = bool(params.get("approved", False))

        msg = BusMessage(
            type=MessageType.STATUS_UPDATE,
            payload={
                "action": "security.approve_action",
                "request_id": request_id,
                "approved": approved,
            },
            source="comm.rpc",
        )
        await self._bus.publish("security.approve_action", msg)
        return {"status": "resolved", "request_id": request_id, "approved": approved}

    async def _handle_scheduler_agent_task(self, params: dict[str, Any]) -> dict[str, Any]:
        """Schedule an agent task via the bus.

        Expected params::

            {
                "task_description": "Check the weather",
                "trigger": "interval",
                "trigger_args": {"hours": 6},
                "skill_hint": "weather"  (optional)
            }
        """
        task_description = params.get("task_description")
        if not task_description or not isinstance(task_description, str):
            raise TypeError("'task_description' param is required and must be a non-empty string")

        msg = BusMessage(
            type=MessageType.SCHEDULE_REQUEST,
            payload={
                "action": "add_job",
                "task_description": task_description,
                "trigger": params.get("trigger", "date"),
                "trigger_args": params.get("trigger_args", {}),
                "skill_hint": params.get("skill_hint"),
                "name": params.get("name", task_description[:50]),
            },
            source="comm.rpc",
        )

        loop = asyncio.get_running_loop()
        result_future: asyncio.Future[dict[str, Any]] = loop.create_future()

        async def _on_result(topic: str, bus_msg: BusMessage) -> None:
            if bus_msg.reply_to == msg.id and not result_future.done():
                result_future.set_result(bus_msg.payload)

        self._bus.subscribe("scheduler.result", _on_result)
        try:
            await self._bus.publish("scheduler.add_job", msg)
            result = await asyncio.wait_for(result_future, timeout=10.0)
        except asyncio.TimeoutError:
            raise TimeoutError("Scheduler did not respond in time")
        finally:
            self._bus.unsubscribe("scheduler.result", _on_result)

        return result

    # ------------------------------------------------------------------
    # Self-improvement RPC handlers
    # ------------------------------------------------------------------

    async def _handle_selfimprove_detect(self, params: dict[str, Any]) -> dict[str, Any]:
        """Trigger issue detection and return discovered issues.

        Expected params::

            {"sources": ["pytest", "logs", "mypy"]}  (optional)

        Returns::

            {"issues": [...], "count": N}
        """
        msg = BusMessage(
            type=MessageType.STATUS_UPDATE,
            payload={"action": "selfimprove.detect", **params},
            source="comm.rpc",
        )

        loop = asyncio.get_running_loop()
        result_future: asyncio.Future[dict[str, Any]] = loop.create_future()

        async def _on_result(topic: str, bus_msg: BusMessage) -> None:
            if bus_msg.reply_to == msg.id and not result_future.done():
                result_future.set_result(bus_msg.payload)

        self._bus.subscribe("selfimprove.result", _on_result)
        try:
            await self._bus.publish("selfimprove.detect", msg)
            result = await asyncio.wait_for(result_future, timeout=60.0)
        except asyncio.TimeoutError:
            return {"issues": [], "error": "Detection timed out"}
        finally:
            self._bus.unsubscribe("selfimprove.result", _on_result)

        return result

    async def _handle_selfimprove_analyze(self, params: dict[str, Any]) -> dict[str, Any]:
        """Run root cause analysis on detected issues.

        Expected params::

            {"issue_ids": ["issue-1", "issue-2"]}  (optional, analyzes all if omitted)

        Returns::

            {"analyses": [...], "count": N}
        """
        msg = BusMessage(
            type=MessageType.STATUS_UPDATE,
            payload={"action": "selfimprove.analyze", **params},
            source="comm.rpc",
        )

        loop = asyncio.get_running_loop()
        result_future: asyncio.Future[dict[str, Any]] = loop.create_future()

        async def _on_result(topic: str, bus_msg: BusMessage) -> None:
            if bus_msg.reply_to == msg.id and not result_future.done():
                result_future.set_result(bus_msg.payload)

        self._bus.subscribe("selfimprove.result", _on_result)
        try:
            await self._bus.publish("selfimprove.analyze", msg)
            result = await asyncio.wait_for(result_future, timeout=120.0)
        except asyncio.TimeoutError:
            return {"analyses": [], "error": "Analysis timed out"}
        finally:
            self._bus.unsubscribe("selfimprove.result", _on_result)

        return result

    async def _handle_selfimprove_generate(self, params: dict[str, Any]) -> dict[str, Any]:
        """Generate fix proposals for analyzed issues.

        Expected params::

            {"analysis_ids": ["analysis-1"]}  (optional)

        Returns::

            {"fixes": [...], "count": N}
        """
        msg = BusMessage(
            type=MessageType.STATUS_UPDATE,
            payload={"action": "selfimprove.generate", **params},
            source="comm.rpc",
        )

        loop = asyncio.get_running_loop()
        result_future: asyncio.Future[dict[str, Any]] = loop.create_future()

        async def _on_result(topic: str, bus_msg: BusMessage) -> None:
            if bus_msg.reply_to == msg.id and not result_future.done():
                result_future.set_result(bus_msg.payload)

        self._bus.subscribe("selfimprove.result", _on_result)
        try:
            await self._bus.publish("selfimprove.generate", msg)
            result = await asyncio.wait_for(result_future, timeout=120.0)
        except asyncio.TimeoutError:
            return {"fixes": [], "error": "Generation timed out"}
        finally:
            self._bus.unsubscribe("selfimprove.result", _on_result)

        return result

    async def _handle_selfimprove_validate(self, params: dict[str, Any]) -> dict[str, Any]:
        """Validate proposed fixes in sandbox.

        Expected params::

            {"fix_ids": ["fix-1"]}  (optional)

        Returns::

            {"validations": [...], "passed": N, "failed": N}
        """
        msg = BusMessage(
            type=MessageType.STATUS_UPDATE,
            payload={"action": "selfimprove.validate", **params},
            source="comm.rpc",
        )

        loop = asyncio.get_running_loop()
        result_future: asyncio.Future[dict[str, Any]] = loop.create_future()

        async def _on_result(topic: str, bus_msg: BusMessage) -> None:
            if bus_msg.reply_to == msg.id and not result_future.done():
                result_future.set_result(bus_msg.payload)

        self._bus.subscribe("selfimprove.result", _on_result)
        try:
            await self._bus.publish("selfimprove.validate", msg)
            result = await asyncio.wait_for(result_future, timeout=300.0)
        except asyncio.TimeoutError:
            return {"validations": [], "error": "Validation timed out"}
        finally:
            self._bus.unsubscribe("selfimprove.result", _on_result)

        return result

    async def _handle_selfimprove_apply(self, params: dict[str, Any]) -> dict[str, Any]:
        """Apply validated fixes.

        Expected params::

            {"fix_ids": ["fix-1"], "require_approval": true}

        Returns::

            {"applied": [...], "count": N}
        """
        msg = BusMessage(
            type=MessageType.STATUS_UPDATE,
            payload={"action": "selfimprove.apply", **params},
            source="comm.rpc",
        )

        loop = asyncio.get_running_loop()
        result_future: asyncio.Future[dict[str, Any]] = loop.create_future()

        async def _on_result(topic: str, bus_msg: BusMessage) -> None:
            if bus_msg.reply_to == msg.id and not result_future.done():
                result_future.set_result(bus_msg.payload)

        self._bus.subscribe("selfimprove.result", _on_result)
        try:
            await self._bus.publish("selfimprove.apply", msg)
            result = await asyncio.wait_for(result_future, timeout=60.0)
        except asyncio.TimeoutError:
            return {"applied": [], "error": "Apply timed out"}
        finally:
            self._bus.unsubscribe("selfimprove.result", _on_result)

        return result

    async def _handle_selfimprove_status(self, params: dict[str, Any]) -> dict[str, Any]:
        """Get current self-improvement status.

        Returns::

            {"state": "idle|detecting|analyzing|...", "current_cycle": {...}}
        """
        msg = BusMessage(
            type=MessageType.STATUS_UPDATE,
            payload={"action": "selfimprove.status"},
            source="comm.rpc",
        )

        loop = asyncio.get_running_loop()
        result_future: asyncio.Future[dict[str, Any]] = loop.create_future()

        async def _on_result(topic: str, bus_msg: BusMessage) -> None:
            if bus_msg.reply_to == msg.id and not result_future.done():
                result_future.set_result(bus_msg.payload)

        self._bus.subscribe("selfimprove.result", _on_result)
        try:
            await self._bus.publish("selfimprove.status", msg)
            result = await asyncio.wait_for(result_future, timeout=10.0)
        except asyncio.TimeoutError:
            return {"state": "unknown", "error": "Status query timed out"}
        finally:
            self._bus.unsubscribe("selfimprove.result", _on_result)

        return result

    async def _handle_selfimprove_cycle(self, params: dict[str, Any]) -> dict[str, Any]:
        """Run a full self-improvement cycle.

        Expected params::

            {"interactive": false, "auto_apply": false}

        Returns::

            {"cycle_id": "...", "status": "started|completed|failed"}
        """
        msg = BusMessage(
            type=MessageType.STATUS_UPDATE,
            payload={"action": "selfimprove.cycle", **params},
            source="comm.rpc",
        )

        loop = asyncio.get_running_loop()
        result_future: asyncio.Future[dict[str, Any]] = loop.create_future()

        async def _on_result(topic: str, bus_msg: BusMessage) -> None:
            if bus_msg.reply_to == msg.id and not result_future.done():
                result_future.set_result(bus_msg.payload)

        self._bus.subscribe("selfimprove.result", _on_result)
        try:
            await self._bus.publish("selfimprove.cycle", msg)
            # Full cycle can take a long time
            result = await asyncio.wait_for(result_future, timeout=600.0)
        except asyncio.TimeoutError:
            return {"status": "timeout", "error": "Cycle timed out after 10 minutes"}
        finally:
            self._bus.unsubscribe("selfimprove.result", _on_result)

        return result
