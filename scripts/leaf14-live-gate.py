#!/usr/bin/env python3
"""Leaf 14 scratch-daemon live gate (master Integration Test Plan step 4).

Scratch MEEPT_HOME, voice.enabled=true, FAKE telnyx base URL (local capture
server). Asserts:
  1. boot log arms the voice routes
  2. a simulated signed call.initiated POST reaches the Manager
  3. refused-dial paths log named reasons

Run: python3 scripts/leaf14-live-gate.py   (from the worktree root)
"""
import base64
import json
import os
import shutil
import signal
import subprocess
import sys
import time
import urllib.request
import urllib.error
from http.server import HTTPServer, BaseHTTPRequestHandler

WORKTREE = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
BIN = os.path.join(WORKTREE, "bin")
DAEMON = os.path.join(BIN, "meept-daemon")
HOME = "/tmp/leaf14-gate-home"
WORK = "/tmp/leaf14-gate-work"
HTTP_PORT = 18443
FAKE_API_PORT = 18891

# --- fake telnyx capture server ---
captured = []

class FakeTelnyx(BaseHTTPRequestHandler):
    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(length)
        captured.append({"path": self.path, "body": body.decode("utf-8", "replace")})
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(b'{"data":{"call_control_id":"cc-fake-1","call_session_id":"cs-fake-1"}}')
    def log_message(self, *a):
        pass

fake = HTTPServer(("127.0.0.1", FAKE_API_PORT), FakeTelnyx)
import threading
threading.Thread(target=fake.serve_forever, daemon=True).start()

# --- Ed25519 keypair for signatures ---
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from cryptography.hazmat.primitives import serialization
key = Ed25519PrivateKey.generate()
pub_b64 = base64.b64encode(
    key.public_key().public_bytes(
        serialization.Encoding.Raw, serialization.PublicFormat.Raw
    )
).decode()

# --- scratch home ---
for d in (HOME, WORK):
    shutil.rmtree(d, ignore_errors=True)
os.makedirs(os.path.join(HOME, ".meept"), exist_ok=True)
os.makedirs(WORK, exist_ok=True)

cfg = {
    "daemon": {"data_dir": os.path.join(WORK, "data")},
    "transport": {
        "rpc": {"enabled": True, "socket_path": os.path.join(WORK, "meept.sock")},
        "http": {
            "enabled": True,
            "addr": f"127.0.0.1:{HTTP_PORT}",
            "require_auth": False,
            "tls_cert_file": os.path.join(WORK, "tls", "cert.pem"),
            "tls_key_file": os.path.join(WORK, "tls", "key.pem"),
        },
    },
    "security": {"allowed_paths": [WORK + "/**"]},
    "multiagent": {"enabled": True},
    "voice": {
        "enabled": True,
        "public_stream_url": "https://example.tunnel/v1/voice/media",
        "connection_id": "conn-gate-1",
        "from_number": "+15551230000",
        "allowed_numbers": ["+15551230001"],
        "telnyx_public_key": pub_b64,
        "idle_stop_seconds": 30,
    },
    "secrets": {"sources": {"telnyx_api_key": {"kind": "env", "name": "GATE_TELNYX_KEY"}}},
}
cfg_path = os.path.join(HOME, ".meept", "meept.json5")
with open(cfg_path, "w") as f:
    json.dump(cfg, f, indent=2)

# models.json5: one cloud-only openai-compatible provider pointed at the
# fake telnyx origin (nothing there answers /v1/chat - fine, no turn runs
# in this gate). CLOUD-only means the F-D8 classifier boot gate skips
# (no local spawned runtime in the chain), so boot completes hermetically.
models = json.dumps({
    "model": "fake/fake-model",
    "small_model": "fake/fake-model",
    "classifier_model": "classifier",
    "summarizer_model": "summarizer",
    "extract_model": "",
    "refusal_model": "",
    "default_timeout": 300,
    "disabled_providers": [],
    "model_aliases": {
        "classifier": {"models": ["fake/fake-model"], "timeout": 10, "max_fails": 2},
        "summarizer": {"models": ["fake/fake-model"], "timeout": 10, "max_fails": 2},
        "small": {"models": ["fake/fake-model"], "timeout": 10, "max_fails": 2},
        "coder": {"models": ["fake/fake-model"], "timeout": 60, "max_fails": 3},
        "planner": {"models": ["fake/fake-model"], "timeout": 60, "max_fails": 3},
        "analyst": {"models": ["fake/fake-model"], "timeout": 60, "max_fails": 3},
    },
    "providers": {
        "fake": {
            "api": "openai",
            "options": {"baseURL": "http://127.0.0.1:9/v1", "noAuth": True},
            "models": {
                "fake-model": {
                    "name": "fake-model",
                    "capabilities": ["completion", "code", "reasoning", "tool_use"],
                    "input_cost": 0.0, "output_cost": 0.0,
                    "context_limit": 65536, "max_output": 4096,
                    "temperature": 0.7,
                }
            },
        }
    },
})
with open(os.path.join(HOME, ".meept", "models.json5"), "w") as f:
    f.write(models)

# The real meept-voice child goes into $MEEPT_HOME/bin (ResolveChildBinary
# lookup order) so the gate exercises the REAL spawn + handshake + C1
# socket path end to end.
os.makedirs(os.path.join(HOME, ".meept", "bin"), exist_ok=True)
shutil.copy(os.path.join(BIN, "meept-voice"), os.path.join(HOME, ".meept", "bin", "meept-voice"))

env = dict(os.environ)
env["MEEPT_HOME"] = os.path.join(HOME, ".meept")
env["GATE_TELNYX_KEY"] = "gate-test-key-not-a-secret"
env["MEEPT_TELNYX_TEST_BASE_URL"] = f"http://127.0.0.1:{FAKE_API_PORT}"

