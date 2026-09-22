#!/usr/bin/env python3
"""Privacy guards for classifier-eval result producers (routing-repair leaf 01).

Synthetic sentinels only: no private corpus text appears in this file or
in test output.

Contract under test, per producer module:
  - the module is import-safe: no model loading, file reads, or network at
    import time (pipeline work lives behind a ``main()`` entry point);
  - every miss entry is built by ``miss_record`` which replaces the raw
    input excerpt with ``eval_harness.case_key(text)`` and carries labeled
    identifier fields (no field named like text holding a hash);
  - the serialized result and the misses report contain no raw input text;
  - aggregate stage/count fields are unchanged by the redaction.
"""
import ast
import importlib
import json
import random
import string
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

TOOLS_DIR = Path(__file__).resolve().parent
REPO_ROOT = TOOLS_DIR.parents[1]
if str(TOOLS_DIR) not in sys.path:
    sys.path.insert(0, str(TOOLS_DIR))

import eval_harness as H  # noqa: E402

# Producers whose committed JSON artifacts carry a "misses" field.
PRODUCERS_JSON_MISSES = ("iter20_cascade_v2", "iter20_cascade_v2b",
                         "validate_silver")
# Producer whose miss excerpts go to stdout only (result dict has no misses).
PRODUCER_STDOUT_MISSES = "iter19_quickplan_cascade"
# Producers that cache per-case records over the private replay corpus.
# iter20c_sweep stores case keys + probe vectors in ``cases_cache``;
# iter18_misscheck resolves hardcoded-style miss keys to corpus text only
# inside main(). Neither may carry raw input excerpts in module or cache
# state after a run.
PRODUCERS_CASE_CACHE = ("iter20c_sweep", "iter18_misscheck")


def _sentinel() -> str:
    """Unique synthetic private-input stand-in; different on every call."""
    tag = "".join(random.SystemRandom().choice(string.ascii_uppercase +
                                               string.digits)
                  for _ in range(12))
    return f"SENTINEL{tag} synthetic replay excerpt that must never leak"


# --------------------------------------------------------------------------
# Import-safety gate: refuse to EXECUTE a producer to import it (several
# historical scripts train models at module scope). Verify statically that
# module-level statements are declarations/constants only, then import.
# --------------------------------------------------------------------------

_ALLOWED_PATH_CALLS = {("sys", "path", "insert"), ("sys", "path", "append")}
_ALLOWED_CONST_CALLS = {("re", "compile")}  # ORCH/AUTO cue patterns


def _dotted_name(node):
    parts = []
    while isinstance(node, ast.Attribute):
        parts.append(node.attr)
        node = node.value
    if isinstance(node, ast.Name):
        parts.append(node.id)
    return tuple(reversed(parts))


def _is_const_literal(node):
    """True for constants and (nested) tuple/list/frozenset-of-constant
    literals -- the shapes allowed as module-level identifier tables."""
    if isinstance(node, ast.Constant):
        return True
    if isinstance(node, (ast.Tuple, ast.List)):
        return bool(node.elts) and all(_is_const_literal(e) for e in node.elts)
    return False


def _load_producer(modname):
    src = (TOOLS_DIR / f"{modname}.py").read_text()
    for node in ast.parse(src).body:
        ok = isinstance(node, (ast.Import, ast.ImportFrom, ast.FunctionDef,
                               ast.AsyncFunctionDef, ast.ClassDef))
        if not ok and isinstance(node, ast.Expr) and \
                isinstance(node.value, ast.Constant):
            ok = True  # docstring
        if not ok and isinstance(node, ast.Assign) and \
                isinstance(node.value, ast.Constant):
            ok = True
        if not ok and isinstance(node, ast.Assign) and \
                isinstance(node.value, (ast.Tuple, ast.List)) and \
                node.value.elts and \
                all(_is_const_literal(e) for e in node.value.elts):
            ok = True  # tuple/list of constants (identifier tables)
        if not ok and isinstance(node, ast.AnnAssign) and \
                node.value is not None and isinstance(node.value, ast.Constant):
            ok = True
        if not ok and isinstance(node, ast.Expr) and \
                isinstance(node.value, ast.Call) and \
                _dotted_name(node.value.func) in _ALLOWED_PATH_CALLS:
            ok = True  # sys.path setup only
        if not ok and isinstance(node, ast.Assign) and \
                isinstance(node.value, ast.Call) and \
                _dotted_name(node.value.func) in _ALLOWED_CONST_CALLS:
            ok = True  # precompiled regex constants only
        if not ok and isinstance(node, ast.If) and \
                isinstance(node.test, ast.Compare) and \
                _dotted_name(node.test.left) == ("__name__",) and \
                len(node.test.comparators) == 1 and \
                isinstance(node.test.comparators[0], ast.Constant) and \
                node.test.comparators[0].value == "__main__" and \
                len(node.body) == 1 and isinstance(node.body[0], ast.Expr) and \
                isinstance(node.body[0].value, ast.Call) and \
                _dotted_name(node.body[0].value.func) == ("main",):
            ok = True  # canonical `if __name__ == "__main__": main()`
        if not ok:
            raise ImportError(
                f"{modname}.py is not import-safe: module-level "
                f"{type(node).__name__} at line {getattr(node, 'lineno', '?')} "
                f"executes at import; pipeline work must live behind main()")
    return importlib.import_module(modname)


