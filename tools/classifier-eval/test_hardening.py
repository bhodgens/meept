#!/usr/bin/env python3
"""Hardening tests for sandbox remapping, build hooks and classifier scoring.

Both scripts carried guard logic that nothing exercised: CI runs
``.githooks/pre-commit`` on a fresh checkout where ``git diff --cached`` is
empty, so the root-artifact classifier never runs there, and the e2e
script's remapper (a python heredoc) had no caller at all. A revert of
either hardening path therefore broke no gate.

This test drives the REAL code, not a copy:

  * the remapper: the python heredoc is EXTRACTED from
    ``scripts/e2e-naive-user-chat.sh`` and executed against synthetic
    configs, so a revert of the endpoint/variable patterns fails here;
  * the hook: ``artifact_row`` / ``root_artifact_state`` /
    ``build_artifact_names`` / ``changed_artifacts`` / ``classify_root_names``
    are EXTRACTED from ``.githooks/pre-commit-build`` and driven on
    synthetic inputs (a clobbered ./gendoc, a new ./gendoc, a stray
    coverage.out, and a go-less PATH).

  * classifier scoring: the real fold runner is driven with fixed embeddings
    and always-route/always-abstain controls; gold OOD labels must not bypass
    prediction or manufacture perfect abstention.

Requires NumPy (already provisioned by the CI self-test job).
No embed server, no private ruler, no daemon.
Exit 0 green, 1 on any failed check.
"""
from __future__ import annotations

import os
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
E2E = REPO / "scripts/e2e-naive-user-chat.sh"
HOOK = REPO / ".githooks/pre-commit-build"
REAL_CONFIG = REPO / "config/models.json5"
BASH = "/bin/bash"

failures: list[str] = []


def check(name: str, cond: bool, detail: str = "") -> None:
    if cond:
        print(f"  ok   {name}")
    else:
        failures.append(name)
        print(f"  FAIL {name} {detail}")


# --------------------------------------------------------------- extraction

def remap_python() -> str:
    """The remapper heredoc body from the e2e script."""
    lines = E2E.read_text().splitlines(keepends=True)
    start = None
    for i, ln in enumerate(lines):
        if 'python3 - "$HOME_DIR/.meept/models.json5" "$HTTP_PORT"' in ln:
            start = i + 1
            break
    if start is None:
        raise AssertionError("remap heredoc not found in " + str(E2E))
    body = []
    for ln in lines[start:]:
        if ln.rstrip("\n") == "PY":
            break
        body.append(ln)
    else:
        raise AssertionError("remap heredoc not terminated")
    return "".join(body)


def extract_py_fn(src: str, name: str) -> str:
    m = re.search(rf"(?ms)^def {re.escape(name)}\(.*?\n(?=^\S)", src)
    if not m:
        raise AssertionError(f"python function {name!r} not found")
    return m.group(0)


def extract_bash_fn(src: str, name: str) -> str:
    m = re.search(rf"(?ms)^{re.escape(name)}\(\) \{{.*?^\}}$", src)
    if not m:
        raise AssertionError(f"bash function {name!r} not found")
    return m.group(0)


# ------------------------------------------------------------------- remap

REMAP = remap_python()
_TMP = Path(tempfile.mkdtemp(prefix="ceval-hardening-"))
REMAP_PY = _TMP / "remap.py"
REMAP_PY.write_text(REMAP)


def run_remap(config_text: str, tag: str):
    cfg = _TMP / f"{tag}.json5"
    cfg.write_text(config_text)
    r = subprocess.run([sys.executable, str(REMAP_PY), str(cfg), "18099"],
                       capture_output=True, text=True)
    return cfg, r


SPAWN_HEAD = '''{
  "providers": {
    "spawner": {
      "options": { "baseURL": "http://127.0.0.1:8081/v1", %s },
      "lifecycle": {
        "spawn_command": %s
      }
    },
    "remote": {
      "options": { "baseURL": "https://api.example.com:8443/v1", "ratio":1.5 }
    }
  }
}
'''


