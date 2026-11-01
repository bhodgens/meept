"""freeze.py - threshold freeze + training manifest (jev-command-gate leaf
03, T3).

The TEACHER sweep on the held-out replay corpus picks the operating
threshold (research methodology, docs/research/jev-command-gate-calibration.md
section 1.3: cost-ratio closed form puts the optimum in the high 0.85-0.90
band; section 3.2: tune on the validation/held-out slice, freeze, NEVER
re-tune on the student). freeze.py:

  1. sweeps teacher scores over candidate thresholds on the HELD-OUT fold,
  2. picks the threshold by the frozen selection rule (below),
  3. writes it into config/jev-command-gate.contract.json5 (surgical edit,
     status -> "frozen"),
  4. hashes the contract file (sha256) into tools/jev-command-gate-eval/
     manifest.json.

SELECTION RULE (frozen): among candidate thresholds on the 0.50-0.90 grid,
choose the one maximizing deny recall, breaking ties toward HIGHER precision,
then toward the HIGHER threshold (conservative: at equal performance, demand
more certainty before auto-allowing; research sections 1.2/1.3). Candidates
are restricted to the research-recommended 0.85-0.90 band FIRST; only when
no candidate in the band achieves the maximum attainable recall does the
grid widen (documented deviation guard: the band is the default, the sweep
is the evidence).

--check mode (the T3 verify command): verifies the contract threshold is
frozen (status == "frozen") and the manifest sha256 matches the CURRENT
contract file bytes.
"""

import argparse
import hashlib
import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import contract_io  # noqa: E402
import replay as replay_mod  # noqa: E402

REPO_ROOT = os.path.dirname(os.path.dirname(os.path.dirname(
    os.path.abspath(__file__))))
CONTRACT_PATH = os.path.join(
    REPO_ROOT, "config", "jev-command-gate.contract.json5")
MANIFEST_PATH = os.path.join(
    os.path.dirname(os.path.abspath(__file__)), "manifest.json")

PREFERRED_BAND = (0.85, 0.90)


def select_threshold(sweep_points):
    """Frozen selection rule over score_threshold_sweep points. Recall is
    the primary (safety); ties break to precision, then to the higher
    threshold (more conservative allow)."""
    if not sweep_points:
        raise SystemExit("threshold sweep produced no points")

    def key(p):
        recall = p["deny_recall"]
        precision = p["deny_precision"]
        return (
            1 if recall is None else recall,
            0 if precision is None else precision,
            p["threshold"],
        )

    best_recall = max((p["deny_recall"] or 0.0) for p in sweep_points)
    in_band = [p for p in sweep_points
               if PREFERRED_BAND[0] <= p["threshold"] <= PREFERRED_BAND[1]]
    candidates = in_band if in_band else sweep_points
    band_best = max((p["deny_recall"] or 0.0) for p in candidates)
    if band_best < best_recall:
        # Documented deviation: the preferred band cannot reach the best
        # attainable recall; widen with the reason recorded by the caller.
        candidates = sweep_points
    return max(candidates, key=key)["threshold"]


def run_freeze(records, scorer, fold_of=None):
    """Full freeze: sweep -> select -> write contract -> write manifest.
    Returns the C7-adjacent freeze summary dict."""
    gate_fold, heldout = replay_mod.fold_partition(
        records, fold_of or (lambda rec: 0 if rec["id"].endswith("1") else 1))
    # The sweep tunes on the held-out slice ONLY (R0 section 3.2); the
    # gate fold is carried into the manifest for provenance.
    sweep = replay_mod.score_threshold_sweep(heldout, scorer)
    threshold = select_threshold(sweep)
    digest = contract_io.freeze_threshold(CONTRACT_PATH, threshold)
    manifest = {
        "contract": "jev-command-gate",
        "contract_sha256": digest,
        "frozen_threshold": threshold,
        "frozen_by": "03-eval",
        "selection_rule": "max deny_recall; ties: deny_precision, then higher threshold",
        "sweep": sweep,
        "fold_sizes": {"gate": len(gate_fold), "heldout": len(heldout)},
    }
    with open(MANIFEST_PATH, "w", encoding="utf-8") as fh:
        json.dump(manifest, fh, indent=2, sort_keys=True)
        fh.write("\n")
    manifest["band_deviation"] = not (
        PREFERRED_BAND[0] <= threshold <= PREFERRED_BAND[1])
    return manifest


def check():
    """--check: contract frozen + manifest hash matches the live file."""
    ok = True
    if not os.path.exists(CONTRACT_PATH):
        print("FAIL: contract file missing: %s" % CONTRACT_PATH)
        return 1
    if not os.path.exists(MANIFEST_PATH):
        print("FAIL: training manifest missing: %s" % MANIFEST_PATH)
        return 1
    contract = contract_io.load_contract(CONTRACT_PATH)
    gate = contract.get("gate_threshold", {})
    status = gate.get("status")
    value = gate.get("value")
    if status != "frozen":
        print("FAIL: gate_threshold.status is %r, want \"frozen\"" % status)
        ok = False
    else:
        print("ok: gate_threshold frozen at %s" % value)
    live_digest = contract_io.sha256_file(CONTRACT_PATH)
    manifest = json.load(open(MANIFEST_PATH, "r", encoding="utf-8"))
    recorded = manifest.get("contract_sha256")
    if recorded != live_digest:
        print("FAIL: manifest sha256 %s != live contract sha256 %s"
              % (recorded, live_digest))
        ok = False
    else:
        print("ok: manifest sha256 matches contract (%s)" % live_digest[:16] + "...")
    if manifest.get("frozen_threshold") != value:
        print("FAIL: manifest frozen_threshold %r != contract value %r"
              % (manifest.get("frozen_threshold"), value))
        ok = False
    else:
        print("ok: manifest frozen_threshold matches contract value (%s)" % value)
    return 0 if ok else 1


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true",
                        help="verify frozen flag + manifest hash")
    args = parser.parse_args(argv)
    if args.check:
        return check()
    parser.print_help()
    return 2


if __name__ == "__main__":
    sys.exit(main())
