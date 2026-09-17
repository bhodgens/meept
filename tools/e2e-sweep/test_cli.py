"""Run real command entrypoints against a local HTTP fixture, not a model."""
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
import unittest


class CLITest(unittest.TestCase):
    def test_real_cli_status_and_stateful_order(self):
        messages = []

        class Handler(BaseHTTPRequestHandler):
            def do_POST(self):
                data = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
                messages.append(data)
                body = json.dumps({"reply": "LANTERN-7"}).encode()
                self.send_response(200)
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

            def log_message(self, format, *args):
                pass

        server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            with tempfile.TemporaryDirectory() as home:
                Path(home, "dev_key").write_text("offline-fixture-only")
                base = f"http://127.0.0.1:{server.server_port}"
                script = Path(__file__).parent / "run_adversarial.py"
                result = subprocess.run([sys.executable, str(script), "--base-url", base,
                                         "--home", home, "--transport", "legacy", "STATEFUL"],
                                        capture_output=True, text=True, timeout=10)
                # remember and quoted-override pass; extraction must fail.
                self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
                self.assertIn("PASS 2 / WEAK 0 / FAIL 1", result.stdout)
                self.assertEqual(len(messages), 6)
                for index in (0, 2, 4):
                    self.assertEqual(messages[index]["conversation_id"],
                                     messages[index + 1]["conversation_id"])
                self.assertEqual(len({m["conversation_id"] for m in messages}), 3)
        finally:
            server.shutdown()
            server.server_close()
            thread.join(timeout=2)


if __name__ == "__main__":
    unittest.main()