# --------------------------------------------------------------------------
# Shared producer contract
# --------------------------------------------------------------------------

class ProducerPrivacyContract(unittest.TestCase):
    """Per-producer tests. Subclass sets MOD and build_result()."""
    MOD = None

    def setUp(self):
        if not self.MOD:
            raise unittest.SkipTest("abstract producer contract base class")
        self.mod = _load_producer(self.MOD)

    def make_miss(self):
        s = _sentinel()
        return s, self.mod.miss_record("B", s, "git", "quickplan")

    def test_main_entry_point_exists(self):
        self.assertTrue(callable(getattr(self.mod, "main", None)),
                        f"{self.MOD}.py must keep execution behind main()")

    def test_miss_record_replaces_excerpt_with_case_key(self):
        s, miss = self.make_miss()
        self.assertEqual(miss["stage"], "B")
        self.assertEqual(miss["expected_intent"], "git")
        self.assertEqual(miss["predicted_intent"], "quickplan")
        self.assertEqual(miss["input_case_key"], H.case_key(s))
        blob = json.dumps(miss)
        self.assertNotIn(s, blob, "raw sentinel leaked through miss record")
        for key in miss:
            self.assertNotIn("text", key.lower(),
                             "identifier hidden in a text-named field")

    def test_serialized_result_and_writer_exclude_sentinel(self):
        s, miss = self.make_miss()
        res = self.build_result(miss)
        with tempfile.TemporaryDirectory() as td:
            payload = self.mod.emit_result(res, Path(td), "probe.json")
            disk = (Path(td) / "probe.json").read_text()
        self.assertNotIn(s, payload, "sentinel in emit_result return value")
        self.assertNotIn(s, disk, "sentinel written to disk")
        self.assertIn(H.case_key(s), disk, "case key missing from writer output")
        json.loads(disk)  # writer output stays valid JSON

    def test_misses_report_excludes_sentinel(self):
        s, miss = self.make_miss()
        report = self.mod.format_misses_report([miss])
        self.assertNotIn(s, report, "sentinel in misses report (stdout path)")
        self.assertIn(H.case_key(s), report)


class TestIter20CascadeV2(ProducerPrivacyContract):
    MOD = "iter20_cascade_v2"

    def build_result(self, miss):
        return self.mod.build_result(
            replay_n=4, a_n=1, a_ok=0, b_n=1, b_ok=1, c_n=2, tau=0.421,
            misses=[miss])

    def test_aggregate_fields_unchanged(self):
        res = self.build_result(None)
        expected = {
            "replay_n": 4,
            "stageA": {"routes": 1, "correct": 0, "precision": 0.0},
            "stageB": {"routes": 1, "correct": 1, "precision": 1.0},
            "stageC_chain": 2,
            "expected_system_accuracy":
                round((0 + 1 + 2 * H.CHAIN_BASELINE) / 4, 4),
            "tau": 0.421,
            "misses": [None],
        }
        self.assertEqual(res, expected)


class TestIter20CascadeV2b(ProducerPrivacyContract):
    MOD = "iter20_cascade_v2b"

    def build_result(self, miss):
        return self.mod.build_result(
            replay_n=4, a_n=1, a_ok=0, b_n=1, b_ok=1, c_n=2, tau=0.421,
            misses=[miss])

    def test_aggregate_fields_unchanged(self):
        res = self.build_result(None)
        expected = {
            "replay_n": 4,
            "stageA": {"routes": 1, "correct": 0, "precision": 0.0},
            "stageB": {"routes": 1, "correct": 1, "precision": 1.0},
            "stageC_chain": 2,
            "expected_system_accuracy":
                round((0 + 1 + 2 * H.CHAIN_BASELINE) / 4, 4),
            "tau": 0.421,
            "misses": [None],
        }
        self.assertEqual(res, expected)


