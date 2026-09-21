"""Teacher-mix sweep runner: two workers + judge over the replay corpus.

Contract: docs/plans/teacher-gate-mixture/master.md (C1, C2, C5, C6, C7).
- One question per turn: intent only. No confidence arithmetic anywhere.
- Judge is called ONLY on worker disagreement (cost control).
- Agreement NEVER raises confidence (campaign: double-confidence FAILED).
- raw/<case_id>.json is crash-safe and resume-aware.
- No verbatim message text is ever written to disk or stdout by this
  script; the corpus join happens in memory only.
"""
from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
import time
import urllib.request
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
from prompt import LANE_NAMES, judge_prompt, worker_prompt  # noqa: E402

OPENCODE_CLI = "/Applications/OpenCode.app/Contents/MacOS/opencode-cli"
CLI_ENV_OVERRIDE = {"XDG_CONFIG_HOME": "/tmp/oc-cfg"}
CASE_TIMEOUT_S = 120
RETRIES = 2
RETRY_BACKOFF_S = 5

# Corpus label -> lane spelling (frozen; leaf-02 contract).
LABEL_MAP = {
    "code": "coding",
    "debug": "debugging",
    "analyze": "analysis",
    "search": "search",
    "chat": "chat",
    "platform": "platform",
    "git": "git",
    "schedule": "scheduling",
    "plan": "planning",
    "review": "review",
    "report": "reporting",
    "recall": "recall",
    "quickplan": "quickplan",
}


def parse_json5_lenient(text: str) -> dict:
    """String-aware JSON5 read: strip // and /* */ comments OUTSIDE strings
    only, then relax JSON5 object syntax (bare keys, trailing commas,
    single quotes) before json.loads(strict=False). (Campaign rule: a naive
    regex stripper eats the // inside URLs in real prompt text.)"""
    out: list[str] = []
    in_str = False
    esc = False
    i = 0
    n = len(text)
    while i < n:
        ch = text[i]
        if in_str:
            out.append(ch)
            if esc:
                esc = False
            elif ch == "\\":
                esc = True
            elif ch == '"':
                in_str = False
            i += 1
            continue
        if ch == '"':
            in_str = True
            out.append(ch)
            i += 1
            continue
        if ch == "/" and i + 1 < n and text[i + 1] == "/":
            while i < n and text[i] != "\n":
                i += 1
            continue
        if ch == "/" and i + 1 < n and text[i + 1] == "*":
            i += 2
            while i + 1 < n and not (text[i] == "*" and text[i + 1] == "/"):
                i += 1
            i += 2
            continue
        out.append(ch)
        i += 1
    cleaned = "".join(out)
    # JSON5 relaxations outside strings: bare keys, trailing commas.
    # Values in the corpus are all double-quoted strings, so key-quoting
    # only needs to cover line-start bare keys.
    cleaned = re.sub(r"(?m)^(\s*)([A-Za-z_][A-Za-z0-9_]*)(\s*):",
                     r'\1"\2"\3:', cleaned)
    cleaned = re.sub(r",(\s*[}\]])", r"\1", cleaned)
    return json.loads(cleaned, strict=False)


def load_cases(replay_path: Path) -> tuple[list[dict], int]:
    """Return (cases, ood_skipped). Each case: {case_id, message, lane}."""
    data = parse_json5_lenient(replay_path.read_text())
    cases: list[dict] = []
    ood = 0
    for idx, entry in enumerate(data.get("cases") or []):
        label = (entry.get("expected_intent") or "").strip()
        if label == "OOD" or entry.get("ood"):
            ood += 1
            continue
        lane = LABEL_MAP.get(label)
        if lane is None:
            raise SystemExit(f"unmapped corpus label {label!r} at case index {idx}")
        cases.append({
            "case_id": entry.get("id") or entry.get("source") or f"case:{idx}",
            "message": entry["input"],
            "lane": lane,
        })
    return cases, ood


def extract_verdict(stdout: str) -> dict | None:
    """First balanced JSON object in stdout; validate shape + lane."""
    start = stdout.find("{")
    if start < 0:
        return None
    depth = 0
    in_str = False
    esc = False
    for i in range(start, len(stdout)):
        ch = stdout[i]
        if in_str:
            if esc:
                esc = False
            elif ch == "\\":
                esc = True
            elif ch == '"':
                in_str = False
            continue
        if ch == '"':
            in_str = True
        elif ch == "{":
            depth += 1
        elif ch == "}":
            depth -= 1
            if depth == 0:
                blob = stdout[start:i + 1]
                try:
                    obj = json.loads(blob)
                except json.JSONDecodeError:
                    return None
                if not isinstance(obj, dict):
                    return None
                intent = obj.get("intent")
                conf = obj.get("confidence", 0.0)
                if intent not in LANE_NAMES or not isinstance(conf, (int, float)):
                    return None
                return {
                    "intent": intent,
                    "confidence": float(conf),
                    "reason": str(obj.get("reason", ""))[:200],
                }
    return None


def call_cli(model: str, prompt_text: str) -> dict | None:
    """Call the opencode CLI for one prompt; return a verdict or None."""
    env = dict(os.environ)
    env.update(CLI_ENV_OVERRIDE)
    for attempt in range(RETRIES + 1):
        try:
            proc = subprocess.run(
                [OPENCODE_CLI, "run", prompt_text, "--model", model],
                capture_output=True, text=True, timeout=CASE_TIMEOUT_S,
                env=env, cwd="/tmp",
            )
            verdict = extract_verdict(proc.stdout or "")
            if verdict is not None:
                return verdict
            err = (proc.stderr or proc.stdout or "")[-200:]
        except subprocess.TimeoutExpired:
            err = f"timeout after {CASE_TIMEOUT_S}s"
        if attempt < RETRIES:
            time.sleep(RETRY_BACKOFF_S)
    else:
        err = "exhausted retries"
    print(f"  [cli-error {model}] {err}", file=sys.stderr)
    return None


