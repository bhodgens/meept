#!/usr/bin/env python3
"""Train LoRA adapter for LFM2.5.

Usage:
    python scripts/train_lora.py \
        --model lfm2.5-8b \
        --dataset ~/.meept/learning/datasets/code.jsonl \
        --output ~/.meept/adapters/code/lfm2.5-8b-v1 \
        --rank 16 \
        --epochs 3

    # Optional YAML config (config/training/lora_lfm2.5_*.yaml) overrides defaults:
    python scripts/train_lora.py --model lfm2.5-1.2b --dataset ... --output ... \
        --config config/training/lora_lfm2.5_1.2b.yaml
"""

from __future__ import annotations

import argparse
import os
import sys
from pathlib import Path


# Official Hugging Face IDs (Liquid AI). Legacy LFM/* names do not resolve.
DEFAULT_MODEL_MAP = {
    "lfm2.5-8b": "LiquidAI/LFM2.5-8B-A1B",
    "lfm2.5-1.2b": "LiquidAI/LFM2.5-1.2B-Instruct",
}

DEFAULT_TARGET_MODULES = [
    "q_proj",
    "k_proj",
    "v_proj",
    "o_proj",
    "gate_proj",
    "up_proj",
    "down_proj",
]


def _supports_bf16() -> bool:
    import torch

    if torch.cuda.is_available():
        return bool(getattr(torch.cuda, "is_bf16_supported", lambda: False)())
    # Apple MPS / CPU: prefer fp32 training args; load weights in float32.
    return False


def _load_yaml_config(path: str | None) -> dict:
    """Load optional training YAML. Returns empty dict when path is None/missing."""
    if not path:
        return {}
    p = Path(path).expanduser()
    if not p.is_file():
        print(f"warning: config not found: {p}", file=sys.stderr)
        return {}
    try:
        import yaml  # type: ignore
    except ImportError:
        print(
            "warning: PyYAML not installed; ignoring --config "
            "(pip install pyyaml)",
            file=sys.stderr,
        )
        return {}
    with p.open() as f:
        data = yaml.safe_load(f) or {}
    if not isinstance(data, dict):
        print(f"warning: config root must be a mapping: {p}", file=sys.stderr)
        return {}
    return data


def _merge_settings(args, yaml_cfg: dict) -> dict:
    """Merge CLI args with YAML config. CLI wins when explicitly set."""
    model_cfg = yaml_cfg.get("model") or {}
    lora_cfg = yaml_cfg.get("lora") or {}
    train_cfg = yaml_cfg.get("training") or {}

    name_or_path = model_cfg.get("name_or_path") or DEFAULT_MODEL_MAP.get(
        args.model, DEFAULT_MODEL_MAP["lfm2.5-8b"]
    )

    rank = args.rank if args.rank is not None else int(lora_cfg.get("rank", 16))
    alpha = lora_cfg.get("alpha")
    if alpha is None:
        alpha = rank * 2
    dropout = float(lora_cfg.get("dropout", 0.05))
    target_modules = lora_cfg.get("target_modules") or list(DEFAULT_TARGET_MODULES)

    epochs = args.epochs if args.epochs is not None else int(train_cfg.get("epochs", 3))
    batch_size = int(train_cfg.get("batch_size", 4))
    grad_accum = int(train_cfg.get("gradient_accumulation", 4))
    lr = float(train_cfg.get("learning_rate", 1.0e-4))
    max_seq = int(train_cfg.get("max_seq_length", 2048))

    return {
        "name_or_path": name_or_path,
        "rank": rank,
        "alpha": int(alpha),
        "dropout": dropout,
        "target_modules": list(target_modules),
        "epochs": epochs,
        "batch_size": batch_size,
        "grad_accum": grad_accum,
        "lr": lr,
        "max_seq": max_seq,
    }


def _build_trainer(model, tokenizer, dataset, settings: dict, use_bf16: bool, output_dir: str):
    """Build SFTTrainer with TRL API compatibility (SFTConfig vs legacy kwargs)."""
    import torch
    from transformers import TrainingArguments

    common = dict(
        per_device_train_batch_size=settings["batch_size"],
        gradient_accumulation_steps=settings["grad_accum"],
        num_train_epochs=settings["epochs"],
        output_dir=output_dir,
        logging_steps=10,
        save_strategy="epoch",
        learning_rate=settings["lr"],
        bf16=use_bf16,
        fp16=(not use_bf16) and torch.cuda.is_available(),
    )

    try:
        from trl import SFTConfig, SFTTrainer

        sft_args = SFTConfig(
            **common,
            dataset_text_field="formatted",
            max_seq_length=settings["max_seq"],
        )
        # Prefer processing_class (newer TRL); fall back to tokenizer= kwarg.
        try:
            return SFTTrainer(
                model=model,
                train_dataset=dataset,
                args=sft_args,
                processing_class=tokenizer,
            )
        except TypeError:
            return SFTTrainer(
                model=model,
                train_dataset=dataset,
                args=sft_args,
                tokenizer=tokenizer,
            )
    except TypeError:
        # Older/newer TRL may reject processing_class or max_seq_length placement.
        pass
    except ImportError:
        pass

    from trl import SFTTrainer

    try:
        # Mid-era TRL: text field / max length as SFTTrainer kwargs.
        return SFTTrainer(
            model=model,
            train_dataset=dataset,
            dataset_text_field="formatted",
            max_seq_length=settings["max_seq"],
            args=TrainingArguments(**common),
            tokenizer=tokenizer,
        )
    except TypeError as exc:
        print(
            f"SFTTrainer API incompatible with installed trl: {exc}\n"
            "Try: pip install -U trl peft transformers datasets",
            file=sys.stderr,
        )
        raise


