#!/usr/bin/env python3
"""Unit tests for the prompt-router sidecar lane resolution.

Stdlib unittest only -- no pytest, no HTTP server, no model load. The module
under test is imported by path, which works only because the sidecar guards
every side effect (model load, serve_forever) behind a __main__ check.

Run: python3 scripts/test_prompt_router_sidecar.py
"""

import importlib.util
import io
import json
import os
import re
import tempfile
import unittest

MODULE_PATH = os.path.join(
    os.path.dirname(os.path.abspath(__file__)), "prompt_router_sidecar.py")

# The original 9 lanes: the no-env default, frozen for back-compat.
EXPECTED_DEFAULT_LANES = [
    "code", "debug", "review", "plan", "report",
    "recall", "analyze", "search", "chat",
]


def load_module():
    """Import the sidecar by path without running its server."""
    spec = importlib.util.spec_from_file_location(
        "prompt_router_sidecar_under_test", MODULE_PATH)
    assert spec is not None and spec.loader is not None, MODULE_PATH
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def load_module_with_env(mapping):
    """Re-execute the module with a controlled environment, then restore it.

    Only the ROUTER_* / MEEPT_HOME knobs are cleared (HOME and PATH survive,
    so ~ expansion and imports behave normally).
    """
    saved = dict(os.environ)
    for key in list(os.environ):
        if key.startswith("ROUTER_") or key == "MEEPT_HOME":
            del os.environ[key]
    os.environ.update(mapping)
    try:
        return load_module()
    finally:
        os.environ.clear()
        os.environ.update(saved)


MOD = load_module()


class LaneResolutionTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp(prefix="router-lanes-")
        self.addCleanup(self._rm_tmp)

    def _rm_tmp(self):
        for name in os.listdir(self.tmp):
            os.unlink(os.path.join(self.tmp, name))
        os.rmdir(self.tmp)

    def _write(self, payload, name="lanes.json"):
        path = os.path.join(self.tmp, name)
        with open(path, "w", encoding="utf-8") as fh:
            if isinstance(payload, str):
                fh.write(payload)
            else:
                json.dump(payload, fh)
        return path

    # -- defaults (back-compat) -------------------------------------------

    def test_default_lanes_load_with_no_env(self):
        lanes, source = MOD.resolve_lanes(env={})
        self.assertEqual(lanes, EXPECTED_DEFAULT_LANES)
        self.assertEqual(source, "default")

    def test_module_default_is_frozen_nine_lane_list(self):
        module = load_module_with_env({})
        self.assertEqual(module.LANES, EXPECTED_DEFAULT_LANES)
        self.assertEqual(module.LANES_SOURCE, "default")
        self.assertEqual(len(module.LANES), 9)

    def test_default_lanes_file_path_is_canonical(self):
        self.assertTrue(MOD.DEFAULT_LANES_FILE.endswith(
            os.path.join(".meept", "prompt_router_lanes.json")))
        self.assertEqual(
            MOD.DEFAULT_LANES_FILE,
            "/Users/caimlas/.meept/prompt_router_lanes.json")

    def test_default_file_is_not_consulted_when_env_unset(self):
        # Back-compat: an artifact on disk must not change lanes unless
        # ROUTER_LANES_FILE points at it.
        lanes, source = MOD.resolve_lanes(env={})
        self.assertEqual(source, "default")
        self.assertEqual(lanes, EXPECTED_DEFAULT_LANES)

    # -- ROUTER_LANES ------------------------------------------------------

    def test_router_lanes_overrides_default(self):
        lanes, source = MOD.resolve_lanes(env={"ROUTER_LANES": "quickplan, code"})
        self.assertEqual(lanes, ["quickplan", "code"])
        self.assertEqual(source, "env")

    def test_router_lanes_module_level_and_trims_blanks(self):
        module = load_module_with_env({"ROUTER_LANES": " alpha , beta ,, gamma "})
        self.assertEqual(module.LANES, ["alpha", "beta", "gamma"])
        self.assertEqual(module.LANES_SOURCE, "env")

    def test_empty_router_lanes_falls_back_to_default(self):
        lanes, source = MOD.resolve_lanes(env={"ROUTER_LANES": " , ,"})
        self.assertEqual(lanes, EXPECTED_DEFAULT_LANES)
        self.assertEqual(source, "default")

    # -- ROUTER_LANES_FILE -------------------------------------------------

    def test_lanes_file_overrides_router_lanes(self):
        path = self._write({"lanes": [{"intent": "quickplan",
                                       "agent": "orchestrator"},
                                      {"intent": "research",
                                       "agent": "researcher"}],
                            "source": "frontmatter"})
        lanes, source = MOD.resolve_lanes(
            env={"ROUTER_LANES_FILE": path, "ROUTER_LANES": "code,debug"})
        self.assertEqual(lanes, ["quickplan", "research"])
        self.assertEqual(source, "json")

    def test_lanes_file_module_level(self):
        path = self._write({"lanes": [{"intent": "code", "agent": "coder"}],
                            "source": "static"})
        module = load_module_with_env({"ROUTER_LANES_FILE": path})
        self.assertEqual(module.LANES, ["code"])
        self.assertEqual(module.LANES_SOURCE, "json")

    def test_object_lanes_accepted_and_order_preserved(self):
        path = self._write({"lanes": [
            {"intent": "architect", "agent": "architect"},
            {"intent": "coder", "agent": "coder"},
            {"intent": "skeptic", "agent": "skeptic"},
        ], "source": "frontmatter"})
        lanes, source = MOD.resolve_lanes(env={"ROUTER_LANES_FILE": path})
        self.assertEqual(lanes, ["architect", "coder", "skeptic"])
        self.assertEqual(source, "json")

    def test_object_lanes_are_deduplicated_preserving_order(self):
        path = self._write({"lanes": [
            {"intent": "code", "agent": "coder"},
            "debug",
            {"intent": "code", "agent": "coder-2"},
            "debug",
            {"intent": "research", "agent": "researcher"},
            "   ",
            {"agent": "no-intent-field"},
            17,
        ], "source": "frontmatter"})
        lanes, source = MOD.resolve_lanes(env={"ROUTER_LANES_FILE": path})
        self.assertEqual(lanes, ["code", "debug", "research"])
        self.assertEqual(source, "json")

    # -- fallbacks ---------------------------------------------------------

    def test_missing_file_falls_back_to_router_lanes(self):
        lanes, source = MOD.resolve_lanes(env={
            "ROUTER_LANES_FILE": os.path.join(self.tmp, "nope.json"),
            "ROUTER_LANES": "code,debug",
        })
        self.assertEqual(lanes, ["code", "debug"])
        self.assertEqual(source, "env")

    def test_missing_file_falls_back_to_default(self):
        lanes, source = MOD.resolve_lanes(env={
            "ROUTER_LANES_FILE": os.path.join(self.tmp, "nope.json")})
        self.assertEqual(lanes, EXPECTED_DEFAULT_LANES)
        self.assertEqual(source, "default")

    def test_malformed_file_falls_back_without_crashing(self):
        path = self._write("{not json at all,,,")
        lanes, source = MOD.resolve_lanes(env={
            "ROUTER_LANES_FILE": path, "ROUTER_LANES": "code,debug"})
        self.assertEqual(lanes, ["code", "debug"])
        self.assertEqual(source, "env")
        self.assertEqual(
            MOD.resolve_lanes(env={"ROUTER_LANES_FILE": path}),
            (EXPECTED_DEFAULT_LANES, "default"))

    def test_unexpected_shapes_fall_back(self):
        for payload in (["code", "debug"], {"lanes": "code"},
                        {"lanes": []}, {"nope": []}, {}, 42):
            path = self._write(payload, name="shape.json")
            lanes, source = MOD.resolve_lanes(env={"ROUTER_LANES_FILE": path})
            self.assertEqual(lanes, EXPECTED_DEFAULT_LANES, payload)
            self.assertEqual(source, "default", payload)

    def test_directory_path_falls_back_without_crashing(self):
        lanes, source = MOD.resolve_lanes(env={"ROUTER_LANES_FILE": self.tmp})
        self.assertEqual(lanes, EXPECTED_DEFAULT_LANES)
        self.assertEqual(source, "default")

    def test_lanes_json_alias_is_honored(self):
        path = self._write({"lanes": [{"intent": "tooluse",
                                       "agent": "tooluser"}],
                            "source": "frontmatter"})
        lanes, source = MOD.resolve_lanes(env={"ROUTER_LANES_JSON": path})
        self.assertEqual(lanes, ["tooluse"])
        self.assertEqual(source, "json")

    def test_file_path_wins_over_alias(self):
        good = self._write({"lanes": [{"intent": "code", "agent": "coder"}],
                            "source": "frontmatter"})
        bad = os.path.join(self.tmp, "missing.json")
        lanes, source = MOD.resolve_lanes(env={
            "ROUTER_LANES_FILE": good, "ROUTER_LANES_JSON": bad})
        self.assertEqual(lanes, ["code"])
        self.assertEqual(source, "json")

    # -- startup diagnostic ------------------------------------------------

    def test_source_is_logged_once_to_stderr(self):
        buf = io.StringIO()
        MOD.log_lanes_source(stream=buf)
        lines = buf.getvalue().strip().splitlines()
        self.assertEqual(lines[0], "prompt-router lanes source=default count=9")

    def test_json_source_log_line(self):
        path = self._write({"lanes": [{"intent": "code", "agent": "coder"},
                                      {"intent": "debug", "agent": "debugger"}],
                            "source": "frontmatter"})
        module = load_module_with_env({"ROUTER_LANES_FILE": path})
        buf = io.StringIO()
        module.log_lanes_source(stream=buf)
        self.assertEqual(buf.getvalue().strip(),
                         "prompt-router lanes source=json count=2")

    def test_unusable_file_logs_the_fallback(self):
        module = load_module_with_env(
            {"ROUTER_LANES_FILE": os.path.join(self.tmp, "gone.json")})
        buf = io.StringIO()
        module.log_lanes_source(stream=buf)
        text = buf.getvalue()
        self.assertIn("source=default count=9", text)
        self.assertIn("file unusable", text)


