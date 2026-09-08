#!/usr/bin/env python3
"""OpenAI-compatible /v1/embeddings server for Qwen3-Embedding on MLX.

Serves the 4-bit DWQ weights already on disk (default
/Volumes/LLMs/Qwen3-Embedding-0.6B-4bit-DWQ) behind the standard
POST /v1/embeddings shape the meept classifier prefilter speaks:

    {"input": "text", "model": "..."} -> {"data": [{"embedding": [...]}]}

Pooling: last-token (EOS) hidden state, L2-normalized — the reference
Qwen3-Embedding recipe. No installs required beyond the `mlx` package
already present in the venv that runs mlx_lm (:8082 daemon model server).

Run:
    ~/.venv/bin/python scripts/embed_server.py \
        --model /Volumes/LLMs/Qwen3-Embedding-0.6B-4bit-DWQ --port 8090

Smoke:
    curl -s localhost:8090/v1/embeddings -H 'Content-Type: application/json' \
        -d '{"input":"fix the failing test","model":"qwen3-emb"}'
"""
import argparse
import json
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import numpy as np

MODEL_PATH = "/Volumes/LLMs/Qwen3-Embedding-0.6B-4bit-DWQ"


class Embedder:
    """Loads the MLX model once; embeds strings on demand."""

    def __init__(self, model_path: str):
        import mlx.core as mx
        from mlx_lm.utils import load  # same loader mlx_lm serve uses

        self.mx = mx
        t0 = time.time()
        loaded = load(model_path)
        # mlx_lm.load returns (model, tokenizer) on older versions and
        # (model, tokenizer, config) on newer ones — unpack tolerantly.
        self.model, self.tokenizer = loaded[0], loaded[1]
        # Qwen3 embeddings use the EOS token as the pooling anchor.
        self.eos_id = self.tokenizer.eos_token_id
        print(f"[embed_server] model loaded in {time.time() - t0:.1f}s: {model_path}")

    def embed(self, text: str) -> list[float]:
        mx = self.mx
        tokens = self.tokenizer.encode(text)
        if not tokens:
            tokens = [self.eos_id]
        if tokens[-1] != self.eos_id:
            tokens = tokens + [self.eos_id]
        input_ids = mx.array([tokens])
        # mlx_lm causal-LM forward returns the logits tensor directly
        # (NOT an HF output object). The hidden state we need is the
        # second-to-last layer output — recompute it via the model's
        # internal layers: embed tokens, run the transformer layers, take
        # the last position. Use model.model (the backbone) when present;
        # fall back to model(...) call for embedder-style wrappers.
        backbone = getattr(self.model, "model", self.model)
        # Quantized MLX models: call the backbone's layers manually via its
        # embed + layer stack. mlx Qwen3 backbone signature is
        # __call__(inputs, cache=None) returning hidden states — but on
        # 4-bit DWQ checkpoints np.array() on the raw buffer fails
        # (PEP 3118 buffer of uint16 vs uint8 view). Convert via
        # mx.array -> .astype(mx.float32) first, which always works.
        hidden = backbone(input_ids)
        if hasattr(hidden, "last_hidden_state"):
            hidden = hidden.last_hidden_state
        vec = np.array(hidden[0, -1].astype(mx.float32))  # last-token (EOS) pooling
        norm = np.linalg.norm(vec)
        if norm > 0:
            vec = vec / norm
        return [float(x) for x in vec]


class Handler(BaseHTTPRequestHandler):
    embedder: Embedder = None  # injected in main()
    lock = threading.Lock()    # serialize GPU forward passes

    def log_message(self, fmt, *args):  # quiet default access log
        pass

    def _json(self, code: int, payload: dict) -> None:
        body = json.dumps(payload).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path == "/health":
            self._json(200, {"status": "ok"})
        else:
            self._json(404, {"error": "not found"})

    def do_POST(self):
        if not self.path.rstrip("/").endswith("/embeddings"):
            self._json(404, {"error": "not found"})
            return
        try:
            length = int(self.headers.get("Content-Length", 0))
            req = json.loads(self.rfile.read(length) or b"{}")
        except json.JSONDecodeError as exc:
            self._json(400, {"error": f"bad json: {exc}"})
            return

        texts = req.get("input")
        if isinstance(texts, str):
            texts = [texts]
        if not isinstance(texts, list) or not texts:
            self._json(400, {"error": "input must be a string or non-empty array"})
            return

        try:
            with Handler.lock:
                embeddings = [self.embedder.embed(t) for t in texts]
        except Exception as exc:  # surface model errors as 500, keep serving
            self._json(500, {"error": f"embed failed: {exc}"})
            return

        self._json(200, {
            "object": "list",
            "data": [
                {"object": "embedding", "index": i, "embedding": vec}
                for i, vec in enumerate(embeddings)
            ],
            "model": req.get("model", "qwen3-embedding"),
            "usage": {"prompt_tokens": 0, "total_tokens": 0},
        })


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--model", default=MODEL_PATH)
    ap.add_argument("--host", default="127.0.0.1")
    ap.add_argument("--port", type=int, default=8090)
    args = ap.parse_args()

    Handler.embedder = Embedder(args.model)
    server = ThreadingHTTPServer((args.host, args.port), Handler)
    print(f"[embed_server] listening on http://{args.host}:{args.port}/v1/embeddings")
    server.serve_forever()


if __name__ == "__main__":
    main()
