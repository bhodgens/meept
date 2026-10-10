#!/usr/bin/env python3
"""Convert `go test -v` (or `go test -json`) output into test-efficacy records.

Reads one `go test` run's output on stdin and appends one JSONL line per test
function to the history file:

  $MEEPT_HOME/test-history/results.jsonl  (or ./test-history/ without it)

Record shape:
  {"type":"test","run":"<utc ts>","test":"<pkg>.<TestName>",
   "result":"pass|fail|skip","ms":<int>,"output":"<failure text, truncated>"}

Subtests (TestFoo/sub) are folded into their parent: the parent result is what
a regression team cares about, and folding keeps flake math per test function.

Output capture: go test prints a failing test's t.Errorf/diagnostic lines when
they execute — often BEFORE the `--- FAIL` marker (40+ lines earlier for
output-heavy tests). Diagnostic lines between `=== RUN <name>` and a result
marker are therefore buffered against the currently-running test and attached
to its record whatever the result. `=== NAME` / `=== PAUSE` / `=== CONT`
markers and slog/log lines are captured the same way; blank lines are ignored.

Usage:
  go test -p 2 ./... -short -v | python3 scripts/test-record.py
  go test -p 2 ./... -json        | python3 scripts/test-record.py
"""

from __future__ import annotations

import datetime
import json
import os
import re
import sys
from pathlib import Path

RE_RUN = re.compile(r"^=== RUN\s+(\S+)")
RE_NAME = re.compile(r"^=== NAME\s+(\S+)")
RE_PAUSE = re.compile(r"^=== PAUSE\s+(\S+)")
RE_CONT = re.compile(r"^=== CONT\s+(\S+)")
RE_RESULT = re.compile(r"--- (PASS|FAIL|SKIP):\s+(\S+)\s+\((\d+(?:\.\d+)?)s\)")
RE_PKG_LINE = re.compile(r"^(?:ok|FAIL|\?|---)\s+(\S+)")

MAX_OUTPUT_CHARS = 2000


def history_file() -> Path:
    home = os.environ.get("MEEPT_HOME")
    d = (Path(home) / "test-history") if home else Path("test-history")
    d.mkdir(parents=True, exist_ok=True)
    return d / "results.jsonl"


def top_level(name: str) -> str:
    """TestFoo/sub_test_case -> TestFoo (fold subtests into the parent)."""
    return name.split("/")[0]


class TextParser:
    """Plain `go test -v` text.

    go test prints a package's summary (`ok <pkg>` / `FAIL <pkg>`) AFTER its
    test lines, so results are buffered and attributed to the package when
    the summary line arrives. Diagnostic lines are buffered against the
    currently-running test (the most recent `=== RUN`) and attached to its
    record when the result marker arrives — order in the log is not
    test-order under package parallelism, but within one package's block
    go test interleaves strictly: RUN, diagnostics, RESULT.
    """

    def __init__(self) -> None:
        self.records: dict[str, dict] = {}  # full name -> record
        self.pending: list[dict] = []  # results awaiting a package summary
        self.diagnostics: dict[str, list[str]] = {}  # test name -> lines
        self.running: str | None = None  # most recent === RUN name

    def feed(self, line: str) -> None:
        m = RE_RESULT.search(line)
        if m:
            result, name, secs = m.group(1).lower(), m.group(2), float(m.group(3))
            out = "".join(self.diagnostics.pop(name, []))
            self.pending.append({"test": top_level(name), "result": result,
                                 "ms": int(secs * 1000), "output": out})
            if self.running == name:
                self.running = None
            return

        m = RE_RUN.match(line) or RE_NAME.match(line) \
            or RE_CONT.match(line) or RE_PAUSE.match(line)
        if m:
            self.running = m.group(1)
            return

        m = RE_PKG_LINE.match(line)
        if m:
            candidate = m.group(1)
            # package summaries carry a module path (contain a dot);
            # `---`-prefixed and bracketed group lines are not packages
            if not candidate.startswith(("[", "---")) and "." in candidate:
                for rec in self.pending:
                    rec["test"] = f"{candidate}.{rec['test']}"
                    self.records[rec["test"]] = rec
                self.pending = []
                self.running = None
            return

        if line and self.running:
            # diagnostic / log line for the test that most recent === RUN named
            self.diagnostics.setdefault(self.running, []).append(line)

    def finish(self) -> dict[str, dict]:
        for rec in self.pending:
            rec.setdefault("output", "")
            self.records.setdefault(f"<unknown-pkg>.{rec['test']}", rec)
        self.pending = []
        return self.records


