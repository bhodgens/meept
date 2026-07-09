#!/usr/bin/env python3
"""Find target modules for LoRA in LFM2.5.

Loads the LFM2.5-8B model and iterates over all named modules,
printing linear and conv layers that are candidates for LoRA targeting.
This helps discover the exact module names to pass to LoraConfig.target_modules.
"""

from transformers import AutoModelForCausalLM


def main():
    model = AutoModelForCausalLM.from_pretrained("LFM/LFM2.5-8B")

    print("=== Target Modules for LoRA ===")
    for name, module in model.named_modules():
        module_type = type(module).__name__
        if "linear" in module_type.lower() or "conv" in module_type.lower():
            print(f"{name}: {module_type}")


if __name__ == "__main__":
    main()
