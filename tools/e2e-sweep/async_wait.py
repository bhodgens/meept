#!/usr/bin/env python3
"""Async-aware runner: task-creating intents return an ACK, so the runner
waits for the step result in tasks.db (MEEPT_HOME/tasks.db) and grades the
stored deliverable."""
import re
import sqlite3
import time

ACK_MARKERS = ("## starting task", "**task:**", "quick plan")


def extract_task_id(reply):
    m = re.search(r"task-([\d.]+-\d+)", reply)
    return f"task-{m.group(1)}" if m else None


def wait_steps(home, task_id, timeout=300):
    db = sqlite3.connect(home + "/tasks.db")
    t0 = time.time()
    while time.time() - t0 < timeout:
        rows = db.execute(
            "SELECT agent_id, state, result FROM task_steps WHERE task_id=?",
            (task_id,)).fetchall()
        real = [r for r in rows if r[1] in ("approved", "completed", "failed", "rejected")]
        if rows and len(real) == len(rows):
            db.close()
            return rows
        time.sleep(6)
    db.close()
    return []


def best_response(rows):
    """Prefer the decoded envelope response of the longest stored result."""
    import json
    best = ""
    for agent, state, raw in rows:
        resp = ""
        try:
            d = json.loads(raw)
            resp = d.get("response", "")
        except Exception:
            resp = raw or ""
        if len(resp) > len(best):
            best = resp
    return best


def run_async_aware(client, home, category, name, agent, message, grade):
    """Returns (verdict, detail, secs). Uses the ACK detect to decide sync vs
    async grading."""
    session = f"e2e-{category}-{name}-{int(time.time())}"
    t0 = time.time()
    reply = client.chat(message, session, agent)
    dt = round(time.time() - t0, 1)
    if not any(m in reply for m in ACK_MARKERS):
        v, d = grade(reply, {})
        return (v, d, dt)
    tid = extract_task_id(reply)
    if not tid:
        return ("FAIL", "ACK without task id", dt)
    rows = wait_steps(home, tid)
    if not rows:
        return ("TIMEOUT", "task did not finish", round(time.time() - t0, 1))
    v, d = grade(best_response(rows), {})
    return (v, d, round(time.time() - t0, 1))
