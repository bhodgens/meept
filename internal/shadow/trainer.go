package shadow

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// TrainerBackend represents a training backend.
type TrainerBackend string

const (
	TrainerUnsloth      TrainerBackend = "unsloth"
	TrainerAxolotl      TrainerBackend = "axolotl"
	TrainerTRL          TrainerBackend = "trl"
	TrainerLlamaFactory TrainerBackend = "llama-factory"
)

// Trainer manages the training execution process.
type Trainer struct {
	config *AdaptersConfig
	logger *slog.Logger
}

// NewTrainer creates a new trainer.
func NewTrainer(config *AdaptersConfig, logger *slog.Logger) *Trainer {
	if logger == nil {
		logger = slog.Default()
	}
	return &Trainer{
		config: config,
		logger: logger,
	}
}

// TrainOptions specifies training parameters.
type TrainOptions struct {
	Backend     TrainerBackend
	BaseModel   string
	DataPath    string
	OutputDir   string
	AdapterName string

	// LoRA parameters (override config if set)
	Rank         int
	Alpha        int
	Dropout      float64
	LearningRate float64
	Epochs       int
	BatchSize    int

	// Callbacks
	OnOutput   func(line string)
	OnProgress func(epoch int, loss float64)
}

// TrainResult contains the result of a training run.
type TrainResult struct {
	Success      bool
	AdapterPath  string
	FinalLoss    float64
	EvalScore    float64
	RecordsUsed  int
	Duration     time.Duration
	ErrorMessage string
}

// DetectBackend attempts to detect which training backends are available.
func (t *Trainer) DetectBackend() ([]TrainerBackend, error) {
	var available []TrainerBackend

	// Check for Python-based trainers
	pythonCheck := []struct {
		backend TrainerBackend
		module  string
	}{
		{TrainerUnsloth, "unsloth"},
		{TrainerAxolotl, "axolotl"},
		{TrainerTRL, "trl"},
	}

	for _, check := range pythonCheck {
		cmd := exec.Command("python3", "-c", fmt.Sprintf("import %s", check.module))
		if err := cmd.Run(); err == nil {
			available = append(available, check.backend)
		}
	}

	// Check for llama-factory CLI
	if _, err := exec.LookPath("llamafactory-cli"); err == nil {
		available = append(available, TrainerLlamaFactory)
	}

	return available, nil
}

// Train executes the training process.
func (t *Trainer) Train(ctx context.Context, opts TrainOptions) (*TrainResult, error) {
	startTime := time.Now()

	// Merge options with config defaults
	if opts.Rank == 0 {
		opts.Rank = t.config.LoRA.Rank
	}
	if opts.Alpha == 0 {
		opts.Alpha = t.config.LoRA.Alpha
	}
	if opts.Dropout == 0 {
		opts.Dropout = t.config.LoRA.Dropout
	}
	if opts.LearningRate == 0 {
		opts.LearningRate = t.config.LoRA.LearningRate
	}
	if opts.Epochs == 0 {
		opts.Epochs = t.config.LoRA.Epochs
	}
	if opts.BatchSize == 0 {
		opts.BatchSize = t.config.LoRA.BatchSize
	}

	// Ensure output directory exists
	if opts.OutputDir == "" {
		opts.OutputDir = expandPath(t.config.AdapterDir)
	}
	if err := os.MkdirAll(opts.OutputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	// Select training method based on backend
	var result *TrainResult
	var err error

	switch opts.Backend {
	case TrainerUnsloth:
		result, err = t.trainWithUnsloth(ctx, opts)
	case TrainerAxolotl:
		result, err = t.trainWithAxolotl(ctx, opts)
	case TrainerTRL:
		result, err = t.trainWithTRL(ctx, opts)
	case TrainerLlamaFactory:
		result, err = t.trainWithLlamaFactory(ctx, opts)
	default:
		// Auto-detect backend
		backends, _ := t.DetectBackend()
		if len(backends) == 0 {
			return nil, fmt.Errorf("no training backend available; install unsloth, axolotl, or trl")
		}
		opts.Backend = backends[0]
		t.logger.Info("Auto-selected training backend", "backend", opts.Backend)
		return t.Train(ctx, opts)
	}

	if result != nil {
		result.Duration = time.Since(startTime)
	}

	return result, err
}

// trainWithUnsloth trains using the Unsloth library.
func (t *Trainer) trainWithUnsloth(ctx context.Context, opts TrainOptions) (*TrainResult, error) {
	// Generate training script
	script := t.generateUnslothScript(opts)
	scriptPath := filepath.Join(opts.OutputDir, "train_unsloth.py")

	if err := os.WriteFile(scriptPath, []byte(script), 0644); err != nil {
		return nil, fmt.Errorf("failed to write training script: %w", err)
	}

	t.logger.Info("Starting Unsloth training",
		"base_model", opts.BaseModel,
		"data_path", opts.DataPath,
		"output_dir", opts.OutputDir,
	)

	// Execute training
	cmd := exec.CommandContext(ctx, "python3", scriptPath)
	cmd.Dir = opts.OutputDir

	return t.executeTraining(cmd, opts)
}

// trainWithAxolotl trains using Axolotl.
func (t *Trainer) trainWithAxolotl(ctx context.Context, opts TrainOptions) (*TrainResult, error) {
	// Generate Axolotl config
	configPath := filepath.Join(opts.OutputDir, "axolotl_config.yaml")
	config := t.generateAxolotlConfig(opts)

	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		return nil, fmt.Errorf("failed to write config: %w", err)
	}

	t.logger.Info("Starting Axolotl training",
		"base_model", opts.BaseModel,
		"config", configPath,
	)

	// Execute training
	cmd := exec.CommandContext(ctx, "accelerate", "launch", "-m", "axolotl.cli.train", configPath)
	cmd.Dir = opts.OutputDir

	return t.executeTraining(cmd, opts)
}