class JsonParser:
    """`go test -json` event stream. Package is on every event, so no
    buffering gymnastics — diagnostics are captured per running test."""

    def __init__(self) -> None:
        self.records: dict[str, dict] = {}
        self.diagnostics: dict[str, list[str]] = {}  # pkg.Test -> lines

    def feed(self, line: str) -> None:
        try:
            ev = json.loads(line)
        except json.JSONDecodeError:
            return
        pkg, name, action = ev.get("Package", ""), ev.get("Test"), ev.get("Action")
        if name is None:
            # package-level output: capture compile failures under a sentinel
            if action == "output" and pkg and ev.get("Output", "").startswith(("# ", "build failed")):
                key = f"{pkg}.<compile-fail>"
                rec = self.records.setdefault(key, {
                    "test": key, "result": "fail", "ms": 0, "output": ""})
                out = ev.get("Output", "")
                rec["output"] += out if isinstance(out, str) else "".join(out)
            return
        key = f"{pkg}.{top_level(name)}" if pkg else top_level(name)
        if action == "output":
            out = ev.get("Output", "")
            if isinstance(out, list):
                out = "".join(out)
            if RE_RESULT.search(out):
                return  # echoed markers are handled by their own events
            if out.strip():
                self.diagnostics.setdefault(key, []).append(out)
            return
        if action in ("pass", "fail", "skip"):
            parent = top_level(name)
            if parent != name:
                # subtest terminal event: the parent's own event carries the
                # authoritative elapsed time; do not overwrite it
                if f"{pkg}.{parent}" in self.records if pkg else parent in self.records:
                    return
                full = key
            else:
                full = key
            ms = int(float(ev.get("Elapsed", 0) or 0) * 1000)
            self.records[full] = {"test": full, "result": action, "ms": ms,
                                  "output": ""}
            if action == "fail":
                # the parent record exists by now unless the subtest failed
                # first — diagnostics follow in output events, but in -json
                # mode output events PRECEDE the terminal event, so attach
                self.records[full]["output"] = "".join(
                    self.diagnostics.pop(key, []))

    def finish(self) -> dict[str, dict]:
        # attach any leftover diagnostics (test panicked, no terminal event)
        for key, lines in self.diagnostics.items():
            if key in self.records and not self.records[key]["output"]:
                self.records[key]["output"] = "".join(lines)
        return self.records


def main() -> int:
    run_id = datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    parser = None
    for raw in sys.stdin:
        line = raw.rstrip("\n")
        if parser is None:
            if line.startswith("{"):
                parser = JsonParser()
            elif line.strip():
                parser = TextParser()
        if parser is not None:
            parser.feed(line)

    tests = parser.finish() if parser else {}
    out = history_file()
    with out.open("a") as fh:
        for rec in sorted(tests.values(), key=lambda r: r["test"]):
            rec.update(type="test", run=run_id)
            rec["output"] = rec["output"].strip()[:MAX_OUTPUT_CHARS]
            fh.write(json.dumps(rec) + "\n")

    n_fail = sum(1 for r in tests.values() if r["result"] == "fail")
    print(f"recorded {len(tests)} test result(s) ({n_fail} fail) for run {run_id} -> {out}",
          file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
