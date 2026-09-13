package llm

import (
	"slices"
	"testing"
)

// llamaSpawnCommand must pass --jinja. Without it llama-server does not render
// the model's chat template, the tool list never reaches the prompt, and the
// model answers in prose instead of emitting a call - so every model
// discovered on disk would be unable to use tools.
func TestLlamaSpawnCommand_EnablesJinja(t *testing.T) {
	args := llamaSpawnCommand("org--model-file", "/models/file.gguf")
	if !slices.Contains(args, "--jinja") {
		t.Errorf("llamaSpawnCommand = %v, want --jinja present", args)
	}
	if args[0] != "llama-server" {
		t.Errorf("args[0] = %q, want llama-server", args[0])
	}
	// The model path must still be passed, and the alias must be stable per
	// model key (it is the name the daemon sends on requests).
	if !slices.Contains(args, "/models/file.gguf") {
		t.Errorf("llamaSpawnCommand = %v, want the model path", args)
	}
	if slices.Index(args, "--alias") < 0 {
		t.Errorf("llamaSpawnCommand = %v, want --alias", args)
	}
	if again := llamaSpawnCommand("org--model-file", "/models/file.gguf"); !slices.Equal(args, again) {
		t.Errorf("not deterministic: %v vs %v", args, again)
	}
	// A different model key gets a different alias.
	other := llamaSpawnCommand("other--model-file", "/models/file.gguf")
	if slices.Equal(args, other) {
		t.Error("distinct model keys must produce distinct aliases")
	}
}