def test_remap_false_rewrites() -> None:
    print("remap: non-port values inside a spawning provider")
    traps = '"timeout_ms":5000, "seed":12345, "blank":"foo/:9000"'
    spawn = ('["llama-server", "--model", "${MODEL_PATH}", "--port", "8081", '
             '"--env", "EXPORT=3000", "--env", "TRANSPORT=4321"]')
    cfg, r = run_remap(SPAWN_HEAD % (traps, spawn), "traps")
    check("traps config exits 0", r.returncode == 0,
          f"-> {r.returncode} {r.stderr}")
    body = cfg.read_text()
    for tok in ('"timeout_ms":5000', '"seed":12345', '"blank":"foo/:9000"',
                '"EXPORT=3000"', '"TRANSPORT=4321"',
                'api.example.com:8443', '"ratio":1.5'):
        check(f"NOT rewritten: {tok}", tok in body, f"-> {tok!r} vanished")
    check("the real endpoint WAS rewritten",
          '"--port", "8081"' not in body, "-> spawn port untouched")


def test_remap_env_allowlist() -> None:
    print("remap: env-form port variable allow-list")
    spawn = ('["llama-server", "--port", "8081", "--env", "EXPORT=3000"]')
    _, r = run_remap(SPAWN_HEAD % ('"x":1', spawn), "envlist")
    body = (_TMP / "envlist.json5").read_text()
    check("EXPORT=3000 not rewritten", '"EXPORT=3000"' in body, "-> rewritten")
    check("real --port 8081 rewritten", '"8081"' not in body, "-> not rewritten")


def test_remap_variable_shapes() -> None:
    print("remap: escaped vs unescaped variable ports (item 4/5)")
    BS = chr(92)
    # the backslash-escaped spelling inside a single JSON5 string:
    #   "spawn_command": "--port \"${X}\""
    esc = '"--port ' + BS + '"${X}' + BS + '""'
    lit = ('{\n  "providers": {\n    "spawner": {\n'
           '      "options": { "baseURL": "http://127.0.0.1:8081/v1" },\n'
           '      "lifecycle": {\n        "spawn_command": %s\n      }\n'
           '    }\n  }\n}\n')
    # literal endpoint + variable port -> allowed, but never silent
    for tag, val in (("var_unescaped_lit", '"--port", "${X}"'),
                     ("var_escaped_lit", esc)):
        cfg, r = run_remap(lit % val, tag)
        body = cfg.read_text()
        check(f"{tag}: literal endpoint -> allowed (exit 0)",
              r.returncode == 0, f"-> {r.returncode} {r.stderr}")
        check(f"{tag}: variable port reported on stderr",
              "WARNING" in r.stderr and "${X}" in r.stderr, f"-> {r.stderr!r}")
        check(f"{tag}: variable port left as written",
              val in body, "-> vanished")
    # NO literal endpoint -> FATAL, and the message names the variable
    nolit = ('{\n  "providers": {\n    "spawner": {\n'
             '      "lifecycle": {\n        "spawn_command": %s\n      }\n'
             '    }\n  }\n}\n')
    for tag, val in (("var_unescaped_nolit", '"--port", "${X}"'),
                     ("var_escaped_nolit", esc)):
        _, r = run_remap(nolit % val, tag)
        check(f"{tag}: no literal endpoint -> FATAL (exit 2)",
              r.returncode == 2, f"-> {r.returncode}")
        check(f"{tag}: FATAL names the variable ${{X}}",
              "${X}" in r.stderr and "not a literal port" in r.stderr,
              f"-> {r.stderr!r}")


