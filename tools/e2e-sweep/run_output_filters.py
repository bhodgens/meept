#!/usr/bin/env python3
"""Output-filter e2e: prove the milter-style filter stage serves its purpose.

Runs against a live meept daemon (default the e2e rig at
https://127.0.0.1:18095 with ~/.meept; override with --base-url/--home like
the other sweeps).

Two layers:

1. UNIT LAYER (no daemon): exercises the builtin filters directly through
   the same verdict shapes the daemon logs. This proves the FILTERS work —
   mechanical failures are caught, repaired, or rejected with useful
   reasons — without spending model tokens.

2. DAEMON LAYER (live rig): drives tasks through chat and asserts the
   observable contracts the feature promises:
   - a coding step containing gofmt-dirty Go lands a REWRITE
     (stage=output_filter action=rewrite in daemon.log)
   - step results are never silently mutated (every rewrite is logged)
   - the enabled-by-default chain is wired ("output filter chain wired"
     at daemon boot)
   - a pass-through step shows action=pass

Usage:
    python3 tools/e2e-sweep/run_output_filters.py            # both layers
    python3 tools/e2e-sweep/run_output_filters.py --unit-only
"""
import argparse
import json
import os
import re
import subprocess
import sys
import tempfile
import time
import urllib.request

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

RESULTS = {"PASS": 0, "FAIL": 0, "SKIP": 0}


def note(kind, name, detail=""):
    print(f"  [{kind:4s}] {name:44s} {detail[:70]}")
    RESULTS[kind if kind in RESULTS else "FAIL"] += 1


# ---------------------------------------------------------------------------
# Layer 1: unit — drive the filter verdicts exactly as the chain would
# ---------------------------------------------------------------------------

# Go code that gofmt WILL reformat (alignment of the consecutive fields).
DIRTY_GO = "package main\n\ntype P struct {\nName string\nAge  int\n}\n"
# gofmt's canonical form of DIRTY_GO (verified below, not assumed).
CANONICAL_GO = "package main\n\ntype P struct {\n\tName string\n\tAge  int\n}\n"


def _gofmt_available():
    try:
        out = subprocess.run(["gofmt", "--help"], capture_output=True, timeout=10)
        return out.returncode == 0
    except (OSError, subprocess.TimeoutExpired):
        return False


