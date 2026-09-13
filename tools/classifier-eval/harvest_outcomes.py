#!/usr/bin/env python3
"""harvest_outcomes.py -- nightly harvest of dispatch_log outcomes into
corpus candidates + per-door accuracy views (classifier-outcome-loop,
leaf 04).

READ-ONLY on metrics.db (opened with file:...?mode=ro). The only write
path is --apply-views (CREATE VIEW IF NOT EXISTS, idempotent), which the
operator must point at a COPY unless they accept view objects in the
live store.

Outputs (default dir: ./harvest-YYYYMMDD/ under tools/classifier-eval/):
  candidates.json   corrected/failed_replan rows, hashes only (SAFE: tracked OK)
  nearmiss.json     Door-1 margins inside the band, routed + abstained (SAFE)
  sheet.local.md    adjudication sheet with text recovered from
                    ~/.hermes/sessions (NEVER tracked; dir gitignored
                    BEFORE first write)
  views.sql         the exact SQL --apply-views applies

Privacy invariants (design.md S4):
  - metrics.db contains no message text; this tool never copies any in
  - verbatim text lands only in sheet.local.md inside the gitignored
    output dir; candidates.json/nearmiss.json carry hashes only
  - the text join runs on the same machine as the daemon and reads
    only ~/.hermes/sessions/session_*.json

Stdlib only. ASCII only. Python 3.14 (/opt/homebrew/bin/python3.14).
"""
from __future__ import annotations

import argparse
import json
import re
import sqlite3
import sys
from datetime import datetime, timedelta, timezone
from pathlib import Path

TOOLS_DIR = Path(__file__).resolve().parent
SESS_DIR = Path.home() / ".hermes" / "sessions"

