#!/usr/bin/env python3
"""M3 prep: check for ModernBERT-base on disk; download if absent (USER-APPROVED)."""
import sys
from pathlib import Path

CANDIDATES = [
    Path("/Volumes/LLMs/mlx-community/ModernBERT-base"),
    Path("/Volumes/LLMs/ModernBERT-base"),
    Path("/Volumes/LLMs/answerdotai/ModernBERT-base"),
]
found = [c for c in CANDIDATES if c.exists()]
print("on-disk candidates:", [str(f) for f in found] or "NONE")

if not found:
    print("downloading answerdotai/ModernBERT-base -> /Volumes/LLMs/answerdotai/ModernBERT-base")
    from huggingface_hub import snapshot_download
    p = snapshot_download(
        "answerdotai/ModernBERT-base",
        local_dir="/Volumes/LLMs/answerdotai/ModernBERT-base",
    )
    print("downloaded to", p)
    found = [Path(p)]

target = found[0]
print("target:", target)
for f in sorted(target.iterdir()):
    print("  ", f.name)