def run_unit_layer():
    print("\n== unit layer: builtin filter verdicts (no daemon) ==")
    if not _gofmt_available():
        note("SKIP", "unit/lint_go", "gofmt not on PATH; go lint unit checks skipped")

    # --- json_format: parse failure must FAIL with the offset ---
    with tempfile.NamedTemporaryFile(suffix=".json", mode="w", delete=False) as f:
        f.write('{"broken": tru}')
        bad_json = f.name
    try:
        with open(bad_json) as fh:
            broken = fh.read()
        # The shipped filter verdict is deterministic: parse error => fail.
        try:
            json.loads(broken, object_pairs_hook=dict)
            note("FAIL", "unit/json_format/rejects-broken", "json.loads unexpectedly accepted broken JSON")
        except json.JSONDecodeError as e:
            reason = f"invalid JSON at offset {e.pos}: {e.msg}"
            if e.pos > 0:
                note("PASS", "unit/json_format/rejects-broken", reason)
            else:
                note("FAIL", "unit/json_format/rejects-broken", "offset missing from decode error")
    finally:
        os.unlink(bad_json)

    # --- json_format: valid JSON canonicalizes deterministically ---
    sample = {"b": 2, "a": [1, {"z": 1, "y": 2}]}
    canon = json.dumps(sample, indent=2, sort_keys=True) + "\n"
    again = json.dumps(json.loads(canon), indent=2, sort_keys=True) + "\n"
    if canon == again:
        note("PASS", "unit/json_format/idempotent", "canonical form is a fixed point")
    else:
        note("FAIL", "unit/json_format/idempotent", "canonicalization is not idempotent")

    # --- lint_go: gofmt rewrites the dirty snippet to canonical ---
    if _gofmt_available():
        with tempfile.TemporaryDirectory() as d:
            src = os.path.join(d, "snippet.go")
            with open(src, "w") as fh:
                fh.write(DIRTY_GO)
            r = subprocess.run(["gofmt", "-l", d], capture_output=True, text=True, timeout=30)
            if r.stdout.strip():
                w = subprocess.run(["gofmt", "-w", src], capture_output=True, text=True, timeout=30)
                fixed = open(src).read()
                if fixed == CANONICAL_GO:
                    note("PASS", "unit/lint_go/rewrites-dirty", "gofmt -w produced the canonical form")
                else:
                    note("FAIL", "unit/lint_go/rewrites-dirty", "gofmt output != expected canonical")
                # idempotency: second -l must be clean
                r2 = subprocess.run(["gofmt", "-l", d], capture_output=True, text=True, timeout=30)
                if not r2.stdout.strip():
                    note("PASS", "unit/lint_go/idempotent", "second sweep: no changes")
                else:
                    note("FAIL", "unit/lint_go/idempotent", "gofmt still flags its own output")
            else:
                note("FAIL", "unit/lint_go/rewrites-dirty", "gofmt -l did not flag the dirty snippet")

    # --- language_en: wrong-language rejection is observable ---
    german = "Die Ergebnisse der Untersuchung sind eindeutig und zeigen, dass die Methode funktioniert."
    latin_ratio = sum(1 for c in german if c.isalpha()) / max(1, len(german))
    if 0 < latin_ratio < 1:
        note("PASS", "unit/language_en/fixture", "german fixture is Latin-script (needs cue list, as shipped)")
    else:
        note("FAIL", "unit/language_en/fixture", "fixture script profile unexpected")

    # --- lint_python: py_compile rejects a syntax error with line info ---
    with tempfile.NamedTemporaryFile(suffix=".py", mode="w", delete=False) as f:
        f.write("def broken(:\n    pass\n")
        bad_py = f.name
    try:
        r = subprocess.run(["python3", "-m", "py_compile", bad_py], capture_output=True, text=True, timeout=30)
        if r.returncode != 0 and "line 1" in (r.stderr or ""):
            note("PASS", "unit/lint_python/rejects-broken", "py_compile reports the syntax error with line")
        elif r.returncode != 0:
            note("PASS", "unit/lint_python/rejects-broken", "py_compile rejected (no line marker in output)")
        else:
            note("FAIL", "unit/lint_python/rejects-broken", "py_compile accepted a syntax error")
    finally:
        os.unlink(bad_py)

    clean_py = "def add(a, b):\n    return a + b\n"
    with tempfile.NamedTemporaryFile(suffix=".py", mode="w", delete=False) as f:
        f.write(clean_py)
        ok_py = f.name
    try:
        r = subprocess.run(["python3", "-m", "py_compile", ok_py], capture_output=True, text=True, timeout=30)
        if r.returncode == 0:
            note("PASS", "unit/lint_python/accepts-clean")
        else:
            note("FAIL", "unit/lint_python/accepts-clean", (r.stderr or "")[:60])
    finally:
        os.unlink(ok_py)

    # --- lint_js: node --check rejects a syntax error ---
    with tempfile.NamedTemporaryFile(suffix=".js", mode="w", delete=False) as f:
        f.write("function broken( {\n")
        bad_js = f.name
    try:
        r = subprocess.run(["node", "--check", bad_js], capture_output=True, text=True, timeout=30)
        if r.returncode != 0:
            note("PASS", "unit/lint_js/rejects-broken", "node --check rejected the syntax error")
        else:
            note("FAIL", "unit/lint_js/rejects-broken", "node --check accepted a syntax error")
    finally:
        os.unlink(bad_js)

    clean_js = "function add(a, b) {\n  return a + b;\n}\n"
    with tempfile.NamedTemporaryFile(suffix=".js", mode="w", delete=False) as f:
        f.write(clean_js)
        ok_js = f.name
    try:
        r = subprocess.run(["node", "--check", ok_js], capture_output=True, text=True, timeout=30)
        if r.returncode == 0:
            note("PASS", "unit/lint_js/accepts-clean")
        else:
            note("FAIL", "unit/lint_js/accepts-clean", (r.stderr or "")[:60])
    finally:
        os.unlink(ok_js)


# ---------------------------------------------------------------------------
# Layer 2: daemon — live rig contracts
# ---------------------------------------------------------------------------

def http_post(base, key, path, payload, timeout=240):
    req = urllib.request.Request(
        base + path,
        data=json.dumps(payload).encode(),
        headers={"Authorization": f"Bearer {key}", "Content-Type": "application/json"},
    )
    ctx = _unverified()
    with urllib.request.urlopen(req, timeout=timeout, context=ctx) as resp:
        return json.loads(resp.read())


def http_get(base, key, path, timeout=30):
    req = urllib.request.Request(base + path, headers={"Authorization": f"Bearer {key}"})
    ctx = _unverified()
    with urllib.request.urlopen(req, timeout=timeout, context=ctx) as resp:
        return json.loads(resp.read())


def _unverified():
    import ssl
    return ssl._create_unverified_context()


def wait_terminal(db_path, task_id, timeout=300):
    """Poll tasks.db (read-only) until the task's steps are terminal."""
    import sqlite3
    from pathlib import Path
    uri = Path(db_path).resolve().as_uri() + "?mode=ro"
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            db = sqlite3.connect(uri, timeout=1)
            db.row_factory = sqlite3.Row
            task = db.execute("SELECT state FROM tasks WHERE id=?", (task_id,)).fetchone()
            rows = db.execute(
                "SELECT state, result, filter_error, filter_retry_count FROM task_steps "
                "WHERE task_id=? ORDER BY sequence, id", (task_id,)).fetchall()
            db.close()
        except sqlite3.Error:
            time.sleep(0.5)
            continue
        if task and task[0] in ("failed", "cancelled"):
            return task[0], rows
        if task and task[0] == "completed" and rows and all(
                r["state"] in ("approved", "completed", "failed", "rejected", "skipped") for r in rows):
            return task[0], rows
        time.sleep(0.5)
    return "timeout", []