VIEWS_SQL = """\
-- Per-door accuracy (classifier-outcome-loop leaf 04).
-- Denominator rule: exclude pending rows and rows with no classifier
-- method (blank classifier_method = unattributed door traffic).
CREATE VIEW IF NOT EXISTS v_door_accuracy AS
SELECT
    classifier_method,
    COUNT(*)                                          AS total,
    SUM(CASE WHEN outcome = 'ok' THEN 1 ELSE 0 END)   AS ok,
    SUM(CASE WHEN outcome = 'corrected' THEN 1 ELSE 0 END) AS corrected,
    SUM(CASE WHEN outcome = 'failed_replan' THEN 1 ELSE 0 END) AS failed_replan,
    ROUND(1.0 * SUM(CASE WHEN outcome = 'ok' THEN 1 ELSE 0 END) / COUNT(*), 4) AS ok_rate
FROM dispatch_log
WHERE outcome != 'pending' AND classifier_method != ''
GROUP BY classifier_method;

-- Per-session correction rate over classified rows.
CREATE VIEW IF NOT EXISTS v_correction_rate AS
SELECT
    session_id,
    COUNT(*)                                          AS total_classified,
    SUM(CASE WHEN outcome = 'corrected' THEN 1 ELSE 0 END) AS corrections,
    ROUND(1.0 * SUM(CASE WHEN outcome = 'corrected' THEN 1 ELSE 0 END)
          / COUNT(*), 4)                              AS correction_rate
FROM dispatch_log
WHERE outcome != 'pending' AND classifier_method != '' AND session_id != ''
GROUP BY session_id;

-- Door-1 margin histogram, CASE buckets 0.00-0.10 in 0.01 steps plus a
-- >=0.10 catch-all, split by verdict (routed / abstain).
-- Verdict is RECONSTRUCTED from what the row stores: margin IS NOT NULL
-- marks a Door-1 observation; classifier_method = 'embedding_prefilter'
-- marks the routed subset; every other row with a margin is an abstain.
-- There is deliberately NO 'suppressed' category. Suppression (tfidf-veto
-- disagreement, quickplan cue guard, H6 gate) returns nil from the
-- prefilter and the row is persisted with the chain's method and no
-- verdict, so a suppressed row is indistinguishable from an ordinary
-- abstain at the DB layer: internal/agent/embedding_prefilter.go:448-452
-- and :466-471 emit PrefilterVerdict{Suppressed:true}, but
-- internal/agent/dispatcher.go:3564-3589 persists only v.Margin -- the
-- flag is dropped. A 'suppressed' bucket would be structurally empty, so
-- it is not offered (F27). Making it real means persisting the verdict:
-- add a prefilter_verdict column to dispatch_log and write it from the
-- stashed PrefilterVerdict at dispatcher.go:3565-3589 (that write site is
-- dispatcher.go, not internal/metrics).
CREATE VIEW IF NOT EXISTS v_margin_hist AS
SELECT
    CASE
        WHEN margin < 0.01 THEN '0.00-0.01'
        WHEN margin < 0.02 THEN '0.01-0.02'
        WHEN margin < 0.03 THEN '0.02-0.03'
        WHEN margin < 0.04 THEN '0.03-0.04'
        WHEN margin < 0.05 THEN '0.04-0.05'
        WHEN margin < 0.06 THEN '0.05-0.06'
        WHEN margin < 0.07 THEN '0.06-0.07'
        WHEN margin < 0.08 THEN '0.07-0.08'
        WHEN margin < 0.09 THEN '0.08-0.09'
        WHEN margin < 0.10 THEN '0.09-0.10'
        ELSE '0.10+'
    END                                               AS margin_bucket,
    CASE
        WHEN classifier_method = 'embedding_prefilter'
             AND intent_type != ''                    THEN 'routed'
        ELSE 'abstain'
    END                                               AS verdict,
    COUNT(*)                                          AS n
FROM dispatch_log
WHERE margin IS NOT NULL
GROUP BY margin_bucket, verdict
ORDER BY margin_bucket, verdict;

-- Fallback traffic per day (routing miss rate over time).
-- classifier_method = 'fallback' is written by exactly one production
-- path: classifyIntent's Step-5 final fallback, whose returned Intent
-- carries Method: "fallback" (internal/agent/dispatcher.go, the literal
-- inside the block opened by the "// Step 5: Final fallback" comment;
-- line numbers drift with every edit above them -- it was :1474 at HEAD
-- ee1580a4 and :1484 during the 2026-09-13 fix wave, so anchor on the
-- comment/literal, not on a line range). It flows through
-- DispatchResult.Intent.Method into recordDispatch, where
-- `classifierMethod = result.Intent.Method` reads it (previously cited
-- here as :3496/:3579 -- both stale).
-- Sibling miss paths write distinct methods ("heuristic_fallback",
-- "llm_empty_fallback_chat"), so this counts only the terminal
-- all-classifiers-failed fallback -- it is not structurally empty.
CREATE VIEW IF NOT EXISTS v_fallback_trend AS
SELECT
    substr(timestamp, 1, 10)                          AS day,
    COUNT(*)                                          AS fallbacks
FROM dispatch_log
WHERE classifier_method = 'fallback'
GROUP BY day
ORDER BY day;
"""


def ro_connect(path: Path) -> sqlite3.Connection:
    uri = f"file:{path}?mode=ro"
    conn = sqlite3.connect(uri, uri=True)
    conn.row_factory = sqlite3.Row
    return conn


REQUIRED_COLS = {
    "input_hash", "model", "margin", "turn_no", "outcome",
    "corrected_agent",
}


def dispatch_cols(conn: sqlite3.Connection) -> set[str]:
    return {r[1] for r in conn.execute("PRAGMA table_info(dispatch_log)")}


def parse_iso(s: str) -> str:
    """Normalize a user ISO bound to 'YYYY-MM-DD HH:MM:SS' (UTC store
    convention: strftime('%Y-%m-%dT%H:%M:%SZ'))."""
    s = s.strip().replace("T", " ").rstrip("Z")
    dt = datetime.fromisoformat(s)
    if dt.tzinfo is not None:
        dt = dt.astimezone(timezone.utc).replace(tzinfo=None)
    return dt.strftime("%Y-%m-%d %H:%M:%S")