def call_http_zai(prompt_text: str) -> dict | None:
    """HTTP fallback for worker-b: api.z.ai coding endpoint, env key."""
    key = os.environ.get("ZAI_API_KEY")
    if not key:
        print("  [http-error] ZAI_API_KEY not set in environment", file=sys.stderr)
        return None
    body = json.dumps({
        "model": "glm-5.3-flash",
        "messages": [{"role": "user", "content": prompt_text}],
        "stream": False,
        "temperature": 0.0,
    }).encode()
    headers = {"Authorization": f"Bearer {key}", "Content-Type": "application/json"}
    for attempt in range(RETRIES + 1):
        try:
            req = urllib.request.Request(
                "https://api.z.ai/api/coding/paas/v4/chat/completions",
                data=body, headers=headers)
            with urllib.request.urlopen(req, timeout=CASE_TIMEOUT_S) as resp:
                data = json.load(resp)
            content = data["choices"][0]["message"]["content"] or ""
            verdict = extract_verdict(content)
            if verdict is not None:
                return verdict
            err = f"unparseable content: {content[:120]}"
        except Exception as exc:  # noqa: BLE001 - report and retry
            err = f"{type(exc).__name__}: {exc}"[:200]
        if attempt < RETRIES:
            time.sleep(RETRY_BACKOFF_S)
    else:
        err = "exhausted retries"
    print(f"  [http-error glm-5.3-flash] {err}", file=sys.stderr)
    return None


def read_route_table(probe_path: Path) -> dict:
    """Parse PROBE.md's route table: route -> (model_id, transport, verdict)."""
    routes: dict[str, tuple[str, str, str]] = {}
    for line in probe_path.read_text().splitlines():
        m = re.match(r"^\|\s*(worker-a|worker-b|judge)\s*\|\s*([^|]+?)\s*\|\s*([^|]+?)\s*\|\s*(OK|FALLBACK|UNAVAILABLE)\s*\|", line)
        if m:
            routes[m.group(1)] = (m.group(2), m.group(3).strip(), m.group(4))
    missing = [r for r in ("worker-a", "worker-b", "judge") if r not in routes]
    if missing:
        raise SystemExit(f"PROBE.md route table incomplete: missing {missing}")
    unavail = [r for r, (_, _, v) in routes.items() if v == "UNAVAILABLE"]
    if unavail:
        raise SystemExit(f"routes UNAVAILABLE per PROBE.md: {unavail}; escalate to user")
    return routes


def main() -> None:
    ap = argparse.ArgumentParser(description="teacher-mix sweep runner")
    ap.add_argument("--replay", required=True, type=Path)
    ap.add_argument("--outdir", required=True, type=Path)
    ap.add_argument("--limit", type=int, default=None)
    ap.add_argument("--only", type=str, default=None, help="comma-separated case ids")
    args = ap.parse_args()

    routes = read_route_table(Path(args.outdir) / "PROBE.md")
    cases, ood = load_cases(args.replay)
    if args.only:
        wanted = {c.strip() for c in args.only.split(",")}
        cases = [c for c in cases if c["case_id"] in wanted]
    if args.limit:
        cases = cases[: args.limit]
    print(f"loaded: {len(cases)} cases ({ood} ood skipped)")

    raw_dir = args.outdir / "raw"
    raw_dir.mkdir(parents=True, exist_ok=True)

    for case in cases:
        raw_path = raw_dir / f"{case['case_id'].replace(':', '__').replace('/', '_')}.json"
        if raw_path.exists():
            existing = json.loads(raw_path.read_text())
            if existing.get("final", {}).get("source") != "error":
                print(f"{case['case_id']} skip (already complete)")
                continue

        pa = worker_prompt(case["message"])
        a = call_cli(routes["worker-a"][0], pa)
        if a is None and routes["worker-a"][2] == "FALLBACK":
            a = call_http_zai(pa)
        b = call_http_zai(pa) if routes["worker-b"][2] == "FALLBACK" \
            else call_cli(routes["worker-b"][0], pa)

        if a is None and b is None:
            final = {"intent": "", "confidence": 0.0, "source": "error"}
            judge = None
        elif a is not None and b is not None and a["intent"] == b["intent"]:
            final = {"intent": a["intent"], "confidence": a["confidence"], "source": "a"}
            judge = None  # agreement: judge not called (cost control)
        else:
            va = a if a is not None else {"intent": "", "confidence": 0.0, "reason": "worker-a error"}
            vb = b if b is not None else {"intent": "", "confidence": 0.0, "reason": "worker-b error"}
            judge = call_cli(routes["judge"][0], judge_prompt(case["message"], va, vb))
            if judge is not None:
                final = {"intent": judge["intent"], "confidence": judge["confidence"], "source": "judge"}
            else:
                candidates = [v for v in (a, b) if v is not None]
                src = max(candidates, key=lambda v: v["confidence"])
                final = {"intent": src["intent"], "confidence": src["confidence"], "source": "fallback"}

        record = {"case_id": case["case_id"], "a": a, "b": b, "judge": judge, "final": final}
        raw_path.write_text(json.dumps(record, ensure_ascii=True, indent=1))
        fmt = lambda v: f"{v['intent']}({v['confidence']:.2f})" if v else "err"  # noqa: E731
        print(f"{case['case_id']} a={fmt(a)} b={fmt(b)} judge={fmt(judge)} "
              f"final={final['intent'] or 'error'}({final['source']})")


if __name__ == "__main__":
    main()