// trainWithTRL trains using TRL (Transformers Reinforcement Learning).
func (t *Trainer) trainWithTRL(ctx context.Context, opts TrainOptions) (*TrainResult, error) {
	// Generate TRL training script
	script := t.generateTRLScript(opts)
	scriptPath := filepath.Join(opts.OutputDir, "train_trl.py")

	if err := os.WriteFile(scriptPath, []byte(script), 0644); err != nil {
		return nil, fmt.Errorf("failed to write training script: %w", err)
	}

	t.logger.Info("Starting TRL DPO training",
		"base_model", opts.BaseModel,
		"data_path", opts.DataPath,
	)

	// Execute training
	cmd := exec.CommandContext(ctx, "python3", scriptPath)
	cmd.Dir = opts.OutputDir

	return t.executeTraining(cmd, opts)
}

// trainWithLlamaFactory trains using LLaMA-Factory.
func (t *Trainer) trainWithLlamaFactory(ctx context.Context, opts TrainOptions) (*TrainResult, error) {
	// Generate LLaMA-Factory dataset config
	datasetConfig := t.generateLlamaFactoryDataset(opts)
	datasetPath := filepath.Join(opts.OutputDir, "dataset_info.json")

	if err := os.WriteFile(datasetPath, []byte(datasetConfig), 0644); err != nil {
		return nil, fmt.Errorf("failed to write dataset config: %w", err)
	}

	t.logger.Info("Starting LLaMA-Factory training",
		"base_model", opts.BaseModel,
		"data_path", opts.DataPath,
	)

	// Execute training using llamafactory-cli
	args := []string{
		"train",
		"--model_name_or_path", opts.BaseModel,
		"--dataset_dir", opts.OutputDir,
		"--dataset", "meept_dpo",
		"--output_dir", filepath.Join(opts.OutputDir, opts.AdapterName),
		"--finetuning_type", "lora",
		"--lora_rank", fmt.Sprintf("%d", opts.Rank),
		"--lora_alpha", fmt.Sprintf("%d", opts.Alpha),
		"--num_train_epochs", fmt.Sprintf("%d", opts.Epochs),
		"--per_device_train_batch_size", fmt.Sprintf("%d", opts.BatchSize),
		"--learning_rate", fmt.Sprintf("%e", opts.LearningRate),
		"--stage", "dpo",
	}

	cmd := exec.CommandContext(ctx, "llamafactory-cli", args...)
	cmd.Dir = opts.OutputDir

	return t.executeTraining(cmd, opts)
}

