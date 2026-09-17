"""Offline regression for stateful acknowledgment ordering."""
import contextlib
import io
from types import SimpleNamespace
import unittest
from unittest.mock import patch

import run_adversarial


class StatefulCompletionTest(unittest.TestCase):
    def test_both_turns_grade_completed_responses_in_one_session(self):
        events = []

        class Client:
            transport = "legacy"
            def chat(self, message, session, agent):
                events.append(("chat", session))
                return "## starting task task-123.4-5"

        def wait(*args, **kwargs):
            events.append(("complete", None))
            return [("chat", "completed", '{"response":"LANTERN-7"}')]

        def grade(reply, ctx=None):
            return ("PASS", "") if reply == "LANTERN-7" else ("FAIL", "graded acknowledgment")

        args = SimpleNamespace(categories=[], home="/unused", timeout=10)
        case = ("STATEFUL", "remember", "chat", "remember", "recall", grade)
        output = io.StringIO()
        with patch.object(run_adversarial.client, "parse_args", return_value=args), \
                patch.object(run_adversarial.client, "Client", return_value=Client()), \
                patch.object(run_adversarial.scenarios, "SCENARIOS", []), \
                patch.object(run_adversarial.scenarios, "STATEFUL", [case]), \
                patch.object(run_adversarial.scenarios, "grade_turn1", grade), \
                patch.object(run_adversarial.async_wait, "wait_steps", side_effect=wait), \
                contextlib.redirect_stdout(output):
            run_adversarial.main()
        self.assertIn("=== PASS 1 /", output.getvalue())
        self.assertEqual([e[0] for e in events], ["chat", "complete", "chat", "complete"])
        self.assertEqual(events[0][1], events[2][1])


if __name__ == "__main__":
    unittest.main()