def test_remap_unquoted_key_and_scheme() -> None:
    print("remap: unquoted providers key + host-less scheme/addr forms")
    unquoted = ('{\n  providers: {\n    "spawner": {\n'
                '      "options": { "baseURL": "http://127.0.0.1:8081/v1" },\n'
                '      "lifecycle": {\n        "spawn_command": '
                '["llama-server", "--port", "8081"]\n      }\n    }\n  }\n}\n')
    _, r = run_remap(unquoted, "unquoted")
    check("unquoted providers: accepted", r.returncode == 0,
          f"-> {r.returncode} {r.stderr}")
    check("unquoted providers: endpoint remapped",
          "127.0.0.1:8081" not in (_TMP / "unquoted.json5").read_text(),
          "-> not rewritten")
    hostless = ('{\n  "providers": {\n    "spawner": {\n'
                '      "options": { "baseURL": "http://:8091/v1", '
                '"addr": ":8092" },\n'
                '      "lifecycle": {\n        "spawn_command": '
                '["llama-server", "--addr=:8093"]\n      }\n    }\n  }\n}\n')
    _, r = run_remap(hostless, "hostless")
    body = (_TMP / "hostless.json5").read_text()
    check("host-less endpoints recognized and remapped",
          r.returncode == 0 and ":8091" not in body and ":8092" not in body
          and ":8093" not in body, f"-> exit {r.returncode} {r.stderr} {body}")


def test_remap_real_config_unchanged() -> None:
    print("remap: the real config diff is unchanged (only spawn-bearing providers)")
    orig = REAL_CONFIG.read_text().splitlines()
    cfg, r = run_remap(REAL_CONFIG.read_text(), "real")
    check("real config remap exits 0", r.returncode == 0, f"-> {r.returncode}")
    changed = [l for l in _unified(orig, cfg.read_text().splitlines())
               if l.startswith(("+", "-")) and not l.startswith(("+++", "---"))]
    check("real config: exactly 8 changed lines (4 providers x baseURL+spawn)",
          len(changed) == 16, f"-> {len(changed)}")
    check("real config: every changed line is a baseURL/spawn_command line",
          all(("baseURL" in l or "spawn_command" in l) for l in changed),
          f"-> {[l for l in changed if 'baseURL' not in l and 'spawn_command' not in l]}")
    body = cfg.read_text()
    check("real config: dial-only endpoints preserved",
          "localhost:11434" in body and "127.0.0.1:8188" in body, "-> rewritten")


def test_blank_comments_char_length() -> None:
    print("remap: blank_comments preserves CHARACTER length, not byte length")
    ns: dict = {}
    for fn in ("_skip_string", "blank_comments"):
        exec(extract_py_fn(REMAP, fn), ns)  # noqa: S102 - extracted source
    s = "x // café-multibyte-secret\ny  /* block */\n"
    out = ns["blank_comments"](s)
    check("character length preserved", len(out) == len(s),
          f"-> {len(out)} != {len(s)}")
    check("byte length NOT preserved (char semantics)",
          len(out.encode()) != len(s.encode()),
          "-> byte length unchanged, which would mean byte semantics")
    check("comment characters blanked, code kept",
          out.startswith("x ") and "secret" not in out and "block" not in out
          and out.endswith("\ny  \n") is False and out.endswith("\n"),
          f"-> {out!r}")


def _unified(a: list[str], b: list[str]):
    import difflib
    return list(difflib.unified_diff(a, b, lineterm="", n=0))


# -------------------------------------------------------------------- hook

HOOK_SRC = HOOK.read_text()


def hook_driver(body: str) -> subprocess.CompletedProcess:
    script = ["YELLOW=''", "RED=''", "NC=''",
              "BUILD_NAMES=''", "BUILD_NAMES_DONE=0", "BUILD_NAMES_OK=0"]
    for fn in ("artifact_row", "root_artifact_state", "changed_artifacts",
               "build_artifact_names", "classify_root_names"):
        script.append(extract_bash_fn(HOOK_SRC, fn))
    script.append(body)
    path = _TMP / "driver.sh"
    path.write_text("\n".join(script) + "\n")
    return subprocess.run([BASH, str(path)], capture_output=True, text=True,
                          cwd=_TMP)


