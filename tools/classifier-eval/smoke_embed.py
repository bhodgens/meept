#!/usr/bin/env python3
"""Smoke the running embed server: one embedding, check dim + unit norm."""
import json
import urllib.request

req = urllib.request.Request(
    "http://127.0.0.1:8090/v1/embeddings",
    data=json.dumps({"input": "fix the failing test", "model": "qwen3-embedding"}).encode(),
    headers={"Content-Type": "application/json"}, method="POST")
with urllib.request.urlopen(req, timeout=30) as resp:
    d = json.loads(resp.read())
v = d["data"][0]["embedding"]
print("dim", len(v), "norm", round(sum(x * x for x in v) ** 0.5, 6))