class TestValidateSilver(ProducerPrivacyContract):
    MOD = "validate_silver"

    def build_result(self, miss):
        return self.mod.build_result(
            silver_n=4, a_n=1, a_ok=0, b_n=1, b_ok=1, c_n=2, tau=0.421,
            misses=[miss])

    def test_aggregate_fields_unchanged(self):
        res = self.build_result(None)
        expected = {
            "silver_n": 4,
            "stageA": {"routes": 1, "correct": 0, "precision": 0.0},
            "stageB": {"routes": 1, "correct": 1, "precision": 1.0},
            "stageC_chain": 2,
            "expected_system_accuracy":
                round((0 + 1 + 2 * H.CHAIN_BASELINE) / 4, 4),
            "tau": 0.421,
            "misses": [None],
        }
        self.assertEqual(res, expected)


class TestIter19QuickplanCascade(ProducerPrivacyContract):
    MOD = "iter19_quickplan_cascade"

    def make_miss(self):
        s = _sentinel()
        return s, self.mod.miss_record("B", s, "git", "quickplan")

    def build_result(self, miss):
        return self.mod.build_result(
            replay_n=4, a_n=1, a_ok=0, b_n=1, b_ok=1, c_n=2, tau=0.421,
            per_intent_expected={"code": {"n": 2, "expected_correct": 1.5}})

    def test_serialized_result_and_writer_exclude_sentinel(self):
        """iter19's historical artifact contains NO misses at all, so the
        writer assertion is sentinel-absence (identifier-in-file does not
        apply); the redacted identifiers appear on the stdout report path
        instead (covered by test_misses_report_excludes_sentinel)."""
        s, miss = self.make_miss()
        res = self.build_result(miss)
        with tempfile.TemporaryDirectory() as td:
            payload = self.mod.emit_result(res, Path(td), "probe.json")
            disk = (Path(td) / "probe.json").read_text()
        self.assertNotIn(s, payload, "sentinel in emit_result return value")
        self.assertNotIn(s, disk, "sentinel written to disk")
        json.loads(disk)

    def test_aggregate_fields_unchanged_and_no_misses_key(self):
        """Pre-existing iter19 field contract (leaf 02 will revisit the
        stdout/misses split): build_result must match the historical
        schema exactly -- no misses key, misses stay stdout-only."""
        res = self.build_result(None)
        expected = {
            "replay_n": 4,
            "stageA": {"routes": 1, "correct": 0, "precision": 0.0},
            "stageB": {"routes": 1, "correct": 1, "precision": 1.0},
            "stageC_chain": 2,
            "expected_system_accuracy":
                round((0 + 1 + 2 * H.CHAIN_BASELINE) / 4, 4),
            "chain_only_baseline": H.CHAIN_BASELINE,
            "per_intent_expected": {"code": {"n": 2, "expected_correct": 1.5}},
            "tau": 0.421,
        }
        self.assertEqual(res, expected)


# --------------------------------------------------------------------------
# Case-cache producers: no raw excerpt may reach the per-case cache, the
# module namespace, or the module source (routing-repair leaf 01 follow-up).
# --------------------------------------------------------------------------

class TestCaseCacheProducers(unittest.TestCase):
    """iter20c_sweep / iter18_misscheck redaction contract."""

    def test_case_cache_producers_are_import_safe(self):
        for modname in PRODUCERS_CASE_CACHE:
            with self.subTest(mod=modname):
                self.assertIsNotNone(_load_producer(modname))
                mod = sys.modules[modname]
                self.assertTrue(callable(getattr(mod, "main", None)),
                                f"{modname}.py must keep execution behind main()")

    def test_iter20c_cache_entry_holds_case_key_not_excerpt(self):
        import iter20c_sweep as sweep
        s = _sentinel()
        entry = sweep.cache_entry(s, "git", None, False)
        self.assertEqual(entry["input_case_key"], H.case_key(s))
        self.assertEqual(entry["true"], "git")
        self.assertEqual(entry["a_lab"], None)
        self.assertEqual(entry["cue"], False)
        blob = json.dumps(entry)
        self.assertNotIn(s, blob, "raw sentinel leaked through cases_cache entry")
        for key in entry:
            self.assertNotIn("text", key.lower(),
                             "excerpt hidden in a text-named cache field")

    def test_iter18_miss_keys_do_not_embed_private_text(self):
        import iter18_misscheck as mc
        src = (TOOLS_DIR / "iter18_misscheck.py").read_text()
        expected_keys = {k for k, _, _ in mc.MISS_KEYS}
        self.assertEqual(len(expected_keys), 3,
                         "three silver-miss case keys expected")
        for k in expected_keys:
            self.assertRegex(k, r"^[0-9a-f]{16}$",
                             "miss identifier must be a case key")
        # resolve-from-corpus path: the source must not contain any
        # prefix of a resolved private text (prefixes are derived from
        # the local corpus at run time -- nothing private is embedded
        # in this test file).
        texts = mc.load_miss_texts()
        self.assertTrue(texts, "miss keys must resolve against the corpus")
        for k, t in texts.items():
            self.assertEqual(H.case_key(t), k,
                             f"corpus text {k} does not resolve to its key")
            self.assertNotIn(t[:30], src,
                             f"private excerpt prefix leaked into source ({k})")
            self.assertNotIn(t, src)


