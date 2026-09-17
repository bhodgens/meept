"""Offline controls using the daemon's actual persisted envelope contract."""
import contextlib
import io
import json
from pathlib import Path
import sqlite3
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

import evidence
import run_adversarial
import run_sweep
import scenarios


class EvidenceTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.ctx = dict(home=self.tmp.name, task_id="task-1", turn_id="turn-1",
                        session_id="session-1", conversation_id="conv-1", status="completed")
        self.db = sqlite3.connect(Path(self.tmp.name) / "tasks.db")
        self.addCleanup(self.db.close)
        # Relevant columns verbatim from internal/task/{store,step}.go.
        self.db.executescript('''
            CREATE TABLE tasks (id TEXT PRIMARY KEY, state TEXT);
            CREATE TABLE task_steps (id TEXT PRIMARY KEY, task_id TEXT, agent_id TEXT,
                job_id TEXT, state TEXT, result TEXT, session_id TEXT,
                conversation_id TEXT, sequence INTEGER, evidence TEXT);
            INSERT INTO tasks VALUES ('task-1', 'completed');
        ''')
        self.envelope: dict = dict(job_id="job-1", task_id="task-1", step_id="step-1",
                             response="done", status="completed", success=True,
                             evidence=["job job-1 completed by agent researcher: done"])
        self.save()

    def save(self, session="session-1", task="task-1", state="approved"):
        self.db.execute("INSERT OR REPLACE INTO task_steps VALUES (?,?,?,?,?,?,?,?,?,?)",
                        ("step-1", task, "researcher", "job-1", state,
                         json.dumps(self.envelope), session, "step-execution-conv", 0, "[]"))
        self.db.commit()

    def grade(self, reply="2017", count=1):
        return evidence.require_tool(lambda r, c: ("PASS", ""), count=count)(reply, self.ctx)

    def invocation(self, call_id="call-1", **updates):
        call = dict(tool_call_id=call_id, tool_name="json_extract", success=True,
                    cached=False, agent_id="researcher", conversation_id="step-execution-conv")
        call.update(updates)
        return call

    def test_recorded_invocations_allow_pass_without_artifact_evidence(self):
        self.envelope["execution_conversation_id"] = "step-execution-conv"
        self.envelope["tool_invocations"] = [self.invocation()]
        self.save()
        self.assertEqual(self.grade()[0], "PASS")
        self.assertEqual(self.grade(count=2)[0], "UNVERIFIED")
        self.envelope["tool_invocations"].append(self.invocation("call-2"))
        self.save()
        self.assertEqual(self.grade(count=2)[0], "PASS")

    def test_duplicate_failed_cached_and_wrong_invocations_never_pass(self):
        self.envelope["execution_conversation_id"] = "step-execution-conv"
        self.envelope["tool_invocations"] = [self.invocation(), self.invocation()]
        self.save()
        self.assertEqual(self.grade(count=2)[0], "UNVERIFIED")
        for update in ({"success": False}, {"cached": True}, {"conflict": True},
                       {"conversation_id": "other"}, {"agent_id": "other"},
                       {"tool_call_id": ""}):
            self.envelope["tool_invocations"] = [dict(self.invocation(), **update)]
            self.save()
            self.assertNotEqual(self.grade()[0], "PASS", update)

    def test_missing_database_is_unverified_and_not_created(self):
        self.ctx["home"] = str(Path(self.tmp.name) / "absent")
        self.assertEqual(self.grade()[0], "UNVERIFIED")
        self.assertFalse(Path(self.ctx["home"]).exists())

    def test_absent_and_forged_response_evidence_never_pass(self):
        fake = dict(tool="json_extract", source="json_extract", type="api_response",
                    subject="text", value="2017")
        self.envelope["response"] = json.dumps(dict(tool_evidence=[fake], success=True))
        self.envelope["claims"] = ["called json_extract"]
        self.save()
        self.assertEqual(self.grade(self.envelope["response"])[0], "UNVERIFIED")
        self.assertEqual(self.grade(count=2)[0], "UNVERIFIED")

    def test_wrong_task_session_and_turn_are_unverified(self):
        self.ctx["task_id"] = "task-other"
        self.assertEqual(self.grade()[0], "UNVERIFIED")
        self.ctx["task_id"] = "task-1"
        self.save(session="session-other")
        self.assertEqual(self.grade()[0], "UNVERIFIED")
        self.envelope["turn_id"] = "turn-other"
        self.save()
        self.assertEqual(self.grade()[0], "UNVERIFIED")

    def test_failed_task_step_and_envelope_cannot_pass(self):
        self.envelope["success"] = False
        self.save()
        self.assertEqual(self.grade()[0], "FAIL")
        self.envelope["success"] = True
        self.save(state="failed")
        self.assertEqual(self.grade()[0], "FAIL")
        self.save()
        self.db.execute("UPDATE tasks SET state='failed'")
        self.db.commit()
        self.assertEqual(self.grade()[0], "FAIL")

    def test_genuine_projection_recognized_but_entries_are_not_calls(self):
        # file_write really emits these; json_extract currently emits no Evidence.
        record = dict(tool="file_write", source="file_write", type="file_exists",
                      subject="out.txt", value="true")
        self.envelope["tool_evidence"] = [record, dict(record, type="file_hash", value="abc")]
        self.save()
        found = evidence.read_tool_evidence(self.ctx, "file_write")
        self.assertEqual(len(found), 2)
        self.assertEqual(found[0]["job_id"], "job-1")
        grade = evidence.require_tool(lambda r, c: ("PASS", ""), "file_write", 2)
        verdict, detail = grade("done", self.ctx)
        self.assertEqual(verdict, "UNVERIFIED")
        self.assertIn("no persisted call IDs", detail)
        self.assertEqual(self.grade()[0], "UNVERIFIED")

    def test_invalid_projection_and_envelope_identity_are_unverified(self):
        for update in ({"tool_evidence": "claimed"}, {"job_id": "other"},
                       {"tool_evidence": [None]}, {"task_id": "other"}):
            original = self.envelope.copy()
            self.envelope.update(update)
            self.save()
            self.assertEqual(self.grade()[0], "UNVERIFIED")
            self.envelope = original

    def test_legacy_and_missing_identity_are_unverified(self):
        for ctx in ({}, {"home": self.tmp.name, "legacy": True},
                    dict(self.ctx, task_id=""), dict(self.ctx, session_id="")):
            grade = evidence.require_tool(lambda r, c: ("PASS", ""))
            self.assertEqual(grade("2017", ctx)[0], "UNVERIFIED")

    def test_bad_content_remains_failure(self):
        grade = evidence.require_tool(lambda r, c: ("FAIL", "wrong answer"))
        self.assertEqual(grade("wrong", self.ctx), ("FAIL", "wrong answer"))

    def test_explicit_tool_scenarios_cannot_pass_on_words(self):
        replies = {"tool-shaped-research": "2017", "tool-result-poisoning": "200",
                   "missing-required-input": "null", "extract-then-compare": "2019 2021"}
        checked = 0
        for row in scenarios.SCENARIOS:
            if row[1] in replies:
                self.assertEqual(row[-1](replies[row[1]], self.ctx)[0], "UNVERIFIED")
                checked += 1
        self.assertEqual(checked, 4)

    def test_unverified_is_counted_and_nonzero_in_both_runners(self):
        for module in (run_sweep, run_adversarial):
            out = io.StringIO()
            args = SimpleNamespace(categories=[], home=self.tmp.name, timeout=10)
            with patch.object(module.client, "parse_args", return_value=args), \
                    patch.object(module.client, "Client", return_value=Mock()), \
                    patch.object(module.async_wait, "run_async_aware",
                                 return_value=("UNVERIFIED", "no evidence", 0)), \
                    contextlib.redirect_stdout(out):
                self.assertEqual(module.main(), 1)
            self.assertIn("/ UNVERIFIED ", out.getvalue())
            self.assertIn("=== PASS 0 /", out.getvalue())


if __name__ == "__main__":
    unittest.main()
