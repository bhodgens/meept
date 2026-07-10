#!/usr/bin/env python3
"""Train LoRA adapter for LFM2.5.

Usage:
    python scripts/train_lora.py \
        --model lfm2.5-8b \
        --dataset ~/.meept/learning/datasets/code.jsonl \
        --output ~/.meept/adapters/code/lfm2.5-8b-v1 \
        --rank 16 \
        --epochs 3
"""

from __future__ import annotations

import argparse
import sys

import torch
from datasets import load_dataset
from peft import LoraConfig, get_peft_model
from transformers import AutoModelForCausalLM, AutoTokenizer, TrainingArguments


def _supports_bf16() -> bool:
    if torch.cuda.is_available():
        return bool(getattr(torch.cuda, "is_bf16_supported", lambda: False)())
    # Apple MPS / CPU: prefer fp32 training args; load weights in float32.
    return False


def _build_trainer(model, dataset, args, use_bf16: bool):
    """Build SFTTrainer with TRL API compatibility (SFTConfig vs legacy kwargs)."""
    common = dict(
        per_device_train_batch_size=4,
        gradient_accumulation_steps=4,
        num_train_epochs=args.epochs,
        output_dir=args.output,
        logging_steps=10,
        save_strategy="epoch",
        learning_rate=1e-4,
        bf16=use_bf16,
        fp16=(not use_bf16) and torch.cuda.is_available(),
    )

    try:
        from trl import SFTConfig, SFTTrainer

        sft_args = SFTConfig(
            **common,
            dataset_text_field="formatted",
            max_seq_length=2048,
        )
        return SFTTrainer(
            model=model,
            train_dataset=dataset,
            args=sft_args,
            processing_class=None,
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
            max_seq_length=2048,
            args=TrainingArguments(**common),
        )
    except TypeError as exc:
        print(
            f"SFTTrainer API incompatible with installed trl: {exc}\n"
            "Try: pip install -U trl peft transformers datasets",
            file=sys.stderr,
        )
        raise


def main():
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
        default=16,
        help="LoRA rank (default: 16)",
    )
    parser.add_argument(
        "--epochs",
        type=int,
        default=3,
        help="Number of training epochs (default: 3)",
    )
    args = parser.parse_args()

    # Map CLI model names to HuggingFace model IDs
    model_map = {
        "lfm2.5-8b": "LFM/LFM2.5-8B",
        "lfm2.5-1.2b": "LFM/LFM2.5-1.2B",
    }

    use_bf16 = _supports_bf16()
    if use_bf16:
        dtype = torch.bfloat16
    elif torch.cuda.is_available():
        dtype = torch.float16
    else:
        dtype = torch.float32

    # Load tokenizer
    tokenizer = AutoTokenizer.from_pretrained(model_map[args.model])
    if tokenizer.pad_token is None:
        tokenizer.pad_token = tokenizer.eos_token

    # Load model with dtype matching training precision (avoid bf16 weights + fp16 amp).
    model = AutoModelForCausalLM.from_pretrained(
        model_map[args.model],
        torch_dtype=dtype,
        device_map="auto" if torch.cuda.is_available() else None,
    )

    # LoRA config - target modules specific to LFM2.5 hybrid architecture
    lora_config = LoraConfig(
        r=args.rank,
        lora_alpha=args.rank * 2,
        target_modules=[
            "q_proj", "k_proj", "v_proj", "o_proj",  # Attention
            "gate_proj", "up_proj", "down_proj",     # MLP
            # "conv_*"  # LFM-specific conv layers (discover via inspect script)
        ],
        lora_dropout=0.05,
        bias="none",
        task_type="CAUSAL_LM",
    )

    model = get_peft_model(model, lora_config)
    model.print_trainable_parameters()

    # Load dataset from JSONL file
    dataset = load_dataset("json", data_files=args.dataset, split="train")

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

    trainer = _build_trainer(model, dataset, args, use_bf16)
    trainer.train()
    trainer.save_model()
    print(f"Adapter saved to {args.output}")


if __name__ == "__main__":
    main()