// executeTraining runs the training command and monitors output.
func (t *Trainer) executeTraining(cmd *exec.Cmd, opts TrainOptions) (*TrainResult, error) {
	result := &TrainResult{}

	// Set up pipes for stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start training: %w", err)
	}

	// Monitor output
	go t.monitorOutput(stdout, opts.OnOutput)
	go t.monitorOutput(stderr, opts.OnOutput)

	// Wait for completion
	err = cmd.Wait()
	if err != nil {
		result.Success = false
		result.ErrorMessage = err.Error()
		return result, fmt.Errorf("training failed: %w", err)
	}

	result.Success = true
	result.AdapterPath = filepath.Join(cmd.Dir, opts.AdapterName)

	// Try to read training results
	resultsPath := filepath.Join(result.AdapterPath, "trainer_state.json")
	if data, err := os.ReadFile(resultsPath); err == nil {
		var state struct {
			BestMetric float64 `json:"best_metric"`
			LogHistory []struct {
				Loss float64 `json:"loss"`
			} `json:"log_history"`
		}
		if err := json.Unmarshal(data, &state); err == nil {
			if len(state.LogHistory) > 0 {
				result.FinalLoss = state.LogHistory[len(state.LogHistory)-1].Loss
			}
			result.EvalScore = state.BestMetric
		}
	}

	return result, nil
}

// monitorOutput reads and logs output from the training process.
func (t *Trainer) monitorOutput(r io.Reader, callback func(string)) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		t.logger.Debug("Training output", "line", line)
		if callback != nil {
			callback(line)
		}
	}
}

// generateUnslothScript generates a Python training script for Unsloth.
func (t *Trainer) generateUnslothScript(opts TrainOptions) string {
	return fmt.Sprintf(`
from unsloth import FastLanguageModel
from datasets import load_dataset
from trl import DPOTrainer, DPOConfig
import torch

# Load model with Unsloth optimizations
model, tokenizer = FastLanguageModel.from_pretrained(
    model_name="%s",
    max_seq_length=2048,
    dtype=torch.float16,
    load_in_4bit=True,
)

# Apply LoRA
model = FastLanguageModel.get_peft_model(
    model,
    r=%d,
    lora_alpha=%d,
    lora_dropout=%f,
    target_modules=["q_proj", "k_proj", "v_proj", "o_proj",
                    "gate_proj", "up_proj", "down_proj"],
)

# Load DPO dataset
dataset = load_dataset("json", data_files="%s", split="train")

def format_dataset(example):
    return {
        "prompt": example["prompt"],
        "chosen": example["chosen"],
        "rejected": example["rejected"],
    }

dataset = dataset.map(format_dataset)

# Configure DPO training
training_args = DPOConfig(
    output_dir="%s/%s",
    num_train_epochs=%d,
    per_device_train_batch_size=%d,
    gradient_accumulation_steps=%d,
    learning_rate=%e,
    warmup_ratio=%f,
    max_grad_norm=%f,
    beta=%f,
    logging_steps=10,
    save_steps=100,
    fp16=True,
)

# Train
trainer = DPOTrainer(
    model=model,
    args=training_args,
    train_dataset=dataset,
    tokenizer=tokenizer,
)

trainer.train()
trainer.save_model()
print("Training complete!")
`, opts.BaseModel, opts.Rank, opts.Alpha, opts.Dropout,
		opts.DataPath, opts.OutputDir, opts.AdapterName,
		opts.Epochs, opts.BatchSize, t.config.LoRA.GradientAccumulation,
		opts.LearningRate, t.config.LoRA.WarmupRatio, t.config.LoRA.MaxGradNorm,
		t.config.DPO.Beta)
}

