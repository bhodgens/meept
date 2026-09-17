"""Validate HTTP envelopes before any content grading."""
import contextlib
import io
import unittest
from unittest.mock import patch

import client


class ClientTest(unittest.TestCase):
    def test_tls_verification_is_default(self):
        self.assertFalse(client.parse_args([]).insecure)

    def test_error_envelope_cannot_become_successful_reply(self):
        c = client.Client.__new__(client.Client)
        c.transport = "legacy"
        c.base, c.key, c.timeout, c.ctx = "https://example.invalid", "fixture", 1, None
        with patch.object(client.urllib.request, "urlopen", return_value=io.BytesIO(
                b'{"reply":"391","error":"provider failed"}')):
            with self.assertRaisesRegex(ValueError, "provider failed"):
                c.chat("multiply", "fixture-session")

    def test_ack_object_is_not_silently_discarded(self):
        c = client.Client.__new__(client.Client)
        c.transport = "legacy"
        c.base, c.key, c.timeout, c.ctx = "https://example.invalid", "fixture", 1, None
        with patch.object(client.urllib.request, "urlopen", return_value=io.BytesIO(
                b'{"turn_id":"turn-fixture","conversation_id":"conv-fixture"}')):
            with self.assertRaisesRegex(ValueError, "reply"):
                c.chat("multiply", "fixture-session")

    def test_nonpositive_timeout_rejected(self):
        with contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit):
            client.parse_args(["--timeout", "0"])


if __name__ == "__main__":
    unittest.main()
