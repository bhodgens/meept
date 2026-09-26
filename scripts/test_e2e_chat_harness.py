"""Offline tests execute shell assertions and both embedded MCP clients."""
import contextlib
import io
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

SOURCE = Path(__file__).with_name("e2e-naive-user-chat.sh").read_text()


class ReplyShapeTest(unittest.TestCase):
    def test_whitespace_json_and_empty_replies_fail(self):
        function = SOURCE.split("assert_reply_shape() {", 1)[1].split("\n}\n", 1)[0]
        for reply in ('{"x":1}\n', '  {"x":1}\n\t', '', ' \n', 'The file is hello.txt.\n'):
            with self.subTest(reply=reply), tempfile.TemporaryDirectory() as home:
                path = Path(home) / "reply"
                path.write_text(reply)
                command = ('note_result() { printf "%s %s\\n" "$1" "$2"; }; '
                           'assert_reply_shape() {' + function + '\n}; assert_reply_shape t1 "$1"')
                result = subprocess.run(["bash", "-c", command, "fixture", str(path)],
                                        capture_output=True, text=True, timeout=5)
                if reply.startswith("The file"):
                    self.assertNotIn("FAIL", result.stdout, result.stdout + result.stderr)
                else:
                    self.assertIn("FAIL", result.stdout, result.stdout + result.stderr)


class EmbeddedClientTest(unittest.TestCase):
    def run_client(self, marker, tool_result, init_error=False):
        start = SOURCE.index(marker)
        code = SOURCE[start:].split("\n", 1)[1].split("\nPY\n", 1)[0]
        # Warmup protocol order (harness embeds it): 1 initialize,
        # 2 meept_subscribe -> subscription_id JSON, 3 meept_chat_submit
        # -> turn_id ack, 4 meept_wait_turn -> final result. For the
        # init-error arm, id 1 is an error so the client exits before
        # anything else. The tool_result appears at id 4 (wait_turn) so
        # the isError arm can force the error path.
        if marker == 'warmup_reply=':
            # async warmup: init, subscribe, submit-ack, wait_turn result
            messages = [{"id": 1, "error": {"message": "bad initialization"}} if init_error else
                        {"id": 1, "result": {}},
                        {"id": 2, "result": {"content": [{"type": "text",
                            "text": "{\"subscription_id\": \"sub-fixture\"}"}]}},
                        {"id": 3, "result": {"content": [{"type": "text",
                            "text": "{\"turn_id\": \"turn-fixture\"}"}]}},
                        {"id": 4, "result": tool_result}]
        else:
            # turn client sync branch: init, meept_send result on id3
            messages = [{"id": 1, "error": {"message": "bad initialization"}} if init_error else
                        {"id": 1, "result": {}},
                        {"id": 3, "result": tool_result}]

        class Process:
            stdin = io.StringIO()
            stdout = io.StringIO("".join(json.dumps(m) + "\n" for m in messages))
            waited = False
            killed = False

            def kill(self):
                self.killed = True

            def wait(self, timeout=None):
                self.waited = True
                return 0

            def poll(self):
                return 0 if self.killed else None

        process = Process()
        argv = ["fixture", "cli", "sock", "/tmp", "/tmp", "session", "test", "1", "300", "1", "hello"]
        stderr_io = io.StringIO()
        with patch.object(subprocess, "Popen", return_value=process), \
                patch.object(sys, "argv", argv), contextlib.redirect_stdout(io.StringIO()), \
                contextlib.redirect_stderr(stderr_io):
            try:
                exec(compile(code, "embedded-mcp-client", "exec"), {})
                status = 0
            except SystemExit as exc:
                status = exc.code
        return status, process, stderr_io

    def test_tool_error_cannot_pass_as_text(self):
        for marker in ("warmup_reply=", "        out=\"$(python3"):
            with self.subTest(marker=marker):
                status, process, stderr_io = self.run_client(marker, {
                    "isError": True, "content": [{"type": "text", "text": "hello.txt created"}]})
                self.assertNotEqual(status, 0)
                self.assertTrue(process.waited, "child must be reaped")

    def test_initialization_error_reaps_child(self):
        for marker in ("warmup_reply=", '        out="$(python3'):
            status, process, stderr_io = self.run_client(marker, {
                "content": [{"type": "text", "text": "ok"}]}, init_error=True)
            self.assertNotEqual(status, 0)
            self.assertTrue(process.waited)

    def test_success_reaps_child(self):
        for marker in ("warmup_reply=", "        out=\"$(python3"):
            status, process, stderr_io = self.run_client(marker, {
                "content": [{"type": "text", "text": "ok"}]})
            if status != 0:
                self.fail(f"success-path exit {status}; stderr: {stderr_io.getvalue()[:400]}")
            self.assertTrue(process.waited, "child must be reaped")


if __name__ == "__main__":
    unittest.main()