// generateAxolotlConfig generates an Axolotl YAML configuration.
func (t *Trainer) generateAxolotlConfig(opts TrainOptions) string {
	return fmt.Sprintf(`
base_model: %s
model_type: AutoModelForCausalLM
tokenizer_type: AutoTokenizer
load_in_4bit: true

datasets:
  - path: %s
    type: chatml.load_dpo

output_dir: %s/%s

adapter: lora
lora_r: %d
lora_alpha: %d
lora_dropout: %f
lora_target_modules:
  - q_proj
  - k_proj
  - v_proj
  - o_proj
  - gate_proj
  - up_proj
  - down_proj

sequence_len: 2048
sample_packing: true
pad_to_sequence_len: true

wandb_project:
wandb_entity:
wandb_watch:
wandb_name:
wandb_log_model:

gradient_accumulation_steps: %d
micro_batch_size: %d
num_epochs: %d
optimizer: adamw_bnb_8bit
lr_scheduler: cosine
learning_rate: %e

train_on_inputs: false
group_by_length: false
bf16: auto
fp16:
tf32: false

gradient_checkpointing: true
early_stopping_patience:
resume_from_checkpoint:
local_rank:
logging_steps: 1
xformers_attention:
flash_attention: true
`, opts.BaseModel, opts.DataPath, opts.OutputDir, opts.AdapterName,
		opts.Rank, opts.Alpha, opts.Dropout,
		t.config.LoRA.GradientAccumulation, opts.BatchSize, opts.Epochs, opts.LearningRate)
}

// generateTRLScript generates a TRL training script.
func (t *Trainer) generateTRLScript(opts TrainOptions) string {
	return fmt.Sprintf(`
from transformers import AutoModelForCausalLM, AutoTokenizer
from datasets import load_dataset
from trl import DPOTrainer, DPOConfig
from peft import LoraConfig, get_peft_model
import torch

# Load base model
model = AutoModelForCausalLM.from_pretrained(
    "%s",
    torch_dtype=torch.float16,
    device_map="auto",
)

tokenizer = AutoTokenizer.from_pretrained("%s")
if tokenizer.pad_token is None:
    tokenizer.pad_token = tokenizer.eos_token

# Configure LoRA
lora_config = LoraConfig(
    r=%d,
    lora_alpha=%d,
    lora_dropout=%f,
    target_modules=["q_proj", "k_proj", "v_proj", "o_proj"],
    task_type="CAUSAL_LM",
)

model = get_peft_model(model, lora_config)

# Load dataset
dataset = load_dataset("json", data_files="%s", split="train")

# Configure DPO
training_args = DPOConfig(
    output_dir="%s/%s",
    num_train_epochs=%d,
    per_device_train_batch_size=%d,
    gradient_accumulation_steps=%d,
    learning_rate=%e,
    warmup_ratio=%f,
    max_grad_norm=%f,
    beta=%f,
    logging_steps=10,
    save_strategy="epoch",
    fp16=True,
    remove_unused_columns=False,
)

# Train
trainer = DPOTrainer(
    model=model,
    args=training_args,
    train_dataset=dataset,
    tokenizer=tokenizer,
)

trainer.train()
trainer.save_model()
print("Training complete!")
`, opts.BaseModel, opts.BaseModel, opts.Rank, opts.Alpha, opts.Dropout,
		opts.DataPath, opts.OutputDir, opts.AdapterName,
		opts.Epochs, opts.BatchSize, t.config.LoRA.GradientAccumulation,
		opts.LearningRate, t.config.LoRA.WarmupRatio, t.config.LoRA.MaxGradNorm,
		t.config.DPO.Beta)
}

// generateLlamaFactoryDataset generates a LLaMA-Factory dataset configuration.
func (t *Trainer) generateLlamaFactoryDataset(opts TrainOptions) string {
	config := map[string]any{
		"meept_dpo": map[string]any{
			"file_name":  opts.DataPath,
			"ranking":    true,
			"formatting": "sharegpt",
			"columns": map[string]string{
				"messages": "prompt",
				"chosen":   "chosen",
				"rejected": "rejected",
			},
		},
	}

	data, _ := json.MarshalIndent(config, "", "  ")
	return string(data)
}

// GenerateTrainingScript generates a standalone training script for manual execution.
func (t *Trainer) GenerateTrainingScript(opts TrainOptions) (string, error) {
	switch opts.Backend {
	case TrainerUnsloth:
		return t.generateUnslothScript(opts), nil
	case TrainerTRL:
		return t.generateTRLScript(opts), nil
	case TrainerAxolotl:
		return t.generateAxolotlConfig(opts), nil
	default:
		return "", fmt.Errorf("unsupported backend: %s", opts.Backend)
	}
}
