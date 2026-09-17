"""Read daemon-owned evidence, never evidence narrated in an LLM reply.

Producer contracts: internal/daemon/components.go AgentJobProcessor.Process and
 toolEvidenceCollector.absorb; internal/task/{store,step}.go. The projection
separates artifact evidence from recorded tool invocations. Invocation identities
prove a minimum observed count, not exact counts or complete event delivery.
"""
import json
from pathlib import Path
import sqlite3


class EvidenceUnavailable(ValueError):
    """No trustworthy, correlated evidence is available."""


class EvidenceFailed(ValueError):
    """A correlated execution explicitly failed."""


def _object(raw):
    try:
        value = json.loads(raw or "{}")
    except (TypeError, ValueError) as exc:
        raise EvidenceUnavailable("malformed stored execution") from exc
    if not isinstance(value, dict):
        raise EvidenceUnavailable("stored execution is not an object")
    return value


def read_tool_evidence(ctx, tool, *, invocations=False):
    """Return successful daemon projection entries for the exact terminal task.

    No task IDs are extracted from prose, no latest-task/session fallback is
    allowed, and step.evidence is not double counted (it is a lossy projection
    of result.tool_evidence). The home must belong to the endpoint's scratch rig.
    """
    if not isinstance(ctx, dict) or ctx.get("legacy"):
        raise EvidenceUnavailable("no canonical terminal identity (legacy or absent context)")
    for key in ("home", "turn_id", "task_id", "session_id", "conversation_id"):
        if not isinstance(ctx.get(key), str) or not ctx[key].strip():
            raise EvidenceUnavailable("missing terminal " + key)
    if ctx.get("status") != "completed":
        raise EvidenceFailed("terminal status is not completed")
    uri = (Path(ctx["home"]) / "tasks.db").resolve().as_uri() + "?mode=ro"
    try:
        db = sqlite3.connect(uri, uri=True, timeout=1)
        db.row_factory = sqlite3.Row
        try:
            task = db.execute("SELECT state FROM tasks WHERE id=?", (ctx["task_id"],)).fetchone()
            if not task:
                raise EvidenceUnavailable("terminal task is absent from tasks.db")
            if task["state"] in ("failed", "cancelled", "rejected"):
                raise EvidenceFailed("stored task ended " + task["state"])
            if task["state"] != "completed":
                raise EvidenceUnavailable("stored task is not completed")
            rows = db.execute(
                "SELECT id, task_id, agent_id, job_id, state, result, session_id, conversation_id "
                "FROM task_steps WHERE task_id=? ORDER BY sequence, id", (ctx["task_id"],)
            ).fetchall()
        finally:
            db.close()
    except sqlite3.Error as exc:
        raise EvidenceUnavailable("cannot read task evidence: " + str(exc)) from exc
    if not rows:
        raise EvidenceUnavailable("terminal task has no steps")
    entries = []
    seen_calls = {}
    for row in rows:
        if row["state"] in ("failed", "cancelled", "rejected"):
            raise EvidenceFailed("stored step ended " + row["state"])
        if row["state"] == "skipped":
            continue
        if row["state"] not in ("approved", "completed"):
            raise EvidenceUnavailable("stored step is not terminal")
        # Step conversation IDs can be per-job execution IDs, not the outer
        # turn conversation. Session ID is the persisted originating identity.
        if row["session_id"] != ctx["session_id"]:
            raise EvidenceUnavailable("step session identity missing or mismatched")
        envelope = _object(row["result"])
        if envelope.get("error") or envelope.get("success") is False or envelope.get("status") in (
                "failed", "rejected", "cancelled", "error"):
            raise EvidenceFailed("stored execution envelope reports failure")
        if (not row["agent_id"] or not row["job_id"] or
                envelope.get("task_id") != ctx["task_id"] or
                envelope.get("step_id") != row["id"] or
                envelope.get("job_id") != row["job_id"]):
            raise EvidenceUnavailable("step/job/task envelope identity is not provable")
        # No turn_id column exists today. Check any explicit envelope identity
        # when present, without inventing a SELECT column or parsing narration.
        if "turn_id" in envelope and envelope["turn_id"] != ctx["turn_id"]:
            raise EvidenceUnavailable("stored execution belongs to another turn")
        if envelope.get("success") is not True or envelope.get("status") != "completed":
            raise EvidenceUnavailable("stored execution success is not proven")
        if invocations:
            calls = envelope.get("tool_invocations")
            execution = envelope.get("execution_conversation_id")
            if not isinstance(calls, list) or not isinstance(execution, str) or not execution:
                raise EvidenceUnavailable("no persisted call IDs for this step")
            for call in calls:
                if not isinstance(call, dict):
                    raise EvidenceUnavailable("malformed invocation record")
                if call.get("tool_name") != tool:
                    continue
                call_id = call.get("tool_call_id")
                if (not isinstance(call_id, str) or not call_id or
                        call.get("agent_id") != row["agent_id"] or
                        call.get("conversation_id") != execution or call.get("conflict")):
                    raise EvidenceUnavailable("invocation identity missing, mismatched, or conflicting")
                if call.get("success") is False:
                    raise EvidenceFailed("recorded tool invocation failed")
                if call.get("success") is not True or call.get("cached") is not False:
                    raise EvidenceUnavailable("invocation is cached or success is not proven")
                identity = (row["job_id"], execution, call_id)
                if identity in seen_calls:
                    if seen_calls[identity] != call:
                        raise EvidenceUnavailable("conflicting duplicate invocation")
                    continue
                seen_calls[identity] = call
                entries.append(dict(call, step_id=row["id"], job_id=row["job_id"]))
            continue
        projection = envelope.get("tool_evidence", [])
        if not isinstance(projection, list):
            raise EvidenceUnavailable("malformed tool evidence projection")
        for entry in projection:
            if not isinstance(entry, dict):
                raise EvidenceUnavailable("malformed tool evidence entry")
            if entry.get("tool") != tool:
                continue
            if entry.get("source") != tool or not all(
                    isinstance(entry.get(k), str) and entry[k].strip()
                    for k in ("type", "subject", "value")):
                raise EvidenceUnavailable("tool evidence identity or value is incomplete")
            entries.append(dict(entry, step_id=row["id"], job_id=row["job_id"]))
    if not entries:
        raise EvidenceUnavailable("no tool-issued " + tool + " evidence for terminal task")
    return entries


def require_tool(content_grade, tool="json_extract", count=1):
    """Require correct content AND recorded execution; absence is UNVERIFIED.

    Require at least count distinct successful, uncached invocation records.
    Completion delivery is best effort, so observed calls prove a lower bound,
    never an exact count. Missing records cannot manufacture a passing result.
    """
    def grade(reply, ctx):
        verdict, detail = content_grade(reply, ctx)
        if verdict != "PASS":
            return verdict, detail
        try:
            entries = read_tool_evidence(ctx, tool, invocations=True)
        except EvidenceFailed as exc:
            return "FAIL", str(exc)
        except (EvidenceUnavailable, OSError, ValueError) as exc:
            return "UNVERIFIED", str(exc)
        if len(entries) < count:
            return "UNVERIFIED", f"recorded {len(entries)} {tool} calls; need at least {count}"
        return "PASS", f"verified {len(entries)} recorded {tool} calls and correct content"
    return grade
