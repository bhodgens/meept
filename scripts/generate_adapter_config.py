#!/usr/bin/env python3
"""Generate adapter registry config after training.

Scans an adapters directory for trained adapter directories, reads
training_meta.json from each (if present), and emits adapter_registry.json.

Usage:
    python scripts/generate_adapter_config.py
    python scripts/generate_adapter_config.py \
        --adapters-dir ~/.meept/adapters \
        --output ~/.meept/adapter_registry.json
"""

from __future__ import annotations

import argparse
import hashlib
import json
import sys
from datetime import datetime
from pathlib import Path


def get_dataset_md5(dataset_path: Path) -> str:
    """Hash the training dataset for provenance."""
    try:
        with open(dataset_path, "rb") as f:
            return hashlib.md5(f.read()).hexdigest()
    except (FileNotFoundError, OSError):
        return ""


def scan_adapters(base_dir: Path, learning_datasets_dir: Path) -> list:
    """Scan adapter directory structure and return list of adapter entries."""
    adapters = []

    if not base_dir.exists():
        return adapters

    for domain_dir in sorted(base_dir.iterdir()):
        if not domain_dir.is_dir():
            continue
        domain = domain_dir.name

        for model_dir in sorted(domain_dir.iterdir()):
            if not model_dir.is_dir():
                continue

            # Read training metadata if available
            meta_file = model_dir / "training_meta.json"
            if meta_file.exists():
                try:
                    meta = json.loads(meta_file.read_text())
                except (json.JSONDecodeError, OSError):
                    meta = {"dataset": f"{domain}.jsonl"}
            else:
                meta = {"dataset": f"{domain}.jsonl"}

            # Prefer model from meta; fall back to directory name without -vN.
            model_id = meta.get("model")
            if not model_id:
                name = model_dir.name
                if "-v" in name:
                    model_id = name.rsplit("-v", 1)[0]
                else:
                    model_id = name

            dataset_name = meta.get("dataset", f"{domain}.jsonl")
            dataset_path = learning_datasets_dir / dataset_name

            # Prefer trained_at from meta for stable created_at across regen.
            created_at = meta.get("trained_at") or datetime.now().isoformat()

            adapters.append({
                "id": f"{domain}-{model_dir.name}",
                "domain": domain,
                "model": model_id,
                "path": str(model_dir.resolve()),
                "created_at": created_at,
                "training_md5": get_dataset_md5(dataset_path),
                "enabled": True,
            })

    return adapters


def main() -> int:
    home = Path.home()
    default_adapters = home / ".meept" / "adapters"
    default_learning = home / ".meept" / "learning" / "datasets"

    parser = argparse.ArgumentParser(description="Generate adapter registry JSON")
    parser.add_argument(
        "--adapters-dir",
        type=Path,
        default=default_adapters,
        help="Root directory of trained adapters (default: ~/.meept/adapters)",
    )
    parser.add_argument(
        "--output",
        type=Path,
        default=None,
        help="Output registry path (default: <parent of adapters-dir>/adapter_registry.json)",
    )
    parser.add_argument(
        "--datasets-dir",
        type=Path,
        default=default_learning,
        help="Domain datasets dir for MD5 provenance (default: ~/.meept/learning/datasets)",
    )
    args = parser.parse_args()

    adapters_dir = args.adapters_dir.expanduser()
    datasets_dir = args.datasets_dir.expanduser()
    if args.output is not None:
        output = args.output.expanduser()
    else:
        # Match daemon resolution: registry lives next to the adapters parent.
        # ~/.meept/adapters -> ~/.meept/adapter_registry.json
        output = adapters_dir.parent / "adapter_registry.json"

    if not adapters_dir.exists():
        print(f"No adapters directory found at {adapters_dir}")
        return 0

    adapters = scan_adapters(adapters_dir, datasets_dir)

    registry = {
        "adapters": adapters,
        "version": 1,
        "generated_at": datetime.now().isoformat(),
    }

    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(registry, indent=2))
    print(f"Generated adapter registry: {len(adapters)} adapters -> {output}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