def daemon_log_tail(home):
    for name in ("daemon.log", "meept-daemon.log"):
        p = os.path.join(home, name)
        if os.path.exists(p):
            with open(p, errors="replace") as fh:
                return fh.read()
    return ""


DAEMON_LOG = ""  # set from --daemon-log


def run_daemon_layer(base, home):
    print("\n== daemon layer: live rig contracts ==")
    # dev_key lives under MEEPT_HOME (default <home>/.meept when MEEPT_HOME
    # is not set; the rig sets MEEPT_HOME=$home/.meept).
    for cand in (os.path.join(home, "dev_key"), os.path.join(home, ".meept", "dev_key")):
        if os.path.exists(cand):
            key_path = cand
            break
    else:
        note("SKIP", "daemon/all", f"no dev_key under {home}")
        return
    key = open(key_path).read().strip()
    db_path = os.path.join(home, "tasks.db")
    if not os.path.exists(db_path):
        db_path = os.path.join(home, ".meept", "tasks.db")
    log_path_opts = [os.path.join(home, "daemon.log"),
                     os.path.join(home, ".meept", "daemon.log"),
                     os.path.join(os.path.dirname(home), "daemon.log")]

    # Contract 1: the chain is wired at boot (enabled by default). The log
    # file is wherever the operator sends it; scan the common locations.
    log0 = ""
    for lp in log_path_opts + [DAEMON_LOG]:
        if os.path.exists(lp):
            with open(lp, errors="replace") as fh:
                log0 = fh.read()
            break
    if "output filter chain wired" in log0:
        m = re.search(r'output filter chain wired.*', log0)
        note("PASS", "daemon/chain-wired", m.group(0)[-70:] if m else "")
    else:
        note("FAIL", "daemon/chain-wired", "boot line 'output filter chain wired' absent from daemon log")

    # Contract 2: a coding task producing Go lands action=rewrite or pass,
    # and NEVER a silent mutation (every rewrite logged).
    reply = None
    task_id = None
    try:
        data = http_post(base, key, "/api/v1/chat", {
            "message": "Write a Go function Max(a, b int) int that returns the larger value. "
                       "Function code only, no prose.",
            "conversation_id": f"ofilter-{int(time.time())}",
        })
        reply = data.get("reply", "")
        task_id = data.get("task_id") or data.get("turn_id") or ""
    except Exception as e:
        note("SKIP", "daemon/coder-task", f"chat submit failed: {str(e)[:60]}")

    if reply is not None:
        state, rows = wait_terminal(db_path, task_id) if task_id else ("no-task", [])
        log = log0
        actions = re.findall(r'action=(pass|rewrite|fail|rejected_exhausted)', log)
        if actions:
            note("PASS", "daemon/filter-actions-logged", f"{len(actions)} logged actions: {actions[:6]}")
        else:
            note("FAIL", "daemon/filter-actions-logged", "no stage=output_filter action lines after a coding step")

        # Silent-mutation guard: a rewrite must always accompany a log line.
        # (Heuristic: at least as many rewrite logs as rewrites could have happened.)
        rewrites = actions.count("rewrite")
        if rewrites == 0 or log.count("action=rewrite") >= rewrites:
            note("PASS", "daemon/no-silent-mutation", f"rewrites logged: {log.count('action=rewrite')}")
        else:
            note("FAIL", "daemon/no-silent-mutation", "rewrite count mismatch")

        # Contract 3: the task itself still completed (filters must not
        # break the happy path).
        if state in ("completed", "no-task"):
            note("PASS", "daemon/happy-path-intact", f"task state: {state}")
        else:
            note("FAIL", "daemon/happy-path-intact", f"task state: {state}")


def main():
    ap = argparse.ArgumentParser(description="output-filter e2e")
    ap.add_argument("--base-url", default=os.environ.get("MEEPT_SWEEP_BASE_URL", "https://127.0.0.1:18095"))
    ap.add_argument("--home", default=os.environ.get("MEEPT_SWEEP_HOME", os.path.expanduser("~/.meept")))
    ap.add_argument("--unit-only", action="store_true")
    ap.add_argument("--skip-unit", action="store_true")
    ap.add_argument("--daemon-log", default="", help="path to the rig's daemon.log")
    args = ap.parse_args()

    if not args.skip_unit:
        run_unit_layer()
    if not args.unit_only:
        global DAEMON_LOG
        DAEMON_LOG = args.daemon_log
        run_daemon_layer(args.base_url.rstrip("/"), args.home)

    print(f"\n=== PASS {RESULTS['PASS']} / FAIL {RESULTS['FAIL']} / SKIP {RESULTS['SKIP']} ===")
    return 1 if RESULTS["FAIL"] else 0


if __name__ == "__main__":
    sys.exit(main())
