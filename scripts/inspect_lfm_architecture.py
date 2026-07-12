#!/usr/bin/env python3
"""Find target modules for LoRA in LFM2.5.

Loads an LFM2.5 model and iterates over all named modules,
printing linear and conv layers that are candidates for LoRA targeting.
This helps discover the exact module names to pass to LoraConfig.target_modules.

Usage:
    python scripts/inspect_lfm_architecture.py
    python scripts/inspect_lfm_architecture.py --model LiquidAI/LFM2.5-1.2B-Instruct
"""

from __future__ import annotations

import argparse

from transformers import AutoModelForCausalLM


def main() -> None:
    parser = argparse.ArgumentParser(description="Inspect LFM2.5 modules for LoRA")
    parser.add_argument(
        "--model",
        default="LiquidAI/LFM2.5-1.2B-Instruct",
        help="Hugging Face model id (default: LiquidAI/LFM2.5-1.2B-Instruct)",
    )
    args = parser.parse_args()

    print(f"Loading {args.model} ...")
    model = AutoModelForCausalLM.from_pretrained(args.model, trust_remote_code=True)

    print("=== Target Modules for LoRA ===")
    for name, module in model.named_modules():
        module_type = type(module).__name__
        if "linear" in module_type.lower() or "conv" in module_type.lower():
            print(f"{name}: {module_type}")


if __name__ == "__main__":
    main()