def q_candidates(conn: sqlite3.Connection, since: str) -> list[dict]:
    rows = conn.execute(
        """
        SELECT input_hash, session_id, timestamp AS ts, classifier_method,
               intent_type, agent_id, corrected_agent, confidence
        FROM dispatch_log
        WHERE outcome IN ('corrected', 'failed_replan') AND timestamp >= ?
        ORDER BY session_id, ts
        """,
        (since,),
    ).fetchall()
    return [dict(r) for r in rows]


def q_nearmiss(conn: sqlite3.Connection, since: str,
               lo: float, hi: float) -> list[dict]:
    rows = conn.execute(
        """
        SELECT input_hash, timestamp AS ts, margin,
               CASE
                   WHEN classifier_method = 'embedding_prefilter'
                        AND intent_type != '' THEN intent_type
                   ELSE ''
               END AS asserted_intent,
               -- No 'suppressed' verdict here: the persisted row drops the
               -- PrefilterVerdict.Suppressed flag, so a suppressed row is
               -- indistinguishable from an abstain (see the v_margin_hist
               -- comment in VIEWS_SQL).
               CASE
                   WHEN classifier_method = 'embedding_prefilter'
                        AND intent_type != '' THEN 'routed'
                   ELSE 'abstain'
               END AS verdict
        FROM dispatch_log
        WHERE margin IS NOT NULL AND margin BETWEEN ? AND ?
          AND timestamp >= ?
        ORDER BY ts
        """,
        (lo, hi, since),
    ).fetchall()
    return [dict(r) for r in rows]


def load_sessions() -> list[dict]:
    """Scan ~/.hermes/sessions/session_*.json (harvest_hermes.py glob).
    Each entry: {path, session_id, msgs:[(ts_str, text)] for user msgs}."""
    out = []
    if not SESS_DIR.is_dir():
        return out
    for p in sorted(SESS_DIR.glob("session_*.json")):
        try:
            d = json.loads(p.read_text(encoding="utf-8", errors="replace"))
        except Exception:
            continue
        sid = str(d.get("session_id") or p.stem)
        msgs = []
        for m in d.get("messages") or []:
            if not isinstance(m, dict) or m.get("role") != "user":
                continue
            c = m.get("content")
            if isinstance(c, list):  # content blocks: join text parts
                c = " ".join(b.get("text", "") for b in c
                             if isinstance(b, dict))
            if not isinstance(c, str):
                continue
            t = c.strip()
            if not t:
                continue
            msgs.append((str(m.get("timestamp") or ""), t))
        if msgs:
            out.append({"path": p, "session_id": sid, "msgs": msgs})
    return out


def _norm_ts(ts: str) -> float:
    s = ts.strip().replace("T", " ").rstrip("Z")
    try:
        return datetime.fromisoformat(s).timestamp()
    except ValueError:
        return 0.0


def nearest_user_text(sessions: list[dict], session_id: str,
                      ts: str) -> str | None:
    """Nearest-timestamp user message for (session_id, ts). Matches on the
    transcript's session_id field OR its filename stem (fallback for
    transcripts whose stored id differs from the file name)."""
    norm_sid = session_id.strip()
    cands = [s for s in sessions
             if s["session_id"] == norm_sid
             or s["path"].stem == norm_sid
             or s["path"].stem.endswith("_" + norm_sid)
             or norm_sid.endswith("_" + s["path"].stem.split("_", 2)[2]
                                 if "_" in s["path"].stem else "")]
    if not cands:
        # last resort: match any session whose messages embed the id
        # is skipped for speed; report unmatched instead
        return None
    want = _norm_ts(ts)
    best, best_delta = None, None
    for s in cands:
        for mts, text in s["msgs"]:
            delta = abs(_norm_ts(mts) - want) if want else 1e18
            if best_delta is None or delta < best_delta:
                best, best_delta = text, delta
    if best is None:
        # session found but no user messages / no timestamps: first msg
        for s in cands:
            if s["msgs"]:
                return s["msgs"][0][1]
        return None
    return best


