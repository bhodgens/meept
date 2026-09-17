"""Real SQLite fixtures for task completion and read-only polling."""
from pathlib import Path
import sqlite3
import tempfile
import unittest

import async_wait


class StoreTest(unittest.TestCase):
    def test_missing_store_is_not_created(self):
        with tempfile.TemporaryDirectory() as home:
            with self.assertRaises(sqlite3.OperationalError):
                async_wait.wait_steps(home, "task-missing", timeout=0.01)
            self.assertFalse((Path(home) / "tasks.db").exists())

    def test_failed_task_with_no_steps_fails_without_timeout(self):
        with tempfile.TemporaryDirectory() as home:
            with sqlite3.connect(str(Path(home) / "tasks.db")) as db:
                db.executescript("CREATE TABLE tasks(id TEXT, state TEXT);"
                                 "CREATE TABLE task_steps(id TEXT, task_id TEXT, agent_id TEXT, "
                                 "state TEXT, result TEXT, sequence INTEGER);"
                                 "INSERT INTO tasks VALUES('task-x','failed');")
            with self.assertRaisesRegex(ValueError, "failed"):
                async_wait.wait_steps(home, "task-x", timeout=0.01)

    def test_skipped_step_is_terminal_and_sequence_orders_result(self):
        with tempfile.TemporaryDirectory() as home:
            with sqlite3.connect(str(Path(home) / "tasks.db")) as db:
                db.executescript("CREATE TABLE tasks(id TEXT, state TEXT);"
                                 "CREATE TABLE task_steps(id TEXT, task_id TEXT, agent_id TEXT, "
                                 "state TEXT, result TEXT, sequence INTEGER);"
                                 "INSERT INTO tasks VALUES('task-x','completed');"
                                 "INSERT INTO task_steps VALUES('s3','task-x','chat','skipped','',3);"
                                 "INSERT INTO task_steps VALUES('s2','task-x','chat','approved','final',2);"
                                 "INSERT INTO task_steps VALUES('s1','task-x','chat','approved','early',1);")
            rows = async_wait.wait_steps(home, "task-x", timeout=0.1)
            self.assertEqual(len(rows), 3)
            self.assertEqual(async_wait.best_response(rows), "final")

    def test_completed_steps_do_not_complete_running_task(self):
        with tempfile.TemporaryDirectory() as home:
            with sqlite3.connect(str(Path(home) / "tasks.db")) as db:
                db.executescript("CREATE TABLE tasks(id TEXT, state TEXT);"
                                 "CREATE TABLE task_steps(id TEXT, task_id TEXT, agent_id TEXT, "
                                 "state TEXT, result TEXT, sequence INTEGER);"
                                 "INSERT INTO tasks VALUES('task-x','running');"
                                 "INSERT INTO task_steps VALUES('s1','task-x','chat','approved','done',1);")
            self.assertEqual(async_wait.wait_steps(home, "task-x", timeout=0.01), [])


if __name__ == "__main__":
    unittest.main()