class ImportSafetyTests(unittest.TestCase):
    def test_import_does_not_start_the_server(self):
        # load_module() returns only if exec_module neither loaded the model
        # (transformers import would fail here) nor entered serve_forever().
        self.assertTrue(callable(MOD.main))
        self.assertTrue(callable(MOD.load_model))
        self.assertIsNone(MOD.model)
        self.assertIsNone(MOD.tok)

    def test_serve_is_guarded_by_main_check(self):
        with open(MODULE_PATH, "r", encoding="utf-8") as fh:
            source = fh.read()
        self.assertIn('if __name__ == "__main__":', source)
        guard = source.split('if __name__ == "__main__":', 1)[1]
        self.assertIn("load_model()", guard)
        self.assertIn("main()", guard)
        # Exactly one call site for main(), indented => inside the guard block.
        calls = re.findall(r"(?m)^([ \t]*)main\(\)[ \t]*$", source)
        self.assertEqual(len(calls), 1)
        self.assertTrue(calls[0], "main() called at module top level")

    def test_module_is_import_safe_with_router_env_set(self):
        module = load_module_with_env({
            "ROUTER_LANES_FILE": "/nonexistent/lanes.json",
            "ROUTER_LANES": "code",
            "ROUTER_PORT": "18082",
        })
        self.assertEqual(module.PORT, 18082)
        self.assertEqual(module.LANES, ["code"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