def test_hook_clobber_and_classification() -> None:
    print("hook: clobber detection + derived-name classification")
    body = """
BUILD_NAMES="$(printf 'gendoc\nselflock\nfieldguard')"; BUILD_NAMES_DONE=1
printf 'abc' > gendoc
BEFORE="$(root_artifact_state)"
printf '' > gendoc
AFTER="$(root_artifact_state)"
echo "P1_BEFORE=[$BEFORE]"
echo "P1_AFTER=[$AFTER]"
echo "P1_CHANGED=[$(changed_artifacts "$BEFORE" "$AFTER")]"
classify_root_names <<EOF2
gendoc
coverage.out
EOF2
echo "P2_BLOCKED=[$BLOCKED]"
echo "P2_OTHER=[$OTHER]"
"""
    r = hook_driver(body)
    out = r.stdout
    def field(k):
        m = re.search(rf"{k}=\[(.*?)\]", out, re.S)
        return m.group(1).strip() if m else None
    check("clobber moves the fingerprint (derived name fingerprinted)",
          bool(field("P1_BEFORE")) and field("P1_BEFORE") != field("P1_AFTER"),
          f"-> before={field('P1_BEFORE')!r} after={field('P1_AFTER')!r}")
    check("clobber is named by changed_artifacts -> BLOCKS",
          field("P1_CHANGED") == "gendoc", f"-> {field('P1_CHANGED')!r}")
    check("a ./gendoc (new entry or clobber) classifies BLOCKED",
          field("P2_BLOCKED") == "gendoc", f"-> {field('P2_BLOCKED')!r}")
    check("a stray coverage.out only warns (OTHER, not blocking)",
          field("P2_OTHER") == "coverage.out", f"-> {field('P2_OTHER')!r}")
    check("driver ran clean", r.returncode == 0, f"-> {r.stderr}")


def test_hook_go_unavailable_warns() -> None:
    print("hook: the derivation warns when 'go' is unavailable (no fail-open)")
    body = ("build_artifact_names\n"
            "echo \"DERIVED=[$BUILD_NAMES] OK=[$BUILD_NAMES_OK]\"\n")
    script = ["YELLOW=''", "RED=''", "NC=''",
              "BUILD_NAMES=''", "BUILD_NAMES_DONE=0", "BUILD_NAMES_OK=0",
              extract_bash_fn(HOOK_SRC, "build_artifact_names"), body]
    path = _TMP / "driver_nogo.sh"
    path.write_text("\n".join(script) + "\n")
    env = dict(os.environ)
    env["PATH"] = "/nonexistent"
    r = subprocess.run([BASH, str(path)], capture_output=True, text=True,
                       cwd=_TMP, env=env)
    check("go-less derivation prints the UNCHECKED warning",
          "UNCHECKED" in r.stdout, f"-> {r.stdout!r} {r.stderr!r}")
    check("go-less derivation yields no names",
          "DERIVED=[] OK=[0]" in r.stdout, f"-> {r.stdout!r}")


def test_hook_derives_real_names() -> None:
    print("hook: build_artifact_names derives real package-main basenames")
    body = ("cd " + str(REPO) + "\nbuild_artifact_names\n"
            "echo \"NAMES=[$BUILD_NAMES]\"\n")
    r = hook_driver(body)
    m = re.search(r"NAMES=\[(.*?)\]", r.stdout, re.S)
    names = (m.group(1).split() if m else [])
    check("derivation returns names in the repo", len(names) > 3,
          f"-> {names}")
    check("gendoc is a derived name", "gendoc" in names, f"-> {names}")


