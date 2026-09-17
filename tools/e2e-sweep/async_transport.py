"""HTTP submit + WebSocket terminal transport. No polling or resubmission.

The WebSocket API has no replay cursor. A disconnect fails closed rather than
reconnecting and silently missing completion. --timeout bounds inactivity, not
healthy work duration. The optional websockets package is loaded only in async mode.
"""
import asyncio
import json
import time
import uuid


class AsyncTransportError(ValueError):
    pass


class TurnTimeout(TimeoutError):
    pass


def websocket_connect(url, key, ssl_context, timeout):
    try:
        from websockets.legacy.client import connect
    except ImportError as exc:
        raise AsyncTransportError(
            "async transport requires optional 'websockets' dependency; "
            "use an environment with it installed or explicitly --transport legacy") from exc
    options = dict(extra_headers={"Authorization": "Bearer " + key},
                   open_timeout=timeout, close_timeout=1, max_queue=256)
    if url.startswith("wss:"):
        options["ssl"] = ssl_context or True
    return connect(url, **options)


def _decode(raw):
    try:
        frame = json.loads(raw)
    except (TypeError, ValueError) as exc:
        raise AsyncTransportError("invalid WebSocket JSON") from exc
    if not isinstance(frame, dict):
        raise AsyncTransportError("WebSocket frame must be an object")
    return frame


async def _receive(ws, timeout):
    try:
        return _decode(await asyncio.wait_for(ws.recv(), timeout))
    except asyncio.TimeoutError as exc:
        raise TurnTimeout("no turn progress before inactivity timeout; not resubmitted") from exc
    except (TurnTimeout, AsyncTransportError):
        raise
    except Exception as exc:
        raise AsyncTransportError(
            "WebSocket disconnected; events may be missing, replay/reconnect unsupported; "
            "turn NOT resubmitted") from exc


async def _chat(client, message, session_id, agent):
    turn_id = "turn-" + uuid.uuid4().hex
    payload = dict(message=message, session_id=session_id,
                   conversation_id=session_id, turn_id=turn_id,
                   source_client="e2e-sweep")
    if agent:
        payload["agent_id"] = agent
    url = client.base.replace("https://", "wss://", 1).replace("http://", "ws://", 1) + "/ws"
    async with websocket_connect(url, client.key, client.ctx, client.timeout) as ws:
        # No server-side session filter: progress producers sometimes carry only
        # conversation_id. Correlate locally instead. A pong after subscribed
        # also fences server-side subscription handling before HTTP submission.
        await ws.send(json.dumps({"type": "subscribe", "data": {"channel": "all"}}))
        ready_deadline = time.monotonic() + client.timeout
        while True:
            frame = await _receive(ws, max(0, ready_deadline - time.monotonic()))
            if frame.get("type") == "error":
                raise AsyncTransportError("WebSocket subscription rejected")
            if frame.get("type") == "subscribed":
                await ws.send(json.dumps({"type": "ping"}))
            if frame.get("type") == "pong":
                break

        # HTTP runs concurrently with receiving: a fast terminal can precede its
        # ack. Buffer that terminal until the ack has validated the identities.
        ack_task = asyncio.create_task(asyncio.to_thread(
            client.post, "/api/v1/chat/submit", payload))
        recv_task = None
        ack = None
        terminal = None
        last_progress = time.monotonic()
        try:
            while True:
                if ack is not None and terminal is not None:
                    if terminal.get("conversation_id") != ack["conversation_id"]:
                        raise AsyncTransportError("terminal conversation_id mismatches ack")
                    return terminal
                if recv_task is None:
                    recv_task = asyncio.create_task(ws.recv())
                waiting = {recv_task}
                if ack is None:
                    waiting.add(ack_task)
                done, _ = await asyncio.wait(
                    waiting, timeout=max(0, client.timeout - (time.monotonic() - last_progress)),
                    return_when=asyncio.FIRST_COMPLETED)
                if not done:
                    raise TurnTimeout("no turn progress before inactivity timeout; not resubmitted")
                if ack_task in done:
                    ack = ack_task.result()
                    if not isinstance(ack, dict) or ack.get("accepted") is not True:
                        raise AsyncTransportError("submit rejected: " + str(ack))
                    for key, expected in (("turn_id", turn_id), ("session_id", session_id),
                                          ("conversation_id", session_id)):
                        if ack.get(key) != expected:
                            raise AsyncTransportError("submit ack mismatches " + key)
                if recv_task in done:
                    try:
                        frame = _decode(recv_task.result())
                    except AsyncTransportError:
                        raise
                    except Exception as exc:
                        raise AsyncTransportError(
                            "WebSocket disconnected; events may be missing, replay/reconnect "
                            "unsupported; turn NOT resubmitted") from exc
                    recv_task = None
                    event = frame.get("data")
                    if not isinstance(event, dict):
                        continue
                    if event.get("turn_id") not in (None, "", turn_id):
                        continue
                    if event.get("session_id") not in (None, "", session_id):
                        continue
                    if event.get("conversation_id") not in (None, "", session_id):
                        continue
                    # Unrelated/global traffic must not keep a stalled turn alive.
                    if not any(event.get(k) == v for k, v in (
                            ("turn_id", turn_id), ("session_id", session_id),
                            ("conversation_id", session_id))):
                        continue
                    last_progress = time.monotonic()
                    if event.get("source_topic") != "turn.terminal" or event.get("turn_id") != turn_id:
                        continue
                    if event.get("status") == "parked":
                        continue
                    if event.get("status") not in ("completed", "failed", "timeout"):
                        raise AsyncTransportError("invalid terminal status")
                    if not isinstance(event.get("reply"), str):
                        raise AsyncTransportError("terminal missing text reply")
                    if not event.get("conversation_id"):
                        raise AsyncTransportError("terminal missing conversation_id")
                    if terminal is not None and event != terminal:
                        raise AsyncTransportError("conflicting duplicate terminal")
                    terminal = dict(event)
        finally:
            for task in (recv_task, ack_task):
                if task is not None:
                    task.cancel()
            await asyncio.gather(*(t for t in (recv_task, ack_task) if t is not None),
                                 return_exceptions=True)


def chat(client, message, session_id, agent=None):
    """Return the real terminal event; completed/failed/timeout remain distinct."""
    return asyncio.run(_chat(client, message, session_id, agent))
