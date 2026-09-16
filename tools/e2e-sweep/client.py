#!/usr/bin/env python3
"""Shared client + config for the meept e2e sweeps.

Resolve the daemon endpoint and dev key the same way the CLI does:
  --base-url   override (default https://127.0.0.1:18095)
  --home       MEEPT_HOME of the target rig (default ~/.meept)
The dev key lives at <home>/dev_key.
"""
import argparse
import json
import os
import ssl
import urllib.request


def parse_args(argv=None):
    p = argparse.ArgumentParser(description="meept e2e sweep")
    p.add_argument("--base-url", default=os.environ.get("MEEPT_SWEEP_BASE_URL", "https://127.0.0.1:18095"))
    p.add_argument("--home", default=os.environ.get("MEEPT_SWEEP_HOME", os.path.expanduser("~/.meept")))
    p.add_argument("--timeout", type=int, default=240)
    p.add_argument("--insecure", action="store_true", default=True,
                   help="skip TLS verification (self-signed dev certs)")
    p.add_argument("categories", nargs="*", help="category/agent filter")
    return p.parse_args(argv)


class Client:
    def __init__(self, args):
        self.base = args.base_url.rstrip("/")
        key_path = os.path.join(args.home, "dev_key")
        self.key = open(key_path).read().strip()
        self.timeout = args.timeout
        self.ctx = ssl._create_unverified_context() if args.insecure else None

    def chat(self, message, conversation_id, agent=None):
        payload = {"message": message, "conversation_id": conversation_id}
        if agent:
            payload["agent_id"] = agent
        req = urllib.request.Request(
            self.base + "/api/v1/chat", data=json.dumps(payload).encode(),
            headers={"Content-Type": "application/json",
                     "Authorization": "Bearer " + self.key},
            method="POST")
        with urllib.request.urlopen(req, timeout=self.timeout, context=self.ctx) as r:
            return json.loads(r.read().decode()).get("reply", "")

    def get(self, path):
        req = urllib.request.Request(self.base + path,
                                     headers={"Authorization": "Bearer " + self.key})
        with urllib.request.urlopen(req, timeout=15, context=self.ctx) as r:
            return json.loads(r.read().decode())
