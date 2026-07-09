#!/usr/bin/env python3
"""Generate adapter registry config after training.

Scans ~/.meept/adapters/ for trained adapter directories, reads
training_meta.json from each (if present), and emits
~/.meept/adapter_registry.json with the full adapter list.

Usage:
    python scripts/generate_adapter_config.py
"""

import hashlib
import json
import sys
from datetime import datetime
from pathlib import Path


def get_dataset_md5(dataset_path):
    """Hash the training dataset for provenance."""
    try:
        with open(dataset_path, "rb") as f:
            return hashlib.md5(f.read()).hexdigest()
    except (FileNotFoundError, OSError):
        return ""


def scan_adapters(base_dir):
    """Scan adapter directory structure and return list of adapter entries."""
    adapters = []
    base = Path(base_dir)

    if not base.exists():
        return adapters

    for domain_dir in sorted(base.iterdir()):
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

            # Compute dataset MD5 for provenance
            dataset_path = Path.home() / ".meept" / "learning" / "datasets" / meta.get("dataset", f"{domain}.jsonl")

            adapters.append({
                "id": f"{domain}-{model_dir.name}",
                "domain": domain,
                "model": model_dir.name.split("-v")[0],
                "path": str(model_dir),
                "created_at": datetime.now().isoformat(),
                "training_md5": get_dataset_md5(dataset_path),
                "enabled": True,
            })

    return adapters


def main():
    base_dir = Path.home() / ".meept" / "adapters"

    if not base_dir.exists():
        print("No adapters directory found at {}".format(base_dir))
        sys.exit(0)

    adapters = scan_adapters(base_dir)

    registry = {
        "adapters": adapters,
        "version": 1,
        "generated_at": datetime.now().isoformat(),
    }

    output = Path.home() / ".meept" / "adapter_registry.json"
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(registry, indent=2))
    print(f"Generated adapter registry: {len(adapters)} adapters -> {output}")


if __name__ == "__main__":
    main()