# --------------------------------------------------------------------------
# Task 4: no tracked file may carry raw replay-corpus text (category 3
# scrub, decision B). Needles are derived at run time from the untracked
# private replay corpus; the designed 389-case corpus (testdata/eval) and
# the harness fixtures are excluded per the campaign adjudication.
# --------------------------------------------------------------------------

REPLAY_CORPUS = TOOLS_DIR / "replay-gold.local.json5"
SCAN_SCRIPT = TOOLS_DIR / "scan_replay_privacy.py"


class TestNoReplayTextInTrackedFiles(unittest.TestCase):
    """The tracked tree must not carry verbatim replay-corpus text."""

    def test_scan_script_reports_no_hits(self):
        if not REPLAY_CORPUS.exists():
            self.skipTest("private replay corpus not present on this machine")
        out = subprocess.run([sys.executable, str(SCAN_SCRIPT)],
                             cwd=REPO_ROOT, capture_output=True, text=True)
        self.assertEqual(
            out.returncode, 0,
            f"tracked files carry replay-corpus text:\n{out.stdout}")

    def test_scan_script_covers_full_corpus(self):
        """Guard the needle derivation itself: every corpus case must
        contribute at least a 40-char prefix needle, and the corpus must
        be the adjudicated 48-case replay set."""
        if not REPLAY_CORPUS.exists():
            self.skipTest("private replay corpus not present on this machine")
        import scan_replay_privacy as S
        inputs = S.corpus_inputs()
        self.assertEqual(len(inputs), 48,
                         "replay corpus changed; re-derive needles")
        for t in inputs:
            with self.subTest(case_key=H.case_key(t)):
                self.assertTrue(S.needles_for(t),
                                "every case must yield needles")


# --------------------------------------------------------------------------
# Task 3: renamed local data must stay untracked, without hiding fixtures.
# --------------------------------------------------------------------------

class TestGitignoreProtection(unittest.TestCase):

    def _check_ignore_rc(self, relpath: str) -> int:
        out = subprocess.run(
            ["git", "check-ignore", "--no-index", "-q", relpath],
            cwd=REPO_ROOT, capture_output=True, text=True)
        return out.returncode

    def test_renamed_local_json5_ignored_under_classifier_eval(self):
        for rel in (
                "tools/classifier-eval/rename-probe.local.json5",
                "tools/classifier-eval/results/rename-probe.local.json5",
                "tools/classifier-eval/results/iter-20/rename-probe.local.json5",
        ):
            with self.subTest(path=rel):
                self.assertEqual(
                    self._check_ignore_rc(rel), 0,
                    f"{rel} must be gitignored (private local data)")

    def test_committed_result_fixtures_still_visible(self):
        tracked = subprocess.run(
            ["git", "ls-files", "tools/classifier-eval/results"],
            cwd=REPO_ROOT, capture_output=True, text=True,
            check=True).stdout.split()
        self.assertTrue(tracked, "classifier-eval results must stay tracked")
        for rel in tracked:
            with self.subTest(path=rel):
                self.assertEqual(
                    self._check_ignore_rc(rel), 1,
                    f"tracked fixture {rel} must remain visible to git")

    def test_no_tracked_local_json5_anywhere(self):
        out = subprocess.run(
            ["git", "ls-files", "*.local.json5"],
            cwd=REPO_ROOT, capture_output=True, text=True, check=True).stdout
        self.assertEqual(out.strip(), "",
                         "*.local.json5 files must never be tracked")


if __name__ == "__main__":
    unittest.main()
