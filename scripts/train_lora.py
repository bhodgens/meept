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

import argparse

import torch
from datasets import load_dataset
from peft import LoraConfig, get_peft_model
from transformers import AutoModelForCausalLM, AutoTokenizer, TrainingArguments
from trl import SFTTrainer


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

    # Load tokenizer
    tokenizer = AutoTokenizer.from_pretrained(model_map[args.model])
    if tokenizer.pad_token is None:
        tokenizer.pad_token = tokenizer.eos_token

    # Load model in bfloat16 with automatic device mapping
    model = AutoModelForCausalLM.from_pretrained(
        model_map[args.model],
        torch_dtype=torch.bfloat16,
        device_map="auto",
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

    # Training
    trainer = SFTTrainer(
        model=model,
        train_dataset=dataset,
        dataset_text_field="formatted",
        max_seq_length=2048,
        args=TrainingArguments(
            per_device_train_batch_size=4,
            gradient_accumulation_steps=4,
            num_train_epochs=args.epochs,
            output_dir=args.output,
            fp16=True,
            logging_steps=10,
            save_strategy="epoch",
        ),
    )

    trainer.train()
    trainer.save_model()
    print(f"Adapter saved to {args.output}")


if __name__ == "__main__":
    main()
