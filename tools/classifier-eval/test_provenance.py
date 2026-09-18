#!/usr/bin/env python3
"""Corpus and cache provenance tests (routing-repair leaf 03).

Synthetic corpus text only; no private corpus text appears in this file
or in test output. Tests are offline: no embed server, no model load,
no writes to the real ~/.meept caches or the tracked fold-assignment
cache (every fold test passes an explicit temporary cache_path).

Covers, per docs/plans/20260917-routing-repair/03-provenance.md:
  Task 1  builder default population == harness eligible key set
  Task 2  artifact provenance metadata + model content identity
  Task 3  cache identity namespace + response-index mapping hardening
  Task 4  fold evidence policy (explicit init, growth preservation)
"""
from __future__ import annotations

import hashlib
import importlib.util
import json
import sys
import tempfile
import unittest
from pathlib import Path

TOOLS_DIR = Path(__file__).resolve().parent
REPO_ROOT = TOOLS_DIR.parents[1]
if str(TOOLS_DIR) not in sys.path:
    sys.path.insert(0, str(TOOLS_DIR))

import eval_harness as H  # noqa: E402


def _load_module(name: str, path: Path):
    spec = importlib.util.spec_from_file_location(name, path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


BUILDER = _load_module(
    "build_prefilter_centroids",
    REPO_ROOT / "scripts" / "build_prefilter_centroids.py")

BUILDER_DEFAULT_TFIDF_OUT = "internal/agent/testdata/prefilter_tfidf_veto.json"


def write_corpus(path: Path, body: str) -> Path:
    path.write_text(body)
    return path


CATEGORIES_LAYOUT = """{
  name: "synthetic categories corpus",
  categories: {
    code: [
      { input: "syn-alpha run the unit test suite", added_in: "t1" },
      // expected_intent override: category label differs from gold intent
      { input: "syn-beta refactor this module please",
        expected_intent: "refactor", added_in: "t1" },
    ],
    chat: [
      { input: "syn-gamma hello there friend", added_in: "t2" },
    ],
  },
}
"""

CASES_LAYOUT = """{
  cases: [
    // OOD entry: must never reach the index or the eligible key set
    { input: "syn-delta what is the meaning of life", ood: true },
    { input: "syn-epsilon write a pong clone",
      expected_intent: "code", id: "syn-eps" },
    // escaped double quote inside the input string
    { input: "syn-zeta say \\"quoted\\" out loud",
      expected_intent: "chat", id: "syn-zeta" },
    // expected_intent override inside the cases layout
    { input: "syn-eta plan the sprint for me",
      expected_intent: "quickplan", id: "syn-eta" },
  ],
}
"""


class TestCorpusParity(unittest.TestCase):
    """Task 1: default builder input == harness eligible population."""

    def test_builder_default_matches_harness_eligible_keys(self):
        """The default selection must equal the non-OOD keys of the
        harness loader's combined base+adversarial population (MEAS-02).
        The historical default was adversarial-only (222 eligible keys
        vs 361): this test pins the repair."""
        base, adv = H.load_cases()
        harness_keys = {H.case_key(c.text) for c in base + adv
                        if not c.ood and c.intent}
        builder_cases = BUILDER.select_default_cases()
        builder_keys = {H.case_key(c.text) for c in builder_cases}
        self.assertEqual(
            len(builder_keys), len(builder_cases),
            "builder selection contains duplicate case keys")
        self.assertEqual(builder_keys, harness_keys,
                         f"builder default population {len(builder_keys)} "
                         f"keys != harness eligible {len(harness_keys)} keys; "
                         f"missing={sorted(harness_keys - builder_keys)[:5]} "
                         f"extra={sorted(builder_keys - harness_keys)[:5]}")

    def test_explicit_subset_is_supported_and_labeled(self):
        """Single-corpus runs remain available and are labeled a subset."""
        with tempfile.TemporaryDirectory() as td:
            adv = write_corpus(Path(td) / "cases.json5", CASES_LAYOUT)
            sel = BUILDER.select_explicit_subset(adv)
            keys = {H.case_key(c.text) for c in sel}
            self.assertEqual(len(keys), 3)  # OOD excluded
            self.assertNotIn(H.case_key("syn-delta what is the meaning of life"),
                             keys)


class TestSelectionSemantics(unittest.TestCase):
    """Shared selection semantics, driven through the harness loader."""

    def _eligible(self, base_body: str | None, adv_body: str | None):
        with tempfile.TemporaryDirectory() as td:
            base = write_corpus(Path(td) / "b.json5",
                                base_body or CATEGORIES_LAYOUT)
            adv = write_corpus(Path(td) / "a.json5", adv_body or CASES_LAYOUT)
            base_cases, adv_cases = H.load_cases(base, adv)
            return [c for c in base_cases + adv_cases if not c.ood and c.intent]

    def test_ood_excluded(self):
        cases = self._eligible(None, None)
        texts = [c.text for c in cases]
        self.assertNotIn("syn-delta what is the meaning of life", texts)

    def test_expected_intent_override_categories_layout(self):
        cases = self._eligible(None, None)
        by_text = {c.text: c.intent for c in cases}
        # category label is "code", gold intent is the override "refactor"
        self.assertEqual(by_text["syn-beta refactor this module please"],
                         "refactor")
        # no override: the category label is the intent
        self.assertEqual(by_text["syn-alpha run the unit test suite"], "code")

    def test_expected_intent_override_cases_layout(self):
        cases = self._eligible(None, None)
        by_text = {c.text: c.intent for c in cases}
        self.assertEqual(by_text["syn-eta plan the sprint for me"], "quickplan")
        self.assertEqual(by_text["syn-epsilon write a pong clone"], "code")

    def test_duplicate_keys_dedupe_in_key_set(self):
        # same text in both corpora: two Case objects, ONE eligible key
        base_body = """{ categories: { code: [
            { input: "syn-dup shared text" },
        ] } }"""
        adv_body = ('{"cases": [{ "input": "syn-dup shared text", '
                    '"expected_intent": "code" }]}')
        cases = self._eligible(base_body, adv_body)
        keys = [H.case_key(c.text) for c in cases]
        self.assertEqual(len(keys), 2)       # loader keeps both rows
        self.assertEqual(len(set(keys)), 1)  # the eligible KEY SET dedupes

    def test_both_layouts_loaded(self):
        cases = self._eligible(None, None)
        self.assertEqual(len(cases), 6)  # 3 categories + 3 non-OOD cases

    def test_escaped_text_round_trips(self):
        cases = self._eligible(None, None)
        by_text = {c.text: c.intent for c in cases}
        self.assertIn('syn-zeta say "quoted" out loud', by_text)
        self.assertEqual(by_text['syn-zeta say "quoted" out loud'], "chat")


class TestProvenanceMetadata(unittest.TestCase):
    """Task 2: artifact provenance emission + Go-tolerated shape."""

    def test_model_dir_hash_covers_content(self):
        """model_dir_sha256 must cover every file's content and relative
        path: two dirs with identical names but different weights hash
        differently; identical content in differently-named dirs hashes
        identically only if layout matches (name+content in sorted
        order). No endpoint URL or credential is ever an input."""
        with tempfile.TemporaryDirectory() as td:
            d1 = Path(td) / "model-A"
            d2 = Path(td) / "model-B"
            d1.mkdir()
            d2.mkdir()
            (d1 / "model.safetensors").write_bytes(b"weights-v1")
            (d1 / "tokenizer.json").write_bytes(b"tok")
            (d2 / "model.safetensors").write_bytes(b"weights-v2")
            (d2 / "tokenizer.json").write_bytes(b"tok")
            h1 = BUILDER.model_dir_sha256(str(d1))
            h1b = BUILDER.model_dir_sha256(str(d1))
            h2 = BUILDER.model_dir_sha256(str(d2))
            self.assertEqual(h1, h1b)          # deterministic
            self.assertNotEqual(h1, h2)        # different content
            # shard layout matters too: a 2-shard variant differs
            d3 = Path(td) / "model-C"
            d3.mkdir()
            (d3 / "model-00001-of-00002.safetensors").write_bytes(b"we")
            (d3 / "model-00002-of-00002.safetensors").write_bytes(b"ights-v1")
            self.assertNotEqual(
                h1, BUILDER.model_dir_sha256(str(d3)))
            with self.assertRaises(ValueError):
                BUILDER.model_dir_sha256(str(Path(td) / "missing"))

    def test_eligible_key_set_hash_is_stable_and_population_sensitive(self):
        k1 = BUILDER.case_key_of("syn-alpha run the unit test suite")
        self.assertEqual(k1, H.case_key("syn-alpha run the unit test suite"))
        keys = sorted(BUILDER.case_key_of(c.text)
                      for c in BUILDER.select_default_cases())
        digest = hashlib.sha256("\n".join(keys).encode()).hexdigest()
        self.assertEqual(len(digest), 64)  # sha256 hex over the full set
        # population sensitivity: a changed set hashes differently
        digest2 = hashlib.sha256(
            "\n".join(keys[:-1]).encode()).hexdigest()
        self.assertNotEqual(digest, digest2)

    def test_store_shape_is_go_decoder_compatible(self):
        """The Go runtime reader (internal/agent/embedding_prefilter.go
        loadIndex) unmarshals into a fixed struct with plain
        json.Unmarshal and no DisallowUnknownFields, so an additive
        ``provenance`` key must not break decoding, and every
        runtime-required field keeps its name and type. Verified by
        reading the Go struct fields from source and decoding a store
        carrying the new block with the same field names."""
        go_src = (REPO_ROOT / "internal" / "agent" /
                  "embedding_prefilter.go").read_text()
        struct_start = go_src.index("json:\"model\"")
        # required fields the Go decoder names explicitly
        for field in ('json:"model"', 'json:"dimension"',
                      'json:"built_at"', 'json:"corpus"',
                      'json:"examples"'):
            self.assertIn(field, go_src)
        # the decoder does NOT reject unknown fields
        self.assertNotIn("DisallowUnknownFields", go_src)
        # simulate the Go decode: unknown keys survive a struct-shaped
        # json round trip; required fields keep their values
        store = {
            "model": "qwen3-embedding", "dimension": 4,
            "built_at": "2026-09-18T00:00:00+00:00",
            "corpus": "synthetic", "k": 5,
            "provenance": {
                "schema": 1, "population": "default-eval-population",
                "document_count": 2,
                "eligible_key_set_sha256": "a" * 64,
                "source_files": ["x.json5"], "source_sha256": {"x.json5": "b" * 64},
                "embedding": {
                    "model_alias": "qwen3-embedding",
                    "model_path": "/synthetic/model",
                    "model_content_sha256": "c" * 64,
                    "instruction": "",
                    "preprocessing": "last-token(EOS) pooling, L2-normalized",
                },
            },
            "examples": [
                {"intent": "code", "agent": "", "text": "synthetic one",
                 "vector": [0.5, 0.5, 0.5, 0.5]},
                {"intent": "chat", "agent": "", "text": "synthetic two",
                 "vector": [0.5, 0.5, 0.5, 0.5]},
            ],
        }
        decoded = json.loads(json.dumps(store))
        # unknown top-level key tolerated (Python dicts are Go maps here;
        # the Go struct check above proves the runtime tolerance), and
        # the fields the Go decoder READS are unchanged in shape
        for f in ("model", "dimension", "built_at", "corpus", "examples"):
            self.assertIn(f, decoded)
        self.assertIsInstance(decoded["provenance"]["document_count"], int)

    def test_tfidf_veto_shape_is_go_decoder_compatible(self):
        """Same check for the veto model (internal/agent/tfidf_veto.go):
        plain Unmarshal into a fixed struct; additive metadata is
        tolerated; required fields unchanged."""
        go_src = (REPO_ROOT / "internal" / "agent" /
                  "tfidf_veto.go").read_text()
        for field in ('json:"vocab"', 'json:"idf"', 'json:"classes"',
                      'json:"coef"', 'json:"intercept"',
                      'json:"ngram_range"', 'json:"char_wb"',
                      'json:"built_at"'):
            self.assertIn(field, go_src)
        self.assertNotIn("DisallowUnknownFields", go_src)
        model = {
            "built_at": "2026-09-18T00:00:00+00:00",
            "corpus": ["synthetic.json5"],
            "ngram_range": [2, 4], "char_wb": True,
            "train_docs": 2, "train_accuracy": 1.0,
            "vocab": {" s": 0, "sy": 1}, "idf": [1.0, 1.0],
            "classes": ["code"], "coef": [[0.1, 0.2]], "intercept": [0.0],
            "threshold": 0.0,
            # additive provenance (new)
            "provenance": {
                "schema": 1,
                "population": "default-eval-population",
                "document_count": 2,
                "eligible_key_set_sha256": "a" * 64,
                "source_files": ["synthetic.json5"],
                "source_sha256": {"synthetic.json5": "b" * 64},
                "preprocessing": {"ngram_lo": 2, "ngram_hi": 4,
                                  "char_wb": True, "sublinear_tf": True,
                                  "smooth_idf": True, "min_df": 1},
            },
        }
        decoded = json.loads(json.dumps(model))
        for f in ("vocab", "idf", "classes", "coef", "intercept",
                  "ngram_range", "char_wb", "built_at"):
            self.assertIn(f, decoded)
        self.assertEqual(decoded["provenance"]["schema"], 1)

    def test_fixture_is_deleted_and_recreatable(self):
        """Recorded decision 2026-09-17 #4: the tracked fixture
        internal/agent/testdata/prefilter_tfidf_veto.json was deleted and
        tests build their own temporary models. The builder keeps that
        path as its DEFAULT OUTPUT so regeneration can recreate the
        artifact; this test pins the disposition (fixture status distinct
        from deployed-artifact status) without restoring the file."""
        fixture = REPO_ROOT / "internal" / "agent" / "testdata" / \
            "prefilter_tfidf_veto.json"
        self.assertFalse(
            fixture.exists(),
            "fixture was deleted by recorded decision; its return needs "
            "an explicit regeneration approval, not a silent rebuild")
        self.assertEqual(
            BUILDER_DEFAULT_TFIDF_OUT,
            "internal/agent/testdata/prefilter_tfidf_veto.json")

    def test_tfidf_veto_build_population_and_provenance(self):
        """build_tfidf_veto.build() records the ACTUAL selected
        population (dedup'd non-OOD docs), not a hardcoded document
        count, and its output carries the provenance block; the Go
        veto decoder tolerates it (checked above)."""
        tv_spec = importlib.util.spec_from_file_location(
            "build_tfidf_veto", REPO_ROOT / "scripts" / "build_tfidf_veto.py")
        TV = importlib.util.module_from_spec(tv_spec)
        tv_spec.loader.exec_module(TV)
        with tempfile.TemporaryDirectory() as td:
            c1 = Path(td) / "cat.json5"
            c1.write_text(CATEGORIES_LAYOUT)
            c2 = Path(td) / "cases.json5"
            c2.write_text(CASES_LAYOUT)
            out = Path(td) / "veto.json"
            rc = TV.build(["-v", "--corpus", str(c1),
                           "--corpus", str(c2), "--out", str(out)])
            self.assertEqual(rc, 0)
            model = json.loads(out.read_text())
            # 6 non-OOD rows loaded; syn-dup? no duplicates in these
            # fixtures -> 6 docs expected, asserted from the LOADER not
            # a constant doc count:
            base_cases, adv_cases = H.load_cases(c1, c2)
            expected = len({H.case_key(c.text)
                            for c in base_cases + adv_cases
                            if not c.ood and c.intent})
            self.assertEqual(model["provenance"]["document_count"],
                             expected)
            # two synthetic corpora != the tracked default population:
            # the label must be the subset experiment, from REAL parity
            self.assertEqual(model["provenance"]["population"],
                             "subset-explicit")
            self.assertIn("eligible_key_set_sha256", model["provenance"])
            # required runtime fields still present and shaped
            for f in ("vocab", "idf", "classes", "coef", "intercept",
                      "ngram_range", "char_wb", "built_at", "train_docs",
                      "train_accuracy", "threshold"):
                self.assertIn(f, model)
            self.assertEqual(model["train_docs"], expected)


class TestCacheIdentity(unittest.TestCase):
    """Task 3: cache namespace must carry verified model content identity
    (MEAS-05, contract C3, recorded decision 2026-09-17 #3)."""

    def _response(self, vectors):
        """OpenAI-shaped response; index i maps to input i."""
        return {"data": [{"index": i, "embedding": list(map(float, v))}
                         for i, v in enumerate(vectors)]}

    def test_reproduces_legacy_alias_cache_collision(self):
        """THE LEAD (reproduction): two different local weight
        directories served under the SAME alias+instruction produced the
        SAME cache namespace in the legacy tag scheme -> the second
        model silently reused the first model's cached vectors."""
        import tempfile
        with tempfile.TemporaryDirectory() as td:
            legacy = H.Embedder("http://syn.invalid/v1", "qwen3-embedding",
                                cache_dir=Path(td))
            same = H.Embedder("http://other.invalid/v1", "qwen3-embedding",
                              cache_dir=Path(td))
            self.assertEqual(legacy.cache_dir, same.cache_dir,
                             "collision no longer reproduces: legacy "
                             "namespace now differs (repair landed?)")

    def test_content_identity_separates_namespaces(self):
        """With a verified content fingerprint in the namespace, two
        aliases over the SAME directory share a cache; the same alias
        over DIFFERENT content does not."""
        import tempfile
        with tempfile.TemporaryDirectory() as td:
            m1 = Path(td) / "m1"
            m1.mkdir()
            (m1 / "model.safetensors").write_bytes(b"weights-1")
            m2 = Path(td) / "m2"
            m2.mkdir()
            (m2 / "model.safetensors").write_bytes(b"weights-2")
            fp1 = H.model_content_identity(str(m1))
            fp2 = H.model_content_identity(str(m2))
            self.assertNotEqual(fp1["fingerprint"], fp2["fingerprint"])
            self.assertTrue(fp1["verified"])
            # same content -> same fingerprint
            fp1b = H.model_content_identity(str(m1))
            self.assertEqual(fp1["fingerprint"], fp1b["fingerprint"])
            # unverified: no fingerprint available
            unv = H.model_content_identity(None)
            self.assertFalse(unv["verified"])

    def test_cache_namespace_layout(self):
        """cache namespace = identity-hash|instruction-hash|preproc-hash;
        missing identity lands in an explicit 'unverified' namespace."""
        import tempfile
        with tempfile.TemporaryDirectory() as td:
            m = Path(td) / "m"
            m.mkdir()
            (m / "w").write_bytes(b"weights")
            root = Path(td) / "cache-root"
            e1 = H.Embedder("http://a.invalid/v1", "alias-a",
                            model_path=str(m), cache_dir=root)
            e2 = H.Embedder("http://b.invalid/v1", "alias-b",
                            model_path=str(m), cache_dir=root)
            # different endpoints+aliases, same verified content and
            # instruction: SAME cache namespace (contract C3)
            self.assertEqual(e1.cache_dir, e2.cache_dir)
            self.assertIn("verified", e1.identity["verified"] and "verified")
            # changed instruction -> different namespace
            e3 = H.Embedder("http://a.invalid/v1", "alias-a",
                            model_path=str(m), instruction="task: code ",
                            cache_dir=Path(td) / "c3")
            self.assertNotEqual(e1.cache_dir, e3.cache_dir)
            # no identity -> explicit unverified namespace
            e4 = H.Embedder("http://a.invalid/v1", "alias-a",
                            cache_dir=Path(td) / "c4")
            self.assertTrue(
                (Path(td) / "c4" / e4.cache_dir.name / "identity.json")
                .exists())
            ident = json.loads((e4.cache_dir / "identity.json").read_text())
            self.assertFalse(ident["verified"])
            self.assertIsNone(ident["model_fingerprint"])
            self.assertFalse(e4.identity["verified"])
            # fail-closed policy: acceptance refuses the unverified
            # namespace; exploratory use can catch and label explicitly
            with self.assertRaises(RuntimeError):
                H.assert_verified_cache(e4)
            try:
                H.assert_verified_cache(e4)
            except RuntimeError as exc:
                self.assertIn("UNVERIFIED", str(exc))
            # verified embedders pass and carry the fingerprint
            got_ident = H.assert_verified_cache(e1)
            self.assertTrue(got_ident["verified"])
            self.assertEqual(got_ident["fingerprint"],
                             e1.identity["fingerprint"])

    def test_embed_response_index_mapping(self):
        """Response rows are mapped by their declared INDEX, not response
        position; duplicate/missing/out-of-range indexes are errors."""
        e = H.Embedder.__new__(H.Embedder)
        e.mem = {}
        e.degenerate_rows = 0
        missing = [("k0", "t0"), ("k1", "t1"), ("k2", "t2")]
        # reversed response order maps correctly by index
        resp = {"data": [
            {"index": 2, "embedding": [0.0, 0.0, 1.0]},
            {"index": 0, "embedding": [1.0, 0.0, 0.0]},
            {"index": 1, "embedding": [0.0, 1.0, 0.0]},
        ]}
        got = H._map_response_vectors(resp, missing)
        self.assertAlmostEqual(float(got[0][0]), 1.0)   # k0
        self.assertAlmostEqual(float(got[1][1]), 1.0)   # k1
        self.assertAlmostEqual(float(got[2][2]), 1.0)   # k2
        # duplicate index -> error
        dup = {"data": [
            {"index": 0, "embedding": [1.0]},
            {"index": 0, "embedding": [2.0]},
        ]}
        with self.assertRaises(RuntimeError):
            H._map_response_vectors(dup, missing)
        # missing index -> error
        short = {"data": [{"index": 0, "embedding": [1.0]}]}
        with self.assertRaises(RuntimeError):
            H._map_response_vectors(short, missing)
        # out-of-range index -> error
        oob = {"data": [{"index": 5, "embedding": [1.0]}]}
        with self.assertRaises(RuntimeError):
            H._map_response_vectors(oob, missing)

    def test_embed_keys_stores_by_index_not_position(self):
        """End-to-end through embed_keys with a reversed-order stub
        server response: each key must receive ITS OWN vector
        (production writer repair, exercised offline)."""
        import tempfile
        import urllib.request

        class FakeNoop:
            """Refuse any real network call: the stub below replaces it."""
            def __call__(self, *a, **k):
                raise AssertionError("no network in provenance tests")

        with tempfile.TemporaryDirectory() as td:
            e = H.Embedder("http://syn.invalid/v1", "m",
                           cache_dir=Path(td) / "cache")
            calls = {}
            payload = {"data": [
                {"index": 1, "embedding": [0.0, 1.0, 0.0]},
                {"index": 0, "embedding": [1.0, 0.0, 0.0]},
            ]}
            class FakeResp:
                def __enter__(self):
                    return self
                def __exit__(self, *a):
                    return False
                def read(self):
                    return json.dumps(payload).encode()
            def fake_urlopen(req, timeout=None):
                calls["body"] = json.loads(req.data.decode())
                return FakeResp()
            real = H.urllib.request.urlopen
            H.urllib.request.urlopen = fake_urlopen
            try:
                e.embed_keys(["text-zero", "text-one"], ["k0", "k1"])
            finally:
                H.urllib.request.urlopen = real
            v0 = e.mem["k0"]
            v1 = e.mem["k1"]
            self.assertAlmostEqual(float(v0[0]), 1.0)
            self.assertAlmostEqual(float(v1[1]), 1.0)




class TestFoldEvidence(unittest.TestCase):
    """Task 4: fold evidence policy (MEAS-06).

    Observed baseline (probe, 2026-09-18): assign_folds with an ABSENT
    cache silently creates one; malformed JSON raises (loud); a stored
    non-integer fold value is accepted verbatim; missing keys are
    assigned deterministically and persisted. Deterministic assignment
    for existing keys was NEVER broken by growth (the cache preserves
    first assignment) -- MEAS-06 is therefore a POLICY repair (explicit
    initialization + mandatory recorded evidence for acceptance), not a
    demonstrated assignment error.
    """

    def setUp(self):
        self._real = H.FOLD_CACHE
        self.td = tempfile.TemporaryDirectory()
        H.FOLD_CACHE = Path(self.td.name) / "folds.json"
        self.cases = [H.Case(f"probe text {i}", "code", "", False,
                             "probe", "", f"p{i}") for i in range(5)]

    def tearDown(self):
        H.FOLD_CACHE = self._real
        self.td.cleanup()

    def test_absent_cache_is_silent_creation(self):
        """Baseline behavior: an absent cache file is auto-created with
        deterministic hash folds (recorded here as evidence; the policy
        repair makes initialization EXPLICIT at the call site)."""
        self.assertFalse(H.FOLD_CACHE.exists())
        folds = H.assign_folds(self.cases)
        self.assertTrue(H.FOLD_CACHE.exists())
        self.assertEqual(len(folds), 5)
        self.assertTrue(all(0 <= v < H.NFOLDS for v in folds.values()))

    def test_assign_folds_explicit_preserves_and_grows(self):
        """explicit_assign_folds: same deterministic folds as the legacy
        path; growth never reassigns an existing key."""
        f1 = H.explicit_assign_folds(self.cases, H.FOLD_CACHE,
                                     experiment="t4-test")
        grown = self.cases + [H.Case(f"probe new {i}", "chat", "", False,
                                     "probe", "", f"n{i}")
                              for i in range(5)]
        f2 = H.explicit_assign_folds(grown, H.FOLD_CACHE,
                                     experiment="t4-test")
        for c in self.cases:
            self.assertEqual(f2[H.case_key(c.text)], f1[H.case_key(c.text)])

    def test_explicit_assign_folds_rejects_malformed(self):
        H.FOLD_CACHE.parent.mkdir(parents=True, exist_ok=True)
        H.FOLD_CACHE.write_text("{not json")
        with self.assertRaises(ValueError) as ctx:
            H.explicit_assign_folds(self.cases, H.FOLD_CACHE,
                                    experiment="t4-test")
        self.assertIn("malformed", str(ctx.exception))

    def test_explicit_assign_folds_rejects_wrong_type_values(self):
        H.FOLD_CACHE.parent.mkdir(parents=True, exist_ok=True)
        bad_key = H.case_key(self.cases[0].text)
        H.FOLD_CACHE.write_text(json.dumps({bad_key: "three"}))
        with self.assertRaises(ValueError) as ctx:
            H.explicit_assign_folds(self.cases, H.FOLD_CACHE,
                                    experiment="t4-test")
        self.assertIn(bad_key, str(ctx.exception))

    def test_explicit_assign_folds_rejects_out_of_range_folds(self):
        H.FOLD_CACHE.parent.mkdir(parents=True, exist_ok=True)
        bad_key = H.case_key(self.cases[0].text)
        H.FOLD_CACHE.write_text(json.dumps({bad_key: 99}))
        with self.assertRaises(ValueError):
            H.explicit_assign_folds(self.cases, H.FOLD_CACHE,
                                    experiment="t4-test")

    def test_experiment_mismatch_requires_new_cache(self):
        """A recorded cache belongs to one experiment: reusing it under a
        different experiment name is refused (no silent fold mixing)."""
        H.explicit_assign_folds(self.cases, H.FOLD_CACHE,
                                experiment="exp-a")
        with self.assertRaises(ValueError) as ctx:
            H.explicit_assign_folds(self.cases, H.FOLD_CACHE,
                                    experiment="exp-b")
        self.assertIn("exp-b", str(ctx.exception))

    def test_recorded_metadata_carries_case_count(self):
        H.explicit_assign_folds(self.cases, H.FOLD_CACHE,
                                experiment="meta-test")
        doc = json.loads(H.FOLD_CACHE.read_text())
        self.assertEqual(doc["experiment"], "meta-test")
        self.assertEqual(doc["cases"], 5)
        self.assertEqual(doc["schema"], 1)
        self.assertIn("created_at", doc)
        self.assertIn("folds", doc)

    def test_acceptance_requires_recorded_fold_evidence(self):
        """The fail-closed policy hook: acceptance must refuse missing,
        malformed, or incomplete fold evidence, and accept a complete
        recording."""
        # missing file
        with self.assertRaises(RuntimeError):
            H.require_fold_evidence(H.FOLD_CACHE, list(self.cases))
        # malformed
        H.FOLD_CACHE.parent.mkdir(parents=True, exist_ok=True)
        H.FOLD_CACHE.write_text("nope")
        with self.assertRaises(RuntimeError):
            H.require_fold_evidence(H.FOLD_CACHE, list(self.cases))
        # wrong schema / incomplete coverage
        H.FOLD_CACHE.write_text(json.dumps({"schema": 1, "cases": 5,
                                            "folds": {}}))
        with self.assertRaises(RuntimeError):
            H.require_fold_evidence(H.FOLD_CACHE, list(self.cases))
        # a schema-1 recording created under NO experiment can be adopted
        # by acceptance only when the coverage is complete; here it is
        # empty, so build a fresh complete recording instead
        H.FOLD_CACHE.unlink()
        folds = H.explicit_assign_folds(self.cases, H.FOLD_CACHE,
                                        experiment="acceptance-test")
        ident = H.require_fold_evidence(H.FOLD_CACHE, list(self.cases))
        for c in self.cases:
            self.assertEqual(ident["folds"][H.case_key(c.text)],
                             folds[H.case_key(c.text)])


if __name__ == "__main__":
    unittest.main()
