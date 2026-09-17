"""A sweep command must not report success on missing or failing cases."""
import contextlib
import io
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

import run_adversarial
import run_sweep


class RunnerExitTest(unittest.TestCase):
    def run_case(self, module, verdict, filters=None):
        args = SimpleNamespace(categories=filters or [], home="/unused", timeout=10)
        with patch.object(module.client, "parse_args", return_value=args), \
                patch.object(module.client, "Client", return_value=Mock()), \
                patch.object(module.async_wait, "run_async_aware", return_value=(verdict, "test", 0)), \
                contextlib.redirect_stdout(io.StringIO()):
            return module.main()

    def test_nonpass_verdicts_fail_both_commands(self):
        for module in (run_sweep, run_adversarial):
            for verdict in ("FAIL", "WEAK", "TIMEOUT", "ERROR"):
                with self.subTest(module=module.__name__, verdict=verdict):
                    self.assertEqual(self.run_case(module, verdict), 1)

    def test_all_pass_succeeds(self):
        for module in (run_sweep, run_adversarial):
            self.assertEqual(self.run_case(module, "PASS"), 0)

    def test_unknown_filter_rejected_before_client(self):
        for module in (run_sweep, run_adversarial):
            args = SimpleNamespace(categories=["TYPO"], home="/unused", timeout=10)
            with patch.object(module.client, "parse_args", return_value=args), \
                    patch.object(module.client, "Client") as client, \
                    contextlib.redirect_stderr(io.StringIO()):
                self.assertEqual(module.main(), 2)
                client.assert_not_called()


if __name__ == "__main__":
    unittest.main()
