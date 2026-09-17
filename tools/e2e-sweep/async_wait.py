#!/usr/bin/env python3
"""Grade canonical terminal events by default; opt-in legacy task-store polling.

Only the async path supplies terminal identity/provenance to graders. Legacy
step selection is an approximation, not the canonical parallel-plan reply.
"""
import json
import re
import sqlite3
import time
import uuid

ACK_MARKERS = ("## starting task", "**task:**", "quick plan")


def extract_task_id(reply):
    m = re.search(r"task-[A-Za-z0-9_.-]+", reply)
    return m.group(0) if m else None


class TaskFailed(ValueError):
    """The stored task reaches an unsuccessful terminal state."""


def wait_steps(home, task_id, timeout: float = 300):
    from pathlib import Path

    uri = (Path(home) / "tasks.db").resolve().as_uri() + "?mode=ro"
    db = sqlite3.connect(uri, uri=True, timeout=min(timeout, 1))
    deadline = time.monotonic() + timeout
    try:
        while time.monotonic() < deadline:
            task = db.execute("SELECT state FROM tasks WHERE id=?", (task_id,)).fetchone()
            if task and task[0] in ("failed", "rejected", "cancelled"):
                raise TaskFailed("task ended " + task[0])
            rows = db.execute(
                "SELECT agent_id, state, result FROM task_steps WHERE task_id=? "
                "ORDER BY sequence, id", (task_id,)).fetchall()
            if task and task[0] == "completed":
                if not rows:
                    raise TaskFailed("completed task has no steps")
                if all(row[1] in ("approved", "completed", "failed", "rejected", "skipped") for row in rows):
                    return rows
            time.sleep(max(0, min(0.25, deadline - time.monotonic())))
    finally:
        db.close()
    return []


def best_response(rows):
    """Decode the final ordered step, never longer intermediate narration."""
    rows = [row for row in rows if row[1] != "skipped"]
    if not rows:
        return ""
    raw = rows[-1][2]
    if raw is None:
        return ""
    try:
        data = json.loads(raw)
    except json.JSONDecodeError:
        return raw
    if not isinstance(data, dict) or "response" not in data:
        return raw
    if data.get("error") or data.get("success") is False or data.get("status") in (
            "failed", "rejected", "cancelled", "error"):
        raise TaskFailed("stored execution envelope reports failure")
    response = data["response"]
    if not isinstance(response, str):
        raise ValueError("stored response must be text")
    return response


def run_async_aware(client, home, category, name, agent, message, grade, session=None):
    """Return (verdict, detail, seconds), grading only a completed deliverable."""
    session = session or f"e2e-{category}-{name}-{uuid.uuid4().hex}"
    t0 = time.monotonic()
    try:
        reply = client.chat(message, session, agent)
    except TimeoutError as exc:
        return ("TIMEOUT", str(exc), round(time.monotonic() - t0, 1))
    dt = round(time.monotonic() - t0, 1)
    if client.transport == "async":
        terminal = client.last_terminal
        if not isinstance(terminal, dict):
            return ("ERROR", "missing terminal event", dt)
        status = terminal.get("status")
        if status != "completed" or terminal.get("error"):
            return ("TIMEOUT" if status == "timeout" else "FAIL",
                    terminal.get("error") or "turn ended " + str(status), dt)
        if not isinstance(reply, str) or not reply.strip():
            return ("FAIL", "empty or invalid reply", dt)
        ctx = dict(terminal, home=home)
        v, d = grade(reply, ctx)
        return (v, d, dt)
    if client.transport != "legacy":
        return ("ERROR", "unknown transport mode", dt)
    if not isinstance(reply, str) or not reply.strip():
        return ("FAIL", "empty or invalid reply", dt)
    legacy = re.match(r"^Task (task-[A-Za-z0-9_.-]+) (is still running;|failed after|completed\.)",
                      reply.strip(), re.IGNORECASE)
    if legacy and not legacy.group(2).lower().startswith("is still"):
        return ("FAIL", "legacy terminal stub has no deliverable", dt)
    if not legacy and not any(m in reply.lower() for m in ACK_MARKERS):
        v, d = grade(reply, {"home": home, "legacy": True})
        return (v, d, dt)
    tid = extract_task_id(reply)
    if not tid:
        return ("FAIL", "ACK without task id", dt)
    try:
        timeout = getattr(client, "timeout", 300)
        if not isinstance(timeout, (int, float)):
            timeout = 300
        remaining = timeout - (time.monotonic() - t0)
        if remaining <= 0:
            return ("TIMEOUT", "turn deadline exceeded", round(time.monotonic() - t0, 1))
        rows = wait_steps(home, tid, timeout=remaining)
    except TaskFailed as exc:
        return ("FAIL", str(exc), round(time.monotonic() - t0, 1))
    if not rows:
        return ("TIMEOUT", "task did not finish", round(time.monotonic() - t0, 1))
    if any(state in ("failed", "rejected") for _, state, _ in rows):
        return ("FAIL", "task ended failed/rejected", round(time.monotonic() - t0, 1))
    try:
        best = best_response(rows)
    except TaskFailed as exc:
        return ("FAIL", str(exc), round(time.monotonic() - t0, 1))
    if not best.strip():
        return ("FAIL", "task finished with empty deliverable", round(time.monotonic() - t0, 1))
    v, d = grade(best, {"home": home, "legacy": True})
    return (v, d, round(time.monotonic() - t0, 1))
