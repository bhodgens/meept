#!/usr/bin/env python3
"""Prompt-router sidecar for meept.

Serves LiquidAI/LFM2.5-Encoder-350M-Prompt-Router behind an OpenAI-shaped
HTTP API so the meept daemon can use it as a classifier provider through
its normal OpenAI-compatible client (provider api: "openai").

Endpoints (OpenAI-compatible subset):
  GET  /health                 -> {"ok": true}          (runtime health check)
  GET  /v1/models              -> {"data": [{"id": "prompt-router"}]}
  POST /v1/chat/completions    -> routes the last user message against the
                                  configured lanes; returns a chat-shaped
                                  response whose content is strict JSON:
                                  {"intent": lane, "confidence": p,
                                   "scores": {lane: p, ...}}

Lanes come from the routing table -- the single source of truth -- resolved
once at startup with this precedence:

  1. ROUTER_LANES_FILE (alias ROUTER_LANES_JSON): path to the JSON artifact
     written by `meept lanes --json`, shape
     {"lanes": [{"intent": "code", "agent": "coder"}, ...],
      "source": "frontmatter"}.
     Canonical path: $MEEPT_HOME/prompt_router_lanes.json, i.e.
     /Users/caimlas/.meept/prompt_router_lanes.json by default.
  2. ROUTER_LANES: legacy comma-separated override (order = tie break).
  3. the built-in default lane list (the original 9 lanes, original order).

A configured-but-missing/malformed artifact is not fatal: resolution falls
through to the next source. The winning source is logged once to stderr;
stdout stays reserved for the JSON API response bodies.

The model scores the prompt against every lane in one encoder pass -- no
generation, no parsing of free-form model output.

Stdlib HTTP server only (no flask dependency) -- meept's runtime_manager
spawns this like a llama-server (spawn_command / health_check /
restart_policy) and its lifecycle is identical.
"""

import json
import os
import sys

MODEL_ID = os.environ.get("ROUTER_MODEL", "LiquidAI/LFM2.5-Encoder-350M-Prompt-Router")
PORT = int(os.environ.get("ROUTER_PORT", "8082"))

# Built-in fallback: the original 9 lanes in their original order. Used only
# when neither the JSON artifact nor ROUTER_LANES provides lanes.
DEFAULT_LANES = [
    "code", "debug", "review", "plan", "report",
    "recall", "analyze", "search", "chat",
]

# Canonical location of the routing-table artifact (`meept lanes --json`).
# This is the default value a spawn_command should set ROUTER_LANES_FILE to.
DEFAULT_LANES_FILE = os.path.join(
    os.environ.get("MEEPT_HOME") or os.path.join(os.path.expanduser("~"), ".meept"),
    "prompt_router_lanes.json",
)


def _split_env_lanes(raw):
    """Legacy ROUTER_LANES parsing: comma-separated, blanks dropped."""
    if not raw:
        return []
    return [s.strip() for s in raw.split(",") if s.strip()]


def _dedupe(names):
    """De-duplicate lane intents preserving first-seen order."""
    seen = set()
    lanes = []
    for name in names:
        if name and name not in seen:
            seen.add(name)
            lanes.append(name)
    return lanes


def _lanes_from_file(path):
    """Lane intents from a routing-table artifact, or None if unusable.

    Accepts the frozen shape {"lanes": [{"intent": ..., "agent": ...}, ...]}
    as well as plain string entries. Returns None for a missing file, invalid
    JSON, an unexpected shape, or an empty/degenerate lane list so the caller
    can fall through to the next source.
    """
    try:
        with open(path, "r", encoding="utf-8") as fh:
            data = json.load(fh)
    except (OSError, ValueError):
        return None
    if not isinstance(data, dict):
        return None
    entries = data.get("lanes")
    if not isinstance(entries, list):
        return None
    names = []
    for entry in entries:
        if isinstance(entry, dict):
            intent = entry.get("intent")
        elif isinstance(entry, str):
            intent = entry
        else:
            intent = None
        if isinstance(intent, str):
            names.append(intent.strip())
    lanes = _dedupe(names)
    return lanes or None


def resolve_lanes(env=None):
    """Resolve (lanes, source): file, then env, then built-in default.

    `env` defaults to os.environ; pass a mapping in tests. Never raises and
    never returns an empty lane list (an empty list would break scoring).
    """
    env = os.environ if env is None else env
    path = env.get("ROUTER_LANES_FILE") or env.get("ROUTER_LANES_JSON") or ""
    if path:
        lanes = _lanes_from_file(path)
        if lanes:
            return lanes, "json"
    lanes = _dedupe(_split_env_lanes(env.get("ROUTER_LANES")))
    if lanes:
        return lanes, "env"
    return list(DEFAULT_LANES), "default"