def test_reordered_embeddings_keep_key_association() -> None:
    """Response indices, not response-array order, identify each input."""
    import io
    import json
    import numpy as np
    from unittest.mock import patch
    import eval_harness as harness

    texts = ["first input", "second input", "zero input"]
    keys = [harness.case_key(text) for text in texts]
    response = {"data": [
        {"index": 2, "embedding": [0.0, 0.0]},
        {"index": 0, "embedding": [3.0, 0.0]},
        {"index": 1, "embedding": [0.0, 4.0]},
    ]}
    with tempfile.TemporaryDirectory() as cache:
        emb = harness.Embedder("http://embed.invalid/v1", "test", cache_dir=Path(cache))
        with patch.object(harness.urllib.request, "urlopen",
                          return_value=io.BytesIO(json.dumps(response).encode())):
            emb.embed_keys(texts, keys)
        np.testing.assert_array_equal(
            emb.vectors(keys), np.array([[1, 0], [0, 1], [0, 0]], dtype=np.float32),
            err_msg="reordered embeddings must stay associated with their input keys")
        check("reordered embeddings: degenerate row counted", emb.degenerate_rows == 1)
        check("reordered embeddings: latency accounting retained",
              emb.embed_calls == 1 and len(emb.latency_ms) == 3)


def test_ood_predictions_are_measured() -> None:
    """Gold OOD labels must never manufacture perfect abstention."""
    import numpy as np
    from unittest.mock import patch
    import eval_harness as harness

    print("classifier: OOD queries use the head, never their gold label")
    cases = [harness.Case(f"synthetic-{i}", "code", "", False,
                          "test", "", f"in-{i}") for i in range(5)]
    cases.append(harness.Case("synthetic-unsupported", "OOD", "", True,
                              "test", "", "ood"))
    folds = {harness.case_key(c.text): i % 5 for i, c in enumerate(cases)}

    class FixedEmbeddings(harness.Embedder):
        def __init__(self):
            # Do not create the normal disk cache or contact an embed server.
            self.latency_ms = []

        def embed_keys(self, texts, keys):
            pass

        def vectors(self, keys):
            return np.tile(np.array([[1.0, 0.0]]), (len(keys), 1))

    for verdict in ("code", None):
        queried = []
        training_labels = []

        class FixedHead:
            def fit(self, indices, intents):
                training_labels.extend(intents)

            def decide(self, query, intents):
                queried.append(query)
                return verdict, 1.0

        # Only substitute external embeddings and the head. Exercise the real
        # fold runner, training exclusion, confusion recording and score pool.
        with patch.object(harness, "build_head", return_value=FixedHead()):
            metrics = harness.run_permutation(
                {"name": "ood-control", "head": {"type": "test"}},
                cases, folds, FixedEmbeddings())
        label = "always-route" if verdict else "always-abstain"
        check(f"{label}: every held-out query reaches head", len(queried) == 6,
              f"-> {len(queried)}")
        check(f"{label}: OOD excluded from training", "OOD" not in training_labels)
        check(f"{label}: OOD denominator retained", metrics["OOD_total"] == 1)
        check(f"{label}: measured OOD abstention",
              metrics["OOD_R"] == (0.0 if verdict else 1.0))
        check(f"{label}: OOD wrong-route penalty",
              metrics["wrong"] == (1 if verdict else 0))
        check(f"{label}: in-domain denominator unchanged", metrics["total"] == 5)
        check(f"{label}: chain credit stays in-domain",
              metrics["E2E"] == (1.0 if verdict else harness.CHAIN_BASELINE))
        if verdict:
            check("always-route: OOD appears in confusion evidence",
                  any(c.case_id == "ood" for c, _, _ in metrics["confusion"]))
            check("always-route: OOD lowers composite score", metrics["SCORE"] == 0.0)


def main() -> int:
    print("classifier-eval hardening test (remap + hook classification)")
    test_remap_false_rewrites()
    test_remap_env_allowlist()
    test_remap_variable_shapes()
    test_remap_unquoted_key_and_scheme()
    test_remap_real_config_unchanged()
    test_blank_comments_char_length()
    test_hook_clobber_and_classification()
    test_hook_go_unavailable_warns()
    test_hook_derives_real_names()
    test_reordered_embeddings_keep_key_association()
    test_ood_predictions_are_measured()
    shutil.rmtree(_TMP, ignore_errors=True)
    if failures:
        print(f"hardening test FAILED: {len(failures)} check(s): {failures}",
              file=sys.stderr)
        return 1
    print("hardening test PASSED")
    return 0


if __name__ == "__main__":
    sys.exit(main())