def sheet_text(cand: dict, text: str | None) -> str:
    lines = [
        f"### {cand['input_hash']}  ({cand['ts']}  session={cand['session_id']})",
        f"- system routed: method=`{cand['method']}` intent=`{cand['intent']}` "
        f"agent=`{cand['agent']}` conf={cand['confidence']:.3f}",
        f"- outcome: `{cand['outcome']}`"
        + (f" corrected_agent=`{cand['corrected_agent']}`"
           if cand["corrected_agent"] else ""),
    ]
    if text is not None:
        for ln in text.splitlines() or [""]:
            lines.append(f"> {ln}")
    else:
        lines.append("> [text not recovered -- session transcript not found "
                     "under ~/.hermes/sessions]")
    lines.append("**your label:** ``")
    return "\n".join(lines)


def one_line(text: str, width: int = 72) -> str:
    return re.sub(r"\s+", " ", text).strip()[:width]


def main() -> int:
    ap = argparse.ArgumentParser(
        description="Harvest dispatch_log outcomes into corpus candidates "
                    "+ accuracy views (READ-ONLY on metrics.db).")
    ap.add_argument("--db", default=str(Path.home() / ".meept" / "metrics.db"),
                    help="metrics.db path (opened READ-ONLY)")
    ap.add_argument("--since", default=None,
                    help="ISO lower bound; default 24h back")
    ap.add_argument("--window", type=int, default=3,
                    help="re-route window (informational; matches L3)")
    ap.add_argument("--margin-band", default="0.025,0.035",
                    help="near-miss band lo,hi")
    ap.add_argument("--dry-run", action="store_true",
                    help="print counts + hash-only preview; write NOTHING")
    ap.add_argument("--apply-views", action="store_true",
                    help="apply views.sql (CREATE VIEW IF NOT EXISTS) to "
                         "the db -- only on a COPY in normal use")
    ap.add_argument("--out", default=None,
                    help="output dir (default "
                         "tools/classifier-eval/harvest-YYYYMMDD/)")
    args = ap.parse_args()

    db_path = Path(args.db).expanduser()
    if not db_path.is_file():
        print(f"metrics.db not found: {db_path}", file=sys.stderr)
        return 0  # measurement-tool convention: exit 0 always

    since = (parse_iso(args.since) if args.since else
             (datetime.now(timezone.utc) - timedelta(hours=24)).strftime(
                 "%Y-%m-%d %H:%M:%S"))
    lo, hi = (float(x) for x in args.margin_band.split(","))

    conn = ro_connect(db_path)
    missing = REQUIRED_COLS - dispatch_cols(conn)
    legacy = bool(missing)
    if legacy:
        # Pre-L1 schema (old daemon binary): no outcome-loop columns.
        # Ship value with whatever rows exist -- empty result sets are a
        # fine first output (leaf 04 Notes).
        print("NOTE: dispatch_log lacks outcome-loop columns "
              f"({', '.join(sorted(missing))}); the running daemon "
              "predates leaves 01-03. Counts will be 0.", file=sys.stderr)
        cands: list[dict] = []
        near: list[dict] = []
    else:
        cands = q_candidates(conn, since)
        for c in cands:  # spec field names
            c["method"] = c.pop("classifier_method")
            c["intent"] = c.pop("intent_type")
            c["agent"] = c.pop("agent_id")
            c["outcome"] = "corrected"  # candidates are corrective rows
        near = q_nearmiss(conn, since, lo, hi)
    n_views = len(re.findall(r"CREATE VIEW", VIEWS_SQL))

    # --apply-views: the ONLY write path. Executes VIEWS_SQL (CREATE VIEW
    # IF NOT EXISTS -- idempotent) through a separate read-write handle.
    # Everything else in this tool is strictly read-only (mode=ro).
    if args.apply_views:
        if legacy:
            print("--apply-views skipped: dispatch_log lacks outcome-loop "
                  "columns; run leaves 01-03 migration first.",
                  file=sys.stderr)
        else:
            view_names = re.findall(r"CREATE VIEW IF NOT EXISTS (\w+)",
                                    VIEWS_SQL)
            wconn = sqlite3.connect(str(db_path))
            try:
                wconn.executescript(VIEWS_SQL)
                wconn.commit()
            finally:
                wconn.close()
            print(f"applied views to {db_path}: {', '.join(view_names)}")

    conn.close()

    # ---- summary ------------------------------------------------------
    print(f"db: {db_path} (read-only)")
    print(f"since: {since}  window: {args.window}  margin-band: [{lo}, {hi}]")
    print(f"candidates (corrected/failed_replan): {len(cands)}")
    print(f"near-miss margins in band: {len(near)}")
    print(f"views WOULD be created by --apply-views: "
          f"{n_views} (v_door_accuracy, v_correction_rate, "
          f"v_margin_hist, v_fallback_trend)")

    if args.dry_run:
        print("\n-- dry run: candidate preview (hash + corrected_agent "
              "only, no text) --")
        for c in cands[:10]:
            print(f"  {c['input_hash'] or '(no hash)':18} "
                  f"{c['ts']}  {c['outcome']:13} "
                  f"{c['agent'] or '-'} -> {c['corrected_agent'] or '-'}")
        print(f"({min(len(cands), 10)} of {len(cands)} shown) "
              "wrote nothing anywhere")
        return 0

    # ---- outputs ------------------------------------------------------
    out_dir = (Path(args.out).expanduser() if args.out else
               TOOLS_DIR / f"harvest-{datetime.now():%Y%m%d}")
    out_dir.mkdir(parents=True, exist_ok=True)

    # PRIVACY: gitignore BEFORE any text file exists in the dir.
    gi = out_dir / ".gitignore"
    if not gi.exists():
        gi.write_text("*\n", encoding="ascii")
    print(f"gitignore written: {gi}")

    # candidates.json contract (leaf spec): hashes only, exact fields.
    # "outcome" is sheet-internal and stays out of the JSON.
    (out_dir / "candidates.json").write_text(
        json.dumps([{k: v for k, v in c.items() if k != "outcome"}
                    for c in cands], indent=1) + "\n", encoding="ascii")
    (out_dir / "nearmiss.json").write_text(
        json.dumps(near, indent=1) + "\n", encoding="ascii")
    (out_dir / "views.sql").write_text(VIEWS_SQL, encoding="ascii")

    # ---- local-only text join -----------------------------------------
    sessions = load_sessions() if cands else []
    blocks = [f"# Outcome Harvest Adjudication Sheet -- {datetime.now():%Y-%m-%d}",
              "",
              "For each candidate: confirm the corrected_agent (or supply "
              "the right intent/agent), or mark `abstain`.",
              "This file is gitignored; verbatim message text NEVER enters "
              "git (design.md S4).", ""]
    recovered = 0
    for c in cands:
        text = nearest_user_text(sessions, c["session_id"], c["ts"])
        if text is not None:
            recovered += 1
        blocks.append(sheet_text(c, text))
        blocks.append("")
    (out_dir / "sheet.local.md").write_text(
        "\n".join(blocks), encoding="utf-8", errors="replace")

    print(f"candidates.json: {len(cands)} rows (hashes only)")
    print(f"nearmiss.json:   {len(near)} rows")
    print(f"sheet.local.md:  {recovered}/{len(cands)} texts recovered from "
          f"{len(sessions)} session transcripts")
    print(f"views.sql written ({n_views} views; apply with --apply-views)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