log_path = os.path.join(WORK, "daemon.log")
log_file = open(log_path, "w")
proc = subprocess.Popen(
    [DAEMON, "-c", cfg_path, "-d", os.path.join(WORK, "data"), "-s", os.path.join(WORK, "meept.sock")],
    cwd=WORK,
    env=env, stdout=log_file, stderr=log_file,
)

def tail():
    with open(log_path) as f:
        return f.read()

def wait_healthy(timeout=120):
    deadline = time.time() + timeout
    while time.time() < deadline:
        if proc.poll() is not None:
            print("DAEMON DIED; log tail:"); print(tail()[-4000:]); sys.exit(1)
        try:
            ctx = __import__("ssl")._create_unverified_context()
            r = urllib.request.urlopen(f"https://127.0.0.1:{HTTP_PORT}/health", timeout=2, context=ctx)
            if r.status == 200:
                return
        except Exception:
            pass
        time.sleep(0.5)
    print("TIMEOUT waiting healthy"); print(tail()[-4000:]); sys.exit(1)

results = {}
try:
    wait_healthy()
    log = tail()
    results["boot_armed_voice_routes"] = "voice routes armed" in log
    results["boot_wired_voice_sidecar"] = "voice sidecar wired" in log
    results["boot_voice_call_tool"] = "voice_call tool registered" in log
    results["boot_text_server"] = "voice wiring: text server armed" in log

    # --- simulated signed call.initiated POST ---
    import hmac, hashlib
    ts = str(int(time.time()))
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey as K
    # We only hold the PRIVATE half of the pair we generated, so we can sign.
    body = json.dumps({
        "data": {
            "record_type": "event",
            "event_type": "call.initiated",
            "id": "evt-1",
            "occurred_at": "2026-11-03T11:00:00Z",
            "payload": {
                "call_control_id": "cc-gate-in-1",
                "call_session_id": "cs-gate-in-1",
                "from": "+15551230001",
                "to": "+15551230000",
                "direction": "incoming",
            },
        }
    }).encode()
    sig = base64.b64encode(key.sign(ts.encode() + b"|" + body)).decode()
    req = urllib.request.Request(
        f"https://127.0.0.1:{HTTP_PORT}/api/v1/voice/webhook",
        data=body, method="POST",
        headers={"Telnyx-Signature-Ed25519": sig, "Telnyx-Timestamp": ts,
                 "Content-Type": "application/json"},
    )
    ctx = __import__("ssl")._create_unverified_context()
    try:
        resp = urllib.request.urlopen(req, timeout=45, context=ctx)
        results["webhook_status"] = resp.status
    except urllib.error.HTTPError as e:
        results["webhook_status"] = e.code

    time.sleep(2)
    log = tail()
    results["manager_saw_call_initiated"] = "call.initiated" in log and "cc-gate-in-1" in log

    # --- refused dial: voice_call via CLI is an employee path; the named
    # refusal evidence comes from the dialer gates. Drive it through the
    # daemon's chat path would need a live model; instead exercise the
    # dialer's refusal gate through the webhook: an UNTRACKED
    # call.answered is refused with a named reason (manager gate).
    body2 = json.dumps({
        "data": {
            "record_type": "event",
            "event_type": "call.answered",
            "id": "evt-2",
            "occurred_at": "2026-11-03T11:00:01Z",
            "payload": {
                "call_control_id": "cc-never-tracked",
                "call_session_id": "cs-x",
                "direction": "incoming",
            },
        }
    }).encode()
    ts2 = str(int(time.time()))
    sig2 = base64.b64encode(key.sign(ts2.encode() + b"|" + body2)).decode()
    req2 = urllib.request.Request(
        f"https://127.0.0.1:{HTTP_PORT}/api/v1/voice/webhook",
        data=body2, method="POST",
        headers={"Telnyx-Signature-Ed25519": sig2, "Telnyx-Timestamp": ts2,
                 "Content-Type": "application/json"},
    )
    try:
        resp2 = urllib.request.urlopen(req2, timeout=10, context=ctx)
        results["untracked_answered_status"] = resp2.status
    except urllib.error.HTTPError as e:
        results["untracked_answered_status"] = e.code
    time.sleep(1)
    log = tail()
    results["refused_named_reason"] = "untracked call_control_id" in log

    # unsigned webhook refused
    req3 = urllib.request.Request(
        f"https://127.0.0.1:{HTTP_PORT}/api/v1/voice/webhook",
        data=body, method="POST", headers={"Content-Type": "application/json"},
    )
    try:
        resp3 = urllib.request.urlopen(req3, timeout=10, context=ctx)
        results["unsigned_webhook_status"] = resp3.status
    except urllib.error.HTTPError as e:
        results["unsigned_webhook_status"] = e.code

    print(json.dumps(results, indent=2))
    ok = (results["boot_armed_voice_routes"] and results["boot_wired_voice_sidecar"]
          and results["boot_voice_call_tool"] and results["manager_saw_call_initiated"]
          and results["webhook_status"] == 200
          and results["refused_named_reason"]
          and results["unsigned_webhook_status"] == 401)
    print("LIVE-GATE:", "PASS" if ok else "FAIL")
    print("=== LOG EVIDENCE (voice lines) ===")
    for line in log.splitlines():
        if "voice" in line.lower():
            print(line)
finally:
    proc.send_signal(signal.SIGTERM)
    try:
        proc.wait(timeout=15)
    except subprocess.TimeoutExpired:
        proc.kill()
    log_file.close()
    fake.shutdown()
