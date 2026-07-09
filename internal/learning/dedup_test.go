package learning

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestIsDuplicateFalse(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	existingFile := filepath.Join(tmp, "code.jsonl")

	// No file exists yet -> not duplicate.
	example := TrainingExample{Instruction: "how to parse json"}
	dup, err := IsDuplicate(example, existingFile)
	if err != nil {
		t.Fatalf("IsDuplicate failed: %v", err)
	}
	if dup {
		t.Error("expected not duplicate for missing file")
	}
}

func TestIsDuplicateTrue(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	existingFile := filepath.Join(tmp, "code.jsonl")

	// Write an existing example to the file.
	existing := TrainingExample{Instruction: "how to parse json in go", Output: "use encoding/json"}
	data, err := json.Marshal(existing)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(existingFile, data, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Same instruction -> duplicate.
	newExample := TrainingExample{Instruction: "how to parse json in go", Output: "different output"}
	dup, err := IsDuplicate(newExample, existingFile)
	if err != nil {
		t.Fatalf("IsDuplicate failed: %v", err)
	}
	if !dup {
		t.Error("expected duplicate")
	}

	// Different instruction -> not duplicate.
	other := TrainingExample{Instruction: "how to write tests in go"}
	dup2, err := IsDuplicate(other, existingFile)
	if err != nil {
		t.Fatalf("IsDuplicate failed: %v", err)
	}
	if dup2 {
		t.Error("expected not duplicate for different instruction")
	}
}
