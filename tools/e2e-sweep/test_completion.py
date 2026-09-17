"""Completion guards must fail closed before scenario graders run."""
import unittest
from unittest.mock import Mock, patch

import async_wait


class CompletionGuardsTest(unittest.TestCase):
    def test_failed_step_never_passes_content_grade(self):
        client = Mock(transport="legacy")
        client.chat.return_value = "## starting task task-123.4-5"
        grade = Mock(return_value=("PASS", ""))
        with patch.object(async_wait, "wait_steps", return_value=[("chat", "failed", "391")]):
            verdict, _, _ = async_wait.run_async_aware(
                client, "/unused", "SWEEP", "chat", "chat", "multiply", grade)
        self.assertEqual(verdict, "FAIL")
        grade.assert_not_called()

    def test_empty_reply_never_passes_permissive_grade(self):
        client = Mock(transport="legacy")
        client.chat.return_value = "  "
        verdict, _, _ = async_wait.run_async_aware(
            client, "/unused", "GARBAGE", "empty", "chat", " ",
            lambda r, c: ("PASS", ""))
        self.assertEqual(verdict, "FAIL")

    def test_final_step_wins_over_long_earlier_narration(self):
        rows = [("coder", "approved", "wrong " * 100),
                ("verifier", "approved", '{"response":"final answer"}')]
        self.assertEqual(async_wait.best_response(rows), "final answer")

    def test_non_string_envelope_is_rejected(self):
        with self.assertRaises(ValueError):
            async_wait.best_response([("chat", "approved", '{"response":123}')])

    def test_legacy_stubs_never_pass_directly(self):
        for text in ("Task task-abc is still running; results will arrive when it completes.",
                     "Task task-abc failed after reaching terminal state.",
                     "Task task-abc completed."):
            client = Mock(timeout=1, transport="legacy")
            client.chat.return_value = text
            grade = Mock(return_value=("PASS", ""))
            with patch.object(async_wait, "wait_steps", return_value=[]):
                verdict, _, _ = async_wait.run_async_aware(
                    client, "/unused", "SWEEP", "writer", "writer", "write", grade)
            self.assertNotEqual(verdict, "PASS", text)
            grade.assert_not_called()

    def test_plain_json_deliverables_survive(self):
        for text in ('391', '{"title":"paper","year":2017}', '[1,2]', 'null'):
            self.assertEqual(async_wait.best_response([("chat", "approved", text)]), text)

    def test_failed_envelope_is_rejected(self):
        for text in ('{"response":"391","success":false}',
                     '{"response":"391","status":"failed"}',
                     '{"response":"391","error":"provider failed"}'):
            with self.assertRaises(ValueError):
                async_wait.best_response([("chat", "approved", text)])

    def test_skipped_final_step_does_not_hide_result(self):
        self.assertEqual(async_wait.best_response([
            ("chat", "approved", "answer"), ("chat", "skipped", "")]), "answer")

    def test_task_id_preserves_random_suffix(self):
        self.assertEqual(async_wait.extract_task_id("## Starting task task-aBc_123-Z9"),
                         "task-aBc_123-Z9")


if __name__ == "__main__":
    unittest.main()
