#!/usr/bin/env python3
"""voice-gap-analyze.py — silence-gap / talk-over evidence for link recordings.

Input: one or TWO raw s16le mono 16 kHz streams (the .raw files written by
`meept voice link --monitor --record DIR`; they carry EXACTLY the bytes handed
to playback, so the gaps here are the gaps the operator HEARD).

Per file:
  - total audio seconds
  - table of SILENCE gaps >= 200 ms: silence = RMS below -45 dBFS measured on
    non-overlapping 20 ms windows; contiguous silent windows merge into one gap
  - summary line: GAPS>=200ms: n, GAPS>=1000ms: m

Two files additionally get OVERLAP analysis: windows where BOTH sides are
non-silent (> -45 dBFS) — the talk-over metric (max run + total seconds).

Stdlib only (array/struct), no numpy dependency.
"""

import argparse
import array
import math
import sys
from pathlib import Path

SAMPLE_RATE = 16000
WINDOW_S = 0.020                      # 20 ms analysis window
WINDOW_SAMPLES = int(SAMPLE_RATE * WINDOW_S)  # 320 samples = 640 bytes
SILENCE_DBFS = -45.0
FULL_SCALE = 32768.0
RMS_THRESHOLD = FULL_SCALE * (10.0 ** (SILENCE_DBFS / 20.0))
MIN_GAP_S = 0.200
SEVERE_GAP_S = 1.000


def load_windows(path: Path):
    """Return (list[bool] non_silent per 20 ms window, total_seconds)."""
    data = path.read_bytes()
    if len(data) % 2:
        # odd byte count = truncated write; drop the dangling half-sample
        data = data[:-1]
    samples = array.array("h")
    samples.frombytes(data)
    if sys.byteorder != "little":
        samples.byteswap()
    n = len(samples)
    total_s = n / SAMPLE_RATE
    non_silent = []
    for start in range(0, n, WINDOW_SAMPLES):
        chunk = samples[start:start + WINDOW_SAMPLES]
        if not chunk:
            continue
        acc = 0
        for s in chunk:
            acc += s * s
        rms = math.sqrt(acc / len(chunk))
        non_silent.append(rms > RMS_THRESHOLD)
    return non_silent, total_s


def find_gaps(flags):
    """Contiguous silent runs (start_s, end_s, dur_s), >= MIN_GAP_S only."""
    gaps = []
    run_start = None
    for i, ns in enumerate(flags):
        if not ns:
            if run_start is None:
                run_start = i
        elif run_start is not None:
            gaps.append((run_start, i))
            run_start = None
    if run_start is not None:
        gaps.append((run_start, len(flags)))
    out = []
    for a, b in gaps:
        dur = (b - a) * WINDOW_S
        if dur >= MIN_GAP_S:
            out.append((a * WINDOW_S, b * WINDOW_S, dur))
    return out


def report_one(name, flags, total_s):
    print(f"== {name} ==")
    print(f"total audio: {total_s:.2f} s ({len(flags)} x {WINDOW_S * 1000:.0f} ms windows)")
    gaps = find_gaps(flags)
    n200 = len(gaps)
    n1000 = sum(1 for _, _, d in gaps if d >= SEVERE_GAP_S)
    if gaps:
        print(f"{'start_s':>9}  {'end_s':>9}  {'dur_ms':>7}")
        for s, e, d in gaps:
            print(f"{s:9.2f}  {e:9.2f}  {d * 1000:7.0f}")
    else:
        print("(no silence gaps >= 200 ms)")
    print(f"GAPS>=200ms: {n200}, GAPS>=1000ms: {n1000}")
    print()
    return flags


def report_overlap(flags_a, flags_b):
    n = min(len(flags_a), len(flags_b))
    both = [flags_a[i] and flags_b[i] for i in range(n)]
    total_windows = sum(both)
    max_run = 0
    run = 0
    for v in both:
        run = run + 1 if v else 0
        if run > max_run:
            max_run = run
    print("== OVERLAP (a vs b) ==")
    print(f"talk-over total: {total_windows * WINDOW_S:.2f} s "
          f"({total_windows} windows both non-silent)")
    print(f"talk-over max run: {max_run * WINDOW_S:.2f} s")


def main():
    ap = argparse.ArgumentParser(description="silence-gap / talk-over analysis of link .raw recordings")
    ap.add_argument("raw", nargs="+", type=Path,
                    help="one or two .raw files (s16le mono 16 kHz)")
    args = ap.parse_args()
    if len(args.raw) > 2:
        ap.error("pass one or two .raw paths")

    parsed = []
    for p in args.raw:
        if not p.is_file():
            print(f"error: not a file: {p}", file=sys.stderr)
            return 1
        flags, total_s = load_windows(p)
        parsed.append((p, flags, total_s))

    for p, flags, total_s in parsed:
        report_one(str(p), flags, total_s)

    if len(parsed) == 2:
        report_overlap(parsed[0][1], parsed[1][1])
    return 0


if __name__ == "__main__":
    sys.exit(main())
