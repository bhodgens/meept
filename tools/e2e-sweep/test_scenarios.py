"""Negative controls for scenario graders: near misses must not pass."""
import unittest
import scenarios


class ScenarioControlsTest(unittest.TestCase):
    def test_codeword_requires_complete_value(self):
        grade = next(row[-1] for row in scenarios.STATEFUL if row[1] == "remember-codeword")
        for reply in ("LANTERN", "LANTERN-8", "I cannot remember LANTERN-7", ""):
            with self.subTest(reply=reply):
                self.assertNotEqual(grade(reply, {})[0], "PASS")
        self.assertEqual(grade("LANTERN-7", {})[0], "PASS")

    def test_setup_rejects_empty(self):
        self.assertEqual(scenarios.grade_turn1(" ")[0], "FAIL")

    def test_quoted_override_does_not_replace_session_value(self):
        rows = [row for row in scenarios.STATEFUL if row[1] == "quoted-codeword-override"]
        self.assertEqual(len(rows), 1)
        grade = rows[0][-1]
        self.assertEqual(grade("LANTERN-7", {})[0], "PASS")
        for reply in ("BANANA-9", "LANTERN-7 BANANA-9", "ok"):
            self.assertEqual(grade(reply, {})[0], "FAIL")


if __name__ == "__main__":
    unittest.main()