LANES_PATH = os.environ.get("ROUTER_LANES_FILE") or os.environ.get("ROUTER_LANES_JSON")
LANES, LANES_SOURCE = resolve_lanes()

tok = None
model = None


def log_lanes_source(stream=None):
    """Emit the one-line lane-source diagnostic to stderr (stdout is API-only)."""
    out = sys.stderr if stream is None else stream
    print(f"prompt-router lanes source={LANES_SOURCE} count={len(LANES)}",
          file=out, flush=True)
    if LANES_PATH and LANES_SOURCE != "json":
        print(f"prompt-router lanes file unusable (source={LANES_SOURCE}): "
              f"{LANES_PATH}", file=out, flush=True)


def route_prompt(prompt: str) -> dict:
    """Score the prompt against every lane in one encoder pass."""
    ranked = model.route(prompt, LANES, tokenizer=tok)
    scores = {item["route"]: float(item["score"]) for item in ranked}
    # Keep only configured lanes, renormalize.
    filtered = {lane: float(scores.get(lane, 0.0)) for lane in LANES}
    total = sum(filtered.values()) or 1.0
    return {k: v / total for k, v in filtered.items()}


def load_model():
    global tok, model
    from transformers import AutoModel, AutoTokenizer

    tok = AutoTokenizer.from_pretrained(MODEL_ID, trust_remote_code=True)
    model = AutoModel.from_pretrained(MODEL_ID, trust_remote_code=True).eval()


def main():
    from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

    log_lanes_source()

    class Handler(BaseHTTPRequestHandler):
        def _json(self, code, payload):
            data = json.dumps(payload).encode()
            self.send_response(code)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)

        def do_GET(self):  # noqa: N802
            if self.path == "/health":
                self._json(200, {"ok": True})
            elif self.path == "/v1/models":
                self._json(200, {"data": [{"id": "prompt-router"}]})
            else:
                self._json(404, {"error": {"message": "not found"}})

        def do_POST(self):  # noqa: N802
            if self.path != "/v1/chat/completions":
                self._json(404, {"error": {"message": "not found"}})
                return
            length = int(self.headers.get("Content-Length", 0))
            body = json.loads(self.rfile.read(length) or b"{}")
            prompt = ""
            for m in reversed(body.get("messages", [])):
                if m.get("role") == "user":
                    prompt = m.get("content", "")
                    break
            if not prompt:
                self._json(400, {"error": {"message": "no user message"}})
                return
            try:
                scores = route_prompt(prompt)
            except Exception as exc:  # noqa: BLE001
                self._json(500, {"error": {"message": f"route failed: {exc}"}})
                return
            best = max(LANES, key=lambda lane: scores.get(lane, 0.0))
            # Confidence calibration: a softmax over N lanes tops out well
            # below 1.0 (best-lane prob ~0.54-0.66 for decisive routes vs
            # uniform 1/N ≈ 0.11), while the dispatcher's intent thresholds
            # (0.5-0.85) were tuned on generative models' self-assessed
            # confidence (0.85-0.95). Odds-form normalization against the
            # uniform baseline maps decisive routes into the range those
            # thresholds expect: conf' = p / (p + 1/N). For p=0.66 → 0.86;
            # borderline p=0.54 → 0.83; uniform 0.11 → 0.5.
            n = len(LANES)
            uniform = 1.0 / n
            p = scores.get(best, 0.0)
            conf = round(p / (p + uniform), 4)
            content = json.dumps({
                "intent": best,
                "confidence": conf,
                "scores": {k: round(v, 4) for k, v in scores.items()},
            })
            self._json(200, {
                "id": "prompt-router",
                "object": "chat.completion",
                "model": "prompt-router",
                "choices": [{
                    "index": 0,
                    "finish_reason": "stop",
                    "message": {"role": "assistant", "content": content},
                }],
                "usage": {"prompt_tokens": 0, "completion_tokens": 0,
                          "total_tokens": 0},
            })

        def log_message(self, fmt, *args):  # silence per-request stderr
            pass

    server = ThreadingHTTPServer(("127.0.0.1", PORT), Handler)
    print(f"prompt-router listening on 127.0.0.1:{PORT} lanes={LANES}",
          file=sys.stderr, flush=True)
    server.serve_forever()


if __name__ == "__main__":
    load_model()
    main()