def main() -> int:
    parser = argparse.ArgumentParser(description="Train LoRA adapter for LFM2.5")
    parser.add_argument(
        "--model",
        required=True,
        choices=["lfm2.5-8b", "lfm2.5-1.2b"],
        help="Base model to train adapter on",
    )
    parser.add_argument(
        "--dataset",
        required=True,
        help="Path to JSONL training dataset",
    )
    parser.add_argument(
        "--output",
        required=True,
        help="Output directory for trained adapter",
    )
    parser.add_argument(
        "--rank",
        type=int,
        default=None,
        help="LoRA rank (default: 16 or value from --config)",
    )
    parser.add_argument(
        "--epochs",
        type=int,
        default=None,
        help="Number of training epochs (default: 3 or value from --config)",
    )
    parser.add_argument(
        "--config",
        default=None,
        help="Optional YAML training config (config/training/lora_lfm2.5_*.yaml)",
    )
    args = parser.parse_args()

    dataset_path = Path(args.dataset).expanduser()
    if not dataset_path.is_file():
        print(f"error: dataset not found: {dataset_path}", file=sys.stderr)
        return 1
    if dataset_path.stat().st_size == 0:
        print(f"error: dataset is empty: {dataset_path}", file=sys.stderr)
        return 1

    yaml_cfg = _load_yaml_config(args.config)
    # Auto-pick matching YAML when --config omitted but stock file exists.
    if not yaml_cfg and args.config is None:
        script_root = Path(__file__).resolve().parent.parent
        stock_name = {
            "lfm2.5-8b": "lora_lfm2.5_8b.yaml",
            "lfm2.5-1.2b": "lora_lfm2.5_1.2b.yaml",
        }.get(args.model)
        if stock_name:
            stock = script_root / "config" / "training" / stock_name
            if stock.is_file():
                yaml_cfg = _load_yaml_config(str(stock))
                if yaml_cfg:
                    print(f"loaded training config: {stock}")

    settings = _merge_settings(args, yaml_cfg)

    import torch
    from datasets import load_dataset
    from peft import LoraConfig, get_peft_model
    from transformers import AutoModelForCausalLM, AutoTokenizer

    use_bf16 = _supports_bf16()
    if use_bf16:
        dtype = torch.bfloat16
    elif torch.cuda.is_available():
        dtype = torch.float16
    else:
        dtype = torch.float32

    model_id = settings["name_or_path"]
    print(f"loading base model: {model_id} (dtype={dtype})")

    # Load tokenizer (required by SFTTrainer for packing/tokenization).
    tokenizer = AutoTokenizer.from_pretrained(model_id, trust_remote_code=True)
    if tokenizer.pad_token is None:
        tokenizer.pad_token = tokenizer.eos_token

    # Load model with dtype matching training precision (avoid bf16 weights + fp16 amp).
    model = AutoModelForCausalLM.from_pretrained(
        model_id,
        torch_dtype=dtype,
        device_map="auto" if torch.cuda.is_available() else None,
        trust_remote_code=True,
    )

    # LoRA config — attention + MLP targets for LFM2.5 hybrid architecture.
    lora_config = LoraConfig(
        r=settings["rank"],
        lora_alpha=settings["alpha"],
        target_modules=settings["target_modules"],
        lora_dropout=settings["dropout"],
        bias="none",
        task_type="CAUSAL_LM",
    )

    model = get_peft_model(model, lora_config)
    model.print_trainable_parameters()

    # Load dataset from JSONL file
    dataset = load_dataset("json", data_files=str(dataset_path), split="train")
    if len(dataset) == 0:
        print(f"error: dataset has 0 examples: {dataset_path}", file=sys.stderr)
        return 1

    # Preprocess into formatted text field with chat template
    def format_example(example):
        parts = []
        parts.append(f"### Instruction:\n{example['instruction']}")
        input_text = example.get("input", "")
        if input_text:
            parts.append(f"### Input:\n{input_text}")
        parts.append(f"### Response:\n{example['output']}")
        return {"formatted": "\n\n".join(parts)}

    dataset = dataset.map(format_example)

    output_dir = str(Path(args.output).expanduser())
    os.makedirs(output_dir, exist_ok=True)

    trainer = _build_trainer(model, tokenizer, dataset, settings, use_bf16, output_dir)
    trainer.train()
    trainer.save_model()
    # Persist tokenizer alongside adapter for inference loaders.
    try:
        tokenizer.save_pretrained(output_dir)
    except Exception as exc:  # noqa: BLE001 — best-effort
        print(f"warning: could not save tokenizer: {exc}", file=sys.stderr)
    print(f"Adapter saved to {output_dir}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
